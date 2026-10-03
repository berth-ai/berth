// Command devserver runs the panel against an in-process fake broker, for
// working on web/ and for the Playwright smoke test. It is never built into
// the image. Sign in as "dev" with the password "dev password"; in setup
// mode open /setup#token=dev-token.
package main

import (
	"flag"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"

	"github.com/berth-ai/berth/internal/panel"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:7691", "listen address")
	dist := flag.String("dist", "", "serve the frontend from this directory instead of the embedded build")
	mode := flag.String("mode", "normal", "broker mode: normal, setup or recovery")
	flag.Parse()
	broker := httptest.NewServer(newFake(*mode))
	defer broker.Close()
	// Nothing listens on port 1, so the terminal and IDE show the
	// "not running" page.
	down := &url.URL{Scheme: "http", Host: "127.0.0.1:1"}
	h := panel.New(broker.Client(), broker.URL, panel.Upstreams{Terminal: down, IDE: down})
	if *dist != "" {
		h.UseAssets(os.DirFS(*dist))
	}
	log.Printf("panel on http://%s (mode %s)", *addr, *mode)
	log.Fatal(http.ListenAndServe(*addr, h))
}
