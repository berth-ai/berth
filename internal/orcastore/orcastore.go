// Package orcastore installs Orca, whose headless server Berth runs, into a
// store in the agents volume, so Orca moves without a new image. The image
// pins one release and its checksums in PinFile; boot installs that release
// while Orca is on, and berth update moves it like any other pin. Each
// complete version is Orca's .deb unpacked by dpkg-deb, sealed read-only and
// named by version and architecture. The image keeps only Orca's system
// libraries, which change far less often.
package orcastore

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/berth-ai/berth/internal/agentstore"
	"github.com/berth-ai/berth/internal/locksource"
	"github.com/berth-ai/berth/internal/pins"
	"github.com/berth-ai/berth/internal/runner"
	"github.com/berth-ai/berth/internal/upstream"
)

const (
	// Root is the Orca store inside the agents volume.
	Root = "/opt/agents/orca"
	// PinFile is the image's pin: the release and its .deb checksums.
	PinFile = "/usr/share/berth/orca.json"
	// Name is Orca's name among the pinned entities.
	Name = "orca"
	// Title is how status shows it.
	Title = "Orca"
	// Releases is where Orca's release assets download from.
	Releases = "https://github.com/stablyai/orca/releases"
	// API lists Orca's releases.
	API = "https://api.github.com/repos/stablyai/orca/releases"
	// AppDir is the unpacked app inside a version: Orca's /opt/Orca.
	AppDir = "Orca"
	// CLI is Orca's command line inside a version; it finds the app
	// relative to itself.
	CLI = AppDir + "/resources/bin/orca-ide"
)

// maxManifest bounds a release's latest-linux.yml.
const maxManifest = 1 << 20

// Pin is the image's Orca: a release and the sha256 of its .deb for each
// architecture the image supports.
type Pin struct {
	Version string            `json:"version"`
	SHA256  map[string]string `json:"sha256"`
}

// ReadPin reads the image's pin. A missing file means an image without Orca
// and returns nil and no error.
func ReadPin(path string) (*Pin, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p Pin
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if !pins.ValidVersion(p.Version) {
		return nil, fmt.Errorf("%s: invalid version %q", path, p.Version)
	}
	return &p, nil
}

// Supports reports whether the pin has a checksum for arch, the only way
// the image's release can be installed there.
func (p *Pin) Supports(arch string) bool {
	return p != nil && len(p.SHA256[arch]) == sha256.Size*2
}

// Hash names a version: the first 12 hex digits of sha256 over the release
// and architecture.
func Hash(version, arch string) string {
	sum := sha256.Sum256([]byte(Name + "@" + version + " linux/" + arch + "\n"))
	return hex.EncodeToString(sum[:])[:12]
}

// Installer builds complete Orca versions. It runs as root: the tree it
// unpacks ends up root-owned and read-only to agent.
type Installer struct {
	Store    agentstore.Store
	Pin      *Pin
	Arch     string       // empty means runtime.GOARCH
	Releases string       // empty means Releases
	Client   *http.Client // nil means a client without an overall timeout
	Runner   runner.Runner
	DpkgDeb  string // empty means dpkg-deb on PATH
	Root     bool   // whether to chown the tree to root
	Now      func() time.Time
	Logf     func(format string, args ...any)
}

// Install makes the version holding Orca at version complete and returns
// it, and whether it had to be built. A failure removes the partial tree.
func (in Installer) Install(ctx context.Context, version string) (*agentstore.Version, bool, error) {
	if !pins.ValidVersion(version) {
		return nil, false, &agentstore.StepError{Step: "prepare", Err: fmt.Errorf("invalid Orca version %q", version)}
	}
	hash := Hash(version, in.arch())
	if v, err := in.Store.Get(hash); err == nil {
		return v, false, nil
	} else if !errors.Is(err, agentstore.ErrNotFound) {
		return nil, false, &agentstore.StepError{Step: "prepare", Err: err}
	}
	final := in.Store.VersionDir(hash)
	tmp := final + ".tmp"
	if err := in.build(ctx, final, tmp, version); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, false, err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, false, &agentstore.StepError{Step: "publish", Err: err}
	}
	v, err := in.Store.Get(hash)
	if err != nil {
		return nil, false, &agentstore.StepError{Step: "publish", Err: err}
	}
	return v, true, nil
}

func (in Installer) build(ctx context.Context, final, tmp, version string) error {
	for _, dir := range []string{tmp, final} { // final is known incomplete here
		if err := os.RemoveAll(dir); err != nil {
			return &agentstore.StepError{Step: "prepare", Err: err}
		}
	}
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return &agentstore.StepError{Step: "prepare", Err: err}
	}
	check, origin, err := in.checksum(ctx, version)
	if err != nil {
		return &agentstore.StepError{Step: "resolve", Err: err}
	}
	deb := filepath.Join(tmp, "orca.deb")
	in.logf("downloading Orca %s for %s", version, in.arch())
	if err := in.download(ctx, in.debURL(version), deb, check); err != nil {
		return &agentstore.StepError{Step: "download", Err: err}
	}
	unpacked := filepath.Join(tmp, "deb")
	extract := runner.Cmd{Path: in.dpkgDeb(), Args: []string{"-x", deb, unpacked}}
	if err := in.Runner.Run(ctx, extract); err != nil {
		return &agentstore.StepError{Step: "install", Err: err}
	}
	if err := os.Rename(filepath.Join(unpacked, "opt", "Orca"), filepath.Join(tmp, AppDir)); err != nil {
		return &agentstore.StepError{Step: "install", Err: fmt.Errorf("the package has no /opt/Orca: %w", err)}
	}
	for _, p := range []string{unpacked, deb} {
		if err := os.RemoveAll(p); err != nil {
			return &agentstore.StepError{Step: "install", Err: err}
		}
	}
	if info, err := os.Stat(filepath.Join(tmp, CLI)); err != nil || info.Mode().Perm()&0o111 == 0 {
		return &agentstore.StepError{Step: "install", Err: fmt.Errorf("the package has no executable %s", CLI)}
	}
	// Seal drops setuid bits too: chrome-sandbox stays an ordinary file, as
	// no-new-privileges would make it one anyway.
	if err := agentstore.Seal(tmp, in.Root); err != nil {
		return &agentstore.StepError{Step: "seal", Err: err}
	}
	src := locksource.Source{Origin: origin, FetchedAt: in.now().UTC()}
	if err := writeJSON(filepath.Join(tmp, "source.json"), src); err != nil {
		return &agentstore.StepError{Step: "complete", Err: err}
	}
	m := agentstore.Manifest{Packages: map[string]string{Name: version}, InstalledAt: in.now().UTC()}
	if err := writeJSON(filepath.Join(tmp, ".complete"), m); err != nil {
		return &agentstore.StepError{Step: "complete", Err: err}
	}
	return nil
}

// checksum is what the .deb must hash to: the image's sha256 for the
// image's release, else the sha512 the release's own update manifest names.
func (in Installer) checksum(ctx context.Context, version string) (digest, locksource.Origin, error) {
	if in.Pin != nil && version == in.Pin.Version {
		if !in.Pin.Supports(in.arch()) {
			return digest{}, "", fmt.Errorf("the image pins no Orca checksum for %s", in.arch())
		}
		want, err := hex.DecodeString(in.Pin.SHA256[in.arch()])
		if err != nil {
			return digest{}, "", err
		}
		return digest{name: "sha256", want: want, new: sha256.New}, locksource.OriginImage, nil
	}
	m, err := in.manifest(ctx, "download/v"+version)
	if err != nil {
		return digest{}, "", err
	}
	if m.Version != version {
		return digest{}, "", fmt.Errorf("Orca's v%s manifest names version %q", version, m.Version)
	}
	sum, ok := m.SHA512[debName(version, in.arch())]
	if !ok {
		return digest{}, "", fmt.Errorf("Orca v%s publishes no checksum for %s", version, debName(version, in.arch()))
	}
	want, err := base64.StdEncoding.DecodeString(sum)
	if err != nil || len(want) != sha512.Size {
		return digest{}, "", fmt.Errorf("Orca v%s names an invalid sha512 for %s", version, debName(version, in.arch()))
	}
	return digest{name: "sha512", want: want, new: sha512.New}, locksource.OriginUpstream, nil
}

type digest struct {
	name string
	want []byte
	new  func() hash.Hash
}

// download writes url to path and fails unless it hashes to check.
func (in Installer) download(ctx context.Context, url, path string, check digest) error {
	resp, err := in.get(ctx, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	h := check.new()
	_, err = io.Copy(io.MultiWriter(f, h), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("%s: %w", url, err)
	}
	if got := h.Sum(nil); !bytes.Equal(got, check.want) {
		return fmt.Errorf("%s: %s mismatch: got %x, want %x", url, check.name, got, check.want)
	}
	return nil
}

// Manifest is the part of a release's latest-linux.yml Berth reads: the
// release and the sha512, base64, of each file.
type Manifest struct {
	Version string
	SHA512  map[string]string
}

// Latest is the newest release, as its update manifest names it.
func (in Installer) Latest(ctx context.Context) (string, error) {
	m, err := in.manifest(ctx, "latest/download")
	if err != nil {
		return "", fmt.Errorf("%w: %s: %w", upstream.ErrLookup, Name, err)
	}
	if !pins.ValidVersion(m.Version) {
		return "", fmt.Errorf("%w: %s: the release manifest names no valid version", upstream.ErrLookup, Name)
	}
	return m.Version, nil
}

func (in Installer) manifest(ctx context.Context, at string) (*Manifest, error) {
	name := "latest-linux.yml"
	if in.arch() != "amd64" {
		name = "latest-linux-" + in.arch() + ".yml"
	}
	resp, err := in.get(ctx, in.releases()+"/"+at+"/"+name)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxManifest))
	if err != nil {
		return nil, err
	}
	return ParseManifest(raw), nil
}

// ParseManifest reads the version and each file's sha512 from an
// electron-builder update manifest, which is this flat:
//
//	version: 1.4.255
//	files:
//	  - url: orca-ide_1.4.255_amd64.deb
//	    sha512: VCkd...
func ParseManifest(raw []byte) *Manifest {
	m := &Manifest{SHA512: map[string]string{}}
	url := ""
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		key, value, ok := strings.Cut(strings.TrimPrefix(line, "- "), ":")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `'"`)
		indented := strings.HasPrefix(sc.Text(), " ")
		switch {
		case key == "version" && !indented:
			m.Version = value
		case key == "url" && strings.HasPrefix(line, "- "):
			url = value
		case key == "sha512" && indented && url != "":
			m.SHA512[url] = value
			url = ""
		}
	}
	return m
}

// Versions lists Orca's published releases, oldest first. GitHub answers
// at most 100 per page, newest first; that is history enough to pick from.
func (in Installer) Versions(ctx context.Context, api string) ([]string, error) {
	if api == "" {
		api = API
	}
	resp, err := in.get(ctx, api+"?per_page=100")
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", upstream.ErrLookup, Name, err)
	}
	defer resp.Body.Close()
	var releases []struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&releases); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", upstream.ErrLookup, Name, err)
	}
	out := []string{}
	for _, r := range releases {
		v := strings.TrimPrefix(r.Tag, "v")
		if !r.Draft && !r.Prerelease && pins.ValidVersion(v) {
			out = append(out, v)
		}
	}
	slices.SortFunc(out, pins.Compare)
	return slices.Compact(out), nil
}

func (in Installer) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := in.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return resp, nil
}

func debName(version, arch string) string { return "orca-ide_" + version + "_" + arch + ".deb" }

func (in Installer) debURL(version string) string {
	return in.releases() + "/download/v" + version + "/" + debName(version, in.arch())
}

func (in Installer) arch() string {
	if in.Arch == "" {
		return runtime.GOARCH
	}
	return in.Arch
}

func (in Installer) releases() string {
	if in.Releases == "" {
		return Releases
	}
	return strings.TrimRight(in.Releases, "/")
}

func (in Installer) dpkgDeb() string {
	if in.DpkgDeb == "" {
		return "dpkg-deb"
	}
	return in.DpkgDeb
}

func (in Installer) now() time.Time {
	if in.Now == nil {
		return time.Now()
	}
	return in.Now()
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
