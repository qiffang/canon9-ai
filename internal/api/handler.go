package api

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/qiffang/engram9/internal/agent"
	"github.com/qiffang/engram9/internal/storage"
)

// Handler implements the engram9 HTTP API.
type Handler struct {
	store   *storage.FS
	ingest  *agent.IngestAgent
	query   *agent.QueryAgent
	compile *agent.CompileAgent

	compileMu     sync.Mutex
	compileCursor uint64
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
	result, err := h.ingest.Remember(r.Context(), req.Text, req.Context)
	if err != nil {
		log.Printf("[api] remember error: %v", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{Result: result})
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
	result, err := h.query.Recall(r.Context(), req.Question, req.Context)
	if err != nil {
		log.Printf("[api] recall error: %v", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{Result: result})
}

func (h *Handler) handleCompile(w http.ResponseWriter, r *http.Request) {
	h.compileMu.Lock()
	cursor := h.compileCursor
	h.compileMu.Unlock()

	log.Printf("[api] compile: cursor=%d", cursor)
	result, newCursor, err := h.compile.Compile(r.Context(), cursor)
	if err != nil {
		log.Printf("[api] compile error: %v", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	h.compileMu.Lock()
	h.compileCursor = newCursor
	h.compileMu.Unlock()

	log.Printf("[api] compile done: new_cursor=%d", newCursor)
	writeJSON(w, http.StatusOK, APIResponse{Result: result})
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	stats, err := h.store.GetMemoryStats()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, stats)
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
