package panel

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const (
	cookieName  = "berth_session"
	sessionIdle = 12 * time.Hour
	sessionMax  = 7 * 24 * time.Hour
)

type session struct {
	user    string
	created time.Time
	seen    time.Time
}

// sessions live in memory: a panel restart signs everyone out.
type sessions struct {
	mu   sync.Mutex
	now  func() time.Time
	byID map[string]*session
}

func newSessions(now func() time.Time) *sessions {
	return &sessions{now: now, byID: map[string]*session{}}
}

func (s *sessions) create(user string) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	id := base64.RawURLEncoding.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, v := range s.byID { // drop expired sessions as new ones arrive
		if s.expired(v, now) {
			delete(s.byID, k)
		}
	}
	s.byID[id] = &session{user: user, created: now, seen: now}
	return id
}

// get returns the request's live session and marks it used, or nil.
func (s *sessions) get(r *http.Request) *session {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.byID[c.Value]
	if sess == nil {
		return nil
	}
	now := s.now()
	if s.expired(sess, now) {
		delete(s.byID, c.Value)
		return nil
	}
	sess.seen = now
	copied := *sess
	return &copied
}

func (s *sessions) expired(sess *session, now time.Time) bool {
	return now.Sub(sess.seen) > sessionIdle || now.Sub(sess.created) > sessionMax
}

func (s *sessions) delete(r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.mu.Lock()
		delete(s.byID, c.Value)
		s.mu.Unlock()
	}
}

// reset signs everyone out, after a login change.
func (s *sessions) reset() {
	s.mu.Lock()
	s.byID = map[string]*session{}
	s.mu.Unlock()
}

func sessionCookie(r *http.Request, value string, maxAge int) *http.Cookie {
	return &http.Cookie{Name: cookieName, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: overTLS(r)}
}

// overTLS reports whether the browser used HTTPS: directly, or through a
// reverse proxy on a loopback or private address that says so.
func overTLS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") && trustedPeer(r)
}

func trustedPeer(r *http.Request) bool {
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	a := ap.Addr().Unmap()
	return a.IsLoopback() || a.IsPrivate()
}
