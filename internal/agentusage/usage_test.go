package agentusage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berth-ai/berth/internal/agentset"
	"github.com/berth-ai/berth/internal/locksource"
)

// now is a Sunday afternoon in Rome; the window starts Monday 26 October.
var (
	rome = mustLoc("Europe/Rome")
	now  = time.Date(2026, 11, 1, 15, 0, 0, 0, rome)
)

func mustLoc(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// newEnv installs agents a (claude-code usage in ~/.a/projects), b (no
// usage source), c (a format from a later build) and an MCP server m.
func newEnv(t *testing.T) (Env, string) {
	t.Helper()
	root := t.TempDir()
	set, home := filepath.Join(root, "set"), filepath.Join(root, "home")
	pkg := `{"dependencies":{"a":"1.0.0","b":"1.0.0","c":"1.0.0","m":"1.0.0"},
	  "berth":{"a":{"kind":"agent","name":"Agent A","keys":[]},"b":{"kind":"agent","name":"Agent B","keys":[]},
	    "c":{"kind":"agent","name":"Agent C","keys":[]},"m":{"kind":"mcp","name":"Server M","keys":[]}},
	  "berthAgents":{"schema":1,"agents":{
	    "a":{"usage":{"format":"claude-code","dir":"~/.a/projects"}},
	    "b":{},
	    "c":{"usage":{"format":"from-the-future","dir":"~/.c"}}},
	   "servers":{"m":{"id":"m","bin":"m"}}}}`
	write(t, filepath.Join(set, "package.json"), pkg, now)
	for _, dep := range []string{"a", "b", "c", "m"} {
		write(t, filepath.Join(set, "node_modules", dep, "package.json"), `{"name":"`+dep+`","bin":{"`+dep+`":"cli.js"}}`, now)
	}
	s, err := agentset.Load(&agentset.Locked{Dir: set, Link: root}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return Env{Set: s, Dirs: locksource.HomeDirs{Home: home}, Now: now, Loc: rome}, home
}

func write(t *testing.T, path, body string, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// line is one assistant transcript line.
func line(at time.Time, session, id, req, model string, in, out, read, wrote int64) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"sessionId":%q,"requestId":%q,"message":{"id":%q,"model":%q,"role":"assistant","usage":{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d}}}`,
		at.UTC().Format(time.RFC3339Nano), session, req, id, model, in, out, read, wrote)
}

func lines(ls ...string) string { return strings.Join(ls, "\n") + "\n" }

func TestReport(t *testing.T) {
	env, home := newEnv(t)
	projects := filepath.Join(home, ".a", "projects")
	today := time.Date(2026, 11, 1, 9, 0, 0, 0, rome)
	// 00:30 in Rome on 1 November is still 31 October in UTC: it counts as today.
	justAfterMidnight := time.Date(2026, 11, 1, 0, 30, 0, 0, rome)
	yesterday := today.AddDate(0, 0, -1)
	sixDaysAgo := time.Date(2026, 10, 26, 0, 10, 0, 0, rome)
	tooOld := time.Date(2026, 10, 25, 23, 50, 0, 0, rome)

	write(t, filepath.Join(projects, "-work-api", "s1.jsonl"), lines(
		`{"type":"user","timestamp":"2026-11-01T08:00:00Z","message":{"role":"user","content":"keep the usage key out: \"usage\""}}`,
		// One streamed response over two lines: counted once, with its final output.
		line(today, "s1", "msg_1", "req_1", "claude-opus-5-5", 10, 5, 1000, 200),
		line(today, "s1", "msg_1", "req_1", "claude-opus-5-5", 10, 40, 1000, 200),
		line(justAfterMidnight, "s1", "msg_2", "req_2", "claude-haiku-5-5", 1, 2, 3, 4),
		`{"type":"assistant","timestamp":"2026-11-01T08:10:00Z","message":{"id":"x","model":"<synthetic>","usage":{"input_tokens":0,"output_tokens":0}}}`,
		`not json, "usage" "assistant"`,
		line(yesterday, "s1", "msg_3", "req_3", "claude-opus-5-5", 100, 100, 0, 0),
		line(tooOld, "s1", "msg_0", "req_0", "claude-opus-5-5", 1e6, 1e6, 0, 0),
	), today)
	// A resumed session repeats msg_3; a subagent writes its own file.
	write(t, filepath.Join(projects, "-work-api", "s2.jsonl"), lines(
		line(yesterday, "s1", "msg_3", "req_3", "claude-opus-5-5", 100, 100, 0, 0),
		line(sixDaysAgo, "s2", "msg_4", "req_4", "claude-opus-5-5", 7, 0, 0, 0),
	), today)
	write(t, filepath.Join(projects, "-work-api", "s2", "subagents", "agent-1.jsonl"), lines(
		line(today, "", "msg_5", "req_5", "claude-haiku-5-5", 0, 9, 0, 0),
		`{"type":"assistant","usage":"`+strings.Repeat("x", maxLine)+`"}`,
	), today)
	// A file last written before the window is not opened.
	write(t, filepath.Join(projects, "-old", "s9.jsonl"), lines(
		line(today, "s9", "msg_9", "req_9", "claude-opus-5-5", 1e9, 0, 0, 0),
	), tooOld)

	rep := env.Report()
	if rep.Schema != ReportSchema || !rep.Since.Equal(time.Date(2026, 10, 26, 0, 0, 0, 0, rome)) {
		t.Errorf("schema %d since %v", rep.Schema, rep.Since)
	}
	if _, ok := rep.Agents["m"]; ok || len(rep.Agents) != 3 {
		t.Errorf("agents = %v", keys(rep.Agents))
	}
	a := rep.Agents["a"]
	if !a.Tracked || a.Error != "" || a.Name != "Agent A" {
		t.Fatalf("a = %+v", a)
	}
	wantToday := Period{Tokens: Tokens{Input: 11, Output: 51, CacheRead: 1003, CacheWrite: 204}, Total: 1269, Messages: 3, Sessions: 2}
	if a.Today != wantToday {
		t.Errorf("today = %+v, want %+v", a.Today, wantToday)
	}
	wantWeek := Period{Tokens: Tokens{Input: 118, Output: 151, CacheRead: 1003, CacheWrite: 204}, Total: 1476, Messages: 5, Sessions: 3}
	if a.Week != wantWeek {
		t.Errorf("week = %+v, want %+v", a.Week, wantWeek)
	}
	wantDays := []Day{{"2026-10-26", 7}, {"2026-10-27", 0}, {"2026-10-28", 0}, {"2026-10-29", 0}, {"2026-10-30", 0}, {"2026-10-31", 200}, {"2026-11-01", 1269}}
	if fmt.Sprint(a.Days) != fmt.Sprint(wantDays) {
		t.Errorf("days = %v", a.Days)
	}
	if len(a.Models) != 2 || a.Models[0].Model != "claude-opus-5-5" || a.Models[0].Total != 1457 || a.Models[0].Messages != 3 ||
		a.Models[1].Model != "claude-haiku-5-5" || a.Models[1].Total != 19 {
		t.Errorf("models = %+v", a.Models)
	}
	if a.LastActive == nil || !a.LastActive.Equal(today) {
		t.Errorf("last active = %v", a.LastActive)
	}
	for _, dep := range []string{"b", "c"} {
		if u := rep.Agents[dep]; u.Tracked || u.LastActive != nil || len(u.Days) != Days || u.Models == nil {
			t.Errorf("%s = %+v", dep, u)
		}
	}
}

func TestReportBeforeFirstRun(t *testing.T) {
	env, _ := newEnv(t)
	a := env.Report().Agents["a"]
	if !a.Tracked || a.Error != "" || a.Week.Total != 0 || a.LastActive != nil {
		t.Errorf("a = %+v", a)
	}
}

func TestReportJSON(t *testing.T) {
	env, _ := newEnv(t)
	raw, err := json.Marshal(env.Report())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"schema":1`, `"tracked":false`, `"last_active":null`, `"models":[]`, `"cache_read":0`, `"date":"2026-11-01"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("JSON lacks %s:\n%s", want, raw)
		}
	}
}

func TestUnreadableFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads everything")
	}
	env, home := newEnv(t)
	p := filepath.Join(home, ".a", "projects", "p", "s.jsonl")
	write(t, p, lines(line(now, "s", "m", "r", "claude-opus-5-5", 1, 1, 0, 0)), now)
	write(t, filepath.Join(home, ".a", "projects", "p", "ok.jsonl"), lines(line(now.Add(-time.Hour), "s", "m2", "r2", "claude-opus-5-5", 1, 1, 0, 0)), now)
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	a := env.Report().Agents["a"]
	if !strings.Contains(a.Error, "permission denied") || a.Week.Total != 2 {
		t.Errorf("a = %+v", a)
	}
}

func keys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
