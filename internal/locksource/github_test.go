package locksource

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const sha = "0123456789abcdef0123456789abcdef01234567"

func fakeGitHub(t *testing.T, files map[string]string) (*httptest.Server, *http.Header) {
	t.Helper()
	var seen http.Header
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/example/berth/commits/{ref...}", func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		if r.PathValue("ref") != "main" && r.PathValue("ref") != "feature/x" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Accept") != "application/vnd.github.sha" {
			http.Error(w, "bad accept", http.StatusBadRequest)
			return
		}
		w.Write([]byte(sha))
	})
	mux.HandleFunc("GET /example/berth/"+sha+"/agents/{file}", func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[r.PathValue("file")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &seen
}

func gh(srv *httptest.Server) GitHub {
	return GitHub{Repo: "example/berth", Token: "t0ken", APIBase: srv.URL, RawBase: srv.URL}
}

func TestFetch(t *testing.T) {
	srv, seen := fakeGitHub(t, map[string]string{"package.json": validPkg, "package-lock.json": validLock})
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.FixedZone("x", 3600))
	l, err := gh(srv).Fetch(context.Background(), "main", now)
	if err != nil {
		t.Fatal(err)
	}
	want := Source{Origin: OriginRef, Repo: "example/berth", Ref: "main", Commit: sha, FetchedAt: now.UTC()}
	if l.Source != want || l.Hash != Hash([]byte(validLock)) {
		t.Fatalf("got %+v", l.Source)
	}
	if (*seen).Get("Authorization") != "Bearer t0ken" {
		t.Fatalf("token not sent")
	}
}

func TestFetchSlashRef(t *testing.T) {
	srv, _ := fakeGitHub(t, map[string]string{"package.json": validPkg, "package-lock.json": validLock})
	if _, err := gh(srv).Fetch(context.Background(), "feature/x", time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestFetchErrors(t *testing.T) {
	srv, _ := fakeGitHub(t, map[string]string{"package.json": validPkg})
	g := gh(srv)
	ctx := context.Background()
	if _, err := g.Fetch(ctx, "nope", time.Now()); !errors.Is(err, ErrFetch) {
		t.Fatalf("unknown ref: %v", err)
	}
	if _, err := g.Fetch(ctx, "main", time.Now()); !errors.Is(err, ErrFetch) {
		t.Fatalf("missing lock file: %v", err)
	}
	if _, err := g.Fetch(ctx, "../etc", time.Now()); !errors.Is(err, ErrFetch) {
		t.Fatalf("traversal ref: %v", err)
	}
	g.Repo = "not a repo"
	if _, err := g.Fetch(ctx, "main", time.Now()); !errors.Is(err, ErrFetch) {
		t.Fatalf("bad repo: %v", err)
	}
}

func TestFetchInvalidLock(t *testing.T) {
	srv, _ := fakeGitHub(t, map[string]string{"package.json": `{"dependencies":{"a":"^1"}}`, "package-lock.json": validLock})
	if _, err := gh(srv).Fetch(context.Background(), "main", time.Now()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
}
