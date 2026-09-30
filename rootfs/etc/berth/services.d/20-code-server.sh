# shellcheck shell=bash
# code-server browser IDE. Enabled by CODE_SERVER_PASSWORD.

code_server_enabled() { [[ -n ${CODE_SERVER_PASSWORD:-} ]]; }

code_server_secrets() { echo CODE_SERVER_PASSWORD; }

code_server_validate() { valid_port CODE_SERVER_PORT "${CODE_SERVER_PORT:-8080}"; }

code_server_start() {
  start_service code_server env PASSWORD="$CODE_SERVER_PASSWORD" \
    code-server --bind-addr "0.0.0.0:${CODE_SERVER_PORT:-8080}" --auth password \
    --disable-telemetry --disable-update-check /workspaces
}

code_server_health() { echo "${CODE_SERVER_PORT:-8080}"; }
