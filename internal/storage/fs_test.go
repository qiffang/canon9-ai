package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestFS(t *testing.T) *FS {
	t.Helper()
	dir := t.TempDir()
	fs, err := NewFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func TestAppendAndReadEvents(t *testing.T) {
	fs := newTestFS(t)

	id, err := fs.AppendEvent(Event{
		Content:    "Alice suggests partition tables",
		Actor:      "user",
		Durability: "long-term",
		SourceType: "user",
		TrustTier:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("expected non-empty event ID")
	}

	page, err := fs.ReadEventsSince(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(page.Events))
	}
	if page.Events[0].Content != "Alice suggests partition tables" {
		t.Errorf("unexpected content: %s", page.Events[0].Content)
	}
	if page.NewCursor != 1 {
		t.Errorf("expected cursor=1, got %d", page.NewCursor)
	}

	// Reading from cursor=1 should return no events.
	page2, err := fs.ReadEventsSince(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Events) != 0 {
		t.Fatalf("expected 0 events from cursor=1, got %d", len(page2.Events))
	}
}

func TestWriteAndReadWikiPage(t *testing.T) {
	fs := newTestFS(t)

	err := fs.WriteWikiPage("semantic/projects/db9.md", "# DB9\n\nPartition tables.\n")
	if err != nil {
		t.Fatal(err)
	}

	page, err := fs.ReadWikiPage("semantic/projects/db9.md")
	if err != nil {
		t.Fatal(err)
	}
	if page.Content != "# DB9\n\nPartition tables.\n" {
		t.Errorf("unexpected content: %q", page.Content)
	}
	if page.Meta == nil {
		t.Fatal("expected non-nil meta")
	}
	if page.Meta.MemoryType != "semantic" {
		t.Errorf("expected memory_type=semantic, got %q", page.Meta.MemoryType)
	}
	if page.Meta.CreatedAt == "" {
		t.Error("expected non-empty created_at")
	}
	if len(page.Meta.AccessDates) != 1 {
		t.Errorf("expected 1 access date, got %d", len(page.Meta.AccessDates))
	}
}

func TestSearchWiki(t *testing.T) {
	fs := newTestFS(t)

	_ = fs.WriteWikiPage("semantic/projects/db9.md", "# DB9\n\nAlice suggests partition tables.\n")
	_ = fs.WriteWikiPage("semantic/people/alice.md", "# Alice\n\nSenior engineer.\n")

	results, err := fs.SearchWiki("alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestArchiveWikiPage(t *testing.T) {
	fs := newTestFS(t)

	_ = fs.WriteWikiPage("episodic/2026-04-12/meeting.md", "# Meeting\n\nDiscussed schema.\n")

	err := fs.ArchiveWikiPage("episodic/2026-04-12/meeting.md", "distilled to semantic")
	if err != nil {
		t.Fatal(err)
	}

	// Original should not exist.
	_, err = fs.ReadWikiPage("episodic/2026-04-12/meeting.md")
	if err == nil {
		t.Fatal("expected error reading archived page")
	}

	// Archive should exist.
	archivePath := filepath.Join(fs.wikiDir(), "archive", "episodic", "2026-04-12", "meeting.md")
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("archive file should exist: %v", err)
	}
}

func TestRebuildIndex(t *testing.T) {
	fs := newTestFS(t)

	_ = fs.WriteWikiPage("semantic/projects/db9.md", "# DB9\n\nPartition tables are great.\n")
	_ = fs.WriteWikiPage("procedural/deploy-drive9.md", "# Deploy Drive9\n\nStep 1: build.\n")

	err := fs.RebuildIndex()
	if err != nil {
		t.Fatal(err)
	}

	idx, err := fs.ReadWikiIndex()
	if err != nil {
		t.Fatal(err)
	}
	if idx == "" {
		t.Fatal("expected non-empty index")
	}

	// Index should mention both pages.
	if !containsStr(idx, "db9.md") {
		t.Error("index should contain db9.md")
	}
	if !containsStr(idx, "deploy-drive9.md") {
		t.Error("index should contain deploy-drive9.md")
	}
}

func TestGetMemoryStats(t *testing.T) {
	fs := newTestFS(t)

	_, _ = fs.AppendEvent(Event{Content: "test1", Durability: "long-term", SourceType: "user", TrustTier: 1})
	_, _ = fs.AppendEvent(Event{Content: "test2", Durability: "long-term", SourceType: "user", TrustTier: 1})
	_ = fs.WriteWikiPage("semantic/test.md", "# Test\n")

	stats, err := fs.GetMemoryStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.EventCount != 2 {
		t.Errorf("expected 2 events, got %d", stats.EventCount)
	}
	if stats.WikiPageCount != 1 {
		t.Errorf("expected 1 wiki page, got %d", stats.WikiPageCount)
	}
}

func TestValidateWikiPath(t *testing.T) {
	tests := []struct {
		path    string
		wantErr bool
	}{
		{"semantic/db9.md", false},
		{"", true},
		{"../etc/passwd", true},
		{"/absolute/path.md", true},
		{".meta/secret.json", true},
	}
	for _, tt := range tests {
		err := validateWikiPath(tt.path)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateWikiPath(%q) err=%v, wantErr=%v", tt.path, err, tt.wantErr)
		}
	}
}

func TestEventPersistence(t *testing.T) {
	dir := t.TempDir()

	// Write events.
	fs1, err := NewFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fs1.AppendEvent(Event{Content: "persisted", Durability: "long-term", SourceType: "user", TrustTier: 1})

	// Reload from disk.
	fs2, err := NewFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	page, err := fs2.ReadEventsSince(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].Content != "persisted" {
		t.Fatalf("expected persisted event, got %+v", page.Events)
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstr(s, substr))
}

func findSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
