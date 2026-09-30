# AGENTS.md

Rules for anyone, human or agent, changing Berth. Git history is the record
of what is done; do not keep status checklists in the repo.

## Layout

- `cmd/berth`, `internal/`: the `berth` CLI (Go). `internal/cli` wires commands; `internal/agentstore` owns `/opt/agents`.
- `rootfs/`: copied to `/` in the image; paths mirror the container.
- `build/`: build-time helpers, bind-mounted during `docker build`, never shipped.
- `agents/`: the agent lockfile. `tools/mise.toml`: the base toolchain.
- `tests/`: Python `unittest` for shell and build config; `tests/smoke-image.sh` for images.

## Checks before every commit

```sh
gofmt -l . && go vet ./... && go test -race ./...
python3 -m unittest discover -s tests -v   # needs bash >= 5.1, jq, zsh
shellcheck -x rootfs/usr/local/bin/* rootfs/usr/local/lib/berth/*.sh rootfs/etc/berth/services.d/*.sh build/*.sh tests/*.sh
hadolint --failure-threshold error Dockerfile
actionlint
```

Image changes: `docker buildx build --load -t berth:dev . && tests/smoke-image.sh berth:dev`.

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
- Environment variables such as `BERTH_SERVICES_DIR` and `BERTH_ETC` exist
  only as test seams; do not document them for users.
- Never commit hostnames, credentials, personal paths or other private data,
  including in fixtures and examples.
