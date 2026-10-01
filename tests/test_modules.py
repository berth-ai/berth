import unittest

from shellharness import Harness, needs_bash


@needs_bash
class ModuleLibraryTest(unittest.TestCase):
    def setUp(self):
        self.h = Harness(self)

    def test_module_name(self):
        r = self.h.bash('module_name /x/20-code-server.sh; module_name /x/10-ttyd.sh; module_name /x/plain.sh')
        self.assertEqual(r.stdout, "code_server\nttyd\nplain\n", r.stderr)

    def test_load_modules_collects_names_and_secrets_in_order(self):
        d = self.h.tmp / "services"
        d.mkdir()
        (d / "20-beta-two.sh").write_text("beta_two_enabled() { :; }\nbeta_two_start() { :; }\n")
        (d / "10-alpha.sh").write_text(
            "alpha_enabled() { :; }\nalpha_start() { :; }\nalpha_secrets() { echo A_ONE; echo A_TWO; }\n")
        r = self.h.bash('load_modules; echo "${BERTH_MODULES[*]}|${BERTH_ALL_SECRETS[*]}"',
                        env={"BERTH_SERVICES_DIR": str(d)})
        self.assertEqual(r.stdout, "alpha beta_two|A_ONE A_TWO\n", r.stderr)

    def test_load_modules_rejects_incomplete_module(self):
        d = self.h.tmp / "services"
        d.mkdir()
        (d / "10-broken.sh").write_text("broken_enabled() { :; }\n")
        r = self.h.bash("load_modules", env={"BERTH_SERVICES_DIR": str(d)})
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("broken_start", r.stderr)

    def test_valid_port(self):
        for value, ok in (("7681", True), ("1", True), ("65535", True), ("0", False),
                          ("65536", False), ("08", True), ("80a", False), ("", False)):
            with self.subTest(value=value):
                r = self.h.bash(f'valid_port PORT "{value}"')
                self.assertEqual(r.returncode == 0, ok, r.stderr)

    def test_start_service_scrubs_other_modules_secrets(self):
        r = self.h.bash(
            'BERTH_ALL_SECRETS=(MINE THEIRS)\n'
            'mine_secrets() { echo MINE; }\n'
            'start_service mine sh -c \'echo "mine=${MINE:-} theirs=${THEIRS:-}"\'\n'
            'wait "${BERTH_PIDS[0]}"; echo "names=${BERTH_PID_NAMES[*]}"',
            env={"MINE": "m", "THEIRS": "t"})
        self.assertEqual(r.stdout, "mine=m theirs=\nnames=mine\n", r.stderr)
        self.assertEqual(self.h.read_log("gosu").split()[0], "agent")

    def test_start_root_service_scrubs_every_secret_and_skips_gosu(self):
        r = self.h.bash(
            'BERTH_ALL_SECRETS=(MINE THEIRS)\n'
            'mine_secrets() { echo MINE; }\n'
            'start_root_service mine sh -c \'echo "mine=${MINE:-} theirs=${THEIRS:-} keep=${KEEP:-}"\'\n'
            'wait "${BERTH_PIDS[0]}"; echo "names=${BERTH_PID_NAMES[*]}"',
            env={"MINE": "m", "THEIRS": "t", "KEEP": "k"})
        self.assertEqual(r.stdout, "mine= theirs= keep=k\nnames=mine\n", r.stderr)
        self.assertEqual(self.h.read_log("gosu"), "")


if __name__ == "__main__":
    unittest.main()
