package broker

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/berth-ai/berth/internal/config"
)

func TestDesiredOrca(t *testing.T) {
	c := config.Defaults()
	c.Frontends.Orca.Enabled = true
	c.Runtime.TZ = "Europe/Rome"
	var names []string
	specs := Desired(c, ModeNormal)
	for _, s := range specs {
		names = append(names, s.Name)
	}
	if !slices.Equal(names, []string{"panel", "ttyd", "orca"}) {
		t.Fatalf("names = %v", names)
	}
	if specs[2].Env["TZ"] != "Europe/Rome" {
		t.Errorf("orca = %+v", specs[2])
	}
	if got := Desired(c, ModeRecovery); len(got) != 1 || got[0].Name != "panel" {
		t.Errorf("recovery = %+v", got)
	}
	c.Frontends.Orca.Enabled = false
	if got := Desired(c, ModeNormal); len(got) != 2 {
		t.Errorf("orca off = %+v", got)
	}
}

func TestOrcaAddress(t *testing.T) {
	for _, c := range []struct {
		bind, port, override, addr, warn string
		err                              error
	}{
		{"100.64.0.10", "6768", "", "100.64.0.10:6768", "", nil},
		{"100.64.0.10", "7000", "", "100.64.0.10:7000", "", nil},
		{"127.0.0.1", "6768", "", "127.0.0.1:6768", "BIND_ADDRESS", nil},
		{"::1", "6768", "", "[::1]:6768", "frontends.orca.pairing_address", nil},
		{"0.0.0.0", "6768", "", "", "", ErrOrcaAddressNeeded},
		{"::", "6768", "", "", "", ErrOrcaAddressNeeded},
		{"0.0.0.0", "6768", "berth.example", "berth.example", "", nil},
		{"127.0.0.1", "6768", "100.64.0.10:6768", "100.64.0.10:6768", "", nil},
	} {
		h := Host{BindAddress: c.bind, OrcaPort: c.port}
		addr, warn, err := h.OrcaAddress(c.override)
		if addr != c.addr || !errors.Is(err, c.err) || (c.warn == "") != (warn == "") || !strings.Contains(warn, c.warn) {
			t.Errorf("%s %s %q: %q %q %v", c.bind, c.port, c.override, addr, warn, err)
		}
	}
}

func TestHostOrcaPort(t *testing.T) {
	if got := HostFromEnv(func(string) string { return "" }).OrcaPort; got != "6768" {
		t.Errorf("default = %q", got)
	}
	if got := HostFromEnv(func(k string) string { return map[string]string{"BERTH_HOST_ORCA_PORT": "7000"}[k] }).OrcaPort; got != "7000" {
		t.Errorf("env = %q", got)
	}
}

func TestConfigApplyRefusesOrcaWhenNotInstalled(t *testing.T) {
	tb := newTestBroker(t)
	tb.b.Load()
	tb.b.OrcaInstalled = func() bool { return false }
	code, out := tb.verb(t, "config.apply", `{"base_revision":0,"patch":{"frontends":{"orca":{"enabled":true}}}}`)
	if code != http.StatusBadRequest || errCode(out) != "invalid_config" ||
		!strings.Contains(out["error"].(map[string]any)["message"].(string), "frontends.orca.enabled: Orca is not installed in this image") {
		t.Fatalf("%d %v", code, out)
	}
	// Other changes still apply while it stays off.
	if code, out := tb.verb(t, "config.apply", `{"base_revision":0,"patch":{"frontends":{"ide":{"enabled":true}}}}`); code != http.StatusOK {
		t.Fatalf("ide: %d %v", code, out)
	}
	tb.b.OrcaInstalled = nil
	if code, out := tb.verb(t, "config.apply", `{"base_revision":1,"patch":{"frontends":{"orca":{"enabled":true}}}}`); code != http.StatusOK {
		t.Fatalf("installed: %d %v", code, out)
	}
	if names := tb.svc.lastNames(); !slices.Contains(names, "orca") {
		t.Errorf("services = %v", names)
	}
}

// orcaBroker is a test broker with Orca on and a fake module: each restart
// reads and deletes the request file and answers with readiness(scope).
func orcaBroker(t *testing.T, bind string, readiness func(req orcaRequest) string) (*testBroker, *bytes.Buffer, *[]orcaRequest) {
	t.Helper()
	tb := newTestBroker(t)
	tb.b.Load()
	tb.b.Host.BindAddress = bind
	tb.b.OrcaRunDir = filepath.Join(t.TempDir(), "orca")
	tb.b.OrcaPairTimeout = 2 * time.Second
	log := &bytes.Buffer{}
	tb.b.Log = log
	if code, out := tb.verb(t, "config.apply", `{"base_revision":0,"patch":{"frontends":{"orca":{"enabled":true}}}}`); code != http.StatusOK {
		t.Fatalf("enable: %d %v", code, out)
	}
	var mu sync.Mutex
	reqs := &[]orcaRequest{}
	tb.svc.onRestart = func(name string) {
		if name != "orca" {
			return
		}
		path := filepath.Join(tb.b.OrcaRunDir, "pair-request.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			return // a start without a request
		}
		os.Remove(path)
		var req orcaRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Errorf("request %s: %v", raw, err)
		}
		mu.Lock()
		*reqs = append(*reqs, req)
		mu.Unlock()
		if out := readiness(req); out != "" {
			if err := os.WriteFile(filepath.Join(tb.b.OrcaRunDir, "readiness.json"), []byte(out), 0o600); err != nil {
				t.Error(err)
			}
		}
	}
	return tb, log, reqs
}

func readyWith(url string) func(orcaRequest) string {
	return func(orcaRequest) string {
		// Orca prints a log line first; the broker must skip it.
		return `{"event":"starting"}` + "\n" + `{"ready":true,"pairing":{"available":true,"url":"` + url + `"}}` + "\n"
	}
}

func assertRunDirEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("run dir holds %v", entries)
	}
}

func TestOrcaPairDesktop(t *testing.T) {
	tb, _, reqs := orcaBroker(t, "100.64.0.10", readyWith("orca://pair?code=x"))
	code, out := tb.verb(t, "orca.pair", `{"scope":"desktop"}`)
	if code != http.StatusOK || out["url"] != "orca://pair?code=x" || out["scope"] != "desktop" || out["address"] != "100.64.0.10:6768" {
		t.Fatalf("%d %v", code, out)
	}
	if _, warned := out["warning"]; warned {
		t.Errorf("warning = %v", out["warning"])
	}
	if qr, _ := out["qr"].([]any); len(qr) < 21 {
		t.Errorf("qr = %v", out["qr"])
	}
	if len(*reqs) != 1 || (*reqs)[0] != (orcaRequest{Scope: "desktop", Address: "100.64.0.10:6768"}) {
		t.Errorf("requests = %+v", *reqs)
	}
	assertRunDirEmpty(t, tb.b.OrcaRunDir)
	if info, err := os.Stat(tb.b.OrcaRunDir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("run dir: %v %v", info, err)
	}
}

func TestOrcaPairMobile(t *testing.T) {
	tb, _, reqs := orcaBroker(t, "100.64.0.10", readyWith("orca://pair?code=m"))
	if code, out := tb.verb(t, "orca.pair", `{"scope":"mobile"}`); code != http.StatusOK || out["scope"] != "mobile" {
		t.Fatalf("%d %v", code, out)
	}
	if len(*reqs) != 1 || (*reqs)[0].Scope != "mobile" {
		t.Errorf("requests = %+v", *reqs)
	}
}

func TestOrcaPairUsesOverride(t *testing.T) {
	tb, _, reqs := orcaBroker(t, "0.0.0.0", readyWith("orca://pair?code=o"))
	if code, out := tb.verb(t, "config.apply", `{"base_revision":1,"patch":{"frontends":{"orca":{"pairing_address":"berth.example"}}}}`); code != http.StatusOK {
		t.Fatalf("%d %v", code, out)
	}
	if code, out := tb.verb(t, "orca.pair", `{"scope":"desktop"}`); code != http.StatusOK || out["address"] != "berth.example" {
		t.Fatalf("%d %v", code, out)
	}
	if (*reqs)[0].Address != "berth.example" {
		t.Errorf("requests = %+v", *reqs)
	}
}

func TestOrcaPairLoopbackWarns(t *testing.T) {
	tb, _, _ := orcaBroker(t, "127.0.0.1", readyWith("orca://pair?code=l"))
	code, out := tb.verb(t, "orca.pair", `{"scope":"desktop"}`)
	if w, _ := out["warning"].(string); code != http.StatusOK || !strings.Contains(w, "BIND_ADDRESS") {
		t.Fatalf("%d %v", code, out)
	}
}

func TestOrcaPairDisabled(t *testing.T) {
	tb := newTestBroker(t)
	tb.b.Load()
	tb.b.OrcaRunDir = filepath.Join(t.TempDir(), "orca")
	code, out := tb.verb(t, "orca.pair", `{"scope":"desktop"}`)
	if code != http.StatusConflict || errCode(out) != "orca_disabled" || !strings.Contains(out["error"].(map[string]any)["message"].(string), "frontends.orca.enabled") {
		t.Fatalf("%d %v", code, out)
	}
	if len(tb.svc.restarts) != 0 {
		t.Errorf("restarts = %v", tb.svc.restarts)
	}
}

func TestOrcaPairWildcard(t *testing.T) {
	tb, _, reqs := orcaBroker(t, "0.0.0.0", readyWith("orca://pair?code=w"))
	if code, out := tb.verb(t, "orca.pair", `{"scope":"desktop"}`); code != http.StatusConflict || errCode(out) != "orca_address_needed" {
		t.Fatalf("%d %v", code, out)
	}
	if len(*reqs) != 0 || len(tb.svc.restarts) != 0 {
		t.Errorf("restarted: %v", tb.svc.restarts)
	}
}

func TestOrcaPairTimeoutCleansUp(t *testing.T) {
	tb, _, _ := orcaBroker(t, "100.64.0.10", func(orcaRequest) string { return "" })
	tb.svc.onRestart = nil // Orca never starts, so the request stays behind
	tb.b.OrcaPairTimeout = 50 * time.Millisecond
	if code, out := tb.verb(t, "orca.pair", `{"scope":"desktop"}`); code != http.StatusGatewayTimeout || errCode(out) != "orca_pair_timeout" {
		t.Fatalf("%d %v", code, out)
	}
	assertRunDirEmpty(t, tb.b.OrcaRunDir)
}

func TestOrcaPairUnavailable(t *testing.T) {
	tb, _, _ := orcaBroker(t, "100.64.0.10", func(orcaRequest) string {
		return `{"pairing":{"available":false,"reason":"no network","guidance":"check the address"}}`
	})
	code, out := tb.verb(t, "orca.pair", `{"scope":"desktop"}`)
	msg, _ := out["error"].(map[string]any)["message"].(string)
	if code != http.StatusBadGateway || errCode(out) != "orca_pairing_unavailable" || !strings.Contains(msg, "no network") || !strings.Contains(msg, "check the address") {
		t.Fatalf("%d %v", code, out)
	}
	assertRunDirEmpty(t, tb.b.OrcaRunDir)
}

func TestOrcaPairPrettyJSON(t *testing.T) {
	tb, _, _ := orcaBroker(t, "100.64.0.10", func(orcaRequest) string {
		return "{\n  \"pairing\": {\n    \"available\": true,\n    \"url\": \"orca://pair?code=p\"\n  }\n}\n"
	})
	if code, out := tb.verb(t, "orca.pair", `{"scope":"desktop"}`); code != http.StatusOK || out["url"] != "orca://pair?code=p" {
		t.Fatalf("%d %v", code, out)
	}
}

func TestOrcaPairSerialized(t *testing.T) {
	var n, active, overlap atomic.Int32
	tb, _, _ := orcaBroker(t, "100.64.0.10", func(orcaRequest) string {
		if active.Add(1) > 1 {
			overlap.Store(1)
		}
		time.Sleep(20 * time.Millisecond)
		active.Add(-1)
		return `{"pairing":{"available":true,"url":"orca://pair?code=` + strconv.Itoa(int(n.Add(1))) + `"}}`
	})
	var wg sync.WaitGroup
	urls := make([]any, 2)
	for i := range 2 {
		wg.Go(func() {
			code, out := tb.verb(t, "orca.pair", `{"scope":"desktop"}`)
			if code != http.StatusOK {
				t.Errorf("%d %v", code, out)
			}
			urls[i] = out["url"]
		})
	}
	wg.Wait()
	if urls[0] == urls[1] || overlap.Load() != 0 || len(tb.svc.restarts) != 2 {
		t.Errorf("urls %v, overlap %d, restarts %v", urls, overlap.Load(), tb.svc.restarts)
	}
}

func TestOrcaPairNeverLogs(t *testing.T) {
	tb, log, _ := orcaBroker(t, "100.64.0.10", readyWith("orca://pair?code=secret"))
	if code, out := tb.verb(t, "orca.pair", `{"scope":"desktop"}`); code != http.StatusOK {
		t.Fatalf("%d %v", code, out)
	}
	if strings.Contains(log.String(), "secret") {
		t.Errorf("log = %s", log)
	}
}

func TestOrcaPairBadScope(t *testing.T) {
	tb, _, _ := orcaBroker(t, "100.64.0.10", readyWith("orca://pair?code=b"))
	for _, body := range []string{`{"scope":"tablet"}`, `{}`, `{"scope":"desktop","x":1}`} {
		if code, out := tb.verb(t, "orca.pair", body); code != http.StatusBadRequest {
			t.Errorf("%s: %d %v", body, code, out)
		}
	}
	if len(tb.svc.restarts) != 0 {
		t.Errorf("restarts = %v", tb.svc.restarts)
	}
}
