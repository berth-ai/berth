package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berth-ai/berth/internal/broker"
	"github.com/berth-ai/berth/internal/pairqr"
)

// orcaBroker answers orca.pair with code and body, and records requests.
func orcaBroker(t *testing.T, code int, body func(scope string) any) (Config, *[]string) {
	t.Helper()
	cfg := testConfig(t)
	cfg.BrokerSocket = filepath.Join(shortTempDir(t), "b.sock")
	l, err := broker.Listen(cfg.BrokerSocket, -1)
	if err != nil {
		t.Fatal(err)
	}
	var scopes []string
	srv := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/verbs/orca.pair" {
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
	for _, args := range [][]string{{"orca"}, {"orca", "frob"}, {"orca", "pair", "extra"}} {
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
