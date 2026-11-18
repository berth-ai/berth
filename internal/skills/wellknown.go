package skills

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/berth-ai/berth/internal/config"
)

// WellKnown reads the Agent Skills index a website publishes at
// /.well-known/agent-skills/index.json (discovery format 0.2.0) and
// downloads the skills it lists, checking each against its digest.
type WellKnown struct {
	Client *http.Client // nil means HTTPS with a timeout; tests pass a TLS test server's
}

// The 0.2.0 discovery format, and the folders a site may publish it in;
// the second is the older location, which some sites use for 0.2.0 too.
const wellKnownSchema = "https://schemas.agentskills.io/discovery/0.2.0/schema.json"

var wellKnownDirs = []string{".well-known/agent-skills", ".well-known/skills"}

const (
	maxIndexBytes    = 1 << 20  // one index
	maxArtifactBytes = 10 << 20 // one downloaded skill, compressed
	maxArchiveBytes  = 25 << 20 // one archive's files
	maxArchiveFiles  = 1000
	webTimeout       = 30 * time.Second
)

// ArtifactDir holds downloaded well-known skills, named by their digest.
func ArtifactDir(home string) string { return filepath.Join(CacheRoot(home), "artifacts") }

func (w WellKnown) client() *http.Client {
	if w.Client != nil {
		return w.Client
	}
	return &http.Client{Timeout: webTimeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		switch {
		case len(via) >= 5:
			return errors.New("too many redirects")
		case req.URL.Scheme != "https":
			return fmt.Errorf("redirected to %s, which is not https", req.URL.Redacted())
		}
		return nil
	}}
}

// httpStatusError is a response other than 200.
type httpStatusError struct{ code int }

func (e httpStatusError) Error() string { return fmt.Sprintf("HTTP %d", e.code) }

// get downloads raw, refusing more than limit bytes.
func (w WellKnown) get(ctx context.Context, raw string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, webTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "berth")
	resp, err := w.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, httpStatusError{resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("larger than %d MiB", max(limit>>20, 1))
	}
	return body, nil
}

// indexCandidates lists where site's index may be: site itself when it
// names an index.json, else each well-known folder below site's path,
// then at the root of its host.
func indexCandidates(site string) ([]string, error) {
	u, err := url.Parse(site)
	if err != nil {
		return nil, err
	}
	u.RawQuery, u.Fragment = "", ""
	if strings.HasSuffix(u.Path, "/index.json") {
		return []string{u.String()}, nil
	}
	base := strings.TrimSuffix(u.Path, "/")
	var out []string
	for _, dir := range wellKnownDirs {
		at := *u
		at.Path = base + "/" + dir + "/index.json"
		out = append(out, at.String())
		if base != "" {
			at.Path = "/" + dir + "/index.json"
			out = append(out, at.String())
		}
	}
	return out, nil
}

// wellKnownEntry is one skill of a 0.2.0 index, as published.
type wellKnownEntry struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Digest      string `json:"digest"`
}

// ScanWellKnown reads the index site publishes (site may also be the
// index's own URL) and lists its skills; it downloads none of them. The
// scan's Commit is a digest of what it found, so it changes exactly when
// a skill is added, removed, reworded or republished; Unchanged compares
// it with since. A broken entry becomes a row with an error.
func (w WellKnown) ScanWellKnown(ctx context.Context, site, since string) (Scan, error) {
	if msg := config.CheckSkillRepo(site); msg != "" {
		return Scan{}, &Error{Code: "invalid_request", Field: "repo", Message: "The website URL " + strings.Replace(msg, "a repository URL such as https://github.com/owner/repo", "a URL such as https://example.com", 1)}
	}
	if since != "" && !config.ValidDigest(since) {
		return Scan{}, &Error{Code: "invalid_request", Field: "since", Message: "Since must be a digest such as sha256:<64 hex digits>"}
	}
	candidates, err := indexCandidates(site)
	if err != nil {
		return Scan{}, &Error{Code: "invalid_request", Field: "repo", Message: "The website URL is not a URL"}
	}
	var legacy, unknown bool
	var lastErr error
	for _, at := range candidates {
		body, err := w.get(ctx, at, maxIndexBytes)
		if err != nil {
			var se httpStatusError
			if !errors.As(err, &se) {
				lastErr = err
			}
			continue
		}
		var index struct {
			Schema *string           `json:"$schema"`
			Skills []json.RawMessage `json:"skills"`
		}
		if json.Unmarshal(body, &index) != nil || index.Skills == nil {
			continue
		}
		switch {
		case index.Schema == nil:
			legacy = true
			continue
		case *index.Schema != wellKnownSchema:
			unknown = true
			continue
		}
		return scanIndex(at, index.Skills, since)
	}
	switch host := hostOf(site); {
	case legacy:
		return Scan{}, &Error{Code: "unsupported_index", Field: "repo",
			Message: "This site publishes the older skills index, without a $schema; Berth reads discovery format 0.2.0"}
	case unknown:
		return Scan{}, &Error{Code: "unsupported_index", Field: "repo",
			Message: "This site's skills index uses a discovery format Berth does not know; Berth reads 0.2.0"}
	case lastErr != nil:
		return Scan{}, &Error{Code: "fetch_failed", Field: "repo", Message: "Could not reach " + host + ": " + lastErr.Error()}
	default:
		return Scan{}, &Error{Code: "no_index", Field: "repo",
			Message: "No Agent Skills index at " + host + "/.well-known/agent-skills/index.json"}
	}
}

func hostOf(site string) string {
	if u, err := url.Parse(site); err == nil && u.Host != "" {
		return u.Host
	}
	return site
}

// scanIndex turns the entries of the index at indexURL into a scan.
func scanIndex(indexURL string, raw []json.RawMessage, since string) (Scan, error) {
	if len(raw) == 0 {
		return Scan{}, &Error{Code: "no_skill", Field: "repo", Message: "The site's skills index lists no skills"}
	}
	if len(raw) > MaxScan {
		return Scan{}, &Error{Code: "too_many", Field: "repo",
			Message: fmt.Sprintf("The site's skills index lists more than %d skills", MaxScan)}
	}
	base, _ := url.Parse(indexURL)
	rows := make([]ScanRow, 0, len(raw))
	seen := map[string]bool{}
	for i, r := range raw {
		var e wellKnownEntry
		row := ScanRow{Path: fmt.Sprintf("entry-%d", i+1)}
		if json.Unmarshal(r, &e) == nil && config.SkillIDPattern.MatchString(e.Name) {
			row.Path = e.Name
		}
		row.Error = indexEntryError(e, r)
		var art *url.URL
		if row.Error == "" {
			var err error
			if art, err = base.Parse(e.URL); err != nil {
				row.Error = "Its url is not a URL"
			} else if msg := config.CheckArtifactURL(art.String()); msg != "" {
				row.Error = "Its url " + msg
			}
		}
		switch {
		case row.Error != "":
		case seen[e.Name]:
			row.Error = "Another entry of the index is also named " + e.Name
		default:
			seen[e.Name] = true
			row.Name, row.Description = e.Name, e.Description
			row.URL, row.Digest, row.Type = art.String(), e.Digest, e.Type
		}
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(a, b ScanRow) int { return strings.Compare(a.Path, b.Path) })
	for i := 1; i < len(rows); i++ {
		if rows[i].Path == rows[i-1].Path && rows[i].Error != "" {
			rows[i].Path = fmt.Sprintf("%s-%d", rows[i].Path, i+1) // a broken duplicate keeps a row of its own
		}
	}
	data, _ := json.Marshal(rows)
	sum := sha256.Sum256(data)
	commit := "sha256:" + hex.EncodeToString(sum[:])
	return Scan{Commit: commit, Index: indexURL, Unchanged: since == commit, Skills: rows}, nil
}

// indexEntryError says what is wrong with index entry e (raw as published), or "".
func indexEntryError(e wellKnownEntry, raw json.RawMessage) string {
	var probe map[string]any
	if json.Unmarshal(raw, &probe) != nil {
		return "The index entry is not an object"
	}
	for _, k := range []string{"name", "type", "description", "url", "digest"} {
		if _, ok := probe[k].(string); !ok {
			return "The index entry has no " + k
		}
	}
	switch n := utf8.RuneCountInString(e.Description); {
	case !config.SkillIDPattern.MatchString(e.Name) || strings.HasSuffix(e.Name, "-") || strings.Contains(e.Name, "--"):
		return fmt.Sprintf("%q is not a skill name", e.Name)
	case n == 0 || n > 1024:
		return "Its description must be 1 to 1024 characters"
	case e.Type != config.ArtifactSkillMD && e.Type != config.ArtifactArchive:
		return fmt.Sprintf("Its type %q is neither skill-md nor archive", e.Type)
	case !config.ValidDigest(e.Digest):
		return "Its digest is not sha256:<64 hex digits>"
	case e.URL == "":
		return "It has no url"
	}
	return ""
}

// Export downloads skill name of a well-known source, as found row f
// describes it, checks it against f's digest and streams it to extract as
// a tar with SKILL.md at its root. Downloads are kept in ArtifactDir(home)
// by digest, so a skill reinstalls even after the site moved on.
func (w WellKnown) Export(ctx context.Context, home, name string, f config.Found, extract func(io.Reader) error) error {
	data, err := w.artifact(ctx, home, f)
	if err != nil {
		return err
	}
	files, err := artifactFiles(f.Type, data)
	if err != nil {
		return err
	}
	var skillMD []byte
	for _, m := range files {
		if m.name == "SKILL.md" && !m.link {
			skillMD = m.data
		}
	}
	if skillMD == nil {
		return errors.New("the download has no SKILL.md")
	}
	meta, err := ParseFrontmatter(skillMD)
	if err != nil {
		return err
	}
	if meta.Name != name {
		return fmt.Errorf("its SKILL.md is named %s, but the site's index calls it %s", meta.Name, name)
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, m := range files {
		h := &tar.Header{Name: m.name, Mode: m.mode, Typeflag: tar.TypeReg, Size: int64(len(m.data))}
		if m.link {
			h = &tar.Header{Name: m.name, Mode: 0o777, Typeflag: tar.TypeSymlink, Linkname: string(m.data)}
		}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if !m.link {
			if _, err := tw.Write(m.data); err != nil {
				return err
			}
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return extract(&buf)
}

// artifact returns f's download, from the cache when it holds it.
func (w WellKnown) artifact(ctx context.Context, home string, f config.Found) ([]byte, error) {
	if !config.ValidDigest(f.Digest) {
		return nil, fmt.Errorf("%q is not a digest", f.Digest)
	}
	if msg := config.CheckArtifactURL(f.URL); msg != "" {
		return nil, fmt.Errorf("the url %s", msg)
	}
	want := strings.TrimPrefix(f.Digest, "sha256:")
	cached := filepath.Join(ArtifactDir(home), want)
	if data, err := os.ReadFile(cached); err == nil {
		if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) == want {
			return data, nil
		}
		os.Remove(cached)
	}
	data, err := w.get(ctx, f.URL, maxArtifactBytes)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", f.URL, err)
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != want {
		return nil, errors.New("the download does not match the digest the site published; check for updates")
	}
	if err := os.MkdirAll(ArtifactDir(home), 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(ArtifactDir(home), ".tmp-")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	return data, os.Rename(tmp.Name(), cached)
}

// artifactFile is one file of a download; a link's data is its target.
type artifactFile struct {
	name string
	mode int64
	link bool
	data []byte
}

// artifactFiles unpacks a download of type typ: a lone SKILL.md, or a
// tar, gzipped tar or zip archive. An archive whose files all sit in one
// folder, with no SKILL.md beside it, is read from inside that folder.
// The tar extract checks the names; this keeps to the limits.
func artifactFiles(typ string, data []byte) ([]artifactFile, error) {
	if typ == config.ArtifactSkillMD {
		return []artifactFile{{name: "SKILL.md", mode: 0o644, data: data}}, nil
	}
	var files []artifactFile
	var err error
	switch {
	case bytes.HasPrefix(data, []byte("PK\x03\x04")):
		files, err = zipFiles(data)
	case bytes.HasPrefix(data, []byte{0x1f, 0x8b}):
		var zr *gzip.Reader
		if zr, err = gzip.NewReader(bytes.NewReader(data)); err == nil {
			files, err = tarFiles(zr)
		}
	default:
		files, err = tarFiles(bytes.NewReader(data))
	}
	if err != nil {
		return nil, fmt.Errorf("read the archive: %w", err)
	}
	return unwrap(files), nil
}

// budget enforces the archive limits across its files.
type budget struct{ files, bytes int64 }

func (b *budget) take(size int64) error {
	b.files++
	b.bytes += size
	switch {
	case b.files > maxArchiveFiles:
		return fmt.Errorf("it holds more than %d files", maxArchiveFiles)
	case b.bytes > maxArchiveBytes:
		return fmt.Errorf("it unpacks to more than %d MiB", maxArchiveBytes>>20)
	}
	return nil
}

// readAll reads r within what is left of the budget.
func (b *budget) readAll(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxArchiveBytes-b.bytes+1))
	if err != nil {
		return nil, err
	}
	return data, b.take(int64(len(data)))
}

func tarFiles(r io.Reader) ([]artifactFile, error) {
	tr := tar.NewReader(r)
	var b budget
	var out []artifactFile
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		switch h.Typeflag {
		case tar.TypeReg:
			data, err := b.readAll(tr)
			if err != nil {
				return nil, err
			}
			out = append(out, artifactFile{name: h.Name, mode: execMode(h.Mode), data: data})
		case tar.TypeSymlink:
			if err := b.take(0); err != nil {
				return nil, err
			}
			out = append(out, artifactFile{name: h.Name, link: true, data: []byte(h.Linkname)})
		}
	}
}

func zipFiles(data []byte) ([]artifactFile, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	var b budget
	var out []artifactFile
	for _, f := range zr.File {
		mode := f.Mode()
		if mode.IsDir() || strings.HasSuffix(f.Name, "/") {
			continue
		}
		if mode&os.ModeType != 0 && mode&os.ModeSymlink == 0 {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		body, err := b.readAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		if mode&os.ModeSymlink != 0 {
			out = append(out, artifactFile{name: f.Name, link: true, data: body})
			continue
		}
		out = append(out, artifactFile{name: f.Name, mode: execMode(int64(mode.Perm())), data: body})
	}
	return out, nil
}

func execMode(mode int64) int64 {
	if mode&0o111 != 0 {
		return 0o755
	}
	return 0o644
}

// unwrap drops macOS resource folders and leading ./, and strips a single
// folder that holds everything when there is no SKILL.md at the root.
func unwrap(files []artifactFile) []artifactFile {
	var out []artifactFile
	for _, f := range files {
		f.name = strings.TrimPrefix(path.Clean(strings.ReplaceAll(f.name, `\`, "/")), "./")
		if f.name == "__MACOSX" || strings.HasPrefix(f.name, "__MACOSX/") || path.Base(f.name) == ".DS_Store" {
			continue
		}
		out = append(out, f)
	}
	top := ""
	for _, f := range out {
		if f.name == "SKILL.md" {
			return out
		}
		first, _, nested := strings.Cut(f.name, "/")
		if !nested || (top != "" && first != top) {
			return out
		}
		top = first
	}
	for i := range out {
		out[i].name = strings.TrimPrefix(out[i].name, top+"/")
	}
	return out
}
