# Repo Ingest

`engram9 repo scan` builds deterministic repository facts for later OKF concept compilation. This command does **not** call an LLM and does **not** summarize code. It only emits source-grounded facts that can be verified against a git checkout.

## Command

```bash
engram9 repo scan \
  --path /path/to/repo \
  --scope pkg/fuse \
  --out ./repo-facts/pkg-fuse
```

Outputs:

- `manifest.json` — scan metadata, git SHAs, scanned files, hashes, fact IDs, and symbol IDs.
- `facts.jsonl` — one JSON fact per line.
- `snippets.jsonl` — one source-grounded core code snippet per declaration fact.

The manifest uses portable repo metadata only: `repo`, `scope`, `base_sha`, `head_sha`, `file_hashes`, `fact_ids`, and `snippet_ids`. It intentionally does not include host-local absolute paths or generation timestamps.

Every fact includes:

- `repo`
- `commit_sha`
- `path`
- `line`
- `symbol`
- `source_anchor`
- `kind`
- `doc`
- `exported`

Current fact kinds:

- `package`
- `import`
- `type`
- `interface`
- `func`
- `method`
- `test`
- `const`
- `var`
- `file` with `status: deleted` for deleted Go files in incremental scans

Snippet records are emitted for declarations that carry repo design meaning:

- `type`
- `interface`
- `func`
- `method`
- `test`
- `const`
- `var`

Each snippet includes:

- `fact_id`
- `repo`
- `commit_sha`
- `path`
- `start_line`
- `end_line`
- `symbol`
- `source_anchor`
- `language`
- `file_hash`
- `content`

## Incremental scan

Use `--since <old-sha>` to scan only files changed within a scope:

```bash
engram9 repo scan \
  --path /path/to/repo \
  --scope pkg/fuse \
  --since <old-sha> \
  --out ./repo-facts/pkg-fuse
```

The scanner runs:

```bash
git diff --name-status --find-renames <old-sha>..HEAD -- <scope>
```

Behavior:

- Changed Go files are rescanned and replace prior facts for that file.
- Deleted Go files emit `file` facts with `status: deleted`.
- Renamed Go files emit a deleted fact for the old path and present facts for the new path.
- Non-Go files may appear in `manifest.changed`, but only Go source facts are emitted in the current scanner.
- File paths are read from git with NUL-separated output so spaces in paths do not corrupt incremental scans.

## Wiki integration contract

Repo facts and snippets are chunked by package and fed into the LLM-backed server pipeline via `POST /remember` (with `source_type=tool`, `evidence_kind=direct_observation`, `trust_tier=2`). The server's ingest agent weaves them into wiki pages, and `POST /compile` synthesizes cross-cutting summaries.

The LLM must treat code facts as the primary source of truth:

1. Wiki pages must cite fact IDs or source anchors derived from `commit_sha + path + line + symbol`.
2. Design summaries must cite snippets or facts from the same `commit_sha`; claims from older commits are stale.
3. Docs, PR bodies, issues, and review comments can explain **why**, but they must not override code facts.
4. If a source anchor disappears after a later scan, the related wiki page must be recompiled or marked `stale_source`.

This avoids mixing knowledge from different commits and prevents agents from trusting stale line numbers or symbols after code changes.
