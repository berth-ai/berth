package config

import (
	"errors"
	"strings"
	"testing"
)

func TestDefaultsAreValid(t *testing.T) {
	d := Defaults()
	if err := d.Validate(); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
	if d.Schema != 1 || d.Revision != 0 || !d.Frontends.Terminal.Enabled || d.Frontends.IDE.Enabled ||
		d.Agents.Ref != "main" || d.Agents.Keep != 3 || d.Agents.BootTimeout != 600 ||
		d.Agents.MCPTimeout != 120 || d.Runtime.TZ != "UTC" || d.Agents.Disabled == nil {
		t.Errorf("defaults = %+v", d)
	}
}

func TestValidateReportsEveryField(t *testing.T) {
	c := Defaults()
	c.Schema = 2
	c.Agents.Ref = "../x"
	c.Agents.Disabled = []string{"Bad Name", "opencode-ai", "opencode-ai"}
	c.Agents.Keep = 51
	c.Agents.BootTimeout = 29
	c.Agents.MCPTimeout = 1801
	c.Runtime.TZ = "Mars/Base"
	err := c.Validate()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v", err)
	}
	var got []string
	for _, f := range ve.Fields {
		got = append(got, f.Field)
	}
	want := "schema agents.ref agents.disabled[0] agents.disabled[2] agents.keep agents.boot_timeout agents.mcp_timeout runtime.tz"
	if strings.Join(got, " ") != want {
		t.Errorf("fields = %v\nwant     %s", got, want)
	}
	if !strings.Contains(err.Error(), "agents.keep: must be between 0 and 50") {
		t.Errorf("message = %q", err.Error())
	}
}

func TestZones(t *testing.T) {
	for tz, ok := range map[string]bool{
		"UTC": true, "Europe/Rome": true, "America/Argentina/Buenos_Aires": true, "Etc/GMT+5": true,
		"": false, "Local": false, "Mars/Base": false, "../etc/passwd": false, "/UTC": false, "Europe/": false,
	} {
		c := Defaults()
		c.Runtime.TZ = tz
		if (c.Validate() == nil) != ok {
			t.Errorf("tz %q: valid = %v, want %v", tz, !ok, ok)
		}
	}
}

func TestValidRef(t *testing.T) {
	for ref, ok := range map[string]bool{
		"main": true, "v1.2.3": true, "feature/x_y-1": true,
		"": false, "-x": false, "a/../b": false, "a b": false, strings.Repeat("a", 201): false,
	} {
		if ValidRef(ref) != ok {
			t.Errorf("ValidRef(%q) = %v", ref, !ok)
		}
	}
}

func TestNormalizeSortsDisabled(t *testing.T) {
	c := Defaults()
	c.Agents.Disabled = []string{"opencode-ai", "@openai/codex"}
	c.normalize()
	if strings.Join(c.Agents.Disabled, ",") != "@openai/codex,opencode-ai" {
		t.Errorf("disabled = %v", c.Agents.Disabled)
	}
	c.Agents.Disabled = nil
	c.normalize()
	if c.Agents.Disabled == nil {
		t.Error("nil disabled must become an empty list")
	}
}
