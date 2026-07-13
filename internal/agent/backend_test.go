package agent

import (
	"context"
	"errors"
	"testing"
)

func TestACPBackendRunQueryReturnsErrNotImplemented(t *testing.T) {
	b := &ACPBackend{}
	_, err := b.RunQuery(context.Background(), "test", nil, nil)
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("ACPBackend.RunQuery() error=%v, want ErrNotImplemented", err)
	}
}

func TestACPBackendRunCompileReturnsErrNotImplemented(t *testing.T) {
	b := &ACPBackend{}
	_, err := b.RunCompile(context.Background(), 0)
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("ACPBackend.RunCompile() error=%v, want ErrNotImplemented", err)
	}
}

func TestNewACPBackendRejectsNonClaudeProvider(t *testing.T) {
	_, err := NewACPBackend(t.TempDir(), ACPBackendConfig{Provider: "codex"})
	if err == nil {
		t.Fatal("expected error for ACP_PROVIDER=codex")
	}
}
