package panel

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// newProxy forwards to target, HTTP and WebSocket alike. strip removes a
// path prefix the upstream does not expect. Upstreams never see the panel's
// session cookie. Their pages may be framed by the panel and by nothing else.
func newProxy(name string, target *url.URL, strip string) http.Handler {
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			if strip != "" {
				pr.Out.URL.Path = "/" + strings.TrimPrefix(strings.TrimPrefix(pr.In.URL.Path, strip), "/")
				pr.Out.URL.RawPath = ""
			}
			dropCookie(pr.Out.Header, cookieName)
			pr.SetXForwarded()
		},
		ModifyResponse: func(resp *http.Response) error {
			setProxiedHeaders(resp.Header)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			unavailable(w, name)
		},
	}
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

const unavailablePage = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Berth</title>
<link rel="stylesheet" href="/static/panel.css"></head>
<body class="unavailable"><main>
<h1>The %[1]s is not running</h1>
<p>It may be switched off. Turn it on in <a href="/#settings" target="_top">Settings</a>,
or read its log on the <a href="/" target="_top">overview</a>.</p>
</main></body></html>
`

func unavailable(w http.ResponseWriter, name string) {
	h := w.Header()
	setProxiedHeaders(h)
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'self'; base-uri 'none'")
	h.Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadGateway)
	fmt.Fprintf(w, unavailablePage, html.EscapeString(name))
}
