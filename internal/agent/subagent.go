package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/airiclenz/apogee/internal/console"
	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/floor"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/sanitize"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/title"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/undo"
)

// ----------------------------------------------------------------------------
// Sub-agent orchestrator (ADR 0013, D2 — privileges ≤ parent, atomic within the Turn)
// ----------------------------------------------------------------------------
//
// A sub-agent IS the embeddable Agent (ADR 0001), one nesting level down. The orchestrator
// here constructs a nested Agent that inherits the parent's privileges VERBATIM OR STRICTER
// (ADR 0005): the same Mode, Approver, Confiner, and confine-to-workspace flag; a fresh
// guardrail bundle that ISOLATES live state but SHARES the dangerous-action floor read-only
// (Guards.ForSubAgent); and a tool set that is a SUBSET of the parent's, never an expansion.
// Its events re-emit into the parent's EventSink at Depth = parent+1 (the nested Agent stamps
// its own depth — base()), so the TUI and bench observe one nested stream.
//
// The sub-agent runs ATOMICALLY WITHIN the parent Turn (D2): the parent is mid-tool-dispatch
// while the nested loop runs to completion, so there is no quiescent boundary inside it. A
// cancel propagates to the nested loop's next boundary and unwinds the whole call (the parent
// rolls its Turn back from the pre-sub_agent boundary); no partial sub-agent result is
// surfaced and no snapshot lands mid-sub-agent. Nested STEPPING (suspend/resume a sub-agent at
// its own boundary) is deliberately out of scope for v1 — the driver below runs the nested
// Agent to its Exchange boundary in one shot, behind a seam a later snapshot-schema-additive
// change can swap for a suspendable driver.

// defaultMaxSubAgentDepth is what a Config.Delegation.MaxDepth of 0 reads as: the top-level agent
// (depth 0) delegates, and its delegates do not. It is the engine's own floor rather than the
// host's — the `delegate-max-depth` key defaults to the same 1 and refuses 0, so a zero only ever
// reaches here from an embedder's untouched Config, and that embedder must still get one level of
// delegation rather than none (ADR 0013 decision 4, superseded 2026-09-15).
const defaultMaxSubAgentDepth = 1

// maxDepth is the recursion bound this Agent runs under, so a model cannot spawn an unbounded
// tower of sub-agents (each level costs a full nested loop). The top-level agent is depth 0; an
// agent at depth maxDepth is the deepest: there the sub_agent tool is withheld from the nested tool
// set AND the recursion point refuses defensively, so the bound holds even if the menu is
// bypassed. It reads Config.Delegation.MaxDepth (the `delegate-max-depth` key), and reads its zero
// value as defaultMaxSubAgentDepth rather than as "no delegation".
func (a *Agent) maxDepth() int {
	if a.cfg.Delegation.MaxDepth > 0 {
		return a.cfg.Delegation.MaxDepth
	}
	return defaultMaxSubAgentDepth
}

// depthLimitReason is the refusal both bound-keepers hand the model — the recursion point and the
// resolver's defensive branch — spelled once so the two cannot drift.
func depthLimitReason(maxDepth int) string {
	return fmt.Sprintf("sub-agent depth limit reached (max %d): cannot spawn a deeper sub-agent", maxDepth)
}

// stepCapClampNoteFormat is the line a delegation's result carries when its spawning call asked for
// a `max_steps` ABOVE the configured cap: the request is applied AS the cap (the model may make a
// delegation cheaper, never longer than the host allows), and this says so, because a clamp the
// parent never hears about is a knob it goes on turning. The verbs are `requested` / `applied`
// (what the model asked, what it got); the middle clause names the cap so the parent learns the
// number to stop exceeding. Appended in the SeatFallbackNote slot — a body note, never the head —
// so stepCapResultFormat and subAgentFaultPrefix stay the first line every reader anchors on.
// Not written at all against an unbounded cap (0): that request is ignored, as it always was.
const stepCapClampNoteFormat = "[max_steps %d requested; the configured cap is %d — %d applied]"

// stepCapResultFormat is the marker line the PARENT model receives when a delegation ended at its
// step cap (Agent.stepCap): a NON-error result whose first line says the answer that follows is
// partial, so the parent can re-delegate a narrower task instead of treating a half-finished
// investigation as the finding. What follows it is the body cappedResultBody renders: the ENGINE
// SUMMARY — the engine's own fold of the child's conversation at the bound (Agent.foldForParent),
// which the parent receives on every bound — and then the child's CLOSING REPORT, which since
// finishAtStepCap (agent.go) is normally the wrap-up Turn's reply, tool-less bar write_file to a
// spawn-named `output_path` (Agent.outputPath), and falls back to whatever the child last said out
// loud when that Turn produced nothing. The line itself is the same whether that Turn produced a
// report or nothing at all: it promises a partial result, and a summary of unfinished work is
// exactly that. Only a closing text that is NOT a report — tool output or narration, closingShapeOf
// — swaps it for the stepCapNonReportFormat variant below. It is a package constant, pinned by
// test, because the parent model reads it as the contract for what the rest of the result is. %d
// is the cap actually applied.
const stepCapResultFormat = "[delegate stopped at its step cap (%d steps); partial result — engine summary and closing report follow]"

// tokenCapResultFormat and timeCapResultFormat are stepCapResultFormat for the two other bounds a
// delegate runs under (Agent.tokenCap, Agent.timeCap): the same shape — a non-error head promising
// a partial result — with the bound that tripped named in the parenthesis, so the parent learns
// which knob its next delegation is up against. Package constants, pinned by test, because the TUI
// reads the three heads by shape (delegationBoundHead) and the parent model reads them as the
// contract for the rest of the result. %d is the token budget applied; %s is the time limit,
// spelled by boundDurationText.
const (
	tokenCapResultFormat = "[delegate stopped at its token budget (%d tokens); partial result — engine summary and closing report follow]"
	timeCapResultFormat  = "[delegate stopped at its time limit (%s); partial result — engine summary and closing report follow]"
)

// The NON-REPORT variants of the three heads above, written when closingShapeOf judges the capped
// child's closing text to be tool output or narration rather than a report (P2 of plan 2026-09-18 -
// 00; owner call, 2026-09-18): the same `[delegate stopped at its <bound>;` prefix — the TUI's
// delegationBoundHead anchors on it and reads the bound unchanged — but the rest of the line says
// there is NO closing report, names what the last reply reads as instead (the %s slot, one of the
// closingShape spellings) and points the parent at the engine summary as the finding. The text is
// still forwarded whole beneath, under closingNarrationHead, because the parent loses nothing it
// could have read; only the parent's reading of it is corrected. Package constants, pinned by test,
// for the same reason the plain heads are. The first %-verb is the bound as in the plain head.
const (
	stepCapNonReportFormat  = "[delegate stopped at its step cap (%d steps); no closing report — the delegate's last reply reads as %s, not a finding; engine summary follows]"
	tokenCapNonReportFormat = "[delegate stopped at its token budget (%d tokens); no closing report — the delegate's last reply reads as %s, not a finding; engine summary follows]"
	timeCapNonReportFormat  = "[delegate stopped at its time limit (%s); no closing report — the delegate's last reply reads as %s, not a finding; engine summary follows]"
)

// The two sub-heads of a capped result's body, in the order cappedResultBody writes them under the
// head line. engineSummaryHead opens the ENGINE FOLD (Agent.capFold): the summary the engine
// authored from the child's conversation at the bound, which the parent receives on every bound
// whatever the wrap-up Turn then produced. closingReportHead opens the child's own closing text —
// its wrap-up reply, or its last visible text when that Turn produced none, or
// stepCapNoTextMarker when it never spoke. Both are package constants, pinned by test, because the
// parent model reads them as the contract for which part of the body is whose.
const (
	engineSummaryHead = "[engine summary]"
	closingReportHead = "[delegate's closing report]"
	// closingNarrationHead replaces closingReportHead when the closing text is a non-report
	// (closingShapeOf): the text still follows, whole, but labelled for what it is — the parent
	// reads the engine summary above it as the finding and this as the noise the child left off on.
	closingNarrationHead = "[delegate's closing report — read as narration, not a finding]"
)

// engineFoldUnavailableFormat stands in for the engine fold when its summary call faulted
// (Agent.foldForParent): the parent still reads the closing report under a head that says the
// summary is MISSING and why, rather than a body silently missing its first part. %v is the cause.
const engineFoldUnavailableFormat = "[engine summary unavailable — %v]"

// continueLineFormat is the body note a CAPPED or FAULTED delegation's result carries when the
// parent retained the child (P6 of plan 2026-09-18 - 00; faults since ADR 0082): the one line that
// tells the parent model the handle it can spell back — `sub_agent` with `continue` naming the
// delegation — instead of re-spawning the work from nothing. It rides the note slot
// delegationResult fills after the missing-output (or draft-output), seat and
// clamp notes and before the user-steered trailer, so the head line stays the first line every
// reader anchors on. Written only when the child ended its run wearing a name, because an unnamed
// delegation is not retained (retainedDelegates.retain) and there would be nothing to continue. %q
// is the post-join display name, exactly as the retention keys it.
const continueLineFormat = "[to continue this delegate: sub_agent with continue: %q]"

// The two heads of a continued child's opening task (continuationTask): the retained task as the
// spawning call spelled it, then the engine fold of the capped or faulted attempt under
// previousAttemptHead, then what the parent now asks for under continuationInstructionsHead. They
// are package constants because the child reads them as the contract for which part of its task is
// whose — the work, what an earlier run of it found, and what to do next.
const (
	previousAttemptHead          = "[previous attempt — engine summary]"
	continuationInstructionsHead = "[continuation instructions]"
)

// unknownContinueFormat is the error result a `continue` naming NO retained delegation is refused
// with: the name as the call spelled it, then the names that ARE retained — the only correction the
// model can act on — or unknownContinueNone when nothing is. %q is the asked name, %s the list.
const (
	unknownContinueFormat = "[no delegate named %q to continue — retained: %s]"
	unknownContinueNone   = "none"
)

// continuationTask composes the opening task of a child continued from prior: the retained task,
// the fold the capped or faulted attempt left (the engineFoldUnavailableFormat marker when that
// fold could not be made — never an empty body under the head), and the instructions the
// continuing call carries in `task`.
func continuationTask(prior retainedDelegate, instructions string) string {
	return prior.task + "\n\n" + previousAttemptHead + "\n" + prior.fold + "\n\n" + continuationInstructionsHead + "\n" + instructions
}

// unknownContinueResult renders the refusal for a `continue` naming no retained delegation, listing
// the names retained in sorted order (retainedDelegates.names).
func unknownContinueResult(asked string, retained []string) string {
	list := unknownContinueNone
	if len(retained) > 0 {
		list = strings.Join(retained, ", ")
	}
	return fmt.Sprintf(unknownContinueFormat, asked, list)
}

// capResultHead is the marker line a capped delegation's result opens with, for the bound capHit
// names — the receiver is the CHILD, as in delegationResult. shape is what closingShapeOf judged
// the child's closing text to be: the plain head for a report (or for no text at all), the
// non-report variant naming the shape for anything else. It is the ONE site both heads are written.
func (a *Agent) capResultHead(shape closingShape) string {
	reportFormat, nonReportFormat, bound := a.capHeadFormats()
	if shape.isNonReport() {
		return fmt.Sprintf(nonReportFormat, bound, string(shape))
	}
	return fmt.Sprintf(reportFormat, bound)
}

// capHeadFormats picks the head formats for the bound capHit names — the plain one and its
// non-report variant — and the value that fills their bound slot.
func (a *Agent) capHeadFormats() (reportFormat, nonReportFormat string, bound any) {
	switch a.capHit {
	case boundTokens:
		return tokenCapResultFormat, tokenCapNonReportFormat, a.tokenCap
	case boundTime:
		return timeCapResultFormat, timeCapNonReportFormat, boundDurationText(a.timeCap)
	}
	return stepCapResultFormat, stepCapNonReportFormat, a.stepCap
}

// subAgentFaultPrefix opens the error result a FAULTED delegation becomes. What follows it is the
// child's own fault sentence (turnLifecycle.lastFault) — the same line the human read at Depth+1 — so the
// parent model reads the cause in the result itself instead of being sent to an error it cannot see.
const subAgentFaultPrefix = "sub-agent faulted before finishing the delegated task: "

// subAgentFaultNoCause is the tail used when the child's Exchange was abandoned without surfacing
// a fault of its own (a recovered extension panic, a pre-request hook that refused): there is no
// cause to name, so the result says so and points at the transcript, which is what this message
// said in full before causes travelled.
const subAgentFaultNoCause = "its exchange was abandoned (see the preceding error), so no result was produced"

// stepCapNoTextMarker stands in for the child's closing report when it produced no visible text —
// a delegate that spent every capped Turn calling tools and never wrote a word, and whose wrap-up
// Turn then faulted or answered with nothing but another tool call. The marker keeps the result
// intelligible: the parent is told, under closingReportHead, that the child itself had nothing to
// show — the engine summary above it is what it reads instead.
const stepCapNoTextMarker = "(no visible text)"

// The three shapes a FINISHED child's closing text is checked against before it is handed to the
// parent as its report (delegationResult's default branch, in this order): a spawn-named
// `output_path` the child never wrote, a reply that is a tool call written out in a vendor
// container, and a reply that is a bare acknowledgement. Each was a session-mining finding
// (2026-09-14, headline 12): a parent that reads "Done." or `<tool_call>…</tool_call>` as the
// finding of a delegation goes on as if the work were done. The first two are ERROR results whose
// first line names the fault and whose body still carries the text, so the parent loses nothing
// it could have read; the third is a non-error marker in stepCapNoTextMarker's shape, because an
// acknowledgement IS no text. They are structural markers on the delegation path, on under Bypass
// like the step-cap marker (ADR 0076, ratified exceptions of plan 2026-09-14 - 03).
const (
	// missingOutputResultFormat heads the error result of a child that ran to completion without
	// writing the file its spawning call named; %s is Agent.outputPath, in the call's spelling.
	missingOutputResultFormat = "sub-agent ended without writing %s; its last text follows:"
	// missingOutputNoteFormat is the same fault on a CAPPED child, where the result keeps its
	// non-error shape and the partial marker first (the TUI reads the head): the note is appended
	// in the SeatFallbackNote slot, a body note like the clamp line. %s is Agent.outputPath.
	missingOutputNoteFormat = "[delegate ended without writing %s]"
	// draftOutputNoteFormat is the converse on a FAULTED child: its spawn-named file IS there and
	// the child wrote it during its run (draftOutputSurvives), so the parent learns from the error
	// result that the work is not all lost — the draft sits at the path the call named, ready to
	// be read or continued from. It rides the same slot the missing-output note does, before the
	// continue line. %s is Agent.outputPath, in the call's spelling.
	draftOutputNoteFormat = "[draft output at %s written before the fault]"
	// markupResultHead heads the error result of a child whose closing text is an unparsed
	// tool call (floor.HasToolCallMarkup) — a call the wire never carried, not a report.
	markupResultHead = "sub-agent reply is unparsed tool-call markup, not a report"
	// noReportMarker stands in for a closing text that was a bare acknowledgement
	// (isAcknowledgement): the parent is told the delegation produced no report, rather than
	// being handed "Done." as one.
	noReportMarker = "[delegate returned no report]"
)

// acknowledgements is the fixed, case-insensitive set of one-word replies isAcknowledgement
// treats as no report (owner call, 2026-09-14). It is a closed list on purpose: a heuristic over
// length or wording would fault real one-line findings, and "Yes." is an answer.
var acknowledgements = []string{"done", "understood", "ok", "okay", "acknowledged", "noted", "sure"}

// acknowledgementPunctuation is the trailing punctuation an acknowledgement may carry and still be
// one: "Done.", "Noted!", "ok," all read as the bare word.
const acknowledgementPunctuation = ".!,;:"

// isAcknowledgement reports whether text is one of acknowledgements, case-insensitively, with any
// surrounding space and trailing punctuation removed — and nothing else: "child done" and
// "Done, I read the file" are reports.
func isAcknowledgement(text string) bool {
	word := strings.ToLower(strings.TrimRight(strings.TrimSpace(text), acknowledgementPunctuation))
	return slices.Contains(acknowledgements, word)
}

// closingShape is what closingShapeOf judged a CAPPED child's closing text to be. shapeReport is
// the ordinary case — a report, forwarded under closingReportHead beneath the plain cap head. The
// other four are the non-report shapes: each value is the phrase the variant cap head's `reads as
// %s` slot carries (stepCapNonReportFormat and siblings), so the head is worded by shape rather
// than by one blanket "not a report".
type closingShape string

const (
	shapeReport         closingShape = ""
	shapeToolCallMarkup closingShape = "tool-call markup"
	shapeFileDump       closingShape = "a file dump"
	shapeGrepDump       closingShape = "a grep dump"
	shapeNarration      closingShape = "narration of its next step"
)

// isNonReport reports whether the shape is one of the four non-report shapes.
func (s closingShape) isNonReport() bool { return s != shapeReport }

// The line shapes closingShapeOf reads, each pinned by a session-mining fixture (2026-09-18,
// session 20260918T143011Z-9788d447; plan 2026-09-18 - 00, item 3).
var (
	// readFileHeaderLine is the header read_file opens every result with —
	// `[File: <path>, <n> lines total, showing lines <a>-<b>]` (internal/tools/read_file.go) — the
	// line a capped delegate pastes when its closing reply is the file it last read rather than a
	// report on it. Read over the first nonReportHeadLines non-blank lines only: a report that
	// merely CITES a path never opens with this header, and one that quotes it deep in its body is
	// still a report.
	readFileHeaderLine = regexp.MustCompile(`^\[File: .+, \d+ lines total`)
	// grepHitLine is a grep hit — `path:line:` — the line shape a pasted search dump is made of.
	// Only a MAJORITY of such lines makes the text a dump: a report cites `path:line` freely, and
	// three hits quoted in a longer report are evidence, not the reply.
	grepHitLine = regexp.MustCompile(`^[\w./-]+:\d+:`)
	// intentAtSentenceStart is a stated next step — "Let me …", "I'll …", "Now I …" — at ANY
	// sentence start of a line, mid-line included: a closing reply that ENDS on one is a delegate
	// narrating what it would do next, not reporting what it did.
	intentAtSentenceStart = regexp.MustCompile(`(?i)(^|[.!?]\s+)(let me|i'll|i will|now i|next i|next, i)\b`)
	// receiptLine is a structured receipt field — `PHASE: `, `STATUS: `, `OUT: `, `SUMMARY: `,
	// `COUNTS: `, `FLAG: ` — the shape a skill's report format prescribes. A trailing intent AFTER a
	// receipt is a report that closes with what remains, so the intent rule yields to it.
	receiptLine = regexp.MustCompile(`^[A-Z][A-Z_-]*: `)
)

// nonReportHeadLines is how many leading non-blank lines readFileHeaderLine is read over: a pasted
// file may follow one line of lead-in ("Now claims 17-20:" was the fixture), not a whole report.
const nonReportHeadLines = 3

// closingShapeOf judges a capped child's closing text: shapeReport for blank text — the wordless
// path is the caller's (stepCapNoTextMarker), never a non-report — and for anything that reads as
// a report; otherwise the first non-report shape that fires, in this order: unparsed tool-call
// markup (floor.HasToolCallMarkup), a read_file header among the first nonReportHeadLines
// non-blank lines, grep hits as the majority of non-blank lines, and a trailing stated intent with
// no receipt line before it. No shape FAULTS the text: the result stays non-error and forwards it
// whole — the closed acknowledgement list (isAcknowledgement) stays the only wording rule that
// withholds one, and a one-line report opening "I'll note …" is demoted to narration, never dropped.
func closingShapeOf(text string) closingShape {
	lines := nonBlankLines(text)
	if len(lines) == 0 {
		return shapeReport
	}
	switch {
	case floor.HasToolCallMarkup(text):
		return shapeToolCallMarkup
	case hasReadFileHeader(lines):
		return shapeFileDump
	case isGrepMajority(lines):
		return shapeGrepDump
	case endsOnIntent(lines):
		return shapeNarration
	}
	return shapeReport
}

// isNonReport reports whether closingShapeOf judges text a non-report; false for blank text.
func isNonReport(text string) bool {
	return closingShapeOf(text).isNonReport()
}

// nonBlankLines splits text into its non-blank lines, each trimmed of surrounding space.
func nonBlankLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// hasReadFileHeader reports whether any of the first nonReportHeadLines lines is a read_file header.
func hasReadFileHeader(lines []string) bool {
	head := lines[:min(len(lines), nonReportHeadLines)]
	return slices.ContainsFunc(head, readFileHeaderLine.MatchString)
}

// isGrepMajority reports whether more than half of lines are grep hits.
func isGrepMajority(lines []string) bool {
	hits := 0
	for _, line := range lines {
		if grepHitLine.MatchString(line) {
			hits++
		}
	}
	return hits*2 > len(lines)
}

// endsOnIntent reports whether the last line carries a stated intent and no receipt line precedes it.
func endsOnIntent(lines []string) bool {
	last := lines[len(lines)-1]
	if !intentAtSentenceStart.MatchString(last) {
		return false
	}
	return !slices.ContainsFunc(lines[:len(lines)-1], receiptLine.MatchString)
}

// A fourth shape a finished child's closing text is checked against, ahead of the three above:
// DEGENERATE narration — one line repeated over and over ("Emit. / write_file. / GO." two thousand
// times was the session-mining case, 2026-09-14, headline 13), the output of a model stuck in a
// loop with no tool to break it. Handed over whole it is thousands of tokens of nothing the parent
// then reads as a report; so it is an error result whose head names the fault and the repeat
// count, and whose body keeps only the first degenerateResultHeadLines lines — enough to see what
// the child was saying, not the whole recital.
const (
	// degenerateRepeatThreshold is how many times the most frequent non-blank line must occur for
	// the text to be degenerate. Real reports repeat a line — a table rule, a fence — a handful of
	// times; fifty of one line is a loop.
	degenerateRepeatThreshold = 50
	// degenerateResultHeadLines is how many leading lines of a degenerate text the fault carries.
	degenerateResultHeadLines = 20
	// degenerateResultFormat heads the error result of a degenerate closing text; %d is the
	// repeat count of its most frequent line.
	degenerateResultFormat = "sub-agent reply is degenerate (one line repeated %d times)"
)

// The absolute cap on the body of EVERY sub_agent result, report or fault: delegationResult
// applies it after the outcome switch and before the body notes and the steered trailer, so those
// always follow the capped tail intact. It is an absolute size, not a share of the window, because
// a delegation's result is the parent's to read on its next Turn and a 200 KB report is a context
// spent on one call whichever window it lands in; the structural clamp in appendToolResult
// (dispatch.go) runs after it, against the window, and is the floor beneath this ceiling. The
// elision is rendered by apogeectx.ElideMiddle so the parent reads the one "the middle was
// dropped" marker every seam renders, never a second idiom.
const (
	// delegateResultMaxBytes is the cap: a body at or under it is untouched, and an elided body
	// — head, marker and tail together — never exceeds it.
	delegateResultMaxBytes = 64 * 1024
	// delegateResultHeadBytes is what an elided body keeps of its start — where a report states
	// its finding; the rest of the cap, less the marker, keeps its end — where it closes.
	delegateResultHeadBytes = 48 * 1024
)

// degenerateRepeat reports whether text is degenerate narration — its most frequent non-blank line
// (compared trimmed of surrounding space) occurs at least degenerateRepeatThreshold times — and
// that line's count.
func degenerateRepeat(text string) (int, bool) {
	counts := map[string]int{}
	most := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		counts[line]++
		most = max(most, counts[line])
	}
	return most, most >= degenerateRepeatThreshold
}

// headLines returns the first n lines of text, joined as they were.
func headLines(text string, n int) string {
	lines := strings.SplitN(text, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// capDelegateResult applies delegateResultMaxBytes to a result body: a body within the cap is
// returned byte for byte; a larger one keeps its first delegateResultHeadBytes and as much of its
// tail as the cap leaves, around the shared elision marker.
func capDelegateResult(body string) string {
	if len(body) <= delegateResultMaxBytes {
		return body
	}
	return apogeectx.ElideMiddle(body, delegateResultMaxBytes, delegateResultHeadBytes)
}

// outputMissing reports whether the delegation was spawned to write a file (Agent.outputPath)
// that is absent once its run has ended. It answers false whenever the child was never in a
// position to write it — no `output_path` that resolved (outputTarget, which resolveOutputPath
// sets only where the child's registry holds write_file), or a Plan-mode child whose ladder
// refuses every workspace write — because a file the ladder forbade is not a fault of the child's;
// and false when the path exists in any form. It reads those conditions itself rather than through
// wrapUpWriter, which since the one-write wrap-up answers true without a target: a writer on the
// menu is not a file the child was asked for. Only a certainly-absent file (fs.ErrNotExist)
// counts: a stat that fails some other way is not evidence the child skipped its write.
func (a *Agent) outputMissing() bool {
	if a.outputTarget == "" || a.Mode() == domain.ModePlan {
		return false
	}
	_, err := os.Stat(a.outputTarget)
	return errors.Is(err, fs.ErrNotExist)
}

// outputBaseline is the state of a delegate's spawn-named output file (Agent.outputTarget) just
// before its Run (Agent.outputBefore): whether something was there, and if so when it was last
// written. It is what draftOutputSurvives compares the file's state after the run against, so
// that only a file the child itself wrote counts as its draft.
type outputBaseline struct {
	present bool
	modTime time.Time
}

// recordOutputBaseline stats the spawn-named output target before the child's Run and keeps what
// it finds (outputBefore). A stat that fails in any way reads as "nothing there": a file that
// then turns up after the run can only have been written during it. It records nothing for a
// delegation whose spawn named no path that resolved.
func (a *Agent) recordOutputBaseline() {
	a.outputBefore = outputBaseline{}
	if a.outputTarget == "" {
		return
	}
	info, err := os.Stat(a.outputTarget)
	if err != nil {
		return
	}
	a.outputBefore = outputBaseline{present: true, modTime: info.ModTime()}
}

// draftOutputSurvives reports whether the delegation's spawn-named output file holds a draft the
// child wrote during its run — the fact a FAULTED delegation's result carries as its draft note
// (draftOutputNoteFormat). It answers true only for a regular file at outputTarget that was
// absent at the baseline runSubAgent recorded (recordOutputBaseline) or whose modification
// time has advanced since: a file that was there before the spawn and was never rewritten is not
// a draft of this child's, and a stat that fails is not evidence of one.
func (a *Agent) draftOutputSurvives() bool {
	if a.outputTarget == "" {
		return false
	}
	info, err := os.Stat(a.outputTarget)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return !a.outputBefore.present || info.ModTime().After(a.outputBefore.modTime)
}

// wrapUpMarker and wrapUpDirectiveFormat are the one-request directive a delegate stopped at its
// step cap is handed for its closing report (turnLifecycle.wrapUp, loop.go): the request that
// carries it carries no tools at all — bar the one write_file wrapUpWriter keeps, which
// wrapUpOutputClauseFormat and wrapUpWriteClause announce below — so the directive is the only
// thing that tells the child WHY its menu vanished and what to do with the reply it has left. It states the cause, the
// prohibition and the ask — report to the agent that delegated the task, unfinished work included
// — because a model that is merely given no tools narrates its next tool call instead of a result,
// which is exactly the scavenged text this replaces. The tail spells out the two ways that reply
// goes wrong — continuing the task, and writing what a tool would have printed — because a capped
// child's closing text has been seen doing both (a fabricated tool dump the parent then read as a
// finding), and names how the reply is read: as the report, nothing else.
//
// It rides the closing tool result of the capping Turn as an engine note under wrapUpNoteTopic
// (buildRequest → Request.NoteOnTail), where the model reads next; the system prompt carries it
// only on the fallback — a tail that is not a tool result — through AppendToSystem, whose
// idempotency contract needs the marker to be a phrase INSIDE the directive (domain/hooks.go).
// %d is the cap actually applied (Agent.stepCap) — the same number the human reads in
// stepCapErrFormat and the parent reads in stepCapResultFormat, so all three tell one story.
// Package constants, pinned by test, because the child reads them as the contract for its last
// reply.
//
// The token and time bounds hand the child the same directive with their own opening clause
// (wrapUpTokenDirectiveFormat, wrapUpTimeDirectiveFormat): the cause differs, the prohibition and
// the ask do not, so the three share wrapUpDirectiveTail and wrapUpDirective picks by capHit.
//
// wrapUpOutputClauseFormat and wrapUpWriteClause are the one clause appended AFTER the directive —
// never folded into it, so the three formats above keep their arity and their pinned text — when,
// and only when, the wrap-up keeps write_file (wrapUpWriter), so the exception is announced exactly
// where the withdrawal is. With a spawn-named `output_path` the clause names the one path the
// child may still write, in the spelling its spawning call used (Agent.outputPath; %s is that
// path); without one it offers the single write for the child's report or partial output, aimed
// wherever the Mode admits.
const (
	wrapUpMarker = "no further tool calls are possible"

	// wrapUpNoteTopic is the engine-note topic the directive is fenced under on the closing tool
	// result — `[engine — wrap-up]` … `[end engine — wrap-up]` — and the key NoteOnTail's
	// idempotency runs on.
	wrapUpNoteTopic = "wrap-up"

	wrapUpOutputClauseFormat = "\n\nYou may still call write_file once, for %s only."

	wrapUpWriteClause = "\n\nYou may still call write_file once, to save your report or partial output before you reply."

	wrapUpDirectiveTail = "no further tool calls are possible: the tools have been withdrawn for this final reply." +
		"\n\nReport back to the agent that delegated this task now: what you found, what you " +
		"concluded, and what remains unfinished. This is your only remaining reply — anything you " +
		"do not write here is lost. Do not continue the task. Do not write what a tool would have " +
		"printed — your reply is read as your report, nothing else."

	wrapUpDirectiveFormat = "You have reached the step limit for this delegation (%d steps) and " +
		wrapUpDirectiveTail

	wrapUpTokenDirectiveFormat = "You have reached the token budget for this delegation (%d tokens) and " +
		wrapUpDirectiveTail

	wrapUpTimeDirectiveFormat = "You have reached the time limit for this delegation (%s) and " +
		wrapUpDirectiveTail
)

// wrapUpDirective renders the closing-report directive for the bound capHit names, with that
// bound's applied value in the clause — the same number the human read in the ErrorEvent and the
// parent reads in the result head, so all three tell one story. The write clause rides it only
// when the wrap-up menu actually carries write_file (wrapUpWriter), so the child is never told it
// may write a file the menu then withholds — the output-path clause where the spawn named a path,
// the plain one-write clause otherwise.
func (a *Agent) wrapUpDirective() string {
	var directive string
	switch a.capHit {
	case boundTokens:
		directive = fmt.Sprintf(wrapUpTokenDirectiveFormat, a.tokenCap)
	case boundTime:
		directive = fmt.Sprintf(wrapUpTimeDirectiveFormat, boundDurationText(a.timeCap))
	default:
		directive = fmt.Sprintf(wrapUpDirectiveFormat, a.stepCap)
	}
	if _, ok := a.wrapUpWriter(); !ok {
		return directive
	}
	if a.outputTarget == "" {
		return directive + wrapUpWriteClause
	}
	return directive + fmt.Sprintf(wrapUpOutputClauseFormat, a.outputPath)
}

// wrapUpOutputRefusalFormat is the error result a wrap-up write_file call gets when its target is
// not the delegation's `output_path`: the one write the Turn was offered is the one write it may
// make, and the refusal names that path in the spelling the directive used. %s is Agent.outputPath.
const wrapUpOutputRefusalFormat = "wrap-up: only %s may be written"

// wrapUpWriter is the ONE tool the step-cap wrap-up Turn keeps, and whether it keeps it:
// write_file, so a capped child can still land its report, its partial output, or — for a
// delegation spawned with an `output_path` (Agent.outputPath) — the file it was asked for, and
// the parent is not handed a fabricated tool dump in place of work that only needed saving. It
// answers false — and the wrap-up stays tool-less and clause-less, never announced-then-refused —
// unless both hold: this Agent's registry holds write_file (a `tools:` roster may have dropped
// it), and its live Mode admits a workspace write at all — a Plan-mode child inherits Plan, whose
// ladder row refuses the call, so offering it there would be a promise the ladder breaks. An
// `output_path` is no longer a condition (2026-09-18, superseding the 2026-09-14 "only when
// named"): it narrows WHERE the one write may go, never whether there is one. It is read by the
// three wrap-up seams (toolMenu, wrapUpDirective, step's call filter) and by resolve's wrap-up row
// through resolutionInput, so the offer and the permission cannot drift apart.
func (a *Agent) wrapUpWriter() (domain.Tool, bool) {
	if a.Mode() == domain.ModePlan {
		return nil, false
	}
	return a.lookupTool(tools.WriteFileToolName)
}

// wrapUpCalls is the wrap-up Turn's call filter (step, loop.go): with write_file on the menu
// (wrapUpWriter) it keeps the write_file calls the reply may make and drops everything else; with
// the menu withdrawn wholesale it keeps nothing. The clause promised write_file "once"
// (wrapUpWriteClause, wrapUpOutputClauseFormat), so a second write the clause already withdrew is
// asking for something the request said it cannot have: it is dropped undispatched like any other
// withdrawn call, and the first write is the one that lands. Without an output path that is the
// FIRST write_file call, wherever it is aimed — the ladder decides whether it runs. With one, it
// is the FIRST call aimed at the output path, judged by classifyWriteTarget's resolved target
// against Agent.outputTarget (two readings of one resolver), plus every one aimed elsewhere: a
// kept elsewhere-write is not permitted — the wrap-up row of resolve refuses one aimed anywhere
// but the output path — it is merely dispatched, so the refusal reaches the transcript instead of
// vanishing with the dropped calls.
func (a *Agent) wrapUpCalls(calls []domain.ToolCall) []domain.ToolCall {
	writer, ok := a.wrapUpWriter()
	if !ok {
		return nil
	}
	var kept []domain.ToolCall
	writeKept := false
	for _, call := range calls {
		if call.Tool != tools.WriteFileToolName {
			continue
		}
		if a.outputTarget == "" || a.classifyWriteTarget(writer, call).real == a.outputTarget {
			if writeKept {
				continue
			}
			writeKept = true
		}
		kept = append(kept, call)
	}
	return kept
}

// resolveOutputPath resolves a spawning call's `output_path` for the child through the SAME fence
// write_file's own target is classified by (tools.WorkspaceWriteTarget over the child's write_file
// tool: workspace-joined, symlinks followed), so the wrap-up's comparison is between two readings
// of one resolver. It sets nothing when the child holds no write_file — then there is no writer to
// keep and no resolver to read — or when the path is not inspectable; either leaves the wrap-up's
// one write unnarrowed exactly as a spawn that named no path.
func (a *Agent) resolveOutputPath(path string) {
	if path == "" {
		return
	}
	writer, ok := a.lookupTool(tools.WriteFileToolName)
	if !ok {
		return
	}
	args, err := json.Marshal(struct {
		Path string `json:"path"`
	}{Path: path})
	if err != nil {
		return
	}
	target, ok := tools.WorkspaceWriteTarget(writer, domain.ToolCall{Tool: writer.Name(), Arguments: args})
	if !ok {
		return
	}
	a.outputPath = path
	a.outputTarget = target
}

// userSteeredTrailerSingular and userSteeredTrailerPluralFormat are the two renderings of the
// PARENT NOTICE a delegation's result carries when the human addressed the child while it ran
// (ADR 0063 D3). The parent model is the one reader that never saw those messages — they landed in
// the CHILD's conversation — so a result it reads as "the task I delegated came back" would
// otherwise hide that the task moved under it. The notice states the COUNT and nothing else: what
// was said is the child's to fold into its own answer, and quoting it here would let a human steer
// the parent through a child they only addressed. %d is the number of messages that LANDED.
//
// userSteeredTrailerHead and userSteeredTrailerPluralTail are the fixed ends both renderings share
// — what splitUserSteeredTrailer recognises the trailer by — and userSteeredTrailerSeparator is
// the blank line that sets it apart from the body.
const (
	userSteeredTrailerHead         = "(the user sent "
	userSteeredTrailerPluralTail   = " messages to this sub-agent while it ran)"
	userSteeredTrailerSingular     = userSteeredTrailerHead + "1 message to this sub-agent while it ran)"
	userSteeredTrailerPluralFormat = userSteeredTrailerHead + "%d" + userSteeredTrailerPluralTail
	userSteeredTrailerSeparator    = "\n\n"
)

// userSteeredTrailer renders the parent notice for steered landed messages — singular for exactly
// one, plural for any other count. Callers append it only when steered > 0.
func userSteeredTrailer(steered int) string {
	if steered == 1 {
		return userSteeredTrailerSingular
	}
	return fmt.Sprintf(userSteeredTrailerPluralFormat, steered)
}

// splitUserSteeredTrailer takes a committed delegation result apart into its body and the
// user-steered trailer delegationResult appended to it — the separator included, so body+trailer
// is the content byte for byte. A result with no trailer comes back whole with an empty trailer.
//
// It exists for the ONE reader that has to put a line under the body after delegationResult has
// already closed it: the parent committing a fan-out group (dispatch.go withBodyNote), which knows
// nothing of the child's steered count and can only read the trailer off the result. The
// recognition is exact — the separator, the head, a count, and the plural tail or the singular
// rendering as a single line — so a child answer that merely mentions the words is not mistaken
// for the notice.
func splitUserSteeredTrailer(content string) (body, trailer string) {
	i := strings.LastIndex(content, userSteeredTrailerSeparator+userSteeredTrailerHead)
	if i < 0 {
		return content, ""
	}
	line := content[i+len(userSteeredTrailerSeparator):]
	if line != userSteeredTrailerSingular && !isPluralUserSteeredTrailer(line) {
		return content, ""
	}
	return content[:i], content[i:]
}

// isPluralUserSteeredTrailer reports whether line is userSteeredTrailerPluralFormat rendered with
// some count — head, digits, tail, and nothing else.
func isPluralUserSteeredTrailer(line string) bool {
	count, ok := strings.CutPrefix(line, userSteeredTrailerHead)
	if !ok {
		return false
	}
	count, ok = strings.CutSuffix(count, userSteeredTrailerPluralTail)
	if !ok || count == "" {
		return false
	}
	for _, r := range count {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// withBodyNote appends note as the last line of a delegation result's BODY: immediately above the
// user-steered trailer when the result carries one — ADR 0063 D3 keeps that trailer the result's
// final line on every outcome — and as the new last line otherwise. It is the parent-side twin of
// the note slot delegationResult fills on the child's side (SeatFallbackNote and its neighbours),
// for a note only the parent can know.
func withBodyNote(content, note string) string {
	body, trailer := splitUserSteeredTrailer(content)
	return body + "\n" + note + trailer
}

// isSubAgentCall reports whether call targets the sub_agent recursion point — the signal the
// dispatch pipeline drives a nested Agent for the call rather than executing a leaf tool.
func isSubAgentCall(call domain.ToolCall) bool {
	return call.Tool == tools.SubAgentToolName
}

// delegationName normalises the OPTIONAL name a sub_agent call may carry into the one form
// every display can paint on a single line: the first line only, trimmed of surrounding
// whitespace. A name that is empty after normalisation is ABSENT — the callers fall back to the
// delegated task's first line, exactly as they did before names existed. It runs once here at
// the recursion point rather than at each display, so a model that pads or newlines its name
// cannot break a status line or a prompt body downstream.
func delegationName(raw string) string {
	return sanitize.FirstLine(raw)
}

// delegationSeat is the Delegation seat ONE spawn is built for (ADR 0069) — the two places a
// delegation may run, plus the absent ask that leaves the choice where it has always been. It is
// unexported and lives here rather than on the wire because the seat is a construction decision:
// the model names one of two strings (tools.RunOnSession / tools.RunOnSubAgentsServer) and the
// engine resolves that, once, into which Upstream the child is built on.
type delegationSeat int

const (
	// seatConfigured is the absent `run_on`: the latch alone decides, exactly as every delegation
	// did before seat choice existed (ADR 0069 decision 2). It is the zero so that every caller
	// that never heard of seats — newChildAgent's wrapper and its test callers — asks for it.
	seatConfigured delegationSeat = iota
	// seatSession is an explicit ask for the session server: the child is built on the parent's
	// Upstream with the parent's posture whatever is latched (ADR 0069 decision 8).
	seatSession
	// seatSubAgentsServer is an explicit ask for the Sub-agent server: routed when a target is
	// latched, and otherwise the session server with the note that says so (decision 9).
	seatSubAgentsServer
)

// parseDelegationSeat resolves the OPTIONAL `run_on` a sub_agent call may carry into the seat the
// spawn is built for. The empty string is the absent ask and the only value the plain tool variant
// can produce, so every call made against a schema without `run_on` — and every call a
// Reaction synthesises — resolves to seatConfigured.
//
// Anything else is refused rather than folded into the default: the two spellings are published in
// the schema's own enum, so a third value is a model that read the menu wrong, and answering it
// with a silent default would leave the parent believing a routing decision it never got. The error
// names both accepted values because the result is the only place the model is told.
func parseDelegationSeat(raw string) (delegationSeat, error) {
	switch raw {
	case "":
		return seatConfigured, nil
	case tools.RunOnSession:
		return seatSession, nil
	case tools.RunOnSubAgentsServer:
		return seatSubAgentsServer, nil
	default:
		return seatConfigured, fmt.Errorf("invalid run_on %q: want %q or %q",
			raw, tools.RunOnSession, tools.RunOnSubAgentsServer)
	}
}

// SeatFallbackNote is the ONE line the result of a fallen-back delegation carries (ADR 0069
// decision 9): the call asked for the Sub-agent server, no usable target was latched, and the work
// ran on the session server instead. It is for the PARENT MODEL — the human already read the
// routing-state notice the host emits once per beat — so it says what happened to the decision the
// model made and nothing about why the server is down, which is not the parent's to act on.
//
// An EXPORTED constant, pinned by test, because it is a contract line: the parent reads it as the
// answer to "did my run_on take effect", and it is appended to the result BODY (delegationResult),
// never prefixed, so the child's own first line stays the head every reader meets first. It is
// exported — and re-exported as apogee.SeatFallbackNote — so the Driver-side tests that assert the
// model reads this sentence read it from here rather than re-typing it.
const SeatFallbackNote = "note: ran on the session server — the sub-agents server was unavailable"

// runSubAgent is the recursion point: it parses the delegated task, constructs a nested Agent
// bounded by this Agent's privileges (ADR 0005/0013), drives it to its Exchange boundary, and
// returns the sub-agent's final message as this call's tool result. A cancellation propagates
// out as dispatchCancelled so the parent rolls the whole Turn back (atomic-within-the-Turn);
// a FAULTED child Exchange — abandoned rather than completed, which closes on the same
// StatusExchangeComplete a real completion does — returns an ERROR result naming the fault
// instead of the child's last assistant text (StepResult.Faulted). A STEP-CAPPED child
// (StepResult.StepCapped) is the third outcome and the only one that is neither success nor
// failure: the engine stopped it mid-task, so the parent gets a NON-error result marked partial.
//
// The nested loop's events already reached the parent's EventSink at Depth+1 as they ran; the
// returned ToolResult is what the PARENT model sees on its next Turn (the delegated work
// summarised back into the parent conversation).
//
// This frame is the ONE recover boundary of every delegation (ADR 0039 decision 4: each child
// keeps panic recovery at its own boundary). Every delegation enters here through runDelegation —
// on the dispatching goroutine at width 1, on a pool worker above it — so a panic raised anywhere
// in the child's life becomes this call's error result at every width, and the parent Step
// carries on with it exactly as it carries on with a recovered leaf-tool panic (executeTool). The defer is registered FIRST so it
// runs LAST: after the reaping defer below has unregistered the child, closed its mailbox and
// released its resources, and still around it, so a panic raised inside that teardown is caught
// here too. The ErrorEvent is stamped with this Agent's current Turn — the same value dispatchTools
// carries as `turn` — read live because the signature stays the shared `(ctx, call)` one.
//
// That same defer is the ONE site the delegate ledger is written from for a call that REACHES this
// frame (children.go, apogee-clb): it runs last of all, after the recover has settled the named
// results, so every way out of this frame — a refusal before any child exists, a cancel, a fault, a
// cap, a completion, a recovered panic — is classified from the ToolResult and dispatchOutcome
// actually returned (classifyDelegation) and lands as one row. The one call that never reaches it
// is a delegation refused past the reply's fan-out ceiling, and dispatchGroup books that row itself
// (recordCeilingRefusal, dispatch.go) — the ceiling's own second site. The spawn index is taken FIRST, under the ledger's
// lock — the one a pooled group reserved for this call in call order (dispatchGroup), else the next
// — because a pool fan-out runs several of these frames at once and neither its dequeue nor its
// completion order is the model's call order; the row records the child's RESOLVED output target,
// never the unresolved argument.
func (a *Agent) runSubAgent(ctx context.Context, call domain.ToolCall) (result domain.ToolResult, outcome dispatchOutcome) {
	var (
		spawnIndex   = a.delegations.open(call.ID)
		ledgerName   string
		ledgerTarget string
		ran          bool
		res          domain.StepResult
	)
	defer func() {
		if r := recover(); r != nil {
			a.cfg.Events.Emit(domain.ErrorEvent{
				EventBase: a.base(a.turns.index),
				Source:    call.Tool,
				Err:       fmt.Sprintf("panic: %v", r),
			})
			result = errorToolResult(call.ID, fmt.Sprintf("tool %q panicked", call.Tool))
			outcome = dispatchDone
		}
		ended, cause := classifyDelegation(result, outcome, ran, res)
		a.delegations.record(delegationRecord{
			spawnIndex: spawnIndex,
			callID:     call.ID,
			name:       delegationLabel(ledgerName, call),
			outcome:    ended,
			cause:      cause,
			outputPath: ledgerTarget,
		})
	}()
	if a.depth >= a.maxDepth() {
		// Defensive floor: the tool is withheld from the menu at the bound, but refuse here
		// too so the bound holds even if a model emits the call anyway.
		return errorToolResult(call.ID, depthLimitReason(a.maxDepth())), dispatchDone
	}

	var args tools.SubAgentArgs
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return errorToolResult(call.ID, "invalid sub_agent arguments: "+err.Error()), dispatchDone
	}
	if args.Task == "" {
		return errorToolResult(call.ID, "sub_agent requires a non-empty task"), dispatchDone
	}

	// A CONTINUATION (P6 of plan 2026-09-18 - 00): the call names a delegation this Agent retained
	// after the engine stopped it at a bound, and the child built below starts from that run's
	// engine fold instead of from nothing. The entry is CONSUMED — a fold is continued once, and a
	// continued child that caps again is retained anew under the same name — and the call's own
	// `task` becomes the continuation instructions under the retained task and fold
	// (continuationTask). Name, roster and output path are inherited from the entry wherever the
	// call leaves them unset, so `continue` with a task is a complete call; and an unknown name is
	// refused with the names that are retained, resolved BEFORE the seat and roster for the reason
	// those are resolved before the child: a refusal costs no child. A refusal also costs no FOLD
	// (apogee-if9): the entry is taken here, ahead of the seat and roster refusals below — an
	// inherited roster can be refused when the parent's menu changed since the cap — and every
	// refusal between this take and the child's Submit gives it back (giveBack), so a corrected
	// retry still finds the delegate it names; once Submit succeeds the child owns the fold, and a
	// second cap retains it anew. The task retained if this child caps again stays the ORIGINAL,
	// so a second continuation composes over one fold, never a fold of a fold.
	task, retainTask := args.Task, args.Task
	var inheritedName string
	giveBack := func() {}
	if args.Continue != "" {
		prior, ok := a.retained.take(args.Continue)
		if !ok {
			return errorToolResult(call.ID, unknownContinueResult(args.Continue, a.retained.names())), dispatchDone
		}
		giveBack = func() { a.retained.retain(prior) }
		task, retainTask = continuationTask(prior, args.Task), prior.task
		if delegationName(args.Name) == "" {
			args.Name, inheritedName = prior.name, prior.name
		}
		if !args.Tools.IsSet() {
			args.Tools = prior.tools
		}
		if args.OutputPath == "" {
			args.OutputPath = prior.outputPath
		}
	}

	// The seat this one delegation runs on (ADR 0069), resolved BEFORE anything is built so an
	// unparseable ask costs no child: it is refused with a result naming the two spellings, which
	// is the only place the model can be told it read the menu wrong.
	//
	// `run_on` is only ever READ where this Agent's own sub_agent tool published it. A child's tool
	// is the plain variant (withoutSeatChoice), and so is every tool built under
	// `sub-agents-choice: fixed`, so a seat named against either is a value the model was never
	// offered: it is IGNORED rather than honoured or refused, which is the identity rule — below the
	// first hop a delegation keeps the seat it landed on (ADR 0069 decision 3) — and which keeps a
	// hallucinated argument from being an error the model cannot act on.
	seat := seatConfigured
	if publishesSeatChoice(a.tools) {
		asked, err := parseDelegationSeat(args.RunOn)
		if err != nil {
			giveBack()
			return errorToolResult(call.ID, err.Error()), dispatchDone
		}
		seat = asked
	}

	// The roster this call asks for (ADR 0005's per-task narrowing, published as `tools`), resolved
	// BEFORE the child is built for the same reason the seat is: an unknown name costs no child and
	// is refused with a result naming it, which is the only correction the model can act on.
	narrowed, err := a.requestedChildTools(args.Tools)
	if err != nil {
		giveBack()
		return errorToolResult(call.ID, err.Error()), dispatchDone
	}

	sub, err := a.newChildAgentOn(seat, call.ID, task, delegationName(args.Name))
	if err != nil {
		giveBack()
		return errorToolResult(call.ID, "could not construct sub-agent: "+err.Error()), dispatchDone
	}
	// Applied to the child's own registry rather than threaded through construction: the spawn
	// signatures stay as they are, and a per-spawn field on the PARENT would race across the
	// siblings a fan-out builds at once (ADR 0039). Subset over the set the child was built with is
	// the intersection ADR 0005 promises — it can drop a name, never add one — and it keeps the
	// tool values verbatim, so a plain sub_agent reaches the child unrebuilt exactly as before.
	if narrowed != nil {
		sub.tools = sub.tools.Subset(narrowed...)
	}
	// AFTER the narrowing, because the output path is resolved through the child's own write_file
	// and kept only where the child still holds one: a `tools:` roster that dropped the writer
	// leaves the wrap-up tool-less, as it must (wrapUpWriter), with no path to narrow it to.
	sub.resolveOutputPath(args.OutputPath)
	// The ledger's output column is the RESOLVED target, read by outputMissing's rule: a spawn that
	// named no path, or a Plan-mode child whose ladder refuses every write, was never in a position
	// to write and reads `none` rather than `missing`.
	if sub.outputTarget != "" && sub.Mode() != domain.ModePlan {
		ledgerTarget = sub.outputTarget
	}
	// And the file's state BEFORE the child runs, the reference a faulted child's draft note is
	// read against (draftOutputSurvives): the same pre/post pair the ledger column reads presence
	// from at render time, so a file that predates the spawn is never reported as the child's
	// draft.
	sub.recordOutputBaseline()
	// The call's optional max_steps against the cap the child was seeded with (resolveStepCap —
	// the same rule runDelegation applied to the started phase event). A continuation takes the
	// same road: its fresh child was seeded from the same key and its call's max_steps clamps the
	// same way.
	sub.stepCap, sub.capRequested = resolveStepCap(sub.stepCap, args.MaxSteps)
	// The out-of-band namer's two handles, declared ABOVE the reaping defer so that defer can stop
	// and join the naming goroutine before anything the child owns is torn down (ADR 0068). Both
	// stay zero when no naming starts — every early return below, a named delegation, and a nil
	// Config.Namer — and the defer is a no-op on them.
	var (
		naming     sync.WaitGroup
		stopNaming context.CancelFunc
	)
	// The delegation is the child's whole life, so this scope is the only one that knows when the
	// child's resources stop being needed — nothing else holds the child to close it later. Close
	// reaps the Consoles this delegation opened (ADR 0059 §6), routed or not, and tears down a
	// ROUTED child's own client; an unrouted child borrowed the parent's client, so that one is
	// left running for the parent (ownsUpstream).
	defer func() {
		// The namer goes FIRST and is JOINED, not merely cancelled: it writes the child's display
		// name and emits through this Agent's sink, so letting it outlive the run would let a name
		// land on a closed child and an event arrive after the delegation was reported. Cancelling
		// its context is also what makes a reply that comes back too late a dropped reply rather
		// than a rename nobody can see (ADR 0068 decision 2).
		if stopNaming != nil {
			stopNaming()
		}
		naming.Wait()
		// Unregister and close the mailbox before the child's resources go: after this the child
		// is no longer addressable, and anything a human queued for it that never reached a
		// boundary is reported undelivered rather than left unaccounted for (ADR 0063 D2).
		a.children.unregister(call.ID)
		sub.reportUndelivered(sub.turns.index, sub.mailbox.close())
		_ = sub.Close()
	}()

	if err := sub.Submit(domain.UserInput{Text: task}); err != nil {
		giveBack()
		return errorToolResult(call.ID, "could not start sub-agent: "+err.Error()), dispatchDone
	}
	// The child is addressable for exactly as long as it runs: published under the id the model
	// chose for this call — the same id the child stamps on every Event it emits, so a Driver
	// addresses it by the identity it already paints (ADR 0063 D1).
	a.children.register(call.ID, sub)
	// A name a continuation INHERITED is re-announced for the new spawn id: the call that spawned
	// this child named nothing, so every Driver reads its block off the call's `task` — the
	// continuation instructions — until told the name the continued delegation already wears. It is
	// the one rename that is not the namer's (ADR 0068), and it reaches the same readers by the same
	// event, stamped with the child's identity as every rename is.
	if inheritedName != "" {
		a.emitSubAgentNamed(a.turns.index, call.ID, inheritedName)
	}
	// Named CONCURRENTLY with the run it names, and only once the child is addressable: the name is
	// worth having while the delegation is still on screen, so waiting for a completion before
	// starting the work would buy a better label at the price of the thing it labels.
	stopNaming = a.startDelegationNaming(ctx, call.ID, sub, &naming)
	ran = true
	res, err = sub.Run(ctx)
	// The namer is stopped and JOINED here, before the run is read, rather than left to the defer
	// alone (whose copies are then no-ops): the name a retained child is kept under below must be
	// the name it ended its run wearing, and the namer's late-drop check reads its context — still
	// live while the result was rendered ahead of the defer — so a reply landing during that
	// rendering would have renamed a delegation the retention had already read under the old name.
	if stopNaming != nil {
		stopNaming()
	}
	naming.Wait()
	ledgerName = sub.displayName()
	result, outcome = sub.delegationResult(call.ID, res, err)
	// A child the engine stopped at a bound is RETAINED for the rest of this Exchange (P6) — the
	// fold and closing text the result carried, and everything the call asked for, so the parent
	// can continue the work from the fold rather than re-spawn it from nothing. A child whose
	// Exchange FAULTED with the parent's ctx still live is retained the same way (ADR 0082): its
	// fold was written at the fault (Agent.finishAtFault) and its last words stand as the closing
	// text, so the Turns it spent before its upstream died are continued from rather than lost —
	// the error result stays an error result, and only gains the continue line. A CANCEL is still
	// neither: it returns no result and retains nothing, so the contract at the head of this file
	// — no partial result surfaces and no snapshot lands mid-sub-agent — holds unchanged. Read
	// AFTER the namer is joined, so a delegation named out of band is retained under the name the
	// parent model has been told (ADR 0068); an unnamed one has no handle and is not retained.
	if res.StepCapped || res.Faulted {
		a.retained.retain(retainedDelegate{
			task:          retainTask,
			name:          sub.displayName(),
			tools:         args.Tools,
			outputPath:    args.OutputPath,
			fold:          sub.capFold,
			closingReport: sub.lastVisibleText(),
			bound:         sub.capHit,
			spawnCallID:   call.ID,
		})
	}
	return result, outcome
}

// classifyDelegation reads how a delegation ended off what runSubAgent is returning for it — the
// result, the dispatch outcome, whether the child's Run was reached (ran) and what it returned
// (res) — into the ledger's outcome word and, for a fault or refusal, the head line of the result
// that told the parent. A cancel is read first: it returns no result at all. An error result is
// then a refusal when no child ever ran (every early return, and a Submit that failed) and a fault
// otherwise — the child's own fault, a Run error, a recovered panic, or a completed reply the
// engine would not hand over as a report (completedResult's error shapes). A non-error result is
// capped when the child's Run said so and completed otherwise.
func classifyDelegation(result domain.ToolResult, outcome dispatchOutcome, ran bool, res domain.StepResult) (delegationOutcome, string) {
	switch {
	case outcome == dispatchCancelled:
		return delegationCancelled, ""
	case result.IsError && !ran:
		return delegationRefused, delegationCause(result.Content)
	case result.IsError:
		return delegationFaulted, delegationCause(result.Content)
	case res.StepCapped:
		return delegationCapped, ""
	default:
		return delegationCompleted, ""
	}
}

// resolveStepCap applies a sub_agent call's optional `max_steps` (asked) to the delegate cap the
// host configured, and reports the cap the child runs under plus the ask it had to clamp. It is
// the ONE rule for both readers — runSubAgent, which seeds the child with it, and runDelegation,
// which puts it on the started phase event (domain.SubAgentPhaseEvent) — so the bound a Driver
// shows is the bound the child runs under.
//
// The ask can only ever LOWER the configured cap: a model may say "this one is small, stop it
// sooner", never "let me run longer than the host allows". Both values must be positive for the
// ask to bite — an ask against an UNBOUNDED cap (0, the key switched off) is ignored, because the
// host turning the bound off is a deliberate posture the model does not get to reinstate per
// call. An ask ABOVE a positive cap is applied as the cap and REMEMBERED in the second value, so
// the result can say so (delegationResult, stepCapClampNoteFormat): the clamp used to be silent,
// and a parent that never hears its ask was cut keeps asking. That second value is 0 for every
// other spawn — no ask, a lower ask that bound, an ask against an unbounded cap.
func resolveStepCap(configured, asked int) (applied, requested int) {
	if asked <= 0 || configured <= 0 {
		return configured, 0
	}
	if asked < configured {
		return asked, 0
	}
	if asked > configured {
		return configured, asked
	}
	return configured, 0
}

// delegationLabel is the name a ledger row spells a delegation by: the display name the child
// ended its run wearing (the call's own `name`, or the namer's), else the first line of the task
// the call asked for — the Driver's own display fallback — else the call id, which is the one
// handle a refused call with no readable task still has and the model itself minted.
func delegationLabel(name string, call domain.ToolCall) string {
	if name != "" {
		return name
	}
	var args tools.SubAgentArgs
	if json.Unmarshal(call.Arguments, &args) == nil {
		if task := sanitize.ClampRunes(sanitize.FirstLine(args.Task), title.MaxDelegateRunes); task != "" {
			return task
		}
	}
	return "call " + call.ID
}

// startDelegationNaming launches the ONE out-of-band completion that names a delegation the model
// left unnamed (ADR 0068), and returns the cancel that stops it. It answers nil — and starts
// nothing — for the two cases that need no name: a delegation the spawning call already named (a
// name the model chose always wins) and a host that supplied no Config.Namer at all, which is the
// bench, an embedder and every test written before this seam existed.
//
// The engine's whole part is stating what it knows: the delegated task, and whether this child
// runs on the Sub-agent server (domain.DelegationNaming). Which endpoint answers, which model,
// which prompt and which cap the reply is cleaned to are the host's (ADR 0031, wire-silent engine)
// — the sanitiser is the only shared piece, because a name that broke a status line would be the
// engine's problem however it was produced. Config.Bypass is never consulted: naming is not a
// Reaction, so the Bypass floor has nothing to say about it (ADR 0022 addendum).
//
// Every failure is silent by contract: an error, a reply with nothing usable in it, or a name that
// arrives after the run has been reported all leave the delegation wearing the task's first line,
// which is exactly what it wore before naming existed. Nothing is logged and no event is emitted,
// so a namer that cannot reach its server costs the run nothing but the better label.
//
// The child inherits Config.Namer verbatim through the whole-Config copy newChildAgent takes, so a
// grandchild the child leaves unnamed is named the same way, one level further down.
func (a *Agent) startDelegationNaming(ctx context.Context, callID string, sub *Agent, wg *sync.WaitGroup) context.CancelFunc {
	if a.cfg.Namer == nil || sub.displayName() != "" {
		return nil
	}
	// Everything the goroutine reads, read HERE on the dispatch goroutine: the request the namer is
	// handed, and the Turn the event is stamped with. The goroutine below then touches nothing of
	// this Agent's or the child's loop state — it writes one field through the child's lock and
	// emits one event.
	req := domain.DelegationNaming{Task: sub.task, Routed: sub.ownsUpstream}
	turn := a.turns.index
	nctx, cancel := context.WithCancel(ctx)
	wg.Add(1)
	go func() {
		defer wg.Done()
		name, err := a.cfg.Namer.NameDelegation(nctx, req)
		if err != nil {
			return
		}
		line, ok := title.SanitizeTo(name, title.MaxDelegateRunes)
		if !ok {
			return
		}
		// The late-reply drop, checked AFTER the call and before the rename: runSubAgent cancels
		// this context on its way out, so a namer that answered once the delegation had already
		// been read and reported finds the run it was naming gone. Renaming it then would move a
		// label the human and the parent model have both already read.
		if nctx.Err() != nil {
			return
		}
		sub.setName(line)
		a.emitSubAgentNamed(turn, callID, line)
	}()
	return cancel
}

// delegationResult renders a child's FINISHED run as the ToolResult the parent model reads on its
// next Turn. It holds the whole outcome switch — a loop-level Run error, a cancel, a fault, the
// step cap, or the child's final answer, validated before it is handed over as a report
// (completedResult) — and, after it, the ONE site the user-steered trailer is appended at, so no
// outcome can grow a result that forgets to tell the parent the human spoke to its delegate.
//
// The receiver is the CHILD, not the spawning parent: the run being reported on is the child's and
// so is every value the report is made of (steered, lastFault, lastVisibleText, finalMessageText).
// callID belongs to the parent's sub_agent call, because the result answers THAT call.
func (a *Agent) delegationResult(callID string, res domain.StepResult, err error) (domain.ToolResult, dispatchOutcome) {
	var result domain.ToolResult
	switch {
	case err != nil:
		// Run returns a Go error only for a loop-level fault the nested Agent could not
		// localise — surface it as an error result to the parent model rather than failing
		// the parent Turn.
		result = errorToolResult(callID, "sub-agent failed: "+err.Error())
	case res.Status == domain.StatusCancelled:
		// The cancel reached the nested loop's boundary and it returned resumably; the parent
		// Turn must now roll back wholesale (D2: the recovery point is the pre-sub_agent
		// boundary — the sub-agent's progress is discarded, no partial result surfaced). Nothing
		// reaches the parent, the trailer included: there is no result to carry it.
		return domain.ToolResult{}, dispatchCancelled
	case res.Faulted:
		// The nested Exchange was ABANDONED, not completed — an Upstream fault, a recovered
		// extension panic, or an overflow the child's one fold could not rescue. It closes on
		// StatusExchangeComplete exactly as a real completion does, so the fault marker is the
		// only thing that tells them apart, and reporting it as a success would hand the parent
		// model a placeholder — or, worse, stale mid-task text from an earlier child Turn
		// (finalMessageText scans backwards for the last assistant message) — as the delegated
		// result. The child's own ErrorEvent already
		// reached the shared EventSink at Depth+1, so the human sees the cause — and the cause now
		// rides the RESULT too, because "see the preceding error" addresses a reader the parent
		// MODEL is not: it reads one tool result and has no transcript to look back through.
		cause := a.turns.fault()
		if cause == "" {
			cause = subAgentFaultNoCause
		}
		result = errorToolResult(callID, subAgentFaultPrefix+cause)
	case res.StepCapped:
		// The engine STOPPED the child at one of its bounds (Agent.Run) — the step cap, or the
		// token or time bound that takes the same path with its own head line (capResultHead) —
		// it was still asking for tools, so what it has is partial. That is not a failure and must
		// not be reported as one: an error result would throw away Turns of real work. So the
		// parent gets a NON-error result whose first line is the marker saying the answer below is
		// partial, followed by the body cappedResultBody renders: the engine's own fold of the
		// child's conversation, then whatever the child last said out loud. The child's own
		// ErrorEvent already told the human the cap hit.
		//
		// The fold is the report (Agent.foldForParent, written before the wrap-up Turn): it reaches
		// the parent on every bound, so the closing text is a second reading rather than the only
		// one. That text is normally the child's CLOSING REPORT: finishAtStepCap spends one
		// tool-less Turn (bar write_file to a spawn-named `output_path`) asking the capped delegate
		// to sum up, and its reply is the last thing committed (agent.go). This branch is also the
		// fallback for that Turn going wrong — a faulted or text-less wrap-up arrives here with
		// Faulted cleared, and the pre-cap text (or stepCapNoTextMarker) stands under the
		// closing-report sub-head exactly as it did before the wrap-up existed.
		result = domain.ToolResult{
			CallID:  callID,
			Content: a.cappedResult(),
			IsError: false,
		}
	default:
		result = a.completedResult(callID)
	}

	// The absolute cap, on every outcome's body before any note is appended to it: the notes and
	// the trailer below are the lines the parent must read whichever way the delegation ended, and
	// appending them after the cut is what keeps them whole — a cap over the finished result
	// would elide them with the middle. The head lines the TUI's recognisers read (stepCapResultFormat,
	// subAgentFaultPrefix) are inside the kept head, so the cut never re-classifies a result.
	result.Content = capDelegateResult(result.Content)

	// The routing note, for a child whose call ASKED for the Sub-agent server and was built on the
	// session server instead (ADR 0069 decision 9). It rides every outcome that produces a result,
	// for the same reason the trailer below does: a parent whose routing decision was overruled
	// must see that from the result whichever way the delegation ended.
	//
	// It is appended, never prefixed — the body's first line is the child's answer, and the marker
	// lines the engine DOES write at the head (stepCapResultFormat, subAgentFaultPrefix) are read
	// anchored there by the TUI's recognisers, so a note in front of them would be a note that
	// re-classified the result. And it is the last line of the BODY rather than of the result:
	// ADR 0063 D3's trailer below is the final line on every outcome and stays there, so where both
	// apply the note sits immediately above it.
	//
	// The missing-output note comes first in that slot, for a CAPPED child whose spawn-named
	// `output_path` is still absent after its wrap-up Turn: the capped result keeps its non-error
	// shape and its partial marker first, so the parent learns the file is not there from a body
	// note rather than from a re-classified head. A child that ran to completion without the
	// write was answered with an error result above instead (completedResult).
	if res.StepCapped && a.outputMissing() {
		result.Content += "\n" + fmt.Sprintf(missingOutputNoteFormat, a.outputPath)
	}
	// Its converse rides the same slot on a FAULTED child: the spawn-named file is there and the
	// child wrote it during its run, so the error result points the parent at the draft the fault
	// left behind (ADR 0082) — a file that predates the spawn earns no note (draftOutputSurvives).
	// The head still says the delegation faulted; the note, like the continue line below it, says
	// what survived.
	if res.Faulted && a.draftOutputSurvives() {
		result.Content += "\n" + fmt.Sprintf(draftOutputNoteFormat, a.outputPath)
	}
	if a.seatFallback {
		result.Content += "\n" + SeatFallbackNote
	}
	// And the clamp note, in the same slot for the same reason: a parent whose max_steps was cut
	// down to the configured cap must learn that from the result whichever way the delegation
	// ended, and must learn it below the head line, never in front of it.
	if a.capRequested > 0 {
		result.Content += "\n" + fmt.Sprintf(stepCapClampNoteFormat, a.capRequested, a.stepCap, a.stepCap)
	}
	// And the continue line, LAST of the body notes, for a capped or faulted child the parent
	// retains — one that ended its run wearing a name (runSubAgent joins the namer before rendering
	// this, so the name read here is the one the retention keys on). A completed child has nothing
	// to continue from and an unnamed one has no handle, so neither carries it. On the fault path
	// it rides the ERROR result: the head still says the delegation faulted and why, and the line
	// below it says the work is not lost.
	if res.StepCapped || res.Faulted {
		if name := a.displayName(); name != "" {
			result.Content += "\n" + fmt.Sprintf(continueLineFormat, name)
		}
	}

	// The parent notice, appended once for EVERY outcome that produces a result (ADR 0063 D3) —
	// the success above, the step cap, the fault and the Run error alike, because a human who
	// steered a delegate must be told they did whichever way it ended. It is deliberately the
	// result's FINAL line: the absolute cap above ran before it was appended, and the only clamp a
	// delegation result meets after this returns is the structural floor in appendToolResult
	// (dispatch.go), which elides the MIDDLE of an oversized body while keeping its head and tail
	// lines — so the trailer survives a clamped result by shape rather than by being re-appended
	// anywhere later.
	if a.steered > 0 {
		result.Content += userSteeredTrailerSeparator + userSteeredTrailer(a.steered)
	}
	return result, dispatchDone
}

// delegation is everything a delegate IS that its Config cannot say — the constructor input
// newDelegateAgent builds a child from (construct.go). newChildAgentOn composes one value per spawn
// and hands it over whole; the constructor copies each fact into the Agent's runtime fields ONCE, so
// a child is never built as a top-level Agent and then overwritten into a delegate. The three kinds
// of fact it carries: the child's IDENTITY and BOUNDS (depth, the spawning call, its task and name,
// the three delegate caps), its SEAT facts (the fallback note, the dialect and client it speaks
// over), and the HANDLES it shares with the parent by reference — where a fresh instance would strand
// state the parent's session owns (the undo journal, ADR 0051; the Console registry, ADR 0059 §6;
// the Delegation-target latch, ADR 0045; the context-file cache, ADR 0026) or lose a chain (the
// guards' shared dangerous floor, the tighten-only mode view). It deliberately carries NO task-list
// handle: the child gets its own fresh one from construction (ADR 0072, ratified call), because a
// delegation is its own run with its own decomposition, and a child ticking rows off the parent's
// checklist would rewrite a list the parent is still working from.
type delegation struct {
	depth       int    // parent+1, so the child's events nest (ADR 0013)
	spawnCallID string // the sub_agent call being served — the child's run identity, stamped on every Event it emits (domain.EventBase.CallID)
	task        string // that call's delegated task — the child's identity in words, on every Approval it raises
	name        string // the call's optional short name, already normalised by delegationName; "" = unnamed

	stepCap  int              // the delegate step cap at spawn (Config.Delegation.MaxSteps); 0 = unbounded
	tokenCap int              // and its two siblings, read at spawn for the same reason
	timeCap  time.Duration    //
	now      func() time.Time // the clock the time bound reads — the parent's, so a pinned parent pins the child

	seatFallback  bool                   // asked for the Sub-agent server and got the session one (ADR 0069 decision 9)
	effortDialect provider.EffortDialect // the wire shape of an effort intent on the server this child speaks to (ADR 0060 §3)
	upstreamOwned bool                   // a routed spawn dialled its own client and the child closes it; an unrouted one borrows the session's
	dial          Dialer                 // the parent's Dialer, inherited: the seam a routed grandchild's client is dialled through (Dialer)
	tap           *wireTap               // the Inspector seam of a client this spawn BUILT, bound once the child's identity is stamped; nil when unrouted

	consoleOwner   string             // the engine-minted Console privilege key (console.Registry.MintOwner), never the model-chosen call id
	guards         security.Guards    // Guards.ForSubAgent: fresh live state over the parent's shared dangerous floor
	contextFiles   []contextFile      // the PARENT's cache, verbatim — a sub-agent is not a session boundary, so the child never re-reads the workspace
	parentLiveMode func() domain.Mode // the parent's effectiveMode accessor — the tighten-only view that composes down the chain (ADR 0013)
	latch          *delegationLatch   // the parent's holder, shared — or an empty one for a session-seated child (ADR 0069 decision 3)
	journal        *undo.Journal      // the parent's undo journal, shared: delegated writes belong to the current Exchange's undo step (ADR 0051)
	consoles       *console.Registry  // the engine's one Console registry, shared: the cap of four is per engine, not per delegation (ADR 0059 §6)
}

// newChildAgent constructs the nested Agent for a sub-agent, threading this Agent's privileges
// bounded (ADR 0005/0013): the parent's LIVE Mode, LIVE confine-to-workspace flag, LIVE Bypass and
// LIVE auto-Compaction gate at spawn (Shift+Tab, /confine and the settings surface can move any of
// them mid-session — the child inherits what the parent
// actually has NOW, never a stale construction seed) / Approver / Confiner verbatim (never
// loosened beyond the parent's current privileges), PLUS a tighten-only
// live view of the parent's EFFECTIVE mode (child.liveMode) so a mid-delegation tightening
// reaches the still-running child at ANY depth, a Guards bundle that isolates live state but shares the dangerous
// floor read-only (Guards.ForSubAgent), a tool set that is a SUBSET of this Agent's tools
// (defaultSubAgentTools — never an expansion, withholding sub_agent at the depth bound and the
// human's seat — ask_user, present_document — at every depth; the call's `tools` argument may
// narrow it further, requestedChildTools),
// the SAME EventSink, the parent session's context-file content
// verbatim (copied, never re-read — a sub-agent is not a session boundary), and Depth =
// parent+1 so its events nest. The
// nested Agent is NOT given the parent's pending input, conversation, or task list — it starts
// fresh with only the delegated task (the ADR-0008 statelessness boundary), and its checklist is
// its own empty one (ADR 0072). The allow-for-session approval memory is
// deliberately NOT on that withheld list: it is scoped to the SESSION rather than to an Agent, and
// it reaches the child through the very Approver threaded above — the shared queueing seam holds it
// (approvalCache in approvalcache.go), so a gate the human already cleared anywhere in the tree does
// not ask the child again, and an allow the child earns outlives it for the parent and its siblings.
//
// The Upstream responder used to be on that inherited list too — this doc said the child gets "the
// SAME Upstream responder and EventSink" — and ADR 0045 reverses exactly that clause for the
// Upstream half: when a Delegation target is LATCHED the spawn is ROUTED, and the child dials the
// Sub-agent server on a provider client of its own, against that server's model, context window
// (the parent's still, when the target names none) and model profile, with the Bypass posture
// the flagged entry carries. With NO target
// latched — nothing flagged, the server unreachable, no model bound there — the child takes the
// parent's Upstream verbatim, which is what every delegation did before routing existed, so the
// fallback is not a degraded mode but the original one (ADR 0045 §4). Routing never widens
// privilege: the Mode, Approver, Confiner, blast radius and tool bounds above are the parent's
// whichever server answers, and only the POSTURE key ADR 0045 §2 puts on the flagged entry —
// Bypass, which gates no tool — may differ, and only because the host was configured to say so.
//
// spawnCallID is the id of the sub_agent tool call being served — the child's RUN IDENTITY,
// stamped on every Event it emits (domain.EventBase.CallID). It is what tells one delegated
// stream from another once siblings share a depth (ADR 0039), so it is threaded at
// construction rather than at each emission: the child's own tools, Reactions and nested
// delegations all emit through its base() and inherit it for free.
//
// task is that same call's delegated task — the child's identity in WORDS rather than in ids, and
// the only one a human can read. It rides every Approval the child raises
// (domain.ApprovalRequest.SubAgentTask), so a prompt that queued behind a sibling's still says
// which agent is asking. It is threaded here for the same reason the id is: the child carries it
// for its whole run, and every gate it reaches is one it asks for itself.
//
// name is the OPTIONAL short name the same call may have supplied, already normalised by
// delegationName — the child's identity in a FEW words where the task is a sentence, so a
// display too narrow for the task can still say which delegation it is showing. Empty means the
// model named no delegation, and every display falls back to the task's first line. Like the
// task it is display identity only: it is never consulted for privilege (ADR 0005).
//
// It is the DEFAULT-SEAT spelling of newChildAgentOn: the seat the `sub-agents-server` key decides,
// which is what every spawn took before ADR 0069 and what every caller that never names a seat
// still means. runSubAgent calls the seat-taking form directly.
func (a *Agent) newChildAgent(spawnCallID, task, name string) (*Agent, error) {
	return a.newChildAgentOn(seatConfigured, spawnCallID, task, name)
}

// newChildAgentOn is newChildAgent with the Delegation SEAT stated (ADR 0069) — the one axis a
// single sub_agent call may move, and the only difference between the two: everything the doc above
// describes (privileges, guards, tools, identity, the shared journal and console registry) is
// built identically whichever seat is asked for, because a seat says where the child runs and never
// what it may do.
//
// seatConfigured reads the latch and takes today's path verbatim. seatSession never reads it, so
// the child is built on the parent's Upstream with the parent's posture however the session is
// routed — and it is handed an EMPTY latch of its own, so its own nested delegations stay where it
// was put (ADR 0045 decision 1's identity-once-there rule, read for the seat the model chose).
// seatSubAgentsServer routes when a target is latched and otherwise degrades to the session server
// (ADR 0045 §4), recording that on the child so its result carries the note.
func (a *Agent) newChildAgentOn(seat delegationSeat, spawnCallID, task, name string) (*Agent, error) {
	childCfg := a.cfg
	childCfg.Mode = a.Mode() // inherit the parent's LIVE mode at spawn (Shift+Tab may have changed it),
	//                          read under the lock since this runs on the worker goroutine during dispatch
	childCfg.ConfineToWorkspace = a.ConfineToWorkspace() // likewise the parent's LIVE blast radius at spawn
	//                                                     (/confine may have moved it since construction)
	childCfg.ScratchDir = a.ScratchDir() // and the parent's LIVE session scratch dir — a session
	//                                      boundary may have moved it (SetScratchDir) since construction
	gen := a.Generation()        // the parent's LIVE Generation at spawn:
	childCfg.Bypass = gen.Bypass // its Bypass and its Floor enable set, read as ONE value so a
	childCfg.Floor = gen.Floor   // child never runs half of each (ADR 0076 A8) — and so it runs
	//                              the same floor the parent is running (ADR 0071) —
	childCfg.ContextFillNotice = gen.ContextFillNotice // and its context-fill notice switch, so a
	//                                                    child inherits the notice (ADR 0077 D1) —
	childCfg.Context.CompactionEnabled = a.compactionEnabled() // and the auto-Compaction and Pruning gates,
	childCfg.Context.PruneToolResults = a.pruneEnabled()       // which the settings surface may have swapped
	// The context-file NAMES are deliberately NOT re-read from the live list: the child copies the
	// parent's context-file CONTENT verbatim below, because a sub-agent is not a session boundary.
	childCfg.Tools = a.defaultSubAgentTools()
	// The armed Reactions are inherited UNCONDITIONALLY, because a reaction the parent runs with
	// is part of the posture the delegation inherits —
	// with Reaction.TopLevelOnly as the one opt-out (ADR 0076). They are filtered HERE rather
	// than at the child's dispatcher so the child's own construction re-validates exactly the
	// set it will fire, and so a nested delegation inherits what its own parent kept. The
	// engine's builtins are not carried at all: the child builds its own from its own
	// Config.Floor, which the LIVE parent value above just seeded.
	//
	// BOTH armed routes travel: the parent's construction-time set and the LIVE sync lane the
	// user's `reactions:` file arms through SetReactions, read from the one Generation snapshot
	// taken above so the child never runs one entry's gate against another generation's Bypass.
	// They arrive on the child as ONE Config.Reactions, which is the child's own construction-time
	// route — so a later swap on the parent does not reach a child already running, exactly as a
	// later Floor swap does not (the sub-agent contract's spawn-time rule). A gate the parent is
	// running is therefore still asked about every call the child makes, which is what makes an
	// `ask` on a delegation safe to defer to the child.
	childCfg.Reactions = inheritedReactions(a.cfg.Reactions, gen.Sync)

	// ROUTING (ADR 0045). The latch is snapshotted ONCE, here, and everything below reads that one
	// value: a beat landing mid-spawn must never build half a child from each target. A nil
	// snapshot is the FALLBACK and takes the path above verbatim — the parent's Upstream, the
	// parent's model, window and profile, the parent's live Bypass and its inherited catalogue — so
	// a session with nothing flagged, or a Sub-agent server that is down, delegates exactly as it
	// did before this existed.
	//
	// A non-nil snapshot makes this delegation ROUTED. The target is the resolved Sub-agent server,
	// computed whole by the host from the flagged entry's pins and its own heartbeat's observations
	// (ADR 0045 §3), so the engine applies what it is handed and reads no config of its own
	// (ADR 0031): the dial facts and the window land on the child's Config, the profile with them
	// because a tool-call format and a thinking-tag shape are facts OF the model the child is about
	// to speak to (ADR 0044) — construction translates it into the child's parse seam through the
	// same processing.ParserFor the one-swap applyProfile runs, so a routed child reads the grunt
	// model's dialect rather than the orchestrator's.
	//
	// The POSTURE key follows ADR 0045 §2's replace-or-inherit rule and is already seeded with the
	// inherited value above: a PRESENT `bypass:` replaces it WHOLE (no OR-ing of flags), an ABSENT
	// one leaves the parent's live value standing. The per-seat `mechanisms:` map that used to
	// travel beside it went with the catalogue it named (ADR 0076 A6), so the seat's only posture
	// is the flag.
	//
	// The client is built rather than mutated — provider.Client.SetModel rebinds the model and
	// deliberately never the endpoint — so the child's wire target moves atomically with its key,
	// the same idiom SwitchUpstream takes for the session (rebind.go), and it is built through the
	// parent's Dialer, the one seam every dial in the engine crosses. The parent's own responder is
	// untouched: routing changes what a SPAWN builds, never what the session speaks to.
	// tap is the Inspector's capture seam for a client this spawn BUILDS (below). An unrouted spawn
	// builds none — it speaks over the parent's connection, whose tap is already bound to the
	// parent — so tap stays nil there and binding it is a no-op (wireTap).
	var tap *wireTap
	upstream := a.upstream
	// ownsUpstream travels with the client below: a routed spawn DIALS one and hands the child the
	// right to tear it down, an unrouted spawn borrows the session's and hands over nothing.
	ownsUpstream := false
	// The routed server's effort dialect, kept out of the block below because the value it settles
	// is composed after it (delegation.effortDialect). The zero is the honest "this spawn is not
	// routed, or its target names no dialect" — both leave the parent's shape standing.
	routedDialect := provider.EffortDialectNone
	// The latch is read for every seat but the session one. seatSession is the model naming the
	// parent's own Upstream, so a target latched a moment before this spawn must not overrule it —
	// not reading the latch is what makes that true by construction rather than by a later branch
	// remembering to. seatConfigured reads it and nothing below can tell the difference from the
	// unconditional read this replaced.
	var target *DelegationTarget
	if seat != seatSession {
		target = a.delegationTarget()
	}
	// An explicit ask for the far seat that finds no usable target degrades to the session server
	// (ADR 0045 §4) and says so in the result (ADR 0069 decision 9). Decided HERE, at the one place
	// that holds both what was asked and what the single snapshot above found: a second read of the
	// latch to answer the same question could see the next beat and disagree with the child that
	// was built.
	seatFallback := seat == seatSubAgentsServer && target == nil
	if target != nil {
		childCfg.Endpoint = target.Endpoint
		childCfg.APIKey = target.APIKey
		// The target's own wire, unconditionally — never the parent's: the child is on another
		// server, and "" is that server's own answer (folded to openai at the dial, ADR 0078).
		childCfg.Wire = target.Wire
		childCfg.Model = target.Model
		// The window is the one target field that may name NOTHING: a flagged entry with no
		// `context-window:` pin, on a server whose beat observed no per-slot window either, resolves
		// to 0 (the host leaves it there rather than inventing a number). Assigning that 0 would
		// build the child WINDOWLESS — its Budget and automatic Compaction inactive, and its readings
		// stamped 0, which sends both Drivers to their "the reading names none" fallback and paints a
		// routed fill against the SESSION's window, the one window that child is not in. So an
		// unnamed window is not a replacement at all: it leaves the parent's standing, seeded above.
		// The parent's number is the better wrong answer than none — a routed child is never
		// constructed windowless — and it is what an UNROUTED child gets anyway. Negative is folded
		// in with 0 because a target cannot mean it (config refuses a negative pin; a beat cannot
		// observe one), so both spellings mean the same thing here: the target named no window.
		if target.ContextWindow > 0 {
			childCfg.Context.MaxContextTokens = target.ContextWindow
		}
		// The room INSIDE that window the child works in — the target's `working-window:`, carried
		// unconditionally like the reply ceiling below and for the same reason: a bound in tokens is
		// a number sized for ONE server's window, so an unbounded target must not leave the parent's
		// standing over the window just settled above. 0 is the honest absent value, and it puts the
		// child back in the whole of the routed server's window.
		childCfg.Context.WorkingWindow = target.WorkingWindow
		// The reply ceiling, unconditionally — the one routed field whose zero IS the answer (ADR
		// 0046). An unpinned target leaves the child deriving its cap from the window just settled
		// above, which is the routed server's; keeping the parent's pin would bound a reply from
		// this server by a number that describes the one the parent happens to be on.
		childCfg.Context.MaxOutputTokens = target.MaxOutputTokens
		// And how the window just settled above is SPLIT — the target's `response-reserve:` override,
		// applied only when it states one (see the field's contract). An entry that states none leaves
		// the parent's resolved share standing, which is the run's top-level key: a fraction stays
		// meaningful against any window, so there is nothing here to describe the wrong server.
		if target.ResponseReserveFraction > 0 {
			childCfg.Context.ResponseReserveFraction = target.ResponseReserveFraction
		}
		childCfg.Profile = target.Profile
		routedDialect = target.EffortDialect
		if target.Bypass != nil {
			childCfg.Bypass = *target.Bypass
		}
		var opts []provider.Option
		opts, tap = dialOptions(childCfg)
		upstream = a.dial(target.Endpoint, target.Model, target.APIKey, opts...)
		ownsUpstream = true
	}

	// Everything the child is that its Config cannot say is composed HERE, once, as the one value
	// its constructor reads (delegation): its identity, its bounds, its seat facts, and every handle
	// it shares with the parent by reference rather than owning afresh. Nothing is written to the
	// child after it is built.
	d := &delegation{
		depth:        a.depth + 1,
		spawnCallID:  spawnCallID,
		task:         task,
		name:         name,
		seatFallback: seatFallback,
		// The delegate step cap, seeded HERE and only here: a top-level Agent stays at 0 (uncapped)
		// however the key is set, because the bound is on delegates alone. It rides childCfg, which is
		// the parent's whole Config, so a ROUTED spawn takes the same cap as an unrouted one — the key
		// is top-level, not per-server (ADR 0045 replaces the dial facts, the two posture keys and the
		// server's effort dialect — no bound on spend) — and a grandchild inherits it the same way.
		// runSubAgent may lower it for this one delegation from the spawning call's max_steps. Its two
		// siblings ride the same Config for the same reasons and are read here at SPAWN, so a value
		// the settings surface moved bounds the children spawned after the move and never one already
		// running.
		stepCap:  childCfg.Delegation.MaxSteps,
		tokenCap: childCfg.Delegation.MaxTokens,
		timeCap:  childCfg.Delegation.Timeout,
		// The clock the time bound reads is the parent's, so a test that pins the parent's now has
		// pinned the child's.
		now: a.now,
		// The wire shape an effort intent is expressed in. The FLOOR is the parent's LIVE field rather
		// than the childCfg copy the child's Config carries. The field is the authority the way it is
		// everywhere else — the Config only ever SEEDS it (agent.go), and a Rebind writes the two
		// together — so reading the field is what makes the child speak the shape the parent's own
		// next request will speak, whatever a rebind arriving around this spawn leaves on the copy.
		// Read the copy instead and every effort-gated decision downstream (the compaction
		// summarizer's EffortOff, compact.go) could be taken against the wrong server.
		//
		// Routing DOES change the answer: a dialect is a property of the SERVER (ADR 0060 §3) and a
		// routed child is on another one, so the target names its own and the spawn takes it — the
		// flagged entry's `effort-dialect:` pin, else the tell that server's own heartbeat saw.
		// Handing a routed child the ORCHESTRATOR's shape is what made its summarizer ask for no
		// reasoning in a field the grunt server ignores: the fold then spent the whole compaction cap
		// thinking and faulted at every Turn boundary. A target that names NO dialect (the zero)
		// leaves the parent's standing, which is exactly what a routed child spoke before the spawn
		// could tell the difference.
		effortDialect: a.effortDialect,
		upstreamOwned: ownsUpstream,
		dial:          a.dial,
		tap:           tap,
		// The Console privilege key, minted by the registry that compares it rather than taken from
		// the spawning call's id: that id is the model's to choose, and a text-format parser numbering
		// calls per Turn can hand two siblings the same one — a collision that would let one sibling's
		// end reap the other's shells (ADR 0059 §6). A nil registry mints "", which is the top-level
		// key; that is harmless on a child, because an engine with no registry holds no Console.
		consoleOwner: a.consoles.MintOwner(),
		guards:       a.guards.ForSubAgent(),
		contextFiles: a.contextFiles,
		// A tighten-only view of the parent's EFFECTIVE mode (ADR 0013): the child's disposition takes
		// TighterMode(parentEffective, spawnMode), so a parent tightening mid-delegation reaches the
		// child while a parent loosening cannot loosen it. It is the parent's effectiveMode accessor —
		// not its own Mode — so the view COMPOSES down the chain: a depth-2 grandchild reads its
		// parent's effective mode, which already folds in the top-level agent's live mode, and a
		// top-level tightening therefore reaches every descendant rather than stopping at depth 1.
		// Capturing an accessor (not the raw field/mutex) keeps every read modeMu-guarded at the agent
		// that owns the field, so the child reads race-free but has no seam to mutate anything.
		parentLiveMode: a.effectiveMode,
		// The Delegation-target latch is shared by HANDLE, not copied (ADR 0045): the child holds the
		// parent's holder, so a target the host pushes after this spawn is the one the child's OWN
		// delegations read, and routing reaches every depth from the one place a host pushes to. A
		// snapshot instead would freeze depth≥1 spawns on whatever was current when their parent was
		// built — and "identity once there" (a routed child's delegations go to the same server) is
		// exactly what one shared latch gives for free.
		latch:    a.delegation,
		journal:  a.journal,
		consoles: a.consoles,
	}
	if routedDialect != provider.EffortDialectNone {
		d.effortDialect = routedDialect
	}
	// — except for a child the model put on the SESSION seat, which is handed an EMPTY latch of its
	// own (ADR 0069 decision 3 + ADR 0045 decision 1). The seat is offered at depth 0 only, so this
	// child's own tool carries no `run_on` and it can neither confirm nor undo the placement; the
	// identity rule that keeps a routed child's delegations on the routed server must therefore keep
	// a session-seated child's on the session server, and sharing the parent's holder would instead
	// send its grandchildren to the very server the model just steered this branch away from. The
	// latch is empty and nothing ever writes it: the host pushes to the top-level Agent it built.
	if seat == seatSession {
		d.latch = &delegationLatch{}
	}
	return newDelegateAgent(childCfg, upstream, d)
}

// childWithheldTools are the two tools NO sub-agent is offered, at any depth and under any `tools`
// ask: ask_user and present_document are the human's seat — a question put to the person and a
// document shown to them — and a delegation has no such seat (ADR 0019 gates both on the host's
// delegates; the owner's 2026-09-14 call withholds both from every child, superseding ADR 0039 §6's
// queued child questions). A child that needs a decision reports the question in its result and
// lets the parent ask; a child that has a deliverable names its path and lets the parent present it.
var childWithheldTools = []string{tools.AskUserToolName, tools.PresentDocumentToolName}

// defaultSubAgentTools returns the tool registry a sub-agent is constructed with: the parent's
// tool set (ADR 0005 — the caller may narrow further per task through the call's `tools`
// argument, requestedChildTools), MINUS the two human-seat tools no child ever gets
// (childWithheldTools) and MINUS the sub_agent recursion point itself when spawning the child
// would put it AT the depth bound (a depth-(max) sub-agent is never offered sub_agent, so it
// cannot recurse further). A nil parent registry yields nil (a tool-less sub-agent — the parent
// had no tools to delegate).
//
// The result is always ≤ the parent's tools: it is built from the parent registry's own names
// via Subset, so it can never name a tool the parent lacks (a privilege expansion is
// structurally impossible — ADR 0005). The ONE tool that does not arrive verbatim is sub_agent
// itself, and only in the narrowing direction: a parent offering seat choice hands the child the
// PLAIN variant, so `run_on` is a depth-0 offer (withoutSeatChoice, ADR 0069 decision 3).
func (a *Agent) defaultSubAgentTools() *domain.ToolRegistry {
	if a.tools == nil {
		return nil
	}
	names := make([]string, 0, len(a.tools.All()))
	childDepth := a.depth + 1
	for _, t := range a.tools.All() {
		// Withhold sub_agent from a child that would itself be AT the depth bound: it must
		// not be able to recurse, so it never sees the tool. (The recursion point also
		// refuses defensively — defence in depth.)
		if t.Name() == tools.SubAgentToolName && childDepth >= a.maxDepth() {
			continue
		}
		// And the human's seat from every child, unconditionally: a sub-agent never puts a
		// question to the person or a document in front of them.
		if slices.Contains(childWithheldTools, t.Name()) {
			continue
		}
		names = append(names, t.Name())
	}
	return withoutSeatChoice(a.tools.Subset(names...))
}

// requestedChildTools turns the `tools` argument of one sub_agent call into the name-list the
// child's registry is narrowed to, or nil when the call named no roster and the child keeps the set
// defaultSubAgentTools built. Both spellings are answered against the set the child would
// otherwise INHERIT, so neither can widen it (ADR 0005):
//
//   - the read-only keyword yields every inherited tool Plan mode admits on every target
//     (planAdmits — the class, never the bare ReadOnly() self-declaration, so a self-declared
//     read-only tool that launches a subprocess is left out exactly as Plan leaves it out) plus
//     sub_agent where the depth bound still offers it, because a read-only child may still
//     delegate read-only work exactly as a Plan-mode parent may (toolMenu keeps it for the same
//     reason);
//   - a list of names is checked against the PARENT's registry and refused whole when any name is
//     unknown — the refusal names every unknown one, in the order given, so the model reads the
//     spellings it got wrong rather than finding the tool silently gone (the check runs BEFORE
//     Subset, which would drop the name without a word). A name the parent holds but no child gets
//     (childWithheldTools, sub_agent at the bound) passes the check and is dropped by the
//     intersection: it is not a tool the model misspelled, it is one a child never has.
//
// An empty list is the zero value — "no narrowing" — rather than a tool-less child, because a model
// that emits `"tools": []` out of habit should not lose its delegate's whole menu to the habit.
func (a *Agent) requestedChildTools(asked tools.SubAgentRoster) ([]string, error) {
	if !asked.IsSet() {
		return nil, nil
	}
	inherited := a.defaultSubAgentTools()
	if asked.ReadOnly {
		if inherited == nil {
			return nil, nil // a tool-less parent has nothing to narrow
		}
		names := make([]string, 0, len(inherited.All()))
		for _, t := range inherited.All() {
			if planAdmits(t) || t.Name() == tools.SubAgentToolName {
				names = append(names, t.Name())
			}
		}
		return names, nil
	}
	var unknown []string
	for _, name := range asked.Names {
		if _, ok := a.lookupTool(name); !ok {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("sub_agent tools: unknown tool %s — name tools from your own menu",
			strings.Join(unknown, ", "))
	}
	return asked.Names, nil
}

// withoutSeatChoice returns roster with its sub_agent tool swapped for the PLAIN variant when the
// one it holds publishes `run_on` — the depth-0-only rule of ADR 0069 decision 3, applied where a
// child's tool set is built. Below the first hop a delegation keeps the seat it landed on, so the
// child is never offered the parameter: removing it from the schema rather than accepting and
// discarding it is the honest form, because a schema advertising a knob the engine ignores teaches
// the model a lie about its own leverage.
//
// It is never a privilege change in either direction — both variants are the same recursion point
// under the same name, and the plain one publishes strictly fewer arguments — so the ADR 0005
// subset property Subset gives is untouched. A roster whose sub_agent is already plain, or which
// holds none at all (the depth bound withheld it, or the parent had no tools), comes back as it
// went in.
func withoutSeatChoice(roster *domain.ToolRegistry) *domain.ToolRegistry {
	if !publishesSeatChoice(roster) {
		return roster
	}
	plain := domain.NewToolRegistry()
	for _, tool := range roster.All() {
		if tool.Name() == tools.SubAgentToolName {
			tool = tools.NewSubAgent()
		}
		// Cannot fail: the names come from a registry, so each is non-empty and appears once.
		_ = plain.Register(tool)
	}
	return plain
}

// publishesSeatChoice reports whether roster's sub_agent tool published the `run_on` argument —
// the ONE question that decides both whether a spawn reads the seat a call names and whether the
// child's own roster must be narrowed. Asking the tool rather than tracking a flag on the Agent is
// what keeps the two answers from ever disagreeing: the published schema is the only thing the
// model was actually told, and a mid-session SwapTools moves it.
//
// A nil roster, a roster without sub_agent, and a foreign tool registered under the name all report
// false — none of them published the argument, and the false answer is the safe one in both
// callers (the seat is ignored, the child's roster is left alone).
func publishesSeatChoice(roster *domain.ToolRegistry) bool {
	if roster == nil {
		return false
	}
	t, ok := roster.Lookup(tools.SubAgentToolName)
	if !ok {
		return false
	}
	spawner, ok := t.(*tools.SubAgent)
	return ok && spawner.OffersSeatChoice()
}

// completedResult renders the result of a child that ran to COMPLETION — the default outcome of
// delegationResult — after checking its closing text against the four shapes that are not a
// report, in this order: degenerate narration (degenerateRepeat), an error result heading the
// text's first degenerateResultHeadLines lines with degenerateResultFormat; (a) a spawn-named
// `output_path` the child never wrote, an error result heading the text with
// missingOutputResultFormat; (b) a closing text that is unparsed tool-call markup
// (floor.HasToolCallMarkup), an error result heading it with markupResultHead; (c) a bare
// acknowledgement (isAcknowledgement), the non-error noReportMarker alone. Every other text is
// the child's report, byte for byte, as it always was — up to the absolute cap delegationResult
// applies to every outcome. The capped outcome runs check (a) only — as a body note, never an
// error — because its text is a partial report by contract; what it judges instead is the text's
// SHAPE (closingShapeOf), and a non-report there changes the head and sub-head, never the shape
// of the result.
func (a *Agent) completedResult(callID string) domain.ToolResult {
	text := a.finalMessageText()
	if repeats, degenerate := degenerateRepeat(text); degenerate {
		return errorToolResult(callID, fmt.Sprintf(degenerateResultFormat, repeats)+"\n"+headLines(text, degenerateResultHeadLines))
	}
	switch {
	case a.outputMissing():
		return errorToolResult(callID, fmt.Sprintf(missingOutputResultFormat, a.outputPath)+"\n"+text)
	case floor.HasToolCallMarkup(text):
		return errorToolResult(callID, markupResultHead+"\n"+text)
	case isAcknowledgement(text):
		return domain.ToolResult{CallID: callID, Content: noReportMarker, IsError: false}
	}
	return domain.ToolResult{CallID: callID, Content: text, IsError: false}
}

// finalMessageText returns the text of the last assistant message in the sub-agent's
// conversation — the delegated result reported back to the parent. An empty conversation (or
// one with no assistant text) yields a neutral note rather than an empty string, so the parent
// model always receives an intelligible result. It is reached only for a COMPLETED Exchange:
// runSubAgent answers a faulted one with an error result before it gets here, so neither that
// note nor a stale mid-task message can stand in for a delegation that never finished.
func (a *Agent) finalMessageText() string {
	if text := a.lastVisibleText(); text != "" {
		return text
	}
	return "(sub-agent completed with no final message)"
}

// cappedResult renders a capped delegation's whole result: the head line capResultHead writes for
// the shape closingShapeOf judged the child's closing text (lastVisibleText) to be, then the body
// cappedResultBody renders for the same text and shape — judged ONCE, so head and body never
// disagree about what the text is. The receiver is the CHILD, as in delegationResult.
func (a *Agent) cappedResult() string {
	text := a.lastVisibleText()
	shape := closingShapeOf(text)
	return a.capResultHead(shape) + "\n" + a.cappedResultBody(text, shape)
}

// cappedResultBody renders the body of a capped delegation's result, under the head line
// capResultHead writes: the engine fold under engineSummaryHead, a blank line, then the child's
// closing text — under closingReportHead for a report, under closingNarrationHead for a non-report
// shape (the text itself is forwarded whole either way), and stepCapNoTextMarker under
// closingReportHead for a child that never spoke.
func (a *Agent) cappedResultBody(text string, shape closingShape) string {
	if text == "" {
		text = stepCapNoTextMarker
	}
	head := closingReportHead
	if shape.isNonReport() {
		head = closingNarrationHead
	}
	return engineSummaryHead + "\n" + a.capFold + "\n\n" + head + "\n" + text
}

// lastVisibleText returns the text of the last assistant message that carried any — the child's
// last words, whether or not they were its final answer — and "" when it produced none. It is the
// seam under finalMessageText, split out because a STEP-CAPPED delegation needs the raw answer:
// the neutral note finalMessageText substitutes says "completed", which is exactly what a capped
// child did not do, so the capped path supplies its own marker (stepCapNoTextMarker) instead.
func (a *Agent) lastVisibleText() string {
	for _, m := range reverseMessages(a.conv.Messages()) {
		if m.Role == domain.RoleAssistant && m.Content != "" {
			return m.Content
		}
	}
	return ""
}

// reverseMessages returns msgs in reverse order so finalMessageText can scan from the most
// recent assistant message backward without indexing gymnastics.
func reverseMessages(msgs []domain.Message) []domain.Message {
	out := make([]domain.Message, len(msgs))
	for i, m := range msgs {
		out[len(msgs)-1-i] = m
	}
	return out
}
