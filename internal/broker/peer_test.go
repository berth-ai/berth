package broker

import (
	"net/http"
	"testing"
)

func TestRootOnlyVerbRefusesOthers(t *testing.T) {
	tb := newTestBroker(t)
	tb.b.Load()
	for name, peer := range map[string]func(*http.Request) (int, bool){
		"unknown peer": func(*http.Request) (int, bool) { return -1, false },
		"berth-panel":  func(*http.Request) (int, bool) { return 990, true },
	} {
		tb.b.PeerUID = peer
		if code, out := tb.verb(t, "auth.reset", `{}`); code != http.StatusForbidden || errCode(out) != "forbidden" {
			t.Errorf("%s: %d %v", name, code, out)
		}
	}
}
