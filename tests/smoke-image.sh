#!/usr/bin/env bash
# smoke-image.sh IMAGE: boot IMAGE in every configuration CI requires and
# check agents, persistence, identity and hygiene. Needs docker and jq.
set -euo pipefail

image=${1:?usage: smoke-image.sh IMAGE}
prefix=berth-smoke-$$
home_vol=$prefix-home
agents_vol=$prefix-agents
ws=$(mktemp -d)
first_boot_timeout=${SMOKE_FIRST_BOOT_TIMEOUT:-600}
boot_timeout=${SMOKE_BOOT_TIMEOUT:-200}

cleanup() {
  docker ps -aq --filter "name=^$prefix-" | xargs -r docker rm -f >/dev/null 2>&1 || true
  docker volume rm -f "$home_vol" "$agents_vol" >/dev/null 2>&1 || true
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
  local name=$1 timeout=$2 status
  shift 2
  docker run -d --name "$prefix-$name" --security-opt no-new-privileges:true \
    -v "$home_vol:/home/agent" -v "$agents_vol:/opt/agents" -v "$ws:/workspaces" \
    "$@" "$image" >/dev/null
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

halt() { docker rm -f "$prefix-$1" >/dev/null; }
x() { docker exec "$prefix-$1" "${@:2}"; }
xa() { docker exec -u agent "$prefix-$1" "${@:2}"; }

step "workspaces owner fixture"
docker run --rm -v "$ws:/workspaces" --entrypoint sh "$image" \
  -c 'touch /workspaces/keep && chown 4242:4242 /workspaces/keep'
[[ $(docker run --rm -v "$ws:/workspaces" --entrypoint stat "$image" -c %u:%g /workspaces/keep) == 4242:4242 ]] ||
  fail "this host's bind mounts do not keep file ownership (Docker Desktop on macOS?); run the smoke test on Linux"

step "first boot, ttyd mode, default ids (installs the seed)"
boot ttyd "$first_boot_timeout" -e WEB_TERMINAL_CREDENTIAL=smoke:smoke
x ttyd berth status --json | jq -e '.current.source.origin == "image" and .follow_image' >/dev/null ||
  fail "status does not report an image-origin current"
for agent in claude codex opencode; do
  xa ttyd "$agent" --version >/dev/null || fail "$agent --version"
done
for ((i = 0; i < 120; i++)); do
  xa ttyd claude mcp list 2>/dev/null | grep -q playwright && break
  sleep 1
done
xa ttyd claude mcp list | grep -q context7 || fail "MCP servers not registered with claude"

step "update is idempotent; rollback works"
x ttyd sh -c 'mkdir -p /tmp/variant && cd /usr/share/berth/agents &&
  jq ".name = \"smoke-variant\"" package.json >/tmp/variant/package.json &&
  jq ".name = \"smoke-variant\" | .packages[\"\"].name = \"smoke-variant\"" package-lock.json >/tmp/variant/package-lock.json'
x ttyd berth update --lock-dir /tmp/variant --json | jq -e '.changed and .installed' >/dev/null || fail "variant update"
x ttyd berth update --lock-dir /tmp/variant --json | jq -e '(.changed | not) and (.installed | not)' >/dev/null ||
  fail "second update is not a no-op"
seed=$(x ttyd jq -r .seed_hash /usr/share/berth/build-info.json)
x ttyd berth rollback --json | jq -e --arg seed "$seed" '.current.hash == $seed' >/dev/null || fail "rollback"
x ttyd berth update --image --json | jq -e '.follow_image' >/dev/null || fail "update --image"

step "shell defaults"
# shellcheck disable=SC2016  # expanded by zsh inside the container
[[ $(xa ttyd zsh -ic 'print -r -- "$EDITOR|$HISTFILE"' </dev/null 2>/dev/null) == "nvim|/home/agent/.local/state/zsh/history" ]] ||
  fail "zsh newuser prompt or missing defaults"
[[ $(xa ttyd nvim --headless '+lua io.write(tostring(vim.o.number))' +qa 2>&1) == true ]] || fail "nvim defaults"
[[ $(xa ttyd tmux -L smoke new-session -d \; show-options -gv history-limit \; kill-server) == 50000 ]] ||
  fail "tmux defaults"
# shellcheck disable=SC2016  # expanded by zsh inside the container
[[ $(xa ttyd zsh -c 'print -l $path | head -n 2 | paste -sd,') == /opt/agents/current/bin,/opt/mise/wrappers ]] ||
  fail "PATH order"
halt ttyd

step "pgid=100 with every frontend, same volumes"
boot pgid "$boot_timeout" -e PGID=100 -e WEB_TERMINAL_CREDENTIAL=smoke:smoke -e CODE_SERVER_PASSWORD=smoke \
  -e PANEL_CREDENTIAL=smoke:smoke
[[ $(x pgid stat -c %g /home/agent) == 100 ]] || fail "home group not repaired"
x pgid berth status --json | jq -e '.current != null' >/dev/null || fail "agents lost across recreation"
[[ $(xa pgid id -g) == 100 ]] || fail "agent gid"

step "web panel"
panel=http://127.0.0.1:7690
[[ $(x pgid stat -c '%a %U %g' /run/berth/broker.sock) == "660 root 100" ]] || fail "broker socket ownership"
[[ $(x pgid ps -eo user:12,args | awk '$2 ~ /berth$/ && $3 == "broker" {print $1}') == root ]] || fail "broker not root"
[[ $(x pgid ps -eo user:12,args | awk '$2 ~ /berth$/ && $3 == "panel" {print $1}') == agent ]] || fail "panel not agent"
[[ $(x pgid curl -s -o /dev/null -w '%{http_code}' "$panel/") == 401 ]] || fail "panel without credentials"
x pgid curl -fsS -u smoke:smoke "$panel/api/status" | jq -e '.current != null' >/dev/null || fail "panel status"
[[ $(x pgid curl -s -o /dev/null -w '%{http_code}' -u smoke:smoke -H 'Content-Type: application/json' \
  -d '{"action":"rollback"}' "$panel/api/jobs") == 403 ]] || fail "panel accepted a POST without X-Berth-Panel"
x pgid curl -fsS -u smoke:smoke -H 'Content-Type: application/json' -H 'X-Berth-Panel: 1' \
  -d '{"action":"update","image":true}' "$panel/api/jobs" >/dev/null || fail "panel job start"
for ((i = 0; i < 300; i++)); do
  state=$(x pgid curl -fsS -u smoke:smoke "$panel/api/job" | jq -r .job.state)
  [[ $state != running ]] && break
  sleep 1
done
x pgid curl -fsS -u smoke:smoke "$panel/api/job" | jq -e '.job.state == "succeeded" and .job.result.follow_image' >/dev/null ||
  fail "panel update --image job: $(x pgid curl -fsS -u smoke:smoke "$panel/api/job")"
halt pgid

step "idle mode"
boot idle "$boot_timeout"
x idle berth status --json | jq -e '.current != null' >/dev/null || fail "idle status"

step "workspaces owner"
[[ $(x idle stat -c %u:%g /workspaces/keep) == 4242:4242 ]] || fail "/workspaces/keep ownership changed"
halt idle

step "no token"
if docker history --no-trunc "$image" | grep -qiE 'github_token=|ghp_|github_pat_'; then
  fail "token in image history"
fi
docker image inspect "$image" | jq -e '[.[0].Config.Env[] | select(startswith("GITHUB_TOKEN"))] | length == 0' >/dev/null ||
  fail "token in image env"

echo
echo "smoke: all checks passed"
