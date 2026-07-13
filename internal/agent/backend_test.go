package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/qiffang/engram9/internal/storage"
)

func TestACPBackendRunQueryReturnsErrNotImplemented(t *testing.T) {
	// ACPBackend.RunQuery must return ErrNotImplemented since Query ACP is not designed yet.
	// We cannot construct a real ACPBackend (requires acpmux binary), so test via the interface contract.
	var backend AgentBackend = &acpQueryStub{}
	_, err := backend.RunQuery(context.Background(), "test", nil, nil)
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("RunQuery error=%v, want ErrNotImplemented", err)
	}
}

// acpQueryStub mimics ACPBackend.RunQuery behavior for testing.
type acpQueryStub struct{}

func (s *acpQueryStub) RunIngest(_ context.Context, _ string, _ string, _ map[string]string) (IngestResult, error) {
	return IngestResult{}, nil
}
func (s *acpQueryStub) RunCompile(_ context.Context, _ uint64) (CompileResult, error) {
	return CompileResult{}, nil
}
func (s *acpQueryStub) RunQuery(_ context.Context, _ string, _ map[string]string, _ []storage.Event) (QueryResult, error) {
	return QueryResult{}, ErrNotImplemented
}
func (s *acpQueryStub) Close() error { return nil }
