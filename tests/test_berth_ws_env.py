"""berth-ws-env: a workspace's variables, overlaid on the environment."""

import json
import os
import shutil
import unittest

from shellharness import BIN_DIR, Harness, needs_bash, needs_jq

WS_ENV = BIN_DIR / "berth-ws-env"

PROBE = """#!/usr/bin/env python3
import json, os, sys
with open(os.environ["PROBE_OUT"], "w") as f:
    json.dump({"argv": sys.argv[1:], "env": dict(os.environ)}, f)
"""

TRICKY = "line one\nline two 'single' \"double\" $(touch pwned) `id` ; *"


@needs_bash
@needs_jq
class BerthWsEnvTest(unittest.TestCase):
    def setUp(self):
        self.h = Harness(self)
        self.probe = self.h.tmp / "probe"
        self.probe.write_text(PROBE)
        self.probe.chmod(0o755)
        self.out = self.h.tmp / "probe.json"
        self.pub = self.h.tmp / "workspaces.json"
        self.secrets = self.h.tmp / "secrets"
        self.secrets.mkdir()
        self.pub.write_text(json.dumps({"schema": 1, "workspaces": {
            "acme": {"path": str(self.h.tmp), "env": {"LOG_LEVEL": "debug", "SHARED": "plain", "PATH": "/evil"}},
        }}))
        (self.secrets / "acme.json").write_text(json.dumps({"schema": 1, "env": {
            "STRIPE_KEY": TRICKY, "SHARED": "secret", "LD_PRELOAD": "/evil.so"}}))
        real_jq = shutil.which("jq")
        self.h.fake("jq", f'printf "%s\\n" "$@" >> "$FAKE_LOG/jq.argv"\nexec {real_jq} "$@"\n')
        self.env = {"BERTH_WORKSPACES_FILE": str(self.pub), "BERTH_WS_SECRETS_DIR": str(self.secrets),
                    "PROBE_OUT": str(self.out), "LOG_LEVEL": "inherited", "ANTHROPIC_API_KEY": "kept"}

    def probe_result(self):
        return json.loads(self.out.read_text())

    def test_overlay_order(self):
        r = self.h.run(WS_ENV, "acme", "--", str(self.probe), "a b", "c", env=self.env)
        self.assertEqual(r.returncode, 0, r.stderr)
        got = self.probe_result()
        self.assertEqual(got["argv"], ["a b", "c"])
        env = got["env"]
        self.assertEqual(env["LOG_LEVEL"], "debug")          # plain beats inherited
        self.assertEqual(env["SHARED"], "secret")            # secret beats plain
        self.assertEqual(env["ANTHROPIC_API_KEY"], "kept")   # untouched
        self.assertNotEqual(env["PATH"], "/evil")            # reserved keys are refused
        self.assertNotIn("LD_PRELOAD", env)
        self.assertIn("ignoring PATH", r.stderr)

    def test_values_are_literal_and_not_in_argv(self):
        r = self.h.run(WS_ENV, "acme", "--", str(self.probe), env=self.env)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.probe_result()["env"]["STRIPE_KEY"], TRICKY)
        self.assertFalse((self.h.tmp / "pwned").exists())
        self.assertFalse(os.path.exists("pwned"))
        jq_argv = self.h.read_log("jq.argv")
        self.assertNotIn("Stripe", jq_argv)
        self.assertNotIn("line one", jq_argv)
        self.assertNotIn("debug", jq_argv)

    def test_unknown_workspace_still_runs(self):
        r = self.h.run(WS_ENV, "nope", "--", str(self.probe), env=self.env)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("no settings", r.stderr)
        self.assertEqual(self.probe_result()["env"]["LOG_LEVEL"], "inherited")

    def test_missing_files_still_run(self):
        env = dict(self.env, BERTH_WORKSPACES_FILE=str(self.h.tmp / "none.json"),
                   BERTH_WS_SECRETS_DIR=str(self.h.tmp / "none"))
        r = self.h.run(WS_ENV, "acme", "--", str(self.probe), env=env)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.probe_result()["argv"], [])

    def test_no_secrets_file(self):
        (self.secrets / "acme.json").unlink()
        r = self.h.run(WS_ENV, "acme", "--", str(self.probe), env=self.env)
        self.assertEqual(r.returncode, 0, r.stderr)
        env = self.probe_result()["env"]
        self.assertEqual(env["SHARED"], "plain")
        self.assertNotIn("STRIPE_KEY", env)

    def test_usage(self):
        for args in ([], ["acme"], ["acme", "--"], ["Acme", "--", "true"], ["acme", "x", "true"],
                     ["a;b", "--", "true"]):
            r = self.h.run(WS_ENV, *args, env=self.env)
            self.assertEqual(r.returncode, 2, args)
            self.assertIn("usage", r.stderr, args)


if __name__ == "__main__":
    unittest.main()
