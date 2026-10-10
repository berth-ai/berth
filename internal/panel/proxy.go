package panel

import (
	"fmt"
	"html"
	"io"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
)

// newProxy forwards to target, HTTP and WebSocket alike. strip removes a
// path prefix the upstream does not expect; query, when set, rewrites the
// query string. Upstreams never see the panel's session cookie. Their
// pages may be framed by the panel and by nothing else. down answers when
// the upstream is not reachable.
func newProxy(name string, target *url.URL, strip string, query func(url.Values) url.Values, down func(http.ResponseWriter, string)) http.Handler {
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			if strip != "" {
				pr.Out.URL.Path = "/" + strings.TrimPrefix(strings.TrimPrefix(pr.In.URL.Path, strip), "/")
				pr.Out.URL.RawPath = ""
			}
			if query != nil {
				pr.Out.URL.RawQuery = query(pr.In.URL.Query()).Encode()
			}
			dropCookie(pr.Out.Header, cookieName)
			pr.SetXForwarded()
		},
		ModifyResponse: func(resp *http.Response) error {
			setProxiedHeaders(resp.Header)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			down(w, name)
		},
	}
}

// terminalArg is the only query ttyd gets: ttyd -a passes arg to
// berth-term, which accepts a workspace session and nothing else.
var terminalArg = regexp.MustCompile(`^ws-[a-z0-9][a-z0-9-]{0,63}$`)

// terminalQuery keeps a single valid arg and drops everything else.
func terminalQuery(q url.Values) url.Values {
	if a := q["arg"]; len(a) == 1 && terminalArg.MatchString(a[0]) {
		return url.Values{"arg": a}
	}
	return url.Values{}
}

func setProxiedHeaders(h http.Header) {
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
}

func dropCookie(h http.Header, name string) {
	cookies := (&http.Request{Header: h}).Cookies()
	h.Del("Cookie")
	var keep []string
	for _, c := range cookies {
		if c.Name != name {
			keep = append(keep, c.Name+"="+c.Value)
		}
	}
	if len(keep) > 0 {
		h.Set("Cookie", strings.Join(keep, "; "))
	}
}

func (s *Server) unavailable(w http.ResponseWriter, name string) {
	h := w.Header()
	setProxiedHeaders(h)
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'self'; base-uri 'none'")
	h.Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadGateway)
	page, err := fs.ReadFile(s.assets, "unavailable.html")
	if err != nil {
		_, _ = fmt.Fprintf(w, "The %s is not running.", html.EscapeString(name))
		return
	}
	// ReplaceAll, not fmt: a built page may contain other % signs.
	_, _ = io.WriteString(w, strings.ReplaceAll(string(page), "%[1]s", html.EscapeString(name)))
}
