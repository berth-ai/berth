package web

import (
	"io/fs"
	"testing"
)

func TestDistIsEmbedded(t *testing.T) {
	if _, err := fs.Stat(Dist, "dist/.gitkeep"); err != nil {
		t.Fatalf("dist/.gitkeep is not embedded: %v", err)
	}
}
