# engram9

**An OKF-compatible knowledge compiler for AI agents.**

engram9 turns raw conversations, repos, docs, and tool traces into git-native knowledge bundles that agents can read, cite, recall, and improve over time.

Knowledge bundles are plain Markdown files with YAML frontmatter, following the [Open Knowledge Format (OKF)](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md) spec. They live in your repo, can be `git diff`'d, reviewed in PRs, and consumed by any OKF-compatible tool.

> **Status**: The runtime is being migrated from a legacy wiki format (HTML-comment metadata, `[[wikilinks]]`) to OKF-compatible YAML frontmatter output. The schema, examples, and API described below reflect the target architecture. See [docs/okf-compatibility.md](docs/okf-compatibility.md) for the full OKF profile specification and which features are planned vs implemented.

## Why engram9?

AI agents lose context between sessions. Today's solutions have tradeoffs:

| Approach | How it works | Limitation |
|---|---|---|
| **Vector DB (RAG)** | Embed chunks, retrieve by cosine similarity | No reasoning over structure; retrieval degrades as corpus grows |
| **MEMORY.md** | Hand-maintained Markdown file | Doesn't scale; no automated consolidation; single-agent only |
| **Chat history replay** | Feed prior conversation back to context | Token-expensive; no distillation; irrelevant noise |
| **engram9** | LLM compiles raw events into structured, linked OKF knowledge pages | Scales via structured routing; knowledge improves over time; any agent can consume |

## How it works

engram9 uses three LLM-powered agents to manage a knowledge lifecycle:

```
Raw events (conversations, tool output, docs)
        |
        v
  +-----------+     +-----------+     +-----------+
  |  Ingest   | --> |   Query   | --> |  Compile  |
  |  Agent    |     |   Agent   |     |   Agent   |
  +-----------+     +-----------+     +-----------+
  Encode new        Recall +          Distill,
  facts into        reconstruct       consolidate,
  wiki pages        from wiki         prune, link
        |               |                 |
        +-------+-------+---------+-------+
                |                  |
                v                  v
     raw/events.jsonl         wiki/ (OKF bundle)
```

1. **Ingest**: New information is immediately woven into existing wiki pages, with source provenance tracked.
2. **Query**: Retrieval is reconstructive — the LLM reads a compact index, navigates to relevant pages, and synthesizes an answer with citations.
3. **Compile**: Periodic consolidation distills episodic knowledge into semantic facts, detects contradictions, prunes stale pages, and strengthens cross-links.

## OKF compatibility

Wiki pages follow the OKF schema — each page is a Markdown file with YAML frontmatter:

```markdown
---
type: concept
title: "Commit Queue State Machine"
description: "Drive9 FUSE commit queue lifecycle: delayed, dispatched, in-flight, done"
timestamp: "2026-06-16T10:00:00Z"
memory_type: semantic
source_events:
  - evt_042
  - evt_055
trust_tier: T1
---

# Commit Queue State Machine

The commit queue manages async uploads with a 4-state lifecycle...

Related: [Shadow Upload](../procedural/shadow-upload.md) | [Write Policy](../semantic/write-policy.md)
```

### Field classification

| Field | Requirement | Description |
|---|---|---|
| `type` | **OKF required** | Concept type (e.g., `concept`, `procedure`, `decision`) |
| `title` | Engram9 profile | Human-readable title |
| `description` | Engram9 profile | One-line summary |
| `timestamp` | Engram9 profile | When this knowledge was last compiled |
| `memory_type` | Engram9 profile | `semantic`, `episodic`, `procedural`, `prospective` |
| `source_events` | Engram9 profile | Raw event IDs that contributed to this page |
| `trust_tier` | Engram9 profile | `T1` (direct statement), `T2` (tool output), `T3` (second-hand) |

OKF consumers that don't understand engram9 profile fields will ignore them gracefully (per OKF spec). Engram9 profile fields enable richer recall, provenance tracking, and consolidation decisions.

### Links

Target output uses standard Markdown links:

```markdown
Related: [Alice](../semantic/people/alice.md)
```

Legacy `[[wikilink]]` syntax is planned for import/migration support (not yet implemented) and will not be used in new output.

## Wiki structure

```
wiki/
├── index.md              # Auto-generated routing table
├── semantic/             # Decontextualized facts (people, projects, APIs)
├── episodic/             # Contextual experiences (who/what/when/where)
├── procedural/           # How-to knowledge (runbooks, workflows, recipes)
├── prospective/          # Future intentions with trigger conditions
└── archive/              # Deprioritized pages (searchable, recoverable)
```

The memory taxonomy follows Tulving's classification (1972). The compile agent moves knowledge through this lifecycle: episodic experiences are distilled into semantic facts, which are cross-linked into procedural runbooks.

## Quick start

```bash
# Build
go build -o engram9 ./cmd/engram9

# Run with Anthropic
ANTHROPIC_API_KEY=sk-xxx ./engram9 -addr :9090 -data ./data

# Run with any OpenAI-compatible API
LLM_PROVIDER=openai OPENAI_API_KEY=xxx OPENAI_BASE_URL=https://your-api/v1 \
  ./engram9 -addr :9090 -data ./data -model your-model

# Validate an OKF bundle
./engram9 validate examples/repo-memory
./engram9 validate --strict examples/repo-memory
```

## API

```bash
# Store a memory
curl -X POST /remember \
  -d '{"text": "Alice prefers dark mode", "context": {"project": "ui"}}'

# Recall (reconstructive retrieval with citations)
curl -X POST /recall \
  -d '{"question": "What are Alice'\''s UI preferences?"}'

# Trigger consolidation
curl -X POST /compile -d '{}'

# System status
curl GET /status
```

## Design

engram9 draws from three sources:

- **[Open Knowledge Format (OKF)](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md)** — Standard for agent-readable knowledge as Markdown + YAML frontmatter
- **[Karpathy's LLM Wiki](https://x.com/karpathy)** — Raw/wiki separation, compile-to-Markdown, index routing, source tracing
- **Neuroscience** — Tulving's memory taxonomy, three-timing consolidation (encoding, retrieval, sleep), active forgetting

For the full design rationale, see [design/agent-memory-v5-design.md](design/agent-memory-v5-design.md).

## License

Apache-2.0 — see [LICENSE](LICENSE).
