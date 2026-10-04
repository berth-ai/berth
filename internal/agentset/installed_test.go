package agentset

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/berth-ai/berth/internal/testutil"
)

func TestInstalled(t *testing.T) {
	root := t.TempDir()
	testutil.AddedInstall(t, root, "copilot", "0123456789ab", "@github/copilot", "1.0.90", testutil.CopilotPackageJSON, "copilot")
	testutil.AddedInstall(t, root, "copilot", "abcdef012345", "@github/copilot", "1.0.91", testutil.CopilotPackageJSON, "copilot")
	store := filepath.Join(root, "copilot")
	os.Remove(filepath.Join(store, "current"))
	testutil.Link(t, store, "current", "abcdef012345")
	testutil.Link(t, store, "previous", "0123456789ab")
	os.MkdirAll(filepath.Join(root, "unfinished", "versions"), 0o755) // no current: skipped
	os.MkdirAll(filepath.Join(root, "Not_An_Id"), 0o755)

	got, problems := Installed(root)
	if len(problems) != 0 || len(got) != 1 {
		t.Fatalf("installed = %+v, problems = %v", got, problems)
	}
	a := got[0]
	if a.ID != "copilot" || a.Package != "@github/copilot" || a.Version != "1.0.91" || a.Hash != "abcdef012345" ||
		a.PreviousVersion != "1.0.90" || !slices.Equal(a.Bins, []string{"copilot"}) ||
		string(a.PackageJSON) != testutil.CopilotPackageJSON {
		t.Errorf("added = %+v", a)
	}
	if len(Pick(got, []string{"@github/copilot"})) != 1 || len(Pick(got, nil)) != 0 {
		t.Error("Pick")
	}
	if got, problems := Installed(filepath.Join(root, "missing")); got != nil || problems != nil {
		t.Errorf("missing root = %v, %v", got, problems)
	}
}

func TestInstalledReportsUnreadableStore(t *testing.T) {
	root := t.TempDir()
	dir := testutil.AddedInstall(t, root, "copilot", "0123456789ab", "@github/copilot", "1.0.91", testutil.CopilotPackageJSON, "copilot")
	os.Remove(filepath.Join(dir, "package.json"))
	if got, problems := Installed(root); len(got) != 0 || len(problems) != 1 {
		t.Errorf("installed = %+v, problems = %v", got, problems)
	}
}

func TestStoreRoot(t *testing.T) {
	if got := StoreRoot("/home/agent"); got != "/home/agent/.local/share/berth/agents" {
		t.Errorf("StoreRoot = %q", got)
	}
}

func TestDecodeReport(t *testing.T) {
	rep, err := DecodeReport([]byte(`{"agents":[{"id":"copilot","package":"@github/copilot","version":"1.0.91",
	  "hash":"0123456789ab","package_json":"e30=","bins":["copilot"]}],"errors":{"x":"y"},"schema":1}`))
	if err != nil || len(rep.Agents) != 1 || string(rep.Agents[0].PackageJSON) != "{}" || rep.Errors["x"] != "y" {
		t.Errorf("report = %+v, %v", rep, err)
	}
	if rep, err := DecodeReport([]byte(`{"schema":1}`)); err != nil || rep.Agents == nil || rep.Errors == nil {
		t.Errorf("empty report = %+v, %v", rep, err)
	}
	if _, err := DecodeReport([]byte("garbage")); err == nil {
		t.Error("garbage decoded")
	}
}
