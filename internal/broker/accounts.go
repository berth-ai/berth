package broker

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/berth-ai/berth/internal/runner"
)

// forgeGroup serializes device logins.
const forgeGroup = "forge"

const defaultLoginTimeout = 15 * time.Minute

// teaForge is the Gitea CLI. Unlike gh and glab it signs in with an access
// token, to any number of Gitea or Forgejo servers, so it has no device job.
const teaForge = "tea"

var (
	emailPattern = regexp.MustCompile(`^[^\s@-][^\s@]{0,127}@[^\s@]{1,126}\.[^\s@]{1,62}$`)
	ghUser       = regexp.MustCompile(`Logged in to \S+ (?:account|as) (\S+)`)
	glabUser     = regexp.MustCompile(`Logged in to \S+ as (\S+)`)
)

// Forge is one code host's sign-in state. Logins lists tea's servers; the
// host and user are then the default login's.
type Forge struct {
	Host     string       `json:"host"`
	SignedIn bool         `json:"signed_in"`
	User     string       `json:"user"`
	Logins   []ForgeLogin `json:"logins,omitempty"`
}

// ForgeLogin is one server tea is signed in to.
type ForgeLogin struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	User    string `json:"user"`
	Default bool   `json:"default"`
}

// forgeSpec is a CLI that signs in to one host with a device code.
type forgeSpec struct {
	path   string
	host   string
	user   *regexp.Regexp
	login  []string
	logout []string
}

func (b *Broker) forges() map[string]forgeSpec {
	return map[string]forgeSpec{
		"gh": {path: b.Home.GH, host: "github.com", user: ghUser,
			login:  []string{"auth", "login", "--hostname", "github.com", "--web", "--git-protocol", "https", "--skip-ssh-key"},
			logout: []string{"auth", "logout", "--hostname", "github.com"}},
		"glab": {path: b.Home.Glab, host: "gitlab.com", user: glabUser,
			login:  []string{"auth", "login", "--hostname", "gitlab.com", "--device", "--git-protocol", "https", "--api-protocol", "https"},
			logout: []string{"auth", "logout", "--hostname", "gitlab.com"}},
	}
}

func validGitName(s string) bool {
	return s != "" && len(s) <= 100 && !strings.HasPrefix(s, "-") &&
		strings.IndexFunc(s, unicode.IsControl) < 0
}

func (b *Broker) gitIdentity(ctx context.Context, body []byte) (int, any) {
	var in struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if !validGitName(in.Name) {
		return badRequest(errors.New("the name must be 1 to 100 characters, without control characters or a leading dash"))
	}
	if !emailPattern.MatchString(in.Email) {
		return badRequest(errors.New("the email must look like name@example.org"))
	}
	for _, kv := range [][2]string{{"user.name", in.Name}, {"user.email", in.Email}} {
		_, stderr, exit, err := b.agentRun(ctx, b.syncTimeout(), b.Home.Git, "config", "--global", kv[0], kv[1])
		if code, v, failed := syncFailure(err, exit, stderr); failed {
			return code, v
		}
	}
	return http.StatusOK, map[string]any{"name": in.Name, "email": in.Email}
}

func (b *Broker) forgeStatus(ctx context.Context, body []byte) (int, any) {
	if err := decode(body, &struct{}{}); err != nil {
		return badRequest(err)
	}
	git := map[string]string{}
	for _, key := range []string{"name", "email"} {
		stdout, _, _, _ := b.agentRun(ctx, b.syncTimeout(), b.Home.Git, "config", "--global", "--get", "user."+key)
		git[key] = strings.TrimSpace(stdout)
	}
	forges := map[string]Forge{}
	for name, spec := range b.forges() {
		stdout, stderr, exit, err := b.agentRun(ctx, b.syncTimeout(), spec.path, "auth", "status", "--hostname", spec.host)
		f := Forge{Host: spec.host}
		if err == nil && exit == 0 {
			f.SignedIn = true
			if m := spec.user.FindStringSubmatch(stdout + stderr); m != nil {
				f.User = m[1]
			}
		}
		forges[name] = f
	}
	// A tea that cannot list its logins counts as signed out.
	logins, _ := b.teaLogins(ctx)
	forges[teaForge] = teaState(logins)
	return http.StatusOK, map[string]any{"git": git, "forges": forges}
}

func (b *Broker) forgeLogin(_ context.Context, body []byte) (int, any) {
	var in struct {
		Forge string `json:"forge"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	spec, ok := b.forges()[in.Forge]
	if !ok {
		return badRequest(fmt.Errorf("forge must be gh or glab, got %q", in.Forge))
	}
	timeout := b.LoginTimeout
	if timeout == 0 {
		timeout = defaultLoginTimeout
	}
	login := b.homeWork(spec.path, spec.login, timeout)
	work := func(ctx context.Context, out io.Writer) (int, json.RawMessage, error) {
		code, res, err := login(ctx, out)
		if err != nil || code != 0 {
			return code, res, err
		}
		if err := b.setCredentialHelper(ctx, spec); err != nil {
			fmt.Fprintf(out, "Signed in, but git could not be set up to use %s: %v\n", spec.path, err)
			return 1, nil, nil
		}
		return 0, res, nil
	}
	return b.jobAccepted("forge.login", forgeGroup, append([]string{spec.path}, spec.login...), work)
}

// setCredentialHelper makes the CLI git's only credential helper for its
// host, so HTTPS clones of private repositories fetch and push. The helper
// names the CLI as Home does, not its install path, so it survives upgrades;
// gh auth setup-git and glab's --setup-git-credential-helper would not.
func (b *Broker) setCredentialHelper(ctx context.Context, spec forgeSpec) error {
	key := "credential.https://" + spec.host + ".helper"
	steps := [][]string{
		{"config", "--global", "--unset-all", key},
		{"config", "--global", "--add", key, ""}, // drops helpers set elsewhere
		{"config", "--global", "--add", key, "!" + spec.path + " auth git-credential"},
	}
	for i, args := range steps {
		_, stderr, exit, err := b.agentRun(ctx, b.syncTimeout(), b.Home.Git, args...)
		if err != nil {
			return err
		}
		if exit != 0 && !(i == 0 && exit == 5) { // 5: no helper was set
			return fmt.Errorf("git %s: %s", strings.Join(args[:2], " "), cmp.Or(strings.TrimSpace(stderr), fmt.Sprintf("exit %d", exit)))
		}
	}
	return nil
}

// forgeToken signs tea in to a server with an access token. The token reaches
// tea through its environment, never argv, and tea registers itself as git's
// credential helper for the server.
func (b *Broker) forgeToken(ctx context.Context, body []byte) (int, any) {
	var in struct {
		Forge string `json:"forge"`
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if in.Forge != teaForge {
		return badRequest(fmt.Errorf("only tea signs in with a token, got %q", in.Forge))
	}
	server, name, err := serverURL(in.URL)
	if err != nil {
		return badRequest(err)
	}
	if !validToken(in.Token) {
		return badRequest(errors.New("the token must be 1 to 512 characters, without spaces"))
	}
	logins, err := b.teaLogins(ctx)
	if err != nil {
		return fail(http.StatusBadGateway, "command_failed", "%v", err)
	}
	for _, l := range logins {
		if l.Name == name || strings.TrimSuffix(l.URL, "/") == server {
			return fail(http.StatusConflict, "signed_in", "already signed in to %s; sign out first", name)
		}
	}
	_, stderr, exit, err := b.agentRunCmd(ctx, b.syncTimeout(), runner.Cmd{Path: b.Home.Tea,
		Args: []string{"login", "add", "--name", name, "--url", server, "--git-credentials"},
		Env:  []string{"GITEA_SERVER_TOKEN=" + in.Token}})
	if code, v, failed := syncFailure(err, exit, teaError(stderr)); failed {
		return code, v
	}
	return http.StatusOK, map[string]any{"name": name, "url": server}
}

// forgeLogout signs a CLI out of its host, or tea out of one of its logins.
// Tokens are removed locally, not revoked.
func (b *Broker) forgeLogout(ctx context.Context, body []byte) (int, any) {
	var in struct {
		Forge string `json:"forge"`
		Name  string `json:"name"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	var path string
	var args []string
	if in.Forge == teaForge {
		if in.Name == "" || strings.HasPrefix(in.Name, "-") {
			return badRequest(errors.New("name must be one of tea's logins"))
		}
		logins, err := b.teaLogins(ctx)
		if err != nil {
			return fail(http.StatusBadGateway, "command_failed", "%v", err)
		}
		if !slices.ContainsFunc(logins, func(l ForgeLogin) bool { return l.Name == in.Name }) {
			return fail(http.StatusNotFound, "unknown_login", "tea has no login named %q", in.Name)
		}
		path, args = b.Home.Tea, []string{"login", "delete", in.Name}
	} else {
		spec, ok := b.forges()[in.Forge]
		if !ok {
			return badRequest(fmt.Errorf("forge must be gh, glab or tea, got %q", in.Forge))
		}
		if in.Name != "" {
			return badRequest(errors.New("name is for tea only"))
		}
		path, args = spec.path, spec.logout
	}
	_, stderr, exit, err := b.agentRun(ctx, b.syncTimeout(), path, args...)
	if code, v, failed := syncFailure(err, exit, teaError(stderr)); failed {
		return code, v
	}
	return http.StatusOK, map[string]any{"forge": in.Forge, "name": in.Name}
}

// teaLogins reads tea's logins from its config; it makes no network call.
func (b *Broker) teaLogins(ctx context.Context) ([]ForgeLogin, error) {
	stdout, stderr, exit, err := b.agentRun(ctx, b.syncTimeout(), b.Home.Tea, "login", "list", "--output", "json")
	if err != nil {
		return nil, err
	}
	if exit != 0 {
		return nil, errors.New(teaError(stderr))
	}
	var raw []struct {
		Name    string `json:"name"`
		URL     string `json:"url"`
		User    string `json:"user"`
		Default any    `json:"default"` // tea prints the string "true"
	}
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		return nil, fmt.Errorf("tea login list: %w", err)
	}
	logins := make([]ForgeLogin, 0, len(raw))
	for _, r := range raw {
		logins = append(logins, ForgeLogin{Name: r.Name, URL: r.URL, User: r.User,
			Default: r.Default == true || r.Default == "true"})
	}
	return logins, nil
}

// teaState sums tea's logins up as a Forge, named after its default login.
func teaState(logins []ForgeLogin) Forge {
	f := Forge{SignedIn: len(logins) > 0, Logins: logins}
	for i, l := range logins {
		if i == 0 || l.Default {
			f.Host, f.User = l.Name, l.User
		}
		if l.Default {
			break
		}
	}
	return f
}

// serverURL checks a Gitea server's address and returns it without a
// trailing slash, with the host[:port] that names its tea login.
func serverURL(s string) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	bad := errors.New("the server must be an https:// address such as https://gitea.com")
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || strings.HasPrefix(u.Host, "-") {
		return "", "", bad
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	u.RawPath = ""
	return u.String(), u.Host, nil
}

func validToken(s string) bool {
	return s != "" && len(s) <= 512 && strings.IndexFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) < 0
}

// teaError is the first line of a CLI's error output, without tea's
// "Error: " prefix; tea follows it with one line per auth method it tried.
func teaError(stderr string) string {
	for line := range strings.Lines(stderr) {
		if line = strings.TrimSpace(line); line != "" {
			return strings.TrimPrefix(line, "Error: ")
		}
	}
	return ""
}
