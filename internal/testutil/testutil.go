// Package testutil provides fixtures and fake npm/node for Berth's Go tests.
// The fakes are the test binary itself, re-executed through wrapper scripts.
package testutil

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const fakeToolEnv = "BERTH_FAKE_TOOL"

// RunFakeToolIfRequested turns the process into a fake npm or node when a
// FakeTools wrapper started it. Call it first in TestMain.
func RunFakeToolIfRequested() {
	tool := os.Getenv(fakeToolEnv)
	if tool == "" {
		return
	}
	code := 127
	switch tool {
	case "npm":
		code = fakeNPM(os.Args[1:])
	case "node":
		code = fakeNode(os.Args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown fake tool %q\n", tool)
	}
	os.Exit(code)
}

// FakeTools puts fake npm and node first on PATH for the rest of the test.
func FakeTools(t *testing.T) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, tool := range []string{"npm", "node"} {
		script := fmt.Sprintf("#!/bin/sh\n%s=%s exec %s \"$@\"\n", fakeToolEnv, tool, shellQuote(exe))
		if err := os.WriteFile(filepath.Join(dir, tool), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// LockDir returns the absolute path of a fixture under testutil/testdata.
func LockDir(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", name)
}

// MakeVersion writes a minimal complete version directory and returns it.
func MakeVersion(t *testing.T, root, hash, origin string, installedAt time.Time) string {
	t.Helper()
	dir := filepath.Join(root, "versions", hash)
	must(t, os.MkdirAll(dir, 0o755))
	writeJSON(t, filepath.Join(dir, "source.json"), map[string]any{"origin": origin, "fetched_at": installedAt})
	writeJSON(t, filepath.Join(dir, ".complete"), map[string]any{
		"packages": map[string]string{"fake-agent": "1.0.0"}, "installed_at": installedAt,
	})
	return dir
}

// Link points root/name at versions/hash.
func Link(t *testing.T, root, name, hash string) {
	t.Helper()
	must(t, os.Symlink(filepath.Join("versions", hash), filepath.Join(root, name)))
}

func writeJSON(t *testing.T, p string, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	must(t, err)
	must(t, os.WriteFile(p, raw, 0o644))
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// fakeNPM implements `npm ci`: it creates node_modules/<dep> for every
// dependency with a sh "cli.js" bin that prints its name, version and
// PLAYWRIGHT_BROWSERS_PATH.
func fakeNPM(args []string) int {
	if os.Getenv("BERTH_FAKE_NPM_FAIL") != "" {
		fmt.Fprintln(os.Stderr, "fake npm: failing on request")
		return 1
	}
	if len(args) == 0 || args[0] != "ci" {
		fmt.Fprintf(os.Stderr, "fake npm: unsupported arguments %q\n", args)
		return 2
	}
	raw, err := os.ReadFile("package.json")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var pkg struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for name, version := range pkg.Dependencies {
		if err := writePackage(filepath.Join("node_modules", name), name, version, true); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	if v, ok := pkg.Dependencies["playwright"]; ok {
		if err := writePackage(filepath.Join("node_modules", "playwright-core"), "playwright-core", v, false); err != nil {
			return 1
		}
	}
	if os.Getenv("BERTH_FAKE_DUPLICATE_CORE") != "" {
		nested := filepath.Join("node_modules", "fake-agent", "node_modules", "playwright-core")
		if err := writePackage(nested, "playwright-core", "0.0.1", false); err != nil {
			return 1
		}
	}
	return 0
}

func writePackage(dir, name, version string, withBin bool) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	manifest := map[string]any{"name": name, "version": version}
	if withBin {
		manifest["bin"] = map[string]string{path.Base(name): "cli.js"}
		script := fmt.Sprintf("#!/bin/sh\necho \"%s %s browsers=$PLAYWRIGHT_BROWSERS_PATH\"\n", name, version)
		if err := os.WriteFile(filepath.Join(dir, "cli.js"), []byte(script), 0o755); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "package.json"), raw, 0o644)
}

// fakeNode implements `node .../playwright/cli.js install chromium` and
// `node -e <script> <playwright dir>`.
func fakeNode(args []string) int {
	switch {
	case len(args) >= 3 && filepath.Base(args[0]) == "cli.js" && args[1] == "install" && args[2] == "chromium":
		if os.Getenv("BERTH_FAKE_PLAYWRIGHT_FAIL") != "" {
			fmt.Fprintln(os.Stderr, "fake playwright: download failed")
			return 1
		}
		if err := os.MkdirAll(filepath.Join(os.Getenv("PLAYWRIGHT_BROWSERS_PATH"), "chromium-fake"), 0o755); err != nil {
			return 1
		}
		return 0
	case len(args) >= 1 && args[0] == "-e":
		if os.Getenv("BERTH_FAKE_CHROMIUM_FAIL") != "" {
			fmt.Fprintln(os.Stderr, "fake chromium: launch failed")
			return 1
		}
		return 0
	}
	fmt.Fprintf(os.Stderr, "fake node: unsupported arguments %q\n", args)
	return 2
}

// CopilotPackageJSON is a registry entry's package.json shaped like
// agents/registry/copilot's: one agent with a key, an MCP file, a skills
// directory, an icon and a device sign-in.
const CopilotPackageJSON = `{"name":"berth-registry-copilot","private":true,
  "dependencies":{"@github/copilot":"1.0.91"},
  "berth":{"@github/copilot":{"kind":"agent","name":"GitHub Copilot CLI",
    "description":"GitHub's coding agent, signed in with your GitHub account","keys":["COPILOT_GITHUB_TOKEN"]}},
  "berthAgents":{"schema":1,"agents":{"@github/copilot":{
    "mcp":{"file":"~/.copilot/mcp-config.json","style":"claude","key":"mcpServers"},
    "skills":{"dir":"~/.copilot/skills"},
    "icon":"githubcopilot",
    "login":{"methods":[{"id":"github","label":"GitHub","flow":"device","args":["login","--device-code"],
      "status":{"file":"~/.copilot/config.json","key":"authTokens"},
      "logout":{"remove":["~/.copilot/config.json"]}}]}}}}}`

// RegistryEntry writes root/id as a registry entry: pkgJSON as its
// package.json and a minimal package-lock.json whose root dependencies
// match it.
func RegistryEntry(t *testing.T, root, id, pkgJSON string) {
	t.Helper()
	var p struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	must(t, json.Unmarshal([]byte(pkgJSON), &p))
	dir := filepath.Join(root, id)
	must(t, os.MkdirAll(dir, 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o644))
	writeJSON(t, filepath.Join(dir, "package-lock.json"), map[string]any{
		"lockfileVersion": 3,
		"packages":        map[string]any{"": map[string]any{"dependencies": p.Dependencies}},
	})
}
