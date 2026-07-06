package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRedactCredentials(t *testing.T) {
	input := `{"error":"bad key sk-fa961234567890abcdef and sk_agent_123456789abcdef"}`
	got := redactCredentials(input)

	if strings.Contains(got, "sk-fa961234567890abcdef") {
		t.Fatalf("redacted output still contains hyphen credential: %q", got)
	}
	if strings.Contains(got, "sk_agent_123456789abcdef") {
		t.Fatalf("redacted output still contains underscore credential: %q", got)
	}
	if count := strings.Count(got, "sk_<redacted>"); count != 2 {
		t.Fatalf("redacted count=%d, want 2 in %q", count, got)
	}
}

func TestOpenAILLMRedactsCredentialsInAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"bad key sk-fa961234567890abcdef"}`, http.StatusPaymentRequired)
	}))
	defer server.Close()

	llm := &OpenAILLM{APIKey: "test-key", Model: "test-model", BaseURL: server.URL}
	_, err := llm.Call(context.Background(), LLMRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected API error")
	}
	if strings.Contains(err.Error(), "sk-fa961234567890abcdef") {
		t.Fatalf("API error leaked credential: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "sk_<redacted>") {
		t.Fatalf("API error=%q, want redacted marker", err.Error())
	}
}
