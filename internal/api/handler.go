package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/qiffang/engram9/internal/agent"
	"github.com/qiffang/engram9/internal/storage"
)

// Handler implements the engram9 HTTP API.
type Handler struct {
	store   storage.Store
	ingest  *agent.IngestAgent
	query   *agent.QueryAgent
	compile *agent.CompileAgent

	// compileMu serializes compile requests (only one compile at a time).
	compileMu sync.Mutex

	// pendingIntegrations tracks the number of async wiki integrations in flight.
	pendingIntegrations atomic.Int64

	// ingestErrors tracks the number of failed async wiki integrations.
	ingestErrors atomic.Int64

	// wg tracks background goroutines for graceful shutdown / testing.
	wg sync.WaitGroup

	// ingestTimeout bounds async /remember wiki integration.
	ingestTimeout time.Duration

	// integrationSlots bounds concurrent async wiki integrations.
	integrationSlots          chan struct{}
	maxConcurrentIntegrations int

	maxToolLoops int

	llmRetryAttempts int
	llmRetryBackoff  time.Duration
	llmCallTimeout   time.Duration
	llmProvider      string
	llmModel         string
	llmBaseURL       string
}

const (
	defaultIngestTimeout             = 5 * time.Minute
	defaultMaxConcurrentIntegrations = 4
	ingestTimeoutEnv                 = "ENGRAM9_INGEST_TIMEOUT"
	maxConcurrentIntegrationsEnv     = "ENGRAM9_MAX_CONCURRENT_INTEGRATIONS"
)

type Options struct {
	MaxToolLoops              int
	IngestTimeout             time.Duration
	MaxConcurrentIntegrations int
	LLMRetryAttempts          int
	LLMRetryBackoff           time.Duration
	LLMCallTimeout            time.Duration
	LLMProvider               string
	LLMModel                  string
	LLMBaseURL                string
}

// New creates a new API handler with all agents wired up.
func New(dataDir string, llm agent.LLM) (*Handler, error) {
	return NewWithOptions(dataDir, llm, Options{})
}

func NewWithOptions(dataDir string, llm agent.LLM, opts Options) (*Handler, error) {
	store, err := storage.NewFS(dataDir)
	if err != nil {
		return nil, err
	}

	executor := agent.NewToolExecutor(store)
	maxToolLoops := opts.MaxToolLoops
	if maxToolLoops <= 0 {
		maxToolLoops = agent.DefaultMaxToolLoops
	}
	runnerOpts := agent.RunnerOptions{MaxToolLoops: maxToolLoops}
	ingestTimeout := opts.IngestTimeout
	if ingestTimeout <= 0 {
		ingestTimeout = ingestTimeoutFromEnv()
	}
	maxConcurrentIntegrations := opts.MaxConcurrentIntegrations
	if maxConcurrentIntegrations <= 0 {
		maxConcurrentIntegrations = maxConcurrentIntegrationsFromEnv()
	}

	return &Handler{
		store:                     store,
		ingest:                    agent.NewIngestAgentWithOptions(llm, executor, runnerOpts),
		query:                     agent.NewQueryAgentWithOptions(llm, executor, runnerOpts),
		compile:                   agent.NewCompileAgentWithOptions(llm, executor, runnerOpts),
		ingestTimeout:             ingestTimeout,
		integrationSlots:          make(chan struct{}, maxConcurrentIntegrations),
		maxConcurrentIntegrations: maxConcurrentIntegrations,
		maxToolLoops:              maxToolLoops,
		llmRetryAttempts:          opts.LLMRetryAttempts,
		llmRetryBackoff:           opts.LLMRetryBackoff,
		llmCallTimeout:            opts.LLMCallTimeout,
		llmProvider:               opts.LLMProvider,
		llmModel:                  opts.LLMModel,
		llmBaseURL:                opts.LLMBaseURL,
	}, nil
}

// Routes returns an http.Handler with all API routes.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /remember", h.handleRemember)
	mux.HandleFunc("POST /recall", h.handleRecall)
	mux.HandleFunc("POST /compile", h.handleCompile)
	mux.HandleFunc("GET /status", h.handleStatus)
	return mux
}

// RememberRequest is the request body for POST /remember.
type RememberRequest struct {
	Text         string            `json:"text"`
	Context      map[string]string `json:"context,omitempty"`
	SourceType   string            `json:"source_type,omitempty"`
	EvidenceKind string            `json:"evidence_kind,omitempty"`
	TrustTier    *int              `json:"trust_tier,omitempty"`
}

// RecallRequest is the request body for POST /recall.
type RecallRequest struct {
	Question string            `json:"question"`
	Context  map[string]string `json:"context,omitempty"`
}

// APIResponse is a generic response envelope.
type APIResponse struct {
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// RememberResponse is the response for POST /remember.
type RememberResponse struct {
	EventID string `json:"event_id"`
}

func (h *Handler) handleRemember(w http.ResponseWriter, r *http.Request) {
	var req RememberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "invalid request body"})
		return
	}
	if req.Text == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "text is required"})
		return
	}

	log.Printf("[api] remember: %s", truncate(req.Text, 80))

	// Validate optional metadata against the canonical enum contract.
	sourceType := "user"
	if req.SourceType != "" {
		if !validSourceType(req.SourceType) {
			writeJSON(w, http.StatusBadRequest, APIResponse{Error: "invalid source_type"})
			return
		}
		sourceType = req.SourceType
	}
	evidenceKind := "user_statement"
	if req.EvidenceKind != "" {
		if !validEvidenceKind(req.EvidenceKind) {
			writeJSON(w, http.StatusBadRequest, APIResponse{Error: "invalid evidence_kind"})
			return
		}
		evidenceKind = req.EvidenceKind
	}
	trustTier := 1
	if req.TrustTier != nil {
		if *req.TrustTier < 1 || *req.TrustTier > 3 {
			writeJSON(w, http.StatusBadRequest, APIResponse{Error: "invalid trust_tier: must be 1, 2, or 3"})
			return
		}
		trustTier = *req.TrustTier
	}
	ev := storage.Event{
		Content:       req.Text,
		Actor:         req.Context["actor"],
		Source:        req.Context["source"],
		SessionID:     req.Context["session_id"],
		ActiveProject: req.Context["active_project"],
		ActiveTask:    req.Context["active_task"],
		Durability:    "long-term",
		Actionability: "informational",
		SourceType:    sourceType,
		EvidenceKind:  evidenceKind,
		TrustTier:     trustTier,
	}

	eventID, err := h.store.AppendEvent(ev)
	if err != nil {
		log.Printf("[api] remember append error: %v", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	// Asynchronous: wiki integration in background.
	h.pendingIntegrations.Add(1)
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		defer h.pendingIntegrations.Add(-1)

		releaseSlot := h.acquireIntegrationSlot()
		defer releaseSlot()

		ctx, cancel := context.WithTimeout(context.Background(), h.effectiveIngestTimeout())
		defer cancel()

		if err := h.ingest.Integrate(ctx, eventID, req.Text, req.Context); err != nil {
			log.Printf("[api] integrate error (event %s): %v", eventID, err)
			h.ingestErrors.Add(1)
		} else {
			log.Printf("[api] integrate done: %s", eventID)
			if err := h.store.RebuildIndex(); err != nil {
				log.Printf("[api] rebuild index error: %v", err)
				h.ingestErrors.Add(1)
			}
		}
	}()

	writeJSON(w, http.StatusOK, RememberResponse{EventID: eventID})
}

func (h *Handler) handleRecall(w http.ResponseWriter, r *http.Request) {
	var req RecallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "invalid request body"})
		return
	}
	if req.Question == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "question is required"})
		return
	}

	log.Printf("[api] recall: %s", truncate(req.Question, 80))

	// Inject recent events so the LLM can answer even if wiki integration is still pending.
	var recentEvents []storage.Event
	if h.pendingIntegrations.Load() > 0 {
		recentEvents, _ = h.store.ReadRecentEvents(10)
	}

	result, err := h.query.Recall(r.Context(), req.Question, req.Context, recentEvents)
	if err != nil {
		log.Printf("[api] recall error: %v", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{Result: result})
}

func (h *Handler) handleCompile(w http.ResponseWriter, r *http.Request) {
	// Only one compile at a time.
	h.compileMu.Lock()
	defer h.compileMu.Unlock()

	// Read cursor from persistent storage — single source of truth.
	cursor := h.store.GetCompileCursor()

	log.Printf("[api] compile: cursor=%d", cursor)
	result, newCursor, err := h.compile.Compile(r.Context(), cursor)
	if err != nil {
		log.Printf("[api] compile error: %v", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	// Persist new cursor only if progress was made.
	if newCursor > cursor {
		if err := h.store.SetCompileCursor(newCursor); err != nil {
			log.Printf("[api] persist cursor error: %v", err)
		}
	}

	log.Printf("[api] compile done: cursor %d -> %d", cursor, newCursor)
	writeJSON(w, http.StatusOK, APIResponse{Result: result})
}

// StatusResponse extends MemoryStats with runtime info.
type StatusResponse struct {
	storage.MemoryStats
	PendingIntegrations       int64  `json:"pending_integrations"`
	IngestErrorCount          int64  `json:"ingest_error_count"`
	IngestTimeout             string `json:"ingest_timeout"`
	MaxConcurrentIntegrations int    `json:"max_concurrent_integrations"`
	MaxToolLoops              int    `json:"max_tool_loops"`
	LLMRetryAttempts          int    `json:"llm_retry_attempts"`
	LLMRetryBackoff           string `json:"llm_retry_backoff"`
	LLMCallTimeout            string `json:"llm_call_timeout"`
	LLMProvider               string `json:"llm_provider,omitempty"`
	LLMModel                  string `json:"llm_model,omitempty"`
	LLMBaseURL                string `json:"llm_base_url,omitempty"`
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	stats, err := h.store.GetMemoryStats()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, StatusResponse{
		MemoryStats:               *stats,
		PendingIntegrations:       h.pendingIntegrations.Load(),
		IngestErrorCount:          h.ingestErrors.Load(),
		IngestTimeout:             h.effectiveIngestTimeout().String(),
		MaxConcurrentIntegrations: h.maxConcurrentIntegrations,
		MaxToolLoops:              h.maxToolLoops,
		LLMRetryAttempts:          h.llmRetryAttempts,
		LLMRetryBackoff:           h.llmRetryBackoff.String(),
		LLMCallTimeout:            h.llmCallTimeout.String(),
		LLMProvider:               h.llmProvider,
		LLMModel:                  h.llmModel,
		LLMBaseURL:                h.llmBaseURL,
	})
}

// Wait blocks until all background integrations finish. Used for testing and graceful shutdown.
func (h *Handler) Wait() { h.wg.Wait() }

func (h *Handler) EffectiveIngestTimeout() time.Duration {
	return h.effectiveIngestTimeout()
}

func (h *Handler) MaxConcurrentIntegrations() int {
	return h.maxConcurrentIntegrations
}

func (h *Handler) MaxToolLoops() int {
	return h.maxToolLoops
}

func (h *Handler) acquireIntegrationSlot() func() {
	if h.integrationSlots == nil {
		return func() {}
	}
	h.integrationSlots <- struct{}{}
	return func() { <-h.integrationSlots }
}

func (h *Handler) effectiveIngestTimeout() time.Duration {
	if h.ingestTimeout > 0 {
		return h.ingestTimeout
	}
	return defaultIngestTimeout
}

func ingestTimeoutFromEnv() time.Duration {
	raw := os.Getenv(ingestTimeoutEnv)
	if raw == "" {
		return defaultIngestTimeout
	}

	timeout, err := time.ParseDuration(raw)
	if err != nil || timeout <= 0 {
		log.Printf("[api] invalid %s=%q, using default %s", ingestTimeoutEnv, raw, defaultIngestTimeout)
		return defaultIngestTimeout
	}
	return timeout
}

func maxConcurrentIntegrationsFromEnv() int {
	raw := os.Getenv(maxConcurrentIntegrationsEnv)
	if raw == "" {
		return defaultMaxConcurrentIntegrations
	}

	maxConcurrent, err := strconv.Atoi(raw)
	if err != nil || maxConcurrent <= 0 {
		log.Printf("[api] invalid %s=%q, using default %d", maxConcurrentIntegrationsEnv, raw, defaultMaxConcurrentIntegrations)
		return defaultMaxConcurrentIntegrations
	}
	return maxConcurrent
}

// runCompile executes a single compile cycle. Safe for concurrent use (serialized by compileMu).
func (h *Handler) runCompile(ctx context.Context) {
	h.compileMu.Lock()
	defer h.compileMu.Unlock()

	cursor := h.store.GetCompileCursor()
	stats, _ := h.store.GetMemoryStats()
	if stats != nil && stats.UncompiledCount == 0 {
		return // nothing to compile
	}

	log.Printf("[auto-compile] starting: cursor=%d, uncompiled=%d", cursor, stats.UncompiledCount)
	_, newCursor, err := h.compile.Compile(ctx, cursor)
	if err != nil {
		log.Printf("[auto-compile] error: %v", err)
		return
	}

	if newCursor > cursor {
		if err := h.store.SetCompileCursor(newCursor); err != nil {
			log.Printf("[auto-compile] persist cursor error: %v", err)
		}
	}
	log.Printf("[auto-compile] done: cursor %d -> %d", cursor, newCursor)
}

// StartAutoCompile runs compile cycles periodically in the background.
// It stops when ctx is cancelled.
func (h *Handler) StartAutoCompile(ctx context.Context, interval time.Duration) {
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		log.Printf("[auto-compile] enabled: every %s", interval)
		for {
			select {
			case <-ctx.Done():
				log.Print("[auto-compile] stopped")
				return
			case <-ticker.C:
				h.runCompile(ctx)
			}
		}
	}()
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Validation helpers matching the canonical enum contract in tooldef.go.

func validSourceType(s string) bool {
	switch s {
	case "user", "assistant", "tool", "system", "compiler":
		return true
	}
	return false
}

func validEvidenceKind(s string) bool {
	switch s {
	case "direct_observation", "user_statement", "inferred", "compiler_synthesis":
		return true
	}
	return false
}
