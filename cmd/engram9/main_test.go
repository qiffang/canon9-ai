package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCLIFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunMigrateOKFCheckAndWrite(t *testing.T) {
	root := t.TempDir()
	writeCLIFile(t, root, "semantic/a.md", "<!-- compiled_from: evt_001 -->\n# A\n\nSee [[procedural/b]].\n")
	writeCLIFile(t, root, "procedural/b.md", `---
type: procedure
title: B
description: B page
timestamp: "2026-06-16T12:00:00Z"
memory_type: procedural
source_events: [evt_002]
trust_tier: T1
---
# B
`)

	if code := runMigrateOKF([]string{"--check", root}); code != 1 {
		t.Fatalf("check code=%d, want 1", code)
	}
	if code := runMigrateOKF([]string{"--write", "--backup=false", root}); code != 0 {
		t.Fatalf("write code=%d, want 0", code)
	}
	if code := runValidate([]string{"--strict", root}); code != 0 {
		t.Fatalf("validate after migration code=%d, want 0", code)
	}
	if code := runMigrateOKF([]string{"--check", root}); code != 0 {
		t.Fatalf("check after migration code=%d, want 0", code)
	}
}

func TestRunMigrateOKFRejectsWriteAndCheck(t *testing.T) {
	root := t.TempDir()
	if code := runMigrateOKF([]string{"--write", "--check", root}); code != 2 {
		t.Fatalf("code=%d, want 2", code)
	}
}
