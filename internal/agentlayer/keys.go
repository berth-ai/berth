// Package agentlayer builds /var/lib/berth/bin, the layer every shell runs
// agents through. It holds one exec wrapper per binary of each enabled agent
// package. A wrapper loads only its own package's API keys from agents.env,
// then execs the binary in /opt/agents/current, so turning a package off
// never touches the sealed version directories.
package agentlayer

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/berth-ai/berth/internal/config"
)

var (
	keyName  = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	keyValue = regexp.MustCompile(`^[\x21-\x7e]+$`)
)

// ValidKeyValue checks an API key before it is stored. Printable ASCII
// without spaces covers every provider's keys and keeps agents.env
// line-based.
func ValidKeyValue(v string) error {
	if len(v) > 4096 || !keyValue.MatchString(v) {
		return errors.New("must be 1 to 4096 printable characters without spaces")
	}
	return nil
}

// ReadKeys reads agents.env. A missing file is empty; malformed lines are
// skipped.
func ReadKeys(path string) (map[string]string, error) {
	keys := map[string]string{}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return keys, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 8192), 8192)
	for sc.Scan() {
		name, value, ok := strings.Cut(sc.Text(), "=")
		if ok && keyName.MatchString(name) && ValidKeyValue(value) == nil {
			keys[name] = value
		}
	}
	return keys, sc.Err()
}

// WriteKeys replaces agents.env, owned by uid:gid (-1 keeps it), mode 0640.
func WriteKeys(path string, keys map[string]string, uid, gid int) error {
	var buf bytes.Buffer
	for _, name := range slices.Sorted(maps.Keys(keys)) {
		if !keyName.MatchString(name) || ValidKeyValue(keys[name]) != nil {
			return fmt.Errorf("invalid key %s", name)
		}
		fmt.Fprintf(&buf, "%s=%s\n", name, keys[name])
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o711); err != nil {
		return err
	}
	return config.WriteFileAtomic(path, buf.Bytes(), 0o640, uid, gid)
}

// FixOwner gives an existing agents.env to root:gid, after the agent group
// was remapped at boot.
func FixOwner(path string, gid int) error {
	if err := os.Chown(path, 0, gid); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
