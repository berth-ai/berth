// Package panel serves the Berth web panel, the only published port and the
// only login. It keeps browser sessions, forwards allowlisted API calls to
// the root broker over its unix socket, and proxies the terminal and the
// IDE. It runs as berth-panel and never executes anything itself.
package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxBodyBytes = 64 << 10
	panelCSP     = "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"
	modeSetup    = "setup"
)

// browserVerbs are the broker verbs the browser may call. auth.verify,
// setup.complete and auth.reset never appear: the panel calls the first two
// itself, and the last is root's alone.
var browserVerbs = map[string]bool{
	"update": true, "rollback": true, "gc": true, "update.check": true,
	"service.restart": true, "auth.change": true,
	"config.get": true, "config.export": true, "config.apply": true, "config.revert": true, "config.reset": true,
	"tools.list": true, "tools.versions": true, "tools.registry": true, "tools.add": true, "tools.remove": true,
	"preset.install": true, "git.identity": true, "forge.status": true, "forge.login": true,
	"skills.sources.preview": true, "skills.sources.add": true, "skills.sources.update": true, "skills.sources.remove": true,
	"config.upgrade":  true,
	"agents.registry": true, "agents.add": true, "agents.remove": true, "agents.rollback": true,
	"workspaces.add": true, "workspaces.forget": true, "workspaces.status": true, "workspaces.candidates": true, "workspaces.launch": true, "workspaces.restart": true,
	"workspaces.instructions.read": true, "workspaces.instructions.write": true, "workspaces.instructions.merge": true,
	"workspaces.report": true, "skills.vendor.resolve": true,
	"login.status": true, "login.start": true, "login.input": true, "login.cancel": true, "login.logout": true,
}

var jobID = regexp.MustCompile(`^[0-9]{1,9}$`)

// Upstreams are the loopback frontends the panel proxies.
type Upstreams struct {
	Terminal *url.URL
	IDE      *url.URL
}

// DefaultUpstreams are ttyd and code-server as the modules start them.
func DefaultUpstreams() Upstreams {
	return Upstreams{
		Terminal: &url.URL{Scheme: "http", Host: "127.0.0.1:7681"},
		IDE:      &url.URL{Scheme: "http", Host: "127.0.0.1:8080"},
	}
}

// Server is the panel's HTTP handler.
type Server struct {
	client   *http.Client
	broker   string
	mux      *http.ServeMux
	sessions *sessions
	limiter  *limiter
	modes    *modeCache
	assets   fs.FS
}

// New returns a panel that forwards to the broker at brokerURL through
// client and proxies up.
func New(client *http.Client, brokerURL string, up Upstreams) *Server {
	s := &Server{
		client:   client,
		broker:   strings.TrimSuffix(brokerURL, "/"),
		mux:      http.NewServeMux(),
		sessions: newSessions(time.Now),
		limiter:  newLimiter(time.Now),
	}
	s.modes = &modeCache{ttl: time.Second, now: time.Now, fetch: s.fetchMode}
	s.assets = builtAssets()
	s.mux.HandleFunc("GET /{$}", s.page("index.html"))
	s.mux.HandleFunc("GET /login", s.page("login.html"))
	s.mux.HandleFunc("GET /setup", s.page("setup.html"))
	s.mux.HandleFunc("GET /static/{path...}", s.handleAsset)
	s.mux.HandleFunc("GET /api/session", s.handleSession)
	s.mux.HandleFunc("POST /api/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/logout", s.handleLogout)
	s.mux.HandleFunc("POST /api/setup", s.handleSetup)
	s.mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		s.forward(w, r, http.MethodGet, "/status", nil)
	})
	s.mux.HandleFunc("GET /api/jobs", func(w http.ResponseWriter, r *http.Request) {
		s.forward(w, r, http.MethodGet, "/jobs", nil)
	})
	s.mux.HandleFunc("GET /api/jobs/{id}", s.handleJob)
	s.mux.HandleFunc("POST /api/verbs/{verb}", s.handleVerb)
	if up.Terminal != nil {
		s.mux.Handle("/terminal/", newProxy("terminal", up.Terminal, "", terminalQuery, s.unavailable))
	}
	if up.IDE != nil {
		s.mux.Handle("/ide/", newProxy("IDE", up.IDE, "/ide", nil, s.unavailable))
	}
	return s
}

// UseAssets serves the frontend from fsys instead of the embedded build:
// pages at its root, files for /static/ under their own paths. The dev
// server points it at web/dist on disk; tests use a fixture.
func (s *Server) UseAssets(fsys fs.FS) {
	s.assets = fsys
}

// open paths need no session; setup paths stay reachable in setup mode.
func open(p string) bool {
	return p == "/login" || p == "/setup" || p == "/api/login" || p == "/api/setup" || p == "/api/session" ||
		strings.HasPrefix(p, "/static/")
}

func setupPath(p string) bool {
	return p == "/setup" || p == "/api/setup" || p == "/api/session" || strings.HasPrefix(p, "/static/")
}

func proxied(p string) bool {
	return strings.HasPrefix(p, "/terminal/") || strings.HasPrefix(p, "/ide/")
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	if !proxied(p) {
		setPanelHeaders(w.Header())
	}
	mode := s.modes.get(r.Context())
	switch {
	case mode == modeSetup && !setupPath(p):
		s.deny(w, r, http.StatusConflict, "setup_required", "finish setup first", "/setup")
		return
	case mode != modeSetup && p == "/setup":
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if !open(p) && s.sessions.get(r) == nil {
		s.deny(w, r, http.StatusUnauthorized, "unauthorized", "sign in first", "/login")
		return
	}
	if proxied(p) && r.Header.Get("Upgrade") != "" && originProblem(r) != "" {
		writeError(w, http.StatusForbidden, "forbidden", "cross-origin WebSocket")
		return
	}
	s.mux.ServeHTTP(w, r)
}

func setPanelHeaders(h http.Header) {
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", panelCSP)
	h.Set("X-Frame-Options", "DENY")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
}

// deny answers API calls with an error and everything else with a redirect.
func (s *Server) deny(w http.ResponseWriter, r *http.Request, code int, errCode, msg, to string) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, code, errCode, msg)
		return
	}
	setPanelHeaders(w.Header())
	http.Redirect(w, r, to, http.StatusSeeOther)
}

func (s *Server) page(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		body, err := fs.ReadFile(s.assets, name)
		if err != nil {
			http.Error(w, "the panel UI is not built; run pnpm build in web/", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(body)
	}
}

func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("path")
	ct, ok := assetTypes[path.Ext(name)]
	if !ok || !fs.ValidPath(name) {
		http.NotFound(w, r)
		return
	}
	body, err := fs.ReadFile(s.assets, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	// Built files carry a content hash in their name and never change.
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	_, _ = w.Write(body)
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"user": nil, "mode": s.modes.get(r.Context())}
	if sess := s.sessions.get(r); sess != nil {
		out["user"] = sess.user
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		User     string `json:"user"`
		Password string `json:"password"`
	}
	body, ok := s.readPost(w, r, maxBodyBytes)
	if !ok {
		return
	}
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "send {\"user\":…,\"password\":…}")
		return
	}
	client := clientAddr(r)
	if wait := s.limiter.wait(client); wait > 0 {
		tooMany(w, wait)
		return
	}
	code, out, err := s.send(r.Context(), http.MethodPost, "/verbs/auth.verify", body)
	if err != nil {
		writeError(w, http.StatusBadGateway, "broker_unavailable", "the supervisor is not reachable: "+err.Error())
		return
	}
	if code != http.StatusOK {
		writeRaw(w, code, out)
		return
	}
	var res struct {
		Valid bool `json:"valid"`
	}
	_ = json.Unmarshal(out, &res)
	if !res.Valid {
		wait := s.limiter.fail(client)
		w.Header().Set("Retry-After", seconds(wait))
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]any{
			"code": "invalid_login", "message": "wrong user name or password", "retry_after": secondsInt(wait)}})
		return
	}
	s.limiter.succeed(client)
	http.SetCookie(w, sessionCookie(r, s.sessions.create(in.User), int(sessionMax.Seconds())))
	writeJSON(w, http.StatusOK, map[string]any{"user": in.User})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.readPost(w, r, maxBodyBytes); !ok {
		return
	}
	s.sessions.delete(r)
	http.SetCookie(w, sessionCookie(r, "", -1))
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	body, ok := s.readPost(w, r, maxBodyBytes)
	if !ok {
		return
	}
	client := clientAddr(r)
	if wait := s.limiter.wait(client); wait > 0 {
		tooMany(w, wait)
		return
	}
	code, out, err := s.send(r.Context(), http.MethodPost, "/verbs/setup.complete", body)
	if err != nil {
		writeError(w, http.StatusBadGateway, "broker_unavailable", "the supervisor is not reachable: "+err.Error())
		return
	}
	switch code {
	case http.StatusForbidden:
		s.limiter.fail(client)
	case http.StatusOK:
		var res struct {
			User string `json:"user"`
		}
		_ = json.Unmarshal(out, &res)
		s.limiter.succeed(client)
		s.modes.invalidate()
		// A new login (first run or after reset-login) signs out every
		// session made with the old one.
		s.sessions.reset()
		http.SetCookie(w, sessionCookie(r, s.sessions.create(res.User), int(sessionMax.Seconds())))
	}
	writeRaw(w, code, out)
}

func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !jobID.MatchString(id) {
		writeError(w, http.StatusNotFound, "not_found", "no such job")
		return
	}
	s.forward(w, r, http.MethodGet, "/jobs/"+id, nil)
}

func (s *Server) handleVerb(w http.ResponseWriter, r *http.Request) {
	verb := r.PathValue("verb")
	if msg := csrfProblem(r); msg != "" {
		writeError(w, http.StatusForbidden, "forbidden", msg)
		return
	}
	if !browserVerbs[verb] {
		writeError(w, http.StatusNotFound, "unknown_verb", "unknown verb")
		return
	}
	body, ok := s.readPost(w, r, bodyLimit(verb))
	if !ok {
		return
	}
	code, out, err := s.send(r.Context(), http.MethodPost, "/verbs/"+verb, body)
	if err != nil {
		writeError(w, http.StatusBadGateway, "broker_unavailable", "the supervisor is not reachable: "+err.Error())
		return
	}
	if verb == "auth.change" && code == http.StatusOK {
		var res struct {
			User string `json:"user"`
		}
		_ = json.Unmarshal(out, &res)
		s.sessions.reset()
		http.SetCookie(w, sessionCookie(r, s.sessions.create(res.User), int(sessionMax.Seconds())))
	}
	writeRaw(w, code, out)
}

// bodyLimit is the largest request body verb accepts: an instruction file
// is up to 256 KiB, which JSON escaping can grow several times.
func bodyLimit(verb string) int64 {
	if verb == "workspaces.instructions.write" {
		return 2 << 20
	}
	return maxBodyBytes
}

// readPost applies the CSRF rules and reads a body of at most limit bytes.
func (s *Server) readPost(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, bool) {
	if msg := csrfProblem(r); msg != "" {
		writeError(w, http.StatusForbidden, "forbidden", msg)
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return nil, false
	}
	if int64(len(body)) > limit {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "request too large")
		return nil, false
	}
	return body, true
}

// csrfProblem explains why r may come from another site, or returns "". A
// mutating request must carry a custom header, which forces a CORS preflight
// the panel never answers. When the browser reports them, its Origin and
// Sec-Fetch-Site must also be same-origin.
func csrfProblem(r *http.Request) string {
	if r.Header.Get("X-Berth-Panel") != "1" {
		return "missing X-Berth-Panel header"
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return "Content-Type must be application/json"
	}
	return originProblem(r)
}

func originProblem(r *http.Request) string {
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host != r.Host {
			return "cross-origin request"
		}
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
	default:
		return "cross-site request"
	}
	return ""
}

func (s *Server) send(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.broker+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return resp.StatusCode, out, err
}

func (s *Server) forward(w http.ResponseWriter, r *http.Request, method, path string, body []byte) {
	code, out, err := s.send(r.Context(), method, path, body)
	if err != nil {
		writeError(w, http.StatusBadGateway, "broker_unavailable", "the supervisor is not reachable: "+err.Error())
		return
	}
	writeRaw(w, code, out)
}

func (s *Server) fetchMode(ctx context.Context) (string, error) {
	code, out, err := s.send(ctx, http.MethodGet, "/mode", nil)
	if err != nil {
		return "", err
	}
	var res struct {
		Mode string `json:"mode"`
	}
	if code != http.StatusOK || json.Unmarshal(out, &res) != nil {
		return "", io.ErrUnexpectedEOF
	}
	return res.Mode, nil
}

// modeCache asks the broker for its mode at most once per ttl. A failed
// lookup is not cached and reads as "": sign-in required, setup unreachable.
type modeCache struct {
	mu    sync.Mutex
	ttl   time.Duration
	now   func() time.Time
	fetch func(context.Context) (string, error)
	mode  string
	at    time.Time
}

func (m *modeCache) get(ctx context.Context) string {
	m.mu.Lock()
	if !m.at.IsZero() && m.now().Sub(m.at) < m.ttl {
		defer m.mu.Unlock()
		return m.mode
	}
	m.mu.Unlock()
	mode, err := m.fetch(ctx)
	if err != nil {
		return ""
	}
	m.mu.Lock()
	m.mode, m.at = mode, m.now()
	m.mu.Unlock()
	return mode
}

func (m *modeCache) invalidate() {
	m.mu.Lock()
	m.at = time.Time{}
	m.mu.Unlock()
}

func tooMany(w http.ResponseWriter, wait time.Duration) {
	w.Header().Set("Retry-After", seconds(wait))
	writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{
		"code": "too_many_attempts", "message": "too many failed attempts; wait and try again",
		"retry_after": secondsInt(wait)}})
}

func secondsInt(d time.Duration) int { return int((d + time.Second - 1) / time.Second) }
func seconds(d time.Duration) string { return strconv.Itoa(secondsInt(d)) }

func writeRaw(w http.ResponseWriter, code int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, errCode, message string) {
	writeJSON(w, code, map[string]any{"error": map[string]string{"code": errCode, "message": message}})
}
