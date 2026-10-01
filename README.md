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
$EDITOR .env        # set BIND_ADDRESS and at least one frontend credential
mkdir -p workspaces # before the first start, so it is yours and not root's
docker compose up -d
docker compose ps   # wait for "healthy"
```

The first start installs the agent CLIs into the `berth-agents` volume, which
takes a few minutes. Later starts are fast.

## Frontends

| Frontend | Enable with | Port |
| --- | --- | --- |
| Browser terminal (ttyd + tmux) | `WEB_TERMINAL_CREDENTIAL=user:password` | `TTYD_PORT` (7681) |
| Browser IDE (code-server) | `CODE_SERVER_PASSWORD=...` | `CODE_SERVER_PORT` (8080) |
| Web panel (agent status, update, rollback) | `PANEL_CREDENTIAL=user:password` | `PANEL_PORT` (7690) |
| Shell | always | `docker compose exec -u agent berth zsh` |

With no frontend enabled, Berth idles and `docker exec` still works. Ports are
published only on `BIND_ADDRESS`; always use a private address.

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
agent store live in named volumes and survive recreation. Never run
`docker compose down -v` to upgrade: it deletes both volumes.

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

| Variable | Default | Meaning |
| --- | --- | --- |
| `BERTH_VERSION` | required | Image tag to run |
| `BIND_ADDRESS` | required | Host address the ports are published on |
| `PUID` / `PGID` | 1000 / 1000 | Identity of the `agent` user |
| `WORKSPACES_DIR` | `./workspaces` | Host path mounted at `/workspaces` |
| `WEB_TERMINAL_CREDENTIAL` | unset | `user:password`; enables ttyd |
| `TTYD_PORT` | 7681 | ttyd port |
| `CODE_SERVER_PASSWORD` | unset | Enables code-server |
| `CODE_SERVER_PORT` | 8080 | code-server port |
| `PANEL_CREDENTIAL` | unset | `user:password`; enables the web panel |
| `PANEL_PORT` | 7690 | Web panel port |
| `AGENTS_BOOT_TIMEOUT` | 600 | Seconds allowed for installing agents at boot |
| `AGENTS_KEEP` | 3 | Old agent sets kept besides current and previous |
| `MCP_SETUP_TIMEOUT` | 120 | Seconds allowed for MCP registration |
| `TZ` | `UTC` | Time zone |

## Security

- All services and agents run as the unprivileged `agent` user, with `no-new-privileges`.
- Each frontend's credential is hidden from the other frontends.
- The web panel runs as `agent`. Agent updates need root, so the panel asks
  a small root broker that listens only on a unix socket, has no network
  port and no credential, and runs only `berth status`, `update` and
  `rollback`.
- The agent store and base toolchain are read-only to `agent`, and agent
  auto-updaters are disabled.
- Playwright's Chromium runs with `--no-sandbox`, because the kernel sandbox
  is unavailable under `no-new-privileges`. It is used only for agent-driven
  browsing.

## License

To be decided.
