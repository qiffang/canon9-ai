package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qiffang/engram9/internal/agent"
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

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	llm := &mockLLM{response: "Memory stored successfully."}
	h, err := NewWithOptions(t.TempDir(), llm, Options{
		IngestTimeout:             time.Second,
		MaxConcurrentIntegrations: defaultMaxConcurrentIntegrations,
		LLMRetryAttempts:          agent.DefaultLLMRetryAttempts,
		LLMRetryBackoff:           agent.DefaultLLMRetryBackoff,
		LLMCallTimeout:            agent.DefaultLLMCallTimeout,
		LLMProvider:               "test",
		LLMModel:                  "mock-model",
		LLMBaseURL:                "https://example.invalid/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
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

func TestMaxConcurrentIntegrationsFromEnv(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  int
	}{
		{name: "default", value: "", want: defaultMaxConcurrentIntegrations},
		{name: "configured", value: "2", want: 2},
		{name: "invalid", value: "many", want: defaultMaxConcurrentIntegrations},
		{name: "zero", value: "0", want: defaultMaxConcurrentIntegrations},
		{name: "negative", value: "-1", want: defaultMaxConcurrentIntegrations},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(maxConcurrentIntegrationsEnv, tt.value)
			if got := maxConcurrentIntegrationsFromEnv(); got != tt.want {
				t.Fatalf("maxConcurrentIntegrationsFromEnv()=%d, want %d", got, tt.want)
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
	if stats.IngestTimeout != h.EffectiveIngestTimeout().String() {
		t.Fatalf("ingest_timeout=%q, want %q", stats.IngestTimeout, h.EffectiveIngestTimeout().String())
	}
	if stats.MaxConcurrentIntegrations != h.MaxConcurrentIntegrations() {
		t.Fatalf("max_concurrent_integrations=%d, want %d", stats.MaxConcurrentIntegrations, h.MaxConcurrentIntegrations())
	}
	if stats.MaxToolLoops != h.MaxToolLoops() {
		t.Fatalf("max_tool_loops=%d, want %d", stats.MaxToolLoops, h.MaxToolLoops())
	}
	if stats.LLMRetryAttempts != agent.DefaultLLMRetryAttempts {
		t.Fatalf("llm_retry_attempts=%d, want %d", stats.LLMRetryAttempts, agent.DefaultLLMRetryAttempts)
	}
	if stats.LLMRetryBackoff != agent.DefaultLLMRetryBackoff.String() {
		t.Fatalf("llm_retry_backoff=%q, want %q", stats.LLMRetryBackoff, agent.DefaultLLMRetryBackoff.String())
	}
	if stats.LLMCallTimeout != agent.DefaultLLMCallTimeout.String() {
		t.Fatalf("llm_call_timeout=%q, want %q", stats.LLMCallTimeout, agent.DefaultLLMCallTimeout.String())
	}
	if stats.LLMProvider != "test" {
		t.Fatalf("llm_provider=%q, want test", stats.LLMProvider)
	}
	if stats.LLMModel != "mock-model" {
		t.Fatalf("llm_model=%q, want mock-model", stats.LLMModel)
	}
	if stats.LLMBaseURL != "https://example.invalid/v1" {
		t.Fatalf("llm_base_url=%q, want https://example.invalid/v1", stats.LLMBaseURL)
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

func TestRememberCustomMetadata(t *testing.T) {
	h := newTestHandler(t)
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()
	defer h.Wait()

	resp, err := http.Post(srv.URL+"/remember",
		"application/json",
		strings.NewReader(`{"text":"repo scan fact","source_type":"tool","evidence_kind":"direct_observation","trust_tier":2}`))
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

	// Verify the stored event has the custom metadata.
	events, err := h.store.ReadRecentEvents(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("expected at least one event")
	}
	ev := events[len(events)-1]
	if ev.SourceType != "tool" {
		t.Errorf("source_type=%q, want tool", ev.SourceType)
	}
	if ev.EvidenceKind != "direct_observation" {
		t.Errorf("evidence_kind=%q, want direct_observation", ev.EvidenceKind)
	}
	if ev.TrustTier != 2 {
		t.Errorf("trust_tier=%d, want 2", ev.TrustTier)
	}
}

func TestRememberDefaultMetadata(t *testing.T) {
	h := newTestHandler(t)
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()
	defer h.Wait()

	resp, err := http.Post(srv.URL+"/remember",
		"application/json",
		strings.NewReader(`{"text":"plain remember"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}

	events, err := h.store.ReadRecentEvents(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("expected at least one event")
	}
	ev := events[len(events)-1]
	if ev.SourceType != "user" {
		t.Errorf("default source_type=%q, want user", ev.SourceType)
	}
	if ev.EvidenceKind != "user_statement" {
		t.Errorf("default evidence_kind=%q, want user_statement", ev.EvidenceKind)
	}
	if ev.TrustTier != 1 {
		t.Errorf("default trust_tier=%d, want 1", ev.TrustTier)
	}
}

func TestStatusIncludesIngestErrorCount(t *testing.T) {
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
	if stats.IngestErrorCount != 0 {
		t.Errorf("initial ingest_error_count=%d, want 0", stats.IngestErrorCount)
	}
}

func TestRememberInvalidSourceType(t *testing.T) {
	h := newTestHandler(t)
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/remember",
		"application/json",
		strings.NewReader(`{"text":"test","source_type":"repo_scan"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

func TestRememberInvalidEvidenceKind(t *testing.T) {
	h := newTestHandler(t)
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/remember",
		"application/json",
		strings.NewReader(`{"text":"test","evidence_kind":"guess"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

func TestRememberInvalidTrustTier(t *testing.T) {
	h := newTestHandler(t)
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	for _, body := range []string{
		`{"text":"test","trust_tier":0}`,
		`{"text":"test","trust_tier":4}`,
		`{"text":"test","trust_tier":-1}`,
	} {
		resp, err := http.Post(srv.URL+"/remember",
			"application/json",
			strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d, want 400", body, resp.StatusCode)
		}
	}
}

// failingLLM always returns an error, used to test async ingest error counting.
type failingLLM struct{}

func (f *failingLLM) Call(_ context.Context, _ agent.LLMRequest) (*agent.LLMResponse, error) {
	return nil, fmt.Errorf("simulated LLM failure")
}

func TestIngestErrorCountIncrementsOnFailure(t *testing.T) {
	h, err := NewWithOptions(t.TempDir(), &failingLLM{}, Options{
		IngestTimeout:             time.Second,
		MaxConcurrentIntegrations: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()
	defer h.Wait()

	resp, err := http.Post(srv.URL+"/remember",
		"application/json",
		strings.NewReader(`{"text":"trigger failure"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}

	// Wait for async integration to complete.
	h.Wait()

	if got := h.ingestErrors.Load(); got != 1 {
		t.Fatalf("ingest_error_count=%d, want 1", got)
	}
}

type activeCountingLLM struct {
	delay     time.Duration
	active    atomic.Int64
	maxActive atomic.Int64
}

func (m *activeCountingLLM) Call(ctx context.Context, _ agent.LLMRequest) (*agent.LLMResponse, error) {
	active := m.active.Add(1)
	m.recordMaxActive(active)
	defer m.active.Add(-1)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(m.delay):
	}

	return &agent.LLMResponse{
		Content: []agent.ContentBlock{
			{Type: "text", Text: "Memory stored successfully."},
		},
		StopReason: "end_turn",
	}, nil
}

func (m *activeCountingLLM) recordMaxActive(active int64) {
	for {
		maxActive := m.maxActive.Load()
		if active <= maxActive || m.maxActive.CompareAndSwap(maxActive, active) {
			return
		}
	}
}

func TestRememberLimitsConcurrentIntegrations(t *testing.T) {
	llm := &activeCountingLLM{delay: 20 * time.Millisecond}
	h, err := NewWithOptions(t.TempDir(), llm, Options{
		IngestTimeout:             time.Second,
		MaxConcurrentIntegrations: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()
	defer h.Wait()

	for i := 0; i < 3; i++ {
		resp, err := http.Post(srv.URL+"/remember",
			"application/json",
			strings.NewReader(fmt.Sprintf(`{"text":"event %d"}`, i)))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d, want 200", resp.StatusCode)
		}
	}

	h.Wait()

	if got := llm.maxActive.Load(); got != 1 {
		t.Fatalf("max active integrations=%d, want 1", got)
	}
	if got := h.ingestErrors.Load(); got != 0 {
		t.Fatalf("ingest_error_count=%d, want 0", got)
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
