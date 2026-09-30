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


if __name__ == "__main__":
    unittest.main()
