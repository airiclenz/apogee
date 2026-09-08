package reactions

// ScheduleRef names the daemon or `/schedule` Schedule a Firing ran for. It is present only on a
// Firing's payload — a TUI session and a plain headless run belong to no Schedule — so a reaction
// can tell "the 6am docs sweep finished" from "the session I am sitting in finished".
type ScheduleRef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// Payload is the JSON document a fired reaction receives — on stdin for a command, as the POST body
// for a webhook. Its field names are a DOCUMENTED CONTRACT: a user's script reads them by name, so
// they are renamed only by a deliberate, documented break.
//
// The first block is present on every event and identifies the firing: which reaction fired, on what,
// when, and in which run. Depth and Turn are the emitting agent's, so a Hook fired by a sub-agent
// reports the child's nesting level rather than the top-level agent's, and CallID is that child's
// run identity — the id of the sub_agent call that spawned it, empty at Depth 0. Every field after
// that block is per-event and omitted when it does not apply, so a script can branch on "event"
// and read only what that event carries.
//
// The payload is NOT secret-scrubbed: it goes to the user's own command or URL, which is the same
// trust as the screen (ADR 0073 §6).
//
// The matcher fills the event-derived fields; the runner stamps the identity ones it alone knows —
// Reaction, Time, Workspace and Schedule — as it hands the payload to each subscribing reaction.
type Payload struct {
	// Event is the notice that fired, spelled exactly as the `events:` list spells it.
	Event Event `json:"event"`
	// Reaction is the name of the entry that fired, the `name:` from its config row.
	Reaction string `json:"reaction"`
	// Time is when the firing was matched, RFC 3339 with seconds resolution or finer.
	Time string `json:"time"`
	// Workspace is the absolute, symlink-resolved workspace the run is rooted in.
	Workspace string `json:"workspace"`
	// Depth is the emitting agent's sub-agent nesting level; 0 is the top-level agent.
	Depth int `json:"depth"`
	// Turn is the Turn index the event belongs to.
	Turn int `json:"turn"`
	// CallID is the run identity of the emitting agent — the id of the sub_agent call that
	// spawned it — and is empty at Depth 0.
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

	// Tool is the tool that wrote the file, or the tool whose call is waiting on an Approval.
	// file-changed, approval-requested, approval-decided.
	Tool string `json:"tool,omitempty"`
	// Path is the absolute, symlink-resolved path the write landed on. file-changed.
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
	// Decision is the verdict the Approver returned, in the domain.ApprovalDecision spelling —
	// `allow`, `deny` or `allow-for-session`. approval-decided.
	Decision string `json:"decision,omitempty"`

	// Source is what faulted — a tool name, a Reaction id, or "loop". error.
	Source string `json:"source,omitempty"`
	// Error is the fault's message. error.
	Error string `json:"error,omitempty"`
}

// Environment variable names a fired command finds the payload's headline facts under. They are a
// convenience for a one-line script that would otherwise pipe stdin through a JSON parser; the
// full document is always on stdin as well.
const (
	EnvEvent        = "APOGEE_REACTION_EVENT"
	EnvName         = "APOGEE_REACTION_NAME"
	EnvWorkspace    = "APOGEE_REACTION_WORKSPACE"
	EnvPath         = "APOGEE_REACTION_PATH"
	EnvScheduleID   = "APOGEE_REACTION_SCHEDULE_ID"
	EnvScheduleName = "APOGEE_REACTION_SCHEDULE_NAME"
)

// Env renders the payload's headline facts as `NAME=value` entries for a fired command's
// environment, in a fixed order. A fact the payload does not carry is OMITTED rather than set
// empty, so a script can test with `[ -n "$APOGEE_REACTION_PATH" ]` and a variable inherited from
// the user's own environment is not silently blanked by a reaction that has nothing to put there.
func (p Payload) Env() []string {
	env := make([]string, 0, 6)
	add := func(name, value string) {
		if value != "" {
			env = append(env, name+"="+value)
		}
	}
	add(EnvEvent, string(p.Event))
	add(EnvName, p.Reaction)
	add(EnvWorkspace, p.Workspace)
	add(EnvPath, p.Path)
	if p.Schedule != nil {
		add(EnvScheduleID, p.Schedule.ID)
		add(EnvScheduleName, p.Schedule.Name)
	}
	return env
}
