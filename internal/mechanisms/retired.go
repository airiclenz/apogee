package mechanisms

import (
	"fmt"
	"slices"
)

// retiredRow is one entry on the roll: the catalogue ID, the release whose notes carry its
// retirement, and — for a PROMOTED row, one retired because its behaviour became engine behaviour —
// the top-level configuration key that governs that behaviour now. Successor is empty for a row
// retired outright, which is what tells the two apart everywhere the roll is read.
type retiredRow struct {
	ID        string
	Release   string
	Successor string
}

// retired is the roll of catalogue IDs apogee once carried and no longer does. It exists so a
// user's saved configuration survives a deletion: a `mechanisms:` key, a per-server delegation
// posture or a Validated-set record naming a retired ID was valid at the release before the
// removal, and the removal must not turn it into a refusal (AGENTS.md: a value apogee itself
// accepted yesterday and rejects today is a regression, never a deferral).
//
// A row joins this roll on A RATIFIED VERDICT (ADR 0071) or when it was INERT BY CONSTRUCTION at
// the moment it was retired — it could not fire on any backend the release supported. Inertness is
// what makes dropping a row from a Validated set behaviour-preserving rather than a silent
// re-tuning of a validated stack (ADR 0016's whole-set-or-nothing amendment, 2026-08-29); a
// ratified verdict is the wider door ADR 0071 opened, where the owner decides a row's evidence no
// longer earns it a place and the set's evidence is void without it either way.
//
// Each row carries its OWN release because the roll now spans more than one wave: a single const
// could only ever be right for whichever removal went last, and the notices below name the release
// a user would look up in the changelog.
//
//   - grammar (retired 2026-08-29) — the json_schema `response_format` shaper. Its only gate was a
//     backend-capability field on Deps that nothing ever populated, over a provider wire that
//     carries no response_format field, so it no-op'd on every backend from the port onwards.
//   - tool_loop_interceptor (retired v0.20.0) — PROMOTED. The identical-repeat-turn detector became
//     the `tool-loop-breaker` Floor guard: it changes only what the model sees after its own
//     failure, so it needs no per-model proof and runs for every model (ADR 0071).
//   - validate (retired v0.20.0) — PROMOTED. The tool-call validator became the `tool-call-repair`
//     Floor guard, on the same reasoning.
//   - empty_response_recovery (retired v0.20.0) — PROMOTED. The empty-reply recovery became the
//     `empty-response-recovery` Floor guard; it always ran under Bypass, so promoting it changes
//     who gets it, not what it does.
//   - tool_use_enforcer (retired v0.20.0) — PROMOTED. The narration off-ramp became the
//     `tool-use-enforcer` Floor guard, on the same reasoning.
//   - cached_content_intercept (retired v0.20.0) — PROMOTED. The redundant-re-read interceptor
//     became the `read-cache` Floor guard: it shapes the pending read without steering the model,
//     so it needs no per-model proof and runs for every model (ADR 0071).
//   - tool_result_cap (retired v0.20.0) — PROMOTED. The per-result trimmer became the
//     `tool-result-cap` Floor guard: it shrinks what an old result costs in the projected request
//     and tells the model how to read the omitted range back, so it shapes without steering.
//   - stall_nudge, list_nudge, tool_use_directive (retired v0.20.0) — RETIRED OUTRIGHT on a
//     ratified verdict (ADR 0071). The three completion nudges apogee-sim's `cot` Transform split
//     into steered the model with proactive directives, which is exactly what a Floor guard may not
//     do, and no per-model A/B ever earned them a place (ADR 0009's gate).
//   - decompose (retired v0.20.0) — RETIRED OUTRIGHT on the same verdict. The task-decomposition
//     steer collapsed complex prompts in history and hinted a single next step into the system
//     prompt: it rewrites what the human asked for, so it steers rather than shapes.
//   - guided_decomposition (retired v0.20.0) — RETIRED OUTRIGHT on the same verdict. The
//     enumeration steer and its batched sub_agent fan-out (ADR 0014) told the model how to plan
//     its own work and then dispatched delegations it never asked for: the most steering row the
//     catalogue carried, and no per-model A/B ever earned it a place (ADR 0009's gate).
//   - filehint (retired v0.20.0) — RETIRED OUTRIGHT on the same verdict. Scoring a freshly listed
//     directory against the prompt and telling the model which files to read first picks the model's
//     next move for it, and no per-model A/B ever earned it a place (ADR 0009's gate).
//   - read_loop (retired v0.20.0) — RETIRED OUTRIGHT on the same verdict. The re-read detector's
//     hints went past shaping into instruction — "the workspace is empty, create X" — deriving a
//     write target from the prompt and steering the model at it.
//   - toolfilter (retired v0.20.0) — RETIRED OUTRIGHT on the same verdict. Scoring the tool menu
//     down to ten entries decides for the model what it is allowed to reach for, and a wrongly
//     trimmed menu removes the tool the task needed; `tools.disabled` is the answer to a menu a
//     model cannot handle.
//   - truncate_history (retired v0.20.0) — RETIRED OUTRIGHT on the same verdict. The
//     drop-the-middle history rewrite was the cheap A/B alternative to generative Compaction and
//     never earned its place against it (ADR 0009's gate); dropping the middle of a conversation
//     silently is a context decision the structural Compaction path makes better and announces.
//   - error_enrichment (retired v0.20.0) — RETIRED OUTRIGHT on the same verdict. Appending
//     category-shaped suggestions to a repeated tool error tells the model what to try next
//     instead of handing it what the tool said, which is steering rather than shaping.
//   - read_repeat (retired v0.20.0) — RETIRED OUTRIGHT on the same verdict. It was the twin of the
//     promoted redundant-re-read interceptor — the two were mutually exclusive on one symptom and
//     only the interceptor carried evidence — so the read-cache Floor guard covers the symptom for
//     every model and the hinting half goes.
//   - syntax (retired v0.20.0) — RETIRED OUTRIGHT on the same verdict. Re-streaming the Turn because
//     a written file failed a syntax check corrects the model's work for it on a signal the tool
//     result would have carried anyway, and no per-model A/B ever earned it a place (ADR 0009's
//     gate). The structural syntax trailer (internal/syntaxcheck, plan 2026-09-02 - 00) is the
//     shaping answer to the same symptom and stays.
//   - autofix (retired v0.20.0) — RETIRED OUTRIGHT on the same verdict. Handing a written payload to
//     an external formatter in a gated sub-process rewrote the model's file behind its back —
//     a mutation of the work, not of what the model sees — and paid a per-write subprocess for it.
//   - library (retired v0.20.0) — RETIRED OUTRIGHT on the same verdict. The cross-session
//     observation store injected a block of remembered per-model behavioural notes into the system
//     prompt, which tells the model what to do before it has done anything wrong — steering, not
//     shaping — and no per-model A/B ever earned it a place (ADR 0009's gate). The store it owned
//     went with it (internal/library keeps only the fingerprint resolver and the probe record);
//     an existing ~/.apogee/library is left on disk, simply never read again.
var retired = []retiredRow{
	{ID: "grammar", Release: "v0.18.7"},
	{ID: "tool_loop_interceptor", Release: "v0.20.0", Successor: "tool-loop-breaker"},
	{ID: "validate", Release: "v0.20.0", Successor: "tool-call-repair"},
	{ID: "empty_response_recovery", Release: "v0.20.0", Successor: "empty-response-recovery"},
	{ID: "tool_use_enforcer", Release: "v0.20.0", Successor: "tool-use-enforcer"},
	{ID: "cached_content_intercept", Release: "v0.20.0", Successor: "read-cache"},
	{ID: "tool_result_cap", Release: "v0.20.0", Successor: "tool-result-cap"},
	{ID: "stall_nudge", Release: "v0.20.0"},
	{ID: "list_nudge", Release: "v0.20.0"},
	{ID: "tool_use_directive", Release: "v0.20.0"},
	{ID: "decompose", Release: "v0.20.0"},
	{ID: "guided_decomposition", Release: "v0.20.0"},
	{ID: "filehint", Release: "v0.20.0"},
	{ID: "read_loop", Release: "v0.20.0"},
	{ID: "toolfilter", Release: "v0.20.0"},
	{ID: "truncate_history", Release: "v0.20.0"},
	{ID: "error_enrichment", Release: "v0.20.0"},
	{ID: "read_repeat", Release: "v0.20.0"},
	{ID: "syntax", Release: "v0.20.0"},
	{ID: "autofix", Release: "v0.20.0"},
	{ID: "library", Release: "v0.20.0"},
}

// RetiredIDs returns the retired catalogue IDs, sorted, as a fresh slice the caller may keep. The
// live catalogue is gone, so this roll is the WHOLE vocabulary the `mechanisms:` key still knows:
// an ID that is not on it is unknown, and unknown stays a loud error everywhere a retired ID is
// quietly dropped.
func RetiredIDs() []string {
	out := make([]string, 0, len(retired))
	for _, r := range retired {
		out = append(out, r.ID)
	}
	slices.Sort(out)
	return out
}

// IsRetired reports whether id names a lab row this build retired.
func IsRetired(id string) bool { return rowFor(id) != nil }

// RetiredRelease returns the version whose notes carry id's retirement, or "" when id is not on the
// roll. It is a per-ID lookup rather than one const because the roll spans several waves, and every
// caller that words a notice about a removed ID — this package's own, a Validated-set shed line —
// must name the release that ID actually went in.
func RetiredRelease(id string) string {
	if r := rowFor(id); r != nil {
		return r.Release
	}
	return ""
}

// Successor returns the top-level configuration key that now governs the behaviour id used to
// provide, or "" when id retired outright (and for an ID that is not on the roll at all). A
// non-empty answer is what turns a retired-ID notice from "this does nothing, delete it" into
// "this is engine behaviour now, and here is the key that switches it".
func Successor(id string) string {
	if r := rowFor(id); r != nil {
		return r.Successor
	}
	return ""
}

// rowFor finds id's roll entry, or nil. The roll is a handful of rows walked linearly rather than a
// map, so the source order stays the documentation it is.
func rowFor(id string) *retiredRow {
	for i := range retired {
		if retired[i].ID == id {
			return &retired[i]
		}
	}
	return nil
}

// RetiredNotices validates a `mechanisms:` map against the retired roll and hands back the
// user-facing lines it earns. The catalogue is permanently EMPTY, so the key arms NOTHING (ADR
// 0076 D11 — the block parses, notices, and drives nothing); the key survives so a saved
// configuration is never refused, and the notices survive so a configuration still asking for a
// removed row says so once instead of going quiet.
//
// EVERY key is validated, enabled AND disabled: a typo'd DISABLED key must still fail loudly at
// this startup boundary rather than pass unread. An unknown key — one that is not on the roll —
// returns the error and no lines: a refused block never reached the point of tolerating anything.
// Keys are walked in sorted spelling so the lines and any error over them are deterministic.
//
// A RETIRED ID is tolerated whatever its value: the key was valid at the release before the
// removal, so refusing it would break a configuration the user never edited. Which values earn a
// line depends on whether the row was PROMOTED (Successor non-empty):
//
//   - Retired OUTRIGHT and set true: the plain line naming the release and asking for the key's
//     removal. Set FALSE it earns no line — the user is not asking for it, and telling them to
//     delete a key that already disables nothing is noise.
//   - PROMOTED and set true: the behaviour is still there, so the line says so and names the
//     top-level key that governs it now.
//   - PROMOTED and set FALSE: this is the one case where silence would MISLEAD. The user wrote a
//     key to switch the behaviour off, and it no longer does — so the line says the old spelling
//     stopped working and names the top-level key that does it, rather than leaving them believing
//     a guard is off when it is on.
//
// It PRINTS nothing itself — the caller decides where the lines go, because only a pre-TUI path
// may write to stderr, a live apply folds them into the answer its pane renders, and a delegate's
// posture discards them.
func RetiredNotices(enabled map[string]bool) (notices []string, err error) {
	keys := make([]string, 0, len(enabled))
	for key := range enabled {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	var noticed []string
	for _, key := range keys {
		if !IsRetired(key) {
			return nil, fmt.Errorf("apogee: unknown mechanism %q; known: %s", key, knownList)
		}
		if enabled[key] || Successor(key) != "" {
			noticed = append(noticed, key)
		}
	}

	for _, key := range noticed {
		switch successor := Successor(key); {
		case successor == "":
			notices = append(notices, fmt.Sprintf(
				"apogee: mechanism %q was retired in %s and is ignored; remove it from mechanisms:",
				key, RetiredRelease(key)))
		case enabled[key]:
			notices = append(notices, fmt.Sprintf(
				"apogee: mechanism %q is the %q floor guard since %s and is on by default; remove it from mechanisms:",
				key, successor, RetiredRelease(key)))
		default:
			notices = append(notices, fmt.Sprintf(
				"apogee: mechanism %q is the %q floor guard since %s; \"%s: false\" under mechanisms: "+
					"no longer turns it off — set %s: false at the top level",
				key, successor, RetiredRelease(key), key, successor))
		}
	}
	return notices, nil
}

// knownList is the tail the unknown-key error names. The catalogue is empty and stays empty, so
// there is never anything to list — "(none)" is the same rendering the resolver produced over an
// empty table before the catalogue was deleted.
const knownList = "(none)"
