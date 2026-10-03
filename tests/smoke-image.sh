#!/usr/bin/env bash
# smoke-image.sh IMAGE: boot IMAGE in every configuration CI requires and
# check agents, services, settings, persistence, identity and hygiene. Needs docker and jq.
set -euo pipefail

image=${1:?usage: smoke-image.sh IMAGE}
prefix=berth-smoke-$$
home_vol=$prefix-home
agents_vol=$prefix-agents
config_vol=$prefix-config
ws=$(mktemp -d)
first_boot_timeout=${SMOKE_FIRST_BOOT_TIMEOUT:-600}
boot_timeout=${SMOKE_BOOT_TIMEOUT:-200}

cleanup() {
  docker ps -aq --filter "name=^$prefix-" | xargs -r docker rm -f >/dev/null 2>&1 || true
  docker volume rm -f "$home_vol" "$agents_vol" "$config_vol" >/dev/null 2>&1 || true
  docker run --rm -v "$ws:/w" --entrypoint rm "$image" -rf /w/keep >/dev/null 2>&1 || true
  rm -rf "$ws"
}
trap cleanup EXIT

step() { printf '\n== %s\n' "$*"; }
fail() {
  echo "smoke: FAIL: $*" >&2
  exit 1
}

# boot NAME TIMEOUT [docker run args...]
boot() {
  local name=$1 timeout=$2
  shift 2
  docker run -d --name "$prefix-$name" --security-opt no-new-privileges:true \
    -v "$home_vol:/home/agent" -v "$agents_vol:/opt/agents" -v "$config_vol:/var/lib/berth" \
    -v "$ws:/workspaces" "$@" "$image" >/dev/null
  wait_healthy "$name" "$timeout"
}

# wait_healthy NAME TIMEOUT waits for Docker's health status (used at boot).
wait_healthy() {
  local name=$1 timeout=$2 status
  for ((i = 0; i < timeout; i++)); do
    status=$(docker inspect -f '{{.State.Health.Status}}' "$prefix-$name")
    [[ $status == healthy ]] && return 0
    if [[ $(docker inspect -f '{{.State.Running}}' "$prefix-$name") != true ]]; then
      docker logs "$prefix-$name" >&2 || true
      fail "$name exited before becoming healthy"
    fi
    sleep 1
  done
  docker logs "$prefix-$name" >&2 || true
  fail "$name not healthy within ${timeout}s"
}

# wait_ok NAME TIMEOUT polls berth health directly. After the start period
# Docker refreshes its health status only every 30 s.
wait_ok() {
  for ((i = 0; i < $2; i++)); do
    x "$1" berth health >/dev/null 2>&1 && return 0
    sleep 1
  done
  x "$1" berth health || true
  fail "$1 not healthy within $2s"
}

# service_pid NAME SERVICE prints the supervisor's pid for SERVICE.
service_pid() {
  x "$1" curl -fsS --unix-socket /run/berth/broker.sock http://b/status |
    jq -r --arg s "$2" '.services[] | select(.name == $s) | .pid'
}

# listens_on_loopback_only NAME PORT: there is at least one listening socket
# on PORT, and every one is bound to a loopback address: 127.0.0.1, ::1 or
# ::ffff:127.0.0.1, as /proc/net/tcp{,6} spell them.
listens_on_loopback_only() {
  local hex addrs
  hex=$(printf '%04X' "$2")
  # shellcheck disable=SC2016  # an awk program, expanded by awk
  addrs=$(x "$1" awk -v suffix=":$hex" '$4 == "0A" && substr($2, length($2) - 4) == suffix {print $2}' \
    /proc/net/tcp /proc/net/tcp6)
  [[ -n $addrs ]] || return 1
  ! grep -qvE "^(0100007F|00000000000000000000000001000000|0000000000000000FFFF00000100007F):$hex\$" <<<"$addrs"
}

halt() { docker rm -f "$prefix-$1" >/dev/null; }
x() { docker exec "$prefix-$1" "${@:2}"; }
xa() { docker exec -u agent "$prefix-$1" "${@:2}"; }

panel=http://127.0.0.1:7690
jar=/tmp/smoke-cookies
# pc NAME ARGS... runs curl against the panel inside the container with a cookie jar.
pc() { x "$1" curl -sS -b "$jar" -c "$jar" -H 'Content-Type: application/json' -H 'X-Berth-Panel: 1' "${@:2}"; }
code_of() { x "$1" curl -s -o /dev/null -w '%{http_code}' -b "$jar" "${@:2}"; }

# setup_token NAME prints the newest setup token from the container log.
setup_token() {
  docker logs "$prefix-$1" 2>&1 | sed -n 's|.*setup: open http://127\.0\.0\.1:7690/setup#token=\([0-9a-f]\{32\}\).*|\1|p' | tail -n 1
}

step "workspaces owner fixture"
docker run --rm -v "$ws:/workspaces" --entrypoint sh "$image" \
  -c 'touch /workspaces/keep && chown 4242:4242 /workspaces/keep'
[[ $(docker run --rm -v "$ws:/workspaces" --entrypoint stat "$image" -c %u:%g /workspaces/keep) == 4242:4242 ]] ||
  fail "this host's bind mounts do not keep file ownership (Docker Desktop on macOS?); run the smoke test on Linux"

step "first boot without any variable: panel only, setup link in the log"
boot main "$first_boot_timeout"
x main berth health --json | jq -e '.healthy' >/dev/null || fail "not healthy in setup mode"
[[ $(x main curl -fsS --unix-socket /run/berth/broker.sock http://b/status | jq -r '[.services[].name] | join(",")') == panel ]] ||
  fail "setup mode must run only the panel"
token=$(setup_token main)
[[ -n $token ]] || fail "no setup link in the log"
[[ $(code_of main "$panel/") == 303 ]] || fail "/ does not redirect to setup"
[[ $(pc main -o /dev/null -w '%{http_code}' -d '{"token":"'"$(printf '0%.0s' {1..32})"'","user":"smoke","password":"smoke smoke smoke"}' "$panel/api/setup") == 403 ]] ||
  fail "a wrong token was accepted"
sleep 2
pc main -f -d '{"token":"'"$token"'","user":"smoke","password":"smoke smoke smoke"}' "$panel/api/setup" >/dev/null ||
  fail "setup"
wait_ok main 120
[[ $(pc main -o /dev/null -w '%{http_code}' -d '{"token":"'"$token"'","user":"x","password":"xxxxxxxxxxxx"}' "$panel/api/setup") == 409 ]] ||
  fail "the setup token was reusable"

step "agents from the seed"
x main berth status --json | jq -e '.current.source.origin == "image" and .follow_image' >/dev/null ||
  fail "status does not report an image-origin current"
for agent in claude codex opencode; do
  xa main "$agent" --version >/dev/null || fail "$agent --version"
done
for ((i = 0; i < 120; i++)); do
  xa main claude mcp list 2>/dev/null | grep -q playwright && break
  sleep 1
done
xa main claude mcp list | grep -q context7 || fail "MCP servers not registered with claude"

step "process identities and socket"
ps_user() { x main ps -eo user:12,args | awk -v pat="$1" '$0 ~ pat {print $1; exit}'; }
[[ $(ps_user 'berth supervise') == root ]] || fail "supervise not root"
[[ $(ps_user 'berth panel') == berth-panel ]] || fail "panel not berth-panel"
[[ $(ps_user 'ttyd ') == agent ]] || fail "ttyd not agent"
[[ $(x main stat -c '%a %U %G' /run/berth/broker.sock) == "660 root berth-panel" ]] || fail "broker socket ownership"
if xa main curl -s --unix-socket /run/berth/broker.sock http://b/status >/dev/null; then
  fail "agent reached the broker socket"
fi
listens_on_loopback_only main 7681 || fail "ttyd listens beyond loopback (check the -i lo flag)"

step "update is idempotent; rollback works"
x main sh -c 'mkdir -p /tmp/variant && cd /usr/share/berth/agents &&
  jq ".name = \"smoke-variant\"" package.json >/tmp/variant/package.json &&
  jq ".name = \"smoke-variant\" | .packages[\"\"].name = \"smoke-variant\"" package-lock.json >/tmp/variant/package-lock.json'
x main berth update --lock-dir /tmp/variant --json | jq -e '.changed and .installed' >/dev/null || fail "variant update"
x main berth update --lock-dir /tmp/variant --json | jq -e '(.changed | not) and (.installed | not)' >/dev/null ||
  fail "second update is not a no-op"
seed=$(x main jq -r .seed_hash /usr/share/berth/build-info.json)
x main berth rollback --json | jq -e --arg seed "$seed" '.current.hash == $seed' >/dev/null || fail "rollback"
x main berth update --image --json | jq -e '.follow_image' >/dev/null || fail "update --image"

step "shell defaults"
# shellcheck disable=SC2016  # expanded by zsh inside the container
[[ $(xa main zsh -ic 'print -r -- "$EDITOR|$HISTFILE"' </dev/null 2>/dev/null) == "nvim|/home/agent/.local/state/zsh/history" ]] ||
  fail "zsh newuser prompt or missing defaults"
[[ $(xa main nvim --headless '+lua io.write(tostring(vim.o.number))' +qa 2>&1) == true ]] || fail "nvim defaults"
[[ $(xa main tmux -L smoke new-session -d \; show-options -gv history-limit \; kill-server) == 50000 ]] ||
  fail "tmux defaults"
# shellcheck disable=SC2016  # expanded by zsh inside the container
[[ $(xa main zsh -c 'print -l $path | head -n 2 | paste -sd,') == /opt/agents/current/bin,/opt/mise/wrappers ]] ||
  fail "PATH order"

step "login, sessions and proxies"
x main rm -f "$jar"
[[ $(code_of main "$panel/api/status") == 401 ]] || fail "status without a session"
[[ $(code_of main "$panel/terminal/") == 303 ]] || fail "terminal without a session"
[[ $(pc main -o /dev/null -w '%{http_code}' -d '{"user":"smoke","password":"wrong wrong"}' "$panel/api/login") == 401 ]] ||
  fail "wrong password accepted"
[[ $(pc main -o /dev/null -w '%{http_code}' -d '{"user":"smoke","password":"smoke smoke smoke"}' "$panel/api/login") == 429 ]] ||
  fail "no backoff after a failed login"
sleep 2
pc main -f -d '{"user":"smoke","password":"smoke smoke smoke"}' "$panel/api/login" >/dev/null || fail "login"
pc main -f "$panel/api/status" | jq -e '.mode == "normal" and .host.panel_port == "7690" and .agents.current != null' >/dev/null ||
  fail "status"
x main curl -fsS -b "$jar" "$panel/terminal/" | grep -qi ttyd || fail "terminal through the proxy"
ws=$(x main curl -s -o /dev/null -w '%{http_code}' --http1.1 --max-time 3 -b "$jar" \
  -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' \
  -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' -H 'Sec-WebSocket-Protocol: tty' \
  -H "Origin: $panel" "$panel/terminal/ws" || true)
[[ $ws == 101 ]] || fail "terminal WebSocket through the proxy answered $ws"
[[ $(x main curl -s -o /dev/null -w '%{http_code}' --http1.1 --max-time 3 -b "$jar" -H 'Connection: Upgrade' \
  -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' \
  -H 'Origin: http://evil.example' "$panel/terminal/ws" || true) == 403 ]] || fail "cross-origin WebSocket accepted"
[[ $(code_of main "$panel/ide/") == 502 ]] || fail "IDE off must show the unavailable page"
[[ $(x main curl -s -o /dev/null -w '%{http_code}' -b "$jar" -H 'Content-Type: application/json' \
  -d '{}' "$panel/api/verbs/rollback") == 403 ]] ||
  fail "panel accepted a POST without X-Berth-Panel"
pc main -f -d '{"image":true}' "$panel/api/verbs/update" >/dev/null || fail "panel job start"
for ((i = 0; i < 300; i++)); do
  state=$(pc main -f "$panel/api/jobs" | jq -r '.jobs[0].state')
  [[ $state != running ]] && break
  sleep 1
done
[[ $state == succeeded ]] || fail "panel update --image job ended $state"

step "config change restarts only what it affects"
xa main tmux new-session -d -s keep
ttyd_pid=$(service_pid main ttyd)
panel_pid=$(service_pid main panel)
x main berth config set frontends.ide.enabled=true --json | jq -e '.changes.started == ["code_server"]' >/dev/null ||
  fail "enabling the IDE"
wait_ok main 120
[[ $(service_pid main ttyd) == "$ttyd_pid" ]] || fail "ttyd restarted for an IDE change"
[[ $(service_pid main panel) == "$panel_pid" ]] || fail "panel restarted for an IDE change"
xa main tmux has-session -t keep || fail "tmux session lost"
[[ $(ps_user 'code-server') == agent ]] || fail "code-server not agent"
listens_on_loopback_only main 8080 || fail "code-server listens beyond loopback"
ide=$(code_of main "$panel/ide/")
[[ $ide == 200 || $ide == 302 ]] || fail "IDE through the proxy answered $ide"
if [[ $ide == 302 ]]; then
  loc=$(x main curl -s -o /dev/null -w '%{redirect_url}' -b "$jar" "$panel/ide/")
  [[ $loc == "$panel/ide/"* ]] || fail "IDE redirects outside /ide/: $loc"
fi
x main berth config set runtime.tz=Europe/Rome --json | jq -e '.changes.restarted == ["ttyd","code_server"]' >/dev/null ||
  fail "a time zone change must restart the frontends only"
[[ $(service_pid main panel) == "$panel_pid" ]] || fail "panel restarted for a TZ change"
xa main tmux has-session -t keep || fail "tmux session lost across a ttyd restart"

step "no credential on any command line or service environment"
if x main sh -c 'cat /proc/[0-9]*/cmdline 2>/dev/null | tr "\0" " "' | grep -q 'smoke smoke smoke'; then
  fail "the panel password appears on a command line"
fi
for pid in $(x main sh -c 'curl -fsS --unix-socket /run/berth/broker.sock http://b/status' | jq -r '.services[].pid'); do
  if x main sh -c "tr '\0' '\n' </proc/$pid/environ" | grep -qE 'PASSWORD|CREDENTIAL|GITHUB_TOKEN'; then
    fail "service $pid has a credential in its environment"
  fi
done

step "reset-login returns to setup"
x main berth config reset-login --json | jq -e '.setup_url | startswith("http://127.0.0.1:7690/setup#token=")' >/dev/null ||
  fail "reset-login"
sleep 2 # the panel caches the broker's mode for a second
[[ $(code_of main "$panel/api/status") == 409 ]] || fail "old session still works after reset-login"
token=$(setup_token main)
x main rm -f "$jar"
pc main -f -d '{"token":"'"$token"'","user":"smoke","password":"smoke smoke smoke"}' "$panel/api/setup" >/dev/null ||
  fail "setup after reset-login"
wait_ok main 120

step "config survives a restart"
docker restart "$prefix-main" >/dev/null
wait_healthy main "$boot_timeout"
x main berth config get --json | jq -e '.config.frontends.ide.enabled and .config.runtime.tz == "Europe/Rome"' >/dev/null ||
  fail "config lost across restart"

step "corrupt config: recovery mode, then reset"
x main sh -c 'echo "{broken" > /var/lib/berth/config.json'
docker restart "$prefix-main" >/dev/null
for ((i = 0; i < boot_timeout; i++)); do
  x main berth health --json 2>/dev/null | jq -e '.problems[0] | startswith("recovery mode")' >/dev/null && break
  sleep 1
done
[[ $(docker inspect -f '{{.State.Running}}' "$prefix-main") == true ]] || fail "container stopped on a corrupt config"
x main berth config get --json | jq -e '.mode == "recovery"' >/dev/null || fail "not in recovery mode"
x main rm -f "$jar" # a restart signs everyone out
pc main -f -d '{"user":"smoke","password":"smoke smoke smoke"}' "$panel/api/login" >/dev/null || fail "login in recovery"
pc main -f "$panel/api/status" | jq -e '.mode == "recovery"' >/dev/null || fail "panel down in recovery"
x main berth config reset --json >/dev/null || fail "reset"
wait_ok main 120
x main test -f /var/lib/berth/config.corrupt.json || fail "corrupt config not kept"
halt main

step "pgid=100, same volumes"
boot pgid "$boot_timeout" -e PGID=100
[[ $(x pgid stat -c %g /home/agent) == 100 ]] || fail "home group not repaired"
x pgid berth status --json | jq -e '.current != null' >/dev/null || fail "agents lost across recreation"
[[ $(xa pgid id -g) == 100 ]] || fail "agent gid"

step "workspaces owner"
[[ $(x pgid stat -c %u:%g /workspaces/keep) == 4242:4242 ]] || fail "/workspaces/keep ownership changed"
halt pgid

step "berth-panel ids are refused for agent"
panel_uid=$(docker run --rm --entrypoint id "$image" -u berth-panel)
if docker run --rm -e PUID="$panel_uid" "$image" true 2>/dev/null; then
  fail "PUID equal to berth-panel's uid was accepted"
fi

step "no token"
if docker history --no-trunc "$image" | grep -qiE 'github_token=|ghp_|github_pat_'; then
  fail "token in image history"
fi
docker image inspect "$image" | jq -e '[.[0].Config.Env[] | select(startswith("GITHUB_TOKEN"))] | length == 0' >/dev/null ||
  fail "token in image env"

echo
echo "smoke: all checks passed"
