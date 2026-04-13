package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
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
}

// New creates a new API handler with all agents wired up.
func New(dataDir string, llm agent.LLM) (*Handler, error) {
	store, err := storage.NewFS(dataDir)
	if err != nil {
		return nil, err
	}

	executor := agent.NewToolExecutor(store)

	return &Handler{
		store:   store,
		ingest:  agent.NewIngestAgent(llm, executor),
		query:   agent.NewQueryAgent(llm, executor),
		compile: agent.NewCompileAgent(llm, executor),
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
	Text    string            `json:"text"`
	Context map[string]string `json:"context,omitempty"`
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

	// Synchronous: write raw event immediately.
	ev := storage.Event{
		Content:       req.Text,
		Actor:         req.Context["actor"],
		Source:        req.Context["source"],
		SessionID:     req.Context["session_id"],
		ActiveProject: req.Context["active_project"],
		ActiveTask:    req.Context["active_task"],
		Durability:    "long-term",
		Actionability: "informational",
		SourceType:    "user",
		EvidenceKind:  "user_statement",
		TrustTier:     1,
	}

	eventID, err := h.store.AppendEvent(ev)
	if err != nil {
		log.Printf("[api] remember append error: %v", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	// Asynchronous: wiki integration in background.
	h.pendingIntegrations.Add(1)
	go func() {
		defer h.pendingIntegrations.Add(-1)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		if err := h.ingest.Integrate(ctx, eventID, req.Text, req.Context); err != nil {
			log.Printf("[api] integrate error (event %s): %v", eventID, err)
		} else {
			log.Printf("[api] integrate done: %s", eventID)
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
	PendingIntegrations int64 `json:"pending_integrations"`
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	stats, err := h.store.GetMemoryStats()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, StatusResponse{
		MemoryStats:         *stats,
		PendingIntegrations: h.pendingIntegrations.Load(),
	})
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
