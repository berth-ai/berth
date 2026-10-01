// Package broker runs the berth commands the web panel may request, as root,
// behind a unix socket. It accepts a fixed set of verbs, builds every argv
// itself and never runs a shell, so the unprivileged panel cannot make it run
// anything else.
package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	maxRequestBytes = 4 << 10
	defaultLogLimit = 256 << 10
	statusTimeout   = 2 * time.Minute
)

var (
	refPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,199}$`)
	hashPattern = regexp.MustCompile(`^[0-9a-f]{12}$`)
)

// Request asks for one mutating berth command. --lock-dir is deliberately not
// expressible: it would let a caller point root at any directory.
type Request struct {
	Action string `json:"action"`
	Ref    string `json:"ref,omitempty"`
	Image  bool   `json:"image,omitempty"`
	Hash   string `json:"hash,omitempty"`
}

// ParseRequest decodes exactly one JSON object of at most 4 KiB with no
// unknown fields.
func ParseRequest(r io.Reader) (Request, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxRequestBytes+1))
	if err != nil {
		return Request{}, err
	}
	if len(raw) > maxRequestBytes {
		return Request{}, fmt.Errorf("request is larger than %d bytes", maxRequestBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var req Request
	if err := dec.Decode(&req); err != nil {
		return Request{}, fmt.Errorf("invalid request: %w", err)
	}
	if dec.More() {
		return Request{}, errors.New("invalid request: trailing data")
	}
	return req, nil
}

// Args validates the request and returns the berth argv for it.
func (r Request) Args() ([]string, error) {
	switch r.Action {
	case "update":
		if r.Hash != "" {
			return nil, errors.New("update takes no hash")
		}
		args := []string{"update", "--json"}
		switch {
		case r.Ref != "" && r.Image:
			return nil, errors.New("ref and image are exclusive")
		case r.Image:
			args = append(args, "--image")
		case r.Ref != "":
			if !refPattern.MatchString(r.Ref) || strings.Contains(r.Ref, "..") {
				return nil, fmt.Errorf("invalid ref %q", r.Ref)
			}
			args = append(args, "--ref", r.Ref)
		}
		return args, nil
	case "rollback":
		if r.Ref != "" || r.Image {
			return nil, errors.New("rollback takes only a hash")
		}
		args := []string{"rollback", "--json"}
		if r.Hash != "" {
			if !hashPattern.MatchString(r.Hash) {
				return nil, fmt.Errorf("invalid hash %q", r.Hash)
			}
			args = append(args, r.Hash)
		}
		return args, nil
	default:
		return nil, fmt.Errorf("unknown action %q", r.Action)
	}
}

// Exec runs berth with args and returns its exit code. err is non-nil only
// when the process could not be run at all.
type Exec func(ctx context.Context, args []string, stdout, stderr io.Writer) (exit int, err error)

// Job states.
const (
	StateRunning   = "running"
	StateSucceeded = "succeeded"
	StateFailed    = "failed"
)

// Job is a snapshot of the current or last job.
type Job struct {
	ID           int             `json:"id"`
	Action       string          `json:"action"`
	Args         []string        `json:"args"`
	State        string          `json:"state"`
	StartedAt    time.Time       `json:"started_at"`
	FinishedAt   *time.Time      `json:"finished_at"`
	ExitCode     *int            `json:"exit_code"`
	Result       json.RawMessage `json:"result"`
	Log          string          `json:"log"`
	LogTruncated bool            `json:"log_truncated"`
}

// ErrJobRunning is returned by Start while another job runs.
var ErrJobRunning = errors.New("another job is running")

// Broker runs at most one job at a time and remembers the last one.
type Broker struct {
	Exec     Exec
	Now      func() time.Time
	LogLimit int
	Log      io.Writer // receives job output as well; nil discards it

	ctx context.Context
	wg  sync.WaitGroup
	mu  sync.Mutex
	job *Job
	out *tailBuffer
}

// New returns a broker whose jobs live as long as ctx, not as long as the
// request that started them.
func New(ctx context.Context, exec Exec) *Broker {
	return &Broker{Exec: exec, Now: time.Now, LogLimit: defaultLogLimit, ctx: ctx}
}

// Start validates req and runs it in the background.
func (b *Broker) Start(req Request) (Job, error) {
	args, err := req.Args()
	if err != nil {
		return Job{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.job != nil && b.job.State == StateRunning {
		return Job{}, ErrJobRunning
	}
	id := 1
	if b.job != nil {
		id = b.job.ID + 1
	}
	b.job = &Job{ID: id, Action: req.Action, Args: args, State: StateRunning, StartedAt: b.Now().UTC()}
	b.out = &tailBuffer{limit: b.LogLimit}
	b.logf("job %d started: berth %s", id, strings.Join(args, " "))
	b.wg.Add(1)
	go b.run(b.job, b.out)
	return b.snapshot(), nil
}

func (b *Broker) run(job *Job, out *tailBuffer) {
	defer b.wg.Done()
	var stdout bytes.Buffer
	var stderr io.Writer = out
	if b.Log != nil {
		stderr = io.MultiWriter(out, b.Log)
	}
	exit, err := b.Exec(b.ctx, job.Args, &stdout, stderr)

	var result json.RawMessage
	switch {
	case err != nil:
		result = errorJSON("exec_failed", err.Error())
	case json.Valid(stdout.Bytes()) && stdout.Len() > 0:
		result = json.RawMessage(bytes.TrimSpace(stdout.Bytes()))
	default:
		result = errorJSON("bad_output", "berth printed no valid JSON")
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.Now().UTC()
	job.FinishedAt = &now
	job.Result = result
	if err == nil {
		job.ExitCode = &exit
	}
	job.State = StateFailed
	if err == nil && exit == 0 {
		job.State = StateSucceeded
	}
	b.logf("job %d %s (exit %d)", job.ID, job.State, exit)
}

// Current returns a snapshot of the current or last job; the zero Job when
// none has run.
func (b *Broker) Current() Job {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.snapshot()
}

func (b *Broker) snapshot() Job {
	if b.job == nil {
		return Job{}
	}
	j := *b.job
	j.Args = append([]string(nil), b.job.Args...)
	j.Log, j.LogTruncated = b.out.snapshot()
	return j
}

// Wait blocks until the running job, if any, has finished.
func (b *Broker) Wait() { b.wg.Wait() }

func (b *Broker) logf(format string, args ...any) {
	if b.Log != nil {
		fmt.Fprintf(b.Log, "berth broker: "+format+"\n", args...)
	}
}

// Handler serves the broker API.
func (b *Broker) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", b.handleStatus)
	mux.HandleFunc("POST /jobs", b.handleStart)
	mux.HandleFunc("GET /jobs/current", b.handleCurrent)
	return mux
}

func (b *Broker) handleStatus(w http.ResponseWriter, r *http.Request) {
	args := []string{"status", "--json"}
	if r.URL.Query().Get("check") == "1" {
		args = append(args, "--check")
	}
	ctx, cancel := context.WithTimeout(r.Context(), statusTimeout)
	defer cancel()
	var stdout bytes.Buffer
	stderr := io.Discard
	if b.Log != nil {
		stderr = b.Log
	}
	exit, err := b.Exec(ctx, args, &stdout, stderr)
	switch {
	case err != nil:
		writeJSON(w, http.StatusBadGateway, errorJSON("exec_failed", err.Error()))
	case stdout.Len() == 0 || !json.Valid(stdout.Bytes()):
		writeJSON(w, http.StatusBadGateway, errorJSON("bad_output", "berth status printed no valid JSON"))
	case exit != 0:
		writeJSON(w, http.StatusBadGateway, json.RawMessage(stdout.Bytes()))
	default:
		writeJSON(w, http.StatusOK, json.RawMessage(stdout.Bytes()))
	}
}

func (b *Broker) handleStart(w http.ResponseWriter, r *http.Request) {
	req, err := ParseRequest(r.Body)
	var job Job
	if err == nil {
		job, err = b.Start(req)
	}
	switch {
	case errors.Is(err, ErrJobRunning):
		writeJSON(w, http.StatusConflict, errorJSON("job_running", err.Error()))
	case err != nil:
		writeJSON(w, http.StatusBadRequest, errorJSON("invalid_request", err.Error()))
	default:
		writeJSON(w, http.StatusAccepted, map[string]any{"job": job})
	}
}

func (b *Broker) handleCurrent(w http.ResponseWriter, _ *http.Request) {
	job := b.Current()
	if job.ID == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"job": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func errorJSON(code, message string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"error": map[string]string{"code": code, "message": message}})
	return raw
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// tailBuffer keeps the last limit bytes written to it.
type tailBuffer struct {
	mu        sync.Mutex
	buf       []byte
	limit     int
	truncated bool
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.limit; over > 0 {
		t.buf = t.buf[:copy(t.buf, t.buf[over:])]
		t.truncated = true
	}
	return len(p), nil
}

func (t *tailBuffer) snapshot() (string, bool) {
	if t == nil {
		return "", false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf), t.truncated
}
