package agentlogin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/berth-ai/berth/internal/agentset"
	"github.com/berth-ai/berth/internal/locksource"
)

// fakeCLI is the bin of agent "a". Its status signs in when $HOME/signed
// exists; logout removes that file; hang never answers.
const fakeCLI = `#!/bin/sh
case "$1" in
status) [ -f "$HOME/signed" ] || exit 1; echo '{"email": "you@example.org"}' ;;
logout) rm -f "$HOME/signed" ;;
stuck-logout) exit 0 ;;
hang) sleep 30 ;;
esac
`

type fixture struct {
	env  Env
	home string
	root string
}

// newFixture installs an agent set with agents a (enabled, bin a-cli) and
// b (installed but switched off), both described by login, a JSON
// methods list. script becomes the agents' bin.
func newFixture(t *testing.T, login, script string) fixture {
	t.Helper()
	root := t.TempDir()
	set, bins, layer, home := filepath.Join(root, "set"), filepath.Join(root, "bin"), filepath.Join(root, "layer"), filepath.Join(root, "home")
	for _, d := range []string{set, bins, layer, home} {
		must(t, os.MkdirAll(d, 0o755))
	}
	pkg := `{"dependencies":{"a":"1.0.0","b":"1.0.0"},
	  "berth":{"a":{"kind":"agent","name":"Agent A","keys":[]},"b":{"kind":"agent","name":"Agent B","keys":[]}},
	  "berthAgents":{"schema":1,"agents":{"a":{"login":` + login + `},"b":{"login":` + login + `}}}}`
	must(t, os.WriteFile(filepath.Join(set, "package.json"), []byte(pkg), 0o644))
	for _, dep := range []string{"a", "b"} {
		dir := filepath.Join(set, "node_modules", dep)
		must(t, os.MkdirAll(dir, 0o755))
		must(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"`+dep+`","bin":{"`+dep+`-cli":"cli.js"}}`), 0o644))
		must(t, os.WriteFile(filepath.Join(bins, dep+"-cli"), []byte(script), 0o755))
	}
	must(t, os.WriteFile(filepath.Join(layer, "a-cli"), []byte("#!/bin/sh\n"), 0o755)) // only a is enabled
	lockedSet, err := agentset.Load(&agentset.Locked{Dir: set, Link: root}, "", nil)
	must(t, err)
	return fixture{
		env: Env{Set: lockedSet, Layer: layer, Dirs: locksource.HomeDirs{Home: home},
			Environ: []string{"HOME=" + home, "PATH=" + os.Getenv("PATH")}},
		home: home, root: root,
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// argsLogin is one device method whose status and logout run fakeCLI.
const argsLogin = `{"methods":[{"id":"m","label":"M","flow":"device","args":["login"],
  "status":{"args":["status"],"account":"\"email\": \"([^\"]+)\""},"logout":{"args":["logout"]}}]}`
