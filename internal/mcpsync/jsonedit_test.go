package mcpsync

import (
	"bytes"
	"errors"
	"slices"
	"testing"

	"github.com/berth-ai/berth/internal/locksource"
)

var pw = server{ID: "playwright", Command: "/l/p", Args: []string{"--x"}}

func claudeEntries(t *testing.T, servers ...server) []entry {
	t.Helper()
	e, err := jsonEntries(locksource.StyleClaude, servers)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestEditJSONCreates(t *testing.T) {
	out, err := editJSON(nil, "mcpServers", claudeEntries(t, pw), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "mcpServers": {
    "playwright": {
      "command": "/l/p",
      "args": [
        "--x"
      ]
    }
  }
}
`
	if string(out) != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}

func TestEditJSONKeepsOrderAndOtherEntries(t *testing.T) {
	in := []byte(`{"z":1,"mcpServers":{"mine":{"command":"m"},"playwright":{"command":"old"},"context7":{"command":"c"}},"a<b":"<&>"}`)
	out, err := editJSON(in, "mcpServers", claudeEntries(t, pw), []string{"context7"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "z": 1,
  "mcpServers": {
    "mine": {
      "command": "m"
    },
    "playwright": {
      "command": "/l/p",
      "args": [
        "--x"
      ]
    }
  },
  "a<b": "<&>"
}
`
	if string(out) != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}

func TestEditJSONUnchangedReturnsInput(t *testing.T) {
	in := []byte("{\n    \"mcpServers\": {\"playwright\": {\"command\": \"/l/p\", \"args\": [\"--x\"]}}\n}\n")
	out, err := editJSON(in, "mcpServers", claudeEntries(t, pw), []string{"context7"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, in) {
		t.Errorf("rewrote an unchanged file:\n%s", out)
	}
	if out, err := editJSON(nil, "mcpServers", nil, []string{"x"}); err != nil || out != nil {
		t.Errorf("empty edit of a missing file = %q, %v", out, err)
	}
}

func TestEditJSONRejects(t *testing.T) {
	for name, in := range map[string]string{
		"comment":  "// c\n{}",
		"trailing": `{"a":1,}`,
		"array":    `[]`,
		"two docs": `{} {}`,
	} {
		if _, err := editJSON([]byte(in), "k", nil, nil); !errors.Is(err, errNotPlainJSON) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := editJSON([]byte(`{"mcpServers":[]}`), "mcpServers", claudeEntries(t, pw), nil); err == nil || errors.Is(err, errNotPlainJSON) {
		t.Errorf("non-object key: err = %v", err)
	}
}

func TestObjectKeys(t *testing.T) {
	keys, err := objectKeys([]byte(`{"mcp":{"b":1,"a":2}}`), "mcp")
	if err != nil || !slices.Equal(keys, []string{"b", "a"}) {
		t.Errorf("keys = %v, %v", keys, err)
	}
	if keys, err := objectKeys(nil, "mcp"); err != nil || keys != nil {
		t.Errorf("missing file keys = %v, %v", keys, err)
	}
}

func TestOpenCodeEntry(t *testing.T) {
	e, err := jsonEntries(locksource.StyleOpenCode, []server{pw, {ID: "c", Command: "/l/c"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(e[0].val); got != `{"type":"local","command":["/l/p","--x"],"enabled":true}` {
		t.Errorf("entry = %s", got)
	}
	if got := string(e[1].val); got != `{"type":"local","command":["/l/c"],"enabled":true}` {
		t.Errorf("entry without args = %s", got)
	}
	if c, _ := jsonEntries(locksource.StyleClaude, []server{{ID: "c", Command: "/l/c"}}); string(c[0].val) != `{"command":"/l/c","args":[]}` {
		t.Errorf("claude entry without args = %s", c[0].val)
	}
	if _, err := jsonEntries(locksource.StyleCodexTOML, []server{pw}); err == nil {
		t.Error("codex-toml is not a JSON style")
	}
}

func TestRemoteEntries(t *testing.T) {
	sv := server{ID: "linear", URL: `https://mcp.example.com/m?q="x"\y&z`, Headers: map[string]string{"Authorization": "Bearer t"}}
	for style, want := range map[string]string{
		locksource.StyleClaude:   `{"type":"http","url":"https://mcp.example.com/m?q=\"x\"\\y&z","headers":{"Authorization":"Bearer t"}}`,
		locksource.StyleOpenCode: `{"type":"remote","url":"https://mcp.example.com/m?q=\"x\"\\y&z","headers":{"Authorization":"Bearer t"},"enabled":true}`,
	} {
		es, err := jsonEntries(style, []server{sv})
		if err != nil || string(es[0].val) != want {
			t.Errorf("%s: %s, %v", style, es[0].val, err)
		}
	}
	es, _ := jsonEntries(locksource.StyleClaude, []server{{ID: "open", URL: "https://a.example/mcp"}})
	if string(es[0].val) != `{"type":"http","url":"https://a.example/mcp"}` {
		t.Errorf("no headers: %s", es[0].val)
	}
}
