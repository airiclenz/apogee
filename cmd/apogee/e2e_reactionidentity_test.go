package main

// The Reaction identity arm (ADR 0076, plan `2026-09-07 - 01` item 1): the firing sequence apogee
// produces today, recorded before the Reaction core replaces the Floor-guard and Mechanism
// plumbing that produces it.
//
// The claim the stage-1 refactor makes is BEHAVIOUR-IDENTICAL, and nothing below the driven run
// can make it. A unit test proves a guard's decision; only a scripted run end to end proves the
// SEQUENCE — which reaction acted, at which Moment, in which Turn, with what to show for it — and
// that sequence is exactly what a refactor of the ladder underneath is able to change without
// breaking a single unit test.
//
// So each case is projected rather than compared raw: the goldens hold one line per firing and
// nothing else, so the eventlines goldens' own churn (a new kind, a member, a usage number) cannot
// move them and a reordered or lost firing cannot hide in them. The projector reads all THREE
// firing kinds — today's `floor_guard` and `mechanism_fired`, and the `reaction_fired` that
// replaces both — so the same goldens keep their meaning across the seam that swaps one for the
// others, and no later item of the plan touches this file's projection.
//
// The goldens are RECORDED once, here, and frozen: from this item on a diff is a finding, never
// something to re-record (the plan's standing requirements).

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// identityGolden names one projection golden. They sit beside the Event lines goldens because they
// are taken off the same stream, and they are `.txt` rather than `.jsonl` because what is pinned is
// the projection this file writes, not the protocol's own bytes.
func identityGolden(name string) string {
	return filepath.Join("testdata", "eventlines", "identity-"+name+".txt")
}

// identityLineGrammar is the projection's own shape, asserted on every line before it reaches a
// golden. It is not redundant with the formatter below: a member that vanished from the stream
// would otherwise be projected as an empty or `<nil>` value and recorded as one, and a golden
// recorded from a broken projection pins nothing.
var identityLineGrammar = regexp.MustCompile(
	`^turn=\d+ depth=\d+ reaction=\S+ moment=\S+ action=\S+ detail=.*$`)

// reactionIdentityCase is one driven run whose firings are recorded.
//
// The prompt is per case rather than shared: the fixtures match on their own opening words, and a
// shared prompt that matched none of a script's turns would fall through to its catch-all and
// record an empty sequence that looks like a pass. The expected exit is per case for the same
// reason — a run whose final Turn the engine abandons is exit 3, and fataling on any error at all
// would lose that case rather than record it.
type reactionIdentityCase struct {
	// name is the case's own name and the suffix of its golden.
	name string
	// script is the stubllm fixture under testdata/stubllm/ the run talks to.
	script string
	// prompt is what the run asks. It must match the script's first turn.
	prompt string
	// target is the reaction id this case exists to record. A projection without it is a
	// failure: an empty or off-target golden would freeze the absence of the very firing the
	// case was written for.
	target string
	// absent names reactions this case's script is written to keep OUT of the way of its
	// target — a guard that would fire first and short-circuit the one being recorded. The
	// golden would show it, but only to a reader who knew to look; naming it here says which
	// firing the script is steering around, and fails on the script rather than on the diff.
	absent []string
	// exitCode is the process exit the run is expected to ask for.
	exitCode int
	// workspace builds the scratch workspace the run edits; nil means the shared one-file
	// workspace every driven run gets.
	workspace func(t *testing.T) string
}

// reactionIdentityCases is one row per Floor guard the arm can reach through a single-prompt
// headless run.
//
// `tool-use-enforcer` has no row, and that is a pinned exemption rather than an oversight:
// floor.EnforceToolUse needs a COMMITTED text-only assistant message with no prior tool use
// (internal/floor/tooluse.go), and a single-prompt headless run commits an assistant message only
// by making a tool call — which is the very thing the guard requires never happened. Its builtin is
// covered by a unit test instead (plan item 5).
var reactionIdentityCases = []reactionIdentityCase{
	{
		name:   "salvage",
		script: "salvage",
		// salvage.yaml matches `^Read a.txt`; the shared eventlines prompt matches none of its
		// turns and would be answered by its catch-all, firing nothing.
		prompt:   "Read a.txt and tell me what is in it.",
		target:   "tool-call-salvage",
		exitCode: 0,
	},
	{
		name:   "empty-reply",
		script: "empty-reply",
		prompt: eventLinesPrompt,
		target: "empty-response-recovery",
		// Every reply is nothing at all, so the recovery guard spends the Turn's retry budget and
		// the engine abandons the final Turn: exit 3, with the stream written on that path too.
		exitCode: exitRunFaulted,
	},
	{
		name:     "loop-breaker",
		script:   "identity-loop-breaker",
		prompt:   "Read a.txt and tell me what is in it.",
		target:   "tool-loop-breaker",
		exitCode: 0,
	},
	{
		name:     "repair",
		script:   "identity-repair",
		prompt:   "Read a.txt and tell me what is in it.",
		target:   "tool-call-repair",
		exitCode: 0,
	},
	{
		name:   "read-cache",
		script: "identity-read-cache",
		prompt: "Read a.txt and tell me what is in it.",
		target: "read-cache",
		// The re-read is the same file spelled another way precisely so the loop breaker stands
		// down: it compares the previous tool-calling Turn's raw argument bytes, and a verbatim
		// repeat would be retried at post-response before the pre-tool-exec seam ever ran.
		absent:   []string{"tool-loop-breaker"},
		exitCode: 0,
	},
	{
		name:   "result-cap",
		script: "identity-result-cap",
		// The prompt names the big file first because the script matches its opening turn on it,
		// and the second read is what moves the big result behind the guard's protected frontier.
		prompt:    "Read big.txt and then a.txt.",
		target:    "tool-result-cap",
		exitCode:  0,
		workspace: resultCapWorkspace,
	},
}

// The tool-result cap's own arithmetic, which is what sizes the workspace this row reads.
//
// A result is trimmed when it is STRICTLY longer than the per-result ceiling
// (internal/floor/resultcap.go capMaxChars): the working window — the pinned 32768-token window
// (eventLinesHome) less the 20% reply reserve — times the uncalibrated 4.0 chars-per-token ratio
// (internal/context, and the fixtures script no usage, so nothing calibrates it), times the 0.4
// budget fraction. That is ~41.9k characters.
//
// There is a ceiling ABOVE it too, and the file has to stay under that one: a result estimated
// above the whole History allocation — 60% of the same working window, ~62.9k characters — is
// elided on its way INTO the conversation by the structural clamp (internal/agent/dispatch.go
// clampToolResult), and a clamped result never grows past the guard's ceiling to be capped at all.
//
// 1000 lines of 53 characters plus their newlines is ~54k: comfortably over the first and under the
// second, with room for the header read_file prepends and for either fraction to be re-tuned by a
// little without silently turning this row into a run that records nothing.
const (
	resultCapFileLines = 1000
	resultCapFileLine  = "filler line for the tool-result cap identity row"
)

// resultCapWorkspace is the shared one-file workspace plus the oversized file the cap row reads.
// The lines are numbered rather than identical so the head/tail elision the guard renders is a
// visible trim of a real file when a failure prints one.
func resultCapWorkspace(t *testing.T) string {
	t.Helper()

	ws := e2eWorkspace(t)
	var big strings.Builder
	for i := 0; i < resultCapFileLines; i++ {
		fmt.Fprintf(&big, "%04d %s\n", i, resultCapFileLine)
	}
	if err := os.WriteFile(filepath.Join(ws, "big.txt"), []byte(big.String()), 0o600); err != nil {
		t.Fatalf("seed the tool-result cap workspace: %v", err)
	}
	return ws
}

// TestE2EReactionIdentity records — and from here on holds — the sequence of reaction firings each
// scripted run produces.
func TestE2EReactionIdentity(t *testing.T) {
	for _, tc := range reactionIdentityCases {
		t.Run(tc.name, func(t *testing.T) {
			stub := stubllm.New(t, loadScript(t, tc.script))
			workspace := tc.workspaceDir(t)
			home := eventLinesHome(t, stub.URL, stub.Model)

			out, errOut, err := headlessEventLines(t, run.Once, home, workspace, tc.prompt)
			tc.assertExit(t, err, errOut)

			projection := projectReactionFirings(t, jsonEventLines(t, out))
			if len(projection) == 0 {
				t.Fatalf("the run fired nothing at all; %s was meant to fire %q\nstream:\n%s",
					tc.script, tc.target, out)
			}
			joined := strings.Join(projection, "\n")
			if !strings.Contains(joined, "reaction="+tc.target+" ") {
				t.Fatalf("no firing of %q in the projection; this case exists to record it:\n%s",
					tc.target, joined)
			}
			for _, unwanted := range tc.absent {
				if strings.Contains(joined, "reaction="+unwanted+" ") {
					t.Fatalf("%q fired; %s is scripted to keep it out of the way of %q, so its "+
						"firing means the script no longer records what it was written for:\n%s",
						unwanted, tc.script, tc.target, joined)
				}
			}

			tuitest.GoldenText(t, identityGolden(tc.name), strings.Join(projection, "\n"))
		})
	}
}

// workspaceDir builds the case's workspace, defaulting to the shared one every driven run gets.
func (tc reactionIdentityCase) workspaceDir(t *testing.T) string {
	t.Helper()

	if tc.workspace != nil {
		return tc.workspace(t)
	}
	return e2eWorkspace(t)
}

// assertExit holds the case's own exit code. A run that fires guards until its final Turn is
// abandoned is a legitimate row of this arm, and it exits 3.
func (tc reactionIdentityCase) assertExit(t *testing.T, err error, errOut string) {
	t.Helper()

	if tc.exitCode == 0 {
		if err != nil {
			t.Fatalf("the scripted run returned %v; want a clean exit\nstderr:\n%s", err, errOut)
		}
		return
	}
	if err == nil {
		t.Fatalf("the scripted run exited cleanly; want exit %d\nstderr:\n%s", tc.exitCode, errOut)
	}
	if code := exitCodeFor(err); code != tc.exitCode {
		t.Fatalf("the scripted run exited %d (%v); want %d\nstderr:\n%s",
			code, err, tc.exitCode, errOut)
	}
}

// projectReactionFirings renders every firing line of the stream, in stream order, as one text line
// naming who fired, where, and what it did.
//
// The projection is the arm's whole point: it spans the three kinds a firing can arrive as, so the
// same golden reads a stream written before the Reaction core and one written after it. The mapping
// is fixed here and nowhere else —
//
//   - `floor_guard`: id `data.guard`, Moment derived from the guard's own seam (the guards are the
//     one firing kind whose line does not carry its seam), action `data.action`, detail
//     `data.detail`;
//   - `mechanism_fired`: id `data.mechanism`, Moment `data.hook`, action `data.action`, no detail —
//     the kind carries none;
//   - `reaction_fired`: every member read straight off the line.
func projectReactionFirings(t *testing.T, lines []map[string]any) []string {
	t.Helper()

	var projected []string
	for i, line := range lines {
		var id, moment, action, detail string
		switch line["event"] {
		case "floor_guard":
			id = stringMember(t, i, line, "guard")
			moment = guardMoments[id]
			if moment == "" {
				t.Fatalf("line %d: no Moment is mapped for the guard %q; the projection table "+
					"has to name every guard's seam", i+1, id)
			}
			action = stringMember(t, i, line, "action")
			detail = stringMember(t, i, line, "detail")
		case "mechanism_fired":
			id = stringMember(t, i, line, "mechanism")
			moment = stringMember(t, i, line, "hook")
			action = stringMember(t, i, line, "action")
		case "reaction_fired":
			id = stringMember(t, i, line, "reaction")
			moment = stringMember(t, i, line, "moment")
			action = stringMember(t, i, line, "action")
			detail = stringMember(t, i, line, "detail")
		default:
			continue
		}
		text := fmt.Sprintf("turn=%d depth=%d reaction=%s moment=%s action=%s detail=%s",
			envelopeNumber(t, i, line, "turn"), envelopeNumber(t, i, line, "depth"),
			id, moment, action, detail)
		if !identityLineGrammar.MatchString(text) {
			t.Fatalf("line %d projects to %q, which is not a projection line; a member the "+
				"stream stopped carrying must fail here rather than be recorded", i+1, text)
		}
		projected = append(projected, text)
	}
	return projected
}

// guardMoments maps each Floor guard's config key to the Moment it fires at. A `floor_guard` line
// names the guard and what it did but not WHERE, because a guard only ever runs at one seam; the
// Reaction lines that replace it carry the Moment themselves, so this table is what lets a golden
// recorded from the old kind be read against the new one.
var guardMoments = map[string]string{
	"tool-call-salvage":       "post-response",
	"tool-loop-breaker":       "post-response",
	"tool-call-repair":        "post-response",
	"empty-response-recovery": "post-response",
	"tool-use-enforcer":       "post-response",
	"read-cache":              "pre-tool-exec",
	"tool-result-cap":         "pre-request",
}

// stringMember reads one `data` member as the string the protocol promises it is. A member that is
// absent or of another type fails the test: the projection would otherwise record its absence.
func stringMember(t *testing.T, i int, line map[string]any, member string) string {
	t.Helper()

	data, ok := line["data"].(map[string]any)
	if !ok {
		t.Fatalf("line %d carried no data object: %v", i+1, line)
	}
	value, ok := data[member]
	if !ok {
		t.Fatalf("line %d (%v) carries no %q member: %v", i+1, line["event"], member, data)
	}
	text, ok := value.(string)
	if !ok {
		t.Fatalf("line %d (%v): %q is %v, want a string", i+1, line["event"], member, value)
	}
	return text
}

// envelopeNumber reads `turn` or `depth` off the envelope. Both are numbers on a firing line —
// a reaction fires inside a Turn, at a depth — so a null here is a protocol break, not a case to
// render around.
func envelopeNumber(t *testing.T, i int, line map[string]any, member string) int {
	t.Helper()

	value, ok := line[member].(float64)
	if !ok {
		t.Fatalf("line %d (%v): %s is %v, want a number — a firing always has one",
			i+1, line["event"], member, line[member])
	}
	return int(value)
}

// identityArmExemption is the one Floor guard of the seven this arm does not record, and the
// exemption is pinned rather than tacit: floor.EnforceToolUse fires only on a COMMITTED text-only
// assistant message with no prior tool use in the request's own history
// (internal/floor/tooluse.go:33-58), and a single-prompt `apogee headless` run commits an assistant
// message only by making a tool call — the very thing the guard requires never happened, and
// headless has no resume flag to build the history another way. Its builtin is covered by a unit
// test instead (plan `2026-09-07 - 01` item 5, TestBuiltinToolUseEnforcer).
const identityArmExemption = "tool-use-enforcer"

// identityRecordedID lifts the reaction id out of one recorded projection line.
var identityRecordedID = regexp.MustCompile(`(?m)^turn=\d+ depth=\d+ reaction=(\S+) `)

// TestE2EReactionIdentityCoversTheFloorGuards holds the arm's COVERAGE, which no single case can:
// between them the goldens must record six of the seven Floor guards, and the seventh must be the
// one named exemption above.
//
// It reads the recorded files rather than re-driving the runs, so it states what is frozen on disk —
// and it fails in BOTH directions. A guard that stops appearing is a fixture that quietly stopped
// firing it, which is exactly the regression this arm exists to catch; a guard that starts appearing
// where the exemption says it cannot is a fixture that outgrew the exemption, and the fix is to
// delete the exemption here rather than to leave a stale comment claiming the arm cannot reach it.
func TestE2EReactionIdentityCoversTheFloorGuards(t *testing.T) {
	recorded := make(map[string]bool)
	for _, tc := range reactionIdentityCases {
		body, err := os.ReadFile(identityGolden(tc.name))
		if err != nil {
			t.Fatalf("read the %s golden: %v", tc.name, err)
		}
		for _, m := range identityRecordedID.FindAllStringSubmatch(string(body), -1) {
			recorded[m[1]] = true
		}
	}

	// guardMoments is the projector's own table of the seven guards (internal/agent/floorguards.go),
	// so a guard added there and mapped here is one this assertion immediately asks for a case for.
	for guard := range guardMoments {
		switch {
		case guard != identityArmExemption && !recorded[guard]:
			t.Errorf("no identity golden records the %q guard; every Floor guard but %q has a case",
				guard, identityArmExemption)
		case guard == identityArmExemption && recorded[guard]:
			t.Errorf("an identity golden records %q, which the pinned exemption says a "+
				"single-prompt headless run cannot reach; the exemption is now stale — drop it "+
				"rather than the case", guard)
		}
	}
}
