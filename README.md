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

## Agents

```sh
docker compose exec berth berth status          # what is installed, and from where
docker compose exec berth berth status --check  # is a newer agent set on main?
docker compose exec berth berth update          # install the agent set from main, no restart
docker compose exec berth berth rollback        # back to the previous set, offline
docker compose exec berth berth update --image  # return to the set shipped with the image
```

Updates install into a new directory and switch atomically. Running agent
sessions keep working on the version they started with. After `update` or
`rollback`, restarts keep your choice. Only `update --image` makes Berth
follow the image again. Every command accepts `--json`.

The web panel does the same from a browser: it shows the current and previous
agent sets, checks the configured ref (`main` by default) for a newer lock,
and runs `update` (from that ref, another git ref or the image) or
`rollback` while showing the log. It cannot run anything else.

Each agent and MCP server can be switched off on its own, and agents take
their API keys from the panel. A key set in your own shell wins over the
panel's. From the command line:

```sh
docker compose exec berth berth config set 'agents.disabled=["opencode-ai"]'
docker compose exec berth berth config set agents.ref=v1.4.0   # where updates come from
```

Switching a package off removes it from `PATH` for new shells and
unregisters its MCP server. The installed files stay, so switching it back
on is instant.

Berth registers its MCP servers in each agent's own configuration
(`~/.claude.json`, `~/.gemini/settings.json`, OpenCode's `opencode.json`,
and a marked block in `~/.codex/config.toml`) and only changes the entries
it wrote: servers you add yourself stay as they are.

Skills come from any git repository over HTTPS (private ones through your
`gh` or `glab` sign-in) or from a short catalog on the panel's Skills page.
Berth pins each one to a commit, keeps one copy in
`~/.local/share/berth/skills`, and links it into the skills directory of
every agent you pick (`~/.claude/skills`, `~/.codex/skills`,
`~/.gemini/skills`; OpenCode reads Claude Code's). A skill moves to a newer
commit only when you click Update. Skills are instructions and scripts your
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

## Upgrading the image

```sh
# fetch the new compose.yaml (or set BERTH_VERSION in .env), then:
docker compose pull && docker compose up -d
```

Your home (agent logins, `gh`/`glab` auth, dotfiles, your own tools) and the
agent store and Berth's settings live in the `berth-home`, `berth-agents` and
`berth-config` volumes and survive recreation. Never run
`docker compose down -v` to upgrade: it deletes all three volumes.

## Tools and shell

- The pinned toolchain lives in `/opt/mise`: Node, Neovim, gh, glab, uv,
  ripgrep, fd, fzf, lazygit, starship, chezmoi, atuin and more.
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
| `GITHUB_TOKEN` | unset | Raises GitHub's rate limit for agent update checks |

The panel shows these values but cannot change them.

Every other setting is changed with `berth config` and kept in the
`berth-config` volume:

| Setting | Default | Meaning |
| --- | --- | --- |
| `frontends.terminal.enabled` | `true` | Run the browser terminal |
| `frontends.ide.enabled` | `false` | Run the browser IDE |
| `agents.ref` | `main` | Git ref `berth update` installs from by default |
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
- The agent store and base toolchain are read-only to `agent`, and agent
  auto-updaters are disabled.
- Playwright's Chromium runs with `--no-sandbox`, because the kernel sandbox
  is unavailable under `no-new-privileges`. It is used only for agent-driven
  browsing.

## License

To be decided.
