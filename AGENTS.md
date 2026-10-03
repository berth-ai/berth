# AGENTS.md

Rules for anyone, human or agent, changing Berth. Git history is the record
of what is done; do not keep status checklists in the repo.

## Layout

- `cmd/berth`, `internal/`: the `berth` CLI (Go). `internal/cli` wires commands; `internal/agentstore` owns `/opt/agents`.
- `rootfs/`: copied to `/` in the image; paths mirror the container.
- `build/`: build-time helpers, bind-mounted during `docker build`, never shipped.
- `agents/`: the agent lockfile. `tools/mise.toml`: the base toolchain.
- `tests/`: Python `unittest` for shell and build config; `tests/smoke-image.sh` for images.
- `web/`: the panel UI (React, Vite, pnpm), from `SalGnt/template-web`. `pnpm build` fills `web/dist`, which `package web` embeds; `internal/panel/devserver` runs the panel against a fake broker for UI work. `web/DESIGN.md` is the panel's design system; read it before any UI change.

## Checks before every commit

```sh
gofmt -l $(git ls-files '*.go') && go vet ./... && go test -race ./...   # build web/ first, or the dist tests skip
python3 -m unittest discover -s tests -v   # needs bash >= 5.1, jq, zsh
shellcheck -x rootfs/usr/local/bin/* rootfs/usr/local/lib/berth/*.sh rootfs/etc/berth/services.d/*.sh build/*.sh tests/*.sh
hadolint --failure-threshold error Dockerfile
actionlint
```

Panel UI changes, in `web/`:

```sh
pnpm install --frozen-lockfile && pnpm fmt:check && pnpm lint && pnpm test && pnpm build && pnpm e2e
```

Work on the UI with `pnpm dev` (a watch build) and `go run ./internal/panel/devserver -dist web/dist` from the repo root; sign in as `dev` / `dev password`.

Image changes: `docker buildx build --load -t berth:dev . && tests/smoke-image.sh berth:dev`.
CI builds the image on every push but runs the smoke test only on demand: `gh workflow run ci --ref <branch> -f smoke=true`.

## Design rules

- Never recursively change ownership of `/workspaces`.
- Keep build-time caches out of `$HOME`; use `/tmp` or `/opt`.
- Pass build secrets as BuildKit secret files, never `ARG` or `ENV`.
- Pin every version and annotate it for Renovate; never use `latest`.
- Every downloaded artifact has a sha256 `ARG`. When Renovate bumps a version
  `ARG`, update its checksums in the same PR:
  `curl -fsSL <url> | sha256sum`, once per architecture.
- Split apt layers. On a failed install, read the first `E:`/`Err:` line, not the exit code.
- Keep `/usr/sbin` and `/sbin` on `PATH`; keep `passwd` installed.
- Expose mise tools through exec wrappers, never bare symlinks.
- One writer per worktree; use separate worktrees for concurrent agents.
- Never use `docker compose down -v` as an upgrade step.
- For ShellCheck on trap handlers, disable both SC2329 and SC2317.
- The `--json` output of `berth` is a contract: add fields freely, but bump
  `schema` for any removal or rename, and update the golden files in
  `internal/cli/testdata/golden` (`go test ./internal/cli -update`).
- The panel CSP never changes. UI code must not inject `<style>` elements or use `dangerouslySetInnerHTML`; every React root sits in Base UI's `CSPProvider disableStyleElements`. The Playwright smoke test fails on any violation.
- Pin `web/` dependencies exactly; add shadcn and ReUI components only with `pnpm dlx shadcn@<pinned version> add …`, never `@latest`.
- Environment variables such as `BERTH_SERVICES_DIR` and `BERTH_ETC` exist
  only as test seams; do not document them for users.
- Never commit hostnames, credentials, personal paths or other private data,
  including in fixtures and examples.
