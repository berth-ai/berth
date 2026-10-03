# shellcheck shell=bash
# Shared helpers for berth-entrypoint and berth-module-run.
# The path variables can be overridden only so the tests can run outside a
# container; they are not part of Berth's configuration.

BERTH_SERVICES_DIR=${BERTH_SERVICES_DIR:-/etc/berth/services.d}
BERTH_HOME=${BERTH_HOME:-/home/agent}
BERTH_WORKSPACES=${BERTH_WORKSPACES:-/workspaces}

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
