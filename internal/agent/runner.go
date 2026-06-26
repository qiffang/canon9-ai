package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
)

const defaultMaxToolLoops = 20

// ToolCallback is called after each tool execution with the tool name and result.
type ToolCallback func(name string, input json.RawMessage, result string, err error)

// Runner drives an agentic tool-use loop: send messages to LLM, execute tool calls,
// feed results back, repeat until the LLM produces a final text response.
type Runner struct {
	llm          LLM
	executor     *ToolExecutor
	maxToolLoops int
}

func NewRunner(llm LLM, executor *ToolExecutor) *Runner {
	return NewRunnerWithMaxToolLoops(llm, executor, defaultMaxToolLoops)
}

func NewRunnerWithMaxToolLoops(llm LLM, executor *ToolExecutor, maxToolLoops int) *Runner {
	if maxToolLoops <= 0 {
		maxToolLoops = defaultMaxToolLoops
	}
	return &Runner{llm: llm, executor: executor, maxToolLoops: maxToolLoops}
}

// Run executes an agent with the given system prompt, tools, and user message.
// Returns the final text response from the LLM.
func (r *Runner) Run(ctx context.Context, system string, tools []Tool, userMessage string) (string, error) {
	result, _, err := r.RunWithCallback(ctx, system, tools, userMessage, nil)
	return result, err
}

// RunWithCallback is like Run but invokes cb after each tool execution,
// allowing callers to extract structured data from tool results.
func (r *Runner) RunWithCallback(ctx context.Context, system string, tools []Tool, userMessage string, cb ToolCallback) (string, []ToolCallRecord, error) {
	messages := []Message{
		{Role: "user", Content: userMessage},
	}

	var records []ToolCallRecord

	for i := 0; i < r.maxToolLoops; i++ {
		resp, err := r.llm.Call(ctx, LLMRequest{
			System:   system,
			Messages: messages,
			Tools:    tools,
		})
		if err != nil {
			return "", records, fmt.Errorf("llm call: %w", err)
		}

		var textParts []string
		var toolCalls []ContentBlock

		for _, block := range resp.Content {
			switch block.Type {
			case "text":
				textParts = append(textParts, block.Text)
			case "tool_use":
				toolCalls = append(toolCalls, block)
			}
		}

		if resp.StopReason != "tool_use" || len(toolCalls) == 0 {
			result := ""
			for _, t := range textParts {
				result += t
			}
			return result, records, nil
		}

		messages = append(messages, Message{
			Role:    "assistant",
			Content: resp.Content,
		})

		var toolResults []ContentBlock
		for _, tc := range toolCalls {
			log.Printf("[agent] tool_use: %s", tc.Name)
			result, execErr := r.executor.Execute(tc.Name, tc.Input)

			rec := ToolCallRecord{Name: tc.Name, Input: tc.Input, Result: result, Err: execErr}
			records = append(records, rec)

			if cb != nil {
				cb(tc.Name, tc.Input, result, execErr)
			}

			block := ContentBlock{
				Type:      "tool_result",
				ToolUseID: tc.ID,
			}
			if execErr != nil {
				block.Content = fmt.Sprintf("Error: %s", execErr.Error())
				block.IsError = true
			} else {
				block.Content = result
			}
			toolResults = append(toolResults, block)
		}

		messages = append(messages, Message{
			Role:    "user",
			Content: toolResults,
		})
	}

	return "", records, fmt.Errorf("exceeded max tool loops (%d)", r.maxToolLoops)
}

// ToolCallRecord stores a single tool invocation and its result.
type ToolCallRecord struct {
	Name   string
	Input  json.RawMessage
	Result string
	Err    error
}

// ExtractText extracts all text from an LLM response's content blocks.
func ExtractText(content []ContentBlock) string {
	var result string
	for _, b := range content {
		if b.Type == "text" {
			result += b.Text
		}
	}
	return result
}

// MarshalToolInput helper to create JSON input for tool calls.
func MarshalToolInput(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
