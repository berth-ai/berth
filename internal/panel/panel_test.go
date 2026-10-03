package panel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/salgnt/berth/internal/broker"
	"github.com/salgnt/berth/internal/config"
	"github.com/salgnt/berth/internal/supervisor"
)

func TestParseCredential(t *testing.T) {
	for cred, want := range map[string][2]string{
		"me:pw":    {"me", "pw"},
		"me:pa:ss": {"me", "pa:ss"},
	} {
		u, p, err := ParseCredential(cred)
		if err != nil || u != want[0] || p != want[1] {
			t.Errorf("ParseCredential(%q) = %q %q %v", cred, u, p, err)
		}
	}
	for _, cred := range []string{"", "mepw", "me:", ":pw"} {
		if _, _, err := ParseCredential(cred); err == nil {
			t.Errorf("ParseCredential(%q) accepted", cred)
		}
	}
}

// fakeBroker records what the panel forwards.
type fakeBroker struct {
	mu       sync.Mutex
	requests []string
	bodies   []string
}

func (f *fakeBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())
	f.bodies = append(f.bodies, string(body))
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/status":
		w.WriteHeader(http.StatusBadGateway)
		io.WriteString(w, `{"error":{"code":"fetch_failed"}}`)
	case "/jobs":
		io.WriteString(w, `{"jobs":[]}`)
	default:
		if strings.HasPrefix(r.URL.Path, "/verbs/") {
			w.WriteHeader(http.StatusAccepted)
			io.WriteString(w, `{"job":{"id":1}}`)
			return
		}
		io.WriteString(w, `{"job":null}`)
	}
}

func (f *fakeBroker) last() (string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		return "", ""
	}
	return f.requests[len(f.requests)-1], f.bodies[len(f.bodies)-1]
}

func newTestPanel(t *testing.T) (*httptest.Server, *fakeBroker) {
	t.Helper()
	fb := &fakeBroker{}
	bsrv := httptest.NewServer(fb)
	t.Cleanup(bsrv.Close)
	s, err := New("me:pw", bsrv.Client(), bsrv.URL)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return srv, fb
}

type req struct {
	method, path, body string
	header             map[string]string
	noAuth             bool
	user, pass         string
}

type result struct {
	StatusCode int
	Header     http.Header
}

func send(t *testing.T, srv *httptest.Server, r req) (result, string) {
	t.Helper()
	hr, err := http.NewRequest(r.method, srv.URL+r.path, strings.NewReader(r.body))
	if err != nil {
		t.Fatal(err)
	}
	if !r.noAuth {
		u, p := "me", "pw"
		if r.user != "" {
			u, p = r.user, r.pass
		}
		hr.SetBasicAuth(u, p)
	}
	for k, v := range r.header {
		hr.Header.Set(k, v)
	}
	resp, err := srv.Client().Do(hr)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return result{resp.StatusCode, resp.Header}, string(body)
}

func TestAuth(t *testing.T) {
	srv, _ := newTestPanel(t)
	for _, r := range []req{
		{method: "GET", path: "/", noAuth: true},
		{method: "GET", path: "/", user: "me", pass: "wrong"},
		{method: "GET", path: "/", user: "you", pass: "pw"},
		{method: "GET", path: "/api/status", noAuth: true},
		{method: "POST", path: "/api/jobs", noAuth: true},
	} {
		resp, _ := send(t, srv, r)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s as %q: %d", r.method, r.path, r.user, resp.StatusCode)
		}
		if !strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), `Basic realm="Berth"`) {
			t.Errorf("WWW-Authenticate = %q", resp.Header.Get("WWW-Authenticate"))
		}
		assertSecurityHeaders(t, resp)
	}
}

func assertSecurityHeaders(t *testing.T, resp result) {
	t.Helper()
	for k, v := range map[string]string{
		"Cache-Control":           "no-store",
		"X-Frame-Options":         "DENY",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
		"Content-Security-Policy": "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'",
	} {
		if got := resp.Header.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestPageAndAssets(t *testing.T) {
	srv, _ := newTestPanel(t)
	resp, body := send(t, srv, req{method: "GET", path: "/"})
	if resp.StatusCode != 200 || !strings.Contains(body, "<title>Berth</title>") ||
		!strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("GET / = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	assertSecurityHeaders(t, resp)
	for path, ct := range map[string]string{"/static/panel.js": "text/javascript", "/static/panel.css": "text/css"} {
		resp, body := send(t, srv, req{method: "GET", path: path})
		if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), ct) || body == "" {
			t.Errorf("GET %s = %d %q", path, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
	}
	for _, path := range []string{"/nope", "/static/", "/static/missing.js", "/api/other"} {
		if resp, _ := send(t, srv, req{method: "GET", path: path}); resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d", path, resp.StatusCode)
		}
	}
}

func TestReadProxy(t *testing.T) {
	srv, fb := newTestPanel(t)
	resp, body := send(t, srv, req{method: "GET", path: "/api/status?check=1&x=y"})
	if resp.StatusCode != http.StatusBadGateway || !strings.Contains(body, "fetch_failed") {
		t.Errorf("status = %d %s", resp.StatusCode, body)
	}
	if got, _ := fb.last(); got != "GET /status" {
		t.Errorf("broker saw %q", got)
	}
	resp, body = send(t, srv, req{method: "GET", path: "/api/jobs"})
	if resp.StatusCode != 200 || body != `{"jobs":[]}` || resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("jobs = %d %s", resp.StatusCode, body)
	}
	if got, _ := fb.last(); got != "GET /jobs" {
		t.Errorf("broker saw %q", got)
	}
	resp, body = send(t, srv, req{method: "GET", path: "/api/jobs/7"})
	if resp.StatusCode != 200 || body != `{"job":null}` {
		t.Errorf("job = %d %s", resp.StatusCode, body)
	}
	if got, _ := fb.last(); got != "GET /jobs/7" {
		t.Errorf("broker saw %q", got)
	}
	assertSecurityHeaders(t, resp)
}

func TestVerbAllowlist(t *testing.T) {
	srv, fb := newTestPanel(t)
	h := map[string]string{"Content-Type": "application/json", "X-Berth-Panel": "1"}
	for _, verb := range []string{"frobnicate", "..%2fstatus", "auth.verify"} {
		before := len(fb.requests)
		resp, body := send(t, srv, req{method: "POST", path: "/api/verbs/" + verb, body: `{}`, header: h})
		if resp.StatusCode != http.StatusNotFound || !strings.Contains(body, "unknown_verb") || len(fb.requests) != before {
			t.Errorf("%s: %d %s", verb, resp.StatusCode, body)
		}
	}
	for _, path := range []string{"/api/jobs/x", "/api/jobs/1a"} {
		if resp, _ := send(t, srv, req{method: "GET", path: path}); resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d", path, resp.StatusCode)
		}
	}
}

func TestJobPostCSRF(t *testing.T) {
	ok := map[string]string{"Content-Type": "application/json", "X-Berth-Panel": "1"}
	with := func(extra map[string]string, drop ...string) map[string]string {
		h := map[string]string{}
		for k, v := range ok {
			h[k] = v
		}
		for _, k := range drop {
			delete(h, k)
		}
		for k, v := range extra {
			h[k] = v
		}
		return h
	}
	srv, fb := newTestPanel(t)
	host := strings.TrimPrefix(srv.URL, "http://")
	for name, tc := range map[string]struct {
		header map[string]string
		want   int
	}{
		"no marker":           {with(nil, "X-Berth-Panel"), http.StatusForbidden},
		"wrong marker":        {with(map[string]string{"X-Berth-Panel": "yes"}), http.StatusForbidden},
		"form content type":   {with(map[string]string{"Content-Type": "application/x-www-form-urlencoded"}), http.StatusForbidden},
		"text content type":   {with(map[string]string{"Content-Type": "text/plain"}), http.StatusForbidden},
		"cross origin":        {with(map[string]string{"Origin": "http://evil.example"}), http.StatusForbidden},
		"null origin":         {with(map[string]string{"Origin": "null"}), http.StatusForbidden},
		"cross site fetch":    {with(map[string]string{"Sec-Fetch-Site": "cross-site"}), http.StatusForbidden},
		"same site fetch":     {with(map[string]string{"Sec-Fetch-Site": "same-site"}), http.StatusForbidden},
		"plain":               {ok, http.StatusAccepted},
		"same origin":         {with(map[string]string{"Origin": "http://" + host, "Sec-Fetch-Site": "same-origin"}), http.StatusAccepted},
		"https origin":        {with(map[string]string{"Origin": "https://" + host}), http.StatusAccepted},
		"json with charset":   {with(map[string]string{"Content-Type": "application/json; charset=utf-8"}), http.StatusAccepted},
		"user-initiated none": {with(map[string]string{"Sec-Fetch-Site": "none"}), http.StatusAccepted},
	} {
		t.Run(name, func(t *testing.T) {
			before, _ := fb.last()
			fb.mu.Lock()
			n := len(fb.requests)
			fb.mu.Unlock()
			resp, _ := send(t, srv, req{method: "POST", path: "/api/verbs/rollback", body: `{}`, header: tc.header})
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.want)
			}
			fb.mu.Lock()
			forwarded := len(fb.requests) > n
			fb.mu.Unlock()
			if forwarded != (tc.want == http.StatusAccepted) {
				t.Errorf("forwarded = %v (last before: %q)", forwarded, before)
			}
		})
	}
	got, body := fb.last()
	if got != "POST /verbs/rollback" || body != `{}` {
		t.Errorf("broker saw %q %q", got, body)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	srv, _ := newTestPanel(t)
	for _, r := range []req{{method: "GET", path: "/api/verbs/update"}, {method: "POST", path: "/api/status"}, {method: "DELETE", path: "/api/jobs"}} {
		if resp, _ := send(t, srv, r); resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d", r.method, r.path, resp.StatusCode)
		}
	}
}

func TestBrokerDown(t *testing.T) {
	bsrv := httptest.NewServer(http.NotFoundHandler())
	url := bsrv.URL
	bsrv.Close()
	s, err := New("me:pw", http.DefaultClient, url)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s)
	defer srv.Close()
	resp, body := send(t, srv, req{method: "GET", path: "/api/status"})
	var out struct {
		Error struct{ Code string } `json:"error"`
	}
	if resp.StatusCode != http.StatusBadGateway || json.Unmarshal([]byte(body), &out) != nil || out.Error.Code != "broker_unavailable" {
		t.Errorf("status = %d %s", resp.StatusCode, body)
	}
}

// TestThroughSocket wires the panel to a real broker over a unix socket.
func TestThroughSocket(t *testing.T) {
	dir, err := os.MkdirTemp("", "pnl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "broker.sock")
	l, err := broker.Listen(sock, -1)
	if err != nil {
		t.Fatal(err)
	}
	jobsCtx, cancel := context.WithCancel(context.Background())
	jobs := broker.NewJobs(jobsCtx)
	b := broker.New(&config.Store{Dir: t.TempDir()}, noServices{}, jobs,
		func(_ context.Context, args []string, stdout, _ io.Writer) (int, error) {
			io.WriteString(stdout, `{"args":"`+strings.Join(args, " ")+`","schema":1}`)
			return 0, nil
		})
	b.Load()
	bsrv := &http.Server{Handler: b.Handler(), ReadHeaderTimeout: time.Second}
	go bsrv.Serve(l)
	t.Cleanup(func() { bsrv.Close(); cancel(); jobs.Wait() })

	s, err := New("me:pw", broker.UnixClient(sock), broker.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s)
	defer srv.Close()
	resp, body := send(t, srv, req{method: "GET", path: "/api/status"})
	if resp.StatusCode != 200 || !strings.Contains(body, `"mode":"normal"`) {
		t.Fatalf("status = %d %s", resp.StatusCode, body)
	}
	resp, body = send(t, srv, req{method: "POST", path: "/api/verbs/update", body: `{"image":true}`,
		header: map[string]string{"Content-Type": "application/json", "X-Berth-Panel": "1"}})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("post = %d %s", resp.StatusCode, body)
	}
	jobs.Wait()
	_, body = send(t, srv, req{method: "GET", path: "/api/jobs/1"})
	if !strings.Contains(body, `"state":"succeeded"`) || !strings.Contains(body, `update --json --image`) {
		t.Errorf("job = %s", body)
	}
}

type noServices struct{}

func (noServices) Reconcile([]supervisor.Spec) supervisor.Changes { return supervisor.Changes{} }
func (noServices) Restart(string) error                           { return nil }
func (noServices) Status() []supervisor.Status                    { return []supervisor.Status{} }
