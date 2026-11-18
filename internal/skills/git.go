package skills

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

// Git runs git for skills: HTTPS only, never prompting, against bare cache
// repositories in the agent's home. The user's global git config applies,
// so the gh and glab credential helpers sign private fetches in.
//
// A cache is a shallow, blobless partial clone: it holds the commits it
// was asked for with their trees, and only the file contents something
// read, which are the skill folders. Every git command but a fetch runs
// with all transports off, so nothing fetches a missing object behind
// Berth's back; the fetches that do run batch what is missing.
type Git struct {
	Path      string    // the git binary; empty means git on PATH
	AllowFile bool      // tests only: also allow file:// repositories
	Log       io.Writer // receives git's error output; nil discards it
}

// CacheRoot holds the bare cache repositories.
func CacheRoot(home string) string { return filepath.Join(home, ".cache", "berth", "skills") }

// CacheDir is the bare repository that caches repo.
func CacheDir(home, repo string) string {
	sum := sha256.Sum256([]byte(repo))
	return filepath.Join(CacheRoot(home), hex.EncodeToString(sum[:8])+".git")
}

// cacheLayout marks a cache as a blobless partial clone; a cache without
// it is an older full one and is started over.
const cacheLayout = "2"

// The refs that keep commits in a cache: keepRefs, the commits sources
// pin, written by Keep; latestRef, the commit the last fetch of a ref gave.
const (
	keepRefs  = "refs/berth/keep/"
	latestRef = "refs/berth/latest"
)

// gitError is a failed git command; stderr is what it said.
type gitError struct {
	stderr string
	err    error
}

func (e *gitError) Error() string {
	if e.stderr != "" {
		lines := strings.Split(e.stderr, "\n")
		return lines[len(lines)-1]
	}
	return e.err.Error()
}

// run runs git on the cache at gitDir with every transport off.
func (g Git) run(ctx context.Context, gitDir string, stdout io.Writer, args ...string) error {
	return g.exec(ctx, false, gitDir, nil, stdout, args...)
}

// exec runs git; online allows HTTPS (and file:// in tests).
func (g Git) exec(ctx context.Context, online bool, gitDir string, stdin io.Reader, stdout io.Writer, args ...string) error {
	full := []string{"-c", "protocol.allow=never"}
	if online {
		full = append(full, "-c", "protocol.https.allow=always")
		if g.AllowFile {
			full = append(full, "-c", "protocol.file.allow=always")
		}
	}
	if gitDir != "" {
		full = append(full, "--git-dir="+gitDir)
	}
	cmd := exec.CommandContext(ctx, cmp.Or(g.Path, "git"), append(full, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never")
	var stderr bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, &stderr
	err := cmd.Run()
	if err != nil {
		if g.Log != nil && stderr.Len() > 0 {
			_, _ = g.Log.Write(stderr.Bytes())
		}
		return &gitError{stderr: strings.TrimSpace(stderr.String()), err: err}
	}
	return nil
}

// output runs git offline and returns its trimmed stdout.
func (g Git) output(ctx context.Context, dir string, args ...string) (string, error) {
	var out bytes.Buffer
	err := g.run(ctx, dir, &out, args...)
	return strings.TrimSpace(out.String()), err
}

// probe is output for a question whose "no" is a failed command: what
// git says then goes nowhere.
func (g Git) probe(ctx context.Context, dir string, args ...string) (string, error) {
	g.Log = nil
	return g.output(ctx, dir, args...)
}

// lock takes the cache repository's lock; wait false fails at once when
// another process holds it.
func lock(dir string, wait bool) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(dir+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	how := syscall.LOCK_EX
	if !wait {
		how |= syscall.LOCK_NB
	}
	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// open makes dir a blobless partial clone of repo, starting over a cache
// of an older layout. The caller holds the lock.
func (g Git) open(ctx context.Context, dir, repo string) error {
	if layout, _ := g.probe(ctx, dir, "config", "--get", "berth.cache"); layout == cacheLayout {
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := g.run(ctx, "", io.Discard, "init", "--bare", "-q", dir); err != nil {
		return err
	}
	for _, kv := range [][2]string{
		{"remote.origin.url", repo},
		{"remote.origin.promisor", "true"},
		{"remote.origin.partialclonefilter", "blob:none"},
		{"gc.auto", "0"},
		{"berth.cache", cacheLayout}, // last: a cache cut short is started over
	} {
		if err := g.run(ctx, dir, io.Discard, "config", kv[0], kv[1]); err != nil {
			return err
		}
	}
	return nil
}

// fetch fetches the commit ref names, with its trees but no file
// contents, and returns it. The caller holds the lock.
func (g Git) fetch(ctx context.Context, dir, ref string) (string, error) {
	if err := g.exec(ctx, true, dir, nil, io.Discard, "fetch", "-q", "--depth", "1", "--filter=blob:none",
		"--no-tags", "--no-recurse-submodules", "origin", ref); err != nil {
		return "", err
	}
	return g.output(ctx, dir, "rev-parse", "--verify", "-q", "FETCH_HEAD^{commit}")
}

// has reports whether the cache holds commit; it never fetches.
func (g Git) has(ctx context.Context, dir, commit string) bool {
	_, err := g.probe(ctx, dir, "cat-file", "-e", commit+"^{commit}")
	return err == nil
}

// ensure fetches commit unless the cache holds it. The caller holds the lock.
func (g Git) ensure(ctx context.Context, dir, commit string) error {
	if g.has(ctx, dir, commit) {
		return nil
	}
	got, err := g.fetch(ctx, dir, commit)
	if err != nil {
		return err
	}
	if got != commit {
		return fmt.Errorf("fetching %s gave %s", short(commit), short(got))
	}
	return nil
}

// missing lists the file contents below treeish the cache lacks.
func (g Git) missing(ctx context.Context, dir, treeish string) (map[string]bool, error) {
	var out bytes.Buffer
	if err := g.run(ctx, dir, &out, "rev-list", "--objects", "--missing=print", treeish); err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for line := range strings.Lines(out.String()) {
		if oid, ok := strings.CutPrefix(strings.TrimSpace(line), "?"); ok {
			set[oid] = true
		}
	}
	return set, nil
}

// fetchBlobs fetches the file contents oids names, in one request.
func (g Git) fetchBlobs(ctx context.Context, dir string, oids []string) error {
	if len(oids) == 0 {
		return nil
	}
	slices.Sort(oids)
	stdin := strings.NewReader(strings.Join(slices.Compact(oids), "\n") + "\n")
	return g.exec(ctx, true, dir, stdin, io.Discard, "-c", "fetch.negotiationAlgorithm=noop",
		"fetch", "-q", "--no-tags", "--no-write-fetch-head", "--recurse-submodules=no",
		"--filter=blob:none", "--stdin", "origin")
}

// treeish names sub (the root when empty) at commit.
func treeish(commit, sub string) string {
	if sub == "" {
		return commit + "^{tree}"
	}
	return commit + ":" + sub
}

// treeFile is one file of a tree.
type treeFile struct{ path, oid string }

// files lists the files at commit under paths, or the whole tree when
// paths is empty or holds the root ("").
func (g Git) files(ctx context.Context, dir, commit string, paths []string) ([]treeFile, error) {
	args := []string{"ls-tree", "-r", "-z", "--format=%(objecttype) %(objectname)%x09%(path)", commit}
	if len(paths) > 0 && !slices.Contains(paths, "") {
		args = append(append(args, "--"), paths...)
	}
	var out bytes.Buffer
	if err := g.run(ctx, dir, &out, args...); err != nil {
		return nil, err
	}
	var files []treeFile
	for rec := range strings.SplitSeq(out.String(), "\x00") {
		head, p, ok := strings.Cut(rec, "\t")
		if typ, oid, _ := strings.Cut(head, " "); ok && typ == "blob" {
			files = append(files, treeFile{path: p, oid: oid})
		}
	}
	return files, nil
}

// show returns the content of the file oid names; the caller fetched it.
func (g Git) show(ctx context.Context, dir, oid string) ([]byte, error) {
	var out bytes.Buffer
	if err := g.run(ctx, dir, &out, "cat-file", "blob", oid); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// Export streams the tar of sub (the root when empty) at commit of repo
// to extract, fetching the commit and that folder's files as needed.
// When extract stops early, git is stopped and extract's error returned.
func (g Git) Export(ctx context.Context, dir, repo, commit, sub string, extract func(io.Reader) error) error {
	unlock, err := lock(dir, true)
	if err != nil {
		return err
	}
	defer unlock()
	if err := g.open(ctx, dir, repo); err != nil {
		return err
	}
	if err := g.ensure(ctx, dir, commit); err != nil {
		return fmt.Errorf("fetch %s at %s: %w", repo, short(commit), err)
	}
	tree := treeish(commit, sub)
	lacks, err := g.missing(ctx, dir, tree)
	if err != nil {
		return err
	}
	if err := g.fetchBlobs(ctx, dir, slices.Collect(maps.Keys(lacks))); err != nil {
		return fmt.Errorf("fetch the files of %s at %s: %w", cmp.Or(sub, "the root"), short(commit), err)
	}
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := g.run(ctx, dir, pw, "archive", "--format=tar", tree)
		pw.CloseWithError(err)
		done <- err
	}()
	xerr := extract(pr)
	if xerr == nil {
		_, _ = io.Copy(io.Discard, pr) // the tar reader stops before git's record padding
	}
	pr.CloseWithError(fmt.Errorf("extraction stopped"))
	gerr := <-done
	if xerr != nil {
		return xerr
	}
	return gerr
}

// Keep drops from the cache at dir every commit but those in commits and
// the newest one a scan fetched; a cache of an older layout goes whole.
// A cache another process holds is left for the next call.
func (g Git) Keep(ctx context.Context, dir string, commits []string) error {
	unlock, err := lock(dir, false)
	if err != nil {
		return nil
	}
	defer unlock()
	if layout, _ := g.probe(ctx, dir, "config", "--get", "berth.cache"); layout != cacheLayout {
		return os.RemoveAll(dir)
	}
	var refs bytes.Buffer
	if err := g.run(ctx, dir, &refs, "for-each-ref", "--format=%(refname)", keepRefs); err != nil {
		return err
	}
	want := map[string]bool{}
	for _, c := range commits {
		if g.has(ctx, dir, c) {
			want[keepRefs+c] = true
		}
	}
	var script strings.Builder
	for ref := range strings.Lines(refs.String()) {
		if ref = strings.TrimSpace(ref); !want[ref] {
			fmt.Fprintf(&script, "delete %s\n", ref)
		}
	}
	for ref := range want {
		fmt.Fprintf(&script, "update %s %s\n", ref, strings.TrimPrefix(ref, keepRefs))
	}
	if err := g.exec(ctx, false, dir, strings.NewReader(script.String()), io.Discard, "update-ref", "--stdin"); err != nil {
		return err
	}
	return g.compact(ctx, dir)
}

// compact repacks the cache at dir down to what its refs and extra
// reach, when it holds any other commit. The caller holds the lock.
func (g Git) compact(ctx context.Context, dir string, extra ...string) error {
	tips, err := g.output(ctx, dir, "for-each-ref", "--format=%(objectname)", "refs/berth/")
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, c := range append(strings.Fields(tips), extra...) {
		if c != "" && g.has(ctx, dir, c) {
			keep[c] = true
		}
	}
	shallow, err := os.ReadFile(filepath.Join(dir, "shallow"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	stale := false
	for _, c := range strings.Fields(string(shallow)) {
		stale = stale || !keep[c]
	}
	switch {
	case !stale:
		return nil
	case len(keep) == 0:
		// Nothing to keep: leave the empty layout for the next fetch.
		return g.reset(ctx, dir)
	}
	heads := slices.Sorted(maps.Keys(keep))
	var objects bytes.Buffer
	if err := g.run(ctx, dir, &objects, append([]string{"rev-list", "--objects", "--missing=allow-promisor"}, heads...)...); err != nil {
		return err
	}
	packDir := filepath.Join(dir, "objects", "pack")
	var name bytes.Buffer
	if err := g.exec(ctx, false, dir, &objects, &name, "pack-objects", "-q", filepath.Join(packDir, "pack")); err != nil {
		return err
	}
	kept := "pack-" + strings.TrimSpace(name.String())
	// Everything in a cache came from the repository, so it may refetch it.
	if err := os.WriteFile(filepath.Join(packDir, kept+".promisor"), nil, 0o644); err != nil {
		return err
	}
	// The new pack is complete before any old one goes, so a crash here
	// leaves extra objects, never missing ones.
	entries, err := os.ReadDir(packDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if n := e.Name(); strings.HasPrefix(n, "pack-") && !strings.HasPrefix(n, kept+".") {
			if err := os.Remove(filepath.Join(packDir, n)); err != nil {
				return err
			}
		}
	}
	// prune drops the loose objects and the shallow lines nothing reaches.
	return g.run(ctx, dir, io.Discard, append([]string{"prune", "--expire=now", "--"}, heads...)...)
}

// reset empties the cache at dir, keeping its layout and remote.
func (g Git) reset(ctx context.Context, dir string) error {
	repo, err := g.output(ctx, dir, "config", "--get", "remote.origin.url")
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return g.open(ctx, dir, repo)
}
