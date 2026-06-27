package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/qiffang/engram9/internal/agent"
	"github.com/qiffang/engram9/internal/storage"
)

// mockLLM returns a canned response for testing without real API calls.
type mockLLM struct {
	response string
}

func (m *mockLLM) Call(_ context.Context, req agent.LLMRequest) (*agent.LLMResponse, error) {
	return &agent.LLMResponse{
		Content: []agent.ContentBlock{
			{Type: "text", Text: m.response},
		},
		StopReason: "end_turn",
	}, nil
}

type compileCursorLLM struct {
	callCount int
}

func (m *compileCursorLLM) Call(_ context.Context, _ agent.LLMRequest) (*agent.LLMResponse, error) {
	m.callCount++
	if m.callCount == 1 {
		return &agent.LLMResponse{
			Content: []agent.ContentBlock{{
				Type:  "tool_use",
				ID:    "call_1",
				Name:  "read_events_since",
				Input: json.RawMessage(`{"cursor": 0}`),
			}},
			StopReason: "tool_use",
		}, nil
	}
	return &agent.LLMResponse{
		Content:    []agent.ContentBlock{{Type: "text", Text: "compiled"}},
		StopReason: "end_turn",
	}, nil
}

type failingCursorStore struct {
	storage.Store
	err error
}

func (s *failingCursorStore) SetCompileCursor(_ uint64) error {
	return s.err
}

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.NewFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	llm := &mockLLM{response: "Memory stored successfully."}
	executor := agent.NewToolExecutor(store)
	return &Handler{
		store:   store,
		ingest:  agent.NewIngestAgent(llm, executor),
		query:   agent.NewQueryAgent(llm, executor),
		compile: agent.NewCompileAgent(llm, executor),
	}
}

func TestIngestTimeoutFromEnv(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{name: "default", value: "", want: defaultIngestTimeout},
		{name: "configured", value: "10m", want: 10 * time.Minute},
		{name: "invalid", value: "slow", want: defaultIngestTimeout},
		{name: "zero", value: "0s", want: defaultIngestTimeout},
		{name: "negative", value: "-1s", want: defaultIngestTimeout},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(ingestTimeoutEnv, tt.value)
			if got := ingestTimeoutFromEnv(); got != tt.want {
				t.Fatalf("ingestTimeoutFromEnv()=%s, want %s", got, tt.want)
			}
		})
	}
}

func TestRememberEndpoint(t *testing.T) {
	h := newTestHandler(t)
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()
	defer h.Wait() // wait for background goroutines before TempDir cleanup

	resp, err := http.Post(srv.URL+"/remember",
		"application/json",
		strings.NewReader(`{"text": "Alice suggests partition tables"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}

	var result RememberResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.EventID == "" {
		t.Error("expected non-empty event_id")
	}
	if !strings.HasPrefix(result.EventID, "evt_") {
		t.Errorf("event_id=%q, want evt_ prefix", result.EventID)
	}
}

func TestRememberEmptyText(t *testing.T) {
	h := newTestHandler(t)
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/remember",
		"application/json",
		strings.NewReader(`{"text": ""}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

func TestRecallEndpoint(t *testing.T) {
	h := newTestHandler(t)
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/recall",
		"application/json",
		strings.NewReader(`{"question": "What did Alice say about db9?"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
}

func TestRecallEmptyQuestion(t *testing.T) {
	h := newTestHandler(t)
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/recall",
		"application/json",
		strings.NewReader(`{"question": ""}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

func TestStatusEndpoint(t *testing.T) {
	h := newTestHandler(t)
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}

	var stats StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}
}

func TestCompileEndpoint(t *testing.T) {
	h := newTestHandler(t)
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/compile",
		"application/json",
		strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
}

func TestCompileEndpointReturnsErrorWhenCursorPersistFails(t *testing.T) {
	dir := t.TempDir()
	baseStore, err := storage.NewFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := baseStore.AppendEvent(storage.Event{
		Content:    "event",
		Durability: "long-term",
		SourceType: "user",
		TrustTier:  1,
	}); err != nil {
		t.Fatal(err)
	}

	store := &failingCursorStore{
		Store: baseStore,
		err:   errors.New("cursor write failed"),
	}
	llm := &compileCursorLLM{}
	executor := agent.NewToolExecutor(store)
	h := &Handler{
		store:   store,
		compile: agent.NewCompileAgent(llm, executor),
	}
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/compile",
		"application/json",
		strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status=%d, want 500", resp.StatusCode)
	}
	if got := baseStore.GetCompileCursor(); got != 0 {
		t.Fatalf("cursor=%d, want 0", got)
	}
}

func TestRememberMethodNotAllowed(t *testing.T) {
	h := newTestHandler(t)
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/remember")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", resp.StatusCode)
	}
}
