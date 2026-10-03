# Berth

Berth is a self-hosted agent workstation in a single container. It bundles:
- the Claude Code, Codex and OpenCode CLIs,
- the Playwright and Context7 MCP servers,
- a pinned developer toolchain,
- a persistent home and your `/workspaces`.

You reach it over a private network through a browser terminal, a browser IDE or `docker exec`.

The agents are the product; the frontends are optional modules.

## Quick start

```sh
curl -fsSLO https://raw.githubusercontent.com/SalGnt/berth/main/compose.yaml
curl -fsSL -o .env https://raw.githubusercontent.com/SalGnt/berth/main/.env.example
$EDITOR .env        # set BIND_ADDRESS and PANEL_CREDENTIAL
mkdir -p workspaces # before the first start, so it is yours and not root's
docker compose up -d
docker compose ps   # wait for "healthy"
```

The first start installs the agent CLIs into the `berth-agents` volume, which
takes a few minutes. Later starts are fast.

## Frontends and settings

The web panel is always on and is the only published port (`PANEL_PORT`,
7690). The browser terminal (ttyd + tmux) is on by default, and the browser
IDE (code-server) is off. Both listen only inside the container; the panel's
proxy for them arrives in the next release. Until then, use
`docker compose exec -u agent berth zsh`.

Settings live in the `berth-config` volume and change without restarting the
container. Only the services a change affects restart:

```sh
docker compose exec berth berth config get
docker compose exec berth berth config set frontends.ide.enabled=true runtime.tz=Europe/Rome
docker compose exec berth berth config revert   # back to the previous revision
```

If the config file becomes unreadable, Berth starts only the panel and
`berth config reset` (or `revert`) recovers.

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
agent sets, checks `main` for a newer lock, and runs `update` (from `main`, a
git ref or the image) or `rollback` while showing the log. It cannot run
anything else.

## Upgrading the image

```sh
# edit BERTH_VERSION in .env, then:
docker compose pull && docker compose up -d
```

Your home (agent logins, `gh`/`glab` auth, dotfiles, your own tools) and the
agent store and Berth's settings live in the `berth-home`, `berth-agents` and
`berth-config` volumes and survive recreation. Never run
`docker compose down -v` to upgrade: it deletes all three volumes.

## Tools and shell

- The pinned toolchain lives in `/opt/mise`: Node, Neovim, gh, glab, uv,
  ripgrep, fd, fzf, lazygit, starship, chezmoi, atuin and more.
- Add your own tools with `mise use -g <tool>@<version>`. They live in your
  home and never shadow the pinned ones.
- Berth ships opinionated defaults for zsh, Starship, tmux, Neovim, atuin,
  zoxide, fzf and direnv. Creating your own file replaces the default for that
  tool: `~/.zshrc`, `~/.config/starship.toml`, `~/.tmux.conf`,
  `~/.config/nvim/init.lua`.

## Configuration

The `.env` file holds only what Docker needs on the host:

| Variable | Default | Meaning |
| --- | --- | --- |
| `BERTH_VERSION` | required | Image tag to run |
| `BIND_ADDRESS` | required | Host address the panel port is published on |
| `PANEL_PORT` | 7690 | Host port mapped to the panel |
| `PUID` / `PGID` | 1000 / 1000 | Identity of the `agent` user |
| `WORKSPACES_DIR` | `./workspaces` | Host path mounted at `/workspaces` |
| `PANEL_CREDENTIAL` | unset | `user:password` for the panel login (temporary) |

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
