# Berth

Berth is a self-hosted agent workstation in a single container. It bundles:
- the Claude Code, Codex, Gemini CLI and OpenCode CLIs,
- the Playwright and Context7 MCP servers,
- a pinned developer toolchain,
- a persistent home and your `/workspaces`.

You reach it over a private network through a browser terminal, a browser IDE or `docker exec`.

The agents are the product; the frontends are optional modules.

## Quick start

```sh
curl -fsSLO https://raw.githubusercontent.com/berth-ai/berth/main/compose.yaml
mkdir -p workspaces   # before the first start, so it is yours and not root's
docker compose up -d
docker compose logs berth | grep 'setup: open'
```

Open the printed link to create the panel login and pick the frontends. The
link works once. A restart before you finish prints a new one. The first
start also installs the agent CLIs into the `berth-agents` volume, which takes
a few minutes. Later starts are fast.

## Frontends and settings

The panel at `http://127.0.0.1:7690` is where Berth is set up and run:
health, terminal, IDE, agents, tools, accounts, settings and job logs. It is
the only published port and the only login. The terminal and the IDE open inside it, at `/terminal/` and `/ide/`,
and neither has a password of its own. The browser terminal (ttyd + tmux) is
on by default, and the browser IDE (code-server) is off.
`docker compose exec -u agent berth zsh` always works too.

Settings live in the `berth-config` volume and change without restarting the
container. Only the services a change affects restart:

```sh
docker compose exec berth berth config get
docker compose exec berth berth config set frontends.ide.enabled=true runtime.tz=Europe/Rome
docker compose exec berth berth config revert   # back to the previous revision
```

If the config file becomes unreadable, Berth starts only the panel and
`berth config reset` (or `revert`) recovers.

Forgot the panel login? `docker compose exec berth berth config reset-login`
deletes it and prints a new setup link.

## Updates

The image pins a version of every agent CLI, MCP server and base tool. Berth
starts from those, and you can move any of them to a newer version without
waiting for a new image. Versions come straight from upstream: the npm
registry for agents and MCP servers, mise for base tools.

```sh
docker compose exec berth berth status --check        # what runs, what the image pins, what is newest
docker compose exec berth berth update                # every minor and patch update; majors wait
docker compose exec berth berth update @openai/codex  # one entity to its newest version, major or not
docker compose exec berth berth update gh@2.104.0     # or to a version you pick
docker compose exec berth berth update --image gh     # back to the image's version (all, without a name)
docker compose exec berth berth rollback              # the previous agent set, offline
docker compose exec berth berth rollback --tools      # the previous base tools
```

Updates install into a new directory and switch atomically. Running agent
sessions keep working on the version they started with. Your picks survive
restarts. When you pull a newer image, a pick stays only while it is newer
than what that image ships; once the image catches up, it follows the image
again. Every command accepts `--json`.

The panel's Updates page does the same from a browser: it lists every pinned
entity with the image's version, the running one and the newest upstream,
and offers Update all, per-entity updates, a version picker, rollback and a
return to the image. A banner announces minor and patch updates; majors are
picked one by one.

npm records each package's integrity and `npm ci` checks it. Base tools are
installed by mise as the `agent` user, then sealed read-only and owned by
root; mise verifies checksums where the tool's backend publishes them. A
version the image did not ship has not been tested with Berth, so roll back
if one misbehaves.

## Agents

Each agent and MCP server can be switched off on its own, and agents take
their API keys from the panel. A key set in your own shell wins over the
panel's. From the command line:

```sh
docker compose exec berth berth config set 'agents.disabled=["opencode-ai"]'
```

Switching a package off removes it from `PATH` for new shells and
unregisters its MCP server. The installed files stay, so switching it back
on is instant.

Berth registers its MCP servers in each agent's own configuration
(`~/.claude.json`, `~/.gemini/settings.json`, OpenCode's `opencode.json`,
and a marked block in `~/.codex/config.toml`) and only changes the entries
it wrote: servers you add yourself stay as they are. A server reaches only
the agents and workspaces you pick for it on the MCP page; none by default.

Skills come from any git repository over HTTPS (private ones through your
`gh`, `glab` or `tea` sign-in) or from a short catalog on the panel's Skills page.
Berth pins each one to a commit, keeps one copy in
`~/.local/share/berth/skills`, and links it into the skills directory of
every agent you pick (`~/.claude/skills`, `~/.codex/skills`,
`~/.gemini/skills`; OpenCode reads Claude Code's). A skill moves to a newer
commit only when you ask: Update on one source, or Check for updates and then
Update all on the Skills page. Skills are instructions and scripts your
agents run, so add only sources you trust. Berth never touches a skill
directory you created yourself.

Agents can also use your Claude, ChatGPT, Google or GitHub Copilot
subscription instead of an API key. Sign each one in from the Agents page:
it shows a code to enter on the vendor's site, or a link whose page gives
you a code to paste back. The credentials stay in the agent's own files in
your home volume, and backups do not include them. From the command line:

```sh
docker compose exec berth berth login status                  # who is signed in where
docker compose exec berth berth login logout @openai/codex chatgpt
```

An API key set on the Agents page or in your shell still wins where the
agent prefers keys (Claude Code does).

Each agent's card also shows the tokens it used today and over the last 7
days, read from the agent's own session files; Berth reads only counts,
models and times. Claude Code is tracked today; other agents show "Not
tracked". From the command line:

```sh
docker compose exec berth berth usage                         # tokens per agent, last 7 days
```

## Upgrading the image

```sh
# fetch the new compose.yaml (or set BERTH_VERSION in .env), then:
docker compose pull && docker compose up -d
```

Your home (agent logins, `gh`/`glab`/`tea` auth, dotfiles, your own tools), the
agent store with the versions you picked, and Berth's settings live in the `berth-home`, `berth-agents` and
`berth-config` volumes and survive recreation. Never run
`docker compose down -v` to upgrade: it deletes all three volumes.

## Tools and shell

- The pinned toolchain lives in `/opt/mise`: Node, Neovim, gh, glab, uv,
  ripgrep, fd, fzf, lazygit, starship, chezmoi, atuin and more. Newer
  versions you pick (see Updates) live in `/opt/agents/tools` and come
  first on `PATH`. The Gitea and Forgejo CLI, `tea`, is in `/usr/local/bin`.
- The panel's Accounts page signs `gh` and `glab` in with a one-time code
  and `tea` in with an access token, to any number of Gitea or Forgejo
  servers. Each one also signs git in over HTTPS.
- Add your own tools from the panel's Tools page, or with `mise use -g
  <tool>@<version>`. They live in your home, after the pinned ones on
  `PATH`. Presets install pinned Go, Rust, Python or Java toolchains the
  same way.
- Sign in to GitHub and GitLab from the Accounts page: it shows a one-time
  code to enter on the forge's website. Tokens stay in your home volume.
- Berth ships opinionated defaults for zsh, Starship, tmux, Neovim, atuin,
  zoxide, fzf and direnv. Creating your own file replaces the default for that
  tool: `~/.zshrc`, `~/.config/starship.toml`, `~/.tmux.conf`,
  `~/.config/nvim/init.lua`.

## Configuration

No `.env` is needed. These optional host-side variables override the
defaults in `compose.yaml`; `.env.example` lists them:

| Variable | Default | Meaning |
| --- | --- | --- |
| `BERTH_VERSION` | the release `compose.yaml` pins | Image tag to run |
| `BIND_ADDRESS` | `127.0.0.1` | Host address the panel port is published on |
| `PANEL_PORT` | 7690 | Host port mapped to the panel |
| `PUID` / `PGID` | 1000 / 1000 | Identity of the `agent` user |
| `WORKSPACES_DIR` | `./workspaces` | Host path mounted at `/workspaces` |

The panel shows these values but cannot change them.

Every other setting is changed with `berth config` and kept in the
`berth-config` volume:

| Setting | Default | Meaning |
| --- | --- | --- |
| `frontends.terminal.enabled` | `true` | Run the browser terminal |
| `frontends.ide.enabled` | `false` | Run the browser IDE |
| `agents.keep` | 3 | Old agent sets kept besides current and previous |
| `agents.boot_timeout` | 600 | Seconds allowed for installing agents at boot |
| `agents.mcp_timeout` | 120 | Seconds allowed for MCP registration |
| `runtime.tz` | `UTC` | Time zone |

## Security

- One published port, bound to `BIND_ADDRESS` (default `127.0.0.1`), and one login.
- ttyd and code-server listen only on loopback inside the container.
- No credential appears on any command line or in any service's environment.
- Berth does not terminate TLS: put a reverse proxy or a VPN in front for
  remote access. Behind a TLS proxy the session cookie is marked Secure.
- The frontends and agents run as the unprivileged `agent` user, with `no-new-privileges`.
- Services get only the settings they need; tokens passed to the container stay with the supervisor.
- The panel runs as its own `berth-panel` user. Privileged work goes through
  a root broker inside `berth supervise`, on a unix socket only `berth-panel`
  can reach. The broker runs a fixed set of verbs.
- The agent store, the base toolchain and the base tools picked over the
  image's are read-only to `agent`, and agent auto-updaters are disabled.
- Playwright's Chromium runs with `--no-sandbox`, because the kernel sandbox
  is unavailable under `no-new-privileges`. It is used only for agent-driven
  browsing.

## License

To be decided.
