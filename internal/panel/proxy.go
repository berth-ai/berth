package panel

import (
	"net/http"
	"net/url"
)

func newProxy(string, *url.URL, string) http.Handler { return http.NotFoundHandler() }
