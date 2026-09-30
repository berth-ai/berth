"""Static checks over the repository's pins and build configuration."""

import json
import re
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


if __name__ == "__main__":
    unittest.main()
