package config

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

func patchFields(t *testing.T, patch string) []FieldError {
	t.Helper()
	_, err := Patch(Defaults(), json.RawMessage(patch))
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("Patch(%s) err = %v, want ValidationError", patch, err)
	}
	return ve.Fields
}

func TestPatchMerges(t *testing.T) {
	base := Defaults()
	base.Revision = 4
	got, err := Patch(base, json.RawMessage(`{"agents":{"keep":5,"disabled":["opencode-ai"]},"frontends":{"ide":{"enabled":true}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Agents.Keep != 5 || got.Agents.Ref != "main" || len(got.Agents.Disabled) != 1 ||
		!got.Frontends.IDE.Enabled || !got.Frontends.Terminal.Enabled || got.Revision != 4 || got.Runtime.TZ != "UTC" {
		t.Errorf("got %+v", got)
	}
}

func TestPatchNullResetsToDefault(t *testing.T) {
	base := Defaults()
	base.Runtime.TZ = "Europe/Rome"
	base.Agents.Keep = 9
	base.Agents.Ref = "v1"
	got, err := Patch(base, json.RawMessage(`{"runtime":{"tz":null},"agents":null}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Runtime.TZ != "UTC" || got.Agents.Keep != 3 || got.Agents.Ref != "main" {
		t.Errorf("got %+v", got)
	}
}

func TestPatchErrors(t *testing.T) {
	for patch, want := range map[string]FieldError{
		`{"agents":{"bogus":1}}`:              {"agents.bogus", "is not a setting"},
		`{"extra":true}`:                      {"extra", "is not a setting"},
		`{"revision":9}`:                      {"revision", "cannot be changed"},
		`{"schema":1}`:                        {"schema", "cannot be changed"},
		`{"frontends":true}`:                  {"frontends", "must be an object"},
		`{"frontends":{"terminal":true}}`:     {"frontends.terminal", "must be an object"},
		`{"agents":{"keep":"5"}}`:             {"agents.keep", "must be a whole number"},
		`{"agents":{"keep":2.5}}`:             {"agents.keep", "must be a whole number"},
		`{"agents":{"disabled":"x"}}`:         {"agents.disabled", "must be a list"},
		`{"frontends":{"ide":{"enabled":1}}}`: {"frontends.ide.enabled", "must be true or false"},
		`[]`:                                  {"", "the patch must be a JSON object"},
		`null`:                                {"", "the patch must be a JSON object"},
		`{"a":1} {"b":2}`:                     {"", "the patch must be a JSON object"},
	} {
		got := patchFields(t, patch)
		if len(got) != 1 || got[0] != want {
			t.Errorf("Patch(%s) = %v, want %v", patch, got, want)
		}
	}
}

func TestPatchDoesNotModifyInput(t *testing.T) {
	base := Defaults()
	base.Agents.Disabled = []string{"opencode-ai"}
	if _, err := Patch(base, json.RawMessage(`{"agents":{"disabled":[]}}`)); err != nil {
		t.Fatal(err)
	}
	if len(base.Agents.Disabled) != 1 {
		t.Errorf("input changed: %v", base.Agents.Disabled)
	}
}

func TestPatchMCPMaps(t *testing.T) {
	c := Defaults()
	c, err := Patch(c, json.RawMessage(`{"mcp":{"servers":{"linear":{"url":"https://mcp.example.com/mcp"}},"available":{"linear":["a-cli"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if s := c.MCP.Servers["linear"]; s.URL != "https://mcp.example.com/mcp" || s.Auth != AuthNone || !s.Enabled {
		t.Errorf("new server = %+v", s)
	}
	c, err = Patch(c, json.RawMessage(`{"mcp":{"servers":{"linear":{"auth":"bearer","enabled":false}},"available":{"linear":["a-cli","b-cli"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if s := c.MCP.Servers["linear"]; s.URL != "https://mcp.example.com/mcp" || s.Auth != AuthBearer || s.Enabled {
		t.Errorf("merged server = %+v", s)
	}
	if !slices.Equal(c.MCP.Available["linear"], []string{"a-cli", "b-cli"}) {
		t.Errorf("available = %v", c.MCP.Available)
	}
	c, err = Patch(c, json.RawMessage(`{"mcp":{"servers":{"linear":null},"available":{"linear":null}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.MCP.Servers) != 0 || len(c.MCP.Available) != 0 {
		t.Errorf("after delete = %+v", c.MCP)
	}
}

func TestPatchMCPErrors(t *testing.T) {
	for patch, field := range map[string]string{
		`{"mcp":{"servers":{"x":"https://a.example"}}}`: "mcp.servers.x",
		`{"mcp":{"servers":{"x":{"surprise":1}}}}`:      "mcp.servers.x.surprise",
		`{"mcp":{"available":{"x":"a-cli"}}}`:           "mcp.available.x",
		`{"mcp":{"surprise":{}}}`:                       "mcp.surprise",
	} {
		_, err := Patch(Defaults(), json.RawMessage(patch))
		var ve *ValidationError
		if !errors.As(err, &ve) || !strings.HasPrefix(ve.Fields[0].Field, field) {
			t.Errorf("%s: err = %v, want field %s", patch, err, field)
		}
	}
}
