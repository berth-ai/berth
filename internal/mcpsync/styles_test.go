package mcpsync

import (
	"strings"
	"testing"

	"github.com/berth-ai/berth/internal/locksource"
)

func TestTokenReferences(t *testing.T) {
	sv := server{ID: "c7", URL: "https://c7.example/mcp", TokenVar: "MCP_C7_TOKEN"}
	for style, want := range map[string]string{
		locksource.StyleClaude:   `{"type":"http","url":"https://c7.example/mcp","headers":{"Authorization":"Bearer ${MCP_C7_TOKEN}"}}`,
		locksource.StyleOpenCode: `{"type":"remote","url":"https://c7.example/mcp","headers":{"Authorization":"Bearer {env:MCP_C7_TOKEN}"},"enabled":true}`,
	} {
		e, err := jsonEntries(style, []server{sv})
		if err != nil || string(e[0].val) != want {
			t.Errorf("%s = %s, %v", style, e[0].val, err)
		}
	}
	got := strings.Join(codexTable(sv), "\n")
	if !strings.Contains(got, `bearer_token_env_var = "MCP_C7_TOKEN"`) || strings.Contains(got, "http_headers") {
		t.Errorf("codex = %s", got)
	}
}
