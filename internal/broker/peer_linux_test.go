//go:build linux

package broker

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestPeerUIDOverSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		c, err := net.Dial("unix", path)
		if err == nil {
			defer c.Close()
			c.Read(make([]byte, 1))
		}
	}()
	c, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx := ConnContext(context.Background(), c)
	if uid, ok := ctx.Value(peerKey{}).(int); !ok || uid != os.Getuid() {
		t.Errorf("peer uid = %v %v, want %d", uid, ok, os.Getuid())
	}
}
