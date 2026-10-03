"""Syntax-check the panel's ES modules with Node when it is available."""

import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
STATIC = REPO / "internal/panel/static"
NODE = shutil.which("node")


@unittest.skipUnless(NODE, "needs node")
class PanelModulesTest(unittest.TestCase):
    def test_modules_parse(self):
        files = sorted(STATIC.rglob("*.js"))
        self.assertTrue(files)
        with tempfile.TemporaryDirectory() as tmp:
            for f in files:
                # .mjs makes Node parse the file as a module.
                copy = Path(tmp) / (f.relative_to(STATIC).as_posix().replace("/", "__") + ".mjs")
                copy.write_text(f.read_text())
                r = subprocess.run([NODE, "--check", str(copy)], capture_output=True, text=True, check=False)
                self.assertEqual(r.returncode, 0, f"{f.relative_to(REPO)}: {r.stderr}")


if __name__ == "__main__":
    unittest.main()
