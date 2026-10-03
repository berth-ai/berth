package agentstore

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berth-ai/berth/internal/locksource"
	"github.com/berth-ai/berth/internal/runner"
	"github.com/berth-ai/berth/internal/smoke"
	"github.com/berth-ai/berth/internal/testutil"
)

func TestMain(m *testing.M) {
	testutil.RunFakeToolIfRequested()
	os.Exit(m.Run())
}

func installer(s Store) Installer {
	r := runner.Exec{Stdout: io.Discard, Stderr: io.Discard}
	return Installer{
		Store:  s,
		Runner: r,
		Smoke:  smoke.Checker{Runner: r, Timeout: 30 * time.Second}.Check,
		Now:    func() time.Time { return t0 },
	}
}

func fixture(t *testing.T, name string) *locksource.Lock {
	t.Helper()
	l, err := locksource.FromDir(testutil.LockDir(name), locksource.Source{Origin: locksource.OriginDir, FetchedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func assertClean(t *testing.T, s Store, hash string) {
	t.Helper()
	for _, p := range []string{s.VersionDir(hash), s.VersionDir(hash) + ".tmp"} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s left behind", p)
		}
	}
}

func TestInstallFresh(t *testing.T) {
	testutil.FakeTools(t)
	s := newStore(t)
	lock := fixture(t, "lock-v1")
	v, installed, err := installer(s).Install(context.Background(), lock)
	if err != nil {
		t.Fatal(err)
	}
	if !installed || v.Hash != lock.Hash || v.Manifest.Packages["fake-agent"] != "1.0.0" ||
		v.Manifest.Packages["playwright"] != "1.0.0" || !v.Manifest.InstalledAt.Equal(t0) {
		t.Fatalf("unexpected version %+v", v)
	}
	for _, p := range []string{"bin/fake-agent", "bin/playwright", "ms-playwright/chromium-fake", "source.json"} {
		if _, err := os.Stat(filepath.Join(v.Dir, p)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(v.Dir, ".npm-cache")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("npm cache must not survive the install")
	}
	info, _ := os.Stat(filepath.Join(v.Dir, "bin", "fake-agent"))
	if info.Mode().Perm()&0o022 != 0 {
		t.Fatalf("wrapper is group/other writable: %v", info.Mode())
	}
	if _, err := os.Stat(s.VersionDir(lock.Hash) + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("tmp dir left behind")
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	testutil.FakeTools(t)
	s := newStore(t)
	lock := fixture(t, "lock-v1")
	installer(s).Install(context.Background(), lock)
	t.Setenv("BERTH_FAKE_NPM_FAIL", "1") // a second install must not run npm at all
	if _, installed, err := installer(s).Install(context.Background(), lock); err != nil || installed {
		t.Fatalf("second install: installed=%v err=%v", installed, err)
	}
}

func TestInstallFailuresCleanUp(t *testing.T) {
	cases := map[string]struct{ env, step string }{
		"npm fails":        {"BERTH_FAKE_NPM_FAIL", "install"},
		"browser download": {"BERTH_FAKE_PLAYWRIGHT_FAIL", "install"},
		"chromium launch":  {"BERTH_FAKE_CHROMIUM_FAIL", "smoke"},
		"duplicate core":   {"BERTH_FAKE_DUPLICATE_CORE", "smoke"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			testutil.FakeTools(t)
			t.Setenv(c.env, "1")
			s := newStore(t)
			lock := fixture(t, "lock-v1")
			_, _, err := installer(s).Install(context.Background(), lock)
			var se *StepError
			if !errors.As(err, &se) || se.Step != c.step {
				t.Fatalf("err = %v, want step %s", err, c.step)
			}
			assertClean(t, s, lock.Hash)
		})
	}
}

func TestInstallRequiresPlaywright(t *testing.T) {
	testutil.FakeTools(t)
	s := newStore(t)
	pkg := []byte(`{"dependencies":{"fake-agent":"1.0.0"}}`)
	lockJSON := []byte(`{"lockfileVersion":3,"packages":{"":{"dependencies":{"fake-agent":"1.0.0"}}}}`)
	lock, err := locksource.Parse(pkg, lockJSON, locksource.Source{Origin: locksource.OriginDir})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = installer(s).Install(context.Background(), lock)
	var se *StepError
	if !errors.As(err, &se) || se.Step != "install" || !strings.Contains(err.Error(), "node_modules/playwright") {
		t.Fatalf("err = %v", err)
	}
	assertClean(t, s, lock.Hash)
}

func TestInstallRecoversFromPartialInstall(t *testing.T) {
	testutil.FakeTools(t)
	s := newStore(t)
	lock := fixture(t, "lock-v1")
	for _, dir := range []string{s.VersionDir(lock.Hash) + ".tmp", s.VersionDir(lock.Hash)} {
		os.MkdirAll(filepath.Join(dir, "node_modules", "junk"), 0o755)
	}
	v, installed, err := installer(s).Install(context.Background(), lock)
	if err != nil || !installed {
		t.Fatalf("installed=%v err=%v", installed, err)
	}
	if _, err := os.Stat(filepath.Join(v.Dir, "node_modules", "junk")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("partial tree was reused")
	}
}
