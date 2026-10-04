package cli

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/berth-ai/berth/internal/agentset"
	"github.com/berth-ai/berth/internal/agentstore"
	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/output"
	"github.com/berth-ai/berth/internal/testutil"
)

func TestAgentSetFollowsPublishedList(t *testing.T) {
	cfg := testConfig(t)
	cfg.ConfigDir = t.TempDir()
	home := t.TempDir()
	testutil.AddedInstall(t, agentset.StoreRoot(home), "copilot", "0123456789ab", "@github/copilot", "1.0.91",
		testutil.CopilotPackageJSON, "copilot")
	a := &app{cfg: cfg, store: agentstore.Store{Root: cfg.AgentsDir}, p: output.Printer{Stderr: io.Discard}}
	if set, err := a.agentSet(home); err != nil || len(set.Packages) != 0 {
		t.Fatalf("unlisted install: %v %v", set.Names(), err)
	}
	if err := config.WriteAgentsFile(filepath.Join(cfg.ConfigDir, "agents.json"), []string{"@github/copilot"}, -1, -1); err != nil {
		t.Fatal(err)
	}
	set, err := a.agentSet(home)
	if err != nil || set.Packages["@github/copilot"].Origin != agentset.OriginAdded {
		t.Errorf("listed install: %v %v", set.Names(), err)
	}
}
