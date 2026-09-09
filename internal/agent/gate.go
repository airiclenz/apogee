package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// The GATE stage of the Approver (ADR 0076 D2): the user's own answer to "may this call run",
// asked once the Resolution is computed and before anything executes.
//
// It is a STAGE OF THE APPROVER rather than a leg of the pre-tool-exec cascade, and the
// difference is the whole design. The cascade reshapes a pending call; a gate decides its fate,
// which is the mode ladder's own business — so the gate runs where the ladder's verdict is in
// hand, folds into it, and hands dispatch a single resolution to execute. Nothing here decides
// anew what resolve() already decided: a gate can only tighten it.
//
// The fold is deliberately one-directional (ADR 0049 §4): a script may say NO, never YES.
// `deny` refuses the call outright, `ask` raises it to the human whatever the mode said, and
// `allow` means nothing at all — the ladder's verdict stands, because a user's script that could
// pre-approve a call would be a second, unaudited autonomy ladder.
//
// Every unreadable answer lands on `ask`, not on `allow`: an empty or unparseable stdout, a
// non-zero exit, a timeout, a refused permit and a panicking Go handler all end with the human
// being asked. A gate the user armed and cannot run is a question, never a silent pass — which
// is the one place the sync lane is NOT fail-open, and is why Bypass leaves gates armed (D9).

const (
	// gateDenialPrefix opens the model-facing refusal a denied call carries. It is
	// ENGINE-AUTHORED and names only the reaction: the reason a gate gives is written for the
	// human being asked and never reaches the model, so a gate cannot smuggle instructions into
	// the conversation through a refusal it manufactured.
	gateDenialPrefix = "tool call denied by reaction "

	// gateDelegationDetail is the Detail an `ask` books when the call it answered is a
	// DELEGATION. Nothing executes at the recursion point, so the ask has nothing to hold up
	// there; it is answered by the same gate on the child's own tool calls, which the child
	// inherits. The firing says so rather than claiming a question was put to anyone here.
	gateDelegationDetail = "deferred to the child's calls"

	// gateReasonRunes caps a gate's human-facing reason. It reaches an Approval prompt, which has
	// room for a sentence and not a paragraph, and it comes from a command whose output the
	// engine does not control.
	gateReasonRunes = 240
)

// applyGates folds the armed gate reactions' answers into one resolved tool call's verdict, and
// is the only place a gate reaction fires (the seam cascade skips class gate — reactions.go).
// Dispatch calls it immediately after resolve() at both of its sites, so the serial path and the
// fan-out's prepare phase gate identically.
//
// The fold, in ladder order: the FIRST deny wins and the call is refused; otherwise the FIRST ask
// upgrades the verdict to a forced Gate, so the approval cache is skipped and the human reads
// which reaction asked and why; an allow, and a gate that gave no verdict at all, leave the
// verdict exactly as resolve() computed it. A verdict that already REFUSES the call is not put to
// the gates: there is nothing left to tighten, and asking a user's command about a call that will
// never run would spawn a process for no decision.
//
// A DELEGATION is the one verdict an ask does not change (owner call 2026-09-09). No Gate ever
// reaches the recursion point — a Tier-2 force is deliberately not applied to a delegation
// (D3/ADR 0013) — and nothing executes there anyway: the child inherits this gate and asks on the
// calls that actually do something, so the human is still asked before any action runs. A deny
// still refuses the delegation, because that answer needs no seam to land on.
func (a *Agent) applyGates(ctx context.Context, turn int, call domain.ToolCall, verdict resolution) resolution {
	if verdict.kind == resolveRefuse {
		return verdict
	}
	gates := a.gateReactions()
	if len(gates) == 0 {
		return verdict
	}
	deferred := verdict.kind == resolveDelegate

	askID, askReason, asked := "", "", false
	for _, r := range gates {
		decision := a.askGate(ctx, turn, r, call)
		if decision.Verdict == "" {
			// The reaction inspected the call and said nothing, which is not a firing.
			continue
		}
		a.bookGate(turn, r, decision, deferred)
		if decision.Verdict == domain.GateDeny {
			// The first no ends it: the later gates are asked nothing about a call that is
			// already refused, so no further command is spawned.
			return gateRefusal(verdict, r.ID)
		}
		if decision.Verdict == domain.GateAsk && !asked {
			askID, askReason, asked = r.ID, decision.Reason, true
		}
	}
	if !asked || deferred {
		return verdict
	}
	return a.gateAsk(verdict, askID, askReason)
}

// gateReactions is the ladder of gate reactions this Agent runs, in arming order: the
// construction-time set first, then the live sync lane the user's `reactions:` file arms through
// SetReactions (armedLadder, reactions.go). Reading the same ladder the seam cascade reads is what
// keeps one entry's gate and the same entry's advise in one order, and it is read ONCE per fold so
// a swap landing mid-fold cannot change the list being walked.
//
// Bypass is not consulted: it switches off what a reaction says to the MODEL, and a gate speaks to
// the human about what the model is about to do (ADR 0076 D9).
func (a *Agent) gateReactions() []domain.Reaction {
	var gates []domain.Reaction
	for _, r := range a.armedLadder() {
		if r.spec.Class == domain.ClassGate && slices.Contains(r.spec.On, domain.MomentPreToolExec) {
			gates = append(gates, r.spec)
		}
	}
	return gates
}

// askGate puts one pending call to one gate reaction and returns the answer the fold reads.
//
// A gate that could not answer — a command that failed, timed out, was refused a permit or wrote
// something unreadable, and a Go handler that panicked — is reported once through the sync lane's
// own reporter (one operator line and one "failed" firing, item 4's shape) and then ASKS, naming
// the failure so the human reads why they are being asked at all. That escalation is what makes a
// broken gate safe: the surface a user armed to bound the model cannot be disarmed by breaking it.
func (a *Agent) askGate(ctx context.Context, turn int, r domain.Reaction, call domain.ToolCall) domain.GateDecision {
	decision, err := a.gateAnswer(ctx, turn, r, call)
	if err == nil {
		return decision
	}
	a.reportReaction(turn, r.ID, domain.MomentPreToolExec, err)
	return domain.GateDecision{
		Verdict: domain.GateAsk,
		Reason:  clampRunes("did not answer ("+err.Error()+")", gateReasonRunes),
	}
}

// gateAnswer invokes one gate reaction's handler and reports its decision, or the error that
// stands in for one. The two handler shapes are the two the domain admits for class gate: a
// user's command over argv, and an engine or embedder's Go func reading Outcome.Gate.
func (a *Agent) gateAnswer(
	ctx context.Context,
	turn int,
	r domain.Reaction,
	call domain.ToolCall,
) (domain.GateDecision, error) {
	switch h := r.Handler.(type) {
	case domain.ArgvHandler:
		stdout, err := a.runSyncArgv(ctx, turn, r, domain.SeamPayload{
			Event: domain.MomentPreToolExec,
			Tool:  call.Tool,
			// Copied rather than referenced: the document outlives the call the cascade may
			// still be reshaping, exactly as the advise route's does.
			Arguments: append(json.RawMessage(nil), call.Arguments...),
		})
		if err != nil {
			return domain.GateDecision{}, err
		}
		return parseGateAnswer(stdout)
	case domain.PreToolExecFunc:
		return a.callGateFunc(ctx, turn, r, h, call)
	default:
		return domain.GateDecision{}, fmt.Errorf("apogee: reaction %q: a gate cannot be a %T", r.ID, r.Handler)
	}
}

// callGateFunc runs one Go gate handler under the same recover boundary the seam cascade installs,
// and reports the verdict it gave. A panic comes back as an ERROR here rather than as the
// cascade's "did nothing": a gate that crashed is a gate that did not answer, and the caller
// escalates that to the human.
//
// The handler is handed a ToolCallEdit over this function's OWN copy of the call, so an edit it
// makes is discarded. That is deliberate: the gate stage runs after the Resolution is computed,
// so a reshaped call here would execute under a verdict that was decided about a different call.
// Reshaping is the shape classes' business at the pre-tool-exec seam; a gate answers with a
// verdict.
func (a *Agent) callGateFunc(
	ctx context.Context,
	turn int,
	r domain.Reaction,
	fn domain.PreToolExecFunc,
	call domain.ToolCall,
) (decision domain.GateDecision, err error) {
	var panicErr error
	defer func() {
		if panicErr != nil {
			decision, err = domain.GateDecision{}, panicErr
		}
	}()
	defer a.recoverHook(turn, r.ID, &panicErr)()

	out, err := fn(ctx, a.loopView(turn), domain.NewToolCallEdit(&call))
	if err != nil {
		return domain.GateDecision{}, err
	}
	if !acted(out) {
		return domain.GateDecision{}, nil
	}
	return out.Gate, nil
}

// parseGateAnswer reads a gate command's standard output as the protocol defines it: the first
// line, trimmed, is exactly `allow`, `deny` or `ask`; everything after it is the reason for the
// human. Anything else is an error, which the caller turns into an ask — the protocol is one word
// on one line precisely so that "the script is broken" and "the script said yes" can never be
// confused.
func parseGateAnswer(stdout string) (domain.GateDecision, error) {
	if strings.TrimSpace(stdout) == "" {
		return domain.GateDecision{}, errors.New("printed nothing")
	}
	lines := strings.Split(stdout, "\n")
	first := strings.TrimSpace(lines[0])
	switch domain.GateVerdict(first) {
	case domain.GateAllow, domain.GateDeny, domain.GateAsk:
		return domain.GateDecision{
			Verdict: domain.GateVerdict(first),
			Reason:  gateReasonText(lines[1:]),
		}, nil
	default:
		return domain.GateDecision{}, fmt.Errorf("answered %q, not allow, deny or ask", first)
	}
}

// gateReasonText folds a gate command's remaining output into the ONE line an Approval prompt
// shows: blank lines dropped, the rest trimmed and joined by a space. A prompt reason is a
// sentence rather than a document, and a script that wrote its explanation across three lines
// means one sentence by it.
func gateReasonText(lines []string) string {
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			kept = append(kept, trimmed)
		}
	}
	return clampRunes(strings.Join(kept, " "), gateReasonRunes)
}

// clampRunes cuts s to at most n runes, on a rune boundary so the result is always valid UTF-8.
func clampRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// gateRefusal is the verdict a denied call carries: a Refuse whose model-facing reason names the
// reaction that said no, carrying over the audit metadata resolve() stamped so the trail records
// the blocked call exactly as an Approver's own deny records it.
//
// It builds a FRESH resolution rather than mutating the one it was handed, because a refused call
// executes nothing: the write-escape target, the confinement box and the demote fallback the
// verdict carried all authorise an execution that will not happen, and a Refuse never carries one.
func gateRefusal(verdict resolution, id string) resolution {
	return resolution{
		kind:          resolveRefuse,
		reason:        gateDenialPrefix + id,
		auditDecision: verdict.auditDecision,
		auditReason:   verdict.auditReason,
	}
}

// gateAsk upgrades a verdict to the forced Gate an asking reaction earns: the human is consulted
// whatever the ladder decided, and `force` keeps the answer out of the allow-for-session memory,
// so a gate asks on every call rather than once per session.
//
// It MUTATES the verdict resolve() computed rather than building a new one, and that is
// load-bearing: resolve() has already stamped the write-escape target this call would authorise,
// the audit metadata, and — for a Confine — the box and the runtime-demote fallback. A fresh
// literal would silently drop all of them, so an approved ask on an out-of-workspace write would
// be denied by the fence the human just authorised past, and an approved ask on a subprocess call
// would run unconfined. A Confine additionally takes confineOnAllow, which is how a forced look
// at a call the ladder would have fenced still executes fenced.
//
// The remedy is CLEARED with the reason it belonged to: it is the way out of the condition that
// forced a gate, and the condition is now "a reaction asked", which has no route out. Leaving the
// old one would blame one condition on the prompt and prescribe the fix for another.
//
// The nil-Approver branch is finishGate's rule, reproduced for the one verdict that reaches a
// gate without passing through it: a Gate always means the Approver is actually consulted (D5), so
// with none configured the call is refused rather than run unapproved. No Driver apogee ships
// reaches it — an unattended one installs a denying Approver — but an embedder may.
func (a *Agent) gateAsk(verdict resolution, id, reason string) resolution {
	if a.cfg.Approver == nil {
		return resolution{
			kind:          resolveRefuse,
			reason:        noApproverReason,
			auditDecision: verdict.auditDecision,
			auditReason:   verdict.auditReason,
		}
	}
	if verdict.kind == resolveConfine {
		verdict.confineOnAllow = true
	}
	verdict.kind = resolveGate
	verdict.force = true
	verdict.reason = gateAskReason(id, reason)
	verdict.remedy = ""
	return verdict
}

// gateAskReason is the Approval prompt's reason for a gated call: which reaction asked, and what
// it said when it asked. A gate that gave no reason still names itself, because "why am I being
// asked this" is the question the prompt exists to answer.
func gateAskReason(id, reason string) string {
	if reason == "" {
		return "reaction " + id + " asks"
	}
	return "reaction " + id + " asks: " + reason
}

// bookGate emits the firing one gate's answer books. The Action is the verdict's own word —
// "allow", "deny" or "ask" — so an observer reads what was decided without having to know the
// engine's action vocabulary, and the Detail is the reason the human was given.
//
// An ask on a DELEGATION books the deferral sentence instead of the reason: no question was put
// to anyone at the recursion point, and a Detail claiming otherwise would misreport where the
// human will actually be asked.
func (a *Agent) bookGate(turn int, r domain.Reaction, decision domain.GateDecision, deferred bool) {
	detail := decision.Reason
	if deferred && decision.Verdict == domain.GateAsk {
		detail = gateDelegationDetail
	}
	a.cfg.Events.Emit(domain.ReactionFiredEvent{
		EventBase: a.base(turn),
		Reaction:  r.ID,
		Origin:    r.Origin,
		Moment:    domain.MomentPreToolExec,
		Action:    string(decision.Verdict),
		Detail:    detail,
	})
}
