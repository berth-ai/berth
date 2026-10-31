import json
import os
import shutil
import stat
import unittest

from shellharness import MODULE_RUN, Harness, needs_bash, needs_jq

ORCA_FAKE = r"""
echo "$*" >> "$FAKE_LOG/orca-ide.calls"
env | sort > "$FAKE_LOG/orca-ide.env"
case " $* " in
  *" --reclaim-stale-lock "*) echo "lock: none"; exit "${FAKE_RECLAIM_STATUS:-4}" ;;
esac
echo "orca: starting" >&2
case " $* " in
  *" --json "*) echo '{"type":"orca_server_ready","schemaVersion":1,"pairing":{"available":true,"url":"orca://pair?code=t"}}' ;;
  *) echo "Orca server ready" ;;
esac
"""


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
                         ["-W", "-a", "-i", "lo", "-p", "7681", "-b", "/terminal", "-w", "/workspaces",
                          "berth-term"])
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
        for name, port in (("ttyd", "7681"), ("code_server", "8080"), ("panel", "7690"), ("orca", "6768")):
            with self.subTest(name=name):
                r = self.h.run(MODULE_RUN, "--health", name)
                self.assertEqual((r.returncode, r.stdout), (0, port + "\n"), r.stderr)
        self.assertEqual(self.h.read_log("gosu"), "", "health probes must not drop privileges")

    def orca(self, request=None, reclaim_status=4):
        self.h.fake("orca-ide", ORCA_FAKE)
        run_dir = self.h.tmp / "orca-run"
        shutil.rmtree(run_dir, ignore_errors=True)
        run_dir.mkdir(mode=0o700)
        if request is not None:
            (run_dir / "pair-request.json").write_text(json.dumps(request))
        r = self.h.run(MODULE_RUN, "orca", env={"BERTH_ORCA_RUN_DIR": str(run_dir),
                                                  "FAKE_RECLAIM_STATUS": str(reclaim_status)})
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertNotIn("orca://", r.stdout + r.stderr)
        calls = [c.split() for c in self.h.read_log("orca-ide.calls").splitlines()]
        return r, run_dir, calls

    def test_orca_runs_as_agent_without_pairing(self):
        r, run_dir, calls = self.orca()
        self.assertTrue(self.h.read_log("gosu").startswith("agent "))
        self.assertEqual(calls[-1], ["serve", "--port", "6768", "--no-pairing"])
        self.assertIn("ORCA_TELEMETRY_DISABLED=1", self.h.read_log("orca-ide.env").splitlines())
        self.assertIn("Orca server ready", r.stdout)
        self.assertFalse((run_dir / "readiness.json").exists())

    def test_orca_reclaims_a_stale_lock_first(self):
        for status in (0, 1, 4):
            with self.subTest(status=status):
                self.h.log_path("orca-ide.calls").unlink(missing_ok=True)
                r, _, calls = self.orca(reclaim_status=status)
                self.assertEqual(calls[0], ["serve", "--reclaim-stale-lock", "--yes"])
                self.assertEqual(calls[1][:2], ["serve", "--port"])
                self.assertNotIn("lock: none", r.stdout, "reclaim output belongs on stderr")

    @needs_jq
    def test_orca_pairs_once_from_a_request(self):
        r, run_dir, calls = self.orca({"scope": "desktop", "address": "100.64.0.10:6768"})
        self.assertEqual(calls[-1], ["serve", "--port", "6768", "--pairing-address", "100.64.0.10:6768", "--json"])
        self.assertFalse((run_dir / "pair-request.json").exists())
        ready = run_dir / "readiness.json"
        self.assertIn("orca://pair?code=t", ready.read_text())
        self.assertEqual(stat.S_IMODE(os.stat(ready).st_mode), 0o600)
        self.assertIn("orca: starting", r.stderr)

    @needs_jq
    def test_orca_mobile_request_adds_mobile_pairing(self):
        _, _, calls = self.orca({"scope": "mobile", "address": "berth.example"})
        self.assertEqual(calls[-1], ["serve", "--port", "6768", "--pairing-address", "berth.example", "--json",
                                     "--mobile-pairing"])

    @needs_jq
    def test_orca_ignores_a_bad_request(self):
        for request in ({"scope": "tablet", "address": "berth.example"}, {"scope": "desktop", "address": ""},
                        {"scope": "desktop", "address": "--force"}):
            with self.subTest(request=request):
                self.h.log_path("orca-ide.calls").unlink(missing_ok=True)
                r, run_dir, calls = self.orca(request)
                self.assertEqual(calls[-1], ["serve", "--port", "6768", "--no-pairing"])
                self.assertFalse((run_dir / "pair-request.json").exists())
                self.assertIn("ignoring", r.stderr)

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
