# OKF Compatibility

This document describes how engram9 knowledge pages will map to the [Open Knowledge Format (OKF)](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md) spec. The schema below is the target profile; the runtime is being migrated from a legacy format (HTML-comment metadata, `[[wikilinks]]`) to OKF-compatible YAML frontmatter.

## OKF overview

OKF defines a standard for agent-readable knowledge as directories of Markdown files with YAML frontmatter. The spec is intentionally minimal:

- Each knowledge file is a `.md` file with YAML frontmatter
- The only **required** frontmatter field is `type`
- All other fields are optional / profile-specific
- Consumers MUST tolerate unknown frontmatter fields
- Links use standard Markdown link syntax

## Engram9 OKF profile

Engram9 defines a profile on top of OKF with additional fields for memory lifecycle management. These fields are **engram9 profile required** but not OKF required — external OKF consumers will ignore them gracefully.

### Frontmatter schema

```yaml
---
# OKF required
type: concept              # concept | procedure | decision | person | project | event | index

# Engram9 profile required
title: "Human-readable title"
description: "One-line summary for index routing"
timestamp: "2026-06-16T10:00:00Z"    # ISO 8601, last compiled time
memory_type: semantic                 # semantic | episodic | procedural | prospective

# Engram9 profile recommended
source_events:                        # Raw event IDs that contributed
  - evt_042
  - evt_055
trust_tier: T1                        # T1 = direct statement, T2 = tool output, T3 = second-hand
confidence: high                      # high | medium | low (compile agent assessment)
supersedes: []                        # Paths of pages this one replaces
contradicts: []                       # Paths of pages with conflicting information
---
```

### Field definitions

| Field | Level | Type | Description |
|---|---|---|---|
| `type` | OKF required | string | Page type. Recognized values: `concept`, `procedure`, `decision`, `person`, `project`, `event`, `index` |
| `title` | Engram9 required | string | Human-readable page title |
| `description` | Engram9 required | string | One-line summary used by the query agent for index routing |
| `timestamp` | Engram9 required | string (ISO 8601) | When this page was last compiled/updated |
| `memory_type` | Engram9 required | string | Memory taxonomy classification: `semantic` (facts), `episodic` (experiences), `procedural` (how-to), `prospective` (future intentions) |
| `source_events` | Engram9 recommended | list of strings | Raw event IDs (`evt_xxx`) that contributed to this page |
| `trust_tier` | Engram9 recommended | string | Source trust: `T1` (user direct statement), `T2` (tool output / inference), `T3` (second-hand / hearsay) |
| `confidence` | Engram9 optional | string | Compile agent's assessment of knowledge reliability: `high`, `medium`, `low` |
| `supersedes` | Engram9 optional | list of strings | Relative paths of pages this one replaces |
| `contradicts` | Engram9 optional | list of strings | Relative paths of pages with conflicting information |

### Type values

| Type | Description | Example |
|---|---|---|
| `concept` | A semantic fact, entity, or idea | "Commit Queue State Machine" |
| `procedure` | A how-to, runbook, or workflow | "How to run W5 benchmark" |
| `decision` | An architectural or design decision with rationale | "Why we chose writeback mode" |
| `person` | A person profile | "Alice — backend engineer" |
| `project` | A project or component description | "Drive9 FUSE mount" |
| `event` | A specific occurrence with context | "PR #565 review — found force-due race" |
| `index` | A routing table for a knowledge bundle or sub-bundle | "engram9 Knowledge Index" |

## Link format

### Canonical (target output)

Standard Markdown links with relative paths:

```markdown
See [Commit Queue](../semantic/commit-queue.md) for details.
Related: [Shadow Upload](../procedural/shadow-upload.md)
```

### Legacy (planned import/migration)

Wiki-style links will be accepted during import and converted to standard Markdown links (not yet implemented):

```markdown
<!-- Legacy format (accepted on import) -->
See [[semantic/commit-queue.md]] for details.

<!-- Converted to canonical format -->
See [Commit Queue](../semantic/commit-queue.md) for details.
```

## Validation rules

The `engram9 validate` subcommand checks a knowledge bundle against this profile:

### Errors (hard fail)

- Missing `type` field (OKF violation)
- Invalid `timestamp` format (not ISO 8601)
- Invalid `memory_type` value (not in allowed set)
- Invalid `trust_tier` value when present (not `T1`, `T2`, or `T3`)
- Invalid `confidence` value when present (not `high`, `medium`, or `low`)

Note: unknown `type` values are **not** a hard error — OKF does not restrict `type` to a closed set. Engram9 recognizes the values listed above; unknown types are accepted but produce a warning in `--strict` mode.

### Warnings (soft, promoted to error with `--strict`)

- Missing engram9 profile required fields (`title`, `description`, `timestamp`, `memory_type`)
- Broken internal links (target file does not exist)
- Missing `source_events` (no provenance)
- Missing `trust_tier` (no source trust tier)

### Tolerated (per OKF spec)

- Unknown frontmatter fields from other profiles
- Structural `index.md` files without frontmatter (links are still checked)
- Missing optional fields (`confidence`, `supersedes`, `contradicts`)
- External links (not validated)

## Bundle structure

A valid engram9 OKF bundle:

```
knowledge-bundle/
├── index.md                    # Routing table (auto-generated)
├── semantic/
│   ├── commit-queue.md         # type: concept, memory_type: semantic
│   └── people/
│       └── alice.md            # type: person, memory_type: semantic
├── episodic/
│   └── pr-565-review.md        # type: event, memory_type: episodic
├── procedural/
│   └── run-benchmark.md        # type: procedure, memory_type: procedural
├── prospective/
│   └── remind-alice-v2.md      # type: event, memory_type: prospective
└── archive/
    └── old-design.md           # Archived, not in index.md
```

## Compatibility guarantees

1. (Target) Any OKF consumer will be able to read engram9 wiki pages — unknown engram9 fields are ignored per spec.
2. (Planned) Engram9 will import any OKF bundle — missing engram9 profile fields will be populated with defaults during compile.
3. `engram9 validate --strict` enforces the full engram9 profile by treating warnings as validation failure; `engram9 validate` reports warnings but exits successfully when there are no hard errors.
