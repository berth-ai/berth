package orcastore

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/berth-ai/berth/internal/agentstore"
	"github.com/berth-ai/berth/internal/locksource"
	"github.com/berth-ai/berth/internal/runner"
	"github.com/berth-ai/berth/internal/upstream"
)

var now = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

// fakeDpkgDeb unpacks nothing: it lays out Orca's /opt/Orca in the target
// of dpkg-deb -x DEB DIR, with a setuid sandbox as the package's postinst
// would leave it.
const fakeDpkgDeb = `#!/bin/sh
set -eu
[ "$1" = -x ] || exit 2
grep -q 'fake deb' "$2"
mkdir -p "$3/opt/Orca/resources/bin" "$3/usr/bin"
printf '#!/bin/sh\necho orca "$@"\n' >"$3/opt/Orca/resources/bin/orca-ide"
chmod 0777 "$3/opt/Orca/resources/bin/orca-ide"
: >"$3/opt/Orca/chrome-sandbox"
chmod 4755 "$3/opt/Orca/chrome-sandbox"
`

type fixture struct {
	srv  *httptest.Server
	hits map[string]int
	deb  []byte
}

// newFixture serves Orca releases: version 1.0.0's and 2.0.0's .debs and
// update manifests, with 2.0.0 the latest.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{hits: map[string]int{}, deb: []byte("fake deb\n")}
	sum := sha512.Sum512(f.deb)
	manifest := func(v string) string {
		return "version: " + v + "\nfiles:\n  - url: orca-linux.AppImage\n    sha512: bm9wZQ==\n    size: 1\n" +
			"  - url: orca-ide_" + v + "_amd64.deb\n    sha512: " + base64.StdEncoding.EncodeToString(sum[:]) +
			"\n    size: 9\npath: orca-linux.AppImage\nsha512: bm9wZQ==\nreleaseDate: '2026-11-09T10:56:23.532Z'\n"
	}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits[r.URL.Path]++
		switch r.URL.Path {
		case "/download/v1.0.0/orca-ide_1.0.0_amd64.deb", "/download/v2.0.0/orca-ide_2.0.0_amd64.deb":
			w.Write(f.deb)
		case "/download/v2.0.0/latest-linux.yml", "/latest/download/latest-linux.yml":
			io.WriteString(w, manifest("2.0.0"))
		case "/api":
			io.WriteString(w, `[{"tag_name":"v2.0.0"},{"tag_name":"v2.1.0-rc.1","prerelease":true},`+
				`{"tag_name":"v1.10.0"},{"tag_name":"v1.9.0","draft":true},{"tag_name":"v1.0.0"}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fixture) installer(t *testing.T) Installer {
	t.Helper()
	dir := t.TempDir()
	dpkg := filepath.Join(dir, "dpkg-deb")
	if err := os.WriteFile(dpkg, []byte(fakeDpkgDeb), 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(f.deb)
	return Installer{
		Store:    agentstore.Store{Root: filepath.Join(dir, "orca")},
		Pin:      &Pin{Version: "1.0.0", SHA256: map[string]string{"amd64": hex.EncodeToString(sum[:])}},
		Arch:     "amd64",
		Releases: f.srv.URL,
		Runner:   runner.Exec{Stdout: io.Discard, Stderr: io.Discard},
		DpkgDeb:  dpkg,
		Now:      func() time.Time { return now },
	}
}

func TestReadPin(t *testing.T) {
	dir := t.TempDir()
	if p, err := ReadPin(filepath.Join(dir, "missing.json")); p != nil || err != nil {
		t.Fatalf("missing: %v %v", p, err)
	}
	path := filepath.Join(dir, "orca.json")
	os.WriteFile(path, []byte(`{"version":"1.4.255","sha256":{"amd64":"`+strings.Repeat("a", 64)+`","arm64":"short"}}`), 0o644)
	p, err := ReadPin(path)
	if err != nil || p.Version != "1.4.255" {
		t.Fatalf("%v %v", p, err)
	}
	if !p.Supports("amd64") || p.Supports("arm64") || p.Supports("riscv64") {
		t.Errorf("supports: %v %v %v", p.Supports("amd64"), p.Supports("arm64"), p.Supports("riscv64"))
	}
	var none *Pin
	if none.Supports("amd64") {
		t.Error("a nil pin supports nothing")
	}
	os.WriteFile(path, []byte(`{"version":"--force"}`), 0o644)
	if _, err := ReadPin(path); err == nil {
		t.Error("an invalid version must fail")
	}
}

func TestHashNamesVersionAndArch(t *testing.T) {
	a, b, c := Hash("1.0.0", "amd64"), Hash("1.0.0", "arm64"), Hash("1.0.1", "amd64")
	if a == b || a == c || len(a) != 12 {
		t.Fatalf("%s %s %s", a, b, c)
	}
}

func TestParseManifest(t *testing.T) {
	raw := []byte("version: 1.4.255\nfiles:\n  - url: orca-linux.AppImage\n    sha512: qWVI==\n    size: 255127258\n" +
		"    blockMapSize: 267486\n  - url: orca-ide_1.4.255_amd64.deb\n    sha512: VCkd==\n    size: 191836440\n" +
		"path: orca-linux.AppImage\nsha512: qWVI==\nreleaseDate: '2026-11-09T10:56:23.532Z'\n")
	m := ParseManifest(raw)
	want := &Manifest{Version: "1.4.255", SHA512: map[string]string{"orca-linux.AppImage": "qWVI==", "orca-ide_1.4.255_amd64.deb": "VCkd=="}}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("%+v", m)
	}
}

func TestInstallsThePinnedRelease(t *testing.T) {
	f := newFixture(t)
	in := f.installer(t)
	v, installed, err := in.Install(context.Background(), "1.0.0")
	if err != nil || !installed {
		t.Fatalf("%v %v", installed, err)
	}
	if v.Hash != Hash("1.0.0", "amd64") || v.Manifest.Packages[Name] != "1.0.0" || !v.Manifest.InstalledAt.Equal(now) {
		t.Errorf("version = %+v", v)
	}
	if v.Source.Origin != locksource.OriginImage {
		t.Errorf("origin = %q", v.Source.Origin)
	}
	if f.hits["/download/v1.0.0/latest-linux.yml"] != 0 {
		t.Error("the pinned release must be checked against the image's sha256, not the release manifest")
	}
	cli, err := os.Stat(filepath.Join(v.Dir, CLI))
	if err != nil || cli.Mode().Perm() != 0o755 {
		t.Errorf("cli: %v %v", cli, err)
	}
	sandbox, err := os.Stat(filepath.Join(v.Dir, AppDir, "chrome-sandbox"))
	if err != nil || sandbox.Mode()&os.ModeSetuid != 0 {
		t.Errorf("sandbox keeps setuid: %v %v", sandbox.Mode(), err)
	}
	for _, gone := range []string{"orca.deb", "deb"} {
		if _, err := os.Stat(filepath.Join(v.Dir, gone)); err == nil {
			t.Errorf("%s left in the version", gone)
		}
	}
	if _, installed, err := in.Install(context.Background(), "1.0.0"); err != nil || installed {
		t.Errorf("second install: %v %v", installed, err)
	}
	if n := f.hits["/download/v1.0.0/orca-ide_1.0.0_amd64.deb"]; n != 1 {
		t.Errorf("downloads = %d", n)
	}
}

func TestInstallsAnotherReleaseAgainstItsManifest(t *testing.T) {
	f := newFixture(t)
	in := f.installer(t)
	v, _, err := in.Install(context.Background(), "2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if v.Source.Origin != locksource.OriginUpstream || f.hits["/download/v2.0.0/latest-linux.yml"] != 1 {
		t.Errorf("origin %q, manifest fetched %d times", v.Source.Origin, f.hits["/download/v2.0.0/latest-linux.yml"])
	}
}

func TestInstallFailuresLeaveNothing(t *testing.T) {
	cases := map[string]func(*fixture, *Installer) string{
		"pinned checksum mismatch": func(_ *fixture, in *Installer) string {
			in.Pin.SHA256["amd64"] = strings.Repeat("0", 64)
			return "1.0.0"
		},
		"manifest checksum mismatch": func(f *fixture, _ *Installer) string {
			f.deb = []byte("fake deb, tampered\n")
			return "2.0.0"
		},
		"no such release": func(*fixture, *Installer) string { return "3.0.0" },
		"no pinned arch":  func(_ *fixture, in *Installer) string { in.Arch = "arm64"; return "1.0.0" },
		"not a package": func(f *fixture, _ *Installer) string {
			// The manifest names the garbage, so only dpkg-deb refuses it.
			f.deb = []byte("garbage\n")
			sum := sha512.Sum512(f.deb)
			f.srv.Config.Handler = manifestFor(f, "2.0.0", sum[:])
			return "2.0.0"
		},
		"invalid version":  func(*fixture, *Installer) string { return "../1.0.0" },
		"missing dpkg-deb": func(_ *fixture, in *Installer) string { in.DpkgDeb = "/nonexistent/dpkg-deb"; return "1.0.0" },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			in := f.installer(t)
			version := setup(f, &in)
			_, _, err := in.Install(context.Background(), version)
			var step *agentstore.StepError
			if !errors.As(err, &step) {
				t.Fatalf("err = %v", err)
			}
			entries, _ := os.ReadDir(filepath.Join(in.Store.Root, "versions"))
			if len(entries) != 0 {
				t.Errorf("left %v", entries)
			}
		})
	}
}

// manifestFor serves version's .deb as f.deb and a manifest naming sum.
func manifestFor(f *fixture, version string, sum []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/download/v" + version + "/orca-ide_" + version + "_amd64.deb":
			w.Write(f.deb)
		case "/download/v" + version + "/latest-linux.yml":
			io.WriteString(w, "version: "+version+"\nfiles:\n  - url: orca-ide_"+version+"_amd64.deb\n    sha512: "+
				base64.StdEncoding.EncodeToString(sum)+"\n")
		default:
			http.NotFound(w, r)
		}
	})
}

func TestLatestAndVersions(t *testing.T) {
	f := newFixture(t)
	in := f.installer(t)
	if v, err := in.Latest(context.Background()); err != nil || v != "2.0.0" {
		t.Fatalf("latest: %q %v", v, err)
	}
	got, err := in.Versions(context.Background(), f.srv.URL+"/api")
	if err != nil || !reflect.DeepEqual(got, []string{"1.0.0", "1.10.0", "2.0.0"}) {
		t.Fatalf("versions: %v %v", got, err)
	}
	in.Releases = f.srv.URL + "/gone"
	if _, err := in.Latest(context.Background()); !errors.Is(err, upstream.ErrLookup) {
		t.Errorf("latest failure: %v", err)
	}
}

func TestManifestPerArch(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path
		io.WriteString(w, "version: 2.0.0\n")
	}))
	defer srv.Close()
	in := Installer{Arch: "arm64", Releases: srv.URL}
	if _, err := in.Latest(context.Background()); err != nil || asked != "/latest/download/latest-linux-arm64.yml" {
		t.Fatalf("%q %v", asked, err)
	}
}
