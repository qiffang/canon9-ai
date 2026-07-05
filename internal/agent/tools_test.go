package agent

import (
	"encoding/json"
	"strings"
	"testing"

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
