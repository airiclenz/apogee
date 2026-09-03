package agent

// The engine-owned TASK LIST BLOCK: the part of the standing system content (loop.go's
// standingSystem) that renders the model's own checklist — the list held on the engine and written
// only through the task_list tool (internal/tasklist, ADR 0072) — so a run that has been compacted
// still reads what it set out to do and what is left of it.
//
// It RIDES ALONG under the orientation block's rule (ADR 0023 §6 amendment, third addendum
// 2026-09-02): standingSystem composes it in only when a configured source already put something
// in the message, never on its own, so the no-prompt-AND-no-context-files anchor stays
// byte-identical on the wire and the Bypass floor with it. An empty list renders "" as well, so a
// session whose model never called the tool costs nothing here.
//
// Position — after the delegate report block, AHEAD of the workspace context files' blocks — is
// the same SECURITY property the two blocks before it have (F-19, orientation.go): every
// engine-owned part rides ahead of the repo-controlled context blocks, so no workspace text can
// precede — and thereby read as a correction of — the host's own statements. ADR 0023's
// 2026-08-26 forgery argument is the whole reason this block goes last of the engine's four
// rather than first of anything: the list is model-authored text, and text the model wrote must
// still not be able to sit where the host's facts sit. TaskListFence below is the other half of
// that guard, exactly as delegateReportFence is for the delegate block's.
//
// KV CACHE — the one thing that makes this block unlike the three around it. Every other part of
// the standing content is a per-session constant, so a server's prefix cache survives a whole
// session; this is the first ENGINE-OWNED block whose content CHANGES WITHIN a session, and every
// task_list call therefore invalidates the prefix from this block onward. That cost is accepted
// and recorded (ADR 0072): a model that re-reads its own checklist after a compaction is worth
// more than the re-encode of the standing tail, and a run that never calls the tool pays nothing
// because the block stays "".

import "github.com/airiclenz/apogee/internal/tasklist"

// TaskListFence is the literal opening of the task list block's header line — the string a
// workspace context file would have to spell to pass its own prose off as the engine's checklist,
// and the prefix forgesStandingStructure (contextfiles.go) fences against.
//
// EXPORTED — and re-exported as apogee.TaskListFence — because it is the anchor a Driver's own
// tests find the block on the wire by (DelegateReportBlock's precedent, delegatereport.go):
// reading the bytes from here is what keeps those assertions from being a retyped copy that can
// drift. It is tasklist.Fence itself rather than a copy of it, so the render and the fence have
// exactly one author.
const TaskListFence = tasklist.Fence

// taskListBlock returns the rendered task list, or "" when this agent holds no tasks — the empty
// case being both the fresh session and every session whose model never calls the tool.
//
// The render lives in internal/tasklist, not here: the block, the tool's confirmation and the
// session snapshot all read one surface, so the text a model sees has a single author.
func (a *Agent) taskListBlock() string {
	return a.tasks.Render()
}
