package dream

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/qiffang/engram9/internal/storage"
)

type Compiler interface {
	Compile(ctx context.Context, cursor uint64) (string, uint64, error)
}

type Result struct {
	StartedAt       string `json:"started_at"`
	FinishedAt      string `json:"finished_at"`
	StartCursor     uint64 `json:"start_cursor"`
	NewCursor       uint64 `json:"new_cursor"`
	ProcessedEvents int    `json:"processed_events"`
	Summary         string `json:"summary"`
}

type Status struct {
	Running             bool   `json:"running"`
	LastStartedAt       string `json:"last_started_at,omitempty"`
	LastFinishedAt      string `json:"last_finished_at,omitempty"`
	LastStartCursor     uint64 `json:"last_start_cursor"`
	LastNewCursor       uint64 `json:"last_new_cursor"`
	LastProcessedEvents int    `json:"last_processed_events"`
	LastError           string `json:"last_error,omitempty"`
	LastSummary         string `json:"last_summary,omitempty"`
}

type Runner struct {
	store    storage.Store
	compiler Compiler
	now      func() time.Time

	runMu    sync.Mutex
	statusMu sync.Mutex
	status   Status
}

func NewRunner(store storage.Store, compiler Compiler) *Runner {
	return &Runner{
		store:    store,
		compiler: compiler,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

func (r *Runner) Dream(ctx context.Context) (Result, error) {
	r.runMu.Lock()
	defer r.runMu.Unlock()

	start := r.now()
	cursor := r.store.GetCompileCursor()
	r.setStatus(Status{
		Running:         true,
		LastStartedAt:   formatTime(start),
		LastStartCursor: cursor,
		LastNewCursor:   cursor,
	})

	summary, newCursor, err := r.compiler.Compile(ctx, cursor)
	if err == nil && newCursor > cursor {
		err = r.store.SetCompileCursor(newCursor)
	}
	if newCursor < cursor || err != nil {
		newCursor = cursor
	}

	finish := r.now()
	result := Result{
		StartedAt:       formatTime(start),
		FinishedAt:      formatTime(finish),
		StartCursor:     cursor,
		NewCursor:       newCursor,
		ProcessedEvents: int(newCursor - cursor),
		Summary:         summary,
	}

	status := Status{
		Running:             false,
		LastStartedAt:       result.StartedAt,
		LastFinishedAt:      result.FinishedAt,
		LastStartCursor:     result.StartCursor,
		LastNewCursor:       result.NewCursor,
		LastProcessedEvents: result.ProcessedEvents,
		LastSummary:         result.Summary,
	}
	if err != nil {
		status.LastError = err.Error()
	}
	r.setStatus(status)

	if err != nil {
		return result, fmt.Errorf("compile memory: %w", err)
	}
	return result, nil
}

func (r *Runner) Status() Status {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	return r.status
}

func (r *Runner) setStatus(status Status) {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	r.status = status
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}
