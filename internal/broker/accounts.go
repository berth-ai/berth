package broker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// forgeGroup serializes device logins.
const forgeGroup = "forge"

const defaultLoginTimeout = 15 * time.Minute

var (
	emailPattern = regexp.MustCompile(`^[^\s@-][^\s@]{0,127}@[^\s@]{1,126}\.[^\s@]{1,62}$`)
	ghUser       = regexp.MustCompile(`Logged in to \S+ (?:account|as) (\S+)`)
	glabUser     = regexp.MustCompile(`Logged in to \S+ as (\S+)`)
)

// Forge is one code host's sign-in state.
type Forge struct {
	Host     string `json:"host"`
	SignedIn bool   `json:"signed_in"`
	User     string `json:"user"`
}

type forgeSpec struct {
	host  string
	user  *regexp.Regexp
	login []string
}

func (b *Broker) forges() map[string]forgeSpec {
	return map[string]forgeSpec{
		"gh": {host: "github.com", user: ghUser,
			login: []string{"auth", "login", "--hostname", "github.com", "--web", "--git-protocol", "https", "--skip-ssh-key"}},
		"glab": {host: "gitlab.com", user: glabUser,
			login: []string{"auth", "login", "--hostname", "gitlab.com", "--device", "--git-protocol", "https", "--api-protocol", "https"}},
	}
}

func (b *Broker) forgePath(name string) string {
	if name == "gh" {
		return b.Home.GH
	}
	return b.Home.Glab
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
		stdout, stderr, exit, err := b.agentRun(ctx, b.syncTimeout(), b.forgePath(name), "auth", "status", "--hostname", spec.host)
		f := Forge{Host: spec.host}
		if err == nil && exit == 0 {
			f.SignedIn = true
			if m := spec.user.FindStringSubmatch(stdout + stderr); m != nil {
				f.User = m[1]
			}
		}
		forges[name] = f
	}
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
	return b.homeJob("forge.login", forgeGroup, b.forgePath(in.Forge), spec.login, timeout)
}
