package dream

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/qiffang/engram9/internal/storage"
)

type fakeCompiler struct {
	mu            sync.Mutex
	calls         int
	maxConcurrent int
	current       int
	summary       string
	newCursor     uint64
	err           error
	entered       chan struct{}
	release       chan struct{}
}

func (f *fakeCompiler) Compile(_ context.Context, cursor uint64) (string, uint64, error) {
	f.mu.Lock()
	f.calls++
	f.current++
	if f.current > f.maxConcurrent {
		f.maxConcurrent = f.current
	}
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	f.mu.Unlock()

	if f.release != nil {
		<-f.release
	}

	f.mu.Lock()
	f.current--
	f.mu.Unlock()

	if f.newCursor == 0 {
		return f.summary, cursor, f.err
	}
	return f.summary, f.newCursor, f.err
}

func TestRunnerDreamAdvancesCursor(t *testing.T) {
	store, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"one", "two"} {
		_, _ = store.AppendEvent(storage.Event{Content: content, Durability: "long-term", SourceType: "user", EvidenceKind: "user_statement", TrustTier: 1})
	}

	compiler := &fakeCompiler{summary: "compiled two events", newCursor: 2}
	runner := NewRunner(store, compiler)
	runner.now = fixedClock()

	result, err := runner.Dream(context.Background())
	if err != nil {
		t.Fatalf("Dream: %v", err)
	}

	if result.StartCursor != 0 || result.NewCursor != 2 || result.ProcessedEvents != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Summary != "compiled two events" {
		t.Fatalf("summary = %q", result.Summary)
	}
	if store.GetCompileCursor() != 2 {
		t.Fatalf("compile cursor = %d, want 2", store.GetCompileCursor())
	}

	status := runner.Status()
	if status.Running {
		t.Fatal("status should not be running after Dream returns")
	}
	if status.LastNewCursor != 2 || status.LastProcessedEvents != 2 || status.LastError != "" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestRunnerDreamRecordsFailureWithoutAdvancingCursor(t *testing.T) {
	store, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = store.AppendEvent(storage.Event{Content: "one", Durability: "long-term", SourceType: "user", EvidenceKind: "user_statement", TrustTier: 1})

	compiler := &fakeCompiler{summary: "partial", newCursor: 1, err: errors.New("llm unavailable")}
	runner := NewRunner(store, compiler)

	_, err = runner.Dream(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if store.GetCompileCursor() != 0 {
		t.Fatalf("compile cursor = %d, want 0", store.GetCompileCursor())
	}

	status := runner.Status()
	if status.LastError == "" {
		t.Fatalf("expected last error in status: %+v", status)
	}
	if status.LastNewCursor != 0 || status.LastProcessedEvents != 0 {
		t.Fatalf("unexpected failure status: %+v", status)
	}
}

func TestRunnerDreamSerializesConcurrentCalls(t *testing.T) {
	store, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	compiler := &fakeCompiler{
		summary:   "compiled",
		entered:   make(chan struct{}, 2),
		release:   make(chan struct{}, 2),
		newCursor: 0,
	}
	runner := NewRunner(store, compiler)

	done := make(chan error, 2)
	go func() {
		_, err := runner.Dream(context.Background())
		done <- err
	}()
	<-compiler.entered

	go func() {
		_, err := runner.Dream(context.Background())
		done <- err
	}()

	select {
	case <-compiler.entered:
		t.Fatal("second compile entered before first released")
	case <-time.After(50 * time.Millisecond):
	}

	compiler.release <- struct{}{}
	if err := <-done; err != nil {
		t.Fatalf("first Dream: %v", err)
	}

	<-compiler.entered
	compiler.release <- struct{}{}
	if err := <-done; err != nil {
		t.Fatalf("second Dream: %v", err)
	}

	compiler.mu.Lock()
	maxConcurrent := compiler.maxConcurrent
	calls := compiler.calls
	compiler.mu.Unlock()
	if maxConcurrent != 1 {
		t.Fatalf("max concurrent compiles = %d, want 1", maxConcurrent)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func fixedClock() func() time.Time {
	times := []time.Time{
		time.Date(2026, 6, 17, 3, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 17, 3, 0, 1, 0, time.UTC),
	}
	var mu sync.Mutex
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		if len(times) == 0 {
			return time.Date(2026, 6, 17, 3, 0, 2, 0, time.UTC)
		}
		t := times[0]
		times = times[1:]
		return t
	}
}
