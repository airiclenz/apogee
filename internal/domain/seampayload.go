package domain

import "encoding/json"

// ScheduleRef names the daemon or `/schedule` Schedule a Firing ran for. It is present only on a
// Firing's payload — a TUI session and a plain headless run belong to no Schedule — so a reaction
// can tell "the 6am docs sweep finished" from "the session I am sitting in finished".
type ScheduleRef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// SeamPayload is the ONE JSON document a fired out-of-process Reaction receives, whichever lane
// fired it: on stdin for an observe command or a sync-lane advise/gate handler (ADR 0076 D2), as
// the POST body for an observe webhook. Its field names are a DOCUMENTED CONTRACT: a user's script
// reads them by name, so they are renamed only by a deliberate, documented break — and because
// both lanes hand out the same document, a script that reads `event`, `reaction`, `turn` or
// `path` off an observe firing reads them off a gate or advise firing without learning a second
// one.
//
// The first block is present on every firing and identifies it: which reaction fired, on what,
// when, and in which run. Depth and Turn are the emitting agent's, so a Reaction fired by a
// sub-agent reports the child's nesting level rather than the top-level agent's, and CallID is that
// child's spawning call — the id of the sub_agent call that spawned it, empty at Depth 0. Every
// field after that block is per-Moment and omitted when it does not apply, so a script can branch
// on "event" and read only what that event carries. The last two members are what only an in-loop
// Moment has: the pending call's `arguments` at pre-tool-exec, and the returned `result` at
// post-tool-result.
//
// The payload is NOT secret-scrubbed: it goes to the user's own command or URL, which is the same
// trust as the screen (ADR 0073 §6).
//
// On the observe lane the matcher (internal/reactions) fills the event-derived fields and the
// Runner stamps the identity ones it alone knows — Reaction, Time, Workspace and Schedule — as it
// hands the payload to each subscribing reaction. On the sync lane the agent builds the document
// whole at the Moment it fires.
type SeamPayload struct {
	// Event is the Moment that fired, spelled exactly as the `on:` list spells it.
	Event Moment `json:"event"`
	// Reaction is the id of the entry that fired, the `id:` from its config row.
	Reaction string `json:"reaction"`
	// Time is when the firing was matched, RFC 3339 with seconds resolution or finer.
	Time string `json:"time"`
	// Workspace is the absolute, symlink-resolved workspace the run is rooted in.
	Workspace string `json:"workspace"`
	// Depth is the emitting agent's sub-agent nesting level; 0 is the top-level agent.
	Depth int `json:"depth"`
	// Turn is the Turn index the Moment belongs to.
	Turn int `json:"turn"`
	// CallID is the spawning call of the emitting agent — the id of the sub_agent call that
	// spawned it (EventBase.CallID, which can collide across runs) — and is empty at Depth 0.
	CallID string `json:"call_id,omitempty"`
	// Schedule names the Schedule this Firing ran for; absent outside a Firing.
	Schedule *ScheduleRef `json:"schedule,omitempty"`

	// Status is the Turn's StepStatus. turn-finished, exchange-finished.
	Status string `json:"status,omitempty"`
	// Faulted marks a Turn the loop ABANDONED rather than completed. turn-finished,
	// exchange-finished.
	Faulted bool `json:"faulted,omitempty"`
	// StepCapped marks an Exchange the delegate step cap ended rather than the model.
	// turn-finished, exchange-finished.
	StepCapped bool `json:"step_capped,omitempty"`

	// Tool is the tool that wrote the file, the tool whose call is pending or whose result came
	// back, or the tool whose call is waiting on an Approval. file-changed, pre-tool-exec,
	// post-tool-result, approval-requested, approval-decided.
	Tool string `json:"tool,omitempty"`
	// Path is the absolute, symlink-resolved path a successful workspace write landed on.
	// file-changed alone.
	Path string `json:"path,omitempty"`

	// Reason is why the Approval was required, in the engine's own words. approval-requested,
	// approval-decided.
	Reason string `json:"reason,omitempty"`
	// Remedy is the optional one-line route out of the condition that forced the Approval.
	// approval-requested, approval-decided.
	Remedy string `json:"remedy,omitempty"`
	// SubAgentName is the display name of the child whose call is waiting, when it has one.
	// approval-requested, approval-decided.
	SubAgentName string `json:"sub_agent_name,omitempty"`
	// Scope is the optional statement of what the call reaches beyond what its arguments name.
	// approval-requested, approval-decided.
	Scope string `json:"scope,omitempty"`
	// Decision is the verdict the Approver returned, in the ApprovalDecision spelling — `allow`,
	// `deny` or `allow-for-session`. approval-decided.
	Decision string `json:"decision,omitempty"`

	// Source is what faulted — a tool name, a Reaction id, or "loop". error.
	Source string `json:"source,omitempty"`
	// Error is the fault's message. error.
	Error string `json:"error,omitempty"`

	// Seam is the seam whose pass closed, in the Moment's own spelling — the notice is that
	// name plus `-finished`, so a script branching on "event" already knows it and one
	// branching on "seam" reads the in-loop point directly. The five seam-closing notices.
	Seam Moment `json:"seam,omitempty"`
	// Reactions are the ids of the reactions booked as firings during the pass, in the order
	// they fired; absent when nothing acted, which is the ordinary pass. The five
	// seam-closing notices.
	Reactions []string `json:"reactions,omitempty"`
	// Value is the JSON projection of the seam's working value as the pass left it — the
	// request at pre-request, the response at post-response, the pending call at
	// pre-tool-exec, the call and its result at post-tool-result, the conversation at
	// history-rewrite. It is built while the engine's Emit is still running, because the
	// reference the event carries is valid only for that call (SeamClosedEvent), and it is a
	// copy: nothing here points back into the loop's own state. The projection itself is
	// internal/reactions' (its payload.go) — this package holds the document, not the engine
	// state it is cut from. The five seam-closing notices.
	Value any `json:"value,omitempty"`

	// Arguments is the raw JSON object the model produced for the call, copied rather than
	// referenced so the document survives the working value it came from. pre-tool-exec.
	Arguments json.RawMessage `json:"arguments,omitempty"`
	// Result is the tool result as the loop holds it when the Moment fires; absent at
	// pre-tool-exec, where no result exists yet. post-tool-result.
	Result *SeamResult `json:"result,omitempty"`
}

// SeamResult is the `result` member of a SeamPayload: what the tool returned, as the sync-lane
// reaction sees it.
type SeamResult struct {
	// Content is the result body the model would read.
	Content string `json:"content,omitempty"`
	// IsError reports that the tool call failed. It is always written, so a script can branch on
	// it without having to tell "absent" from "false".
	IsError bool `json:"is_error"`
}

// Environment variable names a fired command finds the payload's headline facts under, on either
// lane — a script does not care which lane fired it. They are a convenience for a one-line script
// that would otherwise pipe stdin through a JSON parser; the full document is always on stdin as
// well.
const (
	EnvReactionEvent        = "APOGEE_REACTION_EVENT"
	EnvReactionName         = "APOGEE_REACTION_NAME"
	EnvReactionWorkspace    = "APOGEE_REACTION_WORKSPACE"
	EnvReactionPath         = "APOGEE_REACTION_PATH"
	EnvReactionScheduleID   = "APOGEE_REACTION_SCHEDULE_ID"
	EnvReactionScheduleName = "APOGEE_REACTION_SCHEDULE_NAME"
)

// Env renders the payload's headline facts as `NAME=value` entries for the fired command's
// environment, in a fixed order. A fact the payload does not carry is OMITTED rather than set
// empty, so a script can test with `[ -n "$APOGEE_REACTION_PATH" ]` and a variable inherited from
// the user's own environment is not silently blanked by a reaction that has nothing to put there.
func (p SeamPayload) Env() []string {
	env := make([]string, 0, 6)
	add := func(name, value string) {
		if value != "" {
			env = append(env, name+"="+value)
		}
	}
	add(EnvReactionEvent, string(p.Event))
	add(EnvReactionName, p.Reaction)
	add(EnvReactionWorkspace, p.Workspace)
	add(EnvReactionPath, p.Path)
	if p.Schedule != nil {
		add(EnvReactionScheduleID, p.Schedule.ID)
		add(EnvReactionScheduleName, p.Schedule.Name)
	}
	return env
}
