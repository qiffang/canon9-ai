---
type: concept
title: "OKF Compatibility"
description: "How engram9 knowledge pages map to the Open Knowledge Format spec"
timestamp: "2026-06-16T12:00:00Z"
memory_type: semantic
source_events:
  - evt_002
trust_tier: T1
---

# OKF Compatibility

engram9 wiki pages are OKF-compatible Markdown files. The only OKF-required field is `type`; engram9 adds profile-specific fields (`title`, `description`, `timestamp`, `memory_type`, `source_events`, `trust_tier`) that external OKF consumers ignore gracefully.

Key mapping:
- `type` values: `concept`, `procedure`, `decision`, `person`, `project`, `event`
- Links: standard Markdown `[text](path.md)`, not `[[wikilinks]]`
- Bundle structure: `index.md` + `semantic/` + `episodic/` + `procedural/` + `prospective/` + `archive/`

See [docs/okf-compatibility.md](../../docs/okf-compatibility.md) for the full schema specification.

Related: [Three-Timing Consolidation](three-timing-consolidation.md)
