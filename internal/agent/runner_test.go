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

func TestRunnerDefaultAllowsMoreThanTwentyToolLoops(t *testing.T) {
	store, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	llm := &loopingToolLLM{toolLoops: 21}
	runner := NewRunner(llm, NewToolExecutor(store))

	result, records, err := runner.RunWithCallback(context.Background(), "system", []Tool{ToolReadWikiIndex}, "user", nil)
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
