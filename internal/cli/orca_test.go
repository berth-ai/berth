package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berth-ai/berth/internal/broker"
	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/pairqr"
)

// orcaBroker answers orca.pair with code and body, and records requests.
func orcaBroker(t *testing.T, code int, body func(scope string) any) (Config, *[]string) {
	t.Helper()
	return orcaVerbBroker(t, "orca.pair", code, body)
}

// orcaVerbBroker answers verb with code and body, and records each
// request's scope.
func orcaVerbBroker(t *testing.T, verb string, code int, body func(scope string) any) (Config, *[]string) {
	t.Helper()
	cfg := testConfig(t)
	cfg.BrokerSocket = filepath.Join(shortTempDir(t), "b.sock")
	l, err := broker.Listen(cfg.BrokerSocket, -1)
	if err != nil {
		t.Fatal(err)
	}
	var scopes []string
	srv := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/verbs/"+verb {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var in struct{ Scope string }
		_ = json.Unmarshal(raw, &in)
		scopes = append(scopes, in.Scope)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(body(in.Scope))
	})}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	return cfg, &scopes
}

func pairBody(warning string) func(string) any {
	return func(scope string) any {
		rows, _ := pairqr.Matrix("orca://pair?code=x")
		out := map[string]any{"url": "orca://pair?code=x", "scope": scope, "address": "100.64.0.10:6768", "qr": rows}
		if warning != "" {
			out["warning"] = warning
		}
		return out
	}
}

func TestOrcaPairHuman(t *testing.T) {
	cfg, scopes := orcaBroker(t, http.StatusOK, pairBody("only this host can pair"))
	out, errOut, code := run(t, cfg, "orca", "pair")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{"█", "orca://pair?code=x", "100.64.0.10:6768", "A new link replaces any unused one."} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(errOut, "only this host can pair") || strings.Contains(out, "only this host") {
		t.Errorf("warning belongs on stderr: %q", errOut)
	}
	if len(*scopes) != 1 || (*scopes)[0] != "desktop" {
		t.Errorf("scopes = %v", *scopes)
	}
}

func TestOrcaPairJSON(t *testing.T) {
	cfg, _ := orcaBroker(t, http.StatusOK, pairBody(""))
	out, errOut, code := run(t, cfg, "orca", "pair", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if strings.Contains(out, `"qr"`) {
		t.Errorf("json has qr: %s", out)
	}
	golden(t, "orca_pair", out)
}

func TestOrcaPairMobileFlag(t *testing.T) {
	cfg, scopes := orcaBroker(t, http.StatusOK, pairBody(""))
	if _, errOut, code := run(t, cfg, "orca", "pair", "--mobile", "--json"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if len(*scopes) != 1 || (*scopes)[0] != "mobile" {
		t.Errorf("scopes = %v", *scopes)
	}
}

func TestOrcaPairErrors(t *testing.T) {
	cfg, _ := orcaBroker(t, http.StatusConflict, func(string) any {
		return map[string]any{"error": map[string]any{"code": "orca_disabled", "message": "Orca is off; turn on frontends.orca.enabled first"}}
	})
	_, errOut, code := run(t, cfg, "orca", "pair")
	if code != 1 || !strings.Contains(errOut, "frontends.orca.enabled") {
		t.Errorf("exit %d: %s", code, errOut)
	}
}

func TestOrcaUsage(t *testing.T) {
	cfg, scopes := orcaBroker(t, http.StatusOK, pairBody(""))
	for _, args := range [][]string{{"orca"}, {"orca", "frob"}, {"orca", "pair", "extra"}, {"orca", "devices", "extra"}, {"orca", "devices", "--mobile"}} {
		if _, _, code := run(t, cfg, args...); code != 2 {
			t.Errorf("%v: exit %d", args, code)
		}
	}
	cfg.IsRoot = false
	if _, _, code := run(t, cfg, "orca", "pair"); code != 4 {
		t.Errorf("non-root: exit %d", code)
	}
	if len(*scopes) != 0 {
		t.Errorf("scopes = %v", *scopes)
	}
}

func devicesBody(string) any {
	return map[string]any{"devices": []map[string]any{
		{"id": "d1", "name": "Studio Mac", "scope": "desktop", "paired_at": "2026-01-01T00:00:00Z", "last_seen_at": "2026-01-01T01:00:00Z"},
		{"id": "d2", "name": "Mobile 1/2/2026", "scope": "mobile", "paired_at": "2026-01-02T00:00:00Z", "last_seen_at": nil},
	}}
}

func TestOrcaDevicesHuman(t *testing.T) {
	cfg, _ := orcaVerbBroker(t, "orca.devices", http.StatusOK, devicesBody)
	out, errOut, code := run(t, cfg, "orca", "devices")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "NAME") ||
		!strings.Contains(lines[1], "Studio Mac") || !strings.Contains(lines[1], "desktop") ||
		!strings.Contains(lines[2], "Mobile 1/2/2026") || !strings.Contains(lines[2], "phone") || !strings.HasSuffix(lines[2], "never") {
		t.Errorf("stdout:\n%s", out)
	}
}

func TestOrcaDevicesNone(t *testing.T) {
	cfg, _ := orcaVerbBroker(t, "orca.devices", http.StatusOK, func(string) any { return map[string]any{"devices": []any{}} })
	out, errOut, code := run(t, cfg, "orca", "devices")
	if code != 0 || !strings.Contains(out, "berth orca pair") {
		t.Errorf("exit %d: %q %q", code, out, errOut)
	}
}

func TestOrcaDevicesJSON(t *testing.T) {
	cfg, _ := orcaVerbBroker(t, "orca.devices", http.StatusOK, devicesBody)
	out, errOut, code := run(t, cfg, "orca", "devices", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	golden(t, "orca_devices", out)
}

// fakeOrcaIDE writes an orca-ide that has no projects, adds any folder,
// and records its calls in the returned file.
func fakeOrcaIDE(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	script := `#!/bin/sh
echo "$*" >>'` + calls + `'
case "$1 $2" in
"project setups") echo '{"ok":true,"result":{"setups":[]}}' ;;
"repo add") echo '{"ok":true,"result":{"repo":{}}}' ;;
*) echo '{"ok":false,"error":{"message":"unexpected"}}'; exit 1 ;;
esac
`
	bin := filepath.Join(dir, "orca-ide")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, calls
}

func orcaSyncConfig(t *testing.T) (Config, string) {
	t.Helper()
	cfg := testConfig(t)
	cfg.IsRoot = false
	cfg.ConfigDir = t.TempDir()
	home := t.TempDir()
	cfg.Getenv = func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}
	bin, calls := fakeOrcaIDE(t)
	cfg.OrcaIDE = bin
	ws := filepath.Join(t.TempDir(), "proj")
	if err := os.Mkdir(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	pub := map[string]config.PublishedWorkspace{"proj": {Path: ws}}
	if err := config.WriteWorkspacesFile(filepath.Join(cfg.ConfigDir, "workspaces.json"), pub, -1, -1); err != nil {
		t.Fatal(err)
	}
	return cfg, calls
}

func TestOrcaSync(t *testing.T) {
	cfg, calls := orcaSyncConfig(t)
	out, errOut, code := run(t, cfg, "orca", "sync", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var res struct {
		Added []string `json:"added"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil || len(res.Added) != 1 || filepath.Base(res.Added[0]) != "proj" {
		t.Fatalf("%v %s", err, out)
	}
	raw, _ := os.ReadFile(calls)
	if !strings.Contains(string(raw), "repo add --path "+res.Added[0]+" --json") {
		t.Errorf("calls:\n%s", raw)
	}
	golden(t, "orca_sync", strings.ReplaceAll(out, res.Added[0], "/workspaces/proj"))
}

func TestOrcaSyncRefusesRoot(t *testing.T) {
	cfg, _ := orcaSyncConfig(t)
	cfg.IsRoot = true
	if _, errOut, code := run(t, cfg, "orca", "sync"); code == 0 || !strings.Contains(errOut, "must run as agent") {
		t.Errorf("exit %d: %s", code, errOut)
	}
}

func TestOrcaSyncOrcaDown(t *testing.T) {
	cfg, _ := orcaSyncConfig(t)
	cfg.OrcaIDE = filepath.Join(t.TempDir(), "missing")
	cfg.OrcaWait = time.Millisecond
	if out, _, code := run(t, cfg, "orca", "sync", "--json"); code == 0 || !strings.Contains(out, `"orca_unavailable"`) {
		t.Errorf("exit %d: %s", code, out)
	}
}
