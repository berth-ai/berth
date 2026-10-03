import unittest

from shellharness import BIN_DIR, Harness, needs_bash

HEALTHCHECK = BIN_DIR / "berth-healthcheck"


@needs_bash
class HealthcheckTest(unittest.TestCase):
    def test_delegates_to_berth_health(self):
        h = Harness(self)
        for status in ("0", "1"):
            with self.subTest(status=status):
                r = h.run(HEALTHCHECK, env={"FAKE_BERTH_STATUS": status})
                self.assertEqual(r.returncode, int(status), r.stderr)
        self.assertEqual(h.read_log("berth"), "health\nhealth\n")


if __name__ == "__main__":
    unittest.main()
