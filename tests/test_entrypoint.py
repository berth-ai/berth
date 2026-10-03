import unittest

from shellharness import BIN_DIR, Harness, needs_bash

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

    def test_rejects_ids_of_berth_panel(self):
        for env in ({"PUID": "990"}, {"PGID": "990"}):
            with self.subTest(env=env):
                r = self.h.run(ENTRYPOINT, "true", env=env)
                self.assertNotEqual(r.returncode, 0)
                self.assertIn("berth-panel", r.stderr)
                self.assertEqual(self.h.read_log("identity"), "")


@needs_bash
class ServeTest(unittest.TestCase):
    def setUp(self):
        self.h = Harness(self)

    def test_serve_execs_supervisor_as_root(self):
        r = self.h.run(ENTRYPOINT, "serve")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.h.read_log("berth"), "supervise\n")
        self.assertNotIn("berth", self.h.read_log("gosu"))

    def test_default_command_is_serve(self):
        self.assertEqual(self.h.run(ENTRYPOINT).returncode, 0)
        self.assertEqual(self.h.read_log("berth"), "supervise\n")

    def test_warns_when_workspaces_not_writable(self):
        ws = self.h.tmp / "ws"
        ws.mkdir()
        ws.chmod(0o555)
        self.addCleanup(ws.chmod, 0o755)
        r = self.h.run(ENTRYPOINT, "serve", env={"BERTH_WORKSPACES": str(ws)})
        self.assertIn(f"{ws} is not writable by agent", r.stderr)

    def test_no_warning_when_workspaces_writable(self):
        ws = self.h.tmp / "ws"
        ws.mkdir()
        r = self.h.run(ENTRYPOINT, "serve", env={"BERTH_WORKSPACES": str(ws)})
        self.assertNotIn("not writable", r.stderr)


if __name__ == "__main__":
    unittest.main()
