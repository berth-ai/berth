// Package panel serves the Berth web panel: an authenticated page and a small
// API that forwards to the root broker over its unix socket. The panel runs
// unprivileged and never executes anything itself.
package panel

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// BrokerURL is the base URL used with UnixClient; the host is ignored.
const BrokerURL = "http://broker"

const maxBodyBytes = 4 << 10

//go:embed static
var static embed.FS

var assets = map[string]string{
	"panel.js":  "text/javascript; charset=utf-8",
	"panel.css": "text/css; charset=utf-8",
}

// Server is the panel's HTTP handler.
type Server struct {
	user, pass [sha256.Size]byte
	client     *http.Client
	broker     string
	mux        *http.ServeMux
}

// ParseCredential splits user:password at the first colon; both parts must
// be non-empty.
func ParseCredential(cred string) (user, pass string, err error) {
	user, pass, ok := strings.Cut(cred, ":")
	if !ok || user == "" || pass == "" {
		return "", "", errors.New("PANEL_CREDENTIAL must be user:password with both parts non-empty")
	}
	return user, pass, nil
}

// UnixClient returns a client that sends every request to the broker socket.
func UnixClient(socket string) *http.Client {
	var d net.Dialer
	return &http.Client{
		Timeout: 3 * time.Minute, // above the broker's own status timeout
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return d.DialContext(ctx, "unix", socket)
			},
		},
	}
}

// New returns a panel for credential that forwards API calls to brokerURL.
func New(credential string, client *http.Client, brokerURL string) (*Server, error) {
	user, pass, err := ParseCredential(credential)
	if err != nil {
		return nil, err
	}
	s := &Server{
		user:   sha256.Sum256([]byte(user)),
		pass:   sha256.Sum256([]byte(pass)),
		client: client,
		broker: strings.TrimSuffix(brokerURL, "/"),
		mux:    http.NewServeMux(),
	}
	s.mux.HandleFunc("GET /{$}", s.handleIndex)
	s.mux.HandleFunc("GET /static/{name}", s.handleAsset)
	s.mux.HandleFunc("GET /api/status", s.handleStatus)
	s.mux.HandleFunc("GET /api/job", s.handleJob)
	s.mux.HandleFunc("POST /api/jobs", s.handleStart)
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	h.Set("X-Frame-Options", "DENY")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	if !s.authorized(r) {
		h.Set("WWW-Authenticate", `Basic realm="Berth", charset="UTF-8"`)
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	s.mux.ServeHTTP(w, r)
}

// authorized compares digests in constant time, so neither the content nor
// the length of the credential leaks through timing.
func (s *Server) authorized(r *http.Request) bool {
	user, pass, ok := r.BasicAuth()
	if !ok {
		return false
	}
	u := sha256.Sum256([]byte(user))
	p := sha256.Sum256([]byte(pass))
	return subtle.ConstantTimeCompare(u[:], s.user[:])&subtle.ConstantTimeCompare(p[:], s.pass[:]) == 1
}

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	page, _ := static.ReadFile("static/index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(page)
}

func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ct, ok := assets[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	body, _ := static.ReadFile("static/" + name)
	w.Header().Set("Content-Type", ct)
	_, _ = w.Write(body)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	path := "/status"
	if r.URL.Query().Get("check") == "1" {
		path += "?check=1"
	}
	s.forward(w, r, http.MethodGet, path, nil)
}

func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	s.forward(w, r, http.MethodGet, "/jobs/current", nil)
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	if msg := csrfProblem(r); msg != "" {
		writeError(w, http.StatusForbidden, "forbidden", msg)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if len(body) > maxBodyBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "invalid_request", "request too large")
		return
	}
	s.forward(w, r, http.MethodPost, "/jobs", body)
}

// csrfProblem explains why r may come from another site, or returns "".
// Browsers resend Basic credentials on cross-site requests, so a mutating
// request must carry a custom header (which forces a CORS preflight that the
// panel never answers) and, when the browser reports them, a same-origin
// Origin and Sec-Fetch-Site.
func csrfProblem(r *http.Request) string {
	if r.Header.Get("X-Berth-Panel") != "1" {
		return "missing X-Berth-Panel header"
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return "Content-Type must be application/json"
	}
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

func (s *Server) forward(w http.ResponseWriter, r *http.Request, method, path string, body []byte) {
	req, err := http.NewRequestWithContext(r.Context(), method, s.broker+path, bytes.NewReader(body))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "broker_unavailable", "the broker is not reachable: "+err.Error())
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func writeError(w http.ResponseWriter, code int, errCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": errCode, "message": message}})
}
