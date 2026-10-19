"""berth-term: what the browser terminal runs."""

import json
import unittest

from shellharness import BIN_DIR, Harness, needs_bash, needs_jq

BERTH_TERM = BIN_DIR / "berth-term"


@needs_bash
@needs_jq
class BerthTermTest(unittest.TestCase):
    def setUp(self):
        self.h = Harness(self)
        self.h.fake("tmux", 'printf "%s\\n" "$@" >> "$FAKE_LOG/tmux.argv"\necho ---- >> "$FAKE_LOG/tmux.argv"\n'
                    'if [ "$1" = start-server ]; then echo /usr/bin/zsh; fi\n')
        self.ws = self.h.tmp / "workspaces" / "acme" / "api"
        self.ws.mkdir(parents=True)
        self.file = self.h.tmp / "workspaces.json"
        self.file.write_text(json.dumps({"schema": 1, "workspaces": {
            "acme-api": {"path": str(self.ws)},
            "gone": {"path": str(self.h.tmp / "gone")},
        }}))
        self.env = {"BERTH_WORKSPACES_FILE": str(self.file), "BERTH_WS_ENV": "/usr/local/bin/berth-ws-env"}

    def calls(self):
        text = self.h.read_log("tmux.argv")
        return [c.strip("\n").splitlines() for c in text.split("----\n") if c.strip()]

    def argv(self):
        calls = self.calls()
        return calls[-1] if calls else []

    def test_no_argument_attaches_main(self):
        r = self.h.run(BERTH_TERM, env=self.env)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.calls(), [["new", "-A", "-s", "main"]])

    def test_workspace_session_starts_in_its_folder(self):
        r = self.h.run(BERTH_TERM, "ws-acme-api", env=self.env)
        self.assertEqual(r.returncode, 0, r.stderr)
        cmd = "exec '/usr/local/bin/berth-ws-env' 'acme-api' -- '/usr/bin/zsh' -l"
        self.assertEqual(self.argv(), ["new", "-A", "-s", "ws-acme-api", "-c", str(self.ws), cmd,
                                       ";", "set-option", "-t", "=ws-acme-api", "default-command", cmd])

    def test_unknown_workspace(self):
        r = self.h.run(BERTH_TERM, "ws-nope", env=self.env)
        self.assertEqual(r.returncode, 1)
        self.assertIn("no workspace named nope", r.stderr)
        self.assertEqual(self.calls(), [])

    def test_missing_folder(self):
        r = self.h.run(BERTH_TERM, "ws-gone", env=self.env)
        self.assertEqual(r.returncode, 1)
        self.assertIn("is missing", r.stderr)
        self.assertEqual(self.calls(), [])

    def test_missing_file(self):
        r = self.h.run(BERTH_TERM, "ws-acme-api", env={"BERTH_WORKSPACES_FILE": str(self.h.tmp / "none.json")})
        self.assertEqual(r.returncode, 1)
        self.assertIn("no workspace named acme-api", r.stderr)

    def test_malformed_arguments(self):
        for args in (["main"], ["ws-"], ["ws-Acme"], ["ws-a;b"], ["ws-a b"], ["-c"],
                     ["ws-acme-api", "extra"], ["ws-" + "a" * 65]):
            r = self.h.run(BERTH_TERM, *args, env=self.env)
            self.assertEqual(r.returncode, 1, args)
            self.assertIn("usage", r.stderr, args)
        self.assertEqual(self.calls(), [])


if __name__ == "__main__":
    unittest.main()
