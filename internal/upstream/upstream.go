// Package upstream asks where each pinned entity comes from which versions
// exist: the npm registry for agent packages, mise for base tools. Nothing
// here installs anything.
package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/berth-ai/berth/internal/pins"
	"github.com/berth-ai/berth/internal/runner"
)

// DefaultRegistry is the public npm registry.
const DefaultRegistry = "https://registry.npmjs.org"

// maxDoc bounds a registry answer; a package's abbreviated document lists
// every version it ever published.
const maxDoc = 64 << 20

// ErrLookup marks a failed version lookup.
var ErrLookup = errors.New("version lookup")

// NPM reads versions from an npm registry.
type NPM struct {
	Registry string       // empty means DefaultRegistry
	Client   *http.Client // nil means a client with a 30s timeout
}

// Latest is the version the package's latest dist-tag names.
func (n NPM) Latest(ctx context.Context, pkg string) (string, error) {
	var doc struct {
		Version string `json:"version"`
	}
	if err := n.get(ctx, pkg, "/latest", "application/json", &doc); err != nil {
		return "", err
	}
	if !pins.ValidVersion(doc.Version) {
		return "", fmt.Errorf("%w: %s: the registry named no valid latest version", ErrLookup, pkg)
	}
	return doc.Version, nil
}

// Versions lists the package's published versions, oldest first.
func (n NPM) Versions(ctx context.Context, pkg string) ([]string, error) {
	var doc struct {
		Versions map[string]json.RawMessage `json:"versions"`
	}
	if err := n.get(ctx, pkg, "", "application/vnd.npm.install-v1+json", &doc); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(doc.Versions))
	for v := range doc.Versions {
		if pins.ValidVersion(v) {
			out = append(out, v)
		}
	}
	slices.SortFunc(out, pins.Compare)
	return out, nil
}

func (n NPM) get(ctx context.Context, pkg, suffix, accept string, v any) error {
	if !validPackage(pkg) {
		return fmt.Errorf("%w: invalid package name %q", ErrLookup, pkg)
	}
	base := n.Registry
	if base == "" {
		base = DefaultRegistry
	}
	u := strings.TrimRight(base, "/") + "/" + strings.Replace(url.PathEscape(pkg), "%40", "@", 1) + suffix
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrLookup, err)
	}
	req.Header.Set("Accept", accept)
	client := n.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrLookup, pkg, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %s: the registry answered %s", ErrLookup, pkg, resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxDoc+1))
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrLookup, pkg, err)
	}
	if len(raw) > maxDoc {
		return fmt.Errorf("%w: %s: the registry answer is too large", ErrLookup, pkg)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrLookup, pkg, err)
	}
	return nil
}

// validPackage accepts npm package names, scoped or not.
func validPackage(name string) bool {
	if name == "" || len(name) > 214 {
		return false
	}
	rest := name
	if strings.HasPrefix(name, "@") {
		scope, pkg, ok := strings.Cut(name[1:], "/")
		if !ok || !validPart(scope) {
			return false
		}
		rest = pkg
	}
	return validPart(rest)
}

func validPart(s string) bool {
	if s == "" || s[0] == '.' || s[0] == '_' {
		return false
	}
	for _, r := range s {
		lower, digit := r >= 'a' && r <= 'z', r >= '0' && r <= '9'
		if !lower && !digit && !strings.ContainsRune("-._~", r) {
			return false
		}
	}
	return true
}

// Mise reads tool versions through the mise binary, run as agent so no
// backend code ever runs as root.
type Mise struct {
	Path    string // the mise binary
	Runner  runner.Runner
	Env     []string // extra environment, such as an isolated MISE_CONFIG_DIR
	Timeout time.Duration
}

// Latest is the newest stable version mise knows for tool.
func (m Mise) Latest(ctx context.Context, tool string) (string, error) {
	out, err := m.run(ctx, "latest", tool)
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(out)
	if !pins.ValidVersion(v) {
		return "", fmt.Errorf("%w: %s: mise named no valid latest version", ErrLookup, tool)
	}
	return v, nil
}

// Versions lists the versions mise knows for tool, oldest first.
func (m Mise) Versions(ctx context.Context, tool string) ([]string, error) {
	out, err := m.run(ctx, "ls-remote", tool)
	if err != nil {
		return nil, err
	}
	var vs []string
	for _, v := range strings.Fields(out) {
		if pins.ValidVersion(v) {
			vs = append(vs, v)
		}
	}
	slices.SortStableFunc(vs, pins.Compare)
	return vs, nil
}

func (m Mise) run(ctx context.Context, args ...string) (string, error) {
	var stdout bytes.Buffer
	timeout := m.Timeout
	if timeout == 0 {
		timeout = time.Minute
	}
	cmd := runner.Cmd{Path: m.Path, Args: args, Env: m.Env, AsAgent: true, Timeout: timeout, Stdout: &stdout}
	if err := m.Runner.Run(ctx, cmd); err != nil {
		return "", fmt.Errorf("%w: %w", ErrLookup, err)
	}
	return stdout.String(), nil
}
