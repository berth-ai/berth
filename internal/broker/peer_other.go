//go:build !linux

package broker

import "net"

// peerUID is unknown outside Linux, so root-only verbs refuse every caller.
func peerUID(net.Conn) (int, bool) { return -1, false }
