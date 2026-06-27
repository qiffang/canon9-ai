# openclaw-plugin — TypeScript Plugin for OpenClaw

## Overview
OpenClaw plugin that exposes engram9 as 4 agent tools + 3 lifecycle hooks. Pure TypeScript, zero runtime dependencies, no build step.

## Structure
```
openclaw-plugin/
├── index.ts                 Plugin entry (default export), registers tools + hooks
├── backend.ts               Engram9Backend interface (remember/recall/compile/status)
├── server-backend.ts        HTTP fetch-based backend (→ engram9 server)
├── hooks.ts                 Lifecycle hooks (prompt build, agent end, session reset)
├── types.ts                 TypeScript type definitions for API requests/responses
├── openclaw.plugin.json     Plugin manifest (id: "engram9", kind: "tool")
├── package.json             @engram9/openclaw-plugin v0.1.0, peer dep: openclaw
├── tsconfig.json            ES2022, bundler resolution, strict, declaration: true
└── .gitignore               node_modules/, dist/, package-lock.json
```

## Key Conventions

- **No build step** — raw `.ts` files consumed directly by OpenClaw runtime.
- **Typecheck only**: `npx tsc --noEmit` (no `build` script in package.json).
- **ESM only**: `"type": "module"` in package.json. No CommonJS.
- **Peer dep**: Requires `openclaw >=2026.1.26` (runtime host, not bundled).
- **Zero runtime deps**: All HTTP communication is hand-written fetch calls.
- **`moduleResolution: "bundler"`** in tsconfig.json — not `node` or `node16`.

## Backend Architecture

```
Plugin tools ──► Engram9Backend (interface)
                  │
                  └── ServerBackend (HTTP fetch)
                       POST http://localhost:9090/{endpoint}
                       └── apiKey in Authorization header (if configured)
```

Configurable via plugin settings in `openclaw.plugin.json`: `apiUrl` (default `http://localhost:9090`), `apiKey` (optional, sensitive), `agentName`.

## 4 Tools

| Tool | API Endpoint | Input | Output |
|------|-------------|-------|--------|
| `memory_remember` | POST /remember | `{text, context?}` | `{event_id}` |
| `memory_recall` | POST /recall | `{question, context?}` | Reconstructed answer with citations |
| `memory_compile` | POST /compile | `{}` | Consolidation triggered |
| `memory_status` | GET /status | — | `{event_count, wiki_page_count, ...}` |

## 3 Lifecycle Hooks

| Hook | Priority | Behavior |
|------|----------|----------|
| `before_prompt_build` | 50 | Recalls relevant memories, injects as `<engram9-recall>` context block |
| `agent_end` | — | Auto-remember user+assistant messages (max 200KB payload, max 20 messages) |
| `before_reset` | — | Saves session summary before context wipe |

## Auto-Remember Exclusions

The `agent_end` hook skips these messages:
- Tool output (shell results, diffs, JSON blocks) — matched by format pattern
- Cron/heartbeat-triggered runs
- Previously injected `<engram9-recall>` context blocks
- Messages that would exceed the 200KB payload limit

## Testing

**No tests exist** for this package. The TypeScript plugin is completely untested.

## Gotchas

- **`tsconfig.json` mismatches reality**: `outDir: "./dist"` and `declaration: true` suggest a build step, but none exists. The `dist/` directory is gitignored and never created. If adding a build step, align the config; if not, consider adding `noEmit: true`.
- **`package-lock.json` is gitignored** — not committed to the repo. For deterministic installs, this should be tracked.
- **No `npm install` in CI**: TypeScript typecheck is never verified in the GitHub Actions workflow.
- **Hardcoded default `apiUrl`**: `http://localhost:9090` is the fallback — must be overridden in production via OpenClaw config.
