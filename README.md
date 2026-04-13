# engram9

**A brain-inspired memory system for AI agents — no vectors, no full-text search, just LLM + structured wiki.**

engram9 gives AI agents a long-term memory that works like the human brain: new information is immediately woven into an evolving knowledge graph of Markdown pages, recalled through reconstructive retrieval, and consolidated during periodic "sleep cycles" that distill, prune, and strengthen memories over time.

Unlike vector databases or RAG pipelines, engram9 uses **LLM-as-PageRank**: the language model itself acts as the ranking, routing, and consolidation engine — reading a compact wiki index to navigate directly to relevant knowledge pages, much like PageRank routes web traffic through link structure.

## Why not vectors or full-text search?

| Approach | How it retrieves | Limitation |
|---|---|---|
| **Vector DB (RAG)** | Cosine similarity on embeddings | Shallow semantic match; no reasoning over structure; retrieval quality degrades as corpus grows |
| **Full-text search** | BM25 / keyword match | No semantic understanding; misses paraphrases and implicit connections |
| **engram9** | LLM reads a wiki index → navigates to pages → reconstructs answer | Scales via structured routing, not brute-force similarity; knowledge improves over time through consolidation |

The wiki index acts as a **learned routing table** — analogous to PageRank's link graph — letting the LLM jump directly to the 1–3 most relevant pages instead of scanning thousands of chunks. As knowledge accumulates, the compile agent restructures and cross-links pages, continuously improving retrieval quality.

## Architecture

```
┌─────────────────────────────────────────────────┐
│                  engram9 server                  │
│                                                  │
│   POST /remember    POST /recall    POST /compile│
│                                                  │
│   ┌────────────┐ ┌────────────┐ ┌────────────┐  │
│   │   Ingest   │ │   Query    │ │  Compile   │  │
│   │   Agent    │ │   Agent    │ │   Agent    │  │
│   │            │ │            │ │            │  │
│   │ encode +   │ │ recall +   │ │ distill +  │  │
│   │ weave into │ │ reconstruct│ │ prune +    │  │
│   │ wiki       │ │ from wiki  │ │ consolidate│  │
│   └─────┬──────┘ └─────┬──────┘ └─────┬──────┘  │
│         └───────────────┼──────────────┘         │
│                         ▼                        │
│    ┌──────────────────────────────────────────┐  │
│    │  raw/events.jsonl    wiki/ (Markdown)    │  │
│    └──────────────────────────────────────────┘  │
└─────────────────────────────────────────────────┘
```

### Three-timing consolidation (inspired by neuroscience)

The human brain doesn't consolidate memory at a single point — it does so at **three distinct moments**, each at a different granularity:

| Timing | Brain analogy | What engram9 does | Agent |
|---|---|---|---|
| **Encoding** | Hippocampus immediately links new info to existing networks | Ingest agent reads wiki index, weaves new facts into 1–3 related pages | `ingest_agent` |
| **Retrieval** | Every recall is reconstruction, not playback; strengthens connections | Query agent synthesizes across pages, fixes errors, archives new insights | `query_agent` |
| **Sleep** | Hippocampal replay → neocortex; episodic → semantic distillation | Compile agent globally distills, detects contradictions, prunes stale pages | `compile_agent` |

### Wiki structure (memory taxonomy)

```
wiki/
├── index.md              # Auto-generated routing table (the "PageRank")
├── semantic/             # Decontextualized knowledge (facts, people, projects)
├── episodic/             # Contextual experiences (who/what/when/where)
├── procedural/           # How-to knowledge (runbooks, workflows)
├── prospective/          # Future intentions with trigger conditions
└── archive/              # Forgotten pages (searchable, recoverable)
```

Each page type mirrors a distinct memory system from Tulving's taxonomy (1972):

- **semantic/** — "Paris is the capital of France" — persists indefinitely
- **episodic/** — "Had lunch with Alice on April 13" — distilled into semantic, then archived
- **procedural/** — "How to deploy TiKV" — extremely durable, rarely forgotten
- **prospective/** — "Remind me to tell Alice when v2 ships" — auto-triggered on context match
- **archive/** — Not deleted, just deprioritized — still reachable via `search_wiki()`

### Forgetting is a feature

engram9 implements active forgetting modeled after the brain's synaptic homeostasis hypothesis (Tononi & Cirelli, 2003):

- **Episodic pages** are aggressively pruned after their core knowledge is distilled into semantic pages
- **Semantic pages** are never archived due to inactivity — only when explicitly superseded
- **Retrieval strengthens memory** — each `recall` updates access patterns in sidecar metadata, influencing future compile decisions
- **Archive ≠ delete** — archived pages are removed from the index but remain searchable, just like how "forgotten" memories can resurface with the right cue

## Deployment modes

engram9 is designed to be flexible:

### Mode 1: Standalone server

Run as an independent memory service with its own LLM:

```bash
# Using Anthropic
ANTHROPIC_API_KEY=sk-xxx ./engram9 -addr :9090 -data ./data

# Using any OpenAI-compatible API (Qwen, DeepSeek, local models, etc.)
LLM_PROVIDER=openai OPENAI_API_KEY=xxx OPENAI_BASE_URL=https://your-api/v1 \
  ./engram9 -addr :9090 -data ./data -model your-model
```

### Mode 2: Agent-local memory

Embed engram9 as a local memory layer for your AI agent, using the agent's own LLM:

```bash
# Your agent's LLM handles both reasoning and memory consolidation
LLM_PROVIDER=openai OPENAI_BASE_URL=http://localhost:11434/v1 \
  ./engram9 -data ./agent-memory -model llama3
```

### Mode 3: Shared memory service

Deploy once, let multiple agents share a memory:

```bash
# Deploy on Kubernetes / ECS / Fly.io
# Multiple agents POST to the same /remember and /recall endpoints
curl -X POST https://memory.internal/remember \
  -d '{"text": "...", "context": {"agent": "coder", "project": "engram9"}}'
```

## API

```bash
# Store a memory (returns instantly, wiki integration runs async)
curl -X POST /remember \
  -d '{"text": "Alice prefers dark mode", "context": {"project": "ui"}}'
# → {"event_id": "evt_20260413_143022_a7f3"}

# Recall (reconstructive retrieval, not keyword search)
curl -X POST /recall \
  -d '{"question": "What are Alice'\''s UI preferences?"}'
# → {"result": "Alice prefers dark mode [evt_xxx T1]. No other UI preferences recorded."}

# Trigger sleep consolidation
curl -X POST /compile -d '{}'

# System status
curl GET /status
# → {"event_count": 42, "wiki_page_count": 12, "pending_integrations": 0, ...}
```

## How it works: LLM-as-PageRank

Traditional search engines use PageRank to determine which pages are most authoritative based on link structure. engram9 applies the same principle, but with an LLM as the ranking engine:

1. **Index = Link graph.** The auto-generated `index.md` is a compact routing table listing every active wiki page with a one-line description. This is the LLM's "link graph."

2. **LLM = PageRank.** When a query arrives, the LLM reads the index and decides which 1–3 pages are most relevant — effectively computing a query-specific PageRank in a single inference step.

3. **Cross-references = Backlinks.** Wiki pages link to each other with `[[semantic/people/alice.md]]` notation. The compile agent continuously discovers and adds missing cross-references, strengthening the link graph over time.

4. **Consolidation = Link refinement.** Each compile cycle restructures the wiki: merging redundant pages, splitting oversized pages, adding cross-references, and archiving stale content. This continuously improves the routing quality — analogous to how PageRank improves as the web's link structure matures.

5. **Access patterns = Click-through data.** Every page read updates sidecar metadata (`access_dates`, `last_accessed`). The compile agent uses this signal to decide what to keep and what to archive — similar to how search engines use click-through rates to refine rankings.

## Source trust & provenance

Every fact in the wiki is traceable to its source event:

```markdown
## Table Design
- Uses partition tables [evt_042 T1] [evt_055 T2]
- Benchmarked at 3x faster for large queries [evt_061 T1]
- First proposed by [[semantic/people/alice.md]] [evt_042]
```

Trust tiers: **T1** = user direct statement, **T2** = tool output / inference, **T3** = second-hand.

## Building

```bash
go build -o engram9 ./cmd/engram9
go test ./...
```

## Docker

```bash
make docker-build
# or
docker build -t engram9 .
```

## Inspiration

engram9 synthesizes ideas from three sources:

- **[Karpathy's LLM Wiki](https://x.com/karpathy)** — Raw/wiki separation, compile-to-Markdown, index routing, source tracing
- **[Google Always-On Memory Agent](https://github.com/GoogleCloudPlatform/generative-ai/tree/main/gemini/agents/always-on-memory-agent)** — Multi-agent orchestration, background consolidation, tool-function API boundaries
- **Neuroscience** — Tulving's memory taxonomy, Ebbinghaus forgetting curve, Nader's reconsolidation theory, Tononi's synaptic homeostasis hypothesis

## License

MIT
