# internal/api — HTTP Handler & Orchestration

## Overview
HTTP handler that wires `storage.Store` + `agent.LLM` into 4 REST endpoints. The only package that knows about all three agents (ingest, query, compile). Also manages the auto-compile background ticker.

## Structure
```
api/
├── handler.go           HTTP handler + Routes() + StartAutoCompile()
└── handler_test.go      8 tests using mock LLM
```

## Routes

| Method | Path | Sync/Async | Agent | Timeout |
|--------|------|------------|-------|---------|
| POST | /remember | Async (goroutine) | IngestAgent.Integrate() | `ENGRAM9_INGEST_TIMEOUT` (default 2m) |
| POST | /recall | Sync | QueryAgent.Recall() | HTTP request ctx |
| POST | /compile | Async (goroutine) | CompileAgent.Compile() | No timeout |
| GET | /status | Sync | — | HTTP request ctx |

## Wiring

```
api.New(dataDir, llm) error
  └── storage.NewFS(dataDir)          # create store
  └── agent.NewToolExecutor(store)    # create tool executor
  └── agent.NewIngestAgent(...)       # create 3 agents
  └── agent.NewQueryAgent(...)
  └── agent.NewCompileAgent(...)
  └── return Handler{store, agents, llm}
```

## Key Patterns

- **Async /remember**: Event appended synchronously (fast ACK), wiki integration runs in goroutine with timeout. Calls `Integrate()` via `agent.Runner` — the tool-use loop may take several LLM rounds.
- **Auto-compile**: `StartAutoCompile(ctx, interval)` spawns a `time.Ticker` goroutine. `Wait()` blocks until ticker stops. Set interval to 0 to disable.
- **Agent constructors** take `*agent.ToolExecutor` directly — no DI framework. Wired manually in `New()`.

## Testing

- **Mock LLM**: Implement `agent.LLM` with canned `LLMResponse`. See `handler_test.go:mockLLM`.
- **No testify** — pure `testing.T` with `t.Fatal/t.Errorf`.
- **Handler tests** cover all 4 endpoints, error paths, and compile cursor tracking.
- Mock LLM returns a single text response — tool-use patterns are tested in `internal/agent/compile_test.go`.

## Gotchas

- **`h.store` field is used by `Routes()` but `h.agents` is used by route handlers** — the Handler struct holds both. Don't bypass `h.agents` to access the store directly.
- **Ingest timeout applies to the ENTIRE tool-use loop**, not a single LLM call. If the IngestAgent needs 5 rounds of tool use at 30s each, it will hit the timeout.
- **`StartAutoCompile` panics on nil ctx** — the context is assumed valid. Pass `context.Background()` if unsure.
- **`Wait()` blocks forever if `StartAutoCompile` was never called** — the channel is nil until started.
