package agentlogin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/berth-ai/berth/internal/agentset"
	"github.com/berth-ai/berth/internal/locksource"
	"github.com/berth-ai/berth/internal/testutil"
)

func TestAgentsListsEnabledAgentsWithLogin(t *testing.T) {
	f := newFixture(t, argsLogin, fakeCLI)
	agents, err := f.env.Agents()
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].Package != "a" || agents[0].Name != "Agent A" ||
		agents[0].Bin != filepath.Join(f.root, "bin", "a-cli") || agents[0].Methods[0].ID != "m" {
		t.Errorf("agents = %+v", agents)
	}
	if _, _, err := f.env.Find("b", "m"); !errors.Is(err, ErrUnknown) {
		t.Errorf("disabled agent: err = %v", err)
	}
	if _, _, err := f.env.Find("a", "nope"); !errors.Is(err, ErrUnknown) {
		t.Errorf("unknown method: err = %v", err)
	}
	if agents, err := (Env{}).Agents(); err != nil || len(agents) != 0 {
		t.Errorf("no agent set: %v %v", agents, err)
	}
}

func TestProbeArgs(t *testing.T) {
	f := newFixture(t, argsLogin, fakeCLI)
	a, m, err := f.env.Find("a", "m")
	if err != nil {
		t.Fatal(err)
	}
	if p, err := f.env.Probe(context.Background(), a, m); err != nil || p.SignedIn {
		t.Errorf("signed out: %+v %v", p, err)
	}
	must(t, os.WriteFile(filepath.Join(f.home, "signed"), nil, 0o600))
	if p, err := f.env.Probe(context.Background(), a, m); err != nil || !p.SignedIn || p.Account != "you@example.org" {
		t.Errorf("signed in: %+v %v", p, err)
	}
}

func TestProbeFile(t *testing.T) {
	login := `{"methods":[{"id":"m","label":"M","flow":"device","args":[],
	  "status":{"file":"$XDG_DATA_HOME/a/auth.json","key":"openai","account_file":{"file":"~/.a/acct.json","key":"active"}},
	  "logout":{"remove":["$XDG_DATA_HOME/a/auth.json"]}}]}`
	f := newFixture(t, login, fakeCLI)
	a, m, _ := f.env.Find("a", "m")
	auth := filepath.Join(f.home, ".local", "share", "a", "auth.json")
	must(t, os.MkdirAll(filepath.Dir(auth), 0o700))
	must(t, os.MkdirAll(filepath.Join(f.home, ".a"), 0o700))
	for content, want := range map[string]bool{
		"": false, `{"other":{}}`: false, `{"openai":null}`: false, `{"openai":{"type":"oauth"}}`: true,
	} {
		os.Remove(auth)
		if content != "" {
			must(t, os.WriteFile(auth, []byte(content), 0o600))
		}
		if p, err := f.env.Probe(context.Background(), a, m); err != nil || p.SignedIn != want {
			t.Errorf("%q: %+v %v, want signed_in %v", content, p, err, want)
		}
	}
	must(t, os.WriteFile(auth, []byte(`{"openai":{"type":"oauth"}}`), 0o600))
	must(t, os.WriteFile(filepath.Join(f.home, ".a", "acct.json"), []byte(`{"active":"you@example.org"}`), 0o600))
	if p, _ := f.env.Probe(context.Background(), a, m); p.Account != "you@example.org" {
		t.Errorf("account = %q", p.Account)
	}
	must(t, os.WriteFile(auth, []byte("{not json"), 0o600))
	if _, err := f.env.Probe(context.Background(), a, m); err == nil {
		t.Error("a broken auth file was not an error")
	}
}

func TestStatusProbeTimeout(t *testing.T) {
	old := probeTimeout
	probeTimeout = 2 * time.Second
	t.Cleanup(func() { probeTimeout = old })
	login := `{"methods":[
	  {"id":"slow","label":"Slow","flow":"device","status":{"args":["hang"]},"logout":{"args":["logout"]}},
	  {"id":"fast","label":"Fast","flow":"paste","prompt":"Code","status":{"args":["status"]},"logout":{"args":["logout"]}}]}`
	f := newFixture(t, login, fakeCLI)
	began := time.Now()
	rep, err := f.env.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(began) > 5*time.Second {
		t.Errorf("Status took %s", time.Since(began))
	}
	ms := rep.Agents["a"].Methods
	if rep.Schema != 1 || rep.Agents["a"].Name != "Agent A" || len(ms) != 2 {
		t.Fatalf("report = %+v", rep)
	}
	if ms[0].ID != "slow" || ms[0].SignedIn || !strings.Contains(ms[0].Error, "did not answer") {
		t.Errorf("slow = %+v", ms[0])
	}
	if ms[1].ID != "fast" || ms[1].Flow != "paste" || ms[1].Label != "Fast" || ms[1].Error != "" {
		t.Errorf("fast = %+v", ms[1])
	}
	if _, ok := rep.Agents["b"]; ok {
		t.Error("the disabled agent is reported")
	}
}

func TestLogoutArgs(t *testing.T) {
	f := newFixture(t, argsLogin, fakeCLI)
	must(t, os.WriteFile(filepath.Join(f.home, "signed"), nil, 0o600))
	if err := f.env.Logout(context.Background(), "a", "m"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.home, "signed")); !errors.Is(err, os.ErrNotExist) {
		t.Error("logout did not run")
	}
	stuck := strings.Replace(argsLogin, `"logout":{"args":["logout"]}`, `"logout":{"args":["stuck-logout"]}`, 1)
	f = newFixture(t, stuck, fakeCLI)
	must(t, os.WriteFile(filepath.Join(f.home, "signed"), nil, 0o600))
	if err := f.env.Logout(context.Background(), "a", "m"); !errors.Is(err, ErrStillSignedIn) {
		t.Errorf("err = %v, want ErrStillSignedIn", err)
	}
}

func TestLogoutRemovesLinkNotTarget(t *testing.T) {
	login := `{"methods":[{"id":"m","label":"M","flow":"device","args":[],
	  "status":{"file":"~/.a/creds.json"},"logout":{"remove":["~/.a/creds.json","~/.a/missing.json"]}}]}`
	f := newFixture(t, login, fakeCLI)
	target := filepath.Join(f.root, "target.json")
	must(t, os.WriteFile(target, []byte("{}"), 0o600))
	must(t, os.MkdirAll(filepath.Join(f.home, ".a"), 0o700))
	must(t, os.Symlink(target, filepath.Join(f.home, ".a", "creds.json")))
	if err := f.env.Logout(context.Background(), "a", "m"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(f.home, ".a", "creds.json")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the link is still there")
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("the link's target was removed: %v", err)
	}
}

func TestPrepare(t *testing.T) {
	f := newFixture(t, argsLogin, fakeCLI)
	p := &locksource.LoginPrepare{File: "~/.g/settings.json", Set: "security.auth.selectedType", Value: "oauth-personal"}
	path := filepath.Join(f.home, ".g", "settings.json")
	if err := f.env.prepare(p); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `"selectedType": "oauth-personal"`) {
		t.Errorf("settings = %s", raw)
	}
	must(t, os.WriteFile(path, []byte(`{"theme": "x", "security": {"auth": {"selectedType": "oauth-personal"}}}`), 0o640))
	must(t, os.Chmod(path, 0o640)) // WriteFile keeps an existing file's mode
	before, _ := os.Stat(path)
	if err := f.env.prepare(p); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if !after.ModTime().Equal(before.ModTime()) || after.Mode().Perm() != 0o640 {
		t.Error("an unchanged settings file was rewritten")
	}
	must(t, os.WriteFile(path, []byte(`{"theme": "x"}`), 0o640))
	must(t, f.env.prepare(p))
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640 kept", fi.Mode().Perm())
	}
	if f.env.prepare(nil) != nil {
		t.Error("nil prepare failed")
	}
}

func TestStripComments(t *testing.T) {
	for in, want := range map[string]string{
		`{"a":"http://x//y"}`:          `{"a":"http://x//y"}`,
		"{\"a\":1} // c":               "{\"a\":1}     ",
		"{/* x\ny */\"a\":\"\\\"//\"}": "{    \n    \"a\":\"\\\"//\"}",
		"{\"a\":1} /* open":            "{\"a\":1}        ",
	} {
		if got := string(stripComments([]byte(in))); got != want {
			t.Errorf("stripComments(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProbeFileReadsJSONC(t *testing.T) {
	login := `{"methods":[{"id":"m","label":"M","flow":"device","args":[],
	  "status":{"file":"~/.c/config.json","key":"authTokens","account_file":{"file":"~/.c/config.json","key":"user"}},
	  "logout":{"remove":["~/.c/config.json"]}}]}`
	f := newFixture(t, login, fakeCLI)
	a, m, _ := f.env.Find("a", "m")
	cfg := filepath.Join(f.home, ".c", "config.json")
	must(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	for content, want := range map[string]bool{
		"// User settings belong in settings.json.\n{\n  \"loggedInUsers\": []\n}\n":                                   false,
		"// managed\n{\n  /* tokens */ \"authTokens\": {\"https://github.com:you\": \"x\"},\n  \"user\": \"you\"\n}\n": true,
	} {
		must(t, os.WriteFile(cfg, []byte(content), 0o600))
		p, err := f.env.Probe(context.Background(), a, m)
		if err != nil || p.SignedIn != want {
			t.Errorf("%q: %+v %v, want signed_in %v", content, p, err, want)
		}
		if want && p.Account != "you" {
			t.Errorf("account = %q", p.Account)
		}
	}
	must(t, os.WriteFile(cfg, []byte("// header\n{not json"), 0o600))
	if _, err := f.env.Probe(context.Background(), a, m); err == nil {
		t.Error("a broken JSONC file was not an error")
	}
}

func TestAgentsIncludesAddedAgent(t *testing.T) {
	f := newFixture(t, argsLogin, fakeCLI)
	addedRoot := t.TempDir()
	testutil.AddedInstall(t, addedRoot, "copilot", "0123456789ab", "@github/copilot", "1.0.91", testutil.CopilotPackageJSON, "copilot")
	installed, _ := agentset.Installed(addedRoot)
	set, err := agentset.Load(&agentset.Locked{Dir: filepath.Join(f.root, "set"), Link: f.root}, addedRoot, installed)
	must(t, err)
	f.env.Set = set
	must(t, os.WriteFile(filepath.Join(f.env.Layer, "copilot"), []byte("#!/bin/sh\n"), 0o755))
	agents, err := f.env.Agents()
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(agents, func(a Agent) bool { return a.Package == "@github/copilot" })
	if i < 0 || agents[i].Name != "GitHub Copilot CLI" || agents[i].Methods[0].ID != "github" ||
		agents[i].Bin != filepath.Join(addedRoot, "copilot", "current", "bin", "copilot") {
		t.Errorf("agents = %+v", agents)
	}
}
