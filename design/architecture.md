# engram9 — Architecture & Design

## Context

This design synthesizes best practices from three sources:

- **Google Always-On Memory Agent**: Multi-agent orchestration, tool-function API boundaries, background consolidation, stateless requests, LLM importance scoring
- **Karpathy LLM Wiki**: Raw/wiki separation, compile-to-Markdown, index routing, source tracing, query-time wiki maintenance
- **Neuroscience of human memory**: Episodic/semantic separation, contextual encoding, prospective memory, distillation-based consolidation, multi-dimensional tagging, reconsolidation

---

## System Architecture

engram9 is a **complete LLM agent service** with three internal agents (ingest, query, compile) that expose a high-level API.

### External API

```
# High-level API (for AI agents, web apps, plugins)
POST /remember   { text, context? }     → Ingest agent encodes + weaves into wiki
POST /recall     { question, context? } → Query agent reconstructs answer from wiki
POST /compile    {}                     → Compile agent runs global consolidation
GET  /status                            → System statistics
```

### Internal Architecture

```
External callers (AI agents / web apps / CLI)
    │
    ▼
┌──────────────────────────────────────────────────────┐
│                    engram9 server                     │
│                                                      │
│  High-level API: remember / recall / compile / status│
│                                                      │
│  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐ │
│  │ ingest_agent │ │ query_agent  │ │compile_agent │ │
│  │ (LLM)        │ │ (LLM)        │ │ (LLM)        │ │
│  │              │ │              │ │              │ │
│  │ write: raw   │ │ read: wiki   │ │ read: raw    │ │
│  │ read/write:  │ │ write: wiki  │ │ read/write:  │ │
│  │   wiki       │ │ (fixes only) │ │   wiki       │ │
│  └──────┬───────┘ └──────┬───────┘ └──────┬───────┘ │
│         │                │                 │         │
│         ▼                ▼                 ▼         │
│      ┌─────────────────────────────────────────┐    │
│      │  raw/ (append-only event log)           │    │
│      │  wiki/ (LLM-compiled Markdown pages)    │    │
│      └─────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────┘
```

---

## Core Design: Three-Timing Consolidation

### How the human brain consolidates memory

The brain does NOT have a single "background compiler." Memory consolidation happens at **three distinct moments**, each at a different granularity:

1. **At encoding** — The hippocampus immediately links new information to existing knowledge networks. New info is not stored in isolation; it is woven into the existing web at the moment of perception.

2. **At retrieval** — Every recall is reconstruction, not playback. The brain reassembles an answer from fragments in the current context. Nader (2000): recalled memories become labile and are re-stored in updated form. Frequently recalled knowledge becomes progressively stronger and more structured.

3. **During sleep** — Hippocampal replay to neocortex. Multiple episodic experiences are compressed into semantic knowledge. This is the only truly asynchronous phase.

### Mapping to system design

| Timing | Brain mechanism | System behavior | Agent | Scope |
|---|---|---|---|---|
| **Encoding** | Hippocampus links to existing network | Read wiki index → weave into 1–3 related pages | `ingest_agent` | Local (1–3 pages) |
| **Retrieval** | Reconstruction + reconsolidation | Synthesize across pages; fix errors, add cross-refs | `query_agent` | Opportunistic |
| **Sleep** | Episodic → semantic distillation | Global distillation + contradiction detection + pruning | `compile_agent` | Global (all unprocessed events) |

### Why not just async compile?

A system that only compiles during "sleep" is like a person who:
- Hears information → writes it down verbatim, no thinking, no association
- Gets asked a question → flips through notebook, no new understanding
- Only organizes anything during sleep

That's not memory — it's a tape recorder with a nightly organizer. Real memory requires all three timings.

---

## Memory Taxonomy

Based on Tulving's five memory systems (1972):

| Type | Content | Brain analog | Forgetting behavior |
|---|---|---|---|
| **semantic/** | Decontextualized facts, preferences, knowledge | Semantic memory ("Paris is the capital") | Almost never — only when explicitly superseded |
| **episodic/** | Experiences with full context (who/what/when/where) | Episodic memory ("Lunch with Alice, it was raining") | Aggressively pruned after distillation into semantic |
| **procedural/** | How-to steps, workflows, runbooks | Procedural memory ("How to ride a bike") | Extremely persistent — only deprecated tools |
| **prospective/** | Future intentions with trigger conditions | Prospective memory ("Tell Alice when v2 ships") | No decay — completed or cancelled |
| **archive/** | Forgotten pages (still searchable) | Suppressed memories (recoverable with right cue) | Permanent cold storage |

### Wiki directory structure

```
wiki/
├── index.md                          # Auto-generated routing table
├── .meta/                            # Sidecar metadata (mirrors wiki structure)
│   ├── semantic/projects/db9.json
│   └── episodic/2026-04-12/meeting.json
├── semantic/
│   ├── preferences/
│   ├── people/
│   └── projects/
├── episodic/
│   └── 2026-04-12/
├── procedural/
├── prospective/
└── archive/                          # Archived pages (removed from index)
    ├── episodic/
    ├── procedural/
    └── semantic/
```

---

## LLM-as-PageRank: The Retrieval Model

Traditional memory systems retrieve by similarity (vectors) or keyword match (BM25). engram9 uses a fundamentally different approach: **the LLM itself is the ranking engine**.

### How it works

1. **Index as link graph.** `index.md` is a compact routing table listing every active page with a description. This serves the same role as PageRank's link graph — it tells the LLM what knowledge exists and where.

2. **Query-specific ranking.** When a query arrives, the LLM reads the index and selects the 1–3 most relevant pages. This is equivalent to computing a query-specific PageRank in a single inference step — but with semantic understanding that no formula can match.

3. **Cross-references as backlinks.** Wiki pages link to each other: `[[semantic/people/alice.md]]`. The compile agent continuously discovers missing cross-references, strengthening the graph.

4. **Consolidation as link refinement.** Each compile cycle restructures the wiki: merging, splitting, cross-linking, archiving. This improves routing quality over time — analogous to PageRank improving as the web's link structure matures.

5. **Access patterns as click-through signal.** Sidecar metadata tracks `access_dates` and `last_accessed`. The compile agent uses this to decide what to keep vs. archive — like search engines using CTR to refine rankings.

### Advantages over vector search

| Dimension | Vector search | LLM-as-PageRank |
|---|---|---|
| **Retrieval** | Top-K cosine similarity | LLM reads index → navigates to specific pages |
| **Reasoning** | None — retrieval is a separate step from reasoning | Retrieval IS reasoning — same LLM call |
| **Knowledge evolution** | Embeddings are static snapshots | Wiki continuously restructured by compile agent |
| **Explainability** | "Similarity = 0.87" | Every fact cites `[evt_xxx T1]` with trust tier |
| **Scalability** | O(n) chunks to scan | O(1) index read → O(k) page reads (k ≈ 1–3) |

### PageRank optimization opportunities

Several enhancements could make the routing even more effective:

1. **Weighted cross-references.** Currently all `[[links]]` are equal. Adding weights (based on co-access frequency or source trust) would let the compile agent produce a more informative index.

2. **Hub/authority scoring.** Pages that are heavily cross-referenced (authorities) could be surfaced first in the index. Pages that link to many others (hubs) could serve as category entry points.

3. **Query-aware index.** The current index is static. A dynamic index that adapts sections based on recent query patterns would improve routing for frequently-asked topics.

4. **Link-based page importance.** During sleep pruning, pages with many inbound cross-references should be more resistant to archiving — they are structurally important to the knowledge graph.

5. **Dangling page detection.** Pages with zero inbound links are "dangling nodes" in PageRank terms. The compile agent already detects orphans; this could be formalized as a PageRank-based importance score.

---

## Raw Event Schema

```json
{
  "id": "evt_20260412_143022_a7f3",
  "timestamp": "2026-04-12T14:30:22Z",
  "actor": "user",
  "content": "Alice suggests partition tables for db9",
  "source": "conversation",
  "session_id": "sess_abc123",
  "active_project": "db9",
  "active_task": "schema design",
  "durability": "long-term",
  "actionability": "actionable",
  "source_type": "user",
  "evidence_kind": "user_statement",
  "trust_tier": 1
}
```

### Tagging dimensions

- **durability**: `ephemeral` / `session` / `long-term` / `permanent` — determines if/when wiki integration occurs
- **actionability**: `none` / `informational` / `actionable` / `urgent` — influences prospective memory creation
- **trust_tier**: 1 (user direct statement) → 2 (tool output, inference) → 3 (second-hand)

---

## Source Trust & Provenance

Every semantic fact in the wiki preserves its source chain:

```markdown
## Table Design
- Uses partition tables [evt_042 T1] [evt_055 T2] [evt_061 T1]
  (3 sources, trust range T1–T2)
- First proposed by [[semantic/people/alice.md]] [evt_042]
```

Trust tiers:
- **T1** — High trust: user direct statement
- **T2** — Medium trust: tool output, LLM inference
- **T3** — Low trust: second-hand information

---

## Sidecar Metadata

Each wiki page has a companion `.meta/` JSON file tracking telemetry data separately from content:

```json
{
  "created_at": "2026-04-01T10:00:00Z",
  "last_accessed": "2026-04-12T15:00:00Z",
  "access_dates": ["2026-04-01", "2026-04-03", "2026-04-08", "2026-04-12"],
  "source_events": ["evt_042", "evt_055", "evt_061"],
  "trust_tier_max": 1,
  "memory_type": "semantic"
}
```

Key design decision: sidecar is separate from wiki content because access telemetry changes frequently while wiki content changes rarely. This keeps diffs clean and prevents metadata churn from polluting the knowledge base.

---

## Forgetting & Pruning

### Neuroscience foundation

Forgetting is not memory failure — it is a core feature of a healthy memory system:

- **Time decay + retrieval strengthening** (Ebbinghaus 1885, Roediger & Karpicke 2006): Memories decay exponentially, but each successful retrieval significantly strengthens them. Spaced retrieval is more effective than massed retrieval.
- **Active forgetting** (Davis & Zhong 2017): The brain has dedicated forgetting mechanisms — dopaminergic neurons actively disassemble synaptic connections via Rac1 signaling.
- **Semanticization**: Long-term survival depends on episodic → semantic transformation. Details are lost; core meaning is preserved.
- **Synaptic homeostasis** (Tononi & Cirelli 2003): Sleep globally downscales synaptic strength. Strong synapses are protected; weak ones are pruned. Result: improved signal-to-noise ratio.

### Implementation: LLM judgment, not formulas

Key design decision: we do NOT use Ebbinghaus curves or mechanical scoring to compute decay. Wiki pages are not synapses — they don't degrade by not being read. Instead, the **compile agent (LLM) makes judgment calls** based on factual data from sidecar metadata.

### Per-type pruning rules

| Type | Pruning behavior | Archive condition |
|---|---|---|
| **semantic/** | Never archived due to inactivity | Only when explicitly superseded or deduplicated |
| **episodic/** | Aggressively pruned | Core distilled into semantic → archive |
| **procedural/** | Extremely persistent | Tool/process deprecated or replaced |
| **prospective/** | No decay | Intention completed, cancelled, or obsolete |

### Archive ≠ delete

Archived pages are moved to `archive/`, removed from `index.md`, but remain searchable via `search_wiki()`. This mirrors how "forgotten" memories can resurface with the right contextual cue.

---

## Tool Functions (9 total)

| Tool | Used by | Description |
|---|---|---|
| `append_event(...)` | ingest | Write event to append-only log |
| `read_events_since(cursor)` | compile | Read unprocessed events |
| `read_wiki_index()` | all | Read the routing table |
| `read_wiki_page(path)` | all | Read page + sidecar (auto-updates access metadata) |
| `search_wiki(query)` | query, compile | Text search across all pages including archive |
| `write_wiki_page(path, content)` | all | Create/update page (auto-creates sidecar) |
| `archive_wiki_page(path, reason)` | compile | Move to archive, update sidecar, remove from index |
| `rebuild_index()` | compile | Full rescan of active pages → regenerate index.md |
| `get_memory_stats()` | status | Event counts, page counts, pending integrations |

### Agent tool permissions

| Agent | Tools | Read | Write |
|---|---|---|---|
| `ingest_agent` | append_event, read_wiki_index, read_wiki_page, write_wiki_page, search_wiki | wiki | raw + wiki |
| `query_agent` | read_wiki_index, read_wiki_page, search_wiki, write_wiki_page | wiki | wiki (fixes only) |
| `compile_agent` | read_events_since, read_wiki_index, read_wiki_page, write_wiki_page, archive_wiki_page, rebuild_index, search_wiki | raw + wiki | wiki + archive |

---

## Async /remember Design

The `/remember` endpoint is split into sync + async for fast response times:

1. **Synchronous**: `AppendEvent()` writes the raw event immediately → returns `{"event_id": "evt_xxx"}`
2. **Asynchronous**: A background goroutine runs `Integrate()` to weave the event into wiki pages
3. **Read-after-write safety**: When integrations are pending, `/recall` injects the last 10 raw events into the query context so the LLM can answer even before wiki integration completes

---

## Data Directory Layout

```
data/
├── raw/
│   ├── events.jsonl        # Append-only event log
│   └── cursor              # Compile progress cursor
└── wiki/
    ├── index.md            # Auto-generated routing table
    ├── .meta/              # Sidecar metadata
    ├── semantic/
    ├── episodic/
    ├── procedural/
    ├── prospective/
    └── archive/
```

---

## References

- Tulving, E. (1972). Episodic and semantic memory.
- Tulving, E. (1973). Encoding specificity principle.
- Ebbinghaus, H. (1885). Memory: A contribution to experimental psychology.
- Bartlett, F. (1932). Remembering: A study in experimental and social psychology.
- Nader, K. (2000). Memory reconsolidation.
- Roediger, H. & Karpicke, J. (2006). The testing effect.
- Tononi, G. & Cirelli, C. (2003). Synaptic homeostasis hypothesis.
- Davis, R. & Zhong, Y. (2017). Active forgetting via Rac1 signaling.
