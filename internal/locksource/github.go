package locksource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Default GitHub endpoints.
const (
	DefaultAPIBase = "https://api.github.com"
	DefaultRawBase = "https://raw.githubusercontent.com"
)

// ErrFetch marks failures to resolve or download a lock from GitHub.
var ErrFetch = errors.New("fetch agent lock")

var (
	repoPattern = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`)
	refPattern  = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
	shaPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// GitHub fetches agents/package.json and package-lock.json from a ref.
type GitHub struct {
	Repo    string
	Token   string
	APIBase string
	RawBase string
	Client  *http.Client
}

// Fetch resolves ref to a commit, then downloads both files at that commit so
// they are guaranteed to match each other.
func (g GitHub) Fetch(ctx context.Context, ref string, now time.Time) (*Lock, error) {
	if !repoPattern.MatchString(g.Repo) {
		return nil, fmt.Errorf("%w: invalid repository %q", ErrFetch, g.Repo)
	}
	if !refPattern.MatchString(ref) || strings.Contains(ref, "..") {
		return nil, fmt.Errorf("%w: invalid ref %q", ErrFetch, ref)
	}
	sha, err := g.resolve(ctx, ref)
	if err != nil {
		return nil, err
	}
	pkg, err := g.raw(ctx, sha, "agents/package.json")
	if err != nil {
		return nil, err
	}
	lock, err := g.raw(ctx, sha, "agents/package-lock.json")
	if err != nil {
		return nil, err
	}
	return Parse(pkg, lock, Source{Origin: OriginRef, Repo: g.Repo, Ref: ref, Commit: sha, FetchedAt: now.UTC()})
}

func (g GitHub) resolve(ctx context.Context, ref string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/commits/%s", orDefault(g.APIBase, DefaultAPIBase), g.Repo, ref)
	body, err := g.get(ctx, url, "application/vnd.github.sha")
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(string(body))
	if !shaPattern.MatchString(sha) {
		return "", fmt.Errorf("%w: unexpected commit id %q for %s", ErrFetch, sha, ref)
	}
	return sha, nil
}

func (g GitHub) raw(ctx context.Context, sha, file string) ([]byte, error) {
	return g.get(ctx, fmt.Sprintf("%s/%s/%s/%s", orDefault(g.RawBase, DefaultRawBase), g.Repo, sha, file), "")
}

func (g GitHub) get(ctx context.Context, url, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFetch, err)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFetch, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: GET %s: %s", ErrFetch, url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFetch, err)
	}
	return body, nil
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
