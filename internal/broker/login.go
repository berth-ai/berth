package broker

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/locksource"
	"github.com/berth-ai/berth/internal/runner"
)

const (
	// loginVerb is the job of berth login run; agentLoginGroup runs one at a time.
	loginVerb        = "agent.login"
	agentLoginGroup  = "agent-login"
	loginSyncTimeout = 30 * time.Second
	maxCodeBytes     = 2048
	markerPrefix     = "berth: login "
)

// LoginState is what an agent.login job's marker lines say.
type LoginState struct {
	Agent    string `json:"agent"`
	Method   string `json:"method"`
	Flow     string `json:"flow"`
	URL      string `json:"url"`
	Code     string `json:"code"`
	Waiting  bool   `json:"waiting"`
	Inputs   int    `json:"inputs"`
	SignedIn bool   `json:"signed_in"`
	Account  string `json:"account"`
	Failed   string `json:"failed"`
}

// ParseLogin reads the marker lines berth login run prints. Only lines
// that start with the marker count: the CLI's own lines start with "│ ".
// The last two of args are the agent and the method.
func ParseLogin(args []string, log string) *LoginState {
	st := &LoginState{}
	if n := len(args); n >= 2 {
		st.Agent, st.Method = args[n-2], args[n-1]
	}
	for _, line := range strings.Split(log, "\n") {
		rest, ok := strings.CutPrefix(line, markerPrefix)
		if !ok {
			continue
		}
		switch {
		case strings.HasPrefix(rest, "start "):
			st.Flow = strings.TrimPrefix(rest, "start ")
		case strings.HasPrefix(rest, "url "):
			st.URL, st.Code, st.Waiting = strings.TrimPrefix(rest, "url "), "", false
		case strings.HasPrefix(rest, "device-code "):
			st.Code = strings.TrimPrefix(rest, "device-code ")
		case rest == "waiting for code":
			st.Waiting = true
		case rest == "input received":
			st.Inputs++
			st.Waiting = false
		case rest == "signed in":
			st.SignedIn = true
		case strings.HasPrefix(rest, "signed in as "):
			st.SignedIn, st.Account = true, strings.TrimPrefix(rest, "signed in as ")
		case strings.HasPrefix(rest, "failed: "):
			st.Failed = strings.TrimPrefix(rest, "failed: ")
		}
	}
	return st
}

type loginTarget struct {
	Agent  string `json:"agent"`
	Method string `json:"method"`
}

func (t loginTarget) check() error {
	if !config.ValidPackage(t.Agent) {
		return fmt.Errorf("agent %q is not a package name", t.Agent)
	}
	if !locksource.LoginIDPattern.MatchString(t.Method) {
		return fmt.Errorf("method %q is not a sign-in method id", t.Method)
	}
	return nil
}

func (b *Broker) loginStatus(ctx context.Context, body []byte) (int, any) {
	if err := decode(body, &struct{}{}); err != nil {
		return badRequest(err)
	}
	stdout, stderr, exit, err := b.agentRun(ctx, loginSyncTimeout, b.Home.Berth, "login", "status", "--json")
	if code, v, failed := syncFailure(err, exit, stderr); failed {
		return code, v
	}
	if !json.Valid([]byte(stdout)) {
		return fail(http.StatusBadGateway, "bad_output", "berth login status printed no JSON")
	}
	return http.StatusOK, json.RawMessage(stdout)
}

func (b *Broker) loginStart(_ context.Context, body []byte) (int, any) {
	var in loginTarget
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if err := in.check(); err != nil {
		return badRequest(err)
	}
	timeout := cmp.Or(b.LoginTimeout, defaultLoginTimeout)
	args := []string{"login", "run", in.Agent, in.Method}
	shown := append([]string{b.Home.Berth}, args...)
	job, err := b.Jobs.StartInput(loginVerb, agentLoginGroup, shown, func(ctx context.Context, stdin io.Reader, out io.Writer) (int, json.RawMessage, error) {
		r := runner.Exec{Agent: b.Agent, Stdout: out, Stderr: out}
		err := r.Run(ctx, runner.Cmd{Path: b.Home.Berth, Args: args, Stdin: stdin, AsAgent: true, Timeout: timeout})
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode(), nil, nil
		}
		if err != nil {
			return -1, nil, err
		}
		return 0, nil, nil
	})
	if errors.Is(err, ErrJobRunning) {
		return fail(http.StatusConflict, "job_running", "another sign-in is running; finish or cancel it first")
	}
	if err != nil {
		return fail(http.StatusInternalServerError, "internal", "%v", err)
	}
	return http.StatusAccepted, map[string]any{"job": job}
}

// signInJob returns a job the sign-in verbs may act on; a non-zero status
// means they may not.
func (b *Broker) signInJob(id int, verbs ...string) (Job, int, any) {
	job, err := b.Jobs.Get(id)
	if err != nil {
		code, v := fail(http.StatusNotFound, "no_job", "no job %d", id)
		return job, code, v
	}
	for _, v := range verbs {
		if job.Verb == v {
			return job, 0, nil
		}
	}
	code, v := fail(http.StatusBadRequest, "not_a_sign_in", "job %d is not a sign-in", id)
	return job, code, v
}

func (b *Broker) loginInput(_ context.Context, body []byte) (int, any) {
	var in struct {
		Job  int    `json:"job"`
		Code string `json:"code"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	code := strings.TrimSpace(in.Code)
	if code == "" || len(code) > maxCodeBytes || !utf8.ValidString(code) || strings.IndexFunc(code, unicode.IsControl) >= 0 {
		return badRequest(errors.New("the code must be 1 to 2048 characters on one line"))
	}
	job, status, v := b.signInJob(in.Job, loginVerb)
	if status != 0 {
		return status, v
	}
	if job.Login == nil || job.Login.Flow != locksource.FlowPaste {
		return fail(http.StatusBadRequest, "no_code", "this sign-in takes no code")
	}
	switch err := b.Jobs.Input(in.Job, []byte(code+"\n")); {
	case errors.Is(err, ErrJobDone):
		return fail(http.StatusConflict, "job_finished", "the sign-in has ended; start it again")
	case err != nil:
		return fail(http.StatusInternalServerError, "internal", "%v", err)
	}
	return http.StatusOK, map[string]any{"ok": true}
}

func (b *Broker) loginCancel(_ context.Context, body []byte) (int, any) {
	var in struct {
		Job int `json:"job"`
	}
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if _, status, v := b.signInJob(in.Job, loginVerb, "forge.login"); status != 0 {
		return status, v
	}
	switch err := b.Jobs.Cancel(in.Job); {
	case errors.Is(err, ErrJobDone):
		return fail(http.StatusConflict, "job_finished", "the sign-in has already ended")
	case err != nil:
		return fail(http.StatusInternalServerError, "internal", "%v", err)
	}
	return http.StatusOK, map[string]any{"ok": true}
}

func (b *Broker) loginLogout(ctx context.Context, body []byte) (int, any) {
	var in loginTarget
	if err := decode(body, &in); err != nil {
		return badRequest(err)
	}
	if err := in.check(); err != nil {
		return badRequest(err)
	}
	stdout, stderr, exit, err := b.agentRun(ctx, loginSyncTimeout, b.Home.Berth, "login", "logout", in.Agent, in.Method, "--json")
	if err != nil {
		code, v, _ := syncFailure(err, exit, stderr)
		return code, v
	}
	if exit != 0 {
		// With --json, berth prints its error on stdout.
		var doc struct {
			Error struct{ Code, Message string } `json:"error"`
		}
		if json.Unmarshal([]byte(stdout), &doc) != nil || doc.Error.Code == "" {
			code, v, _ := syncFailure(nil, exit, stderr)
			return code, v
		}
		status := http.StatusBadGateway
		switch doc.Error.Code {
		case "unknown":
			status = http.StatusNotFound
		case "still_signed_in":
			status = http.StatusConflict
		}
		return fail(status, doc.Error.Code, "%s", doc.Error.Message)
	}
	return http.StatusOK, json.RawMessage(stdout)
}
