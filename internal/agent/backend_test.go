package agent

import (
	"context"
	"errors"
	"testing"
)

func TestACPBackendRunQueryReturnsErrNotImplemented(t *testing.T) {
	// We cannot construct a full ACPBackend (requires acpmux binary on PATH),
	// but we can call RunQuery on a zero-value ACPBackend to verify the real
	// method returns ErrNotImplemented — not a stub.
	b := &ACPBackend{}
	_, err := b.RunQuery(context.Background(), "test", nil, nil)
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("ACPBackend.RunQuery() error=%v, want ErrNotImplemented", err)
	}
}
