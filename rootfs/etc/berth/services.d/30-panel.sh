# shellcheck shell=bash
# Web panel, always on, as berth-panel. It reaches root and agent only through
# the supervisor's broker socket.

panel_start() { exec berth panel; }

panel_health() { echo 7690; }
