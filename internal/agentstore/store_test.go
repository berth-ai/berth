package agentstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/berth-ai/berth/internal/testutil"
)

var t0 = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

const (
	hashA = "aaaaaaaaaaaa"
	hashB = "bbbbbbbbbbbb"
	hashC = "cccccccccccc"
)

func newStore(t *testing.T) Store { return Store{Root: t.TempDir()} }

func TestCurrentAbsent(t *testing.T) {
	s := newStore(t)
	if v, err := s.Current(); v != nil || err != nil {
		t.Fatalf("got %v, %v", v, err)
	}
}

func TestIncompleteVersionIsNotFound(t *testing.T) {
	s := newStore(t)
	os.MkdirAll(s.VersionDir(hashA), 0o755)
	if _, err := s.Get(hashA); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	testutil.Link(t, s.Root, "current", hashA)
	if v, _ := s.Current(); v != nil {
		t.Fatal("incomplete current must read as absent")
	}
}

func TestGetRejectsBadHashes(t *testing.T) {
	s := newStore(t)
	for _, h := range []string{"../x", "abc", "AAAAAAAAAAAA", ""} {
		if _, err := s.Get(h); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%q: err = %v", h, err)
		}
	}
}

func TestFlipKeepsPrevious(t *testing.T) {
	s := newStore(t)
	testutil.MakeVersion(t, s.Root, hashA, "image", t0)
	testutil.MakeVersion(t, s.Root, hashB, "ref", t0.Add(time.Hour))
	if changed, err := s.Flip(hashA); err != nil || !changed {
		t.Fatalf("first flip: %v %v", changed, err)
	}
	if p, _ := s.Previous(); p != nil {
		t.Fatal("no previous expected after first flip")
	}
	s.Flip(hashB)
	cur, _ := s.Current()
	prev, _ := s.Previous()
	if cur.Hash != hashB || prev.Hash != hashA {
		t.Fatalf("current %s previous %s", cur.Hash, prev.Hash)
	}
	if changed, _ := s.Flip(hashB); changed {
		t.Fatal("flip to current must be a no-op")
	}
	target, _ := os.Readlink(filepath.Join(s.Root, "current"))
	if target != filepath.Join("versions", hashB) {
		t.Fatalf("link must be relative, got %q", target)
	}
	if _, err := s.Flip(hashC); !errors.Is(err, ErrNotFound) {
		t.Fatalf("flip to missing: %v", err)
	}
}

func TestRollback(t *testing.T) {
	s := newStore(t)
	if _, err := s.Rollback(""); !errors.Is(err, ErrNoPrevious) {
		t.Fatalf("empty store: %v", err)
	}
	testutil.MakeVersion(t, s.Root, hashA, "image", t0)
	testutil.MakeVersion(t, s.Root, hashB, "image", t0)
	testutil.MakeVersion(t, s.Root, hashC, "image", t0)
	s.Flip(hashA)
	s.Flip(hashB)
	v, err := s.Rollback("")
	if err != nil || v.Hash != hashA {
		t.Fatalf("rollback: %v %v", v, err)
	}
	if p, _ := s.Previous(); p.Hash != hashB {
		t.Fatalf("previous = %s", p.Hash)
	}
	if v, _ := s.Rollback(hashC); v.Hash != hashC {
		t.Fatalf("named rollback: %s", v.Hash)
	}
	if _, err := s.Rollback("../../etc"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bad target: %v", err)
	}
}

func TestListNewestFirst(t *testing.T) {
	s := newStore(t)
	testutil.MakeVersion(t, s.Root, hashA, "image", t0)
	testutil.MakeVersion(t, s.Root, hashB, "image", t0.Add(2*time.Hour))
	testutil.MakeVersion(t, s.Root, hashC, "image", t0.Add(time.Hour))
	os.MkdirAll(s.VersionDir("dddddddddddd")+".tmp", 0o755)
	vs, err := s.List()
	if err != nil || len(vs) != 3 || vs[0].Hash != hashB || vs[2].Hash != hashA {
		t.Fatalf("list = %v, %v", vs, err)
	}
}

func TestStateDefaultsFromOrigin(t *testing.T) {
	s := newStore(t)
	if st, _ := s.ReadState(); !st.FollowImage {
		t.Fatal("empty store follows the image")
	}
	testutil.MakeVersion(t, s.Root, hashA, "ref", t0)
	s.Flip(hashA)
	if st, _ := s.ReadState(); st.FollowImage {
		t.Fatal("ref origin without state file is pinned")
	}
	if err := s.WriteState(State{FollowImage: true}); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.ReadState(); !st.FollowImage {
		t.Fatal("state file wins")
	}
}

func TestLockBusy(t *testing.T) {
	s := newStore(t)
	unlock, err := s.Lock(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Lock(200 * time.Millisecond); !errors.Is(err, ErrLockBusy) {
		t.Fatalf("err = %v", err)
	}
	unlock()
	again, err := s.Lock(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	again()
}

func TestUnreadableMetadataIsIncomplete(t *testing.T) {
	for _, file := range []string{".complete", "source.json"} {
		t.Run(file, func(t *testing.T) {
			root := t.TempDir()
			dir := testutil.MakeVersion(t, root, "a00000000000", "image", time.Now())
			testutil.Link(t, root, "current", "a00000000000")
			if err := os.WriteFile(filepath.Join(dir, file), nil, 0o644); err != nil {
				t.Fatal(err)
			}
			s := Store{Root: root}
			if _, err := s.Get("a00000000000"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("Get: %v", err)
			}
			if v, err := s.Current(); v != nil || err != nil {
				t.Fatalf("Current: %v %v", v, err)
			}
			if vs, err := s.List(); err != nil || len(vs) != 0 {
				t.Fatalf("List: %v %v", vs, err)
			}
		})
	}
}
