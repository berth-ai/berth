# shellcheck shell=bash
# ttyd + tmux browser terminal, on loopback only; the panel proxies it at
# /terminal/. Runs when frontends.terminal.enabled is set.

ttyd_start() {
  exec ttyd -W -a -i lo -p 7681 -b /terminal -w /workspaces berth-term
}

ttyd_health() { echo 7681; }
