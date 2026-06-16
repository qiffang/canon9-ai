---
type: procedure
title: "Getting Started with engram9"
description: "How to build, run, and use engram9 for agent knowledge management"
timestamp: "2026-06-16T12:00:00Z"
memory_type: procedural
source_events:
  - evt_003
trust_tier: T1
---

# Getting Started with engram9

## Build

```bash
go build -o engram9 ./cmd/engram9
```

## Run

```bash
# With Anthropic
ANTHROPIC_API_KEY=sk-xxx ./engram9 -addr :9090 -data ./data

# With OpenAI-compatible API
LLM_PROVIDER=openai OPENAI_API_KEY=xxx OPENAI_BASE_URL=https://your-api/v1 \
  ./engram9 -addr :9090 -data ./data -model your-model
```

## Use

```bash
# Store a memory
curl -X POST http://localhost:9090/remember \
  -d '{"text": "The commit queue uses a 4-state lifecycle", "context": {"project": "drive9"}}'

# Recall
curl -X POST http://localhost:9090/recall \
  -d '{"question": "How does the commit queue work?"}'

# Trigger consolidation
curl -X POST http://localhost:9090/compile -d '{}'
```

## Validate a knowledge bundle

```bash
# Basic validation (OKF required + engram9 profile required)
engram9 validate ./wiki/

# Strict validation (all warnings become errors)
engram9 validate --strict ./wiki/
```

Related: [Three-Timing Consolidation](../semantic/three-timing-consolidation.md)
