package mcpsync

import (
	"bytes"
	"errors"
	"slices"
	"testing"
)

var cpw = server{ID: "playwright", Command: "/l/playwright-mcp", Args: []string{"--headless", "--no-sandbox"}}

const pwBlock = `# >>> berth mcp (managed; edits are overwritten)
[mcp_servers.playwright]
command = "/l/playwright-mcp"
args = ["--headless", "--no-sandbox"]
# <<< berth mcp
`

func codex(t *testing.T, in string, servers []server, owned []string) (string, []string) {
	t.Helper()
	out, skipped, _, err := editCodex([]byte(in), servers, owned, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return string(out), skipped
}

func TestCodexCreatesBlock(t *testing.T) {
	if out, _ := codex(t, "", []server{cpw}, nil); out != pwBlock {
		t.Errorf("got\n%s", out)
	}
}

func TestCodexAppendsAfterUserContent(t *testing.T) {
	in := "model = \"o3\"\n\n[profiles.x]\nmodel = \"y\"\n"
	if out, _ := codex(t, in, []server{cpw}, nil); out != in+"\n"+pwBlock {
		t.Errorf("got\n%s", out)
	}
}

func TestCodexRegeneratesInPlace(t *testing.T) {
	old := "a = 1\n\n# >>> berth mcp (managed; edits are overwritten)\n[mcp_servers.context7]\ncommand = \"/l/context7-mcp\"\nargs = []\n[mcp_servers.playwright]\ncommand = \"/old\"\nargs = []\n# <<< berth mcp\n\n[profiles.x]\nk = 1\n"
	want := "a = 1\n\n" + pwBlock + "\n[profiles.x]\nk = 1\n"
	if out, _ := codex(t, old, []server{cpw}, []string{"context7", "playwright"}); out != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}

func TestCodexRemovesEmptyBlock(t *testing.T) {
	in := "a = 1\n\n" + pwBlock
	if out, _ := codex(t, in, nil, []string{"playwright"}); out != "a = 1\n\n" {
		t.Errorf("got %q", out)
	}
}

func TestCodexMigratesLegacyTables(t *testing.T) {
	in := "[mcp_servers.playwright]\ncommand = \"/old\"\nargs = []\n\n[mcp_servers.playwright.env]\nX = \"1\"\n\n[mcp_servers.mine]\ncommand = \"m\"\n\n[mcp_servers.\"context7\"]\ncommand = \"/old\"\n"
	want := "[mcp_servers.mine]\ncommand = \"m\"\n\n" + pwBlock
	if out, _ := codex(t, in, []server{cpw}, []string{"context7", "playwright"}); out != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}

func TestCodexSkipsUserTable(t *testing.T) {
	in := "[mcp_servers.playwright]\ncommand = \"mine\"\n"
	out, skipped := codex(t, in, []server{cpw}, nil)
	if out != in || !slices.Equal(skipped, []string{"playwright"}) {
		t.Errorf("out %q skipped %v", out, skipped)
	}
}

func TestCodexUnchangedReturnsInput(t *testing.T) {
	in := []byte("a = 1\n\n" + pwBlock)
	out, _, _, err := editCodex(in, []server{cpw}, []string{"playwright"}, nil, nil)
	if err != nil || !bytes.Equal(out, in) {
		t.Errorf("out %q err %v", out, err)
	}
	if out, _, _, err := editCodex(nil, nil, nil, nil, nil); err != nil || len(out) != 0 {
		t.Errorf("empty edit of a missing file = %q, %v", out, err)
	}
}

func TestCodexRefusesUnsafeFiles(t *testing.T) {
	for name, in := range map[string]string{
		"open basic":   "x = \"\"\"\nabc\n",
		"open literal": "x = '''\nabc\n",
		"open block":   "# >>> berth mcp (managed; edits are overwritten)\n[mcp_servers.a]\n",
	} {
		if _, _, _, err := editCodex([]byte(in), []server{cpw}, nil, nil, nil); !errors.Is(err, errUnsafeTOML) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestTOMLString(t *testing.T) {
	if got := tomlString("/p \"q\"\\\t\x01"); got != `"/p \"q\"\\\t\u0001"` {
		t.Errorf("got %s", got)
	}
	if got := tomlArray(nil); got != "[]" {
		t.Errorf("empty array = %s", got)
	}
}

func TestCodexKeepsForeignTablesInsideBlock(t *testing.T) {
	in := "# >>> berth mcp (managed; edits are overwritten)\n[mcp_servers.playwright]\ncommand = \"/old\"\nargs = []\n[projects.\"/w/x\"]\ntrust_level = \"trusted\"\n[mcp_servers.mine]\ncommand = \"m\"\n# <<< berth mcp\n"
	want := pwBlock + "[projects.\"/w/x\"]\ntrust_level = \"trusted\"\n[mcp_servers.mine]\ncommand = \"m\"\n"
	out, skipped := codex(t, in, []server{cpw}, []string{"playwright"})
	if out != want || len(skipped) != 0 {
		t.Errorf("got\n%s\nwant\n%s\nskipped %v", out, want, skipped)
	}
	if again, _ := codex(t, out, []server{cpw}, []string{"playwright"}); again != out {
		t.Errorf("second edit changed the file:\n%s", again)
	}
}

func TestCodexRemoteTable(t *testing.T) {
	out, _ := codex(t, "", []server{
		{ID: "linear", URL: `https://mcp.example.com/m?q="x"`, Headers: map[string]string{"Authorization": "Bearer t"}},
		{ID: "open", URL: "https://a.example/mcp"},
	}, nil)
	want := blockStart + "\n" +
		"[mcp_servers.linear]\n" +
		`url = "https://mcp.example.com/m?q=\"x\""` + "\n" +
		`http_headers = { "Authorization" = "Bearer t" }` + "\n" +
		"[mcp_servers.open]\n" +
		`url = "https://a.example/mcp"` + "\n" +
		blockEnd + "\n"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}
