package skills

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type entry struct {
	name, body, link string
	dir              bool
	mode             int64
}

func tarOf(t *testing.T, entries ...entry) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	tw.WriteHeader(&tar.Header{Typeflag: tar.TypeXGlobalHeader, Name: "pax_global_header", PAXRecords: map[string]string{"comment": "x"}})
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0o644}
		switch {
		case e.dir:
			h.Typeflag, h.Mode = tar.TypeDir, 0o755
		case e.link != "":
			h.Typeflag, h.Linkname = tar.TypeSymlink, e.link
		default:
			h.Typeflag, h.Size = tar.TypeReg, int64(len(e.body))
			if e.mode != 0 {
				h.Mode = e.mode
			}
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(e.body))
	}
	tw.Close()
	return &buf
}

func TestExtract(t *testing.T) {
	dir := t.TempDir()
	var logged []string
	logf := func(f string, a ...any) { logged = append(logged, f) }
	err := extract(tarOf(t,
		entry{name: "scripts/", dir: true},
		entry{name: "SKILL.md", body: pdfSkill},
		entry{name: "scripts/run.sh", body: "#!/bin/sh\n", mode: 0o755},
		entry{name: "ok-link", link: "scripts/run.sh"},
		entry{name: "a", link: "."},
		entry{name: "escape", link: "a/.."},
		entry{name: "abs", link: "/etc/passwd"},
		entry{name: "up", link: "../x"},
	), dir, logf)
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "scripts", "run.sh")); err != nil || fi.Mode().Perm() != 0o755 {
		t.Errorf("run.sh = %v, %v", fi, err)
	}
	if l, _ := os.Readlink(filepath.Join(dir, "ok-link")); l != "scripts/run.sh" {
		t.Errorf("ok-link = %q", l)
	}
	for _, gone := range []string{"escape", "abs", "up"} {
		if _, err := os.Lstat(filepath.Join(dir, gone)); err == nil {
			t.Errorf("%s was extracted", gone)
		}
	}
	if len(logged) != 3 {
		t.Errorf("logged %d drops: %v", len(logged), logged)
	}
}

func TestExtractLimits(t *testing.T) {
	big := strings.Repeat("x", 1<<20)
	var many []entry
	for i := range 51 {
		many = append(many, entry{name: "f" + string(rune('a'+i%26)) + string(rune('a'+i/26)), body: big})
	}
	if err := extract(tarOf(t, many...), t.TempDir(), func(string, ...any) {}); err == nil || !strings.Contains(err.Error(), "MiB") {
		t.Errorf("size limit: %v", err)
	}
	if err := extract(tarOf(t, entry{name: "../x", body: "x"}), t.TempDir(), func(string, ...any) {}); err == nil {
		t.Error("an entry outside the skill was extracted")
	}
}
