import hashlib
import json
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
BUILD = REPO / "build"


def sh(*args, **kw):
    return subprocess.run([str(a) for a in args], capture_output=True, text=True, check=False, **kw)


class BuildHelperTest(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.tmp)

    @unittest.skipUnless(shutil.which("sha256sum") and shutil.which("curl"), "needs sha256sum and curl")
    def test_fetch_artifact_verifies(self):
        src = self.tmp / "src.bin"
        src.write_bytes(b"berth")
        good = hashlib.sha256(b"berth").hexdigest()
        dest = self.tmp / "out.bin"
        r = sh(BUILD / "fetch-artifact.sh", src.as_uri(), good, dest)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(dest.read_bytes(), b"berth")

    @unittest.skipUnless(shutil.which("sha256sum") and shutil.which("curl"), "needs sha256sum and curl")
    def test_checksum_mismatch_fails(self):
        src = self.tmp / "src.bin"
        src.write_bytes(b"berth")
        dest = self.tmp / "out.bin"
        r = sh(BUILD / "fetch-artifact.sh", src.as_uri(), "0" * 64, dest)
        self.assertEqual(r.returncode, 1)
        self.assertIn("checksum mismatch", r.stderr)
        self.assertFalse(dest.exists())

    def test_mise_wrappers(self):
        a, b, out = self.tmp / "a/bin", self.tmp / "b/bin", self.tmp / "wrappers"
        a.mkdir(parents=True)
        b.mkdir(parents=True)
        for d, name in ((a, "rg"), (b, "fd")):
            (d / name).write_text("#!/bin/sh\necho %s \"$@\"\n" % name)
            (d / name).chmod(0o755)
        (a / "README").write_text("not executable")
        r = sh(BUILD / "mise-wrappers.sh", out, a, b)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(sorted(p.name for p in out.iterdir()), ["fd", "rg"])
        self.assertFalse((out / "rg").is_symlink())
        self.assertEqual(sh(out / "rg", "x").stdout, "rg x\n")

    def test_mise_wrappers_reject_clash(self):
        a, b = self.tmp / "a", self.tmp / "b"
        for d in (a, b):
            d.mkdir()
            (d / "tool").write_text("#!/bin/sh\n")
            (d / "tool").chmod(0o755)
        r = sh(BUILD / "mise-wrappers.sh", self.tmp / "w", a, b)
        self.assertEqual(r.returncode, 1)
        self.assertIn("tool", r.stderr)

    @unittest.skipUnless(shutil.which("jq") and shutil.which("sha256sum"), "needs jq and sha256sum")
    def test_write_build_info(self):
        lock = REPO / "agents/package-lock.json"
        out = self.tmp / "build-info.json"
        r = sh(BUILD / "write-build-info.sh", out, "1.0.0", "abc", "arm64", "sha256:00", "1.7.7", "4.139.1", lock, "1.4.245")
        self.assertEqual(r.returncode, 0, r.stderr)
        info = json.loads(out.read_text())
        self.assertEqual(info["seed_hash"], hashlib.sha256(lock.read_bytes()).hexdigest()[:12])
        self.assertEqual(info["arch"], "arm64")
        self.assertEqual(info["orca_version"], "1.4.245")
        self.assertEqual(set(info), {"version", "commit", "arch", "base_digest", "ttyd_version",
                                     "code_server_version", "orca_version", "seed_hash"})


if __name__ == "__main__":
    unittest.main()
