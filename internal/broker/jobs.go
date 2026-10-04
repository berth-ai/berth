package broker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sync"
	"time"
)

// Job states.
const (
	StateRunning   = "running"
	StateSucceeded = "succeeded"
	StateFailed    = "failed"
)

const (
	maxJobs         = 50
	defaultLogLimit = 256 << 10
)

var (
	// ErrJobRunning is returned by Start while a job of the same group runs.
	ErrJobRunning = errors.New("another job of this kind is running")
	// ErrNoJob is returned by Get for an unknown or forgotten job.
	ErrNoJob = errors.New("no such job")
	// ErrJobDone is returned by Input and Cancel for a finished job.
	ErrJobDone = errors.New("the job has finished")
	// ErrNoInput is returned by Input for a job started without input.
	ErrNoInput = errors.New("the job takes no input")
)

// Job is a snapshot of one job.
type Job struct {
	ID           int             `json:"id"`
	Verb         string          `json:"verb"`
	Group        string          `json:"group"`
	Args         []string        `json:"args"`
	State        string          `json:"state"`
	StartedAt    time.Time       `json:"started_at"`
	FinishedAt   *time.Time      `json:"finished_at"`
	ExitCode     *int            `json:"exit_code"`
	Result       json.RawMessage `json:"result"`
	Log          string          `json:"log,omitempty"`
	LogTruncated bool            `json:"log_truncated"`
	Device       *Device         `json:"device,omitempty"`
}

// Work runs one job, writing its log to out. It returns the exit code of the
// process it ran and the job's JSON result; err means it could not run.
type Work func(ctx context.Context, out io.Writer) (exit int, result json.RawMessage, err error)

// InputWork is Work that also reads in, which carries what Input sends.
type InputWork func(ctx context.Context, in io.Reader, out io.Writer) (exit int, result json.RawMessage, err error)

// Jobs runs work in the background and remembers the last 50 jobs. At most
// one job per group runs at a time. Jobs live as long as the context given to
// NewJobs, not as long as the request that started them.
type Jobs struct {
	Now      func() time.Time
	LogLimit int
	Log      io.Writer // receives every job's log too; nil discards it
	// Annotate may add fields derived from a job's log to snapshots that
	// include the log.
	Annotate func(job *Job, log string)

	ctx  context.Context
	wg   sync.WaitGroup
	mu   sync.Mutex
	last int
	jobs []*entry // oldest first
}

type entry struct {
	job       Job
	out       *tailBuffer
	hasInput  bool
	in        *os.File // the write end of the job's input; nil once it ended
	cancel    context.CancelFunc
	cancelled bool
}

// NewJobs returns an empty history whose jobs are cancelled with ctx.
func NewJobs(ctx context.Context) *Jobs {
	return &Jobs{Now: time.Now, LogLimit: defaultLogLimit, ctx: ctx}
}

// Start runs work in the background as a new job.
func (j *Jobs) Start(verb, group string, args []string, work Work) (Job, error) {
	return j.start(verb, group, args, nil, nil, func(ctx context.Context, _ io.Reader, out io.Writer) (int, json.RawMessage, error) {
		return work(ctx, out)
	})
}

// StartInput is Start for work that reads input. in is a pipe: Input writes
// to it, and it closes when the job ends or is cancelled.
func (j *Jobs) StartInput(verb, group string, args []string, work InputWork) (Job, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return Job{}, err
	}
	job, err := j.start(verb, group, args, r, w, work)
	if err != nil {
		r.Close()
		w.Close()
	}
	return job, err
}

func (j *Jobs) start(verb, group string, args []string, r, w *os.File, work InputWork) (Job, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, e := range j.jobs {
		if e.job.Group == group && e.job.State == StateRunning {
			return Job{}, ErrJobRunning
		}
	}
	j.last++
	ctx, cancel := context.WithCancel(j.ctx)
	e := &entry{
		job: Job{ID: j.last, Verb: verb, Group: group, Args: append([]string{}, args...),
			State: StateRunning, StartedAt: j.Now().UTC(), Result: json.RawMessage("null")},
		out:      &tailBuffer{limit: j.LogLimit},
		hasInput: w != nil,
		in:       w,
		cancel:   cancel,
	}
	j.jobs = append(j.jobs, e)
	j.trim()
	j.logf("job %d (%s) started", e.job.ID, verb)
	j.wg.Add(1)
	var in io.Reader
	if r != nil {
		in = r
	}
	go j.run(ctx, e, in, work)
	return e.snapshot(true, j.Annotate), nil
}

// trim forgets the oldest finished jobs beyond maxJobs. Caller holds j.mu.
func (j *Jobs) trim() {
	for len(j.jobs) > maxJobs {
		i := slices.IndexFunc(j.jobs, func(e *entry) bool { return e.job.State != StateRunning })
		if i < 0 {
			return
		}
		j.jobs = slices.Delete(j.jobs, i, i+1)
	}
}

func (j *Jobs) run(ctx context.Context, e *entry, in io.Reader, work InputWork) {
	defer j.wg.Done()
	defer e.cancel()
	// A cancelled job's input ends, so work blocked reading it returns.
	stop := context.AfterFunc(ctx, func() { j.closeInput(e) })
	defer stop()
	var out io.Writer = e.out
	if j.Log != nil {
		out = io.MultiWriter(e.out, j.Log)
	}
	exit, result, err := work(ctx, in, out)
	if f, ok := in.(*os.File); ok {
		f.Close()
	}
	switch {
	case err != nil:
		result = errorJSON("exec_failed", err.Error())
	case len(result) == 0:
		result = json.RawMessage("{}")
	case !json.Valid(result):
		result = errorJSON("bad_output", "the job produced no valid JSON")
	}
	j.closeInput(e)
	j.mu.Lock()
	defer j.mu.Unlock()
	now := j.Now().UTC()
	e.job.FinishedAt = &now
	e.job.Result = result
	e.job.State = StateFailed
	if err == nil {
		e.job.ExitCode = &exit
		if exit == 0 {
			e.job.State = StateSucceeded
		}
	}
	if e.cancelled {
		e.job.State = StateFailed
		e.job.Result = errorJSON("cancelled", "cancelled")
	}
	j.logf("job %d (%s) %s", e.job.ID, e.job.Verb, e.job.State)
}

// closeInput ends a job's input, once.
func (j *Jobs) closeInput(e *entry) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if e.in != nil {
		e.in.Close()
		e.in = nil
	}
}

// find returns the entry of job id, or nil. Caller holds j.mu.
func (j *Jobs) find(id int) *entry {
	for _, e := range j.jobs {
		if e.job.ID == id {
			return e
		}
	}
	return nil
}

// Get returns one job with its log.
func (j *Jobs) Get(id int) (Job, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if e := j.find(id); e != nil {
		return e.snapshot(true, j.Annotate), nil
	}
	return Job{}, ErrNoJob
}

// Input writes data to a running job's input.
func (j *Jobs) Input(id int, data []byte) error {
	j.mu.Lock()
	e := j.find(id)
	var w *os.File
	err := ErrNoJob
	switch {
	case e == nil:
	case !e.hasInput:
		err = ErrNoInput
	case e.job.State != StateRunning || e.in == nil:
		err = ErrJobDone
	default:
		w, err = e.in, nil
	}
	j.mu.Unlock()
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return ErrJobDone // the job ended and closed its input meanwhile
	}
	return nil
}

// Cancel stops a running job; it ends failed with a "cancelled" result.
func (j *Jobs) Cancel(id int) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	e := j.find(id)
	switch {
	case e == nil:
		return ErrNoJob
	case e.job.State != StateRunning:
		return ErrJobDone
	}
	e.cancelled = true
	e.cancel()
	return nil
}

// List returns every remembered job, newest first, without logs.
func (j *Jobs) List() []Job {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]Job, 0, len(j.jobs))
	for _, e := range slices.Backward(j.jobs) {
		out = append(out, e.snapshot(false, j.Annotate))
	}
	return out
}

// Wait blocks until every running job has finished.
func (j *Jobs) Wait() { j.wg.Wait() }

func (e *entry) snapshot(withLog bool, annotate func(*Job, string)) Job {
	job := e.job
	job.Args = append([]string{}, e.job.Args...)
	if withLog {
		job.Log, job.LogTruncated = e.out.snapshot()
		if annotate != nil {
			annotate(&job, job.Log)
		}
	}
	return job
}

func (j *Jobs) logf(format string, args ...any) {
	if j.Log != nil {
		fmt.Fprintf(j.Log, "berth: "+format+"\n", args...)
	}
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
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf), t.truncated
}
