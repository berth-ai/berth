package locksource

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

var descMeta = map[string]Meta{
	"@a/agent": {Kind: KindAgent, Name: "A"},
	"b-mcp":    {Kind: KindMCP, Name: "B"},
	"c-mcp":    {Kind: KindMCP, Name: "C"},
}

func TestParseDescriptors(t *testing.T) {
	pkg := []byte(`{"berthAgents":{"schema":1,"future":true,
		"agents":{"@a/agent":{"mcp":{"file":"~/.a/settings.json","style":"claude","key":"mcpServers"},"login":{"x":1}}},
		"servers":{"b-mcp":{"id":"b","bin":"b-mcp","args":["--x"]},"c-mcp":{"id":"c","bin":"c-mcp"}}}}`)
	d, err := ParseDescriptors(pkg, descMeta)
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Agents["@a/agent"].MCP; got == nil || got.File != "~/.a/settings.json" || got.Style != StyleClaude || got.Key != "mcpServers" {
		t.Errorf("agent = %+v", got)
	}
	if s := d.Servers["b-mcp"]; s.ID != "b" || s.Bin != "b-mcp" || !slices.Equal(s.Args, []string{"--x"}) {
		t.Errorf("server b = %+v", s)
	}
	if s := d.Servers["c-mcp"]; s.Args == nil || len(s.Args) != 0 {
		t.Errorf("server c args = %#v, want empty non-nil", s.Args)
	}
}

func TestParseDescriptorsMissingBlock(t *testing.T) {
	d, err := ParseDescriptors([]byte(`{"dependencies":{}}`), descMeta)
	if err != nil {
		t.Fatal(err)
	}
	if d.Agents == nil || d.Servers == nil || len(d.Agents)+len(d.Servers) != 0 {
		t.Errorf("descriptors = %+v, want empty non-nil maps", d)
	}
}

func TestParseDescriptorsNewerSchema(t *testing.T) {
	_, err := ParseDescriptors([]byte(`{"berthAgents":{"schema":2,"agents":{"x":{"y":1}}}}`), descMeta)
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "agent lock needs a newer Berth image (descriptor schema 2)") {
		t.Errorf("err = %v", err)
	}
}

func TestParseRejectsBadDescriptors(t *testing.T) {
	agent := func(mcp string) string { return `{"schema":1,"agents":{"@a/agent":{"mcp":` + mcp + `}}}` }
	for name, block := range map[string]string{
		"no schema":       `{"agents":{}}`,
		"schema zero":     `{"schema":0}`,
		"unknown agent":   `{"schema":1,"agents":{"other":{}}}`,
		"agent is mcp":    `{"schema":1,"agents":{"b-mcp":{}}}`,
		"server is agent": `{"schema":1,"servers":{"@a/agent":{"id":"a","bin":"a"}}}`,
		"bad style":       agent(`{"file":"~/.a.json","style":"yaml","key":"k"}`),
		"json no key":     agent(`{"file":"~/.a.json","style":"claude"}`),
		"bad key":         agent(`{"file":"~/.a.json","style":"claude","key":"a.b"}`),
		"toml with key":   agent(`{"file":"~/.a.toml","style":"codex-toml","key":"k"}`),
		"absolute file":   agent(`{"file":"/etc/a.json","style":"claude","key":"k"}`),
		"escaping file":   agent(`{"file":"~/../a.json","style":"claude","key":"k"}`),
		"dotdot inside":   agent(`{"file":"$XDG_CONFIG_HOME/a/../../b.json","style":"claude","key":"k"}`),
		"double slash":    agent(`{"file":"~//etc/a.json","style":"claude","key":"k"}`),
		"bare home":       agent(`{"file":"~/","style":"claude","key":"k"}`),
		"bad id":          `{"schema":1,"servers":{"b-mcp":{"id":"B_1","bin":"b"}}}`,
		"duplicate id":    `{"schema":1,"servers":{"b-mcp":{"id":"x","bin":"b"},"c-mcp":{"id":"x","bin":"c"}}}`,
		"bin with slash":  `{"schema":1,"servers":{"b-mcp":{"id":"b","bin":"../b"}}}`,
		"wrong type":      `{"schema":1,"servers":[]}`,
	} {
		_, err := ParseDescriptors([]byte(`{"berthAgents":`+block+`}`), descMeta)
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestParseCarriesDescriptors(t *testing.T) {
	pkg := []byte(`{"dependencies":{"b-mcp":"1.0.0"},
		"berth":{"b-mcp":{"kind":"mcp","name":"B","keys":[]}},
		"berthAgents":{"schema":1,"servers":{"b-mcp":{"id":"b","bin":"b-mcp"}}}}`)
	lock := []byte(`{"lockfileVersion":3,"packages":{"":{"dependencies":{"b-mcp":"1.0.0"}}}}`)
	l, err := Parse(pkg, lock, Source{Origin: OriginDir})
	if err != nil {
		t.Fatal(err)
	}
	if l.Descriptors.Servers["b-mcp"].ID != "b" {
		t.Errorf("descriptors = %+v", l.Descriptors)
	}
	bad := []byte(`{"dependencies":{"b-mcp":"1.0.0"},"berthAgents":{"schema":9}}`)
	if _, err := Parse(bad, lock, Source{Origin: OriginDir}); !errors.Is(err, ErrInvalid) {
		t.Errorf("Parse accepted schema 9: %v", err)
	}
}
