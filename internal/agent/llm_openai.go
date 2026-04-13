package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// OpenAILLM calls an OpenAI-compatible chat completions API (Qwen, OpenAI, etc.).
// It translates between the internal Anthropic-style data structures and the
// OpenAI ChatCompletions request/response format.
type OpenAILLM struct {
	APIKey  string
	Model   string
	BaseURL string // e.g. "https://dashscope.aliyuncs.com/compatible-mode/v1"
}

// NewOpenAILLM creates an LLM targeting an OpenAI-compatible endpoint.
// Reads OPENAI_API_KEY and OPENAI_BASE_URL from env.
func NewOpenAILLM(model string) *OpenAILLM {
	apiKey := os.Getenv("OPENAI_API_KEY")
	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://dashscope-intl.aliyuncs.com/compatible-mode/v1"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if model == "" {
		model = "qwen-turbo"
	}
	return &OpenAILLM{APIKey: apiKey, Model: model, BaseURL: baseURL}
}

// --- OpenAI request types ---

type oaiRequest struct {
	Model    string       `json:"model"`
	Messages []oaiMessage `json:"messages"`
	Tools    []oaiTool    `json:"tools,omitempty"`
}

type oaiMessage struct {
	Role       string          `json:"role"`
	Content    string          `json:"content,omitempty"`
	ToolCalls  []oaiToolCall   `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Name       string          `json:"name,omitempty"`
}

type oaiTool struct {
	Type     string      `json:"type"`
	Function oaiFunction `json:"function"`
}

type oaiFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type oaiToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function oaiToolCallFunc `json:"function"`
}

type oaiToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// --- OpenAI response types ---

type oaiResponse struct {
	Choices []oaiChoice `json:"choices"`
}

type oaiChoice struct {
	Message      oaiMessage `json:"message"`
	FinishReason string     `json:"finish_reason"` // "stop", "tool_calls"
}

// Call translates the internal LLMRequest into an OpenAI ChatCompletions call,
// then translates the response back into the internal LLMResponse format.
func (o *OpenAILLM) Call(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	if o.APIKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY not set")
	}

	model := req.Model
	if model == "" {
		model = o.Model
	}

	// Build OpenAI messages.
	var msgs []oaiMessage
	if req.System != "" {
		msgs = append(msgs, oaiMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		converted := convertMessageToOAI(m)
		msgs = append(msgs, converted...)
	}

	// Build OpenAI tools.
	var tools []oaiTool
	for _, t := range req.Tools {
		tools = append(tools, oaiTool{
			Type: "function",
			Function: oaiFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}

	oaiReq := oaiRequest{
		Model:    model,
		Messages: msgs,
		Tools:    tools,
	}

	body, err := json.Marshal(oaiReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := o.BaseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.APIKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http call: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var oaiResp oaiResponse
	if err := json.Unmarshal(respBody, &oaiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(oaiResp.Choices) == 0 {
		return nil, fmt.Errorf("empty choices in response")
	}

	return convertOAIResponse(oaiResp.Choices[0]), nil
}

// convertMessageToOAI converts an internal Message (Anthropic format) into
// one or more OpenAI messages.
func convertMessageToOAI(m Message) []oaiMessage {
	switch content := m.Content.(type) {
	case string:
		return []oaiMessage{{Role: m.Role, Content: content}}

	case []any:
		// Content blocks array — could be from assistant (with tool_use) or
		// user (with tool_result).
		blocks := parseContentBlocks(content)

		// Check what types of blocks we have.
		var textParts []string
		var toolCalls []oaiToolCall
		var toolResults []oaiMessage

		for _, b := range blocks {
			switch b.Type {
			case "text":
				textParts = append(textParts, b.Text)
			case "tool_use":
				toolCalls = append(toolCalls, oaiToolCall{
					ID:   b.ID,
					Type: "function",
					Function: oaiToolCallFunc{
						Name:      b.Name,
						Arguments: string(b.Input),
					},
				})
			case "tool_result":
				toolResults = append(toolResults, oaiMessage{
					Role:       "tool",
					ToolCallID: b.ToolUseID,
					Content:    b.Content,
				})
			}
		}

		// Assistant message with tool calls.
		if m.Role == "assistant" && len(toolCalls) > 0 {
			msg := oaiMessage{
				Role:      "assistant",
				Content:   strings.Join(textParts, ""),
				ToolCalls: toolCalls,
			}
			return []oaiMessage{msg}
		}

		// User message containing tool_results → emit as individual tool messages.
		if len(toolResults) > 0 {
			return toolResults
		}

		// Plain text blocks from user.
		return []oaiMessage{{Role: m.Role, Content: strings.Join(textParts, "")}}

	case []ContentBlock:
		// Already typed — same logic as []any but simpler.
		var textParts []string
		var toolCalls []oaiToolCall
		var toolResults []oaiMessage

		for _, b := range content {
			switch b.Type {
			case "text":
				textParts = append(textParts, b.Text)
			case "tool_use":
				toolCalls = append(toolCalls, oaiToolCall{
					ID:   b.ID,
					Type: "function",
					Function: oaiToolCallFunc{
						Name:      b.Name,
						Arguments: string(b.Input),
					},
				})
			case "tool_result":
				toolResults = append(toolResults, oaiMessage{
					Role:       "tool",
					ToolCallID: b.ToolUseID,
					Content:    b.Content,
				})
			}
		}

		if m.Role == "assistant" && len(toolCalls) > 0 {
			return []oaiMessage{{
				Role:      "assistant",
				Content:   strings.Join(textParts, ""),
				ToolCalls: toolCalls,
			}}
		}

		if len(toolResults) > 0 {
			return toolResults
		}

		return []oaiMessage{{Role: m.Role, Content: strings.Join(textParts, "")}}

	default:
		return []oaiMessage{{Role: m.Role, Content: fmt.Sprintf("%v", content)}}
	}
}

// parseContentBlocks converts a []any (from JSON unmarshal) into typed ContentBlocks.
func parseContentBlocks(raw []any) []ContentBlock {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var blocks []ContentBlock
	if err := json.Unmarshal(data, &blocks); err != nil {
		return nil
	}
	return blocks
}

// convertOAIResponse converts an OpenAI choice into the internal LLMResponse.
func convertOAIResponse(choice oaiChoice) *LLMResponse {
	var content []ContentBlock

	if choice.Message.Content != "" {
		content = append(content, ContentBlock{
			Type: "text",
			Text: choice.Message.Content,
		})
	}

	for _, tc := range choice.Message.ToolCalls {
		content = append(content, ContentBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: json.RawMessage(tc.Function.Arguments),
		})
	}

	stopReason := "end_turn"
	if choice.FinishReason == "tool_calls" {
		stopReason = "tool_use"
	}

	return &LLMResponse{
		Content:    content,
		StopReason: stopReason,
	}
}
