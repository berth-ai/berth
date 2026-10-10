package panel

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// upstream records requests and speaks a toy WebSocket: after a 101 it
// echoes bytes back.
type upstream struct {
	srv   *httptest.Server
	path  string
	query string
	hdr   http.Header
}

func newUpstream(t *testing.T) *upstream {
	u := &upstream{}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.path, u.query, u.hdr = r.URL.Path, r.URL.RawQuery, r.Header.Clone()
		if r.Header.Get("Upgrade") == "websocket" {
			conn, rw, _ := w.(http.Hijacker).Hijack()
			defer conn.Close()
			rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
			rw.Flush()
			buf := make([]byte, 4)
			n, _ := io.ReadFull(rw, buf)
			conn.Write(buf[:n])
			return
		}
		w.Header().Set("X-Frame-Options", "DENY")
		io.WriteString(w, "upstream "+r.URL.Path)
	}))
	t.Cleanup(u.srv.Close)
	return u
}

func proxyEnv(t *testing.T) (*env, *upstream, *upstream) {
	term, ide := newUpstream(t), newUpstream(t)
	e := newEnv(t, "normal")
	tu, _ := url.Parse(term.srv.URL)
	iu, _ := url.Parse(ide.srv.URL)
	e.s = New(e.s.client, e.s.broker, Upstreams{Terminal: tu, IDE: iu})
	e.s.modes.ttl = 0
	e.srv.Config.Handler = e.s
	return e, term, ide
}

func TestProxyNeedsSession(t *testing.T) {
	e, term, _ := proxyEnv(t)
	resp, _ := e.do(e.client(), "GET", "/terminal/", "", nil)
	if resp.StatusCode != 303 || resp.Header.Get("Location") != "/login" || term.path != "" {
		t.Errorf("anonymous = %d %s, upstream saw %q", resp.StatusCode, resp.Header.Get("Location"), term.path)
	}
}

func TestTerminalKeepsPathAndIDEStrips(t *testing.T) {
	e, term, ide := proxyEnv(t)
	c := e.client()
	e.login(c)
	resp, body := e.do(c, "GET", "/terminal/token", "", nil)
	if resp.StatusCode != 200 || body != "upstream /terminal/token" || term.path != "/terminal/token" {
		t.Errorf("terminal = %d %q (%q)", resp.StatusCode, body, term.path)
	}
	if got := resp.Header.Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Errorf("X-Frame-Options = %q", got)
	}
	if resp, _ := e.do(c, "GET", "/ide/static/x.js", "", nil); resp.StatusCode != 200 || ide.path != "/static/x.js" {
		t.Errorf("ide = %d %q", resp.StatusCode, ide.path)
	}
	if resp, _ := e.do(c, "GET", "/ide/", "", nil); ide.path != "/" {
		t.Errorf("ide root = %d %q", resp.StatusCode, ide.path)
	}
	if ide.hdr.Get("X-Forwarded-Proto") != "http" || ide.hdr.Get("X-Forwarded-Host") == "" {
		t.Errorf("forwarded headers = %v", ide.hdr)
	}
}

func TestProxyStripsSessionCookie(t *testing.T) {
	e, term, _ := proxyEnv(t)
	c := e.client()
	e.login(c)
	e.do(c, "GET", "/terminal/", "", map[string]string{"Cookie": "other=1"})
	cookie := term.hdr.Get("Cookie")
	if strings.Contains(cookie, "berth_session") {
		t.Errorf("upstream saw %q", cookie)
	}
}

func TestProxyUnavailable(t *testing.T) {
	e, term, _ := proxyEnv(t)
	e.s.UseAssets(fixtureAssets())
	term.srv.Close()
	c := e.client()
	e.login(c)
	resp, body := e.do(c, "GET", "/terminal/", "", nil)
	if resp.StatusCode != 502 || !strings.Contains(body, "The terminal is not running") {
		t.Errorf("down = %d %s", resp.StatusCode, body)
	}
	if resp.Header.Get("X-Frame-Options") != "SAMEORIGIN" {
		t.Errorf("headers = %v", resp.Header)
	}
}

func wsRequest(t *testing.T, e *env, c *http.Client, origin string) (string, net.Conn, *bufio.Reader) {
	t.Helper()
	u, _ := url.Parse(e.srv.URL)
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	var cookie string
	for _, ck := range c.Jar.Cookies(u) {
		cookie = ck.Name + "=" + ck.Value
	}
	req := "GET /terminal/ws HTTP/1.1\r\nHost: " + u.Host + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nCookie: " + cookie + "\r\n"
	if origin != "" {
		req += "Origin: " + origin + "\r\n"
	}
	conn.Write([]byte(req + "\r\n"))
	r := bufio.NewReader(conn)
	status, _ := r.ReadString('\n')
	return status, conn, r
}

func TestWebSocketThroughProxy(t *testing.T) {
	e, _, _ := proxyEnv(t)
	c := e.client()
	e.login(c)
	status, conn, r := wsRequest(t, e, c, e.srv.URL)
	defer conn.Close()
	if !strings.Contains(status, "101") {
		t.Fatalf("status = %q", status)
	}
	// Skip the rest of the 101 headers, then echo.
	for {
		line, _ := r.ReadString('\n')
		if line == "\r\n" || line == "" {
			break
		}
	}
	conn.Write([]byte("ping"))
	buf := make([]byte, 4)
	if _, err := io.ReadFull(r, buf); err != nil || string(buf) != "ping" {
		t.Errorf("echo = %q %v", buf, err)
	}
}

func TestWebSocketCrossOriginRejected(t *testing.T) {
	e, term, _ := proxyEnv(t)
	c := e.client()
	e.login(c)
	status, conn, _ := wsRequest(t, e, c, "http://evil.example")
	conn.Close()
	if !strings.Contains(status, "403") || term.path != "" {
		t.Errorf("status = %q, upstream saw %q", status, term.path)
	}
}

func TestTerminalForwardsOnlyAWorkspaceArg(t *testing.T) {
	e, term, ide := proxyEnv(t)
	c := e.client()
	e.login(c)
	for query, want := range map[string]string{
		"?arg=ws-acme-api":     "arg=ws-acme-api",
		"?arg=ws-acme-api&x=1": "arg=ws-acme-api",
		"?x=1&arg=ws-a":        "arg=ws-a",
		"?arg=main":            "",
		"?arg=ws-Bad":          "",
		"?arg=ws-a&arg=ws-b":   "",
		"?arg=ws-a%3Bid":       "",
		"?arg=-c&arg=ws-a":     "",
		"?foo=bar":             "",
		"":                     "",
	} {
		for _, p := range []string{"/terminal/", "/terminal/ws"} {
			term.query = "unset"
			e.do(c, "GET", p+query, "", nil)
			if term.query != want {
				t.Errorf("%s%s reached ttyd with %q, want %q", p, query, term.query, want)
			}
		}
	}
	e.do(c, "GET", "/ide/?folder=/workspaces/acme", "", nil)
	if ide.query != "folder=/workspaces/acme" {
		t.Errorf("the IDE's query changed: %q", ide.query)
	}
}
