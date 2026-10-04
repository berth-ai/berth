package agentlogin

import (
	"slices"
	"testing"
)

func TestStripTerminal(t *testing.T) {
	for in, want := range map[string]string{
		"\x1b[1mBold\x1b[0m text": "Bold text",
		"\x1b]8;;https://a.example/x\x1b\\https://a.example/x\x1b]8;;\x1b\\": "https://a.example/x",
		"\x1b]0;title\x07after":             "after",
		"\x1b[?25l\x1b[2K\x1b[1G◒  Waiting": "◒  Waiting",
		"tab\there\x00\x7f":                 "tab\there",
		"\x1b(Bplain\x1b=":                  "plain",
		"cut at the end \x1b[3":             "cut at the end ",
	} {
		if got := stripTerminal(in); got != want {
			t.Errorf("stripTerminal(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanerSplitsLinesAndRedraws(t *testing.T) {
	var c cleaner
	var got []string
	got = append(got, c.write([]byte("first\r\nsec"))...)
	got = append(got, c.write([]byte("ond\n\n\rspin 1\rspin 2\r\n"))...)
	want := []string{"first", "second", "spin 1", "spin 2"}
	if !slices.Equal(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
	if rest := c.write([]byte("Paste code here > ")); len(rest) != 0 {
		t.Errorf("a partial line came out early: %q", rest)
	}
	if got := c.flush(); !slices.Equal(got, []string{"Paste code here >"}) {
		t.Errorf("flush = %q", got)
	}
	if got := c.flush(); len(got) != 0 {
		t.Errorf("second flush = %q", got)
	}
}

func TestCleanerRedacts(t *testing.T) {
	var c cleaner
	c.redact("4/0AbCdE-code")
	c.redact("") // ignored
	got := c.write([]byte("Enter the authorization code: 4/0AbCdE-code\r\nagain 4/0AbCdE-code end\n"))
	want := []string{"Enter the authorization code: •••", "again ••• end"}
	if !slices.Equal(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

func TestCleanerFreshDropsRepeatsAndSpinners(t *testing.T) {
	var c cleaner
	var logged []string
	for _, l := range []string{"◒  Waiting for authorization", "◐  Waiting for authorization.",
		"◓  Waiting for authorization..", "Go to: https://x.example", "◑  Waiting for authorization...",
		"Go to: https://x.example", "Done"} {
		if c.fresh(l) {
			logged = append(logged, l)
		}
	}
	want := []string{"◒  Waiting for authorization", "Go to: https://x.example", "Done"}
	if !slices.Equal(logged, want) {
		t.Errorf("logged = %q, want %q", logged, want)
	}
}
