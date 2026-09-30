package locksource

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validPkg = `{"dependencies":{"a":"1.2.3","b":"2.0.0-rc.1"}}`
const validLock = `{"lockfileVersion":3,"packages":{"":{"dependencies":{"a":"1.2.3","b":"2.0.0-rc.1"}}}}`

func TestHash(t *testing.T) {
	if got := Hash([]byte("abc")); got != "ba7816bf8f01" {
		t.Fatalf("Hash = %q", got)
	}
}

func TestParseValid(t *testing.T) {
	l, err := Parse([]byte(validPkg), []byte(validLock), Source{Origin: OriginDir})
	if err != nil {
		t.Fatal(err)
	}
	if l.Hash != Hash([]byte(validLock)) || l.Deps["b"] != "2.0.0-rc.1" || l.Source.Origin != OriginDir {
		t.Fatalf("unexpected lock %+v", l)
	}
}

func TestParseRejects(t *testing.T) {
	big := strings.Repeat(" ", MaxFileSize+1)
	cases := map[string][2]string{
		"bad package json":   {`{`, validLock},
		"no dependencies":    {`{"dependencies":{}}`, validLock},
		"range version":      {`{"dependencies":{"a":"^1.2.3"}}`, `{"lockfileVersion":3,"packages":{"":{"dependencies":{"a":"^1.2.3"}}}}`},
		"tag version":        {`{"dependencies":{"a":"latest"}}`, `{"lockfileVersion":3,"packages":{"":{"dependencies":{"a":"latest"}}}}`},
		"lockfile version 2": {validPkg, `{"lockfileVersion":2,"packages":{"":{"dependencies":{"a":"1.2.3","b":"2.0.0-rc.1"}}}}`},
		"no root package":    {validPkg, `{"lockfileVersion":3,"packages":{}}`},
		"root mismatch":      {validPkg, `{"lockfileVersion":3,"packages":{"":{"dependencies":{"a":"1.2.4","b":"2.0.0-rc.1"}}}}`},
		"bad lock json":      {validPkg, `nope`},
		"too large":          {validPkg, big},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(c[0]), []byte(c[1]), Source{})
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestFromDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(validPkg), 0o644)
	os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(validLock), 0o644)
	l, err := FromDir(dir, Source{Origin: OriginImage})
	if err != nil {
		t.Fatal(err)
	}
	if string(l.PackageJSON) != validPkg || l.Source.Origin != OriginImage {
		t.Fatalf("unexpected %+v", l)
	}
	if _, err := FromDir(t.TempDir(), Source{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing files: err = %v", err)
	}
}
