// Package output renders command results for people or as the stable --json
// contract, and defines the exit codes shared by every command.
package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// Schema is the version of the --json contract. Additive fields keep it;
// removals and renames bump it.
const Schema = 1

// Exit codes.
const (
	ExitOK       = 0
	ExitFailure  = 1
	ExitUsage    = 2
	ExitLockBusy = 3
	ExitNotRoot  = 4
)

// Error is a failure with a stable machine-readable code and the pipeline
// step it happened in.
type Error struct {
	Exit    int
	Code    string
	Step    string
	Message string
}

func (e *Error) Error() string {
	if e.Step == "" {
		return e.Message
	}
	return e.Step + ": " + e.Message
}

// Errorf builds an *Error.
func Errorf(exit int, code, step, format string, args ...any) *Error {
	return &Error{Exit: exit, Code: code, Step: step, Message: fmt.Sprintf(format, args...)}
}

// Printer writes results to Stdout and logs to Stderr.
type Printer struct {
	JSON   bool
	Stdout io.Writer
	Stderr io.Writer
}

// Logf writes one log line to stderr.
func (p Printer) Logf(format string, args ...any) {
	fmt.Fprintf(p.Stderr, "berth: "+format+"\n", args...)
}

// Result prints v as one JSON object with "schema" added, or calls human.
func (p Printer) Result(v any, human func(w io.Writer)) error {
	if !p.JSON {
		human(p.Stdout)
		return nil
	}
	return p.writeJSON(v)
}

// Fail reports err and returns the exit code the process should use.
func (p Printer) Fail(err error) int {
	var e *Error
	if !errors.As(err, &e) {
		e = &Error{Exit: ExitFailure, Code: "internal", Message: err.Error()}
	}
	if p.JSON {
		_ = p.writeJSON(map[string]any{"error": map[string]string{
			"code": e.Code, "step": e.Step, "message": e.Message,
		}})
	} else {
		fmt.Fprintf(p.Stderr, "berth: error: %s\n", e.Error())
	}
	return e.Exit
}

func (p Printer) writeJSON(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return fmt.Errorf("json result must be an object: %w", err)
	}
	fields["schema"] = json.RawMessage(strconv.Itoa(Schema))
	enc := json.NewEncoder(p.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(fields)
}
