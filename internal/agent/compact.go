package agent

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"strings"

	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// Compaction sampling: a low temperature for a faithful, low-embellishment summary, and a
// generous token cap so a long conversation's summary rarely reaches it. They are fixed here
// (not a config surface) — a model-profile knob is a later, additive concern. A summary that
// DOES reach the cap is KEPT, with summaryTruncatedMarker appended so the model reading the fold
// later knows its tail is missing rather than trusting an ending that was never written. ADR 0046
// rejects salvaging the visible text of a cut-off reply (:91-92) for a TURN — half an answer
// committed as the model's answer is a worse failure than an honest fault — and in the same breath
// exempts these auxiliaries from its budgeting (:93-94, "already bound themselves at 4096 and are
// not turns"). The summary is one of them: nothing is committed as a Turn, and discarding it throws
// away tokens already spent while leaving the conversation exactly as over-budget as before.
const (
	compactTemperature = 0.2
	compactMaxTokens   = 4096

	// compactPromptOverheadTokens reserves headroom, on top of compactMaxTokens (the summary
	// response's reserve), for the summarizer's system prompt, the trailing instruction, the
	// per-message role headers, and the slack in the chars→tokens estimate. The rendered
	// transcript is budgeted to whatever the discovered window has left after both reserves.
	compactPromptOverheadTokens = 512

	// compactMinTranscriptTokens floors the transcript budget so a very small window still
	// sends a useful (if heavily elided) tail rather than collapsing to nothing.
	compactMinTranscriptTokens = 256

	// compactUnknownWindowTranscriptTokens bounds the summarizer's transcript when NO window is
	// known — neither discovery nor `context-window:` reported one. Rendering the whole
	// conversation there (what a zero budget means to the reducer) overflows exactly like the
	// request the emergency fold is rescuing, so the fold faults on every attempt and the session
	// wedges until /clear (audit 2026-08-01). The value is deliberately pessimistic rather than
	// accurate: at ~3k tokens the summary call still fits, with room left for the summary itself,
	// inside the smallest window a locally hosted server realistically runs (llama.cpp's 4096-token
	// default n_ctx), so the fold can shed history against ANY unknown window — while staying large
	// enough that the summary is written from a substantial tail rather than the last message alone.
	// A real window always beats it, which is why the give-up event names `context-window:`
	// (overflowGiveUpErr, loop.go).
	//
	// It is also the ONE ceiling the other three window-gated growth bounds fall back to — the
	// predictive guard (requestExceedsWindow, loop.go), the boundary compaction trigger
	// (historyExceedsAllocation) and the structural tool-result clamp (clampToolResult,
	// dispatch.go) — rather than each inventing its own default, and the substitution is made at
	// ONE site, deriveGrowthBounds, that all four readers call. With no window there is nothing to
	// ALLOCATE across a request's parts (context.Allocate returns the zero Allocation for exactly
	// that reason), so the one meaningful rule left is that nothing the fold can shed — the
	// conversation, or a single result about to enter it — may exceed what the fold can actually
	// render. Keying every bound to the fold's own budget is what makes the fold survivable here: a
	// result bigger than it survives the clamp only to become the most recent message the fold's
	// transcript render keeps UNCONDITIONALLY (context.renderBudgetedTranscript), which re-wedges
	// the very session the bound above rescued (audit 2026-08-01, follow-up B).
	//
	// Being a TRANSCRIPT budget is load-bearing for what each bound measures against it: the
	// transcript, never a request's fixed cost. The tool menu and the standing system content ride
	// every request and no fold can shrink either, so measuring them against this ceiling would
	// fire a bound that folding can never satisfy — the default 19-tool menu alone is ~3.8k tokens
	// at a code-heavy ratio, past a 3072-token ceiling before the user has typed anything.
	compactUnknownWindowTranscriptTokens = 3072
)

// growthBounds are the window-gated bounds the engine lets the conversation grow against: the
// room a request may fill before the predictive guard fires (requestExceedsWindow, loop.go), the
// History floor the boundary trigger and the structural clamps measure against
// (historyExceedsAllocation; structuralFloor, dispatch.go), and the token budget the summary
// call's transcript is rendered into (compactTranscriptChars). TWO windows feed them, never one:
// room and transcriptBudget derive off the ADVERTISED window (Budget.Window), the wall the server
// enforces and the one that drives overflow detection, while historyFloor derives off the
// WORKING-room History allocation (Budget.History), the soft ceiling a `working-window:` key
// lowers (domain.ContextConfig.WorkingWindow). With no window known every bound falls back to
// compactUnknownWindowTranscriptTokens, and it does so HERE and nowhere else — deriveGrowthBounds
// is the one substitution site, so the four readers cannot drift onto four defaults.
//
// It is a pure derivation off one Budget view, read live by each reader at the moment it decides
// and never cached at Turn open: Agent.Compact (/compact, outside an Exchange) and a
// SwitchUpstream between Turns must see the window the session is bound to NOW, and a value taken
// when the Turn opened would bound against a server the session no longer talks to.
type growthBounds struct {
	// windowKnown reports whether an advertised window with room past its reserve backs room.
	// False means room is the fallback and the predictive guard measures the TRANSCRIPT alone
	// against it — the only part a fold can shed (requestExceedsWindow).
	windowKnown bool
	// room is what a request may fill: the advertised window less the response reserve. It is
	// pre-margin — the uncalibrated margin (uncalibratedRoomMargin) stays the reader's live read
	// of Budget.Used.
	room int
	// historyFloor is the History allocation the boundary trigger and both structural clamps bound
	// against — the most a renderable conversation, or a single body entering it, may occupy.
	historyFloor int
	// transcriptBudget is the token budget the summary call's rendered transcript fits into: the
	// advertised window less the summary's reply reserve (compactMaxTokens) and prompt overhead,
	// floored at compactMinTranscriptTokens so a tiny window still sends a useful tail.
	transcriptBudget int
}

// deriveGrowthBounds derives the growthBounds from one Budget view (Agent.budget).
func deriveGrowthBounds(b domain.Budget) growthBounds {
	// The fallback is named ONCE: every bound below with no window to derive from takes it.
	fallback := compactUnknownWindowTranscriptTokens
	g := growthBounds{
		room:             b.Window - b.ResponseReserve,
		historyFloor:     b.History,
		transcriptBudget: fallback,
	}
	g.windowKnown = g.room > 0
	if !g.windowKnown {
		g.room = fallback
	}
	if g.historyFloor <= 0 {
		g.historyFloor = fallback
	}
	if b.Window > 0 {
		g.transcriptBudget = max(b.Window-compactMaxTokens-compactPromptOverheadTokens, compactMinTranscriptTokens)
	}
	return g
}

// foldKind names the trigger a fold runs for. Every Compaction path — the on-demand /compact
// (Agent.Compact), the estimate-driven trigger (autoCompact) and the overflow-driven one
// (emergencyFold) — is one row of foldTable keyed by it, and foldFor is the one entry that runs
// the row. The three are STRUCTURAL, not Reactions (D6/ADR 0006): no row consults cfg.Bypass,
// because a naked model overflows its window just as surely as a Reaction-laden one.
type foldKind int

const (
	foldOnDemand foldKind = iota // /compact: the human asking for this fold now
	foldEstimate                 // the history estimate outgrew its allocation at a quiescent boundary
	foldOverflow                 // the server rejected the request: fold once and re-send the Turn
)

// foldEnd classifies how a fold ended.
type foldEnd int

const (
	foldEndFolded    foldEnd = iota // the fold RAN: the conversation is prefix + summary (+ bridge, per the row)
	foldEndDeclined                 // nothing folded: a closed gate, or a skip (foldResult.skipped); conversation untouched
	foldEndCancelled                // ctx cancelled mid-summary: conversation untouched, nothing reported — the caller routes the cancel
	foldEndFaulted                  // the summary call faulted: conversation untouched, latch and event per the row
)

// foldResult is what foldFor hands its three wrappers. skipped is the reducer's Result.Skipped —
// too few messages past the protected prefix to be worth folding, so no upstream call was made and
// the conversation is untouched (end is foldEndDeclined); it is always false on a fault, since a
// fault is not a skip. err is the error the ON-DEMAND caller reports: a closed gate's refusal
// (foldRow.refusal), ctx.Err() on a cancel, the summary call's own fault; nil on every other end.
type foldResult struct {
	end     foldEnd
	skipped bool
	err     error
}

// foldBridge says when the overflow bridge follows a fold that ran. The bridge is required
// wherever the folded conversation is what the NEXT request is built from directly: Compact
// Replaces everything past the protected prefix with a single assistant summary, so the
// conversation ends on an assistant turn — what a strict chat template refuses and what an
// instruct model reads as "keep writing that summary". The bridge's ROLE closes the structure
// back to a legal …assistant → user and its TEXT tells the model, in band, that its visible
// history is a summary of a conversation that outgrew the window (overflowBridge). Appending it
// re-anchors the cached Exchange boundary to it (turnLifecycle.anchorAtBridge, a no-op outside an
// Exchange), so AbortExchange still rolls back to a clean boundary rather than into the protected
// prefix.
type foldBridge int

const (
	foldBridgeNever      foldBridge = iota // the real user message follows the summary as its own turn
	foldBridgeInExchange                   // only a fold that ran MID-Exchange (a child's Turn-boundary fold) ends on the summary
	foldBridgeAlways                       // the retried request is built from the folded conversation directly
)

// foldRow is one row of the latch table: what a trigger gates on, and what a fold that faulted or
// ran leaves behind. The gate column is open + refusal; the latch columns are standsDown, reports,
// saturates and bridge.
type foldRow struct {
	// open reports whether the trigger's gate lets this fold run.
	open func(a *Agent) bool
	// refusal is the error a closed gate hands the on-demand caller; nil declines silently.
	refusal error
	// standsDown latches the estimate-driven trigger off for the rest of the Exchange when the
	// fold FAULTS (turnLifecycle.foldFaulted), and says so on the event when it ran inExchange
	// (foldStandDownSuffix): Compact left the conversation untouched, so the very same summary
	// call over the very same history is what the next Turn boundary would run — the 2026-08-29
	// runaway, a delegate spending ~9 h on one 40-minute failing summary call per Turn, seven
	// times. openExchange clears the latch, so the main agent re-arms at its next opening while a
	// child — whose whole life is ONE Exchange — stands down for the delegation.
	standsDown bool
	// reports surfaces a fault as one ErrorEvent from Source "compaction"; the on-demand fold hands
	// its fault to the caller instead.
	reports bool
	// saturates checks, after a fold that RAN, whether the history still exceeds its allocation
	// (S2 saturation): the folded shape — the protected prefix plus the single summary — cannot
	// shrink further, so the trigger latches off (turnLifecycle.foldSaturated) with one event
	// rather than re-folding at every opening. shouldAutoCompact clears the latch once the
	// estimate drops back under the allocation. Gated on a fold that ran because a skip proves
	// nothing about whether folding can help.
	saturates bool
	// bridge says when the overflow bridge follows a fold that ran.
	bridge foldBridge
}

// foldTable is the latch table, one row per trigger.
//
// On demand: the gate is the quiescent boundary alone — mid-Exchange is refused with
// ErrInputPending so a half-streamed Turn is never orphaned, mirroring ClearContext — and neither
// the live `auto-compact` gate nor the estimate-driven latches (compactSat, compactFailed) are
// consulted: /compact is the human asking for this fold now, its fault is reported to them
// directly rather than swallowed, and `auto-compact: false` never declines it silently. No bridge:
// it runs outside an Exchange, where the next user message follows the summary as its own turn.
//
// Estimate-driven: gated on the re-entrancy guard and shouldAutoCompact (the live `auto-compact`
// gate, the S2 Exchange-boundary rule a child lifts, the allocation compare and both latches). A
// fault stands the trigger down for the Exchange; a fold that ran is checked for saturation; the
// bridge follows only a fold that ran mid-Exchange (a child's Turn-boundary fold — the depth-0
// Exchange-boundary fold runs before pendingInput is consumed, so the real user message follows
// the summary). It is quiet on success — the Replace is the visible effect, and the next Turn's
// UsageEvent re-measures the reduced fill.
//
// Overflow-driven: the reactive twin, and the ONE fold allowed to run MID-Exchange on the MAIN
// agent (ADR 0018 D5/D6, amending S2 for this path alone): a Turn whose request the server just
// rejected cannot wait for the next opening — deferring means abandoning the Exchange, which is
// exactly the failure this recovery exists to prevent. Gated on the live `auto-compact` gate —
// `auto-compact: false` opts out of recovery too, since the emergency fold IS an automatic fold,
// and a user managing the window themselves keeps the abandon behaviour with no upstream call —
// and the re-entrancy guard; neither estimate-driven latch is read or written here: this path is
// bounded by the caller's one-fold-per-Turn rule (turnRun.foldSpent) instead, and it is the
// Turn's only remedy, so an Exchange that stood the boundary trigger down still gets its single
// emergency shot. The bridge always follows, because the retried request is built from the
// folded conversation directly.
var foldTable = [...]foldRow{
	foldOnDemand: {
		open:    func(a *Agent) bool { return !a.turns.inExchange },
		refusal: domain.ErrInputPending,
		bridge:  foldBridgeNever,
	},
	foldEstimate: {
		open:       func(a *Agent) bool { return !a.compacting && a.shouldAutoCompact() },
		standsDown: true,
		reports:    true,
		saturates:  true,
		bridge:     foldBridgeInExchange,
	},
	foldOverflow: {
		open:    func(a *Agent) bool { return a.compactionEnabled() && !a.compacting },
		reports: true,
		bridge:  foldBridgeAlways,
	},
}

// foldFor is the one fold entry: it runs foldTable's row for kind — the gate, the re-entrancy
// guard (compacting, shared by every row so no two triggers can nest), the summary call
// (internal/context.Compact, protected prefix kept verbatim, everything after it Replaced by one
// summary), cancel-versus-fault, the fault's latch and ErrorEvent, the bridge, and the saturation
// check — and reports how it ended. turn stamps the events it emits.
//
// What every fold that RAN leaves behind is done here once: the Replace, the bridge the row asks
// for, and the context-fill notice's ladder re-armed (rearmFillNotice), because the climb it
// tracked was just folded away and the next result measures a new one. A fault leaves the
// conversation untouched (Compact's guarantee) and a skip folded nothing, so neither re-arms — the
// ladder still describes the history the model sees. A cancel is not a fault: it masquerades as
// a stream error, so only ctx can tell them apart, and it ends silently — no latch, no event —
// because the caller's own stream carries the cancel to a clean boundary. The Turn counter is
// untouched and the Agent stays snapshot-safe after it returns.
func (a *Agent) foldFor(ctx context.Context, turn int, kind foldKind) foldResult {
	row := foldTable[kind]
	if !row.open(a) {
		return foldResult{end: foldEndDeclined, err: row.refusal}
	}
	a.compacting = true
	defer func() { a.compacting = false }()

	res, err := apogeectx.Compact(ctx, compactCompleter{a: a}, &a.conv, a.compactTranscriptChars())
	if err != nil {
		if ctx.Err() != nil {
			return foldResult{end: foldEndCancelled, err: ctx.Err()}
		}
		if row.standsDown {
			a.turns.foldFaulted()
		}
		if row.reports {
			msg := err.Error()
			if row.standsDown && a.turns.inExchange {
				msg += foldStandDownSuffix
			}
			a.emitCompactionError(turn, msg)
		}
		return foldResult{end: foldEndFaulted, err: err}
	}
	if res.Skipped {
		return foldResult{end: foldEndDeclined, skipped: true}
	}
	a.rearmFillNotice()

	if row.bridge == foldBridgeAlways || (row.bridge == foldBridgeInExchange && a.turns.inExchange) {
		a.conv.Append(domain.Message{Role: domain.RoleUser, Content: overflowBridge})
		a.turns.anchorAtBridge()
	}
	// With no window known there IS no allocation — the compare ran against the conservative
	// unknown-window ceiling — so the remedy is appended for the same reason the overflow give-up
	// appends it (overflowGiveUpErr): this notice is then the one place the user learns that the
	// bound biting their session is an assumption apogee had to make, and that a config key
	// replaces it with the truth.
	if row.saturates && a.historyExceedsAllocation() {
		a.turns.foldSaturated()
		msg := "compaction could not bring the history under its allocation: the protected prefix " +
			"(system prompt + first user message) and the compaction summary together exceed it; " +
			"automatic folding is paused until the history estimate drops below the allocation"
		if a.cfg.Context.MaxContextTokens <= 0 {
			msg += " — " + unknownWindowRemedy
		}
		a.emitCompactionError(turn, msg)
	}
	return foldResult{end: foldEndFolded}
}

// emitCompactionError is the one site every fold-side notice leaves through: an ErrorEvent from
// Source "compaction", so a reader filtering on that source sees a faulted fold and a saturated
// trigger alike, and never the Turn's own request faults.
func (a *Agent) emitCompactionError(turn int, msg string) {
	a.cfg.Events.Emit(domain.ErrorEvent{EventBase: a.base(turn), Source: "compaction", Err: msg})
}

// Compact triggers generative Compaction on demand — the engine half of the /compact command,
// foldTable's foldOnDemand row. Valid only at a quiescent boundary; calling it mid-Exchange is
// refused (ErrInputPending). A summary-call failure leaves the conversation unchanged and is
// returned; a cancel returns ctx.Err().
//
// skipped reports that the conversation was too small to be worth folding (the reducer's
// Result.Skipped — no upstream call, conv untouched), so the caller can say "nothing to
// compact" and leave the context gauge alone rather than falsely claiming a compaction. It is
// always false on error (a fault is not a skip).
func (a *Agent) Compact(ctx context.Context) (skipped bool, err error) {
	r := a.foldFor(ctx, a.turns.index, foldOnDemand)
	return r.skipped, r.err
}

// foldStandDownSuffix is appended to a failed automatic fold's ErrorEvent when that fold ran
// INSIDE an open Exchange — a child agent's Turn-boundary fold, the only place the stand-down
// latch actually bites, since a main-agent fold runs at an Exchange opening whose openExchange
// clears the latch immediately after. It tells the human why the identical failure will not be
// reported again: one event, then silence for the rest of the Exchange.
const foldStandDownSuffix = " — automatic folding stands down for the rest of this exchange"

// autoCompact runs generative Compaction at a quiescent boundary when the conversation history has
// outgrown its Budget allocation — the automatic, estimate-driven trigger (Phase-4 item 9, CONTEXT:
// Compaction "the default reducer"), foldTable's foldEstimate row. It is opted out only by the
// file-only `auto-compact: false` config key. The loop calls it before it consumes new input, so a
// just-submitted user message rides the folded history as its own turn rather than being folded
// into the summary; a failed fold never corrupts history, and the Turn proceeds with the full
// conversation.
func (a *Agent) autoCompact(ctx context.Context, turn int) {
	a.foldFor(ctx, turn, foldEstimate)
}

// shouldAutoCompact reports whether the automatic Compaction trigger should fire. It fires only when
// compaction is enabled (the live `auto-compact` gate, seeded from cfg.Context.CompactionEnabled and
// on by default, swappable mid-session via SetCompactionEnabled; the on-demand /compact ignores this
// gate and always folds), at an Exchange boundary (NOT inExchange — S2) or, on an Agent that folds
// mid-Exchange (midExchangeCompaction — every child agent), at any Turn boundary,
// and when the history has outgrown its Budget History allocation
// (domain.Budget.HistoryExceedsAllocation) AND neither latch is set: compactFailed, which a fold
// that FAULTED sets for the rest of the Exchange (cleared by openExchange), and compactSat, which a
// fold that RAN and still left the history over its allocation sets. It
// clears the saturation latch the moment the estimate falls back under the allocation, so growth
// alone cannot re-trigger a fold that already proved it cannot help. An unbudgeted Agent (no window
// known, so no allocation) measures against the conservative unknown-window ceiling instead of
// standing down — historyExceedsAllocation owns that substitution and its rationale.
func (a *Agent) shouldAutoCompact() bool {
	if !a.compactionEnabled() {
		return false
	}
	// S2: auto-compaction is Exchange-boundary-only. At the top-of-step() placement inExchange is
	// false only at an Exchange opening (before pendingInput is consumed), so a mid-Exchange
	// over-budget Turn (a tool-continuation) defers the fold to the next opening — the
	// tool-result-cap Floor guard and the loop's structural floor on a single oversized result shape
	// the request mid-Exchange, and emergencyFold rescues it once the window is actually blown
	// (ADR 0018). Folding on THIS estimate mid-Exchange would leave the request ending in an
	// assistant summary; the emergency fold pays for the exception with its user bridge.
	//
	// A CHILD agent (midExchangeCompaction, set by newChildAgent alone) is the standing exception:
	// its whole life is ONE Exchange, so the boundary this guard waits for never comes and the
	// history grows unbounded until the window is blown — the delegate token runaway this lifts.
	// The placement is what makes it safe: the top of step() is a QUIESCENT Turn boundary — the
	// previous Turn's tool calls are all answered — so the fold's prefix → summary Replace strands
	// no tool result and role alternation holds. The main loop keeps the guard, so bench arms
	// comparing Reactions against Bypass are unchanged by this exception.
	if a.turns.inExchange && !a.midExchangeCompaction {
		return false
	}
	// The two stand-down latches — a fold that FAULTED this Exchange (foldFaulted, set by the
	// estimate row of foldTable; cleared by openExchange, so the main agent re-arms next Exchange) and a fold
	// that SATURATED (foldSaturated) — decide the rest against the allocation compare
	// (turnLifecycle.autoFoldArmed).
	return a.turns.autoFoldArmed(a.historyExceedsAllocation)
}

// historyExceedsAllocation reports whether the conversation's estimated token size has outgrown the
// Budget's History allocation — the raw over-budget signal both the auto-fold trigger and the
// post-fold saturation check read. It routes through the single domain compare on the SAME Budget
// view the hooks receive (domain.Budget.HistoryExceedsAllocation), so the compaction trigger and a
// hook reading the Budget can never disagree wherever an allocation exists.
//
// Where one does NOT exist — a zero History, meaning no window is known — the compare would answer
// false for a history of any size, so the boundary trigger never fired and the history grew until
// the server rejected it (audit 2026-08-01, follow-up B). The bound substituted there is the same
// conservative ceiling the emergency fold renders against (growthBounds.historyFloor, which falls
// back to compactUnknownWindowTranscriptTokens): the fold is what this trigger's fold-to-a-summary
// actually costs, so a history the fold cannot render whole is precisely the history worth folding.
// The substitution is the ENGINE's, deliberately: the Budget the Reactions see keeps its
// honest zero allocation, so nothing outside this file starts steering on a guessed window
// (the standing posture: never fire on a guess) or shows a fill against a window nobody reported.
func (a *Agent) historyExceedsAllocation() bool {
	b := a.budget()
	b.History = deriveGrowthBounds(b).historyFloor
	return b.HistoryExceedsAllocation(a.conv.Messages())
}

// promptFS carries this package's prompt text as plain files under prompts/. The prompt is an
// asset rather than a Go string literal so the wording can be read and edited as prose (the
// issue register: hard-coded prompt literals), and go:embed compiles it into the binary — the
// text ships inside the single binary, is never read from disk at runtime, and is never
// user-overridable.
//
//go:embed prompts/*.txt
var promptFS embed.FS

// mustPrompt loads one embedded prompt asset by file name. Every asset ends with exactly one
// trailing newline — a file without one is awkward in an editor and in a diff — and that one
// newline is stripped here, so the string in memory is byte-identical to the literal the asset
// replaced. CRLF endings are normalised first, the way the embedded block art is
// (internal/tui/logo.go), so a core.autocrlf checkout cannot bake \r into a prompt. A name that
// is not in the FS cannot happen in a built binary — go:embed fails the build first — so it is a
// programming error rather than a runtime condition.
func mustPrompt(name string) string {
	b, err := promptFS.ReadFile("prompts/" + name)
	if err != nil {
		panic("apogee: missing embedded prompt asset " + name + ": " + err.Error())
	}
	return strings.TrimSuffix(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
}

// overflowBridge is the user-role message appended after any fold that ran MID-EXCHANGE
// (prompts/overflow-bridge.txt) — emergencyFold's overflow-driven one on any Agent, and a child
// agent's estimate-driven one in autoCompact. Its ROLE is the load-bearing half: the fold leaves the
// conversation ending in the assistant summary, and a request whose last message is an assistant
// turn is what a strict chat template refuses (and what an instruct model reads as "keep writing
// that summary") — the user bridge closes the turn structure back to a legal …user → assistant →
// user. Its TEXT is the other half: the model is told, in-band, that the history it can see is a
// summary of a conversation that outgrew the window, so it resumes the task instead of re-asking
// for context it will never get back.
var overflowBridge = mustPrompt("overflow-bridge.txt")

// summaryTruncatedMarker is appended to a summary the server cut off at compactMaxTokens
// (prompts/summary-truncated.txt), separated by a blank line, so it rides INSIDE the summary
// message the fold writes — after context.Compact's own summaryMessagePrefix — and is therefore
// read by every later request the way the rest of the summary is. What it buys is the model's
// trust calibration: a cut summary ends mid-thought, and its missing tail is exactly the part a
// summary front-loads least — the most recent state and what was about to happen next — so a model
// reading it without the marker takes a truncated history for a complete one and resumes from the
// wrong place. The marker names the cut and points at the remedy that actually works from inside a
// folded conversation (re-derive the state with tools), rather than asking for context no later
// turn can hand back.
var summaryTruncatedMarker = mustPrompt("summary-truncated.txt")

// emergencyFold folds the conversation so an overflowed request can be retried against a history
// that fits — foldTable's foldOverflow row — reporting whether the caller may retry (true ⇒ the
// conversation WAS folded; false ⇒ nothing changed and the Turn must give up exactly as it does
// today). On success the conversation ends …first-user | assistant-summary | user-bridge: strict
// role alternation holds and no dangling tool calls survive the Replace, so any chat template
// accepts the retried request. A skip means there is nothing left to shed, so recovery is
// impossible; a cancel returns false SILENTLY (the caller's own ctx check routes the Turn to the
// cancel path); any other fault emits its one ErrorEvent and returns false with the conversation
// untouched. refold (loop.go) reads foldFor's result directly; this wrapper serves the tests that
// drive the overflow row on its own.
func (a *Agent) emergencyFold(ctx context.Context, turn int) bool {
	return a.foldFor(ctx, turn, foldOverflow).end == foldEndFolded
}

// compactTranscriptChars returns the character budget for the rendered transcript the summary
// call carries, derived from the discovered context window so the call itself cannot overflow at
// exactly the high fill /compact exists to relieve (post-v1 remediation item 6). The window (in
// tokens) minus the response reserve (compactMaxTokens) minus prompt overhead is the transcript's
// token budget, converted to characters via the budget's chars→token estimate.
//
// With an UNKNOWN window (neither discovery nor `context-window:` reported one) the budget
// (growthBounds.transcriptBudget) falls back to compactUnknownWindowTranscriptTokens through the
// same ratio rather than returning 0. A zero budget means "render the whole conversation" to the
// reducer, which is precisely what overflows the emergency fold's own summary call on the long
// session that needed the fold — the give-up then repeats for every message and the session
// wedges (audit 2026-08-01). The result is always positive, so the summary call is bounded on
// every path. It is read live, at the fold: a SwitchUpstream between Turns changes what the next
// /compact renders against.
func (a *Agent) compactTranscriptChars() int {
	b := a.budget()
	return int(float64(deriveGrowthBounds(b).transcriptBudget) * b.CharsPerToken)
}

// cappedSummaryErrFmt is the head of the fault text for a summary call that came back with no
// visible text after running into compactMaxTokens — the 2026-08-29 incident's shape, and the one
// blank reply context.Compact's errEmptySummary would misdescribe: the model DID answer, at length,
// and spent the entire cap on a reasoning pass. So the message names the cap, the reasoning spent
// under it when the reply carried any, and then one of the two causes below, because those are what
// an operator acts on: another server, or a profile whose template honours the off rung. It
// deliberately does not invite a retry (ADR 0046); the same fold meets the same cap. The cap it
// names is the one the summariser request actually carried — Complete formats it with the maxTok
// it set on the request, never the bare constant, so the number the reader sees is the number the
// server was sent.
//
// The cause is chosen on what THIS request actually asked for — its own ThinkingEffort — and never
// on the dialect. compactCompleter's EffortOff override fires on two dialects only, but a session
// whose profile pins `effort: off` carries EffortOff under any of them (internal/agent/wire.go's
// resolvedEffort), and telling that session it never asked would be a lie. The engine cannot
// inspect the server either, so neither cause diagnoses one: the asked half reports what was asked
// and what came back, and stops short of a verdict on a template it has never seen.
const cappedSummaryErrFmt = "compaction summary hit its output cap (%d tokens) with no visible text " +
	"to show for it%s — %s"

// cappedSummaryAskedOffCause is the cause for a summary request that carried the off rung: the
// intent went out and the server reasoned regardless. Why it did — a template that drops the key, a
// model that cannot stop thinking — is beyond the engine, so the text says only what happened.
const cappedSummaryAskedOffCause = "the summarizer asked for no reasoning and this server reasoned anyway"

// cappedSummaryNotAskedCause is the cause for a summary request that carried no off rung at all —
// the three dialects compactCompleter leaves alone, with nothing pinning `effort: off`. Nothing was
// ignored there: the cap simply went on a reasoning pass nobody suppressed, and the remedy is to ask
// (a profile `effort: off`, or a server whose dialect apogee can switch off).
const cappedSummaryNotAskedCause = "the cap went on a reasoning pass this server was never asked to skip"

// compactCompleter adapts the Agent's provider seam to context.Completer: a single upstream
// completion that is silent in the transcript but NOT unaccounted. Unlike streamResponse it emits
// no TokenEvent — compaction is a maintenance call, not a Turn, so it must not stream into the
// transcript. It does emit one UsageEvent flagged Maintenance: the summary call spends real tokens
// that session totals must include, while its prompt/completion counts describe the SUMMARIZER's
// request rather than the conversation's fill, so a reader of the live gauge or the tokens/sec
// clock skips the flagged event (the gauge re-measures on the next real Turn's usage) and a reader
// of the cumulative totals accepts it — the contract on domain.UsageEvent. It reuses the loop's
// request projection (toProviderRequest) and the loop's Delta collector (collectCompletion, with
// no observer); a cancelled ctx or a terminal stream fault surfaces as an error, so the reducer
// leaves the conversation untouched and — having no completed call to account for — emits nothing.
// One fault is re-streamed before it surfaces: a TRANSIENT one (Delta.Retryable), re-sent once
// after the Turn's own hold-off, so a momentary 502 during a summary no longer fails the fold.
//
// The summary call asks for NO reasoning, whatever the session's effort resolves to. Compaction is
// maintenance, not a Turn: the summarizer does a mechanical job under a bounded output cap
// (compactMaxTokens), and a thinking model that spends that whole cap on a reasoning pass comes
// back with finish reason "length" and empty content — so the fold faults, and on a child agent
// (which folds at every quiescent Turn boundary) it faults again at the next one, forever. That is
// not hypothetical: on 2026-08-29 a delegate on Qwen3.8-27B looped for ~9 hours on an empty
// summary, one ~40-minute summary call per Turn boundary. So the projection's resolved effort
// (override ▸ profile ▸ nothing) is overridden to provider.EffortOff here, exactly as the naming
// call does (internal/title). Like that one it states an INTENT, not a guarantee: the intent lands
// only on a server whose chat template honours it.
//
// The override is gated on the SERVER's wire dialect (a.effortDialect — the dialect is the
// server's, ADR 0060) being one whose "off" rung actually means off: llama.cpp's
// chat_template_kwargs (what the incident server is detected as, through the /props probe) and
// OpenRouter's reasoning object. The other three keep resolvedEffort exactly as today — on
// EffortDialectNone because a caller that asks for nothing must change nothing on the wire
// (ADR 0050), on EffortDialectOpenAI because "off" is a documented FLOOR there rather than an off
// switch (it lands as `minimal`), and on EffortDialectOff because nothing effort-shaped reaches
// the wire at all. The dialect itself is never touched here.
//
// delegateFold marks the completer the ENGINE FOLD of a capped delegate runs on (Agent.foldForParent):
// the same call in every respect — model, budget, dialect, sampling, the Maintenance accounting —
// except that its UsageEvent also carries domain.UsageEvent.DelegateFold, because that fold
// replaces nothing in the conversation and a Driver must not trace it as a Compaction.
type compactCompleter struct {
	a            *Agent
	delegateFold bool
}

func (c compactCompleter) Complete(ctx context.Context, msgs []domain.Message) (string, error) {
	// The summarizer request runs no Reactions, so nothing fires against it.
	req := domain.NewRequest(c.a.cfg.Model, msgs, nil, c.a.budget(), c.a.turns.index)
	temp, maxTok := compactTemperature, compactMaxTokens
	req.SetSampling(domain.SamplingParams{Temperature: &temp, MaxTokens: &maxTok})

	// No reasoning pass for the summarizer, on the dialects whose "off" rung means off — this
	// type's doc carries the incident and why the other three keep the session's resolved effort.
	preq := c.a.toProviderRequest(req)
	if c.a.effortDialect == provider.EffortDialectKwargs || c.a.effortDialect == provider.EffortDialectReasoning {
		preq.ThinkingEffort = provider.EffortOff
	}
	// What the request itself ended up asking for decides which cause the capped-summary fault
	// names — read here, off the request that actually goes out, so a session that pinned
	// `effort: off` in its profile counts as having asked even on a dialect the override skips.
	askedForNoReasoning := preq.ThinkingEffort == provider.EffortOff

	// One summary stream, re-streamed ONCE on a TRANSIENT fault (Delta.Retryable — an in-band 502
	// an aggregator wrapped in an HTTP 200, a body cut mid-stream), after the same hold-off the Turn's
	// re-stream waits (holdOffRestream). Silently: nothing streamed into the transcript, so there is
	// no StreamResetEvent to emit, and a fold that recovers is a fold like any other. Before this
	// re-stream a momentary 502 during a summary faulted the fold and latched compactFailed for the
	// rest of the Exchange (foldFaulted) — a stand-down meant for a history that cannot shrink, not
	// for a blip. The second fault, of any class, surfaces as every fault always did.
	var summary completion
	for restreamed := false; ; {
		summary = c.a.collectCompletion(ctx, preq, nil)
		if ctx.Err() != nil {
			return "", ctx.Err() // a cancel masquerades as a stream error; ctx wins (as in respondAndReview)
		}
		if !summary.failed {
			break
		}
		if summary.retryable && !restreamed {
			restreamed = true
			if holdOffRestream(ctx) {
				continue
			}
			// The wait ended on a cancel, not the clock: route it as the cancel it is, never as the
			// fault it was waiting to retry.
			return "", ctx.Err()
		}
		return "", errors.New(summary.errMsg)
	}

	// Account for the completed summary call on the OWNING Agent's tally — the same tally its Turns
	// feed, so /usage stays accurate straight after a fold instead of silently losing the tokens the
	// fold spent. Emitting only here (past the cancel and fault exits) keeps the tally honest about
	// what actually completed, and unlike streamResponse this call does NOT calibrate the chars→token
	// estimator: the summarizer prompt is a rendered transcript, not the conversation the estimator
	// models.
	// The summary arrives assembled the way a Turn's reply is (collectCompletion): the inline thinking
	// a delimited profile emits is lifted out of the content, so no <think> span can ride into the
	// summary message and from there back into the folded conversation, and what is lifted joins the
	// Upstream-split channel for the spend the fault below reports.
	visible, thinking := summary.content, summary.thinking

	if usage := summary.usage; usage != nil {
		event := c.a.usage.record(
			c.a.base(c.a.turns.index), c.a.cfg.Model, summary.served, c.a.cfg.Context.MaxContextTokens,
			usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens, usage.CachedPromptTokens,
		)
		event.Maintenance = true
		event.DelegateFold = c.delegateFold
		c.a.cfg.Events.Emit(event)
	}

	// A reasoning-only reply that ran into compactMaxTokens is not "an empty summary": the call
	// completed, spent the whole cap, and produced nothing visible — so it faults here, naming both
	// numbers and the cause the request itself supports, instead of reaching the reducer's
	// errEmptySummary, which describes a model that produced nothing at all. Every OTHER blank reply
	// still returns "" and keeps that error verbatim. The reasoning spend is an estimate through the
	// chars→token estimator (the stream carries the text, never the server's count of it), hence
	// "roughly" — as in emptyReplyFault.
	if strings.TrimSpace(visible) == "" && summary.finish == domain.FinishLength {
		spent := ""
		if thinking != "" {
			spent = fmt.Sprintf(", after roughly %d tokens of reasoning", c.a.tokens.EstimateTokens(len(thinking)))
		}
		cause := cappedSummaryNotAskedCause
		if askedForNoReasoning {
			cause = cappedSummaryAskedOffCause
		}
		return "", fmt.Errorf(cappedSummaryErrFmt, maxTok, spent, cause)
	}

	// A summary that DID say something before the cap cut it off is kept and marked, not faulted:
	// see the compactMaxTokens comment for why keeping beats discarding here and how that squares
	// with ADR 0046. The marker rides inside the summary message, so context.Compact folds exactly
	// as it does for a complete summary.
	if summary.finish == domain.FinishLength {
		return visible + "\n\n" + summaryTruncatedMarker, nil
	}
	return visible, nil
}
