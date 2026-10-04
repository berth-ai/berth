import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
ETC = REPO / "rootfs/etc/berth"
ZSH = shutil.which("zsh")


@unittest.skipUnless(ZSH, "needs zsh")
class ShellDefaultsTest(unittest.TestCase):
    def setUp(self):
        self.home = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.home)

    def zsh(self, script):
        env = {"HOME": str(self.home), "PATH": "/usr/bin:/bin", "BERTH_ETC": str(ETC), "TERM": "dumb"}
        r = subprocess.run([ZSH, "-f", "-c", script], env=env, capture_output=True, text=True, check=False)
        self.assertEqual(r.returncode, 0, r.stderr)
        return r.stdout

    def test_every_zsh_file_parses(self):
        for f in (ETC / "zshenv", ETC / "zshrc", ETC / "defaults/zshrc", REPO / "rootfs/etc/skel/.zshenv"):
            r = subprocess.run([ZSH, "-n", str(f)], capture_output=True, text=True, check=False)
            self.assertEqual(r.returncode, 0, f"{f}: {r.stderr}")

    def test_path_order(self):
        path = self.zsh('source "$BERTH_ETC/zshenv"; print -l $path').splitlines()
        self.assertEqual(path[:2], ["/var/lib/berth/bin", "/opt/mise/wrappers"])
        shims = str(self.home / ".local/share/mise/shims")
        self.assertEqual(path[2], shims)
        self.assertGreater(path.index(str(self.home / ".local/bin")), path.index("/usr/bin"))

    def test_new_shim_is_found_at_the_next_prompt(self):
        shims = self.home / ".local/share/mise/shims"
        tool = shims / "python3"
        out = self.zsh(
            'source "$BERTH_ETC/zshrc"; print -r -- $commands[python3]; '
            f'mkdir -p {shims}; print "#!/bin/sh" > {tool}; chmod +x {tool}; '
            'print -r -- $commands[python3]; _berth_rehash; print -r -- $commands[python3]'
        ).splitlines()
        self.assertNotEqual(out[0], str(tool))
        self.assertEqual(out[1], out[0])  # zsh still has the old lookup cached
        self.assertEqual(out[2], str(tool))

    def test_defaults_load_without_user_zshrc(self):
        out = self.zsh('source "$BERTH_ETC/zshrc"; print -r -- "$HISTFILE|$STARSHIP_CONFIG"')
        self.assertEqual(out.strip(), f"{self.home}/.local/state/zsh/history|{ETC}/defaults/starship.toml")

    def test_defaults_do_not_shadow_ls_or_cat(self):
        fakebin = self.home / "fakebin"
        fakebin.mkdir()
        for tool in ("eza", "bat"):
            (fakebin / tool).write_text("#!/bin/sh\n")
            (fakebin / tool).chmod(0o755)
        out = self.zsh(f'path=({fakebin} $path); source "$BERTH_ETC/defaults/zshrc"; '
                       'print -r -- "${aliases[ls]-none}|${aliases[cat]-none}|${aliases[ll]-none}"')
        self.assertEqual(out.strip(), "none|none|eza -l --git")

    def test_user_zshrc_replaces_defaults(self):
        (self.home / ".zshrc").write_text("# mine\n")
        out = self.zsh('source "$BERTH_ETC/zshrc"; print -r -- "[$HISTFILE]"')
        self.assertEqual(out.strip(), "[]")

    def test_user_starship_config_wins(self):
        (self.home / ".config").mkdir()
        (self.home / ".config/starship.toml").write_text("")
        out = self.zsh('source "$BERTH_ETC/zshrc"; print -r -- "[${STARSHIP_CONFIG-}]"')
        self.assertEqual(out.strip(), "[]")


class TmuxAndNvimHookTest(unittest.TestCase):
    def test_tmux_hook_defers_to_user_files(self):
        hook = (REPO / "rootfs/etc/tmux.conf").read_text()
        self.assertIn("$HOME/.tmux.conf", hook)
        self.assertIn("tmux/tmux.conf", hook)
        self.assertIn("source-file /etc/berth/defaults/tmux.conf", hook)

    def test_tmux_defaults_load_vendored_plugins(self):
        conf = (REPO / "rootfs/etc/berth/defaults/tmux.conf").read_text()
        for plugin in ("tmux-resurrect/resurrect.tmux", "tmux-continuum/continuum.tmux"):
            self.assertIn(f"/usr/share/berth/tmux-plugins/{plugin}", conf)

    def test_nvim_sysinit_defers_to_user_init(self):
        sysinit = (REPO / "rootfs/etc/berth/defaults/xdg/nvim/sysinit.vim").read_text()
        self.assertIn("init.lua", sysinit)
        self.assertIn("init.vim", sysinit)
        self.assertIn("/etc/berth/defaults/nvim/init.lua", sysinit)


if __name__ == "__main__":
    unittest.main()
