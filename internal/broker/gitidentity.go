package broker

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// A host identity is a git name and email for the repositories whose remote
// is on one code host. Each lives in its own file, which the global git
// config includes for that host's remote URLs:
//
//	[includeIf "hasconfig:remote.*.url:https://gitlab.com/**"]
//		path = ~/.config/berth/git/gitlab.com.gitconfig
//
// so every git, the agents', the terminal's and the IDE's, picks it up.

// identityDir holds the host identity files, relative to agent's home.
const identityDir = ".config/berth/git"

// includePrefix starts every includeIf key git config --get-regexp prints.
const includePrefix = "includeif.hasconfig:remote.*.url:"

var hostPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*(:[0-9]{1,5})?$`)

// HostIdentity is the git identity used for the remotes on one host.
type HostIdentity struct {
	Host  string `json:"host"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func validGitName(s string) bool {
	return s != "" && len(s) <= 100 && !strings.HasPrefix(s, "-") &&
		strings.IndexFunc(s, unicode.IsControl) < 0
}

// normalHost lowercases a host and reports whether it is a DNS name with an
// optional port, which keeps it safe in a config key and a file name.
func normalHost(s string) (string, bool) {
	h := strings.ToLower(strings.TrimSpace(s))
	return h, len(h) <= 253 && hostPattern.MatchString(h)
}

// remotePatterns are the remote URLs a host identity covers: HTTPS, and SSH
// when the host names no port, since an HTTPS port is not SSH's.
func remotePatterns(host string) []string {
	p := []string{"https://" + host + "/**"}
	if !strings.Contains(host, ":") {
		p = append(p, "ssh://git@"+host+"/**", "git@"+host+":*/**")
	}
	return p
}

func identityInclude(host string) string { return "~/" + identityDir + "/" + host + ".gitconfig" }

func includeSection(pattern string) string { return "includeIf.hasconfig:remote.*.url:" + pattern }

// agentHome is the home the host identity files live in.
func (b *Broker) agentHome() string {
	if b.Agent != nil {
		return b.Agent.Home
	}
	return os.Getenv("HOME")
}

func (b *Broker) identityFile(host string) string {
	return path.Join(b.agentHome(), identityDir, host+".gitconfig")
}

// gitRun runs git as agent and turns a failure into a verb answer.
func (b *Broker) gitRun(ctx context.Context, args ...string) (string, int, any, bool) {
	stdout, stderr, exit, err := b.agentRun(ctx, b.syncTimeout(), b.Home.Git, args...)
	code, v, failed := syncFailure(err, exit, stderr)
	return stdout, code, v, failed
}

// includes maps each URL pattern the global config includes a file for to
// that file.
func (b *Broker) includes(ctx context.Context) (map[string]string, error) {
	stdout, stderr, exit, err := b.agentRun(ctx, b.syncTimeout(), b.Home.Git,
		"config", "--global", "--get-regexp", `^includeif\.hasconfig:remote\.\*\.url:.*\.path$`)
	if err != nil {
		return nil, err
	}
	if exit != 0 && exit != 1 { // 1: nothing matched
		return nil, fmt.Errorf("git config: %s", cmp.Or(strings.TrimSpace(stderr), fmt.Sprintf("exit %d", exit)))
	}
	out := map[string]string{}
	for line := range strings.Lines(stdout) {
		key, value, _ := strings.Cut(strings.TrimRight(line, "\n"), " ")
		if pattern, ok := strings.CutPrefix(key, includePrefix); ok {
			if pattern, ok = strings.CutSuffix(pattern, ".path"); ok {
				out[pattern] = value
			}
		}
	}
	return out, nil
}

// identityHosts lists the hosts the global config includes an identity for.
func identityHosts(includes map[string]string) []string {
	var hosts []string
	for _, file := range includes {
		name, ok := strings.CutPrefix(file, "~/"+identityDir+"/")
		if !ok {
			continue
		}
		if host, ok := strings.CutSuffix(name, ".gitconfig"); ok && hostPattern.MatchString(host) && !slices.Contains(hosts, host) {
			hosts = append(hosts, host)
		}
	}
	slices.Sort(hosts)
	return hosts
}

// hostIdentities reads every host identity; one it cannot read is left out.
func (b *Broker) hostIdentities(ctx context.Context) []HostIdentity {
	inc, err := b.includes(ctx)
	if err != nil {
		return []HostIdentity{}
	}
	out := []HostIdentity{}
	for _, host := range identityHosts(inc) {
		stdout, _, _, err := b.agentRun(ctx, b.syncTimeout(), b.Home.Git,
			"config", "--file", b.identityFile(host), "--get-regexp", `^user\.(name|email)$`)
		if err != nil {
			continue
		}
		id := HostIdentity{Host: host}
		for line := range strings.Lines(stdout) {
			key, value, _ := strings.Cut(strings.TrimRight(line, "\n"), " ")
			switch key {
			case "user.name":
				id.Name = value
			case "user.email":
				id.Email = value
			}
		}
		out = append(out, id)
	}
	return out
}

// appendIncludes (re)writes host's includes at the end of the global config.
// Git applies config in order, so an include must follow the [user] section
// it overrides; removing the section first drops its old place.
func (b *Broker) appendIncludes(ctx context.Context, host string, inc map[string]string) (int, any, bool) {
	for _, pattern := range remotePatterns(host) {
		if _, ok := inc[pattern]; ok {
			if _, code, v, failed := b.gitRun(ctx, "config", "--global", "--remove-section", includeSection(pattern)); failed {
				return code, v, true
			}
		}
		if _, code, v, failed := b.gitRun(ctx, "config", "--global", includeSection(pattern)+".path", identityInclude(host)); failed {
			return code, v, true
		}
	}
	return 0, nil, false
}

// foreignInclude names a pattern of host's that the global config already
// includes some other file for, which Berth must not overwrite.
func foreignInclude(host string, inc map[string]string) (string, string) {
	for _, pattern := range remotePatterns(host) {
		if file, ok := inc[pattern]; ok && file != identityInclude(host) {
			return pattern, file
		}
	}
	return "", ""
}

// gitIdentity sets the global git identity, or with a host the identity for
// that host's remotes.
func (b *Broker) gitIdentity(ctx context.Context, body []byte) (int, any) {
	var in struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Host  string `json:"host"`
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
	host, ok := normalHost(in.Host)
	if in.Host != "" && !ok {
		return badRequest(errors.New("the host must be a name such as gitlab.com, with an optional port"))
	}
	inc, err := b.includes(ctx)
	if err != nil {
		return fail(http.StatusBadGateway, "command_failed", "%v", err)
	}
	if in.Host == "" {
		for _, kv := range [][2]string{{"user.name", in.Name}, {"user.email", in.Email}} {
			if _, code, v, failed := b.gitRun(ctx, "config", "--global", kv[0], kv[1]); failed {
				return code, v
			}
		}
		// A [user] section git just created sits below the host includes.
		for _, h := range identityHosts(inc) {
			if code, v, failed := b.appendIncludes(ctx, h, inc); failed {
				return code, v
			}
		}
		return http.StatusOK, map[string]any{"name": in.Name, "email": in.Email}
	}
	if pattern, file := foreignInclude(host, inc); pattern != "" {
		return fail(http.StatusConflict, "include_exists",
			"your git config already includes %s for %s; remove it to let Berth manage %s", file, pattern, host)
	}
	if b.agentHome() == "" {
		return fail(http.StatusInternalServerError, "no_agent_user", "there is no agent home to keep the identity in")
	}
	file := b.identityFile(host)
	_, stderr, exit, err := b.agentRun(ctx, b.syncTimeout(), "mkdir", "-p", path.Dir(file))
	if code, v, failed := syncFailure(err, exit, stderr); failed {
		return code, v
	}
	for _, kv := range [][2]string{{"user.name", in.Name}, {"user.email", in.Email}} {
		if _, code, v, failed := b.gitRun(ctx, "config", "--file", file, kv[0], kv[1]); failed {
			return code, v
		}
	}
	if code, v, failed := b.appendIncludes(ctx, host, inc); failed {
		return code, v
	}
	return http.StatusOK, map[string]any{"host": host, "name": in.Name, "email": in.Email}
}

// gitIdentityRemove drops a host's identity, so its remotes use the global one.
func (b *Broker) gitIdentityRemove(ctx context.Context, body []byte) (int, any) {
	var in struct {
		Host string `json:"host"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	host, ok := normalHost(in.Host)
	if !ok {
		return badRequest(errors.New("the host must be a name such as gitlab.com, with an optional port"))
	}
	inc, err := b.includes(ctx)
	if err != nil {
		return fail(http.StatusBadGateway, "command_failed", "%v", err)
	}
	if !slices.Contains(identityHosts(inc), host) {
		return fail(http.StatusNotFound, "unknown_host", "there is no git identity for %s", host)
	}
	for pattern, file := range inc {
		if file != identityInclude(host) {
			continue
		}
		if _, code, v, failed := b.gitRun(ctx, "config", "--global", "--remove-section", includeSection(pattern)); failed {
			return code, v
		}
	}
	if b.agentHome() != "" {
		_, stderr, exit, err := b.agentRun(ctx, b.syncTimeout(), "rm", "-f", b.identityFile(host))
		if code, v, failed := syncFailure(err, exit, stderr); failed {
			return code, v
		}
	}
	return http.StatusOK, map[string]any{"host": host}
}
