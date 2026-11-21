package cli

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berth-ai/berth/internal/agentstore"
	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/orcastore"
)

const orcaDeb = "fake deb\n"

// orcaConfig turns the Orca track on against fake releases 1.0.0, pinned
// by the image, and 2.0.0, the latest. Orca itself stays off.
func orcaConfig(t *testing.T) (Config, map[string]int) {
	t.Helper()
	cfg := testConfig(t)
	dir := t.TempDir()
	hits := map[string]int{}
	sum := sha512.Sum512([]byte(orcaDeb))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits[r.URL.Path]++
		switch r.URL.Path {
		case "/download/v1.0.0/orca-ide_1.0.0_amd64.deb", "/download/v2.0.0/orca-ide_2.0.0_amd64.deb":
			io.WriteString(w, orcaDeb)
		case "/download/v2.0.0/latest-linux.yml", "/latest/download/latest-linux.yml":
			io.WriteString(w, "version: 2.0.0\nfiles:\n  - url: orca-ide_2.0.0_amd64.deb\n    sha512: "+
				base64.StdEncoding.EncodeToString(sum[:])+"\n")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	dpkg := filepath.Join(dir, "dpkg-deb")
	os.WriteFile(dpkg, []byte("#!/bin/sh\nset -eu\nmkdir -p \"$3/opt/Orca/resources/bin\"\n"+
		"printf '#!/bin/sh\\n' >\"$3/opt/Orca/resources/bin/orca-ide\"\nchmod 0755 \"$3/opt/Orca/resources/bin/orca-ide\"\n"), 0o755)
	cfg.OrcaDir = filepath.Join(cfg.AgentsDir, "orca")
	cfg.OrcaPin = filepath.Join(dir, "orca.json")
	writeOrcaPin(t, cfg, "1.0.0")
	cfg.OrcaReleases = srv.URL
	cfg.DpkgDeb = dpkg
	cfg.Arch = "amd64"
	cfg.ConfigDir = filepath.Join(dir, "berth")
	return cfg, hits
}

func writeOrcaPin(t *testing.T, cfg Config, version string) {
	t.Helper()
	sum := sha256.Sum256([]byte(orcaDeb))
	raw, _ := json.Marshal(orcastore.Pin{Version: version, SHA256: map[string]string{"amd64": hex.EncodeToString(sum[:])}})
	if err := os.WriteFile(cfg.OrcaPin, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func enableOrca(t *testing.T, cfg Config) {
	t.Helper()
	if err := os.MkdirAll(cfg.ConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := (&config.Store{Dir: cfg.ConfigDir}).Apply(json.RawMessage(`{"frontends":{"orca":{"enabled":true}}}`), 0); err != nil {
		t.Fatal(err)
	}
}

// orcaState is what status says of Orca: the pin and the current version.
func orcaState(t *testing.T, cfg Config) (pin map[string]any, current any) {
	t.Helper()
	out, _, code := run(t, cfg, "status", "--json")
	if code != 0 {
		t.Fatalf("status: %d %s", code, out)
	}
	var rep struct {
		Pins []map[string]any `json:"pins"`
		Orca *struct {
			Current any `json:"current"`
		} `json:"orca"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil || rep.Orca == nil {
		t.Fatalf("status: %v %s", err, out)
	}
	for _, p := range rep.Pins {
		if p["track"] == "orca" {
			return p, rep.Orca.Current
		}
	}
	t.Fatalf("no orca pin: %s", out)
	return nil, nil
}

func orcaCurrent(t *testing.T, cfg Config) string {
	t.Helper()
	cur, err := agentstore.Store{Root: cfg.OrcaDir}.Current()
	if err != nil {
		t.Fatal(err)
	}
	if cur == nil {
		return ""
	}
	return cur.Manifest.Packages["orca"]
}

func TestOrcaOffInstallsNothingAtBoot(t *testing.T) {
	cfg, hits := orcaConfig(t)
	out, errOut, code := run(t, cfg, "ensure", "--json")
	if code != 0 || !strings.Contains(out, `"orca": {`) || !strings.Contains(out, `"reason": "off"`) {
		t.Fatalf("ensure: %d %s %s", code, out, errOut)
	}
	if orcaCurrent(t, cfg) != "" || len(hits) != 0 {
		t.Fatalf("Orca off must download nothing: %v", hits)
	}
	pin, cur := orcaState(t, cfg)
	if pin["name"] != "orca" || pin["kind"] != "frontend" || pin["image"] != "1.0.0" || pin["current"] != "1.0.0" || cur != nil {
		t.Errorf("pin %v, current %v", pin, cur)
	}
}

func TestOrcaFollowsTheImageAndOverrides(t *testing.T) {
	cfg, hits := orcaConfig(t)
	enableOrca(t, cfg)
	if out, errOut, code := run(t, cfg, "ensure", "--json"); code != 0 || !strings.Contains(out, `"reason": "release-changed"`) {
		t.Fatalf("ensure: %d %s %s", code, out, errOut)
	}
	if got := orcaCurrent(t, cfg); got != "1.0.0" {
		t.Fatalf("after boot: %q", got)
	}
	if hits["/download/v1.0.0/latest-linux.yml"] != 0 {
		t.Error("the image's release must not need a manifest")
	}

	// berth update NAME moves Orca to the latest release.
	out, errOut, code := run(t, cfg, "update", "orca", "--json")
	if code != 0 {
		t.Fatalf("update: %d %s %s", code, out, errOut)
	}
	var res updateResult
	if err := json.Unmarshal([]byte(out), &res); err != nil || res.Orca == nil || !res.Orca.Changed || !res.Orca.Installed {
		t.Fatalf("update: %v %s", err, out)
	}
	if got := orcaCurrent(t, cfg); got != "2.0.0" {
		t.Fatalf("after update: %q", got)
	}
	if pin, _ := orcaState(t, cfg); pin["image"] != "1.0.0" || pin["current"] != "2.0.0" {
		t.Errorf("pin %v", pin)
	}
	// A restart keeps the override.
	if out, _, code := run(t, cfg, "ensure", "--json"); code != 0 || orcaCurrent(t, cfg) != "2.0.0" {
		t.Fatalf("ensure kept: %d %s", code, out)
	}

	if out, _, code := run(t, cfg, "rollback", "--orca", "--json"); code != 0 || orcaCurrent(t, cfg) != "1.0.0" {
		t.Fatalf("rollback: %d %s", code, out)
	}
	st, _ := agentstore.Store{Root: cfg.OrcaDir}.ReadState()
	if !st.FollowImage || len(st.Overrides) != 0 {
		t.Errorf("rolled back to the image's release: %+v", st)
	}

	// A newer image that pins the override's release drops it.
	if _, _, code := run(t, cfg, "update", "orca@2.0.0", "--json"); code != 0 {
		t.Fatal("update orca@2.0.0")
	}
	writeOrcaPin(t, cfg, "2.0.0")
	out, _, code = run(t, cfg, "ensure", "--json")
	if code != 0 || !strings.Contains(out, `"dropped": [
      "orca"
    ]`) {
		t.Fatalf("ensure after a newer image: %d %s", code, out)
	}
	if st, _ := (agentstore.Store{Root: cfg.OrcaDir}).ReadState(); !st.FollowImage || orcaCurrent(t, cfg) != "2.0.0" {
		t.Errorf("state %+v, current %q", st, orcaCurrent(t, cfg))
	}
}

func TestOrcaOverrideWaitsWhileOff(t *testing.T) {
	cfg, hits := orcaConfig(t)
	if out, errOut, code := run(t, cfg, "update", "orca@2.0.0", "--json"); code != 0 {
		t.Fatalf("update: %d %s %s", code, out, errOut)
	}
	if orcaCurrent(t, cfg) != "" || hits["/download/v2.0.0/orca-ide_2.0.0_amd64.deb"] != 0 {
		t.Fatal("Orca off must record the pick without downloading it")
	}
	if pin, _ := orcaState(t, cfg); pin["current"] != "2.0.0" {
		t.Errorf("pin %v", pin)
	}
	// The broker runs berth orca install when Orca is switched on.
	out, errOut, code := run(t, cfg, "orca", "install", "--json")
	if code != 0 || orcaCurrent(t, cfg) != "2.0.0" {
		t.Fatalf("orca install: %d %s %s", code, out, errOut)
	}
}

func TestOrcaTrackOffWithoutAPin(t *testing.T) {
	cfg, _ := orcaConfig(t)
	cfg.Arch = "arm64" // the pin has no checksum for it
	out, _, code := run(t, cfg, "status", "--json")
	if code != 0 || strings.Contains(out, `"track": "orca"`) || strings.Contains(out, `"orca":`) {
		t.Fatalf("status: %d %s", code, out)
	}
	if out, _, code := run(t, cfg, "rollback", "--orca", "--json"); code != 2 {
		t.Errorf("rollback --orca: %d %s", code, out)
	}
	if out, _, code := run(t, cfg, "orca", "install", "--json"); code != 1 || !strings.Contains(out, "orca_unsupported") {
		t.Errorf("orca install: %d %s", code, out)
	}
}

func TestOrcaGCKeepsCurrentAndPrevious(t *testing.T) {
	cfg, _ := orcaConfig(t)
	enableOrca(t, cfg)
	if out, _, code := run(t, cfg, "ensure", "--json"); code != 0 {
		t.Fatalf("ensure: %d %s", code, out)
	}
	// An older complete version beside current: agent sets keep a few of
	// those, Orca keeps none.
	s := agentstore.Store{Root: cfg.OrcaDir}
	old := s.VersionDir("aaaaaaaaaaaa")
	os.MkdirAll(old, 0o755)
	os.WriteFile(filepath.Join(old, "source.json"), []byte(`{"origin":"image"}`), 0o644)
	os.WriteFile(filepath.Join(old, ".complete"), []byte(`{"packages":{"orca":"0.9.0"},"installed_at":"2026-01-01T00:00:00Z"}`), 0o644)
	if out, _, code := run(t, cfg, "update", "orca@2.0.0", "--json"); code != 0 {
		t.Fatalf("update: %d %s", code, out)
	}
	list, err := s.List()
	if err != nil || len(list) != 2 {
		t.Fatalf("versions = %v %v", list, err)
	}
	if _, err := s.Get("aaaaaaaaaaaa"); err == nil {
		t.Error("the older version survived gc")
	}
}
