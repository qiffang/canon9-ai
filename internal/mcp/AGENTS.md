# internal/mcp — MCP Protocol Server

## Overview
Implements the [MCP 2024-11-05](https://modelcontextprotocol.io/specification/2024-11-05) protocol as a JSON-RPC 2.0 server over stdio. Exposes OKF bundles or runtime stores as 5 MCP tools. Used by the standalone `engram9-mcp` binary.

## Structure
```
mcp/
├── server.go          JSON-RPC 2.0 server (initialize, tools/list, tools/call, ping)
├── tools.go           Tool definitions + execution dispatch
├── server_test.go     22 tests (largest test file in repo: 749 lines)
```

## Architecture

```
stdin (JSON-RPC 2.0) ──► mcp.Server.Serve(io.Reader, io.Writer)
                              │
                              ├── initialize (capability negotiation)
                              ├── tools/list (discovery)
                              ├── tools/call (dispatch to storage.Store)
                              │     ├── search_concepts(query)  → store.SearchWiki()
                              │     ├── read_concept(path)      → store.ReadWikiPage()
                              │     ├── neighbors()             → store.ReadWikiIndex()
                              │     ├── write_memory(text, ...) → store.AppendEvent()
                              │     └── memory_status()         → store.GetMemoryStats()
                              └── ping (keepalive)
```

## Two Operating Modes

| Mode | CLI Flag | Store Impl | Write Support |
|------|----------|------------|---------------|
| Runtime | `-data ./data` | `storage.FS` | Full read-write |
| Bundle | `-bundle ./path` | `storage.BundleFS` | Read-only (writes return errors) |

## Transport

- **stdin/stdout**: JSON-RPC 2.0 messages are newline-delimited JSON.
- **stderr**: All logging goes to stderr to keep stdout clean.
- **Requests vs Notifications**: `id` field presence distinguishes method calls (expect response) from notifications (fire-and-forget).

## Path Validation (Defense in Depth)

`validatePath()` in tools.go blocks:
- Absolute paths (`/etc/passwd` style)
- Parent traversal (`../`, `..\`)
- `.meta/` directory access (sidecar metadata should never be exposed via MCP)

## 5 MCP Tools

| Tool | Access | Notes |
|------|--------|-------|
| `search_concepts(query)` | Search all wiki pages | Returns `SearchResult[]` |
| `read_concept(path)` | Read specific page + metadata | Returns `WikiPage` |
| `neighbors()` | List all pages by category | Returns formatted index |
| `write_memory(text, actor?, source?)` | Store new info | Read-only in bundle mode |
| `memory_status()` | System statistics | Returns `MemoryStats` |

## Testing

- Largest test file: `server_test.go` (749 lines, 22 tests).
- Tests use `bytes.Buffer` as stdin/stdout injection.
- All tests are **procedural** (not table-driven) despite the large count — refactoring to table-driven would reduce duplication.
- JSON unmarshal errors in tests are silently discarded (~18 locations). Not critical for tests but makes debugging failures harder.

## Gotchas

- **`MCPTools` is an exported mutable slice** (tools.go). External packages can modify the tool list at runtime — subtle shared-state risk. If need to extend, prefer passing a tools list via constructor.
- **`mustJSON` panics** (tools.go:240) — same anti-pattern as agent/tooldef.go. A marshal failure in tool definition kills the process.
- **No version negotiation beyond `initialize`**: The server declares protocol version `2024-11-05` but doesn't validate the client's declared version.
