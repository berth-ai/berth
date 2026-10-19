package wsstate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/berth-ai/berth/internal/config"
)

// newStore returns a store whose workspaces.json publishes ws as "api".
func newStore(t *testing.T, ws string) Store {
	t.Helper()
	s := Store{Home: t.TempDir(), Published: filepath.Join(t.TempDir(), "workspaces.json")}
	publish(t, s, map[string]string{"api": ws})
	return s
}

func publish(t *testing.T, s Store, ws map[string]string) {
	t.Helper()
	pub := map[string]config.PublishedWorkspace{}
	for id, p := range ws {
		pub[id] = config.PublishedWorkspace{Path: p}
	}
	if err := config.WriteWorkspacesFile(s.Published, pub, -1, -1); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateWritesStateAndExclude(t *testing.T) {
	ws := gitRepo(t)
	s := newStore(t, ws)
	err := s.Update("api", func(got string, st *State) error {
		if got != ws {
			t.Errorf("folder = %q", got)
		}
		st.Skills.Path = ws
		st.Skills.Entries["fx/pdf"] = Skill{Name: "pdf", Mode: config.ModeLink, Links: []string{".claude/skills/pdf"}}
		st.Skills.Entries["fx/docx"] = Skill{Name: "docx", Mode: config.ModeVendor, Dir: ".agents/skills/docx", Links: []string{".claude/skills/docx"}}
		st.MCP.Path = ws
		st.MCP.Files[".mcp.json"] = MCPFile{Style: "claude", Key: "mcpServers", IDs: []string{"c7"}, Created: true}
		st.MCP.Files["opencode.json"] = MCPFile{Style: "opencode", Key: "mcp", IDs: []string{"c7"}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := BlockStart + "\n/.claude/skills/pdf\n/.mcp.json\n" + BlockEnd + "\n"
	if got := read(t, filepath.Join(ws, ".git", "info", "exclude")); !strings.HasSuffix(got, want) {
		t.Errorf("exclude = %q", got)
	}
	if fi, err := os.Stat(filepath.Join(s.Dir(), "api.json")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("state file: %v %v", fi, err)
	}
	st, err := s.Load("api")
	if err != nil || st.Skills.Entries["fx/pdf"].Mode != config.ModeLink || !st.MCP.Files[".mcp.json"].Created {
		t.Errorf("loaded = %+v, %v", st, err)
	}
	if ids, _ := s.IDs(); !slices.Equal(ids, []string{"api"}) {
		t.Errorf("ids = %v", ids)
	}
}

func TestUpdateGoneAndBadID(t *testing.T) {
	s := newStore(t, t.TempDir())
	if err := s.Update("web", func(string, *State) error { return nil }); !errors.Is(err, ErrGone) {
		t.Errorf("unpublished = %v", err)
	}
	if err := s.Update("../x", func(string, *State) error { return nil }); err == nil {
		t.Error("a bad id was accepted")
	}
}

func TestReleaseDeletesEmptyState(t *testing.T) {
	ws := gitRepo(t)
	s := newStore(t, ws)
	s.Update("api", func(_ string, st *State) error {
		st.Skills.Path = ws
		st.Skills.Entries["fx/pdf"] = Skill{Name: "pdf", Mode: config.ModeLink, Links: []string{".claude/skills/pdf"}}
		st.MCP.Path = ws
		st.MCP.Files[".mcp.json"] = MCPFile{Style: "claude", IDs: []string{"c7"}, Created: true}
		return nil
	})
	publish(t, s, map[string]string{})
	// Releasing one section keeps the other and its exclude line.
	if err := s.Release("api", func(st *State) { st.Skills = Skills{} }); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(ws, ".git", "info", "exclude")); strings.Contains(got, "pdf") || !strings.Contains(got, "/.mcp.json") {
		t.Errorf("exclude after skills release = %q", got)
	}
	if err := s.Release("api", func(st *State) { st.MCP = MCP{} }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), "api.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("state file kept: %v", err)
	}
	if got := read(t, filepath.Join(ws, ".git", "info", "exclude")); strings.Contains(got, BlockStart) {
		t.Errorf("block kept: %q", got)
	}
	// Releasing a workspace whose folder is gone still drops its state.
	gone := filepath.Join(t.TempDir(), "gone")
	publish(t, s, map[string]string{"old": gone})
	s.Update("old", func(_ string, st *State) error { st.Skills.Path = gone; st.Skills.Error = "missing"; return nil })
	if err := s.Release("old", func(st *State) { *st = State{} }); err != nil {
		t.Errorf("release of a missing folder = %v", err)
	}
	if ids, _ := s.IDs(); len(ids) != 0 {
		t.Errorf("ids = %v", ids)
	}
}

func TestConcurrentSections(t *testing.T) {
	ws := gitRepo(t)
	s := newStore(t, ws)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			s.Update("api", func(_ string, st *State) error {
				st.Skills.Path = ws
				st.Skills.Entries[fmt.Sprintf("fx/s%d", i)] = Skill{Name: fmt.Sprintf("s%d", i), Mode: config.ModeLink,
					Links: []string{fmt.Sprintf(".claude/skills/s%d", i)}}
				return nil
			})
		})
		wg.Go(func() {
			s.Update("api", func(_ string, st *State) error {
				st.MCP.Path = ws
				st.MCP.Files[fmt.Sprintf("f%d.json", i)] = MCPFile{Style: "claude", IDs: []string{"x"}, Created: true}
				return nil
			})
		})
	}
	wg.Wait()
	st, _ := s.Load("api")
	if len(st.Skills.Entries) != 20 || len(st.MCP.Files) != 20 {
		t.Fatalf("lost entries: %d skills, %d files", len(st.Skills.Entries), len(st.MCP.Files))
	}
	got := read(t, filepath.Join(ws, ".git", "info", "exclude"))
	if strings.Count(got, "/.claude/skills/s") != 20 || strings.Count(got, ".json\n") != 20 || strings.Count(got, BlockStart) != 1 {
		t.Errorf("exclude = %s", got)
	}
}

func TestSectionsInOtherFolders(t *testing.T) {
	old, now := gitRepo(t), gitRepo(t)
	st := State{Skills: Skills{Path: old, Entries: map[string]Skill{"fx/pdf": {Name: "pdf", Mode: config.ModeLink, Links: []string{".claude/skills/pdf"}}}},
		MCP: MCP{Path: now, Files: map[string]MCPFile{".mcp.json": {IDs: []string{"x"}, Created: true}}}}
	if got := st.Exclude(old); !slices.Equal(got, []string{"/.claude/skills/pdf"}) {
		t.Errorf("old = %v", got)
	}
	if got := st.Exclude(now); !slices.Equal(got, []string{"/.mcp.json"}) {
		t.Errorf("now = %v", got)
	}
	if !(Skills{}).Empty() || (Skills{Error: "x"}).Empty() || !(MCP{Trust: true}).Empty() {
		t.Error("Empty")
	}
}

func TestInstructionsSection(t *testing.T) {
	ws := gitRepo(t)
	s := newStore(t, ws)
	err := s.Update("api", func(folder string, st *State) error {
		st.Instructions.Path = folder
		st.Instructions.Links = []string{"CLAUDE.md", "GEMINI.md"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(ws, ".git", "info", "exclude"))
	if !strings.Contains(string(raw), BlockStart+"\n/CLAUDE.md\n/GEMINI.md\n"+BlockEnd) {
		t.Errorf("exclude = %q", raw)
	}
	st, _ := s.Load("api")
	if !slices.Equal(st.Instructions.Links, []string{"CLAUDE.md", "GEMINI.md"}) {
		t.Errorf("loaded = %+v", st.Instructions)
	}
	// Another converger's update keeps the section and its lines.
	s.Update("api", func(_ string, st *State) error { st.MCP.Path = ws; return nil })
	if raw, _ := os.ReadFile(filepath.Join(ws, ".git", "info", "exclude")); !strings.Contains(string(raw), "/CLAUDE.md") {
		t.Errorf("an MCP update dropped the line: %q", raw)
	}
	if err := s.Release("api", func(st *State) { st.Instructions = Instructions{} }); err != nil {
		t.Fatal(err)
	}
	if ids, _ := s.IDs(); len(ids) != 0 {
		t.Errorf("state left: %v", ids)
	}
	if raw, _ := os.ReadFile(filepath.Join(ws, ".git", "info", "exclude")); strings.Contains(string(raw), BlockStart) {
		t.Errorf("block left: %q", raw)
	}
}
