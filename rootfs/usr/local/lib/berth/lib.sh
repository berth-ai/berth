# shellcheck shell=bash
# Shared helpers for berth-entrypoint and berth-healthcheck.
# The path variables can be overridden only so the tests can run outside a
# container; they are not part of Berth's configuration.

BERTH_SERVICES_DIR=${BERTH_SERVICES_DIR:-/etc/berth/services.d}
BERTH_RUN_DIR=${BERTH_RUN_DIR:-/run/berth}
BERTH_HOME=${BERTH_HOME:-/home/agent}
BERTH_AGENTS_DIR=${BERTH_AGENTS_DIR:-/opt/agents}
BERTH_WORKSPACES=${BERTH_WORKSPACES:-/workspaces}

BERTH_MODULES=()
BERTH_ALL_SECRETS=()
BERTH_PIDS=()
BERTH_PID_NAMES=()

log() { printf 'berth: %s\n' "$*" >&2; }
warn() { printf 'berth: WARNING: %s\n' "$*" >&2; }
die() {
  printf 'berth: ERROR: %s\n' "$*" >&2
  exit 1
}

# module_name FILE prints the function prefix: 20-code-server.sh -> code_server.
module_name() {
  local base=${1##*/}
  base=${base%.sh}
  base=${base#[0-9]*-}
  printf '%s\n' "${base//-/_}"
}

# module_secrets NAME prints the env vars module NAME owns, one per line.
module_secrets() {
  if declare -F "$1_secrets" >/dev/null; then
    "$1_secrets"
  fi
}

# load_modules sources every module in lexical order and records the module
# names and the union of all modules' secrets.
load_modules() {
  local file name var
  BERTH_MODULES=()
  BERTH_ALL_SECRETS=()
  for file in "$BERTH_SERVICES_DIR"/*.sh; do
    [[ -e $file ]] || continue
    # shellcheck source=/dev/null
    source "$file"
    name=$(module_name "$file")
    if ! declare -F "${name}_enabled" >/dev/null || ! declare -F "${name}_start" >/dev/null; then
      die "module $file must define ${name}_enabled and ${name}_start"
    fi
    BERTH_MODULES+=("$name")
    while read -r var; do
      [[ -n $var ]] && BERTH_ALL_SECRETS+=("$var")
    done < <(module_secrets "$name")
  done
}

# valid_port NAME VALUE fails with a message unless VALUE is 1-65535.
valid_port() {
  if [[ $2 =~ ^[0-9]+$ ]] && ((10#$2 >= 1 && 10#$2 <= 65535)); then
    return 0
  fi
  echo "$1 must be a port number between 1 and 65535, got '$2'" >&2
  return 1
}

# start_service NAME ARGV... starts ARGV in the background as agent, without
# any other module's secrets, and records its PID for the supervisor.
start_service() {
  local name=$1 own var
  shift
  local -a scrub=()
  own=$(module_secrets "$name")
  for var in "${BERTH_ALL_SECRETS[@]}"; do
    grep -Fxq -- "$var" <<<"$own" || scrub+=(-u "$var")
  done
  log "starting $name"
  env "${scrub[@]}" gosu agent "$@" &
  BERTH_PIDS+=("$!")
  BERTH_PID_NAMES+=("$name")
}
