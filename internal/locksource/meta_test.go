package locksource

import (
	"errors"
	"testing"
)

var metaDeps = map[string]string{"@a/agent": "1.0.0", "b-mcp": "2.0.0", "plain": "3.0.0"}

func TestParseMeta(t *testing.T) {
	pkg := []byte(`{"dependencies":{},"berth":{
		"@a/agent":{"kind":"agent","name":"A","keys":["A_API_KEY"]},
		"b-mcp":{"kind":"mcp","name":"B","keys":[]}}}`)
	m, err := ParseMeta(pkg, metaDeps)
	if err != nil {
		t.Fatal(err)
	}
	if m["@a/agent"].Name != "A" || m["@a/agent"].Keys[0] != "A_API_KEY" || m["b-mcp"].Kind != KindMCP {
		t.Errorf("meta = %+v", m)
	}
	if p := m["plain"]; p.Kind != KindAgent || p.Name != "plain" || p.Keys == nil || len(p.Keys) != 0 {
		t.Errorf("default entry = %+v", p)
	}
	if m, err := ParseMeta([]byte(`{"dependencies":{}}`), metaDeps); err != nil || len(m) != 3 {
		t.Errorf("no block = %v %v", m, err)
	}
}

func TestParseRejectsBadMeta(t *testing.T) {
	for name, block := range map[string]string{
		"kind":          `{"plain":{"kind":"tool","name":"P","keys":[]}}`,
		"lower key":     `{"plain":{"kind":"agent","name":"P","keys":["api_key"]}}`,
		"empty name":    `{"plain":{"kind":"agent","name":"","keys":[]}}`,
		"unknown pkg":   `{"other":{"kind":"agent","name":"O","keys":[]}}`,
		"extra field":   `{"plain":{"kind":"agent","name":"P","keys":[],"x":1}}`,
		"duplicate key": `{"plain":{"kind":"agent","name":"P","keys":["K","K"]}}`,
		"shared key":    `{"plain":{"kind":"agent","name":"P","keys":["K"]},"b-mcp":{"kind":"mcp","name":"B","keys":["K"]}}`,
	} {
		_, err := ParseMeta([]byte(`{"berth":`+block+`}`), metaDeps)
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestParseCarriesMeta(t *testing.T) {
	lock, err := FromDir("../../agents", Source{Origin: OriginImage})
	if err != nil {
		t.Fatal(err)
	}
	if lock.Meta["@anthropic-ai/claude-code"].Keys[0] != "ANTHROPIC_API_KEY" {
		t.Errorf("meta = %+v", lock.Meta)
	}
}
