package panel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/berth-ai/berth/internal/broker"
	"github.com/berth-ai/berth/internal/config"
	"github.com/berth-ai/berth/internal/login"
	"github.com/berth-ai/berth/internal/supervisor"
)

// fakeBroker answers like the real broker for the panel's needs.
type fakeBroker struct {
	mu       sync.Mutex
	mode     string
	requests []string
	bodies   []string
}

func (f *fakeBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())
	f.bodies = append(f.bodies, string(body))
	mode := f.mode
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	var in map[string]any
	json.Unmarshal(body, &in)
	switch r.URL.Path {
	case "/mode":
		json.NewEncoder(w).Encode(map[string]string{"mode": mode})
	case "/status":
		io.WriteString(w, `{"mode":"`+mode+`"}`)
	case "/jobs":
		io.WriteString(w, `{"jobs":[]}`)
	case "/verbs/auth.verify":
		ok := in["user"] == "me" && in["password"] == "correct horse battery"
		json.NewEncoder(w).Encode(map[string]bool{"valid": ok})
	case "/verbs/setup.complete":
		if in["token"] != "tok" {
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"error":{"code":"invalid_token","message":"x"}}`)
			return
		}
		f.mu.Lock()
		f.mode = "normal"
		f.mu.Unlock()
		io.WriteString(w, `{"user":"me","config":{}}`)
	case "/verbs/auth.change":
		io.WriteString(w, `{"user":"you"}`)
	default:
		if strings.HasPrefix(r.URL.Path, "/verbs/") {
			w.WriteHeader(http.StatusAccepted)
			io.WriteString(w, `{"job":{"id":1}}`)
			return
		}
		io.WriteString(w, `{"job":null}`)
	}
}

// count is the number of requests other than mode lookups, which the panel
// makes on every request when the mode cache is off.
func (f *fakeBroker) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.requests {
		if r != "GET /mode" {
			n++
		}
	}
	return n
}

// last is the latest request other than a mode lookup.
func (f *fakeBroker) last() (string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.requests) - 1; i >= 0; i-- {
		if f.requests[i] != "GET /mode" {
			return f.requests[i], f.bodies[i]
		}
	}
	return "", ""
}

type env struct {
	t   *testing.T
	srv *httptest.Server
	s   *Server
	fb  *fakeBroker
	now time.Time
}

func newEnv(t *testing.T, mode string) *env {
	t.Helper()
	fb := &fakeBroker{mode: mode}
	bsrv := httptest.NewServer(fb)
	t.Cleanup(bsrv.Close)
	e := &env{t: t, fb: fb, now: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)}
	e.s = New(bsrv.Client(), bsrv.URL, Upstreams{})
	clock := func() time.Time { return e.now }
	e.s.sessions.now, e.s.limiter.now = clock, clock
	e.s.modes.ttl = 0 // ask the broker every time
	e.srv = httptest.NewServer(e.s)
	t.Cleanup(e.srv.Close)
	return e
}

// client returns a browser-like client: a cookie jar, no redirects followed.
func (e *env) client() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (e *env) do(c *http.Client, method, path, body string, header map[string]string) (*http.Response, string) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	if method == "POST" {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Berth-Panel", "1")
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func (e *env) login(c *http.Client) {
	e.t.Helper()
	if resp, body := e.do(c, "POST", "/api/login", `{"user":"me","password":"correct horse battery"}`, nil); resp.StatusCode != 200 {
		e.t.Fatalf("login = %d %s", resp.StatusCode, body)
	}
}

func TestSetupModeGates(t *testing.T) {
	e := newEnv(t, "setup")
	e.s.UseAssets(fixtureAssets())
	c := e.client()
	for path, want := range map[string]string{"/": "/setup", "/login": "/setup", "/terminal/": "/setup"} {
		resp, _ := e.do(c, "GET", path, "", nil)
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != want {
			t.Errorf("GET %s = %d → %s", path, resp.StatusCode, resp.Header.Get("Location"))
		}
	}
	if resp, body := e.do(c, "GET", "/api/status", "", nil); resp.StatusCode != 409 || !strings.Contains(body, "setup_required") {
		t.Errorf("api in setup = %d %s", resp.StatusCode, body)
	}
	for _, path := range []string{"/setup", "/static/assets/app-1a2b.js", "/static/assets/app-1a2b.css", "/api/session"} {
		if resp, _ := e.do(c, "GET", path, "", nil); resp.StatusCode != 200 {
			t.Errorf("GET %s = %d", path, resp.StatusCode)
		}
	}
}

func TestSetupCreatesSession(t *testing.T) {
	e := newEnv(t, "setup")
	c := e.client()
	if resp, body := e.do(c, "POST", "/api/setup", `{"token":"bad","user":"me","password":"correct horse battery"}`, nil); resp.StatusCode != 403 || !strings.Contains(body, "invalid_token") {
		t.Fatalf("bad token = %d %s", resp.StatusCode, body)
	}
	e.now = e.now.Add(2 * time.Second) // past the backoff for the bad token
	resp, body := e.do(c, "POST", "/api/setup", `{"token":"tok","user":"me","password":"correct horse battery"}`, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("setup = %d %s", resp.StatusCode, body)
	}
	assertSessionCookie(t, resp, false)
	if resp, _ := e.do(c, "GET", "/api/status", "", nil); resp.StatusCode != 200 {
		t.Errorf("status after setup = %d", resp.StatusCode)
	}
	if resp, _ := e.do(c, "GET", "/setup", "", nil); resp.Header.Get("Location") != "/" {
		t.Errorf("/setup after setup = %d → %s", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func assertSessionCookie(t *testing.T, resp *http.Response, secure bool) {
	t.Helper()
	for _, c := range resp.Cookies() {
		if c.Name == "berth_session" {
			if len(c.Value) != 43 || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.Secure != secure {
				t.Errorf("cookie = %+v", c)
			}
			return
		}
	}
	t.Fatal("no berth_session cookie")
}

func TestLoginRequiredAndBackoff(t *testing.T) {
	e := newEnv(t, "normal")
	c := e.client()
	if resp, _ := e.do(c, "GET", "/", "", nil); resp.StatusCode != 303 || resp.Header.Get("Location") != "/login" {
		t.Errorf("GET / = %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp, body := e.do(c, "GET", "/api/status", "", nil); resp.StatusCode != 401 || !strings.Contains(body, "unauthorized") {
		t.Errorf("api = %d %s", resp.StatusCode, body)
	}
	wrong := `{"user":"me","password":"nope"}`
	resp, body := e.do(c, "POST", "/api/login", wrong, nil)
	if resp.StatusCode != 401 || !strings.Contains(body, `"retry_after":1`) {
		t.Fatalf("wrong = %d %s", resp.StatusCode, body)
	}
	n := e.fb.count()
	resp, _ = e.do(c, "POST", "/api/login", `{"user":"me","password":"correct horse battery"}`, nil)
	if resp.StatusCode != 429 || resp.Header.Get("Retry-After") != "1" || e.fb.count() != n {
		t.Fatalf("locked = %d %s, broker asked: %v", resp.StatusCode, resp.Header.Get("Retry-After"), e.fb.count() != n)
	}
	e.now = e.now.Add(time.Second)
	if _, body := e.do(c, "POST", "/api/login", wrong, nil); !strings.Contains(body, `"retry_after":2`) {
		t.Errorf("second failure = %s", body)
	}
	e.now = e.now.Add(2 * time.Second)
	e.login(c)
	if resp, _ := e.do(c, "GET", "/api/status", "", nil); resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if resp, _ := e.do(c, "GET", "/", "", nil); resp.StatusCode != 200 {
		t.Errorf("index = %d", resp.StatusCode)
	}
}

func TestBackoffCapsAtFiveMinutes(t *testing.T) {
	l := newLimiter(time.Now)
	var d time.Duration
	for range 20 {
		d = l.fail("10.0.0.1")
	}
	if d != 5*time.Minute {
		t.Errorf("delay = %s", d)
	}
	l.succeed("10.0.0.1")
	if l.wait("10.0.0.1") != 0 {
		t.Error("success must clear the backoff")
	}
}

func TestSecureCookieBehindTLSProxy(t *testing.T) {
	e := newEnv(t, "normal")
	c := e.client()
	resp, _ := e.do(c, "POST", "/api/login", `{"user":"me","password":"correct horse battery"}`,
		map[string]string{"X-Forwarded-Proto": "https"})
	assertSessionCookie(t, resp, true)
}

func TestSessionExpiry(t *testing.T) {
	e := newEnv(t, "normal")
	c := e.client()
	e.login(c)
	e.now = e.now.Add(12*time.Hour + time.Second)
	if resp, _ := e.do(c, "GET", "/api/status", "", nil); resp.StatusCode != 401 {
		t.Errorf("after idle timeout = %d", resp.StatusCode)
	}
	e.login(c)
	for range 16 { // active every 11 h: never idle, but past 7 days in total
		e.now = e.now.Add(11 * time.Hour)
		e.do(c, "GET", "/api/status", "", nil)
	}
	if resp, _ := e.do(c, "GET", "/api/status", "", nil); resp.StatusCode != 401 {
		t.Errorf("after the absolute limit = %d", resp.StatusCode)
	}
}

func TestLogout(t *testing.T) {
	e := newEnv(t, "normal")
	c := e.client()
	e.login(c)
	if resp, _ := e.do(c, "POST", "/api/logout", `{}`, nil); resp.StatusCode != 200 {
		t.Fatalf("logout = %d", resp.StatusCode)
	}
	if resp, _ := e.do(c, "GET", "/api/status", "", nil); resp.StatusCode != 401 {
		t.Errorf("after logout = %d", resp.StatusCode)
	}
}

func TestAuthChangeRotatesSessions(t *testing.T) {
	e := newEnv(t, "normal")
	a, b := e.client(), e.client()
	e.login(a)
	e.login(b)
	resp, _ := e.do(a, "POST", "/api/verbs/auth.change", `{"current_password":"correct horse battery","user":"you","password":"another long one"}`, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("change = %d", resp.StatusCode)
	}
	if resp, _ := e.do(b, "GET", "/api/status", "", nil); resp.StatusCode != 401 {
		t.Errorf("other session survived = %d", resp.StatusCode)
	}
	if resp, body := e.do(a, "GET", "/api/session", "", nil); !strings.Contains(body, `"user":"you"`) {
		t.Errorf("caller session = %d %s", resp.StatusCode, body)
	}
}

func TestLoginCSRF(t *testing.T) {
	e := newEnv(t, "normal")
	c := e.client()
	for name, h := range map[string]map[string]string{
		"cross origin": {"Origin": "http://evil.example"},
		"no marker":    {"X-Berth-Panel": ""},
		"form":         {"Content-Type": "application/x-www-form-urlencoded"},
	} {
		if resp, _ := e.do(c, "POST", "/api/login", `{"user":"me","password":"correct horse battery"}`, h); resp.StatusCode != 403 {
			t.Errorf("%s = %d", name, resp.StatusCode)
		}
	}
}

func TestBrokerDownRequiresLogin(t *testing.T) {
	bsrv := httptest.NewServer(http.NotFoundHandler())
	url := bsrv.URL
	bsrv.Close()
	srv := httptest.NewServer(New(http.DefaultClient, url, Upstreams{}))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	resp, _ = http.Post(srv.URL+"/api/login", "application/json", strings.NewReader(`{}`))
	if resp.StatusCode != 403 { // no X-Berth-Panel: CSRF wins before the broker is asked
		t.Errorf("login = %d", resp.StatusCode)
	}
}

func TestClientAddr(t *testing.T) {
	for _, c := range []struct{ remote, xff, want string }{
		{"203.0.113.9:5000", "", "203.0.113.9"},
		{"203.0.113.9:5000", "198.51.100.1", "203.0.113.9"},       // public peer: header ignored
		{"172.17.0.1:5000", "198.51.100.1, 10.0.0.2", "10.0.0.2"}, // private proxy: rightmost entry
		{"127.0.0.1:5000", "garbage", "127.0.0.1"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := clientAddr(r); got != c.want {
			t.Errorf("%+v: %s", c, got)
		}
	}
}

func assertSecurityHeaders(t *testing.T, resp *http.Response) {
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
	e := newEnv(t, "normal")
	e.s.UseAssets(fixtureAssets())
	c := e.client()
	e.login(c)
	resp, body := e.do(c, "GET", "/", "", nil)
	if resp.StatusCode != 200 || !strings.Contains(body, "<title>app</title>") ||
		!strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("GET / = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	assertSecurityHeaders(t, resp)
	for path, ct := range map[string]string{"/static/assets/app-1a2b.js": "text/javascript", "/static/assets/app-1a2b.css": "text/css"} {
		resp, _ := e.do(c, "GET", path, "", nil)
		if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), ct) {
			t.Errorf("GET %s = %d %q", path, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
	}
	for _, path := range []string{"/nope", "/static/", "/static/missing.js", "/api/other"} {
		if resp, _ := e.do(c, "GET", path, "", nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d", path, resp.StatusCode)
		}
	}
	if resp, _ := e.do(e.client(), "GET", "/login", "", nil); resp.StatusCode != 200 {
		t.Errorf("GET /login = %d", resp.StatusCode)
	}
}

func TestReadProxy(t *testing.T) {
	e := newEnv(t, "normal")
	c := e.client()
	e.login(c)
	for path, want := range map[string]string{"/api/status?check=1&x=y": "GET /status", "/api/jobs": "GET /jobs", "/api/jobs/7": "GET /jobs/7"} {
		resp, _ := e.do(c, "GET", path, "", nil)
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/json" {
			t.Errorf("GET %s = %d", path, resp.StatusCode)
		}
		assertSecurityHeaders(t, resp)
		if got, _ := e.fb.last(); got != want {
			t.Errorf("GET %s: broker saw %q", path, got)
		}
	}
}

func TestVerbAllowlist(t *testing.T) {
	e := newEnv(t, "normal")
	c := e.client()
	e.login(c)
	for _, verb := range []string{"frobnicate", "..%2fstatus", "auth.verify", "setup.complete", "auth.reset"} {
		before := e.fb.count()
		resp, body := e.do(c, "POST", "/api/verbs/"+verb, `{}`, nil)
		if resp.StatusCode != http.StatusNotFound || !strings.Contains(body, "unknown_verb") || e.fb.count() != before {
			t.Errorf("%s: %d %s", verb, resp.StatusCode, body)
		}
	}
	for _, verb := range []string{
		"tools.list", "tools.versions", "tools.registry", "tools.add", "tools.remove",
		"preset.install", "git.identity", "forge.status", "forge.login",
		"skills.scan", "skills.add", "skills.update",
		"login.status", "login.start", "login.input", "login.cancel", "login.logout",
	} {
		resp, body := e.do(c, "POST", "/api/verbs/"+verb, `{}`, nil)
		if got, _ := e.fb.last(); got != "POST /verbs/"+verb {
			t.Errorf("%s: %d %s, broker saw %q", verb, resp.StatusCode, body, got)
		}
	}
	for _, path := range []string{"/api/jobs/x", "/api/jobs/1a"} {
		if resp, _ := e.do(c, "GET", path, "", nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d", path, resp.StatusCode)
		}
	}
}

func TestJobPostCSRF(t *testing.T) {
	e := newEnv(t, "normal")
	c := e.client()
	e.login(c)
	host := strings.TrimPrefix(e.srv.URL, "http://")
	for name, tc := range map[string]struct {
		header map[string]string
		want   int
	}{
		"no marker":           {map[string]string{"X-Berth-Panel": ""}, http.StatusForbidden},
		"wrong marker":        {map[string]string{"X-Berth-Panel": "yes"}, http.StatusForbidden},
		"form content type":   {map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, http.StatusForbidden},
		"text content type":   {map[string]string{"Content-Type": "text/plain"}, http.StatusForbidden},
		"cross origin":        {map[string]string{"Origin": "http://evil.example"}, http.StatusForbidden},
		"null origin":         {map[string]string{"Origin": "null"}, http.StatusForbidden},
		"cross site fetch":    {map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		"same site fetch":     {map[string]string{"Sec-Fetch-Site": "same-site"}, http.StatusForbidden},
		"plain":               {nil, http.StatusAccepted},
		"same origin":         {map[string]string{"Origin": "http://" + host, "Sec-Fetch-Site": "same-origin"}, http.StatusAccepted},
		"https origin":        {map[string]string{"Origin": "https://" + host}, http.StatusAccepted},
		"json with charset":   {map[string]string{"Content-Type": "application/json; charset=utf-8"}, http.StatusAccepted},
		"user-initiated none": {map[string]string{"Sec-Fetch-Site": "none"}, http.StatusAccepted},
	} {
		t.Run(name, func(t *testing.T) {
			n := e.fb.count()
			resp, _ := e.do(c, "POST", "/api/verbs/rollback", `{}`, tc.header)
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.want)
			}
			if forwarded := e.fb.count() > n; forwarded != (tc.want == http.StatusAccepted) {
				t.Errorf("forwarded = %v", forwarded)
			}
		})
	}
	if got, body := e.fb.last(); got != "POST /verbs/rollback" || body != `{}` {
		t.Errorf("broker saw %q %q", got, body)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	e := newEnv(t, "normal")
	c := e.client()
	e.login(c)
	for _, r := range [][2]string{{"GET", "/api/verbs/update"}, {"POST", "/api/status"}, {"DELETE", "/api/jobs"}} {
		if resp, _ := e.do(c, r[0], r[1], "", nil); resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d", r[0], r[1], resp.StatusCode)
		}
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
	logins := login.Store{Path: filepath.Join(dir, "secrets", "panel.json")}
	if err := logins.Set("me", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	jobsCtx, cancel := context.WithCancel(context.Background())
	jobs := broker.NewJobs(jobsCtx)
	b := broker.New(&config.Store{Dir: t.TempDir()}, logins, noServices{}, jobs,
		func(_ context.Context, args []string, stdout, _ io.Writer) (int, error) {
			io.WriteString(stdout, `{"args":"`+strings.Join(args, " ")+`","schema":1}`)
			return 0, nil
		})
	b.Load()
	bsrv := &http.Server{Handler: b.Handler(), ReadHeaderTimeout: time.Second}
	go bsrv.Serve(l)
	t.Cleanup(func() { bsrv.Close(); cancel(); jobs.Wait() })

	srv := httptest.NewServer(New(broker.UnixClient(sock), broker.BaseURL, Upstreams{}))
	defer srv.Close()
	e := &env{t: t, srv: srv}
	c := e.client()
	e.login(c)
	resp, body := e.do(c, "GET", "/api/status", "", nil)
	if resp.StatusCode != 200 || !strings.Contains(body, `"mode":"normal"`) {
		t.Fatalf("status = %d %s", resp.StatusCode, body)
	}
	resp, body = e.do(c, "POST", "/api/verbs/update", `{"image":true}`, nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("post = %d %s", resp.StatusCode, body)
	}
	jobs.Wait()
	_, body = e.do(c, "GET", "/api/jobs/1", "", nil)
	if !strings.Contains(body, `"state":"succeeded"`) || !strings.Contains(body, `update --json --image`) {
		t.Errorf("job = %s", body)
	}
}

type noServices struct{}

func (noServices) Reconcile([]supervisor.Spec) supervisor.Changes { return supervisor.Changes{} }
func (noServices) Restart(string) error                           { return nil }
func (noServices) Status() []supervisor.Status                    { return []supervisor.Status{} }

// After berth config reset-login, finishing setup again must sign out every
// session from before the reset.
func TestSetupSignsOutOldSessions(t *testing.T) {
	e := newEnv(t, "normal")
	old := e.client()
	e.login(old)
	e.fb.mu.Lock()
	e.fb.mode = "setup"
	e.fb.mu.Unlock()
	if resp, _ := e.do(e.client(), "POST", "/api/setup", `{"token":"tok","user":"me","password":"correct horse battery"}`, nil); resp.StatusCode != 200 {
		t.Fatalf("setup = %d", resp.StatusCode)
	}
	if resp, _ := e.do(old, "GET", "/api/status", "", nil); resp.StatusCode != 401 {
		t.Errorf("session from before the reset = %d", resp.StatusCode)
	}
}
