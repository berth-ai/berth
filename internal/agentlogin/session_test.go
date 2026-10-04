package agentlogin

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func waitFor(t *testing.T, out *syncBuffer, want string, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Count(out.String(), want) >= n {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("never saw %q %d times in:\n%s", want, n, out.String())
}

// start runs method id of agent a in the background.
func start(t *testing.T, f fixture, id string, in io.Reader) (*syncBuffer, chan error, context.CancelFunc) {
	t.Helper()
	a, m, err := f.env.Find("a", id)
	if err != nil {
		t.Fatal(err)
	}
	out := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := Session{Env: f.env, Agent: a, Method: m, In: in, Out: out,
		Poll: 50 * time.Millisecond, URLTimeout: 2 * time.Second, Idle: 50 * time.Millisecond, Grace: 200 * time.Millisecond}
	errc := make(chan error, 1)
	go func() { errc <- s.Run(ctx) }()
	return out, errc, cancel
}

func startSession(t *testing.T, f fixture) (*syncBuffer, chan error, context.CancelFunc) {
	t.Helper()
	return start(t, f, "m", nil)
}

func wait(t *testing.T, errc chan error) error {
	t.Helper()
	select {
	case err := <-errc:
		return err
	case <-time.After(15 * time.Second):
		t.Fatal("the session did not end")
		return nil
	}
}

const statusCase = `status) [ -f "$HOME/signed" ] || exit 1; echo '{"email": "you@example.org"}' ;;`

const deviceLogin = `{"methods":[{"id":"m","label":"M","flow":"device","args":["login"],"status":{"args":["status"]},"logout":{"args":["logout"]}}]}`

func TestSessionDeviceOverPipes(t *testing.T) {
	script := "#!/bin/sh\ncase \"$1\" in\n" + statusCase + `
login)
  printf '\033[1mWelcome to Fake [v1.2.3]\033[0m\n'
  printf '1. Open this link\n   https://auth.example.com/codex/device\n\n2. Enter this one-time code\n   CJUT-YI8TS\n'
  sleep 0.5; touch "$HOME/signed"; sleep 30 ;;
esac
`
	login := `{"methods":[{"id":"m","label":"M","flow":"device","args":["login"],
	  "status":{"args":["status"],"account":"\"email\": \"([^\"]+)\""},"logout":{"args":["logout"]}}]}`
	out, errc, _ := startSession(t, newFixture(t, login, script))
	if err := wait(t, errc); err != nil {
		t.Fatalf("Run = %v\n%s", err, out)
	}
	log := out.String()
	for _, want := range []string{
		"berth: login start device\n",
		"│ Welcome to Fake [v1.2.3]\n",
		"berth: login url https://auth.example.com/codex/device\n",
		"berth: login device-code CJUT-YI8TS\n",
		"berth: login signed in as you@example.org\n",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, "\x1b") {
		t.Errorf("escape codes in the log:\n%q", log)
	}
}

func TestSessionPasteOverPipes(t *testing.T) {
	script := "#!/bin/sh\ncase \"$1\" in\n" + statusCase + `
login)
  printf "If the browser didn't open, visit: \033]8;;https://login.example.com/authorize?a=1\007https://login.example.com/authorize?a=1\033]8;;\007\n"
  printf 'Paste code here if prompted > '
  read code
  [ "$code" = "good-code#1" ] || { echo "Login failed: Request failed with status code 400"; exit 1; }
  touch "$HOME/signed"; echo "Login successful." ;;
esac
`
	login := `{"methods":[{"id":"m","label":"M","flow":"paste","prompt":"Paste code here","args":["login"],
	  "status":{"args":["status"]},"logout":{"args":["logout"]}}]}`
	f := newFixture(t, login, script)

	pr, pw := io.Pipe()
	out, errc, _ := start(t, f, "m", pr)
	waitFor(t, out, "berth: login waiting for code\n", 1)
	if !strings.Contains(out.String(), "berth: login url https://login.example.com/authorize?a=1\n") {
		t.Errorf("no url marker:\n%s", out)
	}
	io.WriteString(pw, "  good-code#1  \n")
	if err := wait(t, errc); err != nil {
		t.Fatalf("Run = %v\n%s", err, out)
	}
	if log := out.String(); !strings.Contains(log, "berth: login input received\n") || !strings.Contains(log, "berth: login signed in\n") {
		t.Errorf("log:\n%s", log)
	}

	os.Remove(filepath.Join(f.home, "signed"))
	pr, pw = io.Pipe()
	out, errc, _ = start(t, f, "m", pr)
	waitFor(t, out, "berth: login waiting for code\n", 1)
	io.WriteString(pw, "wrong\n")
	if err := wait(t, errc); !errors.Is(err, ErrNotSignedIn) {
		t.Errorf("bad code: Run = %v", err)
	}
	if log := out.String(); !strings.Contains(log, "berth: login failed: ") || !strings.Contains(log, "exited with status 1") {
		t.Errorf("log:\n%s", log)
	}
}

func TestSessionPasteUnderPTYRetriesAndRedacts(t *testing.T) {
	script := "#!/bin/sh\ncase \"$1\" in\n" + statusCase + `
login)
  echo "no-browser=$NO_BROWSER"
  n=0
  while :; do
    n=$((n+1))
    echo "Please visit the following URL to authorize the application:"
    echo "https://accounts.example.com/auth?state=$n"
    printf 'Enter the authorization code: '
    read code
    [ "$code" = "good-code" ] && break
    echo "Failed to authenticate with authorization code:invalid_grant"
  done
  touch "$HOME/signed"
  sleep 30 ;;
esac
`
	login := `{"methods":[{"id":"m","label":"M","flow":"paste","pty":true,"prompt":"Enter the authorization code",
	  "args":["login"],"env":{"NO_BROWSER":"true"},
	  "prepare":{"file":"~/.g/settings.json","set":"security.auth.selectedType","value":"oauth-personal"},
	  "status":{"args":["status"]},"logout":{"args":["logout"]}}]}`
	f := newFixture(t, login, script)
	pr, pw := io.Pipe()
	out, errc, _ := start(t, f, "m", pr)
	waitFor(t, out, "berth: login waiting for code\n", 1)
	io.WriteString(pw, "bad-code\n")
	waitFor(t, out, "berth: login url https://accounts.example.com/auth?state=2\n", 1)
	waitFor(t, out, "berth: login waiting for code\n", 2)
	io.WriteString(pw, "good-code\n")
	if err := wait(t, errc); err != nil {
		t.Fatalf("Run = %v\n%s", err, out)
	}
	log := out.String()
	if strings.Contains(log, "bad-code") || strings.Contains(log, "good-code") {
		t.Errorf("a pasted code reached the log:\n%s", log)
	}
	if strings.Count(log, "berth: login input received\n") != 2 || !strings.Contains(log, "│ no-browser=true\n") {
		t.Errorf("log:\n%s", log)
	}
	raw, _ := os.ReadFile(filepath.Join(f.home, ".g", "settings.json"))
	if !strings.Contains(string(raw), `"selectedType": "oauth-personal"`) {
		t.Errorf("prepare did not run: %s", raw)
	}
}

func TestSessionAnswersAMenu(t *testing.T) {
	script := "#!/bin/sh\ncase \"$1\" in\n" + statusCase + `
login)
  echo "Select GitHub deployment type"
  read choice
  echo "Go to: https://github.example.com/login/device"
  echo "Enter code: 15F6-CA96"
  sleep 0.5; touch "$HOME/signed"; sleep 30 ;;
esac
`
	login := `{"methods":[{"id":"m","label":"M","flow":"device","pty":true,"args":["login"],
	  "answers":[{"expect":"deployment type","send":"\r"}],
	  "status":{"args":["status"]},"logout":{"args":["logout"]}}]}`
	out, errc, _ := startSession(t, newFixture(t, login, script))
	if err := wait(t, errc); err != nil {
		t.Fatalf("Run = %v\n%s", err, out)
	}
	if !strings.Contains(out.String(), "berth: login device-code 15F6-CA96\n") {
		t.Errorf("log:\n%s", out)
	}
}

func TestSessionFailsWithoutURL(t *testing.T) {
	script := "#!/bin/sh\ncase \"$1\" in\n" + statusCase + "\nlogin) echo starting; sleep 30 ;;\nesac\n"
	began := time.Now()
	out, errc, _ := startSession(t, newFixture(t, deviceLogin, script))
	if err := wait(t, errc); !errors.Is(err, ErrNotSignedIn) {
		t.Errorf("Run = %v", err)
	}
	if !strings.Contains(out.String(), "berth: login failed: the sign-in page did not appear") {
		t.Errorf("log:\n%s", out)
	}
	if time.Since(began) > 8*time.Second {
		t.Errorf("took %s", time.Since(began))
	}
}

func TestSessionChildExitsUnsigned(t *testing.T) {
	script := "#!/bin/sh\ncase \"$1\" in\n" + statusCase + "\nlogin) echo https://x.example/device; echo 'code ABCD-1234'; exit 3 ;;\nesac\n"
	out, errc, _ := startSession(t, newFixture(t, deviceLogin, script))
	if err := wait(t, errc); !errors.Is(err, ErrNotSignedIn) {
		t.Errorf("Run = %v", err)
	}
	if !strings.Contains(out.String(), "exited with status 3") {
		t.Errorf("log:\n%s", out)
	}
}

func TestSessionChildKeepsPipeOpen(t *testing.T) {
	script := "#!/bin/sh\ncase \"$1\" in\n" + statusCase + "\nlogin) (sleep 30) & echo https://x.example/device; echo ABCD-1234; touch \"$HOME/signed\"; exit 0 ;;\nesac\n"
	began := time.Now()
	out, errc, _ := startSession(t, newFixture(t, deviceLogin, script))
	if err := wait(t, errc); err != nil {
		t.Errorf("Run = %v\n%s", err, out)
	}
	if time.Since(began) > 8*time.Second {
		t.Errorf("a background child stalled the session for %s", time.Since(began))
	}
}

func TestSessionCancelKillsTheCLI(t *testing.T) {
	script := "#!/bin/sh\ncase \"$1\" in\n" + statusCase + "\nlogin) echo $$ > \"$HOME/pid\"; echo https://x.example/device; sleep 30 ;;\nesac\n"
	f := newFixture(t, deviceLogin, script)
	out, errc, cancel := startSession(t, f)
	waitFor(t, out, "berth: login url", 1)
	cancel()
	if err := wait(t, errc); !errors.Is(err, context.Canceled) {
		t.Errorf("Run = %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(f.home, "pid"))
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	deadline := time.Now().Add(3 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("the CLI (pid %d) outlived the cancelled session", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
