package agent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/qiffang/engram9/internal/storage"
	"github.com/stretchr/testify/require"
)

// mockStore implements storage.Store for testing readEventsSince truncation.
type mockStore struct {
	storage.Store
	events []storage.Event
}

func (m *mockStore) ReadEventsSince(cursor uint64) (*storage.EventsPage, error) {
	if cursor >= uint64(len(m.events)) {
		return &storage.EventsPage{NewCursor: uint64(len(m.events))}, nil
	}
	events := make([]storage.Event, len(m.events)-int(cursor))
	copy(events, m.events[cursor:])
	return &storage.EventsPage{Events: events, NewCursor: uint64(len(m.events))}, nil
}

func TestReadEventsSince_TruncatesLargeContent(t *testing.T) {
	largeContent := strings.Repeat("x", 50000) // 50KB > 2000 char limit
	smallContent := "short content"

	store := &mockStore{
		events: []storage.Event{
			{ID: "evt_1", Content: largeContent},
			{ID: "evt_2", Content: smallContent},
		},
	}
	te := NewToolExecutor(store)

	input, _ := json.Marshal(map[string]uint64{"cursor": 0})
	result, err := te.readEventsSince(input)
	require.NoError(t, err)

	var page storage.EventsPage
	require.NoError(t, json.Unmarshal([]byte(result), &page))
	require.Len(t, page.Events, 2)

	// Large content should be truncated
	require.True(t, len(page.Events[0].Content) < 3000,
		"expected truncated content, got len=%d", len(page.Events[0].Content))
	require.Contains(t, page.Events[0].Content, "[truncated for compile]")

	// Small content should be unchanged
	require.Equal(t, smallContent, page.Events[1].Content)

	// Event metadata preserved
	require.Equal(t, "evt_1", page.Events[0].ID)
	require.Equal(t, "evt_2", page.Events[1].ID)
}

func TestReadEventsSince_TotalSizeUnder10MB(t *testing.T) {
	// Simulate 48 events each with 300KB content (total ~14MB raw)
	// After truncation, should be well under 10MB
	var events []storage.Event
	bigContent := strings.Repeat("a", 300_000)
	for i := 0; i < 48; i++ {
		events = append(events, storage.Event{
			ID:      "evt_" + strings.Repeat("0", 4),
			Content: bigContent,
		})
	}

	store := &mockStore{events: events}
	te := NewToolExecutor(store)

	input, _ := json.Marshal(map[string]uint64{"cursor": 0})
	result, err := te.readEventsSince(input)
	require.NoError(t, err)

	// Total result must be under 10MB (OpenAI per-message limit)
	require.Less(t, len(result), 10*1024*1024,
		"result size %d exceeds 10MB limit", len(result))
}

func newTestStore(t *testing.T) storage.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.NewFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestWriteWikiPage_ValidPaths(t *testing.T) {
	te := NewToolExecutor(newTestStore(t))

	validPaths := []string{
		"semantic/test.md",
		"episodic/2026-07-14/event.md",
		"procedural/deploy.md",
		"prospective/goal.md",
		"index.md",
	}
	for _, path := range validPaths {
		input, _ := json.Marshal(map[string]string{
			"path":    path,
			"content": "# Test\n\nSome content.",
		})
		result, err := te.Execute("write_wiki_page", input)
		if err != nil {
			t.Errorf("valid path %q rejected: %v", path, err)
			continue
		}
		if !strings.Contains(result, `"status": "ok"`) {
			t.Errorf("valid path %q: unexpected result %s", path, result)
		}
	}
}

func TestWriteWikiPage_InvalidPaths(t *testing.T) {
	te := NewToolExecutor(newTestStore(t))

	invalidPaths := []string{
		"MEMORY.md",
		"notes.md",
		"archive/old.md",
		"config/settings.md",
		".meta/something",
	}
	for _, path := range invalidPaths {
		input, _ := json.Marshal(map[string]string{
			"path":    path,
			"content": "# Test",
		})
		_, err := te.Execute("write_wiki_page", input)
		if err == nil {
			t.Errorf("invalid path %q was accepted, should have been rejected", path)
			continue
		}
		if !strings.Contains(err.Error(), "invalid wiki path") {
			t.Errorf("invalid path %q: expected 'invalid wiki path' error, got: %v", path, err)
		}
	}
}

func TestWriteWikiPage_FrontmatterInjection(t *testing.T) {
	store := newTestStore(t)
	te := NewToolExecutor(store)

	input, _ := json.Marshal(map[string]string{
		"path":    "semantic/test.md",
		"content": "# Test\n\nSome content.",
	})
	_, err := te.Execute("write_wiki_page", input)
	if err != nil {
		t.Fatal(err)
	}

	page, err := store.ReadWikiPage("semantic/test.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page.Content, "<!-- compiled_from:") {
		t.Error("expected compiled_from frontmatter to be injected")
	}
	if !strings.Contains(page.Content, "<!-- last_compiled:") {
		t.Error("expected last_compiled frontmatter to be injected")
	}
}

func TestWriteWikiPage_ExistingFrontmatterPreserved(t *testing.T) {
	store := newTestStore(t)
	te := NewToolExecutor(store)

	content := "<!-- compiled_from: evt_001 -->\n<!-- last_compiled: 2026-07-14T00:00:00Z -->\n# Test\n\nExisting."
	input, _ := json.Marshal(map[string]string{
		"path":    "semantic/existing.md",
		"content": content,
	})
	_, err := te.Execute("write_wiki_page", input)
	if err != nil {
		t.Fatal(err)
	}

	page, err := store.ReadWikiPage("semantic/existing.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(page.Content, "<!-- compiled_from:") != 1 {
		t.Errorf("expected exactly 1 compiled_from, got %d", strings.Count(page.Content, "<!-- compiled_from:"))
	}
	if strings.Count(page.Content, "<!-- last_compiled:") != 1 {
		t.Errorf("expected exactly 1 last_compiled, got %d", strings.Count(page.Content, "<!-- last_compiled:"))
	}
	if !strings.Contains(page.Content, "evt_001") {
		t.Error("original compiled_from value should be preserved")
	}
}

func TestWriteWikiPage_SourceEventsInFrontmatter(t *testing.T) {
	store := newTestStore(t)
	te := NewToolExecutor(store)

	input, _ := json.Marshal(map[string]interface{}{
		"path":          "semantic/sourced.md",
		"content":       "# Sourced\n\nContent [evt_042 T1]",
		"source_events": []string{"evt_042", "evt_055"},
	})
	_, err := te.Execute("write_wiki_page", input)
	if err != nil {
		t.Fatal(err)
	}

	page, err := store.ReadWikiPage("semantic/sourced.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page.Content, "<!-- compiled_from: evt_042, evt_055 -->") {
		t.Errorf("expected source events in compiled_from, got: %s", page.Content)
	}
}

func TestEnsureFrontmatter(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)

	t.Run("injects both when missing", func(t *testing.T) {
		result := EnsureFrontmatter("# Title\n\nContent", nil, now)
		if !strings.HasPrefix(result, "<!-- compiled_from: ingest -->") {
			t.Errorf("expected compiled_from: ingest prefix, got: %s", result)
		}
		if !strings.Contains(result, "<!-- last_compiled: 2026-07-14T12:00:00Z -->") {
			t.Errorf("expected last_compiled timestamp, got: %s", result)
		}
	})

	t.Run("uses source events when provided", func(t *testing.T) {
		result := EnsureFrontmatter("# Title", []string{"evt_1", "evt_2"}, now)
		if !strings.Contains(result, "<!-- compiled_from: evt_1, evt_2 -->") {
			t.Errorf("expected source events, got: %s", result)
		}
	})

	t.Run("preserves existing frontmatter", func(t *testing.T) {
		content := "<!-- compiled_from: evt_old -->\n<!-- last_compiled: 2026-01-01T00:00:00Z -->\n# Title"
		result := EnsureFrontmatter(content, nil, now)
		if result != content {
			t.Errorf("expected content unchanged, got: %s", result)
		}
	})

	t.Run("injects only missing fields", func(t *testing.T) {
		content := "<!-- compiled_from: evt_old -->\n# Title"
		result := EnsureFrontmatter(content, nil, now)
		if !strings.Contains(result, "<!-- last_compiled: 2026-07-14T12:00:00Z -->") {
			t.Errorf("expected last_compiled injected, got: %s", result)
		}
		if strings.Count(result, "<!-- compiled_from:") != 1 {
			t.Error("compiled_from should not be duplicated")
		}
	})
}
