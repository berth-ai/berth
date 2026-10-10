package workspace

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeTmux writes a tmux that appends its arguments to a log, one call
// per line, and answers with cases, the body of a shell case on $1.
func fakeTmux(t *testing.T, cases string) (Tmux, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "tmux.log")
	p := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\necho \"$*\" >> \"" + log + "\"\ncase $1 in\n" + cases + "\nesac\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return Tmux{Path: p}, log
}

func logLines(t *testing.T, path string) []string {
	t.Helper()
	raw, _ := os.ReadFile(path)
	if len(raw) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}

const (
	noSessionYet = `has-session) echo "can't find session: ws-api" >&2; exit 1 ;;`
	shellIsZsh   = `start-server) echo /usr/bin/zsh ;;`
	claudeCmd    = `'/var/lib/berth/bin/claude'; exec '/usr/bin/zsh'`
)

func TestLaunch(t *testing.T) {
	agent := Window{Name: "claude", Run: "/var/lib/berth/bin/claude"}
	for name, tc := range map[string]struct {
		cases string
		w     Window
		want  []string
	}{
		"agent, new session": {noSessionYet + "\n" + shellIsZsh, agent, []string{
			"start-server ; show-options -gv default-shell",
			"has-session -t =ws-api",
			"new-session -d -s ws-api -c /w/acme/api -n claude " + claudeCmd,
		}},
		"agent, running session": {shellIsZsh, agent, []string{
			"start-server ; show-options -gv default-shell",
			"has-session -t =ws-api",
			"new-window -t =ws-api: -c /w/acme/api -n claude " + claudeCmd,
		}},
		"shell, new session": {noSessionYet, Window{}, []string{
			"has-session -t =ws-api",
			"new-session -d -s ws-api -c /w/acme/api",
		}},
		"shell, running session": {"", Window{}, []string{
			"has-session -t =ws-api",
			"new-window -t =ws-api: -c /w/acme/api",
		}},
		"session created meanwhile": {noSessionYet + "\n" + `new-session) echo "duplicate session: ws-api" >&2; exit 1 ;;`, Window{}, []string{
			"has-session -t =ws-api",
			"new-session -d -s ws-api -c /w/acme/api",
			"new-window -t =ws-api: -c /w/acme/api",
		}},
	} {
		t.Run(name, func(t *testing.T) {
			tm, log := fakeTmux(t, tc.cases)
			if err := tm.Launch(context.Background(), "api", "/w/acme/api", tc.w); err != nil {
				t.Fatal(err)
			}
			if got := logLines(t, log); !slices.Equal(got, tc.want) {
				t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}

func TestLaunchReportsTmuxErrors(t *testing.T) {
	tm, _ := fakeTmux(t, `has-session) echo "server exited unexpectedly" >&2; exit 1 ;;`)
	if err := tm.Launch(context.Background(), "api", "/w", Window{}); err == nil || !strings.Contains(err.Error(), "server exited unexpectedly") {
		t.Errorf("Launch = %v", err)
	}
}

func TestKill(t *testing.T) {
	for name, tc := range map[string]struct {
		cases  string
		killed bool
		fails  bool
	}{
		"running":   {"", true, false},
		"no such":   {`kill-session) echo "can't find session: =ws-api" >&2; exit 1 ;;`, false, false},
		"no server": {`kill-session) echo "no server running on /tmp/tmux-1000/default" >&2; exit 1 ;;`, false, false},
		"other":     {`kill-session) echo "lost server" >&2; exit 1 ;;`, false, true},
	} {
		t.Run(name, func(t *testing.T) {
			tm, log := fakeTmux(t, tc.cases)
			killed, err := tm.Kill(context.Background(), "api")
			if killed != tc.killed || (err != nil) != tc.fails {
				t.Errorf("Kill = %v, %v", killed, err)
			}
			if got := logLines(t, log); !slices.Equal(got, []string{"kill-session -t =ws-api"}) {
				t.Errorf("calls = %v", got)
			}
		})
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("/opt/it's"); got != `'/opt/it'\''s'` {
		t.Errorf("shellQuote = %s", got)
	}
}
