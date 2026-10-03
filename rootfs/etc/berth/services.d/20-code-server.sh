# shellcheck shell=bash
# code-server browser IDE, on loopback only; the panel proxies it at /ide/
# and is the only login. Runs when frontends.ide.enabled is set.

code_server_start() {
  exec code-server --bind-addr 127.0.0.1:8080 --auth none --abs-proxy-base-path /ide \
    --disable-telemetry --disable-update-check /workspaces
}

code_server_health() { echo 8080; }
