# shellcheck shell=bash
# Web panel for agent status, update and rollback. Enabled by
# PANEL_CREDENTIAL=user:password. The panel runs as agent; a root broker,
# reachable only through a unix socket, runs the few berth commands it allows.

panel_enabled() { [[ -n ${PANEL_CREDENTIAL:-} ]]; }

panel_secrets() { echo PANEL_CREDENTIAL; }

panel_validate() {
  local cred=$PANEL_CREDENTIAL
  if [[ $cred != *:* || -z ${cred%%:*} || -z ${cred#*:} ]]; then
    echo "PANEL_CREDENTIAL must be user:password with both parts non-empty" >&2
    return 1
  fi
  valid_port PANEL_PORT "${PANEL_PORT:-7690}"
}

panel_start() {
  start_root_service panel_broker berth broker
  start_service panel berth panel
}

panel_health() {
  [[ -S $BERTH_RUN_DIR/broker.sock ]] || return 1
  echo "${PANEL_PORT:-7690}"
}
