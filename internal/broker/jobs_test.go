package broker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func newTestJobs(t *testing.T) *Jobs {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	j := NewJobs(ctx)
	j.Now = func() time.Time { return time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { cancel(); j.Wait() })
	return j
}

func blocking(release chan struct{}, log string, exit int, result string) Work {
	return func(ctx context.Context, out io.Writer) (int, json.RawMessage, error) {
		io.WriteString(out, log)
		select {
		case <-release:
		case <-ctx.Done():
			return -1, nil, ctx.Err()
		}
		return exit, json.RawMessage(result), nil
	}
}

func waitJob(t *testing.T, j *Jobs, id int, state string) Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if job, err := j.Get(id); err == nil && job.State == state {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %d never reached %s", id, state)
	return Job{}
}

func TestJobLifecycle(t *testing.T) {
	j := newTestJobs(t)
	release := make(chan struct{})
	job, err := j.Start("update", "agents", []string{"update", "--json"}, blocking(release, "installing\n", 0, `{"changed":true}`))
	if err != nil || job.ID != 1 || job.State != StateRunning || job.Verb != "update" || job.Group != "agents" {
		t.Fatalf("Start = %+v %v", job, err)
	}
	if _, err := j.Start("gc", "agents", nil, blocking(release, "", 0, `{}`)); !errors.Is(err, ErrJobRunning) {
		t.Fatalf("second agents job: %v", err)
	}
	other, err := j.Start("tools.add", "tools", nil, func(context.Context, io.Writer) (int, json.RawMessage, error) {
		return 0, nil, nil
	})
	if err != nil {
		t.Fatalf("a job in another group must start: %v", err)
	}
	if got := waitJob(t, j, other.ID, StateSucceeded); string(got.Result) != "{}" {
		t.Errorf("empty result = %s", got.Result)
	}
	if live, _ := j.Get(1); live.Log != "installing\n" {
		t.Errorf("live log = %q", live.Log)
	}
	close(release)
	done := waitJob(t, j, 1, StateSucceeded)
	if *done.ExitCode != 0 || done.FinishedAt == nil || string(done.Result) != `{"changed":true}` {
		t.Errorf("done = %+v", done)
	}
}

func TestJobFailures(t *testing.T) {
	j := newTestJobs(t)
	a, _ := j.Start("rollback", "agents", nil, func(context.Context, io.Writer) (int, json.RawMessage, error) {
		return 3, json.RawMessage(`{"error":{"code":"no_previous"}}`), nil
	})
	if got := waitJob(t, j, a.ID, StateFailed); *got.ExitCode != 3 {
		t.Errorf("exit = %v", got.ExitCode)
	}
	b, _ := j.Start("update", "agents", nil, func(context.Context, io.Writer) (int, json.RawMessage, error) {
		return -1, nil, fmt.Errorf("no such file")
	})
	got := waitJob(t, j, b.ID, StateFailed)
	if got.ExitCode != nil || string(got.Result) != `{"error":{"code":"exec_failed","message":"no such file"}}` {
		t.Errorf("job = %+v %s", got, got.Result)
	}
	c, _ := j.Start("gc", "agents", nil, func(context.Context, io.Writer) (int, json.RawMessage, error) {
		return 0, json.RawMessage(`not json`), nil
	})
	if got := waitJob(t, j, c.ID, StateSucceeded); string(got.Result) != `{"error":{"code":"bad_output","message":"the job produced no valid JSON"}}` {
		t.Errorf("result = %s", got.Result)
	}
}

func TestHistoryKeepsFiftyNewestFirst(t *testing.T) {
	j := newTestJobs(t)
	quick := func(context.Context, io.Writer) (int, json.RawMessage, error) { return 0, nil, nil }
	for i := 1; i <= 55; i++ {
		job, err := j.Start("gc", "agents", nil, quick)
		if err != nil {
			t.Fatal(err)
		}
		waitJob(t, j, job.ID, StateSucceeded)
	}
	list := j.List()
	if len(list) != 50 || list[0].ID != 55 || list[49].ID != 6 {
		t.Fatalf("list: %d jobs, first %d, last %d", len(list), list[0].ID, list[len(list)-1].ID)
	}
	if _, err := j.Get(5); !errors.Is(err, ErrNoJob) {
		t.Errorf("Get(5) = %v", err)
	}
}

func TestListOmitsLogs(t *testing.T) {
	j := newTestJobs(t)
	job, _ := j.Start("gc", "agents", nil, func(_ context.Context, out io.Writer) (int, json.RawMessage, error) {
		io.WriteString(out, "a log line\n")
		return 0, nil, nil
	})
	waitJob(t, j, job.ID, StateSucceeded)
	raw, _ := json.Marshal(j.List())
	if json.Valid(raw) && containsKey(raw, "log") {
		t.Errorf("list carries logs: %s", raw)
	}
}

func containsKey(raw []byte, key string) bool {
	var list []map[string]any
	json.Unmarshal(raw, &list)
	for _, m := range list {
		if _, ok := m[key]; ok {
			return true
		}
	}
	return false
}

func TestShutdownFailsRunningJob(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	j := NewJobs(ctx)
	job, _ := j.Start("update", "agents", nil, blocking(make(chan struct{}), "", 0, `{}`))
	cancel()
	j.Wait()
	if got, _ := j.Get(job.ID); got.State != StateFailed {
		t.Errorf("state = %s", got.State)
	}
}

func TestLogKeepsTail(t *testing.T) {
	var tb tailBuffer
	tb.limit = 8
	io.WriteString(&tb, "0123")
	io.WriteString(&tb, "456789ab")
	got, truncated := tb.snapshot()
	if got != "456789ab" || !truncated {
		t.Errorf("tail = %q truncated=%v", got, truncated)
	}
}

// echoInput copies one line of input to the log, then waits for release.
func echoInput(release chan struct{}) InputWork {
	return func(ctx context.Context, in io.Reader, out io.Writer) (int, json.RawMessage, error) {
		line, err := bufio.NewReader(in).ReadString('\n')
		if err != nil {
			return 1, nil, nil
		}
		fmt.Fprintf(out, "got %s", line)
		select {
		case <-release:
		case <-ctx.Done():
			return -1, nil, ctx.Err()
		}
		return 0, nil, nil
	}
}

func TestInputReachesTheJob(t *testing.T) {
	j := newTestJobs(t)
	release := make(chan struct{})
	job, err := j.StartInput("agent.login", "agent-login", nil, echoInput(release))
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Input(job.ID, []byte("code-123\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for got, _ := j.Get(job.ID); !strings.Contains(got.Log, "got code-123"); got, _ = j.Get(job.ID) {
		if time.Now().After(deadline) {
			t.Fatalf("log = %q", got.Log)
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	waitJob(t, j, job.ID, StateSucceeded)
}

func TestInputAfterFinish(t *testing.T) {
	j := newTestJobs(t)
	job, _ := j.StartInput("agent.login", "agent-login", nil, func(context.Context, io.Reader, io.Writer) (int, json.RawMessage, error) {
		return 0, nil, nil
	})
	waitJob(t, j, job.ID, StateSucceeded)
	done := make(chan error, 1)
	go func() { done <- j.Input(job.ID, []byte("late\n")) }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrJobDone) {
			t.Errorf("Input after finish = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Input to a finished job blocked")
	}
	release := make(chan struct{})
	defer close(release)
	plain, _ := j.Start("update", "agents", nil, blocking(release, "", 0, `{}`))
	if err := j.Input(plain.ID, []byte("x\n")); !errors.Is(err, ErrNoInput) {
		t.Errorf("Input to a job without input = %v", err)
	}
	if err := j.Input(999, []byte("x\n")); !errors.Is(err, ErrNoJob) {
		t.Errorf("Input to an unknown job = %v", err)
	}
}

func TestCancel(t *testing.T) {
	j := newTestJobs(t)
	// echoInput blocks reading input; Cancel must still end it.
	job, _ := j.StartInput("agent.login", "agent-login", []string{"a"}, echoInput(make(chan struct{})))
	if err := j.Cancel(job.ID); err != nil {
		t.Fatal(err)
	}
	got := waitJob(t, j, job.ID, StateFailed)
	if string(got.Result) != `{"error":{"code":"cancelled","message":"cancelled"}}` {
		t.Errorf("result = %s", got.Result)
	}
	if err := j.Cancel(job.ID); !errors.Is(err, ErrJobDone) {
		t.Errorf("second Cancel = %v", err)
	}
	if err := j.Cancel(999); !errors.Is(err, ErrNoJob) {
		t.Errorf("Cancel unknown = %v", err)
	}
	if _, err := j.StartInput("agent.login", "agent-login", nil, echoInput(make(chan struct{}))); err != nil {
		t.Errorf("the group stayed busy after a cancel: %v", err)
	}
}
