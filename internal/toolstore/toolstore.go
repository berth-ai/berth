// Package toolstore installs the base tools a user moved off the image's
// pins. Each complete set lives in an agentstore.Store under /opt/agents/tools,
// is named by the tools it holds, is built by mise as agent and is sealed
// read-only. Its wrappers directory leads the image's /opt/mise/wrappers on
// PATH, so only the tools in the set change; the rest stay the image's.
package toolstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/berth-ai/berth/internal/agentstore"
	"github.com/berth-ai/berth/internal/locksource"
	"github.com/berth-ai/berth/internal/runner"
)

// Root is the tools store inside the agents volume.
const Root = "/opt/agents/tools"

// WrappersDir is the PATH entry of the current set's wrappers. It names no
// directory while no set is current.
const WrappersDir = Root + "/current/wrappers"

// Installer builds complete tool sets.
type Installer struct {
	Store  agentstore.Store
	Mise   string // the mise binary
	Runner runner.Runner
	Agent  *runner.Identity // nil when berth is not root
	Now    func() time.Time
	Logf   func(format string, args ...any)
}

// Hash names a set: the first 12 hex digits of sha256 over its sorted
// name@version lines.
func Hash(tools map[string]string) string {
	h := sha256.New()
	for _, name := range slices.Sorted(maps.Keys(tools)) {
		fmt.Fprintf(h, "%s@%s\n", name, tools[name])
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// Install makes the set holding tools complete and returns it, and whether
// it had to be built. mise records absolute paths, so the set is built in
// place; it counts as complete only once .complete is written last, and a
// failure removes it.
func (in Installer) Install(ctx context.Context, tools map[string]string) (*agentstore.Version, bool, error) {
	if len(tools) == 0 {
		return nil, false, &agentstore.StepError{Step: "prepare", Err: errors.New("a tool set needs at least one tool")}
	}
	hash := Hash(tools)
	if v, err := in.Store.Get(hash); err == nil {
		return v, false, nil
	} else if !errors.Is(err, agentstore.ErrNotFound) {
		return nil, false, &agentstore.StepError{Step: "prepare", Err: err}
	}
	dir := in.Store.VersionDir(hash)
	if err := in.build(ctx, dir, tools); err != nil {
		_ = os.RemoveAll(dir)
		return nil, false, err
	}
	v, err := in.Store.Get(hash)
	if err != nil {
		return nil, false, &agentstore.StepError{Step: "publish", Err: err}
	}
	return v, true, nil
}

func (in Installer) build(ctx context.Context, dir string, tools map[string]string) error {
	if err := in.prepare(dir, tools); err != nil {
		return &agentstore.StepError{Step: "prepare", Err: err}
	}
	in.logf("installing tools %s", describe(tools))
	env := []string{
		"MISE_DATA_DIR=" + filepath.Join(dir, "mise"),
		"MISE_CACHE_DIR=" + filepath.Join(dir, ".cache"),
		"MISE_STATE_DIR=" + filepath.Join(dir, ".state"),
		"MISE_CONFIG_DIR=" + filepath.Join(dir, ".config"),
		"MISE_GLOBAL_CONFIG_FILE=" + filepath.Join(dir, "mise.toml"),
		"MISE_TRUSTED_CONFIG_PATHS=" + dir,
		"MISE_YES=1",
	}
	install := runner.Cmd{Path: in.Mise, Args: []string{"install"}, Dir: dir, Env: env, AsAgent: true}
	if err := in.Runner.Run(ctx, install); err != nil {
		return &agentstore.StepError{Step: "install", Err: err}
	}
	var paths bytes.Buffer
	binPaths := runner.Cmd{Path: in.Mise, Args: []string{"bin-paths"}, Dir: dir, Env: env, AsAgent: true, Stdout: &paths}
	if err := in.Runner.Run(ctx, binPaths); err != nil {
		return &agentstore.StepError{Step: "install", Err: err}
	}
	for _, scratch := range []string{".cache", ".state", ".config"} {
		if err := os.RemoveAll(filepath.Join(dir, scratch)); err != nil {
			return &agentstore.StepError{Step: "install", Err: err}
		}
	}
	bins, err := WriteWrappers(filepath.Join(dir, "wrappers"), dir, strings.Fields(paths.String()))
	if err != nil {
		return &agentstore.StepError{Step: "seal", Err: err}
	}
	if len(bins) == 0 {
		return &agentstore.StepError{Step: "install", Err: errors.New("mise installed no executables")}
	}
	if err := agentstore.Seal(dir, in.Agent != nil); err != nil {
		return &agentstore.StepError{Step: "seal", Err: err}
	}
	if err := reinode(dir); err != nil {
		return &agentstore.StepError{Step: "seal", Err: err}
	}
	m := agentstore.Manifest{Packages: maps.Clone(tools), InstalledAt: in.Now().UTC()}
	if err := writeJSON(filepath.Join(dir, ".complete"), m); err != nil {
		return &agentstore.StepError{Step: "complete", Err: err}
	}
	return nil
}

// prepare writes the mise config and source into a fresh dir owned by agent.
func (in Installer) prepare(dir string, tools map[string]string) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var cfg strings.Builder
	cfg.WriteString("# Base tools picked over the image's pins; written by berth update.\n[tools]\n")
	for _, name := range slices.Sorted(maps.Keys(tools)) {
		fmt.Fprintf(&cfg, "%s = %s\n", strconv.Quote(name), strconv.Quote(tools[name]))
	}
	if err := os.WriteFile(filepath.Join(dir, "mise.toml"), []byte(cfg.String()), 0o644); err != nil {
		return err
	}
	src := locksource.Source{Origin: locksource.OriginUpstream, FetchedAt: in.Now().UTC()}
	if err := writeJSON(filepath.Join(dir, "source.json"), src); err != nil {
		return err
	}
	if in.Agent == nil {
		return nil
	}
	uid, gid := int(in.Agent.UID), int(in.Agent.GID)
	return filepath.WalkDir(dir, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Lchown(p, uid, gid)
	})
}

// WriteWrappers writes an exec wrapper in out for every executable in the
// bin dirs, as the image's mise-wrappers does. Every bin dir must lie inside
// root; a name provided twice fails. It returns the names, sorted.
func WriteWrappers(out, root string, binDirs []string) ([]string, error) {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, err
	}
	var names []string
	for _, bin := range binDirs {
		bin = filepath.Clean(bin)
		if !strings.HasPrefix(bin, filepath.Clean(root)+string(filepath.Separator)) {
			return nil, fmt.Errorf("mise named a bin directory outside the set: %s", bin)
		}
		entries, err := os.ReadDir(bin)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			target := filepath.Join(bin, e.Name())
			info, err := os.Stat(target)
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
				continue
			}
			wrapper := filepath.Join(out, e.Name())
			if _, err := os.Lstat(wrapper); err == nil {
				return nil, fmt.Errorf("%s is provided by more than one tool (second: %s)", e.Name(), target)
			}
			script := fmt.Sprintf("#!/bin/sh\nexec %s \"$@\"\n", shellQuote(target))
			if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
				return nil, err
			}
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	return names, nil
}

// reinode replaces every regular file in the sealed dir with a fresh copy.
// agent built the tree, and a descriptor it opened then would still write
// to the file after Seal; it now writes to an orphan instead. Files open
// with O_NOFOLLOW, so a name agent swapped for a symlink is refused.
func reinode(dir string) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		src, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return err
		}
		defer src.Close()
		dst, err := os.CreateTemp(filepath.Dir(p), ".reinode-*")
		if err != nil {
			return err
		}
		_, err = io.Copy(dst, src)
		if err == nil {
			err = dst.Chmod(info.Mode().Perm())
		}
		if cerr := dst.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Rename(dst.Name(), p)
		}
		if err != nil {
			_ = os.Remove(dst.Name())
		}
		return err
	})
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func describe(tools map[string]string) string {
	parts := make([]string, 0, len(tools))
	for _, name := range slices.Sorted(maps.Keys(tools)) {
		parts = append(parts, name+"@"+tools[name])
	}
	return strings.Join(parts, " ")
}

func (in Installer) logf(format string, args ...any) {
	if in.Logf != nil {
		in.Logf(format, args...)
	}
}

func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}
