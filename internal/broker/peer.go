package broker

import (
	"context"
	"net"
	"net/http"
)

type peerKey struct{}

// ConnContext records the connecting process's uid, so root-only verbs can
// tell root (berth config, via docker exec) from berth-panel. Use it as
// http.Server.ConnContext.
func ConnContext(ctx context.Context, c net.Conn) context.Context {
	if uid, ok := peerUID(c); ok {
		return context.WithValue(ctx, peerKey{}, uid)
	}
	return ctx
}

func peerFromContext(r *http.Request) (int, bool) {
	uid, ok := r.Context().Value(peerKey{}).(int)
	return uid, ok
}
