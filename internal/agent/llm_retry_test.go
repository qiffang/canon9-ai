package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type retryTestLLM struct {
	errs  []error
	calls int
}

func (m *retryTestLLM) Call(_ context.Context, _ LLMRequest) (*LLMResponse, error) {
	m.calls++
	if m.calls <= len(m.errs) {
		return nil, m.errs[m.calls-1]
	}
	return &LLMResponse{Content: []ContentBlock{{Type: "text", Text: "ok"}}, StopReason: "end_turn"}, nil
}

func TestRetryLLMRetriesTimeoutThenSucceeds(t *testing.T) {
	base := &retryTestLLM{errs: []error{context.DeadlineExceeded}}
	llm := NewRetryLLM(base, RetryOptions{MaxAttempts: 2})

	resp, err := llm.Call(context.Background(), LLMRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if ExtractText(resp.Content) != "ok" {
		t.Fatalf("response=%q, want ok", ExtractText(resp.Content))
	}
	if base.calls != 2 {
		t.Fatalf("calls=%d, want 2", base.calls)
	}
}

func TestRetryLLMRetriesRateLimitAndServerErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "rate limit", err: errors.New("API error 429: slow down")},
		{name: "server error", err: errors.New("API error 503: unavailable")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := &retryTestLLM{errs: []error{tt.err}}
			llm := NewRetryLLM(base, RetryOptions{MaxAttempts: 2})

			resp, err := llm.Call(context.Background(), LLMRequest{})
			if err != nil {
				t.Fatal(err)
			}
			if ExtractText(resp.Content) != "ok" {
				t.Fatalf("response=%q, want ok", ExtractText(resp.Content))
			}
			if base.calls != 2 {
				t.Fatalf("calls=%d, want 2", base.calls)
			}
		})
	}
}

func TestRetryLLMDoesNotRetryNonRetryableError(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "bad request", err: errors.New("API error 400: invalid request")},
		{name: "insufficient balance", err: errors.New("API error 402: insufficient balance")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := &retryTestLLM{errs: []error{tt.err}}
			llm := NewRetryLLM(base, RetryOptions{MaxAttempts: 3})

			_, err := llm.Call(context.Background(), LLMRequest{})
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "after 1 attempt(s)") {
				t.Fatalf("error=%q, want single attempt count", err.Error())
			}
			if base.calls != 1 {
				t.Fatalf("calls=%d, want 1", base.calls)
			}
		})
	}
}

func TestRetryLLMStopsAtAttemptLimit(t *testing.T) {
	base := &retryTestLLM{errs: []error{context.DeadlineExceeded, context.DeadlineExceeded, context.DeadlineExceeded}}
	llm := NewRetryLLM(base, RetryOptions{MaxAttempts: 2})

	_, err := llm.Call(context.Background(), LLMRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "after 2 attempt(s)") {
		t.Fatalf("error=%q, want attempt count", err.Error())
	}
	if base.calls != 2 {
		t.Fatalf("calls=%d, want 2", base.calls)
	}
}
