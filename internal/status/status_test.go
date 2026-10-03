package status

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/salgnt/berth/internal/agentstore"
	"github.com/salgnt/berth/internal/testutil"
)

func TestUserTools(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("[tools]\nterraform = \"1.9.0\"\npython = [\"3.12\", \"3.13\"]\ngo = { version = \"1.27\" }\n"), 0o644)
	tools, err := UserTools(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []Tool{{"go", "1.27"}, {"python", "3.12, 3.13"}, {"terraform", "1.9.0"}}
	if len(tools) != 3 || tools[0] != want[0] || tools[1] != want[1] || tools[2] != want[2] {
		t.Fatalf("got %v", tools)
	}
	if tools, err := UserTools(filepath.Join(t.TempDir(), "missing.toml")); err != nil || tools == nil || len(tools) != 0 {
		t.Fatalf("missing file: %v %v", tools, err)
	}
}

func TestCollectEmptyStore(t *testing.T) {
	root := t.TempDir()
	rep, err := Collect(Inputs{
		Store:      agentstore.Store{Root: filepath.Join(root, "agents")},
		SeedDir:    testutil.LockDir("lock-v1"),
		BuildInfo:  filepath.Join(root, "missing.json"),
		MiseConfig: filepath.Join(root, "missing.toml"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Current != nil || rep.Seed == nil || !rep.FollowImage || rep.Build != nil || rep.UserTools == nil {
		t.Fatalf("report %+v", rep)
	}
}

func TestCollectWarnsOnBrokenUserConfig(t *testing.T) {
	root := t.TempDir()
	bad := filepath.Join(root, "config.toml")
	os.WriteFile(bad, []byte("[tools\n"), 0o644)
	rep, err := Collect(Inputs{Store: agentstore.Store{Root: root}, SeedDir: root, BuildInfo: bad, MiseConfig: bad})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Warnings) != 2 { // seed dir without lock files, broken mise config
		t.Fatalf("warnings %v", rep.Warnings)
	}
}

func TestWriteHuman(t *testing.T) {
	root := t.TempDir()
	s := agentstore.Store{Root: root}
	testutil.MakeVersion(t, root, "aaaaaaaaaaaa", "image", time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC))
	s.Flip("aaaaaaaaaaaa")
	rep, _ := Collect(Inputs{Store: s, SeedDir: testutil.LockDir("lock-v1")})
	var out bytes.Buffer
	WriteHuman(&out, rep)
	for _, want := range []string{"current   aaaaaaaaaaaa  image", "fake-agent 1.0.0", "previous  none", "following the image seed"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in:\n%s", want, out.String())
		}
	}
}

func TestCollectListsVersions(t *testing.T) {
	root := t.TempDir()
	old := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	testutil.MakeVersion(t, root, "aaaaaaaaaaaa", "image", old)
	testutil.MakeVersion(t, root, "bbbbbbbbbbbb", "ref", old.Add(time.Hour))
	testutil.Link(t, root, "current", "bbbbbbbbbbbb")
	rep, err := Collect(Inputs{Store: agentstore.Store{Root: root}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Versions) != 2 || rep.Versions[0].Hash != "bbbbbbbbbbbb" || rep.Versions[1].Hash != "aaaaaaaaaaaa" {
		t.Errorf("versions = %+v", rep.Versions)
	}
	empty, _ := Collect(Inputs{Store: agentstore.Store{Root: t.TempDir()}})
	if empty.Versions == nil {
		t.Error("versions must be an empty list, not null")
	}
}
