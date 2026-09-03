package domain

import "encoding/json"

// ----------------------------------------------------------------------------
// Tool-stage hook working values (hooks.go's ToolCall / ToolResult half)
// ----------------------------------------------------------------------------
//
// ToolCallEdit and ToolResultEdit are what the two tool-stage hooks receive in place of
// the plain ToolCall / ToolResult structs. They are the working-value shape Request,
// Response and Conversation already have: an opaque wrapper with a method-only surface —
// read accessors, a curated set of mutators, and a Revision counter every mutator bumps —
// so the loop reads "did this hook act?" (R4) off one integer instead of comparing values.
//
// The counter is what earns the wrapper. The hand-written probes it replaces snapshotted
// the value before each fire and diffed it field by field afterwards, and
// ToolResult.Summary is an interface that routinely holds an UNCOMPARABLE value (ReadSpan
// carries the []int of located lines, so every successful read_file produces one): the
// whole-struct compare that shipped panicked with "comparing uncomparable type …" on
// exactly the no-op path, and had to be hotfixed into a field-by-field twin. A revision
// counter cannot have that failure mode — it never looks at the value at all.
//
// The plain structs (tools.go) are unchanged and stay the currency everywhere else: the
// provider parses into them, a Tool returns one, the loop commits one to history. Only
// the two hook seams speak in edit values.
//
// The identity fields are deliberately read-only. ToolCall.ID and ToolResult.CallID are
// what the loop pairs a result to its call by — the ToolCallEvent naming the call is
// already emitted when a pre-tool-exec hook runs, and the committed tool-result message
// links back through the same id — so a hook may reshape WHAT runs and WHAT came back,
// never which call it was.
//
// Like NewRequest / NewResponse / NewConversation, the constructors are the ENGINE SEAM:
// internal/agent wraps the live struct it owns, hands ONE wrapper to every hook at that
// point (so their mutations compose, as at every other hook point), and reads the result
// back through the pointer it still holds. The root facade aliases the two types, so an
// embedder can implement the hook interfaces, but not the constructors — a hook receives
// an edit value and never mints one.

// ToolCallEdit is the pending tool call a pre-tool-exec hook may reshape before the loop
// executes it. It writes through to the ToolCall the loop owns, so a mutation is live the
// moment it is made and composes with the next hook's view of the call.
type ToolCallEdit struct {
	call     *ToolCall
	revision int // bumped by each mutator — the acted-fire probe (R4), read via Revision
}

// NewToolCallEdit wraps the pending call the loop is about to run (engine seam). call must
// be non-nil and must outlive the hook cascade: the wrapper holds it rather than a copy,
// which is how the loop reads the hooks' mutations back.
func NewToolCallEdit(call *ToolCall) *ToolCallEdit {
	return &ToolCallEdit{call: call}
}

// ID is the call id the model assigned, linking this call to its result. Read-only — see
// the identity note above.
func (e *ToolCallEdit) ID() string { return e.call.ID }

// Tool is the name of the tool the model asked for.
func (e *ToolCallEdit) Tool() string { return e.call.Tool }

// Arguments is the raw JSON argument object the model produced. The slice is copied, so a
// hook cannot edit the pending call by writing through the backing array — every mutation
// goes through SetArguments and is counted.
func (e *ToolCallEdit) Arguments() json.RawMessage {
	return append(json.RawMessage(nil), e.call.Arguments...)
}

// Revision reports how many mutations have been applied to the call — the loop's
// acted-fire probe (R4, engine seam): hookrun snapshots it around each catalogued fire and
// books the fire only when the counter moved. A hook never needs it.
func (e *ToolCallEdit) Revision() int { return e.revision }

// SetTool redirects the call to a different tool. The arguments are left as they are — a
// hook redirecting to a tool with another argument shape sets both.
func (e *ToolCallEdit) SetTool(name string) {
	e.call.Tool = name
	e.revision++
}

// SetArguments replaces the call's argument object — the shaping operation the re-read
// family uses (the read-cache Floor guard caps a redundant re-read by adding max_lines). The
// slice is copied so the caller cannot reach back into the pending call afterwards.
func (e *ToolCallEdit) SetArguments(args json.RawMessage) {
	e.call.Arguments = append(json.RawMessage(nil), args...)
	e.revision++
}

// ToolResultEdit is the tool result a post-tool-result hook may rewrite before the model
// next sees it. Like ToolCallEdit it writes through to the ToolResult the loop owns, so
// the loop commits what the hooks left behind.
type ToolResultEdit struct {
	result   *ToolResult
	revision int // bumped by each mutator — the acted-fire probe (R4), read via Revision
}

// NewToolResultEdit wraps the result the tool just returned (engine seam). result must be
// non-nil and must outlive the hook cascade, for the same reason NewToolCallEdit's call
// must.
func NewToolResultEdit(result *ToolResult) *ToolResultEdit {
	return &ToolResultEdit{result: result}
}

// CallID is the id of the call this result answers. Read-only — see the identity note
// above.
func (e *ToolResultEdit) CallID() string { return e.result.CallID }

// Content is the prose half of the outcome — what the model reads.
func (e *ToolResultEdit) Content() string { return e.result.Content }

// IsError reports whether the tool failed. It is the authoritative flag: once the result
// is committed to history all that survives is the text, so a hook asking "did this call
// fail?" about the LIVE result reads it here rather than sniffing the prose.
func (e *ToolResultEdit) IsError() bool { return e.result.IsError }

// Summary is the OPTIONAL structured half of the outcome, for a host that renders this
// result (toolsummary.go). Nil means there is nothing but the prose.
func (e *ToolResultEdit) Summary() ToolSummary { return e.result.Summary }

// Revision reports how many mutations have been applied to the result — the loop's
// acted-fire probe (R4, engine seam), read exactly as ToolCallEdit.Revision is. A hook
// never needs it.
func (e *ToolResultEdit) Revision() int { return e.revision }

// SetContent replaces the prose the model will read — the rewrite a post-tool-result hook makes
// when it appends its own guidance to a failure.
func (e *ToolResultEdit) SetContent(content string) {
	e.result.Content = content
	e.revision++
}

// SetIsError sets the authoritative failure flag: a hook that turns a failure into a
// recovered outcome (or the reverse) moves the flag with the prose, so the marker
// committed beside the text keeps telling the truth (ToolOutcomeOf, hooks.go).
func (e *ToolResultEdit) SetIsError(isError bool) {
	e.result.IsError = isError
	e.revision++
}

// SetSummary replaces the structured half of the outcome. A hook rewriting Content usually
// leaves it alone — the summary describes what the TOOL DID, so text rewriting never
// invalidates it (tools.go) — but a hook that changes the outcome itself sets both. nil
// clears it back to prose-only.
func (e *ToolResultEdit) SetSummary(summary ToolSummary) {
	e.result.Summary = summary
	e.revision++
}
