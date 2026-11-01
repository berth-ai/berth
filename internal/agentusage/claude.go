package agentusage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// maxLine bounds one transcript line; a longer one (a huge tool result) is
// skipped, never buffered.
const maxLine = 8 << 20

// readClaudeCode reads Claude Code's transcripts: dir is ~/.claude/projects,
// holding one folder per project of <session>.jsonl files, subagents' files
// below. Each assistant line carries message.usage. A streamed response is
// written as several lines with one message id and request id, and a
// resumed session repeats earlier lines, so responses are counted once,
// keeping the largest output count seen.
func readClaudeCode(dir string, since time.Time, add func(record)) error {
	seen := map[string]record{}
	var order []string
	var errs []error
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == dir {
				return err
			}
			errs = append(errs, err)
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		if fi, err := d.Info(); err != nil || fi.ModTime().Before(since) {
			return nil // a file last written before since holds nothing newer
		}
		if err := claudeFile(path, func(key string, r record) {
			prev, ok := seen[key]
			if !ok {
				order = append(order, key)
			}
			if !ok || r.tokens.Output > prev.tokens.Output {
				seen[key] = r
			}
		}); err != nil {
			errs = append(errs, err)
		}
		return nil
	})
	for _, k := range order {
		add(seen[k])
	}
	if walkErr != nil {
		return walkErr
	}
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	}
	return fmt.Errorf("%w (and %d more files)", errs[0], len(errs)-1)
}

type claudeLine struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	SessionID string    `json:"sessionId"`
	RequestID string    `json:"requestId"`
	Message   struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			Input      int64 `json:"input_tokens"`
			Output     int64 `json:"output_tokens"`
			CacheRead  int64 `json:"cache_read_input_tokens"`
			CacheWrite int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

func claudeFile(path string, add func(key string, r record)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	session := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	return eachLine(f, func(line []byte) {
		// Most lines are prompts and tool output: skip them unparsed.
		if !bytes.Contains(line, []byte(`"usage"`)) || !bytes.Contains(line, []byte(`"assistant"`)) {
			return
		}
		var l claudeLine
		if json.Unmarshal(line, &l) != nil || l.Type != "assistant" || l.Message.Usage == nil || l.Timestamp.IsZero() {
			return
		}
		// Claude Code writes its own notices as <synthetic> responses.
		if l.Message.Model == "" || strings.HasPrefix(l.Message.Model, "<") {
			return
		}
		u := l.Message.Usage
		r := record{
			at:      l.Timestamp,
			session: l.SessionID,
			model:   l.Message.Model,
			tokens:  Tokens{Input: u.Input, Output: u.Output, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite},
		}
		if r.session == "" {
			r.session = session
		}
		key := l.Message.ID + "\x00" + l.RequestID
		if l.Message.ID == "" {
			key = path + "\x00" + l.Timestamp.String()
		}
		add(key, r)
	})
}

// eachLine calls fn for each line of r up to maxLine bytes; longer lines
// are skipped.
func eachLine(r io.Reader, fn func([]byte)) error {
	br := bufio.NewReaderSize(r, 64<<10)
	var buf []byte
	skipping := false
	for {
		chunk, err := br.ReadSlice('\n')
		switch {
		case skipping:
		case len(buf)+len(chunk) > maxLine:
			skipping, buf = true, buf[:0]
		default:
			buf = append(buf, chunk...)
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if !skipping && len(buf) > 0 {
			fn(bytes.TrimRight(buf, "\r\n"))
		}
		buf, skipping = buf[:0], false
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
