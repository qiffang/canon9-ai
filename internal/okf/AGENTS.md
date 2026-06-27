# internal/okf — OKF Validation & Legacy Migration

## Overview
Standalone package for OKF bundle validation and legacy-to-OKF migration. No dependency on `agent`, `api`, `mcp`, or `storage` — used only by the CLI subcommands (`validate`, `migrate-okf`).

## Structure
```
okf/
├── validator.go          ValidateBundle(root, strict) → Result
├── migration.go          MigrateLegacyBundle(root, opts) → MigrationResult
├── validator_test.go     7 validation tests
├── migration_test.go     7 migration tests
```

## Validation Rules

### Hard Fail (always error)
- Missing `type` field in YAML frontmatter
- Invalid `timestamp` (not RFC3339)
- Invalid `memory_type` (not in: semantic, episodic, procedural, prospective)
- Invalid `trust_tier` (not T1, T2, or T3)
- Invalid `confidence` (not high, medium, or low)

### Warning → Error (with `--strict`)
- Missing `title`, `description`, `timestamp`, `memory_type`
- Broken internal links (target file does not exist)
- Missing `source_events` or `trust_tier`

### Tolerated (never error)
- Unknown `type` values (OKF doesn't restrict to closed set)
- Unknown frontmatter fields from other profiles
- `index.md` without frontmatter (structural file)
- Missing optional fields (`confidence`, `supersedes`, `contradicts`)
- External links (not validated)

## Migration (Legacy → OKF)

`MigrateLegacyBundle` converts two legacy patterns:
1. **HTML comments → YAML frontmatter**: `<!-- compiled_from: evt_001 -->`, `<!-- last_compiled: ... -->`, `<!-- memory_type: ... -->`, `<!-- trust_tier: ... -->`
2. **`[[wikilinks]]` → standard Markdown**: `[[Foo]]` → `[Foo](foo.md)`, `[[Bar|label]]` → `[label](bar.md)`

### Inference Rules (when fields are missing)
- `memory_type` ← folder name (semantic/episodic/procedural/prospective)
- `type` ← memory_type (concept/event/procedure)
- `title` ← first H1 heading in body
- `description` ← first non-empty paragraph after heading
- `timestamp` ← modification time of the file

### Important Behaviors
- **Links in fenced code blocks and inline code are left unchanged** — intentional. Never modify these.
- **`--write` creates `.bak` backups** — enable with `--backup=false` to suppress.
- **`--check` is a CI guard** — exits non-zero if any legacy format remains. Use in CI to prevent legacy drift.
- **Idempotent**: running migration twice on the same file should be a no-op.

## Modes
| Flag | Behavior |
|------|----------|
| `--write` | Convert in-place (creates `.bak` backups) |
| `--backup=false` | Skip backup creation |
| `--check` | CI guard: fail if legacy format found (no write) |
| (none) | Dry-run: report what would change |

## Testing

- Tests use programmatically-generated fixtures (no `testdata/` directory despite AGENTS.md claim).
- Both `validator_test.go` and `main_test.go` have identical `writeFile` helpers — if adding a `testutil/` package, deduplicate there.
- Migration tests validate byte-for-byte output of converted files.

## Gotchas

- **`_ = frontmatter` dead assignment** (migration.go:144) — variable declared and assigned but never read. Marked as low-priority cleanup.
- **OKF import/export CLI is deferred** — only `validate` and `migrate-okf` exist. The README references planned `engram9 export okf` / `engram9 import okf` that don't exist yet.
