# shellcheck shell=bash
# ttyd + tmux browser terminal. Enabled by WEB_TERMINAL_CREDENTIAL=user:password.

ttyd_enabled() { [[ -n ${WEB_TERMINAL_CREDENTIAL:-} ]]; }

ttyd_secrets() { echo WEB_TERMINAL_CREDENTIAL; }

ttyd_validate() {
  local cred=$WEB_TERMINAL_CREDENTIAL
  if [[ $cred != *:* || -z ${cred%%:*} || -z ${cred#*:} ]]; then
    echo "WEB_TERMINAL_CREDENTIAL must be user:password with both parts non-empty" >&2
    return 1
  fi
  valid_port TTYD_PORT "${TTYD_PORT:-7681}"
}

ttyd_start() {
  start_service ttyd ttyd -W -p "${TTYD_PORT:-7681}" -w /workspaces \
    -c "$WEB_TERMINAL_CREDENTIAL" tmux new -A -s main
}

ttyd_health() { echo "${TTYD_PORT:-7681}"; }
