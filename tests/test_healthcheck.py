import os
import socket
import subprocess
import unittest

from shellharness import BIN_DIR, Harness, needs_bash

HEALTHCHECK = BIN_DIR / "berth-healthcheck"


@needs_bash
class HealthcheckTest(unittest.TestCase):
    def setUp(self):
        self.h = Harness(self)
        self.h.run_dir.mkdir()

    def state(self, pid, modules):
        (self.h.run_dir / "supervisor.pid").write_text(f"{pid}\n")
        (self.h.run_dir / "modules").write_text("".join(m + "\n" for m in modules))

    def test_not_started(self):
        r = self.h.run(HEALTHCHECK)
        self.assertEqual(r.returncode, 1)
        self.assertIn("has not started", r.stdout)

    def test_dead_supervisor(self):
        done = subprocess.Popen(["true"])
        done.wait()
        self.state(done.pid, [])
        r = self.h.run(HEALTHCHECK)
        self.assertEqual(r.returncode, 1)
        self.assertIn("not running", r.stdout)

    def test_idle_is_healthy(self):
        self.state(os.getpid(), [])
        r = self.h.run(HEALTHCHECK)
        self.assertEqual((r.returncode, r.stdout), (0, "healthy\n"), r.stderr)

    def test_port_probe(self):
        srv = socket.socket()
        srv.bind(("127.0.0.1", 0))
        srv.listen()
        port = srv.getsockname()[1]
        self.state(os.getpid(), ["ttyd"])
        env = {"WEB_TERMINAL_CREDENTIAL": "me:pw", "TTYD_PORT": str(port)}
        self.assertEqual(self.h.run(HEALTHCHECK, env=env).returncode, 0)
        srv.close()
        r = self.h.run(HEALTHCHECK, env=env)
        self.assertEqual(r.returncode, 1)
        self.assertIn(f"port {port}", r.stdout)

    def test_custom_health_function(self):
        d = self.h.tmp / "services"
        d.mkdir()
        (d / "50-custom.sh").write_text(
            "custom_enabled() { :; }\ncustom_start() { :; }\ncustom_health() { return 1; }\n")
        self.state(os.getpid(), ["custom"])
        r = self.h.run(HEALTHCHECK, env={"BERTH_SERVICES_DIR": str(d)})
        self.assertEqual(r.returncode, 1)
        self.assertIn("custom health check failed", r.stdout)


if __name__ == "__main__":
    unittest.main()
