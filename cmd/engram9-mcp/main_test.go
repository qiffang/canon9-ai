package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBundleAgentModeRejected(t *testing.T) {
	// Build the binary into a temp dir.
	bin := filepath.Join(t.TempDir(), "engram9-mcp")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	bundleDir := t.TempDir()
	os.MkdirAll(filepath.Join(bundleDir, "semantic"), 0o755)

	cmd := exec.Command(bin, "-bundle", bundleDir, "-mode", "agent")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected non-zero exit for -bundle -mode agent")
	}
	if !strings.Contains(string(out), "read-only") {
		t.Errorf("expected 'read-only' in error output, got: %s", out)
	}
}
