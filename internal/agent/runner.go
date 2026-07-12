package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
)

const DefaultMaxToolLoops = 80
const DefaultMaxRepeatedReadOnlyToolCalls = 8
const DefaultMaxInvalidToolCalls = 3

type RunnerOptions struct {
	MaxToolLoops                 int
	MaxRepeatedReadOnlyToolCalls int
	MaxInvalidToolCalls          int
}

// ToolCallback is called after each tool execution with the tool name and result.
type ToolCallback func(name string, input json.RawMessage, result string, err error)

// Runner drives an agentic tool-use loop: send messages to LLM, execute tool calls,
// feed results back, repeat until the LLM produces a final text response.
type Runner struct {
	llm                          LLM
	executor                     *ToolExecutor
	maxToolLoops                 int
	maxRepeatedReadOnlyToolCalls int
	maxInvalidToolCalls          int
}

func NewRunner(llm LLM, executor *ToolExecutor) *Runner {
	return NewRunnerWithOptions(llm, executor, RunnerOptions{})
}

func NewRunnerWithOptions(llm LLM, executor *ToolExecutor, opts RunnerOptions) *Runner {
	maxToolLoops := opts.MaxToolLoops
	if maxToolLoops <= 0 {
		maxToolLoops = DefaultMaxToolLoops
	}
	maxRepeatedReadOnlyToolCalls := opts.MaxRepeatedReadOnlyToolCalls
	if maxRepeatedReadOnlyToolCalls <= 0 {
		maxRepeatedReadOnlyToolCalls = DefaultMaxRepeatedReadOnlyToolCalls
	}
	maxInvalidToolCalls := opts.MaxInvalidToolCalls
	if maxInvalidToolCalls <= 0 {
		maxInvalidToolCalls = DefaultMaxInvalidToolCalls
	}
	return &Runner{
		llm:                          llm,
		executor:                     executor,
		maxToolLoops:                 maxToolLoops,
		maxRepeatedReadOnlyToolCalls: maxRepeatedReadOnlyToolCalls,
		maxInvalidToolCalls:          maxInvalidToolCalls,
	}
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
	allowedTools := allowedToolNames(tools)
	readOnlyTools := readOnlyToolNames(tools)
	lastReadOnlyCallKey := ""
	consecutiveReadOnlyToolCalls := 0
	consecutiveInvalidToolCalls := 0

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
			if !allowedTools[tc.Name] {
				err := fmt.Errorf("invalid tool call %q: not in advertised tool set", tc.Name)
				records = append(records, ToolCallRecord{Name: tc.Name, Input: tc.Input, Err: err})
				consecutiveInvalidToolCalls++
				if consecutiveInvalidToolCalls > r.maxInvalidToolCalls {
					return "", records, fmt.Errorf("invalid tool call exceeded limit (%d): %w", r.maxInvalidToolCalls, err)
				}
				toolResults = append(toolResults, toolErrorResult(tc.ID, err))
				continue
			}
			consecutiveInvalidToolCalls = 0
			if readOnlyTools[tc.Name] {
				key := toolCallKey(tc)
				if key == lastReadOnlyCallKey {
					consecutiveReadOnlyToolCalls++
				} else {
					lastReadOnlyCallKey = key
					consecutiveReadOnlyToolCalls = 1
				}
				if consecutiveReadOnlyToolCalls > r.maxRepeatedReadOnlyToolCalls {
					err := fmt.Errorf("repeated read-only tool call %q exceeded limit (%d); possible model stall", tc.Name, r.maxRepeatedReadOnlyToolCalls)
					records = append(records, ToolCallRecord{Name: tc.Name, Input: tc.Input, Err: err})
					return "", records, err
				}
			}
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
				if !readOnlyTools[tc.Name] {
					lastReadOnlyCallKey = ""
					consecutiveReadOnlyToolCalls = 0
				}
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

func allowedToolNames(tools []Tool) map[string]bool {
	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Name] = true
	}
	return names
}

func readOnlyToolNames(tools []Tool) map[string]bool {
	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		if tool.ReadOnly {
			names[tool.Name] = true
		}
	}
	return names
}

func toolCallKey(tc ContentBlock) string {
	return tc.Name + "\x00" + string(tc.Input)
}

func toolErrorResult(toolUseID string, err error) ContentBlock {
	return ContentBlock{
		Type:      "tool_result",
		ToolUseID: toolUseID,
		Content:   fmt.Sprintf("Error: %s", err.Error()),
		IsError:   true,
	}
}
