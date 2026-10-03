package broker

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeHome writes recording fakes for mise, git, gh, glab and berth into
// a temp dir and points tb's broker at them.
func fakeHome(t *testing.T, tb *testBroker) string {
	t.Helper()
	dir := filepath.Join(tb.dir, "home-bin")
	os.MkdirAll(dir, 0o755)
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte("#!/bin/sh\necho \"$*\" >> \""+dir+"/"+name+".log\"\n"+body), 0o755)
		return p
	}
	tb.b.Home = HomeCommands{
		Mise: write("mise", `case $1 in
  ls-remote) [ -n "$FAKE_SLOW" ] && sleep 5; printf '1.8.0\n1.8.1\n1.8.2\n' ;;
  registry) printf 'jq                 aqua:jqlang/jq\nnode               core:node\n' ;;
  use|unuse) echo "MISE_YES=$MISE_YES" ;;
esac
`),
		Git:   write("git", ""),
		GH:    write("gh", ""),
		Glab:  write("glab", ""),
		Berth: write("berth", `printf '{"user_tools":[{"name":"go","version":"1.26.0"}],"warnings":[],"schema":1}'`),
	}
	tb.b.BaseTools = "../../tools/mise.toml"
	tb.b.PresetsDir = "../../tools/presets"
	return dir
}

func callLog(dir, name string) string {
	raw, _ := os.ReadFile(filepath.Join(dir, name+".log"))
	return string(raw)
}

func TestToolsList(t *testing.T) {
	tb := newTestBroker(t)
	dir := fakeHome(t, tb)
	code, out := tb.verb(t, "tools.list", `{}`)
	if code != 200 {
		t.Fatalf("list = %d %v", code, out)
	}
	if len(out["base"].([]any)) < 25 || len(out["user"].([]any)) != 1 {
		t.Errorf("base/user = %v %v", len(out["base"].([]any)), out["user"])
	}
	var goState string
	for _, p := range out["presets"].([]any) {
		if p.(map[string]any)["id"] == "go" {
			goState = p.(map[string]any)["state"].(string)
		}
	}
	if goState != "outdated" {
		t.Errorf("go preset = %q", goState)
	}
	if got := callLog(dir, "berth"); got != "status --json\n" {
		t.Errorf("berth ran %q", got)
	}
}

func TestToolsListReadsAsAgent(t *testing.T) {
	tb := newTestBroker(t)
	fakeHome(t, tb)
	// The broker must not know where the agent's mise config lives: every
	// read goes through berth status, which runs with the agent's rights.
	src, _ := os.ReadFile("tools.go")
	if strings.Contains(string(src), "config.toml") || strings.Contains(string(src), "UserTools(") {
		t.Error("tools.go reads the user track itself")
	}
}

func TestToolsVersionsAndRegistry(t *testing.T) {
	tb := newTestBroker(t)
	dir := fakeHome(t, tb)
	code, out := tb.verb(t, "tools.versions", `{"tool":"jq"}`)
	if code != 200 || !slices.Equal(toStrings(out["versions"]), []string{"1.8.0", "1.8.1", "1.8.2"}) {
		t.Errorf("versions = %d %v", code, out)
	}
	if got := callLog(dir, "mise"); got != "ls-remote jq\n" {
		t.Errorf("mise ran %q", got)
	}
	code, out = tb.verb(t, "tools.registry", `{}`)
	if code != 200 || !slices.Equal(toStrings(out["tools"]), []string{"jq", "node"}) {
		t.Errorf("registry = %d %v", code, out)
	}
}

func TestVersionsTimeout(t *testing.T) {
	tb := newTestBroker(t)
	fakeHome(t, tb)
	tb.b.SyncTimeout = 200 * time.Millisecond
	t.Setenv("FAKE_SLOW", "1")
	start := time.Now()
	code, out := tb.verb(t, "tools.versions", `{"tool":"jq"}`)
	if code != 504 || errCode(out) != "timeout" || time.Since(start) > 3*time.Second {
		t.Errorf("slow = %d %v after %s", code, out, time.Since(start))
	}
}

func TestToolJobs(t *testing.T) {
	tb := newTestBroker(t)
	dir := fakeHome(t, tb)
	for _, c := range []struct{ verb, body, want string }{
		{"tools.add", `{"tool":"jq","version":"1.8.2"}`, "use -g jq@1.8.2"},
		{"tools.remove", `{"tool":"jq"}`, "unuse -g jq"},
		{"preset.install", `{"preset":"go"}`, "use -g go@1.27.1"},
	} {
		code, out := tb.verb(t, c.verb, c.body)
		if code != 202 || out["job"].(map[string]any)["group"] != "tools" {
			t.Fatalf("%s = %d %v", c.verb, code, out)
		}
		tb.waitIdle(t)
		if lines := strings.Split(strings.TrimSpace(callLog(dir, "mise")), "\n"); lines[len(lines)-1] != c.want {
			t.Errorf("%s ran %q", c.verb, lines[len(lines)-1])
		}
	}
	job, _ := tb.b.Jobs.Get(1)
	if !strings.Contains(job.Log, "MISE_YES=1") {
		t.Errorf("job log = %q", job.Log)
	}
}

func TestToolArgumentValidation(t *testing.T) {
	tb := newTestBroker(t)
	dir := fakeHome(t, tb)
	for _, c := range []struct{ verb, body string }{
		{"tools.add", `{"tool":"--help","version":"1"}`},
		{"tools.add", `{"tool":"jq","version":"1;rm -rf ~"}`},
		{"tools.add", `{"tool":"jq"}`},
		{"tools.remove", `{"tool":"-g"}`},
		{"tools.versions", `{"tool":"a b"}`},
		{"preset.install", `{"preset":"../go"}`},
	} {
		if code, out := tb.verb(t, c.verb, c.body); code != 400 || errCode(out) != "invalid_request" {
			t.Errorf("%s %s = %d %v", c.verb, c.body, code, out)
		}
	}
	if code, out := tb.verb(t, "preset.install", `{"preset":"cobol"}`); code != 404 || errCode(out) != "unknown_preset" {
		t.Errorf("unknown preset = %d %v", code, out)
	}
	if got := callLog(dir, "mise"); got != "" {
		t.Errorf("mise ran for rejected input: %q", got)
	}
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}
