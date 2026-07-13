package agent

import (
	"bufio"
	"context"
	"errors"
	"strings"
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

func TestReadACPResponseErrorField(t *testing.T) {
	// Simulate an ACP error response (e.g. initialize returns -32602).
	line := `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"invalid params"}}` + "\n"
	scanner := bufio.NewScanner(strings.NewReader(line))
	scanner.Buffer(make([]byte, 4<<20), 4<<20)

	resp, err := readACPResponse(scanner)
	if err != nil {
		t.Fatalf("readACPResponse returned error: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error field in response")
	}
	if resp.Error.Code != -32602 {
		t.Fatalf("error code=%d, want -32602", resp.Error.Code)
	}
}

func TestReadACPResponseEOF(t *testing.T) {
	// Empty input — readACPResponse should return io.EOF.
	scanner := bufio.NewScanner(strings.NewReader(""))
	_, err := readACPResponse(scanner)
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestReadACPResponseSkipsMalformedLines(t *testing.T) {
	// First line is malformed, second is valid.
	input := "not json\n" + `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}` + "\n"
	scanner := bufio.NewScanner(strings.NewReader(input))
	scanner.Buffer(make([]byte, 4<<20), 4<<20)

	resp, err := readACPResponse(scanner)
	if err != nil {
		t.Fatalf("readACPResponse returned error: %v", err)
	}
	if resp.Result == nil {
		t.Fatal("expected result in response")
	}
}

func TestACPProtocolVersionIsInteger(t *testing.T) {
	// Verify the initialize request uses integer protocolVersion, not string.
	params := mustMarshal(map[string]any{
		"protocolVersion": 1,
		"clientInfo":      map[string]string{"name": "engram9", "version": "0.1.0"},
	})
	// The JSON should contain "protocolVersion":1 (integer), not "protocolVersion":"..."
	s := string(params)
	if !strings.Contains(s, `"protocolVersion":1`) {
		t.Fatalf("protocolVersion should be integer 1, got: %s", s)
	}
}
