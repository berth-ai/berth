import json
import unittest

from shellharness import BIN_DIR, Harness, needs_bash, needs_jq

MCP_SETUP = BIN_DIR / "berth-mcp-setup"
RECORDER = '#!/bin/sh\necho "$*" >> "$FAKE_LOG/$(basename "$0")"\n'


@needs_bash
@needs_jq
class McpSetupTest(unittest.TestCase):
    def setUp(self):
        self.h = Harness(self)
        self.bin = self.h.tmp / "layer"
        self.bin.mkdir(parents=True)
        self.config = self.h.home / ".config" / "opencode" / "opencode.json"

    def agent(self, name):
        p = self.bin / name
        p.write_text(RECORDER)
        p.chmod(0o755)

    def server(self, name):
        p = self.bin / name
        p.write_text("#!/bin/sh\n")
        p.chmod(0o755)

    def servers(self):
        self.server("playwright-mcp")
        self.server("context7-mcp")

    def run_setup(self):
        return self.h.run(MCP_SETUP, env={"XDG_CONFIG_HOME": str(self.h.home / ".config"),
                                          "BERTH_LAYER_DIR": str(self.bin)})

    def test_registers_with_every_agent(self):
        for name in ("claude", "codex", "opencode"):
            self.agent(name)
        self.servers()
        r = self.run_setup()
        self.assertEqual(r.returncode, 0, r.stderr)
        pw = f"{self.bin}/playwright-mcp --headless --no-sandbox"
        c7 = f"{self.bin}/context7-mcp"
        self.assertEqual(self.h.read_log("claude").splitlines(), [
            "mcp remove --scope user playwright", f"mcp add --scope user playwright -- {pw}",
            "mcp remove --scope user context7", f"mcp add --scope user context7 -- {c7}",
        ])
        self.assertEqual(self.h.read_log("codex").splitlines(), [
            "mcp remove playwright", f"mcp add playwright -- {pw}",
            "mcp remove context7", f"mcp add context7 -- {c7}",
        ])
        cfg = json.loads(self.config.read_text())
        self.assertEqual(cfg["mcp"]["playwright"], {
            "type": "local", "command": [f"{self.bin}/playwright-mcp", "--headless", "--no-sandbox"], "enabled": True})
        self.assertEqual(cfg["mcp"]["context7"]["command"], [c7])

    def test_opencode_preserves_user_entries_and_is_idempotent(self):
        self.agent("opencode")
        self.servers()
        self.config.parent.mkdir(parents=True)
        self.config.write_text(json.dumps({"theme": "x", "mcp": {"mine": {"type": "remote", "url": "u"}}}))
        self.assertEqual(self.run_setup().returncode, 0)
        first = self.config.read_text()
        self.assertEqual(self.run_setup().returncode, 0)
        self.assertEqual(self.config.read_text(), first)
        cfg = json.loads(first)
        self.assertEqual(cfg["theme"], "x")
        self.assertEqual(cfg["mcp"]["mine"], {"type": "remote", "url": "u"})
        self.assertIn("playwright", cfg["mcp"])

    def test_invalid_opencode_config_is_untouched(self):
        for name in ("claude", "opencode"):
            self.agent(name)
        self.servers()
        self.config.parent.mkdir(parents=True)
        original = '{\n  // my comment\n  "theme": "x",\n}\n'
        self.config.write_text(original)
        r = self.run_setup()
        self.assertEqual(r.returncode, 1)
        self.assertEqual(self.config.read_text(), original)
        self.assertIn("mcp add --scope user context7", self.h.read_log("claude"))
        self.assertEqual(list(self.config.parent.iterdir()), [self.config])  # no temp files left

    def test_missing_agents_are_skipped(self):
        self.agent("claude")
        self.servers()
        r = self.run_setup()
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("codex not enabled", r.stderr)
        self.assertFalse(self.config.exists())

    def test_disabled_server_is_removed_everywhere(self):
        for name in ("claude", "codex", "opencode"):
            self.agent(name)
        self.server("context7-mcp")  # playwright-mcp is disabled: no wrapper
        self.config.parent.mkdir(parents=True)
        self.config.write_text(json.dumps({"mcp": {"playwright": {"type": "local"}, "mine": {"type": "remote"}}}))
        r = self.run_setup()
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("mcp remove --scope user playwright", self.h.read_log("claude"))
        self.assertNotIn("mcp add --scope user playwright", self.h.read_log("claude"))
        self.assertIn("mcp remove playwright", self.h.read_log("codex"))
        cfg = json.loads(self.config.read_text())
        self.assertNotIn("playwright", cfg["mcp"])
        self.assertEqual(cfg["mcp"]["mine"], {"type": "remote"})
        self.assertIn("context7", cfg["mcp"])

    def test_disabled_agent_is_left_alone(self):
        self.agent("claude")
        self.server("playwright-mcp")
        self.config.parent.mkdir(parents=True)
        original = '{"mcp": {}}\n'
        self.config.write_text(original)
        r = self.run_setup()
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("opencode not enabled", r.stderr)
        self.assertEqual(self.config.read_text(), original)


    def test_opencode_symlinked_config_keeps_link_and_mode(self):
        self.agent("opencode")
        self.servers()
        target = self.h.home / "dotfiles" / "opencode.json"
        target.parent.mkdir()
        target.write_text('{"theme": "dark"}\n')
        target.chmod(0o644)
        self.config.parent.mkdir(parents=True)
        self.config.symlink_to(target)
        r = self.run_setup()
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertTrue(self.config.is_symlink(), "config symlink was replaced")
        self.assertEqual(target.stat().st_mode & 0o777, 0o644)
        data = json.loads(target.read_text())
        self.assertEqual(data["theme"], "dark")
        self.assertEqual(sorted(data["mcp"]), ["context7", "playwright"])

if __name__ == "__main__":
    unittest.main()
