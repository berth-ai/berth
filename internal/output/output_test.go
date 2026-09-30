package output

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestResultJSONAddsSchema(t *testing.T) {
	var out bytes.Buffer
	p := Printer{JSON: true, Stdout: &out, Stderr: io.Discard}
	if err := p.Result(struct {
		Hash string `json:"hash"`
	}{"abc"}, nil); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"hash\": \"abc\",\n  \"schema\": 1\n}\n"
	if out.String() != want {
		t.Fatalf("got %q, want %q", out.String(), want)
	}
}

func TestResultHuman(t *testing.T) {
	var out bytes.Buffer
	p := Printer{Stdout: &out, Stderr: io.Discard}
	_ = p.Result(nil, func(w io.Writer) { io.WriteString(w, "hello\n") })
	if out.String() != "hello\n" {
		t.Fatalf("got %q", out.String())
	}
}

func TestFailJSON(t *testing.T) {
	var out bytes.Buffer
	p := Printer{JSON: true, Stdout: &out, Stderr: io.Discard}
	code := p.Fail(Errorf(ExitLockBusy, "lock_busy", "lock", "busy for %ds", 30))
	if code != ExitLockBusy {
		t.Fatalf("exit %d", code)
	}
	want := "{\n  \"error\": {\n    \"code\": \"lock_busy\",\n    \"message\": \"busy for 30s\",\n    \"step\": \"lock\"\n  },\n  \"schema\": 1\n}\n"
	if out.String() != want {
		t.Fatalf("got %q", out.String())
	}
}

func TestFailPlainErrorIsInternal(t *testing.T) {
	var errOut bytes.Buffer
	p := Printer{Stdout: io.Discard, Stderr: &errOut}
	if code := p.Fail(errors.New("boom")); code != ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(errOut.String(), "boom") {
		t.Fatalf("stderr %q", errOut.String())
	}
}

func TestLogfGoesToStderr(t *testing.T) {
	var out, errOut bytes.Buffer
	Printer{Stdout: &out, Stderr: &errOut}.Logf("step %d", 3)
	if out.Len() != 0 || errOut.String() != "berth: step 3\n" {
		t.Fatalf("stdout %q stderr %q", out.String(), errOut.String())
	}
}
