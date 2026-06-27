# PROJECT KNOWLEDGE BASE

**Generated:** 2026-06-26
**Commit:** 5afd912
**Branch:** fix/7-configurable-ingest-timeout

## OVERVIEW
engram9 is a Go-based OKF-compatible knowledge compiler for AI agents. Three LLM-powered agents (ingest, query, compile) turn raw events into structured, linked OKF knowledge bundles stored as Markdown + YAML frontmatter on the filesystem. Two binaries: HTTP server (engram9) + MCP stdio server (engram9-mcp). Single external dependency (yaml.v3). Also includes a TypeScript OpenClaw plugin.

## STRUCTURE
```
/
├── cmd/
│   ├── engram9/           # HTTP server binary (:9090 default)
│   └── engram9-mcp/       # MCP JSON-RPC 2.0 stdio server
├── internal/
│   ├── agent/             # 3 LLM agents (ingest/query/compile) + Runner loop
│   ├── api/               # HTTP handler — wires agents to storage, routes, auto-compile
│   ├── mcp/               # MCP protocol server (5 tools over stdio)
│   ├── okf/               # OKF bundle validation + legacy migration
│   └── storage/           # Store interface (15 methods) + FS + BundleFS impls
├── openclaw-plugin/       # TypeScript plugin (separate npm pkg, zero runtime deps)
├── design/                # Architecture docs
├── docs/                  # User-facing docs (OKF compatibility spec)
├── scripts/               # Shell scripts (launcher, PDF ingestion)
└── examples/              # Example OKF bundles
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| HTTP API routes | `internal/api/handler.go` | POST /remember, /recall, /compile; GET /status |
| MCP tool definitions | `internal/mcp/tools.go` | 5 tools: search_concepts, read_concept, neighbors, write_memory, memory_status |
| LLM agent logic | `internal/agent/` | IngestAgent, QueryAgent, CompileAgent + Runner (tool-use loop) |
| Agent—storage mapping | `internal/agent/tools.go` | ToolExecutor: maps 9 tool names → Store methods |
| Data persistence | `internal/storage/store.go` | Store interface; FS=read-write, BundleFS=read-only |
| OKF validation rules | `internal/okf/validator.go` | ValidateBundle(root, strict) |
| Legacy migration | `internal/okf/migration.go` | HTML comments + [[wikilinks]] → OKF YAML frontmatter |
| CLI entry points | `cmd/engram9/main.go`, `cmd/engram9-mcp/main.go` | Two binaries, shared internal/ packages |
| Plugin entry | `openclaw-plugin/index.ts` | 4 tools + 3 lifecycle hooks for OpenClaw |
| Architecture docs | `design/architecture.md` | Three-timing consolidation, LLM-as-PageRank, Tulving taxonomy |
| CI/CD | `.github/workflows/dev-cd.yml` | Push to main → ECR → EKS dev-dat9 |
| Tests | `make test` or `go test ./...` | All stdlib testing, no testify |

## CODE MAP

| Symbol | Type | Location | Refs | Role |
|--------|------|----------|------|------|
| `Store` | interface | `internal/storage/store.go` | 50+ | All data operations (15 methods) |
| `LLM` | interface | `internal/agent/llm.go` | 10+ | Single Call(ctx, req)→(*Response, error) |
| `Runner` | struct | `internal/agent/runner.go` | 4 | Tool-use loop (max 20 rounds) |
| `Handler` | struct | `internal/api/handler.go` | 3 | HTTP handler + auto-compile |
| `Server` | struct | `internal/mcp/server.go` | 2 | MCP JSON-RPC server |
| `FS` | struct | `internal/storage/fs.go` | 5 | Read-write filesystem store |
| `BundleFS` | struct | `internal/storage/bundle.go` | 3 | Read-only bundle consumer |
| `ToolExecutor` | struct | `internal/agent/tools.go` | 3 | Maps tool names → Store methods |
| `IngestAgent` | struct | `internal/agent/ingest.go` | 2 | Encode new memories into wiki |
| `QueryAgent` | struct | `internal/agent/query.go` | 2 | Reconstructive recall with citations |
| `CompileAgent` | struct | `internal/agent/compile.go` | 2 | Global consolidation, pruning, index rebuild |

## CONVENTIONS

- **No external test framework** — pure `testing.T` with `t.Fatal/t.Errorf`. No testify, no gomega.
- **Tests colocated** — `*_test.go` files in same package as production code. No `testdata/` directories; use `t.TempDir()` for ephemeral fixtures.
- **Mock via interface** — implement `agent.LLM` interface with canned responses (see `handler_test.go`, `compile_test.go`).
- **No build step for TS plugin** — raw `.ts` consumed by OpenClaw runtime. `tsc --noEmit` typecheck only.
- **LLM provider selection** — env var `LLM_PROVIDER=openai` switches to OpenAI-compatible; default is Anthropic.
- **Go 1.22+ route patterns** — `"POST /remember"` style in `http.NewServeMux`.
- **Event ID format** — `evt_YYYYMMDD_HHMMSS_rrrr` using `crypto/rand`.
- **Memory taxonomy** — semantic/episodic/procedural/prospective mapped to `data/wiki/` subdirectories.
- **Sidecar separation** — access telemetry in `.meta/*.json` kept separate from wiki `.md` content for clean git diffs.
- **OKF frontmatter** — all wiki pages use YAML frontmatter with engram9 profile fields (memory_type, trust_tier, source_events).
- **Single external Go dep** — only `gopkg.in/yaml.v3`. No router, no ORM, no CLI framework. Pure stdlib.

## ANTI-PATTERNS (THIS PROJECT)

- **`mustJSON` panics in library code** — `panic(err)` in `agent/tooldef.go:130`, `mcp/tools.go:240`, `agent/tools.go`. Library panic kills process. Return errors instead.
- **Silenced sidecar writes** — `_ = f.writeSidecar(...)` in `storage/fs.go` (4 locations). Telemetry loss goes undetected. Intentional for availability but should log.
- **Silenced Rel() failures** — `filepath.Rel` errors discarded in `storage/fs.go` (3 locations) and `storage/bundle.go` (2 locations). Returns empty string.
- **Hardcoded Anthropic model** — `"claude-sonnet-4-20250514"` is a const in `agent/llm.go`. Requires code edit to change.
- **TypeScript plugin untested** — `openclaw-plugin/` has zero tests. No CI typecheck step.
- **No `package-lock.json`** — gitignored in `openclaw-plugin/`. Not deterministic.
- **`tsconfig.json` mismatch** — `outDir: "./dist"` and `declaration: true` but no build step exists.
- **Exported mutable `MCPTools` slice** — `internal/mcp/tools.go:18`. External packages can modify tool list at runtime.
- **No linting config** — no `.golangci.yml`, no `eslint`, no pre-commit hooks.
- **Dockerfile copies everything** — `COPY . .` with no `.dockerignore`. Build context includes tmp/, claude-notes/, etc.

## COMMANDS

```bash
# Build
make build                           # go build -o engram9 ./cmd/engram9

# Test
make test                            # go test ./...
go test -tags smoke -run TestSmoke   # integration tests (needs ENGRAM9_URL)

# Run (Anthropic default)
ANTHROPIC_API_KEY=sk-xxx ./engram9 -addr :9090 -data ./data

# Run (OpenAI-compatible)
LLM_PROVIDER=openai OPENAI_API_KEY=xxx OPENAI_BASE_URL=https://api/v1 \
  ./engram9 -addr :9090 -data ./data -model gpt-4

# OKF CLI
./engram9 validate [--strict] <bundle-dir>
./engram9 migrate-okf [--write] [--check] <bundle-dir>

# MCP server
./engram9-mcp -data ./data           # runtime (read-write)
./engram9-mcp -bundle ./path         # bundle (read-only)

# Docker
make docker-build
docker run -e ANTHROPIC_API_KEY=sk-xxx -p 9090:9090 engram9:latest

# TypeScript plugin
cd openclaw-plugin && npx tsc --noEmit   # typecheck only
```

## NOTES

- **Go 1.26.1** — bleeding edge. The current stable release may differ.
- **Package-level AGENTS.md** — `internal/agent/`, `internal/mcp/`, `internal/okf/`, `internal/storage/`, `internal/api/`, `openclaw-plugin/` each have per-package AGENTS.md with detailed gotchas.
- **OKF import/export CLI** (`engram9 export okf` / `engram9 import okf`) is deferred per docs/okf-compatibility.md.
- **Runner is synchronous** — `Runner.Round()` blocks until all LLM + tool rounds complete (max 20). No yielding between rounds.
- **Compile cursor is uint64** — stored as decimal in `data/raw/cursor`. No atomic increments; serialized by compile agent mutex.
- **Only `AppendEvent` is thread-safe** — storage mutex only protects event log. Callers coordinate ReadWikiPage/WriteWikiPage access.
