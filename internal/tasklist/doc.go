// Package tasklist holds the model's own checklist: the complete list of what a run is
// doing, written only by the model through the task_list tool, and re-rendered into the
// standing system content so a long run still knows what is left after a compaction
// (ADR 0072).
//
// Why the harness keeps it at all. A model that decomposes a job into steps holds those
// steps in the conversation — and the conversation is exactly what gets summarised away
// on a long run. The list survives because it is not in the conversation: it is state on
// the engine, re-rendered as a standing block on every request, so the checklist arrives
// intact whatever happened to the messages that produced it. The tool-surface record
// denied a todo tool once, as guided decomposition the model had not asked for; what is
// built here is the other thing — a plain container the model fills, or leaves empty, on
// its own judgment.
//
// Whole-list replace, and why there are no ids. One call carries the COMPLETE list.
// There is nothing to address, so there is no identifier a model must carry across turns
// to tick a row off — the failure mode of every id-bearing checklist tool, and the one
// this shape removes outright. The cost is that a call must restate what has not
// changed, which is also the property that makes the state self-correcting: whatever the
// model last said the list is, it is.
//
// What the caps are for. [MaxItems] and [MaxTextChars] are context budgets rather than
// storage limits — the whole list is re-encoded into every request, so an unbounded one
// spends the window the work needs. A call that breaks either cap is refused whole and
// the held list is left exactly as it was, so a rejected update never leaves a model
// reconstructing a half-applied list.
//
// What this package deliberately is NOT. It performs no I/O, emits no events, reads no
// config, and knows nothing about tools, the engine or the TUI — it imports the standard
// library and nothing else. It is one deep module: the tool, the standing block and the
// session snapshot all go through this surface, and none of them re-implements the
// render, so the fence a block is recognised by ([Fence]) and the text a model reads
// have exactly one author.
//
// Files:
//   - doc.go — this map and the package's rationale.
//   - tasklist.go — the Item, the mutex-guarded List, its whole-list Replace and its render.
//   - context.go — the context seam a tool call reaches the engine's list through.
package tasklist
