package agentlogin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/berth-ai/berth/internal/locksource"
	"github.com/berth-ai/berth/internal/mcpsync"
)

// ReportSchema is the schema of berth login status --json.
const ReportSchema = 1

// logoutTimeout bounds a logout command.
const logoutTimeout = 30 * time.Second

// probeTimeout bounds one status probe; tests shorten it.
var probeTimeout = 10 * time.Second

// ErrStillSignedIn means a logout ran but the method still reports signed in.
var ErrStillSignedIn = errors.New("still signed in")

// Probe is one method's sign-in state.
type Probe struct {
	SignedIn bool
	Account  string
}

// MethodStatus is one method in berth login status.
type MethodStatus struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Flow     string `json:"flow"`
	SignedIn bool   `json:"signed_in"`
	Account  string `json:"account"`
	Error    string `json:"error"`
}

// AgentStatus is one agent in berth login status.
type AgentStatus struct {
	Name    string         `json:"name"`
	Methods []MethodStatus `json:"methods"`
}

// Report is berth login status --json.
type Report struct {
	Schema int                    `json:"schema"`
	Agents map[string]AgentStatus `json:"agents"`
}

// Probe runs m's status check.
func (e Env) Probe(ctx context.Context, a Agent, m locksource.LoginMethod) (Probe, error) {
	s := m.Status
	if s.Args != nil {
		out, exit, err := e.run(ctx, probeTimeout, a.Bin, s.Args, nil)
		if err != nil || exit != 0 {
			return Probe{}, err
		}
		p := Probe{SignedIn: true}
		if s.Account != "" {
			// ParseDescriptors checked that it compiles with one group.
			if mm := regexp.MustCompile(s.Account).FindStringSubmatch(out); mm != nil {
				p.Account = mm[1]
			}
		}
		return p, nil
	}
	raw, err := os.ReadFile(e.Dirs.Expand(s.File))
	if errors.Is(err, fs.ErrNotExist) {
		return Probe{}, nil
	}
	if err != nil {
		return Probe{}, err
	}
	if s.Key != "" {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			return Probe{}, fmt.Errorf("%s: %w", s.File, err)
		}
		if v, ok := obj[s.Key]; !ok || string(v) == "null" {
			return Probe{}, nil
		}
	}
	p := Probe{SignedIn: true}
	if af := s.AccountFile; af != nil {
		p.Account = jsonString(e.Dirs.Expand(af.File), af.Key)
	}
	return p, nil
}

// jsonString reads the string at top-level key of a JSON file; "" when
// anything is missing.
func jsonString(path, key string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var obj map[string]json.RawMessage
	var s string
	if json.Unmarshal(raw, &obj) != nil || json.Unmarshal(obj[key], &s) != nil {
		return ""
	}
	return s
}

// Status probes every method of every enabled agent with a login, in
// parallel. A failed probe is reported on its method, not returned.
func (e Env) Status(ctx context.Context) (Report, error) {
	agents, err := e.Agents()
	if err != nil {
		return Report{}, err
	}
	rep := Report{Schema: ReportSchema, Agents: map[string]AgentStatus{}}
	var wg sync.WaitGroup
	for _, a := range agents {
		st := AgentStatus{Name: a.Name, Methods: make([]MethodStatus, len(a.Methods))}
		for i, m := range a.Methods {
			st.Methods[i] = MethodStatus{ID: m.ID, Label: m.Label, Flow: m.Flow}
			wg.Go(func() {
				p, err := e.Probe(ctx, a, m)
				ms := &st.Methods[i]
				ms.SignedIn, ms.Account = p.SignedIn, p.Account
				if err != nil {
					ms.Error = err.Error()
				}
			})
		}
		rep.Agents[a.Package] = st
	}
	wg.Wait()
	return rep, nil
}

// Logout signs one method out, then checks that it worked.
func (e Env) Logout(ctx context.Context, pkg, id string) error {
	a, m, err := e.Find(pkg, id)
	if err != nil {
		return err
	}
	if args := m.Logout.Args; args != nil {
		out, exit, err := e.run(ctx, logoutTimeout, a.Bin, args, nil)
		if err != nil {
			return err
		}
		if exit != 0 {
			return fmt.Errorf("%s %s exited %d: %s", filepath.Base(a.Bin), strings.Join(args, " "), exit, lastLine(out))
		}
	}
	for _, f := range m.Logout.Remove {
		// Remove unlinks a symlink; it never follows it.
		if err := os.Remove(e.Dirs.Expand(f)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	p, err := e.Probe(ctx, a, m)
	if err != nil {
		return err
	}
	if p.SignedIn {
		return fmt.Errorf("%w: %s, %s", ErrStillSignedIn, a.Name, m.Label)
	}
	return nil
}

// prepare sets the method's settings key, keeping the rest of the file.
func (e Env) prepare(p *locksource.LoginPrepare) error {
	if p == nil {
		return nil
	}
	path := e.Dirs.Expand(p.File)
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real // keep a settings file linked from a dotfiles repo a link
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	val, _ := json.Marshal(p.Value)
	next, err := mcpsync.SetPath(data, strings.Split(p.Set, "."), val)
	if err != nil {
		return fmt.Errorf("%s: %w", p.File, err)
	}
	if bytes.Equal(next, data) {
		return nil
	}
	perm := fs.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		perm = fi.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".berth-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // fails harmlessly after the rename
	if _, err := tmp.Write(next); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// run runs bin with args, stdin empty, and returns its combined output and
// exit code. err means it could not run or timed out; the whole process
// group is killed then.
func (e Env) run(ctx context.Context, timeout time.Duration, bin string, args, env []string) (string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(e.environ(), env...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if ctx.Err() != nil {
		return out.String(), -1, fmt.Errorf("%s %s did not answer within %s", filepath.Base(bin), strings.Join(args, " "), timeout)
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.String(), ee.ExitCode(), nil
	}
	if err != nil {
		return out.String(), -1, err
	}
	return out.String(), 0, nil
}

// lastLine is the last non-blank line of s.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
