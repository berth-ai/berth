package broker

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

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
