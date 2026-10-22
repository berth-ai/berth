"""Static checks over the repository's pins and build configuration."""

import json
import re
import tomllib
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
EXACT = re.compile(r"^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$")


def read(rel):
    return (REPO / rel).read_text()


class AgentsLockTest(unittest.TestCase):
    def test_every_dependency_has_berth_metadata(self):
        pkg = json.loads(read("agents/package.json"))
        meta = pkg["berth"]
        self.assertEqual(sorted(meta), sorted(pkg["dependencies"]))
        seen = set()
        for name, entry in meta.items():
            self.assertEqual(sorted(entry), ["keys", "kind", "name"], name)
            self.assertIn(entry["kind"], ("agent", "mcp"), name)
            self.assertTrue(entry["name"], name)
            for key in entry["keys"]:
                self.assertRegex(key, r"^[A-Z][A-Z0-9_]{0,63}$", name)
                self.assertNotIn(key, seen)
                seen.add(key)

    def test_exact_versions_only(self):
        deps = json.loads(read("agents/package.json"))["dependencies"]
        self.assertEqual(sorted(deps), sorted([
            "@anthropic-ai/claude-code", "@google/gemini-cli", "@openai/codex", "opencode-ai",
            "@playwright/mcp", "@upstash/context7-mcp"]))
        for name, version in deps.items():
            self.assertRegex(version, EXACT, name)

    def test_descriptors_cover_agents_and_servers(self):
        pkg = json.loads(read("agents/package.json"))
        block = pkg["berthAgents"]
        self.assertEqual(block["schema"], 1)
        kinds = {name: entry["kind"] for name, entry in pkg["berth"].items()}
        self.assertEqual(sorted(block["agents"]), sorted(n for n, k in kinds.items() if k == "agent"))
        self.assertEqual(sorted(block["servers"]), sorted(n for n, k in kinds.items() if k == "mcp"))

    def test_gemini_self_update_is_off(self):
        settings = json.loads(read("rootfs/etc/gemini-cli/settings.json"))
        self.assertIs(settings["general"]["enableAutoUpdate"], False)
        self.assertIs(settings["general"]["enableAutoUpdateNotification"], False)

    def test_lock_matches_package_json(self):
        pkg = json.loads(read("agents/package.json"))
        lock = json.loads(read("agents/package-lock.json"))
        self.assertTrue(pkg.get("private"))
        self.assertEqual(lock["lockfileVersion"], 3)
        self.assertEqual(lock["packages"][""]["dependencies"], pkg["dependencies"])

    def test_single_playwright_core(self):
        lock = json.loads(read("agents/package-lock.json"))
        versions = {meta["version"] for path, meta in lock["packages"].items()
                    if path.endswith("node_modules/playwright-core")}
        self.assertEqual(len(versions), 1, versions)

    def test_both_linux_platforms_locked(self):
        paths = json.loads(read("agents/package-lock.json"))["packages"]
        for suffix in ("linux-x64", "linux-arm64"):
            self.assertTrue(any(p.endswith(f"claude-code-{suffix}") for p in paths), suffix)
            self.assertTrue(any(p.endswith(f"codex-{suffix}") for p in paths), suffix)


class MiseToolsTest(unittest.TestCase):
    def test_every_tool_pinned_exactly(self):
        tools = tomllib.loads(read("tools/mise.toml"))["tools"]
        self.assertGreaterEqual(len(tools), 25)
        for name in ("node", "neovim", "gh", "glab", "uv", "ripgrep", "starship", "chezmoi",
                     "atuin", "zoxide", "direnv", "fzf", "shellcheck", "hadolint"):
            self.assertIn(name, tools)
        for name, version in tools.items():
            self.assertIsInstance(version, str, name)
            self.assertRegex(version, r"^\d+(\.\d+)+$", name)


class RegistryLockTest(unittest.TestCase):
    def entries(self):
        return sorted(p for p in (REPO / "agents" / "registry").iterdir() if p.is_dir())

    def test_entries_are_locked_single_agents(self):
        entries = self.entries()
        self.assertGreaterEqual(len(entries), 1)
        for d in entries:
            with self.subTest(entry=d.name):
                self.assertRegex(d.name, r"^[a-z0-9-]{1,32}$")
                pkg = json.loads((d / "package.json").read_text())
                lock = json.loads((d / "package-lock.json").read_text())
                self.assertTrue(pkg.get("private"))
                self.assertEqual(lock["lockfileVersion"], 3)
                self.assertEqual(lock["packages"][""]["dependencies"], pkg["dependencies"])
                (name, version), = pkg["dependencies"].items()
                self.assertRegex(version, EXACT)
                self.assertEqual(list(pkg["berth"]), [name])
                meta = pkg["berth"][name]
                self.assertEqual(sorted(meta), ["description", "keys", "kind", "name"])
                self.assertEqual(meta["kind"], "agent")
                self.assertLessEqual(len(meta["description"]), 120)
                block = pkg["berthAgents"]
                self.assertEqual(block["schema"], 1)
                self.assertEqual(list(block["agents"]), [name])
                self.assertNotIn("servers", block)

    def test_copilot_locks_both_linux_binaries(self):
        paths = json.loads(read("agents/registry/copilot/package-lock.json"))["packages"]
        for suffix in ("linux-x64", "linux-arm64"):
            self.assertIn(f"node_modules/@github/copilot-{suffix}", paths)

    def test_registry_is_shipped(self):
        self.assertIn("COPY agents/registry/ /usr/share/berth/registry/", read("Dockerfile"))


# package-lock.json is left out: dependency names such as "latest-version"
# are legitimate there, and exact pins are enforced by AgentsLockTest.
PINNED_FILES = ["Dockerfile", "compose.yaml", ".env.example", "tools/mise.toml", "agents/package.json", "agents/registry/copilot/package.json"]


class DockerfileTest(unittest.TestCase):
    def setUp(self):
        self.lines = read("Dockerfile").splitlines()

    def test_no_latest(self):
        for rel in PINNED_FILES:
            self.assertNotRegex(read(rel), r"\blatest\b", rel)

    def test_base_images_pinned_by_digest(self):
        images = [l for l in self.lines if re.match(r"ARG \w+_IMAGE=", l)]
        self.assertEqual(len(images), 3)
        for line in images:
            self.assertRegex(line, r"@sha256:[0-9a-f]{64}$")
        for line in self.lines:
            if line.startswith("FROM "):
                self.assertRegex(line, r"^FROM (--platform=\$BUILDPLATFORM )?\$\{\w+_IMAGE\} AS [\w-]+$")

    def test_web_build_feeds_go_build(self):
        text = "\n".join(self.lines)
        self.assertIn("FROM --platform=$BUILDPLATFORM ${NODE_IMAGE} AS web-build", text)
        self.assertIn("RUN pnpm install --frozen-lockfile", text)
        go_stage = text.split("AS go-build", 1)[1].split("FROM ", 1)[0]
        self.assertIn("COPY --from=web-build /src/web/dist web/dist", go_stage)
        self.assertLess(go_stage.index("COPY --from=web-build"), go_stage.index("go build"))

    def test_version_args_have_renovate_annotations(self):
        for i, line in enumerate(self.lines):
            if re.match(r"ARG \w+_VERSION=", line) and not line.startswith("ARG BERTH_"):
                self.assertRegex(self.lines[i - 1], r"^# renovate: datasource=\S+ depName=\S+", line)

    def test_every_download_has_a_checksum(self):
        text = read("Dockerfile")
        for name in ("MISE", "TTYD", "CODE_SERVER", "TEA"):
            for arch in ("AMD64", "ARM64"):
                self.assertRegex(text, rf"ARG {name}_SHA256_{arch}=[0-9a-f]{{64}}\n")
        for name in ("TMUX_RESURRECT", "TMUX_CONTINUUM"):
            self.assertRegex(text, rf"ARG {name}_SHA256=[0-9a-f]{{64}}\n")
        self.assertNotRegex(text, r"\bcurl\s+-", "download only through fetch-artifact")

    def test_every_copy_source_is_in_the_build_context(self):
        # .dockerignore is an allowlist: a COPY source it does not re-include
        # fails only at image build time, with "not found".
        allowed = [l[1:] for l in read(".dockerignore").splitlines() if l.startswith("!")]
        for line in self.lines:
            if not line.startswith("COPY ") or "--from=" in line:
                continue
            for src in line.split()[1:-1]:
                self.assertTrue(any(src == a or (a.endswith("/") and src.startswith(a)) for a in allowed),
                                f"{src} is excluded by .dockerignore")

    def test_token_only_as_secret(self):
        text = read("Dockerfile")
        self.assertNotRegex(text, r"(ARG|ENV)\s+GITHUB_TOKEN")
        self.assertIn("--mount=type=secret,id=github_token", text)

    def test_runtime_contract(self):
        text = read("Dockerfile")
        self.assertIn('ENTRYPOINT ["tini", "--", "berth-entrypoint"]', text)
        self.assertIn('CMD ["serve"]', text)
        self.assertIn("DISABLE_AUTOUPDATER=1", text)
        self.assertIn("PATH=/var/lib/berth/bin:/opt/mise/wrappers:", text)
        self.assertIn("/usr/sbin:/usr/bin:/sbin:/bin", text)
        self.assertNotIn("VOLUME", text)
        self.assertIn("passwd", text)
        self.assertIn("EXPOSE 7690\n", text)
        self.assertIn("useradd --system --gid berth-panel", text)
        self.assertIn("install -d -m 0755 /var/lib/berth", text)


class ComposeTest(unittest.TestCase):
    def setUp(self):
        self.text = read("compose.yaml")

    def test_no_required_variables(self):
        self.assertNotIn(":?", self.text)
        self.assertNotIn("env_file", self.text)

    def test_image_pin_is_annotated(self):
        self.assertRegex(self.text, r"# renovate: datasource=docker depName=ghcr.io/berth-ai/berth\n\s+image: "
                                    r"ghcr\.io/berth-ai/berth:\$\{BERTH_VERSION:-\d+\.\d+\.\d+\}\n")

    def test_only_the_panel_port(self):
        ports = re.findall(r'^\s+- "(.*:7690)"$', self.text, re.M)
        self.assertEqual(ports, ["${BIND_ADDRESS:-127.0.0.1}:${PANEL_PORT:-7690}:7690"])
        self.assertEqual(len(re.findall(r"^\s+- \"", self.text, re.M)), 1)

    def test_host_values_passed_through(self):
        for var, src in (("BIND_ADDRESS", "BIND_ADDRESS:-127.0.0.1"), ("PANEL_PORT", "PANEL_PORT:-7690"),
                         ("PUID", "PUID:-1000"), ("PGID", "PGID:-1000"), ("WORKSPACES_DIR", "WORKSPACES_DIR:-./workspaces")):
            self.assertIn(f"BERTH_HOST_{var}: ${{{src}}}", self.text)
        self.assertIn("PUID: ${PUID:-1000}", self.text)

    def test_volumes_and_hardening(self):
        self.assertIn("no-new-privileges:true", self.text)
        for vol in ("berth-home", "berth-agents", "berth-config"):
            self.assertIn(f"name: {vol}", self.text)
        self.assertNotIn("0.0.0.0", self.text)

    def test_env_example_is_optional(self):
        env = read(".env.example")
        self.assertNotRegex(env, r"(?m)^[A-Z_]+=", "every override is commented out")
        for var in ("BERTH_VERSION", "BIND_ADDRESS", "PANEL_PORT", "PUID", "PGID", "WORKSPACES_DIR", "GITHUB_TOKEN"):
            self.assertRegex(env, rf"(?m)^# {var}=", var)
        for gone in ("PANEL_CREDENTIAL", "WEB_TERMINAL_CREDENTIAL", "CODE_SERVER_PASSWORD"):
            self.assertNotIn(gone, env)


WORKFLOWS = REPO / ".github/workflows"


def workflow(name):
    return (WORKFLOWS / name).read_text()


class WorkflowTest(unittest.TestCase):
    def all_workflows(self):
        return {p.name: p.read_text() for p in sorted(WORKFLOWS.glob("*.yml"))}

    def test_actions_pinned_by_sha(self):
        for name, text in self.all_workflows().items():
            for line in text.splitlines():
                if "uses:" in line:
                    self.assertRegex(line, r"uses: [\w.-]+/[\w./-]+@[0-9a-f]{40} # v\d", f"{name}: {line.strip()}")

    def test_no_pull_request_target(self):
        for name, text in self.all_workflows().items():
            self.assertNotIn("pull_request_target", text, name)

    def test_ci_permissions_read_only(self):
        text = workflow("ci.yml")
        self.assertIn("permissions:\n  contents: read", text)
        self.assertNotIn("write", text)

    def test_required_check_names(self):
        text = workflow("ci.yml")
        for job in ("lint:", "go-test:", "shell-test:"):
            self.assertIn(f"\n  {job}", text)
        self.assertIn("name: image-${{ matrix.arch }}", text)
        self.assertIn("- arch: amd64\n            runner: ubuntu-24.04\n", text)

    def test_image_smoke_runs_only_on_demand(self):
        text = workflow("ci.yml")
        self.assertIn("  workflow_dispatch:\n    inputs:\n      smoke:\n", text)
        self.assertIn("      - name: Smoke test\n        if: inputs.smoke\n", text)

    def test_no_moving_image_tags(self):
        for name, text in self.all_workflows().items():
            self.assertNotRegex(text, r"image: \S+:latest", name)


class ReleaseTest(unittest.TestCase):
    def setUp(self):
        self.text = workflow("release.yml")

    def test_release_tag_filter(self):
        self.assertIn("tags: ['v*.*.*']", self.text)
        # Prerelease tags such as v1.2.0-rc.1 also match the glob; they must not move latest.
        self.assertIn("type=raw,value=latest,enable=${{ startsWith(github.ref, 'refs/tags/v') && "
                      "!contains(github.ref_name, '-') }}", self.text)

    def test_tags_and_attestations(self):
        for tag in ("type=edge,branch=main", "type=sha,format=short,prefix=sha-",
                    "type=semver,pattern={{version}}", "type=semver,pattern={{major}}.{{minor}}"):
            self.assertIn(tag, self.text)
        self.assertIn("provenance: mode=max", self.text)
        self.assertIn("sbom: true", self.text)
        self.assertIn("push-by-digest=true", self.text)
        self.assertIn("- arch: amd64\n            runner: ubuntu-24.04\n", self.text)

    def test_write_permissions_scoped_to_jobs(self):
        top = self.text.split("\njobs:")[0]
        self.assertIn("permissions:\n  contents: read", top)
        self.assertNotIn("packages: write", top)


class RenovateTest(unittest.TestCase):
    def setUp(self):
        self.cfg = json.loads(read("renovate.json"))
        self.rules = self.cfg["packageRules"]

    def test_agents_group_covers_registry(self):
        entry = self.effective("npm", "minor", "agents/registry/copilot/package.json")
        self.assertEqual(entry["groupName"], "agent CLIs")
        self.assertEqual(entry["rangeStrategy"], "pin")

    def effective(self, manager, update_type, file_name):
        """Apply packageRules in order (later wins), as Renovate does."""
        result = {}
        for rule in self.rules:
            if "matchManagers" in rule and manager not in rule["matchManagers"]:
                continue
            if "matchUpdateTypes" in rule and update_type not in rule["matchUpdateTypes"]:
                continue
            if "matchFileNames" in rule and not any(
                    re.fullmatch(p.replace("**", ".*"), file_name) for p in rule["matchFileNames"]):
                continue
            result.update({k: v for k, v in rule.items() if not k.startswith("match") and k != "description"})
        return result

    def test_renovate_automerge_matrix(self):
        cases = [
            ("npm", "major", "agents/package.json", True),
            ("npm", "patch", "agents/package.json", True),
            ("mise", "minor", "tools/mise.toml", True),
            ("mise", "major", "tools/mise.toml", False),
            ("github-actions", "digest", ".github/workflows/ci.yml", True),
            ("github-actions", "major", ".github/workflows/ci.yml", False),
            ("gomod", "patch", "go.mod", True),
            ("dockerfile", "digest", "Dockerfile", False),
            ("custom.regex", "patch", "Dockerfile", False),
            ("custom.regex", "minor", "compose.yaml", False),
        ]
        for manager, update, path, automerge in cases:
            with self.subTest(manager=manager, update=update):
                self.assertEqual(self.effective(manager, update, path).get("automerge", False), automerge)

    def test_compose_pin_manager(self):
        m = next(m for m in self.cfg["customManagers"] if "compose" in m["managerFilePatterns"][0])
        pattern = re.compile(m["matchStrings"][0].replace("(?<", "(?P<"))
        found = pattern.search(read("compose.yaml"))
        self.assertIsNotNone(found)
        self.assertEqual(found.group("depName"), "ghcr.io/berth-ai/berth")

    def test_agents_group(self):
        agents = self.effective("npm", "minor", "agents/package.json")
        self.assertEqual(agents["groupName"], "agent CLIs")
        self.assertEqual(agents["schedule"], ["* */6 * * *"])
        self.assertEqual(agents["rangeStrategy"], "pin")
        self.assertTrue(self.cfg["lockFileMaintenance"]["enabled"])

    def test_regex_managers_cover_annotations(self):
        managers = self.cfg["customManagers"]
        dockerfile = next(m for m in managers if "Dockerfile" in m["managerFilePatterns"][0])
        pattern = re.compile(dockerfile["matchStrings"][0].replace("(?<", "(?P<"))
        found = {m.group("depName") for m in pattern.finditer(read("Dockerfile"))}
        self.assertEqual(found, {"jdx/mise", "tsl0922/ttyd", "coder/code-server",
                                 "tmux-plugins/tmux-resurrect", "tmux-plugins/tmux-continuum", "gitea/tea"})



class CacheFriendlyBuildArgsTest(unittest.TestCase):
    def test_per_commit_args_come_after_expensive_layers(self):
        text = read("Dockerfile")
        go_stage, rest = text.split("FROM ${UBUNTU_IMAGE} AS tools", 1)
        runtime = rest.split("FROM ${UBUNTU_IMAGE} AS runtime", 1)[1]
        self.assertLess(go_stage.index("RUN go mod download"), go_stage.index("ARG BERTH_COMMIT"))
        self.assertLess(runtime.index("COPY rootfs/ /"), runtime.index("ARG BERTH_COMMIT"))
        self.assertLess(runtime.index("COPY rootfs/ /"), runtime.index("ARG BERTH_VERSION"))


class RenovateFixturesTest(unittest.TestCase):
    def test_testdata_is_ignored(self):
        rules = json.loads(read("renovate.json"))["packageRules"]
        self.assertTrue(any(r.get("matchFileNames") == ["**/testdata/**"] and r.get("enabled") is False
                            for r in rules))
        self.assertIs(rules[-1].get("enabled"), False, "the disabling rule must come last")


class PresetsTest(unittest.TestCase):
    PRESETS = sorted((REPO / "tools/presets").glob("*.toml"))

    def test_initial_presets(self):
        self.assertEqual([p.stem for p in self.PRESETS], ["go", "java", "python", "rust"])

    def test_presets_are_exact_and_described(self):
        for path in self.PRESETS:
            data = tomllib.loads(path.read_text())
            self.assertEqual(sorted(data), ["preset", "tools"], path.name)
            self.assertTrue(data["preset"]["name"] and data["preset"]["description"], path.name)
            self.assertTrue(data["tools"], path.name)
            for tool, version in data["tools"].items():
                self.assertIsInstance(version, str, f"{path.name}: {tool}")
                self.assertRegex(version, r"^(?:[a-z]+-)?\d+\.\d+\.\d+(?:[+.][0-9A-Za-z.]+)?$", f"{path.name}: {tool}")

    def test_renovate_mise_manager_covers_presets(self):
        cfg = json.loads(read("renovate.json"))
        patterns = cfg["mise"]["managerFilePatterns"]
        self.assertTrue(any(re.search(p.strip("/"), "tools/presets/go.toml") for p in patterns), patterns)
        self.assertTrue(any(re.search(p.strip("/"), "tools/mise.toml") for p in patterns), patterns)

    def test_image_ships_pins(self):
        text = read("Dockerfile")
        self.assertIn("COPY tools/mise.toml /usr/share/berth/mise.toml", text)
        self.assertIn("COPY tools/presets/ /usr/share/berth/presets/", text)


if __name__ == "__main__":
    unittest.main()
