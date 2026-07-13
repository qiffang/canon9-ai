package agent

import (
	"context"

	"github.com/qiffang/engram9/internal/storage"
)

// LLMBackend wraps the existing Runner + LLM + ToolExecutor into the
// AgentBackend interface. No behavior change — pure adapter.
type LLMBackend struct {
	ingest  *IngestAgent
	compile *CompileAgent
	query   *QueryAgent
}

// NewLLMBackend creates an LLMBackend from the existing agent components.
func NewLLMBackend(llm LLM, executor *ToolExecutor, opts RunnerOptions) *LLMBackend {
	return &LLMBackend{
		ingest:  NewIngestAgentWithOptions(llm, executor, opts),
		compile: NewCompileAgentWithOptions(llm, executor, opts),
		query:   NewQueryAgentWithOptions(llm, executor, opts),
	}
}

func (b *LLMBackend) RunIngest(ctx context.Context, eventID string, text string, ctxInfo map[string]string) (IngestResult, error) {
	err := b.ingest.Integrate(ctx, eventID, text, ctxInfo)
	if err != nil {
		return IngestResult{}, err
	}
	return IngestResult{Summary: "integrated via LLM"}, nil
}

func (b *LLMBackend) RunCompile(ctx context.Context, cursor uint64) (CompileResult, error) {
	summary, newCursor, err := b.compile.Compile(ctx, cursor)
	if err != nil {
		return CompileResult{}, err
	}
	return CompileResult{Summary: summary, NewCursor: newCursor}, nil
}

func (b *LLMBackend) RunQuery(ctx context.Context, question string, ctxInfo map[string]string, recentEvents []storage.Event) (QueryResult, error) {
	answer, err := b.query.Recall(ctx, question, ctxInfo, recentEvents)
	if err != nil {
		return QueryResult{}, err
	}
	return QueryResult{Answer: answer}, nil
}

func (b *LLMBackend) Close() error {
	return nil
}
