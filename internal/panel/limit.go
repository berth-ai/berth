package panel

import (
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const (
	firstDelay = time.Second
	maxDelay   = 5 * time.Minute
	maxClients = 10000
)

type attempt struct {
	fails int
	until time.Time
}

// limiter backs off failed logins per client: 1 s after the first failure,
// doubling up to 5 min.
type limiter struct {
	mu      sync.Mutex
	now     func() time.Time
	clients map[string]*attempt
}

func newLimiter(now func() time.Time) *limiter {
	return &limiter{now: now, clients: map[string]*attempt{}}
}

// wait returns how long client must still wait; 0 means it may try.
func (l *limiter) wait(client string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.clients[client]
	if a == nil {
		return 0
	}
	return max(0, a.until.Sub(l.now()))
}

// fail records a failure and returns the new delay.
func (l *limiter) fail(client string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.clients) >= maxClients {
		for k, a := range l.clients {
			if now.After(a.until.Add(maxDelay)) {
				delete(l.clients, k)
			}
		}
	}
	a := l.clients[client]
	if a == nil {
		a = &attempt{}
		l.clients[client] = a
	}
	a.fails++
	d := firstDelay
	for i := 1; i < a.fails && d < maxDelay; i++ {
		d *= 2
	}
	d = min(d, maxDelay)
	a.until = now.Add(d)
	return d
}

func (l *limiter) succeed(client string) {
	l.mu.Lock()
	delete(l.clients, client)
	l.mu.Unlock()
}

// clientAddr identifies the browser. Behind a reverse proxy on a loopback or
// private address (including Docker's port forwarder), that is the rightmost
// X-Forwarded-For entry. A client on a private LAN can forge it, which is why
// the broker also spaces out every password check.
func clientAddr(r *http.Request) string {
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	peer := ap.Addr().Unmap()
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" && (peer.IsLoopback() || peer.IsPrivate()) {
		parts := strings.Split(xff, ",")
		if a, err := netip.ParseAddr(strings.TrimSpace(parts[len(parts)-1])); err == nil {
			return a.Unmap().String()
		}
	}
	return peer.String()
}
