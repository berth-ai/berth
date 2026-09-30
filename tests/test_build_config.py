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
    def test_exact_versions_only(self):
        deps = json.loads(read("agents/package.json"))["dependencies"]
        self.assertEqual(sorted(deps), sorted([
            "@anthropic-ai/claude-code", "@openai/codex", "opencode-ai",
            "@playwright/mcp", "@upstash/context7-mcp"]))
        for name, version in deps.items():
            self.assertRegex(version, EXACT, name)

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


# package-lock.json is left out: dependency names such as "latest-version"
# are legitimate there, and exact pins are enforced by AgentsLockTest.
PINNED_FILES = ["Dockerfile", "compose.yaml", ".env.example", "tools/mise.toml", "agents/package.json"]


class DockerfileTest(unittest.TestCase):
    def setUp(self):
        self.lines = read("Dockerfile").splitlines()

    def test_no_latest(self):
        for rel in PINNED_FILES:
            self.assertNotRegex(read(rel), r"\blatest\b", rel)

    def test_base_images_pinned_by_digest(self):
        images = [l for l in self.lines if re.match(r"ARG \w+_IMAGE=", l)]
        self.assertEqual(len(images), 2)
        for line in images:
            self.assertRegex(line, r"@sha256:[0-9a-f]{64}$")
        for line in self.lines:
            if line.startswith("FROM "):
                self.assertRegex(line, r"^FROM (--platform=\$BUILDPLATFORM )?\$\{\w+_IMAGE\} AS [\w-]+$")

    def test_version_args_have_renovate_annotations(self):
        for i, line in enumerate(self.lines):
            if re.match(r"ARG \w+_VERSION=", line) and not line.startswith("ARG BERTH_"):
                self.assertRegex(self.lines[i - 1], r"^# renovate: datasource=\S+ depName=\S+", line)

    def test_every_download_has_a_checksum(self):
        text = read("Dockerfile")
        for name in ("MISE", "TTYD", "CODE_SERVER"):
            for arch in ("AMD64", "ARM64"):
                self.assertRegex(text, rf"ARG {name}_SHA256_{arch}=[0-9a-f]{{64}}\n")
        for name in ("TMUX_RESURRECT", "TMUX_CONTINUUM"):
            self.assertRegex(text, rf"ARG {name}_SHA256=[0-9a-f]{{64}}\n")
        self.assertNotRegex(text, r"\bcurl\s+-", "download only through fetch-artifact")

    def test_token_only_as_secret(self):
        text = read("Dockerfile")
        self.assertNotRegex(text, r"(ARG|ENV)\s+GITHUB_TOKEN")
        self.assertIn("--mount=type=secret,id=github_token", text)

    def test_runtime_contract(self):
        text = read("Dockerfile")
        self.assertIn('ENTRYPOINT ["tini", "--", "berth-entrypoint"]', text)
        self.assertIn('CMD ["serve"]', text)
        self.assertIn("DISABLE_AUTOUPDATER=1", text)
        self.assertIn("PATH=/opt/agents/current/bin:/opt/mise/wrappers:", text)
        self.assertIn("/usr/sbin:/usr/bin:/sbin:/bin", text)
        self.assertNotIn("VOLUME", text)
        self.assertIn("passwd", text)


class ComposeTest(unittest.TestCase):
    def test_compose_contract(self):
        text = read("compose.yaml")
        self.assertIn("image: ghcr.io/salgnt/berth:${BERTH_VERSION:?", text)
        self.assertIn("${BIND_ADDRESS:?", text)
        self.assertIn("${TTYD_PORT:-7681}:${TTYD_PORT:-7681}", text)
        self.assertIn("${CODE_SERVER_PORT:-8080}:${CODE_SERVER_PORT:-8080}", text)
        self.assertIn("no-new-privileges:true", text)
        for vol in ("berth-home", "berth-agents"):
            self.assertIn(f"name: {vol}", text)
        self.assertNotIn("0.0.0.0", text)

    def test_env_example_documents_every_variable(self):
        env = read(".env.example")
        for var in ("BERTH_VERSION", "BIND_ADDRESS", "PUID", "PGID", "WORKSPACES_DIR",
                    "WEB_TERMINAL_CREDENTIAL", "TTYD_PORT", "CODE_SERVER_PASSWORD", "CODE_SERVER_PORT",
                    "AGENTS_BOOT_TIMEOUT", "AGENTS_KEEP", "MCP_SETUP_TIMEOUT", "TZ"):
            self.assertRegex(env, rf"(?m)^{var}=", var)
        self.assertRegex(env, r"# renovate: datasource=docker depName=ghcr.io/salgnt/berth\nBERTH_VERSION=\d+\.\d+\.\d+\n")


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
        self.assertIn("runner: ubuntu-24.04-arm", text)

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
        self.assertIn("runner: ubuntu-24.04-arm", self.text)

    def test_write_permissions_scoped_to_jobs(self):
        top = self.text.split("\njobs:")[0]
        self.assertIn("permissions:\n  contents: read", top)
        self.assertNotIn("packages: write", top)


class RenovateTest(unittest.TestCase):
    def setUp(self):
        self.cfg = json.loads(read("renovate.json"))
        self.rules = self.cfg["packageRules"]

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
            ("custom.regex", "minor", ".env.example", False),
        ]
        for manager, update, path, automerge in cases:
            with self.subTest(manager=manager, update=update):
                self.assertEqual(self.effective(manager, update, path).get("automerge", False), automerge)

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
                                 "tmux-plugins/tmux-resurrect", "tmux-plugins/tmux-continuum"})



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


if __name__ == "__main__":
    unittest.main()
