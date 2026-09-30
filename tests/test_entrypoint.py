import unittest

from shellharness import BIN_DIR, SERVICES_DIR as SERVICES, Harness, needs_bash

ENTRYPOINT = BIN_DIR / "berth-entrypoint"


@needs_bash
class IdentityAndRoutingTest(unittest.TestCase):
    def setUp(self):
        self.h = Harness(self)

    def test_rejects_invalid_ids(self):
        for env in ({"PUID": "abc"}, {"PUID": "-1"}, {"PGID": "0"}, {"PUID": ""}):
            with self.subTest(env=env):
                r = self.h.run(ENTRYPOINT, "true", env=env)
                self.assertNotEqual(r.returncode, 0)
                self.assertIn("must be a non-zero number", r.stderr)
                self.assertEqual(self.h.read_log("gosu"), "")

    def test_remaps_to_existing_group(self):
        r = self.h.run(ENTRYPOINT, "true", env={"PUID": "1000", "PGID": "100"})
        self.assertEqual(r.returncode, 0, r.stderr)
        identity = self.h.read_log("identity")
        self.assertIn("groupmod -o -g 100 agent", identity)
        self.assertNotIn("usermod", identity)
        self.assertIn(f"chown -R 1000:100 {self.h.home}", identity)

    def test_remaps_uid(self):
        r = self.h.run(ENTRYPOINT, "true", env={"PUID": "1001", "FAKE_HOME_OWNER": "1001:1000"})
        self.assertEqual(r.returncode, 0, r.stderr)
        identity = self.h.read_log("identity")
        self.assertIn("usermod -o -u 1001 agent", identity)
        self.assertNotIn("chown", identity)

    def test_leaves_matching_identity_alone(self):
        r = self.h.run(ENTRYPOINT, "true")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.h.read_log("identity"), "")

    def test_berth_runs_as_root(self):
        r = self.h.run(ENTRYPOINT, "berth", "status", "--json")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.h.read_log("berth"), "status --json\n")
        self.assertEqual(self.h.read_log("gosu"), "")

    def test_other_commands_run_as_agent_without_boot(self):
        r = self.h.run(ENTRYPOINT, "echo", "hello")
        self.assertEqual(r.stdout, "hello\n", r.stderr)
        self.assertEqual(self.h.read_log("gosu"), "agent echo hello\n")
        self.assertEqual(self.h.read_log("berth"), "")


@needs_bash
class BootTest(unittest.TestCase):
    """Boot checks that do not need a working supervisor."""

    def setUp(self):
        self.h = Harness(self)

    def test_credential_forms(self):
        for cred, ok in (("me:pw", True), ("me:pa:ss", True), ("me:", False), (":pw", False), ("nocolon", False)):
            with self.subTest(cred=cred):
                r = self.h.bash(f'source "{SERVICES}/10-ttyd.sh"; WEB_TERMINAL_CREDENTIAL="{cred}" ttyd_validate')
                self.assertEqual(r.returncode == 0, ok, r.stderr)

    def test_invalid_config_starts_nothing(self):
        r = self.h.run(ENTRYPOINT, "serve",
                       env={"WEB_TERMINAL_CREDENTIAL": "me:", "CODE_SERVER_PASSWORD": "pw"})
        self.assertEqual(r.returncode, 1)
        self.assertIn("user:password", r.stderr)
        self.assertFalse(self.h.log_path("ttyd.argv").exists())
        self.assertFalse(self.h.log_path("code-server.argv").exists())

    def test_invalid_port_starts_nothing(self):
        r = self.h.run(ENTRYPOINT, "serve", env={"CODE_SERVER_PASSWORD": "pw", "CODE_SERVER_PORT": "99999"})
        self.assertEqual(r.returncode, 1)
        self.assertIn("CODE_SERVER_PORT", r.stderr)
        self.assertFalse(self.h.log_path("code-server.argv").exists())

    def test_invalid_boot_timeout(self):
        r = self.h.run(ENTRYPOINT, "serve", env={"AGENTS_BOOT_TIMEOUT": "soon"})
        self.assertEqual(r.returncode, 1)
        self.assertIn("AGENTS_BOOT_TIMEOUT", r.stderr)


@needs_bash
class ServeTest(unittest.TestCase):
    def setUp(self):
        self.h = Harness(self)

    def serve(self, **env):
        proc = self.h.start(ENTRYPOINT, "serve", env=env)
        self.h.wait_for(lambda: (self.h.run_dir / "supervisor.pid").exists() or proc.poll() is not None)
        self.assertIsNone(proc.poll(), "entrypoint exited early")
        return proc

    def test_idle_mode(self):
        proc = self.serve()
        self.assertEqual(self.h.read_log("berth"), "ensure\n")
        self.h.wait_for(lambda: self.h.read_log("mcp"))
        code, _, err = self.h.stop(proc)
        self.assertEqual(code, 143)
        self.assertIn("ttyd: set WEB_TERMINAL_CREDENTIAL", err)
        self.assertIn("code_server: set CODE_SERVER_PASSWORD", err)
        self.assertEqual((self.h.run_dir / "modules").read_text(), "")

    def test_starts_modules_and_scrubs_secrets(self):
        proc = self.serve(WEB_TERMINAL_CREDENTIAL="me:pa:ss", CODE_SERVER_PASSWORD="hunter2", TTYD_PORT="9000")
        self.h.wait_for(lambda: self.h.log_path("ttyd.env").exists() and self.h.log_path("code-server.env").exists())
        argv = self.h.read_log("ttyd.argv").splitlines()
        self.assertEqual(argv, ["-W", "-p", "9000", "-w", "/workspaces", "-c", "me:pa:ss", "tmux", "new", "-A", "-s", "main"])
        ttyd_env = self.h.read_log("ttyd.env").splitlines()
        cs_env = self.h.read_log("code-server.env").splitlines()
        self.assertIn("WEB_TERMINAL_CREDENTIAL=me:pa:ss", ttyd_env)
        self.assertFalse([l for l in ttyd_env if l.startswith("CODE_SERVER_PASSWORD=")])
        self.assertFalse([l for l in cs_env if l.startswith("WEB_TERMINAL_CREDENTIAL=")])
        self.assertIn("PASSWORD=hunter2", cs_env)
        self.assertEqual((self.h.run_dir / "modules").read_text(), "ttyd\ncode_server\n")
        code, _, _ = self.h.stop(proc)
        self.assertEqual(code, 143)
        self.assertEqual(self.h.read_log("ttyd.signals"), "TERM\n")
        self.assertEqual(self.h.read_log("code-server.signals"), "TERM\n")

    def test_service_exit_stops_others(self):
        proc = self.h.start(ENTRYPOINT, "serve", env={
            "WEB_TERMINAL_CREDENTIAL": "me:pw", "CODE_SERVER_PASSWORD": "pw", "FAKE_EXIT_SERVICE": "ttyd:7"})
        proc.communicate(timeout=15)
        self.assertEqual(proc.returncode, 7)
        self.assertEqual(self.h.read_log("code-server.signals"), "TERM\n")

    def test_stubborn_service_is_killed_after_grace(self):
        self.h.fake("code-server", "trap '' TERM\nwhile :; do sleep 0.05; done\n")
        proc = self.serve(CODE_SERVER_PASSWORD="pw", BERTH_STOP_TIMEOUT="1")
        code, _, _ = self.h.stop(proc, timeout=10)
        self.assertEqual(code, 143)

    def test_mcp_setup_runs_in_background(self):
        proc = self.serve(FAKE_MCP_SLEEP="30")
        self.assertEqual(self.h.read_log("mcp"), "")  # still sleeping, yet we are up
        self.h.wait_for(lambda: "120 gosu agent berth-mcp-setup" in self.h.read_log("timeout"))
        # The sleeping MCP job still holds the output pipes, so wait on the
        # entrypoint alone; harness cleanup kills the process group.
        proc.terminate()
        self.assertEqual(proc.wait(timeout=15), 143)

    def test_ensure_failure_with_current_continues(self):
        (self.h.agents / "current").mkdir()
        (self.h.agents / "current" / ".complete").write_text("{}")
        proc = self.serve(FAKE_BERTH_STATUS="1")
        _, _, err = self.h.stop(proc)
        self.assertIn("continuing with the installed agents", err)

    def test_ensure_failure_without_current_warns_loudly(self):
        proc = self.serve(FAKE_BERTH_STATUS="1")
        _, _, err = self.h.stop(proc)
        self.assertIn("NO AGENTS INSTALLED", err)


if __name__ == "__main__":
    unittest.main()
