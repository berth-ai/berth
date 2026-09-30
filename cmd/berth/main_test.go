package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBinaryVersionWithLdflags(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "berth")
	build := exec.Command("go", "build", "-o", bin,
		"-ldflags", "-X main.version=9.9.9 -X main.commit=feedbee -X main.defaultRepo=example/berth", ".")
	build.Env = append(build.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	out, err := exec.Command(bin, "version").Output()
	if err != nil || strings.TrimSpace(string(out)) != "berth 9.9.9 (feedbee)" {
		t.Fatalf("%v %q", err, out)
	}
	if err := exec.Command(bin, "nope").Run(); err == nil {
		t.Fatal("unknown command must fail")
	} else if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 2 {
		t.Fatalf("exit %v", err)
	}
}
