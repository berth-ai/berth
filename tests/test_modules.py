import unittest

from shellharness import MODULE_RUN, Harness, needs_bash


@needs_bash
class ModuleNameTest(unittest.TestCase):
    def test_module_name(self):
        h = Harness(self)
        r = h.bash('module_name /x/20-code-server.sh; module_name /x/10-ttyd.sh; module_name /x/plain.sh')
        self.assertEqual(r.stdout, "code_server\nttyd\nplain\n", r.stderr)


@needs_bash
class ModuleRunTest(unittest.TestCase):
    def setUp(self):
        self.h = Harness(self)

    def test_ttyd_runs_as_agent_on_loopback(self):
        r = self.h.run(MODULE_RUN, "ttyd", env={"TZ": "Europe/Rome"})
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.h.read_log("gosu").split()[0:2], ["agent", str(MODULE_RUN)])
        self.assertEqual(self.h.read_log("ttyd.argv").splitlines(),
                         ["-W", "-i", "lo", "-p", "7681", "-b", "/terminal", "-w", "/workspaces",
                          "tmux", "new", "-A", "-s", "main"])
        self.assertIn("TZ=Europe/Rome", self.h.read_log("ttyd.env").splitlines())

    def test_code_server_runs_as_agent_without_auth(self):
        r = self.h.run(MODULE_RUN, "code_server")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertTrue(self.h.read_log("gosu").startswith("agent "))
        self.assertEqual(self.h.read_log("code-server.argv").splitlines(),
                         ["--bind-addr", "127.0.0.1:8080", "--auth", "none", "--abs-proxy-base-path", "/ide",
                          "--disable-telemetry", "--disable-update-check", "/workspaces"])
        self.assertFalse([l for l in self.h.read_log("code-server.env").splitlines() if l.startswith("PASSWORD=")])

    def test_panel_runs_as_berth_panel(self):
        self.h.fake("berth", 'echo "$*" >> "$FAKE_LOG/berth"\n')
        r = self.h.run(MODULE_RUN, "panel")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertTrue(self.h.read_log("gosu").startswith("berth-panel "))
        self.assertEqual(self.h.read_log("berth"), "panel\n")

    def test_health(self):
        for name, port in (("ttyd", "7681"), ("code_server", "8080"), ("panel", "7690")):
            with self.subTest(name=name):
                r = self.h.run(MODULE_RUN, "--health", name)
                self.assertEqual((r.returncode, r.stdout), (0, port + "\n"), r.stderr)
        self.assertEqual(self.h.read_log("gosu"), "", "health probes must not drop privileges")

    def test_health_without_function_is_healthy(self):
        d = self.h.tmp / "services"
        d.mkdir()
        (d / "50-plain.sh").write_text("plain_start() { exec true; }\n")
        r = self.h.run(MODULE_RUN, "--health", "plain", env={"BERTH_SERVICES_DIR": str(d)})
        self.assertEqual((r.returncode, r.stdout), (0, ""), r.stderr)

    def test_rejects_unknown_and_invalid_names(self):
        for name in ("nope", "../etc", "Ttyd", ""):
            with self.subTest(name=name):
                r = self.h.run(MODULE_RUN, name)
                self.assertNotEqual(r.returncode, 0)
                self.assertEqual(self.h.read_log("gosu"), "")

    def test_start_must_exec(self):
        d = self.h.tmp / "services"
        d.mkdir()
        (d / "50-lazy.sh").write_text("lazy_start() { :; }\n")
        r = self.h.run(MODULE_RUN, "lazy", env={"BERTH_SERVICES_DIR": str(d)})
        self.assertEqual(r.returncode, 1)
        self.assertIn("returned instead of exec", r.stderr)

    def test_module_without_start_fails(self):
        d = self.h.tmp / "services"
        d.mkdir()
        (d / "50-broken.sh").write_text("broken_health() { echo 1; }\n")
        r = self.h.run(MODULE_RUN, "broken", env={"BERTH_SERVICES_DIR": str(d)})
        self.assertEqual(r.returncode, 1)
        self.assertIn("broken_start", r.stderr)


if __name__ == "__main__":
    unittest.main()
