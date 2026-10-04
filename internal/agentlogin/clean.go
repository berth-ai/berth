// Package agentlogin signs agent CLIs in to their vendor accounts, as the
// agent user: it runs the sign-in a lock descriptor names, turns its
// terminal output into a readable log with marker lines the broker parses,
// probes whether a method is signed in, and signs it out.
package agentlogin

import (
	"bytes"
	"regexp"
	"strings"
)

var (
	oscSeq   = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)
	csiSeq   = regexp.MustCompile(`\x1b\[[0-9;?<>=]*[ -/]*[@-~]`)
	escSeq   = regexp.MustCompile(`\x1b[()][A-Za-z0-9]|\x1b[=>78DEHMc]`)
	cutSeq   = regexp.MustCompile(`\x1b[^A-Za-z\x07]*$`)
	ctrlRune = regexp.MustCompile(`[\x00-\x08\x0b-\x1f\x7f]`)
)

// spinners are the frames CLIs redraw in place; fresh ignores them.
const spinners = "◐◓◑◒⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏|/-\\ "

// recentLines is how many logged lines fresh remembers.
const recentLines = 32

// stripTerminal removes escape sequences and control characters other than
// tab. An OSC 8 hyperlink keeps its visible text.
func stripTerminal(s string) string {
	s = oscSeq.ReplaceAllString(s, "")
	s = csiSeq.ReplaceAllString(s, "")
	s = escSeq.ReplaceAllString(s, "")
	s = cutSeq.ReplaceAllString(s, "")
	return ctrlRune.ReplaceAllString(s, "")
}

// cleaner turns raw terminal output into lines: escape sequences removed,
// carriage-return redraws split into lines, blank lines dropped, and every
// redacted string replaced with •••. Callers serialize access.
type cleaner struct {
	pending []byte
	secrets []string
	recent  []string
}

// redact hides s in every line cleaned from now on.
func (c *cleaner) redact(s string) {
	if s != "" {
		c.secrets = append(c.secrets, s)
	}
}

// write adds output and returns the lines it completed.
func (c *cleaner) write(p []byte) []string {
	c.pending = append(c.pending, p...)
	var out []string
	for {
		i := bytes.IndexAny(c.pending, "\r\n")
		if i < 0 {
			return out
		}
		out = c.add(out, c.pending[:i])
		c.pending = c.pending[i+1:]
	}
}

// flush returns the pending partial line, such as a prompt, and forgets it.
func (c *cleaner) flush() []string {
	line := c.pending
	c.pending = nil
	return c.add(nil, line)
}

func (c *cleaner) add(out []string, raw []byte) []string {
	line := strings.TrimRight(stripTerminal(string(raw)), " \t")
	for _, s := range c.secrets {
		line = strings.ReplaceAll(line, s, "•••")
	}
	if strings.TrimSpace(line) == "" {
		return out
	}
	return append(out, line)
}

// fresh reports whether line differs from the recently logged ones, ignoring
// spinner frames and trailing dots, and remembers it when it does.
func (c *cleaner) fresh(line string) bool {
	key := strings.TrimRight(strings.TrimLeft(line, spinners), ". ")
	for _, r := range c.recent {
		if r == key {
			return false
		}
	}
	c.recent = append(c.recent, key)
	if len(c.recent) > recentLines {
		c.recent = c.recent[1:]
	}
	return true
}
