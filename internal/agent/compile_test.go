package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/qiffang/engram9/internal/storage"
)

// toolUseLLM is a mock LLM that returns tool_use calls followed by a final text response.
// Each call alternates: first returns toolCalls, second returns final text.
type toolUseLLM struct {
	calls     []ContentBlock // tool_use blocks to return on the first call
	callCount int
}

func (m *toolUseLLM) Call(_ context.Context, req LLMRequest) (*LLMResponse, error) {
	m.callCount++
	if m.callCount == 1 {
		return &LLMResponse{
			Content:    m.calls,
			StopReason: "tool_use",
		}, nil
	}
	return &LLMResponse{
		Content:    []ContentBlock{{Type: "text", Text: "Compile done."}},
		StopReason: "end_turn",
	}, nil
}

type repeatedToolUseLLM struct {
	toolRounds int
	callCount  int
}

func (m *repeatedToolUseLLM) Call(_ context.Context, req LLMRequest) (*LLMResponse, error) {
	m.callCount++
	if m.callCount <= m.toolRounds {
		return &LLMResponse{
			Content: []ContentBlock{
				{
					Type:  "tool_use",
					ID:    fmt.Sprintf("call_%d", m.callCount),
					Name:  "read_events_since",
					Input: json.RawMessage(`{"cursor": 0}`),
				},
			},
			StopReason: "tool_use",
		}, nil
	}
	return &LLMResponse{
		Content:    []ContentBlock{{Type: "text", Text: "Compile done after many loops."}},
		StopReason: "end_turn",
	}, nil
}

func TestCompileAllowsConfiguredToolLoopBudget(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = store.AppendEvent(storage.Event{
		Content:    "event",
		Durability: "long-term",
		SourceType: "user",
		TrustTier:  1,
	})

	executor := NewToolExecutor(store)
	llm := &repeatedToolUseLLM{toolRounds: defaultMaxToolLoops + 1}
	agent := NewCompileAgentWithMaxToolLoops(llm, executor, defaultMaxToolLoops+2)

	result, newCursor, err := agent.Compile(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if result != "Compile done after many loops." {
		t.Fatalf("unexpected result: %q", result)
	}
	if newCursor != 1 {
		t.Fatalf("cursor=%d, want 1", newCursor)
	}
}

func TestCompileCursorRejectsWrongInput(t *testing.T) {
	// Setup: store with 5 events, compile cursor starts at 3.
	dir := t.TempDir()
	store, err := storage.NewFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		_, _ = store.AppendEvent(storage.Event{
			Content:    fmt.Sprintf("event %d", i),
			Durability: "long-term",
			SourceType: "user",
			TrustTier:  1,
		})
	}

	executor := NewToolExecutor(store)

	// Mock LLM that calls read_events_since with cursor=999 (wrong).
	// The tool will still return new_cursor=5, but compile should reject it
	// because 999 != startCursor (3).
	llm := &toolUseLLM{
		calls: []ContentBlock{
			{
				Type:  "tool_use",
				ID:    "call_1",
				Name:  "read_events_since",
				Input: json.RawMessage(`{"cursor": 999}`),
			},
		},
	}

	agent := NewCompileAgent(llm, executor)
	_, newCursor, err := agent.Compile(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}

	// Cursor must NOT advance because the tool was called with wrong cursor.
	if newCursor != 3 {
		t.Errorf("cursor advanced to %d, want 3 (should not advance on wrong input cursor)", newCursor)
	}
}

func TestCompileCursorAcceptsCorrectInput(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		_, _ = store.AppendEvent(storage.Event{
			Content:    fmt.Sprintf("event %d", i),
			Durability: "long-term",
			SourceType: "user",
			TrustTier:  1,
		})
	}

	executor := NewToolExecutor(store)

	// Mock LLM that calls read_events_since with the correct cursor=3.
	llm := &toolUseLLM{
		calls: []ContentBlock{
			{
				Type:  "tool_use",
				ID:    "call_1",
				Name:  "read_events_since",
				Input: json.RawMessage(`{"cursor": 3}`),
			},
		},
	}

	agent := NewCompileAgent(llm, executor)
	_, newCursor, err := agent.Compile(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}

	// Cursor should advance to 5 (total events).
	if newCursor != 5 {
		t.Errorf("cursor=%d, want 5", newCursor)
	}
}

func TestCompileCursorIgnoresRetryAfterCorrectCall(t *testing.T) {
	// After the first correct read_events_since, subsequent calls should be ignored.
	dir := t.TempDir()
	store, err := storage.NewFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		_, _ = store.AppendEvent(storage.Event{
			Content:    fmt.Sprintf("event %d", i),
			Durability: "long-term",
			SourceType: "user",
			TrustTier:  1,
		})
	}

	executor := NewToolExecutor(store)

	// Mock LLM: first call returns two tool_use blocks —
	// correct cursor=5 (returns new_cursor=10), then a spurious cursor=0 (returns new_cursor=10).
	// The callback should only accept the first one.
	llm := &toolUseLLM{
		calls: []ContentBlock{
			{
				Type:  "tool_use",
				ID:    "call_1",
				Name:  "read_events_since",
				Input: json.RawMessage(`{"cursor": 5}`),
			},
			{
				Type:  "tool_use",
				ID:    "call_2",
				Name:  "read_events_since",
				Input: json.RawMessage(`{"cursor": 0}`),
			},
		},
	}

	agent := NewCompileAgent(llm, executor)
	_, newCursor, err := agent.Compile(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}

	// Should advance to 10 from the first (correct) call.
	if newCursor != 10 {
		t.Errorf("cursor=%d, want 10", newCursor)
	}
}
