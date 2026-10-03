"""Syntax-check the panel's ES modules with Node when it is available."""

import re
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


IMPORT = re.compile(r"^import \{([^}]+)\} from \"[^\"]+\";$", re.M)


class PanelImportsTest(unittest.TestCase):
    def test_no_unused_imports(self):
        for f in sorted(STATIC.rglob("*.js")):
            src = f.read_text()
            body = IMPORT.sub("", src)
            for group in IMPORT.findall(src):
                for name in (n.strip().split(" as ")[-1] for n in group.split(",")):
                    if name and not re.search(rf"\b{re.escape(name)}\b", body):
                        self.fail(f"{f.relative_to(REPO)} imports {name} but never uses it")

    def test_no_void_leftovers(self):
        for f in sorted(STATIC.rglob("*.js")):
            self.assertNotRegex(f.read_text(), r"(?m)^\s*void \w+;$", f"{f.relative_to(REPO)} keeps a void leftover")


if __name__ == "__main__":
    unittest.main()
