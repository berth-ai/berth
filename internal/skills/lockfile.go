package skills

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/berth-ai/berth/internal/config"
)

// LockFileName is the project lock file of the vercel-labs skills CLI
// (npx skills): the skills a repository asks for, each pinned to where it
// came from and hashed as installed in .agents/skills/<name>.
const LockFileName = "skills-lock.json"

const maxLockBytes = 1 << 20

// lockEntry is one skill of skills-lock.json, as the skills CLI writes it.
type lockEntry struct {
	Source       string `json:"source"`
	SourceURL    string `json:"sourceUrl"`
	Ref          string `json:"ref"`
	Commit       string `json:"commit"`
	SourceType   string `json:"sourceType"`
	SkillPath    string `json:"skillPath"`
	SkillName    string `json:"skillName"`
	ComputedHash string `json:"computedHash"`
}

// The states of a lock's skill in the workspace's .agents/skills.
const (
	LockAbsent     = "absent"     // no folder
	LockMatch      = "match"      // its files hash to the lock's computedHash
	LockDiffers    = "differs"    // they do not
	LockUnverified = "unverified" // there is a folder but no hash to compare
)

// LockSkill is one skill of a workspace's lock as Berth reads it: the
// source Berth would add for it (Repo empty, with Reason, when it cannot)
// and what the workspace already holds.
type LockSkill struct {
	Name       string `json:"name"`
	Source     string `json:"source"`
	SourceType string `json:"source_type"`
	Kind       string `json:"kind,omitempty"` // the Berth source's kind
	Repo       string `json:"repo,omitempty"` // a repository URL, or a website for a well-known skill
	Ref        string `json:"ref,omitempty"`
	Commit     string `json:"commit,omitempty"`
	Folder     string `json:"folder"` // the skill's folder in the repository; "" is the root or unknown
	Installed  string `json:"installed"`
	Reason     string `json:"reason,omitempty"`
}

// Lock is what ReadLock found; Found is false when the workspace has no
// skills-lock.json.
type Lock struct {
	Found   bool        `json:"found"`
	Version int         `json:"version,omitempty"`
	Skills  []LockSkill `json:"skills"`
}

// ReadLock reads skills-lock.json at the root of workspace folder ws and
// checks each skill against ws/.agents/skills/<name>. A lock this cannot
// use (merge conflict markers, an unknown version) is an error.
func ReadLock(ws string) (Lock, error) {
	out := Lock{Skills: []LockSkill{}}
	p := filepath.Join(ws, LockFileName)
	fi, err := os.Lstat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return out, nil
	case err != nil:
		return out, err
	case !fi.Mode().IsRegular():
		return out, fmt.Errorf("%s is not a file", LockFileName)
	}
	f, err := os.Open(p)
	if err != nil {
		return out, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxLockBytes+1))
	if err != nil {
		return out, err
	}
	if len(raw) > maxLockBytes {
		return out, fmt.Errorf("%s is larger than 1 MiB", LockFileName)
	}
	var doc struct {
		Version int                  `json:"version"`
		Skills  map[string]lockEntry `json:"skills"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Skills == nil {
		return out, fmt.Errorf("%s is not a skills lock file; resolve any merge conflict in it", LockFileName)
	}
	if doc.Version != 1 && doc.Version != 2 {
		return out, fmt.Errorf("%s has version %d; Berth reads versions 1 and 2", LockFileName, doc.Version)
	}
	if len(doc.Skills) > MaxScan {
		return out, fmt.Errorf("%s lists more than %d skills", LockFileName, MaxScan)
	}
	out.Found, out.Version = true, doc.Version
	for _, name := range slices.Sorted(func(yield func(string) bool) {
		for k := range doc.Skills {
			if !yield(k) {
				return
			}
		}
	}) {
		e := doc.Skills[name]
		s := lockSkill(name, e)
		s.Installed = installed(ws, name, e.ComputedHash, doc.Version)
		out.Skills = append(out.Skills, s)
	}
	return out, nil
}

// lockSkill maps entry e of skill name to the source Berth would add.
func lockSkill(name string, e lockEntry) LockSkill {
	s := LockSkill{Name: name, Source: e.Source, SourceType: e.SourceType}
	if config.ValidCommit(e.Commit) {
		s.Commit = e.Commit
	}
	switch {
	case !config.SkillIDPattern.MatchString(name):
		s.Reason = fmt.Sprintf("%q is not a skill name", name)
		return s
	case e.SkillName != "" && e.SkillName != name:
		s.Reason = "It was installed under another name with --rename; Berth installs skills under their own name"
		return s
	}
	switch e.SourceType {
	case "github", "gitlab", "git":
		s.Repo = repoOf(e)
		if msg := config.CheckSkillRepo(s.Repo); s.Repo == "" || msg != "" {
			s.Repo, s.Reason = "", "Its repository is not an https URL Berth can fetch"
			return s
		}
		s.Ref = e.Ref
		if s.Ref == "" {
			s.Ref = "HEAD"
		}
		if !config.ValidGitRef(s.Ref) {
			s.Repo, s.Reason = "", fmt.Sprintf("Its ref %q is not a branch, tag or commit", e.Ref)
			return s
		}
		folder := strings.TrimSuffix(strings.TrimSuffix(e.SkillPath, "SKILL.md"), "/")
		if !config.ValidSkillPath(folder) {
			s.Repo, s.Reason = "", fmt.Sprintf("Its skillPath %q is not a folder of the repository", e.SkillPath)
			return s
		}
		s.Folder = folder
	case "well-known":
		s.Kind, s.Repo = config.KindWellKnown, cmpOr(e.SourceURL, e.Source)
		if msg := config.CheckSkillRepo(s.Repo); msg != "" {
			s.Kind, s.Repo, s.Reason = "", "", "Its website is not an https URL"
		}
	default:
		s.Reason = fmt.Sprintf("Berth cannot add skills from a %s source", cmpOr(e.SourceType, "unnamed"))
	}
	return s
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// repoOf is the repository URL of a git entry: its sourceUrl when that is
// one, else owner/repo on the forge its sourceType names.
func repoOf(e lockEntry) string {
	if u, err := url.Parse(e.SourceURL); err == nil && u.Scheme == "https" && u.Host != "" {
		return strings.TrimSuffix(e.SourceURL, ".git")
	}
	if u, err := url.Parse(e.Source); err == nil && u.Scheme == "https" && u.Host != "" {
		return strings.TrimSuffix(e.Source, ".git")
	}
	owner, repo, ok := strings.Cut(strings.TrimSuffix(e.Source, ".git"), "/")
	if !ok || owner == "" || repo == "" || strings.ContainsAny(owner, ":@ ") {
		return ""
	}
	switch e.SourceType {
	case "github":
		return "https://github.com/" + owner + "/" + repo
	case "gitlab":
		return "https://gitlab.com/" + owner + "/" + repo
	}
	return ""
}

// installed says what ws/.agents/skills/name holds against hash. A
// version 1 lock's hash used another algorithm, so it is not compared.
func installed(ws, name, hash string, version int) string {
	if !config.SkillIDPattern.MatchString(name) {
		return LockAbsent
	}
	dir := filepath.Join(ws, ".agents", "skills", name)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return LockAbsent
	}
	if version < 2 || hash == "" {
		return LockUnverified
	}
	got, err := ComputedHash(dir)
	if err != nil {
		return LockUnverified
	}
	if got == hash {
		return LockMatch
	}
	return LockDiffers
}

// ComputedHash is the skills CLI's computedHash of the skill folder dir
// (skills-lock.json version 2): every file but metadata.json, outside
// .git, __pycache__ and __pypackages__, a symlink counted as what it points
// to inside the folder; each framed as path NUL length NUL content, with
// CRLF made LF in text, in byte order of path, into one SHA-256.
func ComputedHash(dir string) (string, error) {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	type file struct {
		path string
		data []byte
	}
	var files []file
	var total int64
	var walk func(dir, prefix string, ancestors map[string]bool) error
	walk = func(dir, prefix string, ancestors map[string]bool) error {
		real, err := filepath.EvalSymlinks(dir)
		if err != nil || ancestors[real] {
			return nil
		}
		branch := map[string]bool{real: true}
		for a := range ancestors {
			branch[a] = true
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			name, full := e.Name(), filepath.Join(dir, e.Name())
			isDir := e.IsDir()
			if excludedName(name, isDir) {
				continue
			}
			if e.Type()&fs.ModeSymlink != 0 {
				target, err := filepath.EvalSymlinks(full)
				if err != nil || (target != root && !strings.HasPrefix(target, root+string(filepath.Separator))) {
					continue
				}
				fi, err := os.Stat(target)
				if err != nil {
					continue
				}
				if isDir = fi.IsDir(); isDir && excludedName(name, true) {
					continue
				}
			} else if !isDir && !e.Type().IsRegular() {
				continue
			}
			if isDir {
				if err := walk(full, prefix+name+"/", branch); err != nil {
					return err
				}
				continue
			}
			if len(files) >= maxSkillFiles {
				return fmt.Errorf("the skill has more than %d files", maxSkillFiles)
			}
			data, err := os.ReadFile(full)
			if err != nil {
				return err
			}
			if total += int64(len(data)); total > maxSkillBytes {
				return fmt.Errorf("the skill is larger than %d MiB", maxSkillBytes>>20)
			}
			files = append(files, file{path: prefix + name, data: data})
		}
		return nil
	}
	if err := walk(dir, "", map[string]bool{}); err != nil {
		return "", err
	}
	slices.SortFunc(files, func(a, b file) int { return strings.Compare(a.path, b.path) })
	h := sha256.New()
	for _, f := range files {
		data := f.data
		if bytes.IndexByte(data, 0) < 0 {
			data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
		}
		h.Write([]byte(f.path))
		h.Write([]byte{0})
		h.Write([]byte(strconv.Itoa(len(data))))
		h.Write([]byte{0})
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// excludedName reports whether the skills CLI leaves out an entry: a
// metadata.json file, or a .git, __pycache__ or __pypackages__ folder.
func excludedName(name string, isDir bool) bool {
	return name == "metadata.json" || isDir && (name == ".git" || name == "__pycache__" || name == "__pypackages__")
}
