package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// Message represents a conversation message.
type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []ContentBlock
}

// ContentBlock is a typed block within a message (text, tool_use, tool_result).
type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

// Tool defines a tool the LLM can call.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// LLMRequest is sent to the LLM API.
type LLMRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system,omitempty"`
	Messages  []Message `json:"messages"`
	Tools     []Tool    `json:"tools,omitempty"`
}

// LLMResponse is the parsed LLM API response.
type LLMResponse struct {
	Content    []ContentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
}

// LLM defines the interface for language model calls.
type LLM interface {
	Call(ctx context.Context, req LLMRequest) (*LLMResponse, error)
}

// AnthropicLLM calls the Anthropic Messages API.
type AnthropicLLM struct {
	APIKey string
	Model  string
}

// NewAnthropicLLM creates an LLM using the Anthropic API.
// Reads ANTHROPIC_API_KEY from env if apiKey is empty.
func NewAnthropicLLM(model string) *AnthropicLLM {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	return &AnthropicLLM{APIKey: apiKey, Model: model}
}

func (a *AnthropicLLM) Call(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	if a.APIKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
	}
	if req.Model == "" {
		req.Model = a.Model
	}
	if req.MaxTokens == 0 {
		req.MaxTokens = 4096
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

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
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, redactCredentials(string(respBody)))
	}

	var llmResp LLMResponse
	if err := json.Unmarshal(respBody, &llmResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	return &llmResp, nil
}
