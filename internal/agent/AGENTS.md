# internal/agent — LLM Agents & Tool-Use Loop

## Overview
Three brain-inspired agents (ingest, query, compile) sharing a common `Runner` tool-use loop and `LLM` abstraction. No reflection, no dynamic dispatch, no code generation.

## Structure
```
agent/
├── llm.go              LLM interface + AnthropicLLM
├── llm_openai.go        OpenAI-compatible adapter (translates internal↔OpenAI formats)
├── runner.go            Tool-use loop driver (max 20 rounds)
├── tooldef.go           Tool JSON schemas + per-agent tool sets
├── tools.go             ToolExecutor: maps tool names → Store methods
├── ingest.go            IngestAgent: encode new memories into wiki
├── query.go             QueryAgent: reconstructive recall with citations
├── compile.go           CompileAgent: global consolidation, pruning, index rebuild
├── compile_test.go      Cursor tracking tests (mock LLM)
```

## Key Types

| Type | File | Purpose |
|------|------|---------|
| `LLM` (interface) | llm.go | Single `Call(ctx, LLMRequest) → (*LLMResponse, error)` method |
| `AnthropicLLM` | llm.go | POSTs to api.anthropic.com/v1/messages |
| `OpenAILLM` | llm_openai.go | Converts Anthropic-style internal format ↔ OpenAI ChatCompletions |
| `Runner` | runner.go | Tool-use loop: call LLM → execute tools → feed results → repeat (max 20) |
| `ToolExecutor` | tools.go | 9 methods directly mapping to storage.Store |
| `IngestAgent`, `QueryAgent`, `CompileAgent` | ingest.go, query.go, compile.go | Each embeds a Runner with specific tool set |

## Agent→Tool Mapping

| Agent | Tools |
|-------|-------|
| Ingest | append_event, read_wiki_index, read_wiki_page, write_wiki_page, search_wiki |
| Query | read_wiki_index, read_wiki_page, search_wiki, write_wiki_page |
| Compile | read_events_since, read_wiki_index, read_wiki_page, write_wiki_page, archive_wiki_page, rebuild_index, search_wiki |

## Testing

- **Mock LLM**: Implement `agent.LLM` with canned `LLMResponse`. See compile_test.go (`toolUseLLM` struct) and api/handler_test.go (`mockLLM` struct).
- **No testify** — all tests use standard `testing.T` with `t.Fatal/t.Errorf`.
- Refer to `../api/handler_test.go` for the full wiring test pattern (Handler→agents→Store).

## Gotchas

- **`mustJSON` panics**: Two helper functions (`tooldef.go:130`, `tools.go:240`) call `panic(err)` on marshal failure. These are in non-main packages — do NOT cargo-cult this pattern. A panic in a library kills the process with no recovery.
- **Anthropic model hardcoded**: `claude-sonnet-4-20250514` is a const in llm.go. Changing models requires a code edit, not a CLI flag.
- **OpenAI adapter is a translation layer**: `OpenAILLM` converts Anthropic's tool-use schema to OpenAI format. The system prompt and tool JSON schemas stay in Anthropic format regardless of provider.
- **Runner.Round() is synchronous per call**: It does NOT yield between LLM calls and tool execution. The entire loop blocks until complete or max rounds hit.
- **Agent constructors take `*ToolExecutor` directly**: No dependency injection framework. Wire manually as seen in api/handler.go.
