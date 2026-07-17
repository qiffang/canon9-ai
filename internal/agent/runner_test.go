package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/qiffang/engram9/internal/storage"
)

type loopingToolLLM struct {
	toolLoops int
	calls     int
}

func (m *loopingToolLLM) Call(_ context.Context, _ LLMRequest) (*LLMResponse, error) {
	m.calls++
	if m.calls <= m.toolLoops {
		return &LLMResponse{
			Content: []ContentBlock{{
				Type:  "tool_use",
				ID:    fmt.Sprintf("call_%d", m.calls),
				Name:  "read_wiki_index",
				Input: json.RawMessage(`{}`),
			}},
			StopReason: "tool_use",
		}, nil
	}
	return &LLMResponse{
		Content:    []ContentBlock{{Type: "text", Text: "done"}},
		StopReason: "end_turn",
	}, nil
}

type uniqueReadToolLLM struct {
	toolLoops int
	calls     int
}

func (m *uniqueReadToolLLM) Call(_ context.Context, _ LLMRequest) (*LLMResponse, error) {
	m.calls++
	if m.calls <= m.toolLoops {
		return &LLMResponse{
			Content: []ContentBlock{{
				Type:  "tool_use",
				ID:    fmt.Sprintf("call_%d", m.calls),
				Name:  "read_wiki_page",
				Input: json.RawMessage(fmt.Sprintf(`{"path":"missing-%d.md"}`, m.calls)),
			}},
			StopReason: "tool_use",
		}, nil
	}
	return &LLMResponse{
		Content:    []ContentBlock{{Type: "text", Text: "done"}},
		StopReason: "end_turn",
	}, nil
}

type invalidToolLLM struct{}

func (m *invalidToolLLM) Call(_ context.Context, _ LLMRequest) (*LLMResponse, error) {
	return &LLMResponse{
		Content: []ContentBlock{{
			Type:  "tool_use",
			ID:    "call_invalid",
			Name:  "调用的函数名称",
			Input: json.RawMessage(`{}`),
		}},
		StopReason: "tool_use",
	}, nil
}

type invalidThenValidToolLLM struct {
	calls            int
	sawInvalidResult bool
}

func (m *invalidThenValidToolLLM) Call(_ context.Context, req LLMRequest) (*LLMResponse, error) {
	m.calls++
	switch m.calls {
	case 1:
		return &LLMResponse{
			Content: []ContentBlock{{
				Type:  "tool_use",
				ID:    "call_invalid",
				Name:  "调用的函数名称",
				Input: json.RawMessage(`{}`),
			}},
			StopReason: "tool_use",
		}, nil
	case 2:
		if len(req.Messages) == 0 {
			return nil, fmt.Errorf("missing invalid tool result message")
		}
		blocks, ok := req.Messages[len(req.Messages)-1].Content.([]ContentBlock)
		if !ok || len(blocks) != 1 {
			return nil, fmt.Errorf("last message content = %#v, want one tool result", req.Messages[len(req.Messages)-1].Content)
		}
		result := blocks[0]
		if result.Type != "tool_result" || result.ToolUseID != "call_invalid" || !result.IsError ||
			!strings.Contains(result.Content, "invalid tool call") {
			return nil, fmt.Errorf("invalid tool result block = %#v", result)
		}
		m.sawInvalidResult = true
		return &LLMResponse{
			Content: []ContentBlock{{
				Type:  "tool_use",
				ID:    "call_valid",
				Name:  "read_wiki_index",
				Input: json.RawMessage(`{}`),
			}},
			StopReason: "tool_use",
		}, nil
	default:
		return &LLMResponse{
			Content:    []ContentBlock{{Type: "text", Text: "done"}},
			StopReason: "end_turn",
		}, nil
	}
}

type namedToolLoopLLM struct {
	toolName  string
	toolLoops int
	calls     int
}

func (m *namedToolLoopLLM) Call(_ context.Context, _ LLMRequest) (*LLMResponse, error) {
	m.calls++
	if m.calls <= m.toolLoops {
		return &LLMResponse{
			Content: []ContentBlock{{
				Type:  "tool_use",
				ID:    fmt.Sprintf("call_%d", m.calls),
				Name:  m.toolName,
				Input: json.RawMessage(`{}`),
			}},
			StopReason: "tool_use",
		}, nil
	}
	return &LLMResponse{
		Content:    []ContentBlock{{Type: "text", Text: "done"}},
		StopReason: "end_turn",
	}, nil
}

type sequenceToolLLM struct {
	calls []ContentBlock
	next  int
}

func (m *sequenceToolLLM) Call(_ context.Context, _ LLMRequest) (*LLMResponse, error) {
	if m.next < len(m.calls) {
		block := m.calls[m.next]
		m.next++
		return &LLMResponse{
			Content:    []ContentBlock{block},
			StopReason: "tool_use",
		}, nil
	}
	return &LLMResponse{
		Content:    []ContentBlock{{Type: "text", Text: "done"}},
		StopReason: "end_turn",
	}, nil
}

func TestRunnerDefaultAllowsMoreThanTwentyToolLoops(t *testing.T) {
	store, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	llm := &uniqueReadToolLLM{toolLoops: 21}
	runner := NewRunner(llm, NewToolExecutor(store))

	result, records, err := runner.RunWithCallback(context.Background(), "system", []Tool{ToolReadWikiPage}, "user", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != "done" {
		t.Fatalf("result=%q, want done", result)
	}
	if len(records) != 21 {
		t.Fatalf("tool records=%d, want 21", len(records))
	}
}

func TestRunnerCustomToolLoopCapFailsAtConfiguredLimit(t *testing.T) {
	store, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	llm := &loopingToolLLM{toolLoops: 3}
	runner := NewRunnerWithOptions(llm, NewToolExecutor(store), RunnerOptions{MaxToolLoops: 2})

	_, records, err := runner.RunWithCallback(context.Background(), "system", []Tool{ToolReadWikiIndex}, "user", nil)
	if err == nil {
		t.Fatal("expected tool loop limit error")
	}
	if !strings.Contains(err.Error(), "exceeded max tool loops (2)") {
		t.Fatalf("error=%q, want configured loop limit", err.Error())
	}
	if len(records) != 2 {
		t.Fatalf("tool records=%d, want 2", len(records))
	}
}

func TestRunnerFeedsBackSingleInvalidToolCall(t *testing.T) {
	store, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	llm := &invalidThenValidToolLLM{}
	runner := NewRunner(llm, NewToolExecutor(store))

	result, records, err := runner.RunWithCallback(context.Background(), "system", []Tool{ToolReadWikiIndex}, "user", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != "done" {
		t.Fatalf("result=%q, want done", result)
	}
	if !llm.sawInvalidResult {
		t.Fatal("LLM did not receive invalid tool error result")
	}
	if len(records) != 2 {
		t.Fatalf("tool records=%d, want 2", len(records))
	}
	if records[0].Err == nil || !strings.Contains(records[0].Err.Error(), "invalid tool call") {
		t.Fatalf("first record err=%v, want invalid tool call", records[0].Err)
	}
	if records[1].Err != nil || records[1].Name != "read_wiki_index" {
		t.Fatalf("second record=%+v, want successful read_wiki_index", records[1])
	}
}

func TestRunnerStopsConsecutiveInvalidToolCalls(t *testing.T) {
	store, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunnerWithOptions(&invalidToolLLM{}, NewToolExecutor(store), RunnerOptions{
		MaxInvalidToolCalls: 2,
	})

	_, records, err := runner.RunWithCallback(context.Background(), "system", []Tool{ToolReadWikiIndex}, "user", nil)
	if err == nil {
		t.Fatal("expected invalid tool limit error")
	}
	if !strings.Contains(err.Error(), "invalid tool call exceeded limit (2)") {
		t.Fatalf("error=%q, want invalid tool limit", err.Error())
	}
	if !strings.Contains(err.Error(), "调用的函数名称") {
		t.Fatalf("error=%q, want invalid tool name", err.Error())
	}
	if len(records) != 3 {
		t.Fatalf("tool records=%d, want 3", len(records))
	}
}

func TestRunnerStopsRepeatedReadOnlyToolCall(t *testing.T) {
	store, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	llm := &loopingToolLLM{toolLoops: 4}
	runner := NewRunnerWithOptions(llm, NewToolExecutor(store), RunnerOptions{
		MaxRepeatedReadOnlyToolCalls: 3,
	})

	_, records, err := runner.RunWithCallback(context.Background(), "system", []Tool{ToolReadWikiIndex}, "user", nil)
	if err == nil {
		t.Fatal("expected repeated read-only tool call error")
	}
	if !strings.Contains(err.Error(), "repeated read-only tool call") {
		t.Fatalf("error=%q, want repeated read-only guard", err.Error())
	}
	if !strings.Contains(err.Error(), "read_wiki_index") {
		t.Fatalf("error=%q, want tool name", err.Error())
	}
	if len(records) != 4 {
		t.Fatalf("tool records=%d, want 4", len(records))
	}
}

func TestRunnerReadOnlyRepeatGuardResetsAfterSuccessfulWrite(t *testing.T) {
	store, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	llm := &sequenceToolLLM{calls: []ContentBlock{
		{Type: "tool_use", ID: "read_1", Name: "read_wiki_index", Input: json.RawMessage(`{}`)},
		{Type: "tool_use", ID: "read_2", Name: "read_wiki_index", Input: json.RawMessage(`{}`)},
		{Type: "tool_use", ID: "write_1", Name: "write_wiki_page", Input: json.RawMessage(`{"path":"semantic/notes.md","content":"progress"}`)},
		{Type: "tool_use", ID: "read_3", Name: "read_wiki_index", Input: json.RawMessage(`{}`)},
		{Type: "tool_use", ID: "read_4", Name: "read_wiki_index", Input: json.RawMessage(`{}`)},
	}}
	runner := NewRunnerWithOptions(llm, NewToolExecutor(store), RunnerOptions{
		MaxRepeatedReadOnlyToolCalls: 2,
	})

	result, records, err := runner.RunWithCallback(context.Background(), "system", []Tool{
		ToolReadWikiIndex,
		ToolWriteWikiPage,
	}, "user", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != "done" {
		t.Fatalf("result=%q, want done", result)
	}
	if len(records) != 5 {
		t.Fatalf("tool records=%d, want 5", len(records))
	}
	for i, record := range records {
		if record.Err != nil {
			t.Fatalf("record %d err=%v", i, record.Err)
		}
	}
}

func TestRunnerUsesToolMetadataForReadOnlyGuard(t *testing.T) {
	store, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	llm := &namedToolLoopLLM{toolName: "custom_read", toolLoops: 3}
	runner := NewRunnerWithOptions(llm, NewToolExecutor(store), RunnerOptions{
		MaxRepeatedReadOnlyToolCalls: 2,
	})
	tools := []Tool{{
		Name:        "custom_read",
		Description: "custom read-only test tool",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		ReadOnly:    true,
	}}

	_, records, err := runner.RunWithCallback(context.Background(), "system", tools, "user", nil)
	if err == nil {
		t.Fatal("expected repeated read-only tool call error")
	}
	if !strings.Contains(err.Error(), "repeated read-only tool call") {
		t.Fatalf("error=%q, want repeated read-only guard", err.Error())
	}
	if !strings.Contains(err.Error(), "custom_read") {
		t.Fatalf("error=%q, want tool name", err.Error())
	}
	if len(records) != 3 {
		t.Fatalf("tool records=%d, want 3", len(records))
	}
}
