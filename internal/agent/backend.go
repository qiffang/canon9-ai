package agent

import (
	"context"
	"errors"

	"github.com/qiffang/engram9/internal/storage"
)

// ErrNotImplemented is returned when a backend does not support a given operation.
var ErrNotImplemented = errors.New("not implemented")

// IngestResult contains the outcome of an ingest run.
type IngestResult struct {
	// Summary is a free-form text summary of what the agent did.
	Summary string
}

// CompileResult contains the outcome of a compile run.
type CompileResult struct {
	// Summary is a free-form text summary of what the agent did.
	Summary string
	// NewCursor is the event cursor after compile. Zero means no progress.
	NewCursor uint64
}

// QueryResult contains the outcome of a query run.
type QueryResult struct {
	// Answer is the reconstructed answer to the question.
	Answer string
}

// AgentBackend defines the contract for running wiki agent turns.
//
// This is NOT the LLM interface. LLM.Call() returns content blocks from a
// single API call. AgentBackend represents a complete agent turn that may
// involve multiple internal tool calls, file I/O, and multi-step reasoning.
type AgentBackend interface {
	// RunIngest processes a chunk of events and integrates them into the wiki.
	RunIngest(ctx context.Context, eventID string, text string, ctxInfo map[string]string) (IngestResult, error)

	// RunCompile runs a full compile cycle (distill + prune + rebuild index).
	RunCompile(ctx context.Context, cursor uint64) (CompileResult, error)

	// RunQuery answers a question by reconstructing knowledge from the memory system.
	RunQuery(ctx context.Context, question string, ctxInfo map[string]string, recentEvents []storage.Event) (QueryResult, error)

	// Close releases resources held by the backend.
	Close() error
}
