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
  docker run --rm -v "$ws:/w" --entrypoint rm "$image" -rf /w/keep /w/octocat >/dev/null 2>&1 || true
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

# listens_on_any NAME PORT: some socket on PORT is bound to every address
# (0.0.0.0 or ::), so a published port reaches it.
listens_on_any() {
  local hex
  hex=$(printf '%04X' "$2")
  # shellcheck disable=SC2016  # an awk program, expanded by awk
  x "$1" awk -v suffix=":$hex" '$4 == "0A" && ($2 == "00000000" suffix || $2 == "00000000000000000000000000000000" suffix) {found = 1} END {exit !found}' \
    /proc/net/tcp /proc/net/tcp6
}

# orca_daemons NAME prints the pids of agent's Orca processes outside the
# orca service's process group: the terminal daemon and its helpers.
orca_daemons() {
  local svc
  svc=$(service_pid "$1" orca)
  x "$1" ps -eo pid=,pgid=,user=,args= | awk -v svc="$svc" '$3 == "agent" && $2 != svc && /Orca|orca-ide|orcad/ {print $1}'
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
# The fixed host name mirrors compose.yaml, which keeps Orca's lock recognisable.
boot main "$first_boot_timeout" --hostname berth
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
for agent in claude codex gemini opencode; do
  xa main "$agent" --version >/dev/null || fail "$agent --version"
done
for ((i = 0; i < 120; i++)); do
  xa main claude mcp list 2>/dev/null | grep -q playwright && break
  sleep 1
done
xa main claude mcp list | grep -q context7 || fail "MCP servers not registered with claude"
xa main jq -e '.mcpServers.playwright and .mcpServers.context7' /home/agent/.gemini/settings.json >/dev/null ||
  fail "MCP servers not registered with gemini"
xa main grep -qx '\[mcp_servers.playwright\]' /home/agent/.codex/config.toml ||
  fail "MCP servers not registered with codex"
xa main jq -e '.mcp.playwright.enabled' /home/agent/.config/opencode/opencode.json >/dev/null ||
  fail "MCP servers not registered with opencode"
x main jq -e '.general.enableAutoUpdate == false' /etc/gemini-cli/settings.json >/dev/null ||
  fail "gemini self-update is not off"
# A server the user adds by hand must survive later syncs (checked below).
xa main sh -c 'jq ".mcpServers.mine = {\"command\": \"true\"}" ~/.gemini/settings.json > /tmp/g && mv /tmp/g ~/.gemini/settings.json' ||
  fail "adding a user MCP server to gemini"

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
[[ $(xa main zsh -c 'print -l $path | head -n 2 | paste -sd,') == /var/lib/berth/bin,/opt/mise/wrappers ]] ||
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
# Read the whole page first: grep -q would close the pipe early, and the
# SIGPIPE fails the pipeline under pipefail.
term_page=$(x main curl -fsS -b "$jar" "$panel/terminal/") || fail "terminal through the proxy"
grep -qi ttyd <<<"$term_page" || fail "terminal through the proxy"
ws_code=$(x main curl -s -o /dev/null -w '%{http_code}' --http1.1 --max-time 3 -b "$jar" \
  -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' \
  -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' -H 'Sec-WebSocket-Protocol: tty' \
  -H "Origin: $panel" "$panel/terminal/ws" || true)
[[ $ws_code == 101 ]] || fail "terminal WebSocket through the proxy answered $ws_code"
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

# verb NAME VERB JSON calls a broker verb as root over the socket.
verb() {
  x "$1" curl -fsS --unix-socket /run/berth/broker.sock -H 'Content-Type: application/json' -d "$3" "http://b/verbs/$2"
}
rev() { x "$1" berth config get --json | jq -r .config.revision; }

step "agent toggles and API keys"
x main test -L /var/lib/berth/bin || fail "the agent layer is not a symlink"
for agent in claude codex gemini opencode; do
  xa main "$agent" --version >/dev/null || fail "$agent --version through the layer"
done
verb main config.apply '{"base_revision":'"$(rev main)"',"patch":{"agents":{"disabled":["opencode-ai","@playwright/mcp"]}}}' >/dev/null ||
  fail "disabling opencode and Playwright"
if xa main sh -lc 'command -v opencode' >/dev/null 2>&1; then fail "opencode still on PATH"; fi
xa main claude --version >/dev/null || fail "claude broken by disabling opencode"
for ((i = 0; i < 120; i++)); do
  ! xa main claude mcp list 2>/dev/null | grep -q playwright && break
  sleep 1
done
xa main claude mcp list | grep -q playwright && fail "disabled Playwright still registered with claude"
xa main claude mcp list | grep -q context7 || fail "context7 lost"
xa main jq -e '.mcpServers | has("playwright") | not' /home/agent/.gemini/settings.json >/dev/null ||
  fail "disabled Playwright still registered with gemini"
if xa main grep -q 'mcp_servers.playwright' /home/agent/.codex/config.toml; then
  fail "disabled Playwright still registered with codex"
fi
xa main jq -e '.mcpServers.mine and .mcpServers.context7' /home/agent/.gemini/settings.json >/dev/null ||
  fail "a user MCP server or context7 was lost from gemini"
verb main config.apply '{"secrets":{"ANTHROPIC_API_KEY":"sk-smoke-test"}}' >/dev/null || fail "setting a key"
verb main config.get '{}' | jq -e '.secrets.ANTHROPIC_API_KEY.set' >/dev/null || fail "key not reported as set"
if verb main config.get '{}' | grep -q sk-smoke-test; then fail "config.get leaked the key"; fi
[[ $(x main stat -c '%a %U %G' /var/lib/berth/secrets/agents.env) == "640 root agent" ]] || fail "agents.env ownership"
# shellcheck disable=SC2016  # expanded by the inner sh
xa main sh -c 'sh -x /var/lib/berth/bin/claude --version 2>&1 | grep -q "export ANTHROPIC_API_KEY=sk-smoke-test"' ||
  fail "the claude wrapper does not load its key"
# shellcheck disable=SC2016
if xa main sh -c 'sh -x /var/lib/berth/bin/codex --version 2>&1 | grep -q ANTHROPIC'; then
  fail "codex's wrapper loads another package's key"
fi
if docker exec -u berth-panel "$prefix-main" cat /var/lib/berth/secrets/agents.env 2>/dev/null; then
  fail "berth-panel can read agent keys"
fi
verb main config.apply '{"base_revision":'"$(rev main)"',"patch":{"agents":{"disabled":[]}}}' >/dev/null
xa main opencode --version >/dev/null || fail "opencode back after re-enabling"

step "a remote MCP server reaches every agent, narrows and goes away"
verb main config.apply '{"base_revision":'"$(rev main)"',"patch":{"mcp":{"servers":{"remote-demo":{"url":"https://mcp.example.com/mcp","auth":"bearer"}}}},"secrets":{"MCP_REMOTE_DEMO_TOKEN":"smoke-token"}}' >/dev/null ||
  fail "adding a remote MCP server"
for ((i = 0; i < 60; i++)); do
  xa main jq -e '.mcpServers["remote-demo"]' /home/agent/.gemini/settings.json >/dev/null 2>&1 && break
  sleep 1
done
xa main jq -e '.mcpServers["remote-demo"].headers.Authorization == "Bearer smoke-token"' /home/agent/.claude.json >/dev/null ||
  fail "remote server not registered with claude"
xa main jq -e '.mcpServers["remote-demo"].type == "http"' /home/agent/.gemini/settings.json >/dev/null ||
  fail "remote server not registered with gemini"
xa main jq -e '.mcp["remote-demo"].type == "remote"' /home/agent/.config/opencode/opencode.json >/dev/null ||
  fail "remote server not registered with opencode"
xa main grep -q 'url = "https://mcp.example.com/mcp"' /home/agent/.codex/config.toml ||
  fail "remote server not registered with codex"
xa main codex mcp list | grep -q remote-demo || fail "codex cannot read the remote server"
[[ $(x main stat -c '%a %U %G' /var/lib/berth/mcp.json) == "640 root agent" ]] || fail "mcp.json ownership"
if x main grep -q smoke-token /var/lib/berth/mcp.json; then fail "mcp.json holds the token"; fi

verb main config.apply '{"base_revision":'"$(rev main)"',"patch":{"mcp":{"available":{"remote-demo":["@anthropic-ai/claude-code"]}}}}' >/dev/null ||
  fail "narrowing a remote MCP server"
for ((i = 0; i < 60; i++)); do
  xa main jq -e '.mcpServers | has("remote-demo") | not' /home/agent/.gemini/settings.json >/dev/null 2>&1 && break
  sleep 1
done
xa main jq -e '.mcpServers | has("remote-demo") | not' /home/agent/.gemini/settings.json >/dev/null ||
  fail "narrowed server still registered with gemini"
xa main jq -e '.mcpServers["remote-demo"]' /home/agent/.claude.json >/dev/null ||
  fail "narrowed server lost from claude"

verb main config.apply '{"base_revision":'"$(rev main)"',"patch":{"mcp":{"servers":{"remote-demo":null},"available":{"remote-demo":null}}}}' >/dev/null ||
  fail "removing a remote MCP server"
for ((i = 0; i < 60; i++)); do
  xa main jq -e '.mcpServers | has("remote-demo") | not' /home/agent/.claude.json >/dev/null 2>&1 && break
  sleep 1
done
xa main jq -e '.mcpServers | has("remote-demo") | not' /home/agent/.claude.json >/dev/null ||
  fail "removed server still registered with claude"
if x main grep -q MCP_REMOTE_DEMO_TOKEN /var/lib/berth/secrets/agents.env; then
  fail "removed server's token kept"
fi
verb main update.check '{}' | jq -e '.check.ref == "main" and (.check.update_available != null or .check.error != null)' >/dev/null ||
  fail "update.check"
x main berth status --json | jq -e '.versions | length >= 1' >/dev/null || fail "status lacks versions"

step "a skill source is pinned, attached to each agent, previewed, narrowed, detached and removed"
pin=8a1541c4a3ffa5a20a5a91de0dcf3f0bab1d1ef4
anth=https://github.com/anthropics/skills
src=/home/agent/.local/share/berth/skills/anthropics-skills
# wait_job ID waits up to two minutes for a broker job and prints it as JSON.
wait_job() {
  local job
  for ((i = 0; i < 120; i++)); do
    job=$(x main curl -fsS --unix-socket /run/berth/broker.sock "http://b/jobs/$1" | jq -c .job)
    [[ $(jq -r .state <<<"$job") != running ]] && break
    sleep 1
  done
  printf '%s\n' "$job"
}
found=$(wait_job "$(verb main skills.sources.preview '{"repo":"'"$anth"'","ref":"'"$pin"'","path":"skills"}' | jq -r .job.id)")
jq -e --arg c "$pin" '.state == "succeeded" and .result.commit == $c and .result.id == "anthropics-skills" and .result.exists == "" and
  (.result.found | has("pdf") and has("docx") and has("xlsx"))' <<<"$found" >/dev/null ||
  fail "skills.sources.preview: $found"
added=$(wait_job "$(verb main skills.sources.add '{"repo":"'"$anth"'","ref":"'"$pin"'","path":"skills","commit":"'"$pin"'"}' | jq -r .job.id)")
jq -e '.state == "succeeded" and .result.id == "anthropics-skills" and .result.attached == []' <<<"$added" >/dev/null ||
  fail "skills.sources.add: $added"
verb main config.apply '{"base_revision":'"$(rev main)"',"patch":{"skills":{"sources":{"anthropics-skills":{"attached":{"pdf":{},"docx":{},"xlsx":{}}}}}}}' >/dev/null ||
  fail "attaching pdf, docx and xlsx"
verb main config.get '{}' | jq -e --arg c "$pin" '.config.skills.sources["anthropics-skills"] | .commit == $c and (.attached | length == 3)' >/dev/null ||
  fail "the source is not pinned"
[[ $(x main stat -c '%a %U %G' /var/lib/berth/skills.json) == "640 root agent" ]] || fail "skills.json ownership"
x main jq -e '.schema == 2' /var/lib/berth/skills.json >/dev/null || fail "skills.json is not schema 2"
for ((i = 0; i < 120; i++)); do
  xa main test -L /home/agent/.gemini/skills/xlsx && break
  sleep 1
done
for s in pdf docx xlsx; do
  xa main test -f "$src/$s/SKILL.md" || fail "$s export missing"
  for d in .claude/skills .codex/skills .gemini/skills; do
    [[ $(xa main readlink "/home/agent/$d/$s") == "$src/$s" ]] || fail "$s not linked in ~/$d"
  done
  # OpenCode reads ~/.claude/skills, so it gets no link of its own.
  if xa main test -e "/home/agent/.config/opencode/skills/$s"; then fail "$s linked twice for opencode"; fi
done
xa main gemini skills list 2>/dev/null | grep -q pdf || fail "gemini does not load the linked skill"

same=$(wait_job "$(verb main skills.sources.preview '{"id":"anthropics-skills"}' | jq -r .job.id)")
jq -e '.state == "succeeded" and .result.changed == false and .result.removed == []' <<<"$same" >/dev/null ||
  fail "skills.sources.preview of the pinned source: $same"

verb main config.apply '{"base_revision":'"$(rev main)"',"patch":{"skills":{"sources":{"anthropics-skills":{"attached":{"pdf":{"agents":["@anthropic-ai/claude-code"]}}}}}}}' >/dev/null ||
  fail "narrowing pdf"
for ((i = 0; i < 60; i++)); do
  ! xa main test -e /home/agent/.gemini/skills/pdf && break
  sleep 1
done
if xa main test -e /home/agent/.gemini/skills/pdf; then fail "narrowed skill still linked for gemini"; fi
if xa main test -e /home/agent/.codex/skills/pdf; then fail "narrowed skill still linked for codex"; fi
xa main test -L /home/agent/.claude/skills/pdf || fail "narrowing removed claude's link"

verb main config.apply '{"base_revision":'"$(rev main)"',"patch":{"skills":{"sources":{"anthropics-skills":{"attached":{"docx":null,"xlsx":null}}}}}}' >/dev/null ||
  fail "detaching docx and xlsx"
for ((i = 0; i < 60; i++)); do
  ! xa main test -e "$src/docx" && break
  sleep 1
done
for s in docx xlsx; do
  if xa main test -e "$src/$s"; then fail "detached $s's export kept"; fi
  if xa main test -e "/home/agent/.claude/skills/$s"; then fail "detached $s still linked for claude"; fi
done
xa main test -f "$src/pdf/SKILL.md" || fail "detaching others removed pdf"

verb main skills.sources.remove '{"id":"anthropics-skills"}' | jq -e '.id == "anthropics-skills"' >/dev/null ||
  fail "removing the source"
for ((i = 0; i < 60; i++)); do
  ! xa main test -e "$src" && break
  sleep 1
done
if xa main test -e "$src"; then fail "the removed source's folder was kept"; fi
if xa main test -e /home/agent/.claude/skills/pdf; then fail "removed pdf still linked for claude"; fi

step "a workspace is cloned, launched, forgotten, adopted again and deleted"
hello=https://github.com/octocat/Hello-World
hello_pin=7fd1a60b01f91b314f59955a4e4d4e80d8edf11d
# The test's /workspaces is a private temporary folder; let agent create projects in it.
x main chmod 0777 /workspaces
added=$(wait_job "$(verb main workspaces.add '{"mode":"clone","repo":"'"$hello"'","ref":"'"$hello_pin"'","path":"octocat/Hello-World","id":"hello"}' | jq -r .job.id)")
jq -e '.state == "succeeded" and .result.id == "hello" and .result.writable' <<<"$added" >/dev/null ||
  fail "workspaces.add clone: $added"
[[ $(x main stat -c '%a %U %G' /var/lib/berth/workspaces.json) == "640 root agent" ]] || fail "workspaces.json ownership"
x main jq -e '.workspaces.hello.path == "/workspaces/octocat/Hello-World"' /var/lib/berth/workspaces.json >/dev/null ||
  fail "workspaces.json: $(x main cat /var/lib/berth/workspaces.json)"
[[ $(x main stat -c %U /workspaces/octocat/Hello-World) == agent ]] || fail "the clone is not agent's"
if x main sh -c 'ls -A /workspaces/octocat | grep -q "^\.berth-tmp-"'; then fail "the clone left its temporary folder"; fi
status=$(verb main workspaces.status '{}')
jq -e --arg h "${hello_pin:0:12}" '.workspaces.hello | .exists and .git and .writable and .head == $h and .branch == "" and .dirty == 0' \
  <<<"$status" >/dev/null || fail "workspaces.status: $status"
verb main workspaces.launch '{"id":"hello"}' | jq -e '.session == "ws-hello"' >/dev/null || fail "shell launch"
[[ $(xa main tmux display-message -p -t ws-hello '#{pane_current_path}') == /workspaces/octocat/Hello-World ]] ||
  fail "the session does not start in the workspace"
verb main workspaces.launch '{"id":"hello","agent":"@anthropic-ai/claude-code"}' | jq -e '.window == "claude"' >/dev/null ||
  fail "agent launch"
xa main tmux list-windows -t ws-hello -F '#{window_name}' | grep -qx claude || fail "no claude window in ws-hello"
if xa main berth-term ws-nope 2>/dev/null; then fail "berth-term attached an unknown workspace"; fi
forgot=$(wait_job "$(verb main workspaces.forget '{"id":"hello"}' | jq -r .job.id)")
jq -e '.state == "succeeded" and (.result.deleted | not)' <<<"$forgot" >/dev/null || fail "workspaces.forget: $forgot"
if xa main tmux has-session -t =ws-hello 2>/dev/null; then fail "forget left the session running"; fi
x main test -d /workspaces/octocat/Hello-World/.git || fail "forget removed the folder"
verb main config.get '{}' | jq -e '.config.workspaces == {}' >/dev/null || fail "forget kept the workspace"
verb main workspaces.candidates '{}' | jq -e '.candidates | any(.path == "octocat/Hello-World" and .git)' >/dev/null ||
  fail "the forgotten folder is not a candidate"
adopted=$(wait_job "$(verb main workspaces.add '{"mode":"adopt","path":"octocat/Hello-World","id":"hello"}' | jq -r .job.id)")
jq -e '.state == "succeeded" and .result.git and .result.writable' <<<"$adopted" >/dev/null || fail "adopt: $adopted"
deleted=$(wait_job "$(verb main workspaces.forget '{"id":"hello","delete":true,"confirm":"octocat/Hello-World"}' | jq -r .job.id)")
jq -e '.state == "succeeded" and .result.deleted' <<<"$deleted" >/dev/null || fail "delete: $deleted"
if x main test -e /workspaces/octocat/Hello-World; then fail "delete left the folder"; fi
x main test -d /workspaces/octocat || fail "delete removed the parent folder"

step "skills and an MCP server are attached to a workspace, linked and vendored, then released on forget"
hello_dir=/workspaces/octocat/Hello-World
store=/home/agent/.local/share/berth/skills/anthropics-skills
added=$(wait_job "$(verb main workspaces.add '{"mode":"clone","repo":"'"$hello"'","ref":"'"$hello_pin"'","path":"octocat/Hello-World","id":"hello"}' | jq -r .job.id)")
jq -e '.state == "succeeded"' <<<"$added" >/dev/null || fail "cloning hello again: $added"
pin=8a1541c4a3ffa5a20a5a91de0dcf3f0bab1d1ef4
wait_job "$(verb main skills.sources.add '{"repo":"https://github.com/anthropics/skills","ref":"'"$pin"'","path":"skills","commit":"'"$pin"'"}' | jq -r .job.id)" |
  jq -e '.state == "succeeded"' >/dev/null || fail "skills.sources.add for the workspace"
verb main config.apply '{"base_revision":'"$(rev main)"',"patch":{"skills":{"sources":{"anthropics-skills":{"attached":{
  "pdf":{"enabled":true,"agents":[],"workspaces":{"hello":"link"}},
  "docx":{"enabled":true,"agents":[],"workspaces":{"hello":"vendor"}}}}}},
  "mcp":{"workspaces":{"playwright":["hello"]}}}}' >/dev/null || fail "attaching to the workspace"
for ((i = 0; i < 120; i++)); do
  xa main test -f "$hello_dir/.agents/skills/docx/SKILL.md" && xa main test -L "$hello_dir/.claude/skills/pdf" &&
    xa main test -f "$hello_dir/.mcp.json" && break
  sleep 1
done
for d in .claude/skills .agents/skills; do
  [[ $(xa main readlink "$hello_dir/$d/pdf") == "$store/pdf" ]] || fail "pdf not linked in $d"
done
[[ $(xa main readlink "$hello_dir/.claude/skills/docx") == ../../.agents/skills/docx ]] || fail "docx not linked to its copy"
if xa main test -L "$hello_dir/.agents/skills/docx"; then fail "the vendored docx is a link"; fi
exclude=$(xa main cat "$hello_dir/.git/info/exclude")
for line in /.claude/skills/pdf /.agents/skills/pdf /.mcp.json /opencode.json /.codex/config.toml /.gemini/settings.json; do
  grep -qxF "$line" <<<"$exclude" || fail "exclude block lacks $line: $exclude"
done
if grep -q docx <<<"$exclude"; then fail "vendored docx is hidden from git"; fi
while IFS= read -r l; do
  [[ -z $l || $l == '?? .agents/skills/docx/'* || $l == '?? .claude/skills/docx' ]] || fail "unexpected git status line: $l"
done < <(xa main git -C "$hello_dir" status --porcelain --untracked-files=all)
xa main jq -e '.mcpServers.playwright.command == "/var/lib/berth/bin/playwright-mcp"' "$hello_dir/.mcp.json" >/dev/null ||
  fail ".mcp.json: $(xa main cat "$hello_dir/.mcp.json")"
for ((i = 0; i < 60; i++)); do
  xa main grep -qF "[projects.\"$hello_dir\"]" /home/agent/.codex/config.toml && break
  sleep 1
done
xa main grep -qF "[projects.\"$hello_dir\"]" /home/agent/.codex/config.toml || fail "Codex does not trust the workspace"
listed=$(xa main sh -c "cd $hello_dir && gemini skills list" 2>/dev/null || true)
if ! grep -q pdf <<<"$listed" || ! grep -q docx <<<"$listed"; then
  fail "gemini does not see the workspace's skills: $listed"
fi
verb main workspaces.report '{"id":"hello"}' |
  jq -e '.report.skills.entries["anthropics-skills/docx"].mode == "vendor" and .report.mcp.files[".mcp.json"].created and .report.mcp.trust' \
  >/dev/null || fail "workspaces.report: $(verb main workspaces.report '{"id":"hello"}')"

forgot=$(wait_job "$(verb main workspaces.forget '{"id":"hello"}' | jq -r .job.id)")
jq -e '.state == "succeeded" and .result.kept == []' <<<"$forgot" >/dev/null || fail "forget: $forgot"
for p in .claude .agents .mcp.json opencode.json .codex .gemini; do
  if xa main test -e "$hello_dir/$p"; then fail "forget left $p"; fi
done
if xa main grep -qF '# >>> berth' "$hello_dir/.git/info/exclude"; then fail "forget left the exclude block"; fi
[[ -z $(xa main git -C "$hello_dir" status --porcelain --untracked-files=all) ]] || fail "forget left the checkout dirty"
if xa main test -e /home/agent/.local/state/berth/workspaces/hello.json; then fail "forget left the state file"; fi
for ((i = 0; i < 60; i++)); do
  ! xa main grep -qF "[projects.\"$hello_dir\"]" /home/agent/.codex/config.toml && break
  sleep 1
done
if xa main grep -qF "[projects.\"$hello_dir\"]" /home/agent/.codex/config.toml; then fail "forget left Codex's trust"; fi
verb main skills.sources.remove '{"id":"anthropics-skills"}' >/dev/null || fail "removing the source"

step "a workspace's env reaches its windows, instructions are shared, and forget cleans up"
hello_dir=/workspaces/octocat/Hello-World
# The previous step forgot hello and kept its folder: adopt it again.
added=$(wait_job "$(verb main workspaces.add '{"mode":"adopt","path":"octocat/Hello-World","id":"hello"}' | jq -r .job.id)")
jq -e '.state == "succeeded"' <<<"$added" >/dev/null || fail "adopting hello for env: $added"
# shellcheck disable=SC2016 # the $(id) must stay literal: nothing may evaluate it
secret='smoke s3cret $(id) "q"'
verb main config.apply "$(jq -nc --argjson rev "$(rev main)" --arg s "$secret" '{base_revision: $rev,
  patch: {workspaces: {hello: {env: {LOG_LEVEL: {value: "smoke"}, SMOKE_SECRET: {secret: true, value: ""}}}}},
  secrets: {"workspaces.hello.SMOKE_SECRET": $s}}')" >/dev/null || fail "setting workspace env"
[[ $(x main stat -c '%a %U %G' /var/lib/berth/secrets/workspaces/hello.json) == "640 root agent" ]] ||
  fail "secrets file ownership"
if x main grep -q s3cret /var/lib/berth/config.json /var/lib/berth/workspaces.json; then fail "the secret reached a config file"; fi
verb main config.get '{}' | jq -e '.secrets["workspaces.hello.SMOKE_SECRET"].set' >/dev/null || fail "secret not reported as set"
verb main workspaces.launch '{"id":"hello"}' >/dev/null || fail "shell launch with env"
pane=$(xa main tmux list-panes -t =ws-hello -F '#{pane_pid}' | head -1)
for ((i = 0; i < 20; i++)); do
  xa main sh -c "tr '\\0' '\\n' </proc/$pane/environ" | grep -qx 'LOG_LEVEL=smoke' && break
  sleep 0.5
done
environ=$(xa main sh -c "tr '\\0' '\\n' </proc/$pane/environ")
grep -qx 'LOG_LEVEL=smoke' <<<"$environ" || fail "the window lacks LOG_LEVEL"
grep -qxF "SMOKE_SECRET=$secret" <<<"$environ" || fail "the window lacks SMOKE_SECRET"
if x main ps -eo args | grep -q s3cret; then fail "the secret is in a process's argv"; fi
if xa main tmux show-environment -t =ws-hello 2>/dev/null | grep -q s3cret; then fail "the secret is in tmux's environment"; fi
xa main tmux show-options -t =ws-hello default-command | grep -q berth-ws-env || fail "default-command does not use berth-ws-env"
verb main workspaces.restart '{"id":"hello"}' | jq -e '.killed' >/dev/null || fail "workspaces.restart"

agents_md=$(verb main workspaces.instructions.write '{"id":"hello","file":"CLAUDE.md","content":"# from smoke\n","base_sha256":""}')
jq -e '.sha256 | length == 64' <<<"$agents_md" >/dev/null || fail "instructions write: $agents_md"
verb main config.apply '{"base_revision":'"$(rev main)"',"patch":{"workspaces":{"hello":{"instructions":{"shared":true}}}}}' >/dev/null ||
  fail "sharing instructions"
verb main workspaces.instructions.merge '{"id":"hello","file":"CLAUDE.md"}' | jq -e '.merged' >/dev/null || fail "instructions merge"
for ((i = 0; i < 60; i++)); do
  xa main test -L "$hello_dir/GEMINI.md" && break
  sleep 1
done
for f in CLAUDE.md GEMINI.md; do
  [[ $(xa main readlink "$hello_dir/$f") == AGENTS.md ]] || fail "$f does not link to AGENTS.md"
done
[[ $(xa main cat "$hello_dir/AGENTS.md") == "# from smoke" ]] || fail "AGENTS.md: $(xa main cat "$hello_dir/AGENTS.md")"
exclude=$(xa main cat "$hello_dir/.git/info/exclude")
for line in /CLAUDE.md /GEMINI.md; do
  grep -qxF "$line" <<<"$exclude" || fail "links not hidden from git: $exclude"
done
[[ $(xa main git -C "$hello_dir" status --porcelain --untracked-files=all) == '?? AGENTS.md' ]] ||
  fail "git status: $(xa main git -C "$hello_dir" status --porcelain --untracked-files=all)"

forgot=$(wait_job "$(verb main workspaces.forget '{"id":"hello"}' | jq -r .job.id)")
jq -e '.state == "succeeded"' <<<"$forgot" >/dev/null || fail "forget: $forgot"
for f in CLAUDE.md GEMINI.md; do
  if xa main test -e "$hello_dir/$f" || xa main test -L "$hello_dir/$f"; then fail "forget left $f"; fi
done
xa main test -f "$hello_dir/AGENTS.md" || fail "forget removed the user's AGENTS.md"
if x main test -e /var/lib/berth/secrets/workspaces/hello.json; then fail "forget left the secrets file"; fi
deleted=$(wait_job "$(verb main workspaces.add '{"mode":"adopt","path":"octocat/Hello-World","id":"hello"}' | jq -r .job.id)")
jq -e '.state == "succeeded"' <<<"$deleted" >/dev/null || fail "adopting hello to delete it"
deleted=$(wait_job "$(verb main workspaces.forget '{"id":"hello","delete":true,"confirm":"octocat/Hello-World"}' | jq -r .job.id)")
jq -e '.state == "succeeded" and .result.deleted' <<<"$deleted" >/dev/null || fail "deleting hello: $deleted"

step "a registry agent is added, wired, signs in and is removed"
added=$(wait_job "$(verb main agents.add '{"id":"copilot"}' | jq -r .job.id)")
jq -e '.state == "succeeded" and .result.package == "@github/copilot"' <<<"$added" >/dev/null ||
  fail "agents.add: $(jq -c '{state, result}' <<<"$added"); log: $(jq -r .log <<<"$added" | tail -20)"
[[ $(x main stat -c '%a %U %G' /var/lib/berth/agents.json) == "640 root agent" ]] || fail "agents.json ownership"
x main test -d /home/agent/.local/share/berth/agents/copilot/current || fail "no Copilot install"
[[ $(x main stat -c '%U' /home/agent/.local/share/berth/agents/copilot/current/) == agent ]] ||
  fail "the Copilot install is not agent's"
for ((i = 0; i < 120; i++)); do
  xa main test -x /var/lib/berth/bin/copilot && break
  sleep 1
done
xa main copilot --version >/dev/null || fail "copilot --version through the layer"
for ((i = 0; i < 120; i++)); do
  xa main jq -e '.mcpServers.playwright and .mcpServers.context7' /home/agent/.copilot/mcp-config.json >/dev/null 2>&1 && break
  sleep 1
done
xa main jq -e '.mcpServers.playwright and .mcpServers.context7' /home/agent/.copilot/mcp-config.json >/dev/null ||
  fail "Copilot did not get Playwright and Context7"
pin=8a1541c4a3ffa5a20a5a91de0dcf3f0bab1d1ef4
wait_job "$(verb main skills.sources.add '{"repo":"https://github.com/anthropics/skills","ref":"'"$pin"'","path":"skills","commit":"'"$pin"'"}' | jq -r .job.id)" |
  jq -e '.state == "succeeded"' >/dev/null || fail "skills.sources.add for Copilot"
verb main config.apply '{"base_revision":'"$(rev main)"',"patch":{"skills":{"sources":{"anthropics-skills":{"attached":{"pdf":{}}}}}}}' >/dev/null ||
  fail "attaching pdf for Copilot"
for ((i = 0; i < 120; i++)); do
  xa main test -L /home/agent/.copilot/skills/pdf && break
  sleep 1
done
[[ $(xa main readlink /home/agent/.copilot/skills/pdf) == /home/agent/.local/share/berth/skills/anthropics-skills/pdf ]] ||
  fail "pdf not linked for Copilot"
verb main skills.sources.remove '{"id":"anthropics-skills"}' >/dev/null || fail "removing the source for Copilot"
id=$(verb main login.start '{"agent":"@github/copilot","method":"github"}' | jq -r .job.id) || fail "login.start copilot"
for ((i = 0; i < 90; i++)); do
  job=$(x main curl -fsS --unix-socket /run/berth/broker.sock "http://b/jobs/$id")
  jq -e '.job.login.code != ""' <<<"$job" >/dev/null && break
  [[ $(jq -r .job.state <<<"$job") != running ]] && break
  sleep 1
done
jq -e '(.job.login.url | startswith("https://github.com/login/device")) and
  (.job.login.code | test("^[A-Z0-9]{4}-[A-Z0-9]{4}$"))' <<<"$job" >/dev/null ||
  fail "Copilot sign-in: no device code; log: $(jq -r .job.log <<<"$job" | tail -20)"
verb main login.cancel '{"job":'"$id"'}' >/dev/null || fail "Copilot login.cancel"
for ((i = 0; i < 40; i++)); do
  job=$(x main curl -fsS --unix-socket /run/berth/broker.sock "http://b/jobs/$id")
  [[ $(jq -r .job.state <<<"$job") != running ]] && break
  sleep 0.5
done
jq -e '.job.result.error.code == "cancelled"' <<<"$job" >/dev/null || fail "Copilot sign-in did not cancel cleanly"
verb main agents.remove '{"package":"@github/copilot"}' >/dev/null || fail "agents.remove"
# The converger deletes the install, then its job rebuilds the layer.
for ((i = 0; i < 120; i++)); do
  ! x main test -e /home/agent/.local/share/berth/agents/copilot && ! x main test -e /var/lib/berth/bin/copilot && break
  sleep 1
done
if x main test -e /home/agent/.local/share/berth/agents/copilot; then fail "Copilot install left after remove"; fi
if x main test -e /var/lib/berth/bin/copilot; then fail "Copilot wrapper left after remove"; fi

step "tools and accounts (as agent)"
verb main tools.list '{}' | jq -e '(.base | map(.name) | index("node")) and (.presets | length == 4)' >/dev/null ||
  fail "tools.list"
verb main tools.versions '{"tool":"jq"}' | jq -e '.versions | index("1.8.2")' >/dev/null || fail "tools.versions"
verb main tools.add '{"tool":"jq","version":"1.8.2"}' >/dev/null || fail "tools.add"
for ((i = 0; i < 300; i++)); do
  state=$(x main curl -fsS --unix-socket /run/berth/broker.sock http://b/jobs | jq -r '[.jobs[] | select(.verb == "tools.add")][0].state')
  [[ $state != running ]] && break
  sleep 1
done
[[ $state == succeeded ]] || fail "tools.add ended $state"
# The system jq comes first on PATH; ask mise for the user-track one.
xa main zsh -lc 'mise exec jq@1.8.2 -- jq --version' | grep -q 'jq-1.8.2' || fail "user-track jq not installed"
verb main tools.list '{}' | jq -e '.user | map(.name) | index("jq")' >/dev/null || fail "jq not in the user track"
[[ $(x main stat -c %U /home/agent/.config/mise/config.toml) == agent ]] || fail "user mise config not owned by agent"
verb main tools.remove '{"tool":"jq"}' >/dev/null
verb main git.identity '{"name":"Smoke Test","email":"smoke@example.org"}' >/dev/null || fail "git.identity"
[[ $(xa main git config --global user.email) == smoke@example.org ]] || fail "git identity not written as agent"
xa main tea --version >/dev/null || fail "tea --version"
verb main forge.status '{}' | jq -e '.forges.gh.signed_in == false and .forges.tea.signed_in == false and .git.name == "Smoke Test"' >/dev/null ||
  fail "forge.status"

step "agent sign-ins reach their sign-in page and cancel cleanly"
# Run as root, berth login re-runs itself as agent. ANTHROPIC_API_KEY is set
# (above), and must not count as a Claude sign-in.
status=$(x main berth login status --json)
jq -e '[.agents[].methods[]] | length == 5 and all(.signed_in == false and .error == "")' <<<"$status" >/dev/null ||
  fail "login status: $status"
for target in "@anthropic-ai/claude-code claude" "@openai/codex chatgpt" "@google/gemini-cli google" \
  "opencode-ai chatgpt" "opencode-ai copilot"; do
  read -r agent method <<<"$target"
  before=$(x main ps -u agent -o pid= | sort)
  id=$(verb main login.start '{"agent":"'"$agent"'","method":"'"$method"'"}' | jq -r .job.id) ||
    fail "login.start $target"
  for ((i = 0; i < 90; i++)); do
    job=$(x main curl -fsS --unix-socket /run/berth/broker.sock "http://b/jobs/$id")
    jq -e '.job.login.url != "" and (.job.login.code != "" or .job.login.waiting)' <<<"$job" >/dev/null && break
    [[ $(jq -r .job.state <<<"$job") != running ]] && break
    sleep 1
  done
  jq -e '.job.login.url | startswith("https://")' <<<"$job" >/dev/null ||
    fail "$target: no sign-in URL; log: $(jq -r .job.log <<<"$job" | tail -20)"
  case $(jq -r .job.login.flow <<<"$job") in
  device) jq -e '.job.login.code | test("^[A-Z0-9]{4,}-[A-Z0-9]{4,}$")' <<<"$job" >/dev/null ||
    fail "$target: no device code; log: $(jq -r .job.log <<<"$job" | tail -20)" ;;
  paste) jq -e '.job.login.waiting' <<<"$job" >/dev/null ||
    fail "$target: never asked for the code; log: $(jq -r .job.log <<<"$job" | tail -20)" ;;
  *) fail "$target: unknown flow" ;;
  esac
  verb main login.cancel '{"job":'"$id"'}' >/dev/null || fail "$target: login.cancel"
  for ((i = 0; i < 40; i++)); do
    job=$(x main curl -fsS --unix-socket /run/berth/broker.sock "http://b/jobs/$id")
    [[ $(jq -r .job.state <<<"$job") != running ]] && break
    sleep 0.5
  done
  jq -e '.job.state == "failed" and .job.result.error.code == "cancelled"' <<<"$job" >/dev/null ||
    fail "$target: cancel ended $(jq -c '{state: .job.state, result: .job.result}' <<<"$job")"
  left=
  for ((i = 0; i < 20; i++)); do
    left=$(comm -13 <(echo "$before") <(x main ps -u agent -o pid= | sort))
    [[ -z $left ]] && break
    sleep 0.5
  done
  # shellcheck disable=SC2086 # unquoted on purpose: joins the PIDs
  [[ -z $left ]] || fail "$target: processes left after cancel: $(x main ps -o pid,args -p "$(echo $left | tr ' ' ,)")"
done
xa main jq -e '.security.auth.selectedType == "oauth-personal" and .mcpServers.playwright' /home/agent/.gemini/settings.json >/dev/null ||
  fail "gemini's sign-in setting lost or clobbered the MCP entries"

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
  # Follow it: curl 8.5 computes %{redirect_url} for "./?folder=…" without
  # the trailing slash, though it requests the right URL.
  loc=$(x main curl -sL -o /dev/null -w '%{url_effective}' -b "$jar" "$panel/ide/")
  [[ $loc == "$panel/ide/"* ]] || fail "IDE redirects outside /ide/: $loc"
fi
x main berth config set runtime.tz=Europe/Rome --json | jq -e '.changes.restarted == ["ttyd","code_server"]' >/dev/null ||
  fail "a time zone change must restart the frontends only"
[[ $(service_pid main panel) == "$panel_pid" ]] || fail "panel restarted for a TZ change"
xa main tmux has-session -t keep || fail "tmux session lost across a ttyd restart"

step "orca pairs without its link reaching any log"
if ! x main test -x /usr/bin/orca-ide; then
  echo "smoke: this image has no Orca ($(x main uname -m)); skipping the Orca steps"
else
  x main berth config set frontends.orca.enabled=true frontends.orca.pairing_address=100.64.0.10 --json |
    jq -e '.changes.started == ["orca"]' >/dev/null || fail "enabling Orca"
  for ((i = 0; i < 120; i++)); do
    listens_on_any main 6768 && break
    sleep 1
  done
  listens_on_any main 6768 || fail "Orca does not listen on every address on 6768"
  wait_ok main 120
  [[ $(ps_user 'orca-ide|orcad') == agent ]] || fail "Orca does not run as agent"
  orca_pid=$(service_pid main orca)
  x main sh -c "tr '\0' '\n' </proc/$orca_pid/environ" | grep -qx 'ORCA_TELEMETRY_DISABLED=1' ||
    fail "Orca runs with telemetry on"
  x main sh -c "tr '\0' '\n' </proc/$orca_pid/environ" | grep -q '^PATH=/var/lib/berth/bin:' ||
    fail "Orca's PATH lacks the agent CLIs"
  xa main sh -c 'command -v claude && command -v codex' >/dev/null || fail "agent CLIs not on agent's PATH"
  echo "smoke: Orca serves with: $(x main ps -o args= -p "$orca_pid" | cut -c1-120)"
  daemons=$(orca_daemons main)
  echo "smoke: Orca processes outside the service group before pairing: ${daemons:-none}"
  pair=$(x main berth orca pair --json) || fail "berth orca pair"
  url=$(jq -r .url <<<"$pair")
  [[ $url == orca://pair\?* ]] || fail "pairing returned no orca:// link: $pair"
  [[ $(jq -r .address <<<"$pair") == 100.64.0.10 ]] || fail "pairing ignored pairing_address: $pair"
  x main berth orca pair --mobile --json | jq -e '.scope == "mobile" and (.url | startswith("orca://pair?"))' >/dev/null ||
    fail "mobile pairing"
  if docker logs "$prefix-main" 2>&1 | grep -qF "$url"; then fail "a pairing link reached the container log"; fi
  if x main curl -fsS --unix-socket /run/berth/broker.sock http://b/status | grep -qF 'orca://'; then
    fail "a pairing link reached a service log"
  fi
  [[ -z $(x main ls -A /run/berth/orca) ]] || fail "pairing files left in /run/berth/orca"
  [[ $(x main stat -c '%U %a' /run/berth/orca) == "agent 700" ]] || fail "/run/berth/orca is not agent's alone"
  for pid in $daemons; do
    x main kill -0 "$pid" 2>/dev/null || fail "pairing restarted Orca and killed its process $pid"
  done
  wait_ok main 120
fi

step "no credential on any command line or service environment"
if x main sh -c 'cat /proc/[0-9]*/cmdline 2>/dev/null | tr "\0" " "' | grep -q 'smoke smoke smoke'; then
  fail "the panel password appears on a command line"
fi
for pid in $(x main sh -c 'curl -fsS --unix-socket /run/berth/broker.sock http://b/status' | jq -r '.services[].pid'); do
  if x main sh -c "tr '\0' '\n' </proc/$pid/environ" | grep -qE 'PASSWORD|CREDENTIAL|GITHUB_TOKEN'; then
    fail "service $pid has a credential in its environment"
  fi
done

step "orca survives a recreate with the same home"
if x main test -x /usr/bin/orca-ide; then
  halt main
  boot main "$boot_timeout" --hostname berth
  started=$SECONDS
  for ((i = 0; i < 360; i++)); do
    [[ $(x main curl -fsS --unix-socket /run/berth/broker.sock http://b/status |
      jq -r '.services[] | select(.name == "orca") | .state') == running ]] && listens_on_any main 6768 && break
    sleep 1
  done
  listens_on_any main 6768 || {
    x main curl -fsS --unix-socket /run/berth/broker.sock http://b/status | jq '.services[] | select(.name == "orca")' >&2
    fail "Orca did not come back after a recreate"
  }
  echo "smoke: Orca served again $((SECONDS - started))s after the recreate"
  wait_ok main 120
fi

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
