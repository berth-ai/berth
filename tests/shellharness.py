"""Run Berth's shell scripts outside a container, with fakes on PATH."""

import os
import shutil
import signal
import subprocess
import tempfile
import time
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
ROOTFS = REPO / "rootfs"
LIB_DIR = ROOTFS / "usr/local/lib/berth"
BIN_DIR = ROOTFS / "usr/local/bin"
SERVICES_DIR = ROOTFS / "etc/berth/services.d"


def _find_bash():
    candidates = [
        os.environ.get("BERTH_TEST_BASH"),
        shutil.which("bash"),
        "/opt/homebrew/bin/bash",
        "/usr/local/bin/bash",
    ]
    for candidate in candidates:
        if not candidate or not os.path.exists(candidate):
            continue
        out = subprocess.run(
            [candidate, "-c", 'echo "${BASH_VERSINFO[0]} ${BASH_VERSINFO[1]}"'],
            capture_output=True, text=True, check=False,
        ).stdout.split()
        if len(out) == 2 and (int(out[0]), int(out[1])) >= (5, 1):
            return candidate
    return None


BASH = _find_bash()
needs_bash = unittest.skipUnless(BASH, "needs bash >= 5.1 (set BERTH_TEST_BASH or install a newer bash)")
needs_jq = unittest.skipUnless(shutil.which("jq"), "needs jq")

SERVICE_FAKE = r"""
name=$(basename "$0")
printf '%s\n' "$@" > "$FAKE_LOG/$name.argv"
env | sort > "$FAKE_LOG/$name.env.tmp" && mv "$FAKE_LOG/$name.env.tmp" "$FAKE_LOG/$name.env"
trap 'echo TERM >> "$FAKE_LOG/$name.signals"; exit 0' TERM
case "${FAKE_EXIT_SERVICE:-}" in
  "$name":*) sleep 0.3; exit "${FAKE_EXIT_SERVICE#*:}" ;;
esac
while :; do sleep 0.05; done
"""


class Harness:
    def __init__(self, test):
        self.tmp = Path(tempfile.mkdtemp(prefix="berth-test-"))
        test.addCleanup(shutil.rmtree, self.tmp, ignore_errors=True)
        self.fakebin = self.tmp / "fakebin"
        self.logdir = self.tmp / "log"
        self.run_dir = self.tmp / "run"
        self.home = self.tmp / "home"
        self.agents = self.tmp / "agents"
        for d in (self.fakebin, self.logdir, self.home, self.agents):
            d.mkdir()
        os.symlink(BASH, self.fakebin / "bash")
        self.env = {
            # Fakes win; real tools (jq, sleep, env, grep) come from the caller's PATH.
            "PATH": f"{self.fakebin}:{os.environ.get('PATH', '/usr/bin:/bin')}",
            "HOME": str(self.home),
            "LC_ALL": "C",
            "BERTH_LIB_DIR": str(LIB_DIR),
            "BERTH_SERVICES_DIR": str(SERVICES_DIR),
            "BERTH_RUN_DIR": str(self.run_dir),
            "BERTH_HOME": str(self.home),
            "BERTH_AGENTS_DIR": str(self.agents),
            "FAKE_LOG": str(self.logdir),
        }
        self._procs = []
        test.addCleanup(self._cleanup)
        self._default_fakes()

    def fake(self, name, body):
        path = self.fakebin / name
        path.write_text("#!/bin/sh\n" + body)
        path.chmod(0o755)
        return path

    def _default_fakes(self):
        self.fake("gosu", 'echo "$*" >> "$FAKE_LOG/gosu"\nshift\nexec "$@"\n')
        self.fake("timeout", 'echo "$*" >> "$FAKE_LOG/timeout"\nshift\nexec "$@"\n')
        self.fake("id", 'case "$1" in -u) echo "${FAKE_UID:-1000}" ;; -g) echo "${FAKE_GID:-1000}" ;; *) exec /usr/bin/id "$@" ;; esac\n')
        for tool in ("groupmod", "usermod", "chown"):
            self.fake(tool, f'echo "{tool} $*" >> "$FAKE_LOG/identity"\n')
        self.fake("stat", 'echo "${FAKE_HOME_OWNER:-1000:1000}"\n')
        self.fake("berth", 'echo "$*" >> "$FAKE_LOG/berth"\nexit "${FAKE_BERTH_STATUS:-0}"\n')
        self.fake("berth-mcp-setup", 'sleep "${FAKE_MCP_SLEEP:-0}"\necho ran >> "$FAKE_LOG/mcp"\n')
        for service in ("ttyd", "code-server"):
            self.fake(service, SERVICE_FAKE)

    def _env(self, extra):
        env = dict(self.env)
        env.update(extra or {})
        return env

    def run(self, script, *args, env=None, timeout=20):
        return subprocess.run([BASH, str(script), *args], env=self._env(env),
                              capture_output=True, text=True, timeout=timeout, check=False)

    def bash(self, snippet, env=None):
        return subprocess.run([BASH, "-c", f'source "$BERTH_LIB_DIR/lib.sh"\n{snippet}'],
                              env=self._env(env), capture_output=True, text=True, timeout=20, check=False)

    def start(self, script, *args, env=None):
        proc = subprocess.Popen([BASH, str(script), *args], env=self._env(env),
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
                                start_new_session=True)
        self._procs.append(proc)
        return proc

    def stop(self, proc, sig=signal.SIGTERM, timeout=15):
        proc.send_signal(sig)
        out, err = proc.communicate(timeout=timeout)
        return proc.returncode, out, err

    def _cleanup(self):
        for proc in self._procs:
            try:
                os.killpg(proc.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            proc.communicate()

    def log_path(self, name):
        return self.logdir / name

    def read_log(self, name):
        path = self.log_path(name)
        return path.read_text() if path.exists() else ""

    def wait_for(self, predicate, timeout=10):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if predicate():
                return
            time.sleep(0.05)
        raise AssertionError("condition not met within %ss" % timeout)
