package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
)

const maxToolLoops = 20

// Runner drives an agentic tool-use loop: send messages to LLM, execute tool calls,
// feed results back, repeat until the LLM produces a final text response.
type Runner struct {
	llm      LLM
	executor *ToolExecutor
}

func NewRunner(llm LLM, executor *ToolExecutor) *Runner {
	return &Runner{llm: llm, executor: executor}
}

// Run executes an agent with the given system prompt, tools, and user message.
// Returns the final text response from the LLM.
func (r *Runner) Run(ctx context.Context, system string, tools []Tool, userMessage string) (string, error) {
	messages := []Message{
		{Role: "user", Content: userMessage},
	}

	for i := 0; i < maxToolLoops; i++ {
		resp, err := r.llm.Call(ctx, LLMRequest{
			System:   system,
			Messages: messages,
			Tools:    tools,
		})
		if err != nil {
			return "", fmt.Errorf("llm call: %w", err)
		}

		// Collect text and tool_use blocks.
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

		// If no tool calls, we're done — return the text response.
		if resp.StopReason != "tool_use" || len(toolCalls) == 0 {
			result := ""
			for _, t := range textParts {
				result += t
			}
			return result, nil
		}

		// Append assistant response with all content blocks.
		messages = append(messages, Message{
			Role:    "assistant",
			Content: resp.Content,
		})

		// Execute each tool call and build tool_result blocks.
		var toolResults []ContentBlock
		for _, tc := range toolCalls {
			log.Printf("[agent] tool_use: %s", tc.Name)
			result, err := r.executor.Execute(tc.Name, tc.Input)
			block := ContentBlock{
				Type:      "tool_result",
				ToolUseID: tc.ID,
			}
			if err != nil {
				block.Content = fmt.Sprintf("Error: %s", err.Error())
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

	return "", fmt.Errorf("exceeded max tool loops (%d)", maxToolLoops)
}

// RunWithMessages is like Run but takes pre-built messages for more control.
func (r *Runner) RunWithMessages(ctx context.Context, system string, tools []Tool, messages []Message) (string, []Message, error) {
	for i := 0; i < maxToolLoops; i++ {
		resp, err := r.llm.Call(ctx, LLMRequest{
			System:   system,
			Messages: messages,
			Tools:    tools,
		})
		if err != nil {
			return "", messages, fmt.Errorf("llm call: %w", err)
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
			messages = append(messages, Message{Role: "assistant", Content: resp.Content})
			return result, messages, nil
		}

		messages = append(messages, Message{Role: "assistant", Content: resp.Content})

		var toolResults []ContentBlock
		for _, tc := range toolCalls {
			log.Printf("[agent] tool_use: %s", tc.Name)
			result, err := r.executor.Execute(tc.Name, tc.Input)
			block := ContentBlock{
				Type:      "tool_result",
				ToolUseID: tc.ID,
			}
			if err != nil {
				block.Content = fmt.Sprintf("Error: %s", err.Error())
				block.IsError = true
			} else {
				block.Content = result
			}
			toolResults = append(toolResults, block)
		}

		messages = append(messages, Message{Role: "user", Content: toolResults})
	}

	return "", messages, fmt.Errorf("exceeded max tool loops (%d)", maxToolLoops)
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
