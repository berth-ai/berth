package upstream

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/berth-ai/berth/internal/runner"
)

func TestNPMLatestAndVersions(t *testing.T) {
	var accepts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accepts = append(accepts, r.Header.Get("Accept"))
		switch r.URL.EscapedPath() {
		case "/@scope%2Fpkg/latest":
			fmt.Fprint(w, `{"version":"1.10.0"}`)
		case "/@scope%2Fpkg":
			fmt.Fprint(w, `{"versions":{"1.10.0":{},"1.2.0":{},"1.9.0-beta.1":{},"bad version":{}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	n := NPM{Registry: srv.URL + "/"}
	v, err := n.Latest(context.Background(), "@scope/pkg")
	if err != nil || v != "1.10.0" {
		t.Fatalf("Latest = %q, %v", v, err)
	}
	vs, err := n.Versions(context.Background(), "@scope/pkg")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"1.2.0", "1.9.0-beta.1", "1.10.0"}; !slices.Equal(vs, want) {
		t.Errorf("Versions = %v, want %v", vs, want)
	}
	if accepts[1] != "application/vnd.npm.install-v1+json" {
		t.Errorf("Versions asked for %q", accepts[1])
	}
	if _, err := n.Latest(context.Background(), "missing"); !errors.Is(err, ErrLookup) {
		t.Errorf("missing package: %v", err)
	}
}

func TestValidPackage(t *testing.T) {
	for _, p := range []string{"opencode-ai", "@anthropic-ai/claude-code", "@upstash/context7-mcp", "a.b_c"} {
		if !validPackage(p) {
			t.Errorf("validPackage(%q) = false", p)
		}
	}
	for _, p := range []string{"", "@scope", "@/x", "../x", "a/b", "UPPER", "a b", ".hidden", "@s/../x"} {
		if validPackage(p) {
			t.Errorf("validPackage(%q) = true", p)
		}
	}
}

type scripted struct {
	out  map[string]string
	cmds []runner.Cmd
}

func (s *scripted) Run(_ context.Context, c runner.Cmd) error {
	s.cmds = append(s.cmds, c)
	key := c.Args[0] + " " + c.Args[1]
	out, ok := s.out[key]
	if !ok {
		return errors.New("exit status 1")
	}
	_, _ = c.Stdout.Write([]byte(out))
	return nil
}

func TestMise(t *testing.T) {
	r := &scripted{out: map[string]string{
		"latest gh":    "2.104.0\n",
		"ls-remote gh": "2.9.0\n2.10.0\n2.100.0\n",
		"latest bad":   "not a version!\n",
	}}
	m := Mise{Path: "mise", Runner: r, Env: []string{"MISE_YES=1"}}
	v, err := m.Latest(context.Background(), "gh")
	if err != nil || v != "2.104.0" {
		t.Fatalf("Latest = %q, %v", v, err)
	}
	if !r.cmds[0].AsAgent {
		t.Error("mise ran as root")
	}
	vs, err := m.Versions(context.Background(), "gh")
	if err != nil || !slices.Equal(vs, []string{"2.9.0", "2.10.0", "2.100.0"}) {
		t.Fatalf("Versions = %v, %v", vs, err)
	}
	if _, err := m.Latest(context.Background(), "bad"); !errors.Is(err, ErrLookup) {
		t.Errorf("bad output: %v", err)
	}
	if _, err := m.Latest(context.Background(), "missing"); !errors.Is(err, ErrLookup) {
		t.Errorf("failed run: %v", err)
	}
}
