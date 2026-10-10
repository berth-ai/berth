package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// workspacesConfig is testConfig with a workspaces root and the git
// file:// seam on.
func workspacesConfig(t *testing.T) (Config, string) {
	t.Helper()
	cfg := testConfig(t)
	cfg.WorkspacesDir = t.TempDir()
	cfg.Getenv = func(k string) string {
		if k == "BERTH_SKILLS_ALLOW_FILE" {
			return "1"
		}
		return ""
	}
	return cfg, cfg.WorkspacesDir
}

func TestWorkspacesClone(t *testing.T) {
	cfg, root := workspacesConfig(t)
	repo := skillRepo(t)
	out, errOut, code := run(t, cfg, "workspaces", "clone", "--json", "--repo", repo, "--path", "acme/api")
	var res struct{ Path, Commit, Branch string }
	if err := json.Unmarshal([]byte(out), &res); err != nil || code != 0 || res.Path != "acme/api" ||
		len(res.Commit) != 40 || res.Branch != "main" {
		t.Fatalf("clone: exit %d, %s %s", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(root, "acme", "api", "skills", "demo", "SKILL.md")); err != nil {
		t.Error(err)
	}
	out, _, code = run(t, cfg, "workspaces", "clone", "--json", "--repo", repo, "--path", "acme/api")
	if code != 1 || !strings.Contains(out, `"code": "path_exists"`) || !strings.Contains(out, `"step": "path"`) {
		t.Errorf("second clone: exit %d, %s", code, out)
	}
}

func TestWorkspacesFolders(t *testing.T) {
	cfg, root := workspacesConfig(t)
	os.MkdirAll(filepath.Join(root, "a", ".git"), 0o755)
	os.MkdirAll(filepath.Join(root, "b"), 0o755)
	out, _, code := run(t, cfg, "workspaces", "probe", "--json", "--path", "a")
	if code != 0 || !strings.Contains(out, `"git": true`) || !strings.Contains(out, `"writable": true`) {
		t.Errorf("probe: exit %d, %s", code, out)
	}
	out, _, code = run(t, cfg, "workspaces", "probe", "--json", "--path", "nope")
	if code != 1 || !strings.Contains(out, `"code": "missing"`) {
		t.Errorf("probe missing: exit %d, %s", code, out)
	}
	out, _, code = run(t, cfg, "workspaces", "candidates", "--json", "--exclude", "b")
	var cands struct{ Candidates []struct{ Path string } }
	if json.Unmarshal([]byte(out), &cands); code != 0 || len(cands.Candidates) != 1 || cands.Candidates[0].Path != "a" {
		t.Errorf("candidates: exit %d, %s", code, out)
	}
	out, _, code = run(t, cfg, "workspaces", "remove", "--json", "--path", "b")
	if _, err := os.Stat(filepath.Join(root, "b")); code != 0 || !strings.Contains(out, `"removed": true`) || err == nil {
		t.Errorf("remove: exit %d, %s", code, out)
	}
}

func TestWorkspacesSessions(t *testing.T) {
	cfg, root := workspacesConfig(t)
	os.MkdirAll(filepath.Join(root, "acme", "api"), 0o755)
	log := filepath.Join(t.TempDir(), "tmux.log")
	cfg.Tmux = filepath.Join(t.TempDir(), "tmux")
	os.WriteFile(cfg.Tmux, []byte("#!/bin/sh\necho \"$*\" >> \""+log+"\"\ncase $1 in\n"+
		"list-windows) printf 'ws-api\\tzsh\\n' ;;\n"+
		"has-session) echo \"can't find session: ws-api\" >&2; exit 1 ;;\n"+
		"start-server) echo /bin/zsh ;;\nesac\n"), 0o755)
	out, _, code := run(t, cfg, "workspaces", "status", "--json", "--ws", "api=acme/api", "--ws", "gone=acme/gone")
	var st struct {
		Workspaces map[string]struct {
			Exists, Session bool
			Windows         []string
		}
	}
	if json.Unmarshal([]byte(out), &st); code != 0 || !st.Workspaces["api"].Exists || !st.Workspaces["api"].Session ||
		st.Workspaces["gone"].Exists || st.Workspaces["gone"].Windows == nil {
		t.Errorf("status: exit %d, %s", code, out)
	}
	out, _, code = run(t, cfg, "workspaces", "launch", "--json", "--id", "api", "--path", "acme/api",
		"--window", "claude", "--run", "/var/lib/berth/bin/claude")
	if code != 0 || !strings.Contains(out, `"session": "ws-api"`) {
		t.Errorf("launch: exit %d, %s", code, out)
	}
	raw, _ := os.ReadFile(log)
	if want := "new-session -d -s ws-api -c " + filepath.Join(root, "acme", "api") + " -n claude"; !strings.Contains(string(raw), want) {
		t.Errorf("tmux calls:\n%s\nwant a line with %q", raw, want)
	}
	out, _, code = run(t, cfg, "workspaces", "launch", "--json", "--id", "gone", "--path", "acme/gone")
	if code != 1 || !strings.Contains(out, `"code": "missing"`) {
		t.Errorf("launch in a missing folder: exit %d, %s", code, out)
	}
	out, _, code = run(t, cfg, "workspaces", "kill", "--json", "--id", "api")
	if code != 0 || !strings.Contains(out, `"killed": true`) {
		t.Errorf("kill: exit %d, %s", code, out)
	}
}

func TestWorkspacesUsage(t *testing.T) {
	cfg, _ := workspacesConfig(t)
	for _, args := range [][]string{
		{"workspaces"},
		{"workspaces", "bogus", "--json"},
		{"workspaces", "status", "--json", "--ws", "nope"},
		{"workspaces", "status", "--json", "--ws", "Bad=x"},
		{"workspaces", "launch", "--json", "--id", "api", "--path", "acme/api", "--run", "claude"},
		{"workspaces", "launch", "--json", "--id", "api", "--path", "acme/api", "--window", "a b"},
		{"workspaces", "kill", "--json", "--id", "Bad"},
		{"workspaces", "probe", "--json", "--path", "a", "extra"},
	} {
		if _, _, code := run(t, cfg, args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}
