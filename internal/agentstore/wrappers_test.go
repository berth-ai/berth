package agentstore

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writePkg(t *testing.T, dir, name, pkgJSON string) {
	t.Helper()
	p := filepath.Join(dir, "node_modules", name)
	os.MkdirAll(p, 0o755)
	os.WriteFile(filepath.Join(p, "package.json"), []byte(pkgJSON), 0o644)
}

func TestWrappersResolveTheirOwnVersion(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "versions", hashA)
	writePkg(t, dir, "@scope/tool", `{"name":"@scope/tool","bin":"run.sh"}`)
	os.WriteFile(filepath.Join(dir, "node_modules/@scope/tool/run.sh"),
		[]byte("#!/bin/sh\necho \"$PLAYWRIGHT_BROWSERS_PATH|$1\"\n"), 0o755)
	writePkg(t, dir, "multi", `{"name":"multi","bin":{"m-one":"a.sh","m-two":"b.sh"}}`)

	bins, err := WriteWrappers(dir, []string{"multi", "@scope/tool"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(bins, []string{"m-one", "m-two", "tool"}) {
		t.Fatalf("bins = %v", bins)
	}
	os.Symlink(filepath.Join("versions", hashA), filepath.Join(root, "current"))
	out, err := exec.Command(filepath.Join(root, "current", "bin", "tool"), "hi").Output()
	if err != nil {
		t.Fatal(err)
	}
	realDir, _ := filepath.EvalSymlinks(dir)
	want := filepath.Join(realDir, "ms-playwright") + "|hi\n"
	if string(out) != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

func TestWrappersRejectClashesAndBadNames(t *testing.T) {
	dir := t.TempDir()
	writePkg(t, dir, "a", `{"name":"a","bin":{"same":"x.js"}}`)
	writePkg(t, dir, "b", `{"name":"b","bin":{"same":"y.js"}}`)
	if _, err := WriteWrappers(dir, []string{"a", "b"}); err == nil || !strings.Contains(err.Error(), "same") {
		t.Fatalf("clash: %v", err)
	}
	dir2 := t.TempDir()
	writePkg(t, dir2, "c", `{"name":"c","bin":{"../evil":"x.js"}}`)
	if _, err := WriteWrappers(dir2, []string{"c"}); err == nil {
		t.Fatal("bad bin name accepted")
	}
	dir3 := t.TempDir()
	writePkg(t, dir3, "d", `{"name":"d","bin":{"d":"../../x.js"}}`)
	if _, err := WriteWrappers(dir3, []string{"d"}); err == nil {
		t.Fatal("escaping target accepted")
	}
}

func TestSealRemovesGroupAndOtherWrite(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f")
	os.WriteFile(f, nil, 0o666)
	os.Chmod(f, 0o666)
	os.Chmod(dir, 0o777)
	if err := Seal(dir, false); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]os.FileMode{f: 0o644, dir: 0o755} {
		info, _ := os.Stat(p)
		if info.Mode().Perm() != want {
			t.Fatalf("%s mode %v", p, info.Mode().Perm())
		}
	}
}
