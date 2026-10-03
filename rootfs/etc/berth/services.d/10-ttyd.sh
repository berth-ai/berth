# shellcheck shell=bash
# ttyd + tmux browser terminal, on loopback only; the panel proxies it at
# /terminal/. Runs when frontends.terminal.enabled is set.

ttyd_start() {
  exec ttyd -W -i lo -p 7681 -b /terminal -w /workspaces tmux new -A -s main
}

ttyd_health() { echo 7681; }
