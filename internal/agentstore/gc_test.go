package agentstore

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/salgnt/berth/internal/testutil"
)

func hashN(i int) string { return string(rune('a'+i)) + "00000000000" }

func TestGCRetention(t *testing.T) {
	s := newStore(t)
	for i := range 6 { // a is oldest, f newest
		testutil.MakeVersion(t, s.Root, hashN(i), "image", t0.Add(time.Duration(i)*time.Hour))
	}
	s.Flip(hashN(0)) // current and previous are the two oldest
	s.Flip(hashN(1))
	os.MkdirAll(s.VersionDir("eeeeeeeeeeee")+".tmp", 0o755)
	res, err := s.GC(2, nil)
	if err != nil {
		t.Fatal(err)
	}
	// kept: current b, previous a, newest two f, e; removed d, c and the tmp dir
	if !slices.Equal(res.Kept, []string{hashN(5), hashN(4), hashN(1), hashN(0)}) {
		t.Fatalf("kept %v", res.Kept)
	}
	if !slices.Equal(res.Removed, []string{hashN(3), hashN(2), "eeeeeeeeeeee.tmp"}) {
		t.Fatalf("removed %v", res.Removed)
	}
}

func TestGCSkipsVersionsInUse(t *testing.T) {
	sources := map[string]func(proc, dir string){
		"exe": func(p, d string) { os.Symlink(filepath.Join(d, "ms-playwright/chrome"), filepath.Join(p, "exe")) },
		"cwd": func(p, d string) { os.Symlink(d, filepath.Join(p, "cwd")) },
		"maps": func(p, d string) {
			os.WriteFile(filepath.Join(p, "maps"), []byte("7f00-7f01 r-xp 0 08:01 1 "+d+"/lib.so\n"), 0o644)
		},
		"cmdline": func(p, d string) {
			os.WriteFile(filepath.Join(p, "cmdline"), []byte("node\x00"+d+"/node_modules/x/cli.js\x00"), 0o644)
		},
		"environ": func(p, d string) {
			os.WriteFile(filepath.Join(p, "environ"), []byte("A=1\x00PLAYWRIGHT_BROWSERS_PATH="+d+"/ms-playwright\x00"), 0o644)
		},
	}
	for name, plant := range sources {
		t.Run(name, func(t *testing.T) {
			s := newStore(t)
			dir := testutil.MakeVersion(t, s.Root, hashA, "image", t0)
			testutil.MakeVersion(t, s.Root, hashB, "image", t0.Add(time.Hour))
			s.Flip(hashB)
			proc := filepath.Join(t.TempDir(), "proc")
			pid := filepath.Join(proc, "4242")
			os.MkdirAll(pid, 0o755)
			os.MkdirAll(filepath.Join(proc, "self-not-a-pid"), 0o755)
			plant(pid, dir)
			res, err := s.GC(0, InUsePaths(proc))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(res.InUse, []string{hashA}) || len(res.Removed) != 0 {
				t.Fatalf("result %+v", res)
			}
		})
	}
}

func TestGCEmptyStore(t *testing.T) {
	res, err := newStore(t).GC(3, nil)
	if err != nil || res.Removed == nil || res.Kept == nil || res.InUse == nil {
		t.Fatalf("%+v %v", res, err)
	}
}
