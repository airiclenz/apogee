package domain

import "encoding/json"

// SeamPayload is the JSON document a SYNC-lane reaction receives on standard input — the stdin
// half of what an argv handler of class advise or gate is handed when the loop passes its Moment
// (ADR 0076 D2). It is the in-loop counterpart of the observe lane's payload
// (internal/reactions.Payload): the same identity block, spelled with the same JSON keys, so a
// user's script that already reads `event`, `reaction`, `turn` or `path` off an observe firing
// reads them off a gate or advise firing without learning a second document.
//
// The keys are a DOCUMENTED CONTRACT — a script reads them by name — and the shared ones are
// pinned against the observe payload's tags by a test, so the two cannot drift apart silently.
// What the sync document adds is what only an in-loop Moment has: the pending call's `arguments`
// at pre-tool-exec, and the returned `result` at post-tool-result. Both are omitted where the
// Moment does not carry them.
//
// Like the observe payload it is NOT secret-scrubbed: it goes to the user's own command, which is
// the same trust as the screen (ADR 0073 §6).
type SeamPayload struct {
	// Event is the Moment that fired, spelled exactly as the `on:` list spells it.
	Event Moment `json:"event"`
	// Reaction is the id of the entry that fired, the `id:` from its config row.
	Reaction string `json:"reaction"`
	// Time is when the firing was matched, RFC 3339 with seconds resolution or finer.
	Time string `json:"time"`
	// Workspace is the absolute, symlink-resolved workspace the run is rooted in.
	Workspace string `json:"workspace"`
	// Depth is the firing agent's sub-agent nesting level; 0 is the top-level agent.
	Depth int `json:"depth"`
	// Turn is the Turn index the Moment belongs to.
	Turn int `json:"turn"`
	// CallID is the run identity of the firing agent — the id of the sub_agent call that spawned
	// it — and is empty at Depth 0.
	CallID string `json:"call_id,omitempty"`
	// Tool is the tool whose call is pending (pre-tool-exec) or whose result came back
	// (post-tool-result, file-changed).
	Tool string `json:"tool,omitempty"`
	// Path is the absolute, symlink-resolved path a successful workspace write landed on.
	// file-changed alone.
	Path string `json:"path,omitempty"`

	// Arguments is the raw JSON object the model produced for the call, copied rather than
	// referenced so the document survives the working value it came from.
	Arguments json.RawMessage `json:"arguments,omitempty"`
	// Result is the tool result as the loop holds it when the Moment fires; absent at
	// pre-tool-exec, where no result exists yet.
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

// Environment variable names a fired sync-lane command finds the payload's headline facts under.
// They are the observe lane's four spellings (internal/reactions' EnvEvent, EnvName, EnvWorkspace
// and EnvPath), repeated here rather than imported because internal/reactions imports THIS
// package and the dependency cannot point back. One vocabulary, two lanes: a script does not care
// which lane fired it.
const (
	EnvReactionEvent     = "APOGEE_REACTION_EVENT"
	EnvReactionName      = "APOGEE_REACTION_NAME"
	EnvReactionWorkspace = "APOGEE_REACTION_WORKSPACE"
	EnvReactionPath      = "APOGEE_REACTION_PATH"
)

// Env renders the payload's headline facts as `NAME=value` entries for the fired command's
// environment, in a fixed order. A fact the payload does not carry is OMITTED rather than set
// empty — the observe lane's rule, for the same reason: a script tests with
// `[ -n "$APOGEE_REACTION_PATH" ]`, and a variable inherited from the user's own environment is
// not silently blanked by a reaction that has nothing to put there.
func (p SeamPayload) Env() []string {
	env := make([]string, 0, 4)
	add := func(name, value string) {
		if value != "" {
			env = append(env, name+"="+value)
		}
	}
	add(EnvReactionEvent, string(p.Event))
	add(EnvReactionName, p.Reaction)
	add(EnvReactionWorkspace, p.Workspace)
	add(EnvReactionPath, p.Path)
	return env
}
