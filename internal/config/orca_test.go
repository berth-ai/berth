package config

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestDefaultsOrcaOff(t *testing.T) {
	c := Defaults()
	if c.Frontends.Orca.Enabled || c.Frontends.Orca.PairingAddress != "" {
		t.Fatalf("orca defaults = %+v", c.Frontends.Orca)
	}
}

func TestCheckPairingAddress(t *testing.T) {
	for _, ok := range []string{"", "100.64.0.10", "berth.example", "berth.example:7000", "[fd7a::1]:6768", "fd7a::1", "wss://berth.example", "ws://100.64.0.10:7000"} {
		if msg := CheckPairingAddress(ok); msg != "" {
			t.Errorf("%q rejected: %s", ok, msg)
		}
	}
	for _, bad := range []string{"127.0.0.1", "127.1.2.3:6768", "localhost", "localhost:6768", "::1", "0.0.0.0", "::", "[::]:6768", "http://berth.example", "wss://localhost", "a b", "berth.example:0", "berth.example:99999", "berth.example:x", "wss://"} {
		if CheckPairingAddress(bad) == "" {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestPatchOrca(t *testing.T) {
	c, err := Patch(Defaults(), json.RawMessage(`{"frontends":{"orca":{"enabled":true,"pairing_address":"100.64.0.10"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !c.Frontends.Orca.Enabled || c.Frontends.Orca.PairingAddress != "100.64.0.10" {
		t.Errorf("orca = %+v", c.Frontends.Orca)
	}
	c, err = Patch(Defaults(), json.RawMessage(`{"frontends":{"orca":{"pairing_address":"localhost"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	var ve *ValidationError
	if err := c.Validate(); !errors.As(err, &ve) || len(ve.Fields) != 1 || ve.Fields[0].Field != "frontends.orca.pairing_address" {
		t.Errorf("Validate = %v", err)
	}
}

func TestUpgradeKeepsOrcaOff(t *testing.T) {
	c, err := Upgrade([]byte(`{"schema":6,"revision":2,"frontends":{"terminal":{"enabled":true},"ide":{"enabled":false}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Frontends.Orca != (OrcaFrontend{}) {
		t.Errorf("orca = %+v", c.Frontends.Orca)
	}
}
