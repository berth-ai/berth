package agentlogin

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/berth-ai/berth/internal/locksource"
)

// ErrNotSignedIn means the sign-in ended without the method signed in.
var ErrNotSignedIn = errors.New("the sign-in did not complete")

var (
	urlPattern  = regexp.MustCompile("https://[^\\s\"'<>`]+")
	codePattern = regexp.MustCompile(`\b[A-Z0-9]{4,}-[A-Z0-9]{4,}\b`)
)

// Session is one sign-in. Out receives the cleaned CLI output, each line
// prefixed "│ ", and the marker lines the broker parses; In carries pasted
// codes, one per line.
type Session struct {
	Env    Env
	Agent  Agent
	Method locksource.LoginMethod
	In     io.Reader
	Out    io.Writer

	Poll       time.Duration // status polling after the first URL; 0 means 2s
	URLTimeout time.Duration // 0 means 60s
	Idle       time.Duration // a partial line is flushed after this; 0 means 200ms
	Grace      time.Duration // SIGTERM to SIGKILL; 0 means 3s
}

// progress is what the session has seen since the last URL.
type progress struct {
	url      string
	code     bool // device: the code for url was marked
	waited   bool // paste: the prompt for url was marked
	answered int
}

// Run signs in. It returns nil once the method's status reports signed in,
// ErrNotSignedIn when the CLI gives up first, or ctx's error.
func (s Session) Run(ctx context.Context) error {
	poll, idleFor := cmp.Or(s.Poll, 2*time.Second), cmp.Or(s.Idle, 200*time.Millisecond)
	grace := cmp.Or(s.Grace, 3*time.Second)
	if err := s.Env.prepare(s.Method.Prepare); err != nil {
		return s.fail("could not prepare the settings: %v", err)
	}
	s.mark("start %s", s.Method.Flow)
	c, err := s.Env.start(s.Agent, s.Method)
	if err != nil {
		return s.fail("could not start %s: %v", filepath.Base(s.Agent.Bin), err)
	}
	quit := make(chan struct{})
	defer close(quit)
	defer c.out.Close()

	chunks := make(chan []byte, 16)
	go func() {
		defer close(chunks)
		buf := make([]byte, 32<<10)
		for {
			n, err := c.out.Read(buf)
			if n > 0 {
				select {
				case chunks <- bytes.Clone(buf[:n]):
				case <-quit:
					return
				}
			}
			if err != nil {
				return // EOF, or EIO from a pty whose CLI exited
			}
		}
	}()
	exited := make(chan error, 1)
	go func() { exited <- c.cmd.Wait() }()
	inputs := make(chan string)
	if s.In != nil {
		go func() {
			sc := bufio.NewScanner(s.In)
			for sc.Scan() {
				select {
				case inputs <- sc.Text():
				case <-quit:
					return
				}
			}
		}()
	}

	var cl cleaner
	var pr progress
	urlTimer := time.NewTimer(cmp.Or(s.URLTimeout, 60*time.Second))
	defer urlTimer.Stop()
	idle := time.NewTimer(idleFor)
	idle.Stop()
	var poller *time.Ticker
	defer func() {
		if poller != nil {
			poller.Stop()
		}
	}()
	var pollC <-chan time.Time
	for {
		select {
		case b, ok := <-chunks:
			if !ok {
				chunks = nil
				continue
			}
			s.handle(c, &cl, &pr, cl.write(b))
			idle.Reset(idleFor)
		case <-idle.C:
			s.handle(c, &cl, &pr, cl.flush())
		case line := <-inputs:
			s.forward(c, &cl, line)
		case <-urlTimer.C:
			if pr.url == "" {
				c.stop(0)
				<-exited
				return s.fail("the sign-in page did not appear; the CLI's output is above")
			}
		case <-pollC:
			if p, err := s.Env.Probe(ctx, s.Agent, s.Method); err == nil && p.SignedIn {
				c.stop(grace)
				s.settle(c, exited, &cl, &pr, chunks, grace+time.Second)
				return s.done(p)
			}
		case err := <-exited:
			c.stop(0) // whatever it left behind in its session
			s.drain(c, &cl, &pr, chunks)
			if p, perr := s.Env.Probe(ctx, s.Agent, s.Method); perr == nil && p.SignedIn {
				return s.done(p)
			}
			return s.fail("%s", exitText(filepath.Base(s.Agent.Bin), err))
		case <-ctx.Done():
			c.stop(0)
			<-exited
			return ctx.Err()
		}
		if poller == nil && pr.url != "" {
			urlTimer.Stop()
			poller = time.NewTicker(poll)
			pollC = poller.C
		}
	}
}

// handle logs lines, answers menus and marks URLs, codes and prompts.
func (s Session) handle(c *child, cl *cleaner, pr *progress, lines []string) {
	m := s.Method
	for _, line := range lines {
		if cl.fresh(line) {
			fmt.Fprintf(s.Out, "│ %s\n", line)
		}
		if pr.answered < len(m.Answers) && strings.Contains(line, m.Answers[pr.answered].Expect) {
			_, _ = io.WriteString(c.in, m.Answers[pr.answered].Send)
			pr.answered++
		}
		if u := strings.TrimRight(urlPattern.FindString(line), ".,;)"); u != "" && u != pr.url {
			pr.url, pr.code, pr.waited = u, false, false
			s.mark("url %s", u)
		}
		if pr.url == "" {
			continue
		}
		switch {
		case m.Flow == locksource.FlowDevice && !pr.code:
			if code := codePattern.FindString(urlPattern.ReplaceAllString(line, "")); code != "" {
				pr.code = true
				s.mark("device-code %s", code)
			}
		case m.Flow == locksource.FlowPaste && !pr.waited && strings.Contains(line, m.Prompt):
			pr.waited = true
			s.mark("waiting for code")
		}
	}
}

// forward passes one pasted code to the CLI and hides it from the log.
func (s Session) forward(c *child, cl *cleaner, line string) {
	code := strings.TrimSpace(line)
	if code == "" {
		return
	}
	cl.redact(code)
	eol := "\n"
	if s.Method.PTY {
		eol = "\r"
	}
	if _, err := io.WriteString(c.in, code+eol); err != nil {
		fmt.Fprintf(s.Out, "berth: could not pass the code on: %v\n", err)
		return
	}
	s.mark("input received")
}

// drain handles the output left after the CLI exited, waiting at most a
// second: a background child may hold the output open.
func (s Session) drain(c *child, cl *cleaner, pr *progress, chunks <-chan []byte) {
	deadline := time.After(time.Second)
	for chunks != nil {
		select {
		case b, ok := <-chunks:
			if !ok {
				chunks = nil
				continue
			}
			s.handle(c, cl, pr, cl.write(b))
		case <-deadline:
			chunks = nil
		}
	}
	s.handle(c, cl, pr, cl.flush())
}

// settle waits for a stopped CLI to exit, logging what it prints meanwhile.
func (s Session) settle(c *child, exited <-chan error, cl *cleaner, pr *progress, chunks <-chan []byte, limit time.Duration) {
	select {
	case <-exited:
	case <-time.After(limit):
	}
	s.drain(c, cl, pr, chunks)
}

func (s Session) mark(format string, args ...any) {
	fmt.Fprintf(s.Out, "berth: login "+format+"\n", args...)
}

func (s Session) done(p Probe) error {
	if p.Account != "" {
		s.mark("signed in as %s", p.Account)
	} else {
		s.mark("signed in")
	}
	return nil
}

func (s Session) fail(format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	s.mark("failed: %s", msg)
	return fmt.Errorf("%w: %s", ErrNotSignedIn, msg)
}

func exitText(bin string, err error) string {
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee) && ee.ExitCode() >= 0:
		return fmt.Sprintf("%s exited with status %d before the sign-in completed", bin, ee.ExitCode())
	case err != nil:
		return fmt.Sprintf("%s stopped: %v", bin, err)
	}
	return bin + " exited before the sign-in completed"
}
