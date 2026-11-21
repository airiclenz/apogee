package tools

import (
	"testing"
)

// The published sub_agent and fan_out schemas, pinned byte for byte as they stood before the
// delegation offer became one value (plan 2026-11-21 - 00). A schema is prefill on every request of
// every session that offers the tool, so a reshaped offer that changes one byte of it changes what
// every model reads: these literals are the reference that reshaping is checked against. They are
// captured output, deliberately NOT composed from subAgentSchemaTemplate or fanOutSchemaTemplate —
// a pin built from the code it pins would move with it. Edit one only where a plan item names the
// change.

// TestDelegationSchemas_BytesArePinned proves every variant each constructor publishes — the 8
// sub_agent variants (seat choice × background × context) and the 4 fan_out variants (seat choice ×
// background) — is byte-identical to its pinned literal, and that the plain constructors publish the
// plain variant.
func TestDelegationSchemas_BytesArePinned(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		{"sub_agent plain (NewSubAgent)", string(NewSubAgent().Schema()), pinnedSubAgentSchemaPlain},
		{"sub_agent plain", subAgentSchemaFor(SubAgentOptions{}), pinnedSubAgentSchemaPlain},
		{"sub_agent seat choice", subAgentSchemaFor(SubAgentOptions{SeatChoice: true}), pinnedSubAgentSchemaSeatChoice},
		{"sub_agent background", subAgentSchemaFor(SubAgentOptions{Background: true}), pinnedSubAgentSchemaBackground},
		{"sub_agent context", subAgentSchemaFor(SubAgentOptions{Context: true}), pinnedSubAgentSchemaContext},
		{"sub_agent seat choice + background", subAgentSchemaFor(SubAgentOptions{SeatChoice: true, Background: true}), pinnedSubAgentSchemaSeatChoiceBackground},
		{"sub_agent seat choice + context", subAgentSchemaFor(SubAgentOptions{SeatChoice: true, Context: true}), pinnedSubAgentSchemaSeatChoiceContext},
		{"sub_agent background + context", subAgentSchemaFor(SubAgentOptions{Background: true, Context: true}), pinnedSubAgentSchemaBackgroundContext},
		{"sub_agent every option", subAgentSchemaFor(SubAgentOptions{SeatChoice: true, Background: true, Context: true}), pinnedSubAgentSchemaAll},
		{"fan_out plain (NewFanOut)", string(NewFanOut().Schema()), pinnedFanOutSchemaPlain},
		{"fan_out plain", fanOutSchemaFor(FanOutOptions{}), pinnedFanOutSchemaPlain},
		{"fan_out seat choice", fanOutSchemaFor(FanOutOptions{SeatChoice: true}), pinnedFanOutSchemaSeatChoice},
		{"fan_out background", fanOutSchemaFor(FanOutOptions{Background: true}), pinnedFanOutSchemaBackground},
		{"fan_out seat choice + background", fanOutSchemaFor(FanOutOptions{SeatChoice: true, Background: true}), pinnedFanOutSchemaSeatChoiceBackground},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.got != tc.want {
				at := firstDifference(tc.got, tc.want)
				t.Errorf("schema differs from the pin at byte %d:\n got: %q\nwant: %q",
					at, excerptAt(tc.got, at), excerptAt(tc.want, at))
			}
		})
	}
}

// subAgentSchemaFor is the schema the sub_agent constructor publishes for opts.
func subAgentSchemaFor(opts SubAgentOptions) string { return string(NewSubAgentWith(opts).Schema()) }

// fanOutSchemaFor is the schema the fan_out constructor publishes for opts.
func fanOutSchemaFor(opts FanOutOptions) string { return string(NewFanOutWith(opts).Schema()) }

// firstDifference is the byte offset at which a and b first differ: the shorter length when one is
// a prefix of the other.
func firstDifference(a, b string) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// excerptAt is the stretch of s around offset at, so a failing pin names the bytes that moved
// rather than printing two whole schemas.
func excerptAt(s string, at int) string {
	const radius = 60
	return s[max(0, at-radius):min(len(s), at+radius)]
}

const pinnedSubAgentSchemaPlain = `{
  "type": "object",
  "required": ["task"],
  "properties": {
    "task": {"type": "string", "minLength": 20, "description": "The focused sub-task to delegate to a nested agent. Describe it self-containedly: the sub-agent starts with a fresh conversation and reports a single result back."},
    "name": {"type": "string", "description": "Short name for this delegation, shown in the UI: 2–4 words naming the job, e.g. \"scout config keys\". Give one."},
    "max_steps": {"type": "integer", "minimum": 1, "description": "optional; a lower cap for this delegation only, in Turns — see the Delegation bounds line of the host orientation for the configured cap; a request above it is clamped and the result says so."},
    "tools": {"type": ["string", "array"], "items": {"type": "string"}, "description": "optional; narrow the sub-agent's tools: the string \"read-only\" for the read-only set, or an array of tool names from your own menu. It can only remove tools, never add them; an unknown name is refused."},
    "continue": {"type": "string", "description": "optional; the name of a delegation earlier in this session — one you named, or one that was capped, faulted or stopped. The sub-agent restarts from its task and earlier reports with a fresh step cap; task says what to do next."},
    "output_path": {"type": "string", "description": "optional; the file the sub-agent is expected to write, relative to the workspace root or absolute. If it hits its step cap, write_file to this one path stays available for its final reply."}
  }
}`

const pinnedSubAgentSchemaSeatChoice = `{
  "type": "object",
  "required": ["task"],
  "properties": {
    "task": {"type": "string", "minLength": 20, "description": "The focused sub-task to delegate to a nested agent. Describe it self-containedly: the sub-agent starts with a fresh conversation and reports a single result back."},
    "name": {"type": "string", "description": "Short name for this delegation, shown in the UI: 2–4 words naming the job, e.g. \"scout config keys\". Give one."},
    "max_steps": {"type": "integer", "minimum": 1, "description": "optional; a lower cap for this delegation only, in Turns — see the Delegation bounds line of the host orientation for the configured cap; a request above it is clamped and the result says so."},
    "tools": {"type": ["string", "array"], "items": {"type": "string"}, "description": "optional; narrow the sub-agent's tools: the string \"read-only\" for the read-only set, or an array of tool names from your own menu. It can only remove tools, never add them; an unknown name is refused."},
    "continue": {"type": "string", "description": "optional; the name of a delegation earlier in this session — one you named, or one that was capped, faulted or stopped. The sub-agent restarts from its task and earlier reports with a fresh step cap; task says what to do next."},
    "output_path": {"type": "string", "description": "optional; the file the sub-agent is expected to write, relative to the workspace root or absolute. If it hits its step cap, write_file to this one path stays available for its final reply."},
    "run_on": {"type": "string", "enum": ["session", "sub-agents-server"], "description": "Optional; where this delegation runs — see the Delegations line of the host orientation. Leave unset for the configured default."}
  }
}`

const pinnedSubAgentSchemaBackground = `{
  "type": "object",
  "required": ["task"],
  "properties": {
    "task": {"type": "string", "minLength": 20, "description": "The focused sub-task to delegate to a nested agent. Describe it self-containedly: the sub-agent starts with a fresh conversation and reports a single result back."},
    "name": {"type": "string", "description": "Short name for this delegation, shown in the UI: 2–4 words naming the job, e.g. \"scout config keys\". Give one."},
    "max_steps": {"type": "integer", "minimum": 1, "description": "optional; a lower cap for this delegation only, in Turns — see the Delegation bounds line of the host orientation for the configured cap; a request above it is clamped and the result says so."},
    "tools": {"type": ["string", "array"], "items": {"type": "string"}, "description": "optional; narrow the sub-agent's tools: the string \"read-only\" for the read-only set, or an array of tool names from your own menu. It can only remove tools, never add them; an unknown name is refused."},
    "continue": {"type": "string", "description": "optional; the name of a delegation earlier in this session — one you named, or one that was capped, faulted or stopped. The sub-agent restarts from its task and earlier reports with a fresh step cap; task says what to do next."},
    "output_path": {"type": "string", "description": "optional; the file the sub-agent is expected to write, relative to the workspace root or absolute. If it hits its step cap, write_file to this one path stays available for its final reply."},
    "background": {"type": "boolean", "description": "Optional; true runs this delegation in the background and answers at once. You are woken with its report when it ends; use the workflow tool to check on it or stop it meanwhile. Not with continue."}
  }
}`

const pinnedSubAgentSchemaContext = `{
  "type": "object",
  "required": ["task"],
  "properties": {
    "task": {"type": "string", "minLength": 20, "description": "The focused sub-task to delegate to a nested agent. Describe it self-containedly: the sub-agent starts with a fresh conversation and reports a single result back."},
    "name": {"type": "string", "description": "Short name for this delegation, shown in the UI: 2–4 words naming the job, e.g. \"scout config keys\". Give one."},
    "max_steps": {"type": "integer", "minimum": 1, "description": "optional; a lower cap for this delegation only, in Turns — see the Delegation bounds line of the host orientation for the configured cap; a request above it is clamped and the result says so."},
    "tools": {"type": ["string", "array"], "items": {"type": "string"}, "description": "optional; narrow the sub-agent's tools: the string \"read-only\" for the read-only set, or an array of tool names from your own menu. It can only remove tools, never add them; an unknown name is refused."},
    "continue": {"type": "string", "description": "optional; the name of a delegation earlier in this session — one you named, or one that was capped, faulted or stopped. The sub-agent restarts from its task and earlier reports with a fresh step cap; task says what to do next."},
    "output_path": {"type": "string", "description": "optional; the file the sub-agent is expected to write, relative to the workspace root or absolute. If it hits its step cap, write_file to this one path stays available for its final reply."},
    "context": {"type": "array", "items": {"type": "string"}, "description": "Optional; files to hand the sub-agent: their contents are placed once ahead of its task, so it starts with them and need not read them itself. A path that cannot be read refuses the call."}
  }
}`

const pinnedSubAgentSchemaSeatChoiceBackground = `{
  "type": "object",
  "required": ["task"],
  "properties": {
    "task": {"type": "string", "minLength": 20, "description": "The focused sub-task to delegate to a nested agent. Describe it self-containedly: the sub-agent starts with a fresh conversation and reports a single result back."},
    "name": {"type": "string", "description": "Short name for this delegation, shown in the UI: 2–4 words naming the job, e.g. \"scout config keys\". Give one."},
    "max_steps": {"type": "integer", "minimum": 1, "description": "optional; a lower cap for this delegation only, in Turns — see the Delegation bounds line of the host orientation for the configured cap; a request above it is clamped and the result says so."},
    "tools": {"type": ["string", "array"], "items": {"type": "string"}, "description": "optional; narrow the sub-agent's tools: the string \"read-only\" for the read-only set, or an array of tool names from your own menu. It can only remove tools, never add them; an unknown name is refused."},
    "continue": {"type": "string", "description": "optional; the name of a delegation earlier in this session — one you named, or one that was capped, faulted or stopped. The sub-agent restarts from its task and earlier reports with a fresh step cap; task says what to do next."},
    "output_path": {"type": "string", "description": "optional; the file the sub-agent is expected to write, relative to the workspace root or absolute. If it hits its step cap, write_file to this one path stays available for its final reply."},
    "run_on": {"type": "string", "enum": ["session", "sub-agents-server"], "description": "Optional; where this delegation runs — see the Delegations line of the host orientation. Leave unset for the configured default."},
    "background": {"type": "boolean", "description": "Optional; true runs this delegation in the background and answers at once. You are woken with its report when it ends; use the workflow tool to check on it or stop it meanwhile. Not with continue."}
  }
}`

const pinnedSubAgentSchemaSeatChoiceContext = `{
  "type": "object",
  "required": ["task"],
  "properties": {
    "task": {"type": "string", "minLength": 20, "description": "The focused sub-task to delegate to a nested agent. Describe it self-containedly: the sub-agent starts with a fresh conversation and reports a single result back."},
    "name": {"type": "string", "description": "Short name for this delegation, shown in the UI: 2–4 words naming the job, e.g. \"scout config keys\". Give one."},
    "max_steps": {"type": "integer", "minimum": 1, "description": "optional; a lower cap for this delegation only, in Turns — see the Delegation bounds line of the host orientation for the configured cap; a request above it is clamped and the result says so."},
    "tools": {"type": ["string", "array"], "items": {"type": "string"}, "description": "optional; narrow the sub-agent's tools: the string \"read-only\" for the read-only set, or an array of tool names from your own menu. It can only remove tools, never add them; an unknown name is refused."},
    "continue": {"type": "string", "description": "optional; the name of a delegation earlier in this session — one you named, or one that was capped, faulted or stopped. The sub-agent restarts from its task and earlier reports with a fresh step cap; task says what to do next."},
    "output_path": {"type": "string", "description": "optional; the file the sub-agent is expected to write, relative to the workspace root or absolute. If it hits its step cap, write_file to this one path stays available for its final reply."},
    "run_on": {"type": "string", "enum": ["session", "sub-agents-server"], "description": "Optional; where this delegation runs — see the Delegations line of the host orientation. Leave unset for the configured default."},
    "context": {"type": "array", "items": {"type": "string"}, "description": "Optional; files to hand the sub-agent: their contents are placed once ahead of its task, so it starts with them and need not read them itself. A path that cannot be read refuses the call."}
  }
}`

const pinnedSubAgentSchemaBackgroundContext = `{
  "type": "object",
  "required": ["task"],
  "properties": {
    "task": {"type": "string", "minLength": 20, "description": "The focused sub-task to delegate to a nested agent. Describe it self-containedly: the sub-agent starts with a fresh conversation and reports a single result back."},
    "name": {"type": "string", "description": "Short name for this delegation, shown in the UI: 2–4 words naming the job, e.g. \"scout config keys\". Give one."},
    "max_steps": {"type": "integer", "minimum": 1, "description": "optional; a lower cap for this delegation only, in Turns — see the Delegation bounds line of the host orientation for the configured cap; a request above it is clamped and the result says so."},
    "tools": {"type": ["string", "array"], "items": {"type": "string"}, "description": "optional; narrow the sub-agent's tools: the string \"read-only\" for the read-only set, or an array of tool names from your own menu. It can only remove tools, never add them; an unknown name is refused."},
    "continue": {"type": "string", "description": "optional; the name of a delegation earlier in this session — one you named, or one that was capped, faulted or stopped. The sub-agent restarts from its task and earlier reports with a fresh step cap; task says what to do next."},
    "output_path": {"type": "string", "description": "optional; the file the sub-agent is expected to write, relative to the workspace root or absolute. If it hits its step cap, write_file to this one path stays available for its final reply."},
    "background": {"type": "boolean", "description": "Optional; true runs this delegation in the background and answers at once. You are woken with its report when it ends; use the workflow tool to check on it or stop it meanwhile. Not with continue."},
    "context": {"type": "array", "items": {"type": "string"}, "description": "Optional; files to hand the sub-agent: their contents are placed once ahead of its task, so it starts with them and need not read them itself. A path that cannot be read refuses the call."}
  }
}`

const pinnedSubAgentSchemaAll = `{
  "type": "object",
  "required": ["task"],
  "properties": {
    "task": {"type": "string", "minLength": 20, "description": "The focused sub-task to delegate to a nested agent. Describe it self-containedly: the sub-agent starts with a fresh conversation and reports a single result back."},
    "name": {"type": "string", "description": "Short name for this delegation, shown in the UI: 2–4 words naming the job, e.g. \"scout config keys\". Give one."},
    "max_steps": {"type": "integer", "minimum": 1, "description": "optional; a lower cap for this delegation only, in Turns — see the Delegation bounds line of the host orientation for the configured cap; a request above it is clamped and the result says so."},
    "tools": {"type": ["string", "array"], "items": {"type": "string"}, "description": "optional; narrow the sub-agent's tools: the string \"read-only\" for the read-only set, or an array of tool names from your own menu. It can only remove tools, never add them; an unknown name is refused."},
    "continue": {"type": "string", "description": "optional; the name of a delegation earlier in this session — one you named, or one that was capped, faulted or stopped. The sub-agent restarts from its task and earlier reports with a fresh step cap; task says what to do next."},
    "output_path": {"type": "string", "description": "optional; the file the sub-agent is expected to write, relative to the workspace root or absolute. If it hits its step cap, write_file to this one path stays available for its final reply."},
    "run_on": {"type": "string", "enum": ["session", "sub-agents-server"], "description": "Optional; where this delegation runs — see the Delegations line of the host orientation. Leave unset for the configured default."},
    "background": {"type": "boolean", "description": "Optional; true runs this delegation in the background and answers at once. You are woken with its report when it ends; use the workflow tool to check on it or stop it meanwhile. Not with continue."},
    "context": {"type": "array", "items": {"type": "string"}, "description": "Optional; files to hand the sub-agent: their contents are placed once ahead of its task, so it starts with them and need not read them itself. A path that cannot be read refuses the call."}
  }
}`

const pinnedFanOutSchemaPlain = `{
  "type": "object",
  "properties": {
    "task": {"type": "string", "minLength": 20, "description": "The brief every helper gets, one helper per item: {item} is replaced by the item and {out} by the file that helper writes its detail to. Write it self-containedly; each helper starts fresh. Required unless recipe is set."},
    "over": {"type": ["array", "object"], "items": {"type": "string"}, "properties": {"files": {"type": "string"}, "lines": {"type": "string"}, "split": {"type": "string"}}, "description": "The items: an array of strings, or an object with one of files (a workspace glob, ** allowed; each match is an item), lines (a file whose non-blank lines are the items) or split (a directory cut into parts sized for one helper). Required unless recipe is set."},
    "batch": {"type": "integer", "minimum": 1, "description": "optional; how many items one helper gets (default 1)."},
    "context": {"type": "array", "items": {"type": "string"}, "description": "optional; files every helper reads before its item."},
    "returns": {"type": "object", "additionalProperties": {"type": "string"}, "description": "optional; the typed fields each helper reports beside its status and one-line summary, as name: type — int, text, list, or an enum written a|b|c. Example: {\"findings\": \"int\"}."},
    "out": {"type": "string", "description": "optional; where each helper writes its detail output, with {item} in the path. Leave unset and each item gets a file in the workflow folder."},
    "verify": {"type": "object", "properties": {"when": {"type": "string", "description": "optional; which items to check, as a condition on the returns fields, e.g. \"findings > 0\". Unset checks every item."}, "task": {"type": "string", "description": "optional; what the checker should look at, with {item}."}}, "description": "optional; one adversarial check that tries to refute each selected item's report."},
    "merge": {"type": "object", "required": ["task"], "properties": {"task": {"type": "string", "description": "The brief for the one helper that reads every item's output and writes the combined report."}}, "description": "optional; one helper over all the items' outputs."},
    "tools": {"type": "array", "items": {"type": "string"}, "description": "optional; narrow each helper's tools to these names from your own menu. It can only remove tools, never add them."},
    "recipe": {"type": "string", "description": "optional; the name of a recipe to run instead of describing the fan-out yourself. Give only inputs with it."},
    "inputs": {"type": "object", "additionalProperties": {"type": "string"}, "description": "optional; the recipe's declared inputs, as name: value."}
  }
}`

const pinnedFanOutSchemaSeatChoice = `{
  "type": "object",
  "properties": {
    "task": {"type": "string", "minLength": 20, "description": "The brief every helper gets, one helper per item: {item} is replaced by the item and {out} by the file that helper writes its detail to. Write it self-containedly; each helper starts fresh. Required unless recipe is set."},
    "over": {"type": ["array", "object"], "items": {"type": "string"}, "properties": {"files": {"type": "string"}, "lines": {"type": "string"}, "split": {"type": "string"}}, "description": "The items: an array of strings, or an object with one of files (a workspace glob, ** allowed; each match is an item), lines (a file whose non-blank lines are the items) or split (a directory cut into parts sized for one helper). Required unless recipe is set."},
    "batch": {"type": "integer", "minimum": 1, "description": "optional; how many items one helper gets (default 1)."},
    "context": {"type": "array", "items": {"type": "string"}, "description": "optional; files every helper reads before its item."},
    "returns": {"type": "object", "additionalProperties": {"type": "string"}, "description": "optional; the typed fields each helper reports beside its status and one-line summary, as name: type — int, text, list, or an enum written a|b|c. Example: {\"findings\": \"int\"}."},
    "out": {"type": "string", "description": "optional; where each helper writes its detail output, with {item} in the path. Leave unset and each item gets a file in the workflow folder."},
    "verify": {"type": "object", "properties": {"when": {"type": "string", "description": "optional; which items to check, as a condition on the returns fields, e.g. \"findings > 0\". Unset checks every item."}, "task": {"type": "string", "description": "optional; what the checker should look at, with {item}."}}, "description": "optional; one adversarial check that tries to refute each selected item's report."},
    "merge": {"type": "object", "required": ["task"], "properties": {"task": {"type": "string", "description": "The brief for the one helper that reads every item's output and writes the combined report."}}, "description": "optional; one helper over all the items' outputs."},
    "tools": {"type": "array", "items": {"type": "string"}, "description": "optional; narrow each helper's tools to these names from your own menu. It can only remove tools, never add them."},
    "recipe": {"type": "string", "description": "optional; the name of a recipe to run instead of describing the fan-out yourself. Give only inputs with it."},
    "inputs": {"type": "object", "additionalProperties": {"type": "string"}, "description": "optional; the recipe's declared inputs, as name: value."},
    "run_on": {"type": "string", "enum": ["session", "sub-agents-server"], "description": "Optional; where the helpers run — see the Delegations line of the host orientation. Leave unset for the configured default."}
  }
}`

const pinnedFanOutSchemaBackground = `{
  "type": "object",
  "properties": {
    "task": {"type": "string", "minLength": 20, "description": "The brief every helper gets, one helper per item: {item} is replaced by the item and {out} by the file that helper writes its detail to. Write it self-containedly; each helper starts fresh. Required unless recipe is set."},
    "over": {"type": ["array", "object"], "items": {"type": "string"}, "properties": {"files": {"type": "string"}, "lines": {"type": "string"}, "split": {"type": "string"}}, "description": "The items: an array of strings, or an object with one of files (a workspace glob, ** allowed; each match is an item), lines (a file whose non-blank lines are the items) or split (a directory cut into parts sized for one helper). Required unless recipe is set."},
    "batch": {"type": "integer", "minimum": 1, "description": "optional; how many items one helper gets (default 1)."},
    "context": {"type": "array", "items": {"type": "string"}, "description": "optional; files every helper reads before its item."},
    "returns": {"type": "object", "additionalProperties": {"type": "string"}, "description": "optional; the typed fields each helper reports beside its status and one-line summary, as name: type — int, text, list, or an enum written a|b|c. Example: {\"findings\": \"int\"}."},
    "out": {"type": "string", "description": "optional; where each helper writes its detail output, with {item} in the path. Leave unset and each item gets a file in the workflow folder."},
    "verify": {"type": "object", "properties": {"when": {"type": "string", "description": "optional; which items to check, as a condition on the returns fields, e.g. \"findings > 0\". Unset checks every item."}, "task": {"type": "string", "description": "optional; what the checker should look at, with {item}."}}, "description": "optional; one adversarial check that tries to refute each selected item's report."},
    "merge": {"type": "object", "required": ["task"], "properties": {"task": {"type": "string", "description": "The brief for the one helper that reads every item's output and writes the combined report."}}, "description": "optional; one helper over all the items' outputs."},
    "tools": {"type": "array", "items": {"type": "string"}, "description": "optional; narrow each helper's tools to these names from your own menu. It can only remove tools, never add them."},
    "recipe": {"type": "string", "description": "optional; the name of a recipe to run instead of describing the fan-out yourself. Give only inputs with it."},
    "inputs": {"type": "object", "additionalProperties": {"type": "string"}, "description": "optional; the recipe's declared inputs, as name: value."},
    "background": {"type": "boolean", "description": "Optional; true starts the workflow in the background and answers at once. You are woken with its result when it ends; use the workflow tool to check on it or stop it meanwhile."}
  }
}`

const pinnedFanOutSchemaSeatChoiceBackground = `{
  "type": "object",
  "properties": {
    "task": {"type": "string", "minLength": 20, "description": "The brief every helper gets, one helper per item: {item} is replaced by the item and {out} by the file that helper writes its detail to. Write it self-containedly; each helper starts fresh. Required unless recipe is set."},
    "over": {"type": ["array", "object"], "items": {"type": "string"}, "properties": {"files": {"type": "string"}, "lines": {"type": "string"}, "split": {"type": "string"}}, "description": "The items: an array of strings, or an object with one of files (a workspace glob, ** allowed; each match is an item), lines (a file whose non-blank lines are the items) or split (a directory cut into parts sized for one helper). Required unless recipe is set."},
    "batch": {"type": "integer", "minimum": 1, "description": "optional; how many items one helper gets (default 1)."},
    "context": {"type": "array", "items": {"type": "string"}, "description": "optional; files every helper reads before its item."},
    "returns": {"type": "object", "additionalProperties": {"type": "string"}, "description": "optional; the typed fields each helper reports beside its status and one-line summary, as name: type — int, text, list, or an enum written a|b|c. Example: {\"findings\": \"int\"}."},
    "out": {"type": "string", "description": "optional; where each helper writes its detail output, with {item} in the path. Leave unset and each item gets a file in the workflow folder."},
    "verify": {"type": "object", "properties": {"when": {"type": "string", "description": "optional; which items to check, as a condition on the returns fields, e.g. \"findings > 0\". Unset checks every item."}, "task": {"type": "string", "description": "optional; what the checker should look at, with {item}."}}, "description": "optional; one adversarial check that tries to refute each selected item's report."},
    "merge": {"type": "object", "required": ["task"], "properties": {"task": {"type": "string", "description": "The brief for the one helper that reads every item's output and writes the combined report."}}, "description": "optional; one helper over all the items' outputs."},
    "tools": {"type": "array", "items": {"type": "string"}, "description": "optional; narrow each helper's tools to these names from your own menu. It can only remove tools, never add them."},
    "recipe": {"type": "string", "description": "optional; the name of a recipe to run instead of describing the fan-out yourself. Give only inputs with it."},
    "inputs": {"type": "object", "additionalProperties": {"type": "string"}, "description": "optional; the recipe's declared inputs, as name: value."},
    "run_on": {"type": "string", "enum": ["session", "sub-agents-server"], "description": "Optional; where the helpers run — see the Delegations line of the host orientation. Leave unset for the configured default."},
    "background": {"type": "boolean", "description": "Optional; true starts the workflow in the background and answers at once. You are woken with its result when it ends; use the workflow tool to check on it or stop it meanwhile."}
  }
}`
