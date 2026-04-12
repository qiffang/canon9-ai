package agent

import (
	"context"
	"encoding/json"
	"fmt"
)

const compileSystemPrompt = `You are the Compile Agent of a brain-inspired memory system. Your role is like the brain during sleep — performing global consolidation, distillation, and pruning across all memories.

## Your Process (Three Phases)

### Phase 1: Distill New Events

1. **Read unprocessed events** — Call read_events_since() with the provided cursor to get new events.

2. **For each event**, determine its memory type and how it should be integrated:
   - Is the core knowledge already in wiki (put there by ingest_agent)? → Skip or enrich
   - Does it contradict existing wiki content? → Mark conflict
   - Is it a new topic needing a new page? → Create page
   - Does it contain future intent? → Create/update prospective page

3. **Distill episodic → semantic**: When multiple episodic events about the same topic exist:
   - Extract the core knowledge into semantic pages
   - Preserve source references: [evt_xxx T1]
   - The episodic details (who said what when) stay in episodic pages

4. **Detect contradictions**: When new information conflicts with existing wiki:
   - Add ⚠️ CONFLICT markers in both locations
   - Note which sources support each side
   - Higher trust_tier sources get more weight

5. **Cross-reference completion**: Find pages that should link to each other but don't.

### Phase 2: Sleep Pruning

6. **Read all active wiki pages** with their sidecar metadata.

7. **Apply per-type pruning rules**:

   **episodic/**: Aggressively prune
   - Core already distilled into semantic → archive_wiki_page()
   - Not yet distilled → distill first, then archive
   - Exception: pages marked permanent → keep

   **semantic/**: Almost never archive
   - Only archive if explicitly superseded by newer information
   - Only archive if content fully merged into another page (dedup)
   - NEVER archive just because it hasn't been accessed recently

   **procedural/**: Very persistent
   - Only archive if the tool/process described is deprecated
   - Only archive if replaced by a newer version

   **prospective/**: No decay
   - Archive only if intention is completed or clearly obsolete

8. **Log pruning decisions** — For each archived page, note why.

### Phase 3: Wrap Up

9. **Rebuild index** — Call rebuild_index() to regenerate the routing table.

10. **Report** — Summarize what was done: events processed, pages created/updated/archived.

## Page Format

Every wiki page must have frontmatter comments:
` + "```" + `
<!-- compiled_from: evt_xxx, evt_yyy -->
<!-- last_compiled: YYYY-MM-DDTHH:MM:SSZ -->

# Page Title

Content with source references [evt_xxx T1]
` + "```" + `

## Important Rules

- You do GLOBAL restructuring. The ingest agent does local weaving; you do cross-cutting optimization.
- Be conservative with archiving. When in doubt, keep the page.
- Always preserve source references when distilling.
- Conflicts are valuable — don't resolve them by picking a side. Mark them clearly.
- After significant changes, always rebuild_index().`

// CompileAgent handles the compile() flow — global consolidation and pruning.
type CompileAgent struct {
	runner   *Runner
	executor *ToolExecutor
}

func NewCompileAgent(llm LLM, executor *ToolExecutor) *CompileAgent {
	return &CompileAgent{
		runner:   NewRunner(llm, executor),
		executor: executor,
	}
}

// Compile runs the full sleep cycle: distill + prune + rebuild index.
// The new cursor is extracted programmatically from the read_events_since tool
// results — not from free-form LLM text — and clamped to [oldCursor, eventCount].
func (a *CompileAgent) Compile(ctx context.Context, cursor uint64) (string, uint64, error) {
	userMsg := fmt.Sprintf(`Run a full compile cycle.

Current compile cursor: %d (call read_events_since with cursor=%d to get unprocessed events).

Execute all three phases:
1. Distill new events into wiki
2. Sleep pruning (archive stale pages per memory-type rules)
3. Rebuild index

Report what you did when finished.`, cursor, cursor)

	// Track new_cursor only from a read_events_since call whose input cursor
	// matches our expected start cursor. Accept only the first valid match to
	// prevent later spurious/retry calls from overwriting the value.
	var lastNewCursor uint64
	foundCursor := false

	cb := func(name string, input json.RawMessage, result string, err error) {
		if name != "read_events_since" || err != nil || foundCursor {
			return
		}
		// Validate the tool was called with the correct starting cursor.
		var args struct {
			Cursor uint64 `json:"cursor"`
		}
		if json.Unmarshal(input, &args) != nil || args.Cursor != cursor {
			return
		}
		var page struct {
			NewCursor uint64 `json:"new_cursor"`
		}
		if json.Unmarshal([]byte(result), &page) == nil {
			lastNewCursor = page.NewCursor
			foundCursor = true
		}
	}

	result, _, err := a.runner.RunWithCallback(ctx, compileSystemPrompt, CompileTools, userMsg, cb)
	if err != nil {
		return "", cursor, err
	}

	// Only advance cursor if we actually got a value from the tool transcript.
	if !foundCursor {
		return result, cursor, nil
	}

	// Clamp: never go backward, never exceed current event count.
	newCursor := lastNewCursor
	if newCursor < cursor {
		newCursor = cursor
	}
	eventCount := a.currentEventCount()
	if newCursor > eventCount {
		newCursor = eventCount
	}

	return result, newCursor, nil
}

// currentEventCount returns the current number of events in the store.
func (a *CompileAgent) currentEventCount() uint64 {
	page, err := a.executor.store.ReadEventsSince(0)
	if err != nil {
		return 0
	}
	return page.NewCursor
}
