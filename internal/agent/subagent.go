package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/tools"
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

// maxSubAgentDepth bounds sub-agent recursion so a model cannot spawn an unbounded tower of
// sub-agents (each level costs a full nested loop). The top-level agent is depth 0; a depth-0
// agent may spawn a depth-1 sub-agent and a depth-1 may spawn a depth-2, but a depth-2
// sub-agent is the deepest: at maxSubAgentDepth the sub_agent tool is withheld from the
// nested tool set AND the recursion point refuses defensively, so the bound holds even if the
// menu is bypassed. Three levels is ample for real delegation while making a runaway tower
// structurally impossible.
const maxSubAgentDepth = 2

// stepCapResultFormat is the marker line the PARENT model receives when a delegation ended at its
// step cap (Agent.stepCap): a NON-error result whose first line says the answer that follows is
// partial, so the parent can re-delegate a narrower task instead of treating a half-finished
// investigation as the finding. It is a package constant, pinned by test, because the parent model
// reads it as the contract for what the rest of the result is. %d is the cap actually applied.
const stepCapResultFormat = "[delegate stopped at its step cap (%d steps); partial result — its last visible text follows]"

// subAgentFaultPrefix opens the error result a FAULTED delegation becomes. What follows it is the
// child's own fault sentence (Agent.lastFault) — the same line the human read at Depth+1 — so the
// parent model reads the cause in the result itself instead of being sent to an error it cannot see.
const subAgentFaultPrefix = "sub-agent faulted before finishing the delegated task: "

// subAgentFaultNoCause is the tail used when the child's Exchange was abandoned without surfacing
// a fault of its own (a recovered extension panic, a pre-request hook that refused): there is no
// cause to name, so the result says so and points at the transcript, which is what this message
// said in full before causes travelled.
const subAgentFaultNoCause = "its exchange was abandoned (see the preceding error), so no result was produced"

// stepCapNoTextMarker stands in for the child's last visible text when it produced none — a
// delegate that spent every capped Turn calling tools and never wrote a word. The marker keeps
// the result intelligible: the parent is told the delegation was stopped AND that it has nothing
// to show, rather than being handed a bare marker line with an empty body.
const stepCapNoTextMarker = "(no visible text)"

// isSubAgentCall reports whether call targets the sub_agent recursion point — the signal
// resolveAndExecute drives a nested Agent for the call rather than executing a leaf tool.
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
	first, _, _ := strings.Cut(raw, "\n")
	return strings.TrimSpace(first)
}

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
func (a *Agent) runSubAgent(ctx context.Context, call domain.ToolCall) (domain.ToolResult, dispatchOutcome) {
	if a.depth >= maxSubAgentDepth {
		// Defensive floor: the tool is withheld from the menu at the bound, but refuse here
		// too so the bound holds even if a model emits the call anyway.
		return errorToolResult(call.ID, fmt.Sprintf(
			"sub-agent depth limit reached (max %d): cannot spawn a deeper sub-agent", maxSubAgentDepth)), dispatchDone
	}

	var args tools.SubAgentArgs
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return errorToolResult(call.ID, "invalid sub_agent arguments: "+err.Error()), dispatchDone
	}
	if args.Task == "" {
		return errorToolResult(call.ID, "sub_agent requires a non-empty task"), dispatchDone
	}

	sub, err := a.newChildAgent(call.ID, args.Task, delegationName(args.Name))
	if err != nil {
		return errorToolResult(call.ID, "could not construct sub-agent: "+err.Error()), dispatchDone
	}
	// The call's optional max_steps can only ever LOWER the configured cap: a model may say "this
	// one is small, stop it sooner", never "let me run longer than the host allows". Both values
	// must be positive for the request to bite — a request against an UNBOUNDED cap (0, the key
	// switched off) is ignored too, because the host turning the bound off is a deliberate posture
	// the model does not get to reinstate per call.
	if args.MaxSteps > 0 && sub.stepCap > 0 && args.MaxSteps < sub.stepCap {
		sub.stepCap = args.MaxSteps
	}
	// The delegation is the child's whole life, so this scope is the only one that knows when the
	// child's resources stop being needed — nothing else holds the child to close it later. Close
	// reaps the Consoles this delegation opened (ADR 0059 §6), routed or not, and tears down a
	// ROUTED child's own client; an unrouted child borrowed the parent's client, so that one is
	// left running for the parent (ownsUpstream).
	defer func() { _ = sub.Close() }()

	if err := sub.Submit(domain.UserInput{Text: args.Task}); err != nil {
		return errorToolResult(call.ID, "could not start sub-agent: "+err.Error()), dispatchDone
	}
	res, err := sub.Run(ctx)
	if err != nil {
		// Run returns a Go error only for a loop-level fault the nested Agent could not
		// localise — surface it as an error result to the parent model rather than failing
		// the parent Turn.
		return errorToolResult(call.ID, "sub-agent failed: "+err.Error()), dispatchDone
	}
	if res.Status == domain.StatusCancelled {
		// The cancel reached the nested loop's boundary and it returned resumably; the parent
		// Turn must now roll back wholesale (D2: the recovery point is the pre-sub_agent
		// boundary — the sub-agent's progress is discarded, no partial result surfaced).
		return domain.ToolResult{}, dispatchCancelled
	}
	if res.Faulted {
		// The nested Exchange was ABANDONED, not completed — an Upstream fault, a recovered
		// extension panic, or an overflow the child's one fold could not rescue. It closes on
		// StatusExchangeComplete exactly as a real completion does, so the fault marker is the
		// only thing that tells them apart, and reporting it as a success would hand the parent
		// model a placeholder — or, worse, stale mid-task text from an earlier child Turn
		// (finalMessageText scans backwards for the last assistant message) — as the delegated
		// result. An error result also books the delegation as HARMFUL rather than as a
		// productive write for self-regulation (noteToolProductivity, R3), so a failure can no
		// longer clear the parent's strikes and Turn Budget. The child's own ErrorEvent already
		// reached the shared EventSink at Depth+1, so the human sees the cause — and the cause now
		// rides the RESULT too, because "see the preceding error" addresses a reader the parent
		// MODEL is not: it reads one tool result and has no transcript to look back through.
		cause := sub.lastFault
		if cause == "" {
			cause = subAgentFaultNoCause
		}
		return errorToolResult(call.ID, subAgentFaultPrefix+cause), dispatchDone
	}

	if res.StepCapped {
		// The engine STOPPED the child at its step cap (Agent.Run) — it was still asking for tools,
		// so what it has is partial. That is not a failure and must not be reported as one: an error
		// result would throw away Turns of real work AND book the delegation as harmful for
		// self-regulation (noteToolProductivity, R3). So the parent gets a NON-error result whose
		// first line is the marker saying the answer below is partial, followed by whatever the
		// child last said out loud. The child's own ErrorEvent already told the human the cap hit.
		text := sub.lastVisibleText()
		if text == "" {
			text = stepCapNoTextMarker
		}
		return domain.ToolResult{
			CallID:  call.ID,
			Content: fmt.Sprintf(stepCapResultFormat, sub.stepCap) + "\n" + text,
			IsError: false,
		}, dispatchDone
	}

	return domain.ToolResult{CallID: call.ID, Content: sub.finalMessageText(), IsError: false}, dispatchDone
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
// (defaultSubAgentTools — never an expansion, and withholding sub_agent at the depth bound),
// the SAME EventSink, the parent session's context-file content
// verbatim (copied, never re-read — a sub-agent is not a session boundary), and Depth =
// parent+1 so its events nest. The
// nested Agent is NOT given the parent's pending input or conversation — it starts fresh with only
// the delegated task (the ADR-0008 statelessness boundary). The allow-for-session approval memory is
// deliberately NOT on that withheld list: it is scoped to the SESSION rather than to an Agent, and
// it reaches the child through the very Approver threaded above — the shared queueing seam holds it
// (approvalCache in approvalcache.go), so a gate the human already cleared anywhere in the tree does
// not ask the child again, and an allow the child earns outlives it for the parent and its siblings.
//
// The Upstream responder used to be on that inherited list too — this doc said the child gets "the
// SAME Upstream responder and EventSink" — and ADR 0045 reverses exactly that clause for the
// Upstream half: when a Delegation target is LATCHED the spawn is ROUTED, and the child dials the
// Sub-agent server on a provider client of its own, against that server's model, context window
// (the parent's still, when the target names none) and model profile, with the Bypass and Mechanism
// posture the flagged entry carries. With NO target
// latched — nothing flagged, the server unreachable, no model bound there — the child takes the
// parent's Upstream verbatim, which is what every delegation did before routing existed, so the
// fallback is not a degraded mode but the original one (ADR 0045 §4). Routing never widens
// privilege: the Mode, Approver, Confiner, blast radius and tool bounds above are the parent's
// whichever server answers, and only the two POSTURE keys ADR 0045 §2 puts on the flagged entry —
// Bypass and the Mechanism catalogue, neither of which gates a tool — may differ, and only because
// the host was configured to say so.
//
// spawnCallID is the id of the sub_agent tool call being served — the child's RUN IDENTITY,
// stamped on every Event it emits (domain.EventBase.CallID). It is what tells one delegated
// stream from another once siblings share a depth (ADR 0039), so it is threaded at
// construction rather than at each emission: the child's own tools, Mechanisms and nested
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
func (a *Agent) newChildAgent(spawnCallID, task, name string) (*Agent, error) {
	childCfg := a.cfg
	childCfg.Mode = a.Mode() // inherit the parent's LIVE mode at spawn (Shift+Tab may have changed it),
	//                          read under the lock since this runs on the worker goroutine during dispatch
	childCfg.ConfineToWorkspace = a.ConfineToWorkspace() // likewise the parent's LIVE blast radius at spawn
	//                                                     (/confine may have moved it since construction)
	childCfg.ScratchDir = a.ScratchDir() // and the parent's LIVE session scratch dir — a session
	//                                      boundary may have moved it (SetScratchDir) since construction
	childCfg.Bypass = a.bypassEnabled()                        // and the parent's LIVE Bypass + auto-Compaction gates, which the
	childCfg.Context.CompactionEnabled = a.compactionEnabled() // settings surface may have swapped since construction
	// The context-file NAMES are deliberately NOT re-read from the live list: the child copies the
	// parent's context-file CONTENT verbatim below, because a sub-agent is not a session boundary.
	childCfg.Tools = a.defaultSubAgentTools()
	// The sub-agent inherits the parent's ALREADY-BUILT catalogue (a.registry — the parent's
	// Config.Mechanisms merged with whatever Config.EnableMechanisms armed), so it fires the same
	// catalogued + experimental Mechanisms; an explicit per-sub-agent catalogue is a later refinement
	// (ADR 0013 leaves the default = the parent's). It inherits it through ForSubAgent rather than by
	// pointer: siblings in a depth-0 fan-out run AT ONCE (ADR 0039), so the child gets a registry of
	// its own — and a hook carrying live state gets a per-child instance, the same isolate-the-live-
	// state / share-the-read-only-floor answer Guards.ForSubAgent gives one line down. A hook with
	// nothing live to isolate is still inherited verbatim, so the delegation is unchanged for every
	// Mechanism in today's catalogue. EnableMechanisms is cleared because those IDs are already built
	// into the inherited registry — re-building them into it would trip the already-registered
	// rejection and fail every sub-agent spawn.
	childCfg.Mechanisms = a.registry.ForSubAgent()
	childCfg.EnableMechanisms = nil

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
	// The two POSTURE keys follow ADR 0045 §2's replace-or-inherit rule, and both are already
	// seeded with the inherited value above: a PRESENT key replaces it WHOLE (no per-ID merge, no
	// OR-ing of flags), an ABSENT one leaves the parent's live value standing. Mechanisms arrives as
	// a FACTORY rather than a registry for the reason ForSubAgent exists one line up: siblings in a
	// depth-0 fan-out run at once (ADR 0039), so each child needs a registry of its own and the
	// factory is called once per child.
	//
	// The client is built rather than mutated — provider.Client.SetModel rebinds the model and
	// deliberately never the endpoint — so the child's wire target moves atomically with its key,
	// the same idiom SwitchUpstream takes for the session (rebind.go). The parent's own responder is
	// untouched: routing changes what a SPAWN builds, never what the session speaks to.
	// tap is the Inspector's capture seam for a client this spawn BUILDS (below). An unrouted spawn
	// builds none — it speaks over the parent's connection, whose tap is already bound to the
	// parent — so tap stays nil there and binding it is a no-op (wireTap).
	var tap *wireTap
	upstream := a.upstream
	// ownsUpstream travels with the client below: a routed spawn DIALS one and hands the child the
	// right to tear it down, an unrouted spawn borrows the session's and hands over nothing.
	ownsUpstream := false
	if target := a.delegationTarget(); target != nil {
		childCfg.Endpoint = target.Endpoint
		childCfg.APIKey = target.APIKey
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
		if target.Bypass != nil {
			childCfg.Bypass = *target.Bypass
		}
		if target.Mechanisms != nil {
			childCfg.Mechanisms = target.Mechanisms()
		}
		var opts []provider.Option
		opts, tap = armWireCapture(childCfg)
		upstream = provider.NewClient(target.Endpoint, target.Model,
			append(opts, provider.WithAPIKey(target.APIKey))...)
		ownsUpstream = true
	}

	child, err := newAgent(childCfg, upstream)
	if err != nil {
		return nil, err
	}
	// A routed child closes the client it dialled; an unrouted one must never close the session's
	// out from under the parent that is still speaking over it (Agent.Close).
	child.ownsUpstream = ownsUpstream
	// The wire shape an effort intent is expressed in, taken from the parent's LIVE field rather
	// than from the childCfg copy newAgent just seeded it from. Rebind commits a newly observed
	// dialect onto the Agent alone (rebind.go) — a.cfg.EffortDialect keeps the value construction
	// read — so a session rebound since startup would otherwise spawn children speaking the shape
	// of the server it left, and every effort-gated decision downstream (the compaction
	// summarizer's EffortOff, compact.go) would read the wrong server. Routing does not change the
	// answer: ADR 0045 replaces the dial facts and the two posture keys, and a dialect is neither,
	// so a routed child speaks its parent's until a Rebind of its own observes the target's —
	// which is what an unrouted child got from the Config before this line existed.
	child.effortDialect = a.effortDialect
	// The child's token estimator needs no reset for a routed spawn — the reason SwitchUpstream and
	// Rebind reset theirs (a chars→token calibration that described the departed model) cannot
	// arise here: newAgent seeds every child with a fresh apogeectx.NewTokenEstimator, and
	// newChildAgent never copies the parent's, so a routed child starts uncalibrated by
	// construction.
	child.depth = a.depth + 1
	// The delegate step cap, seeded HERE and only here: a top-level Agent stays at 0 (uncapped)
	// however the key is set, because the bound is on delegates alone. It rides childCfg, which is
	// the parent's whole Config, so a ROUTED spawn takes the same cap as an unrouted one — the key
	// is top-level, not per-server (ADR 0045 replaces only the dial facts and the two posture
	// keys) — and a grandchild inherits it the same way. runSubAgent may lower it for this one
	// delegation from the spawning call's max_steps.
	child.stepCap = childCfg.Delegation.MaxSteps
	// And the child's other structural bound on runaway context: it folds under budget pressure at
	// quiescent TURN boundaries, not only at Exchange boundaries (shouldAutoCompact's S2 guard). A
	// delegation is ONE Exchange from its first Turn to its report, so the boundary the main loop's
	// trigger waits for never arrives for a child — without this its history simply grows until the
	// window is blown. Set on EVERY child, routed or not: it is the child's contract, not a
	// Mechanism and not a per-server posture, so there is no key to disagree about.
	child.midExchangeCompaction = true
	child.callID = spawnCallID
	// The Console privilege key, minted by the registry that compares it rather than taken from
	// the spawning call's id: that id is the model's to choose, and a text-format parser numbering
	// calls per Turn can hand two siblings the same one — a collision that would let one sibling's
	// end reap the other's shells (ADR 0059 §6). A nil registry mints "", which is the top-level
	// key; that is harmless on a child, because an engine with no registry holds no Console.
	child.consoleOwner = a.consoles.MintOwner()
	child.task = task
	child.name = name
	// Bind the routed spawn's own capture seam AFTER its identity is stamped, so its WireEvents
	// carry the child's depth and spawning call id rather than the zero values newAgent left.
	tap.bind(child)
	child.guards = a.guards.ForSubAgent()
	// The child belongs to the PARENT's session, so it speaks from the parent's context-file
	// bytes: copy the cache over the one its own construction just read. A sub-agent is not a
	// session boundary, so an AGENTS.md edited (or deleted) mid-delegation must not reach it.
	child.contextFiles = a.contextFiles
	// A tighten-only view of the parent's EFFECTIVE mode (ADR 0013): the child's disposition takes
	// TighterMode(parentEffective, spawnMode), so a parent tightening mid-delegation reaches the
	// child while a parent loosening cannot loosen it. It is the parent's effectiveMode accessor —
	// not its own Mode — so the view COMPOSES down the chain: a depth-2 grandchild reads its
	// parent's effective mode, which already folds in the top-level agent's live mode, and a
	// top-level tightening therefore reaches every descendant rather than stopping at depth 1.
	// Capturing an accessor (not the raw field/mutex) keeps every read modeMu-guarded at the agent
	// that owns the field, so the child reads race-free but has no seam to mutate anything.
	child.liveMode = a.effectiveMode
	// The Delegation-target latch is shared by HANDLE, not copied (ADR 0045): the child holds the
	// parent's holder, so a target the host pushes after this spawn is the one the child's OWN
	// delegations read, and routing reaches every depth from the one place a host pushes to. A
	// snapshot instead would freeze depth≥1 spawns on whatever was current when their parent was
	// built — and "identity once there" (a routed child's delegations go to the same server) is
	// exactly what one shared latch gives for free.
	child.delegation = a.delegation
	// The undo journal is shared by HANDLE too, and for a reason of its own (ADR 0051, ratified
	// call 8): a delegation is work the human asked for in the CURRENT Exchange, so the files a
	// child writes are files that Exchange changed and belong in its undo step. Handing the child
	// its own journal — which newAgent just built — would strand its writes in a journal nothing
	// can reach, and `/undo` would silently leave delegated work in place. The journal is
	// mutex-guarded, which is what makes one instance safe for a fan-out of siblings writing at
	// once (ADR 0039); the child never opens a GROUP of its own (loop.go), so its records join
	// the parent's current one however deep the delegation nests.
	child.journal = a.journal
	// The console registry is shared by HANDLE for the same structural reason and one of its own
	// (ADR 0059 §6): the Consoles are the ENGINE's live processes, not a per-Agent resource, so
	// one registry per engine is what makes the cap of four mean four across the whole tree rather
	// than four per delegation. Ownership is not lost by the sharing — a Console records the
	// engine-minted owner key of the run that opened it (dispatch stamps it from the ctx), so the
	// child's Close reaps exactly the Consoles this delegation opened and leaves the parent's
	// untouched.
	child.consoles = a.consoles
	return child, nil
}

// defaultSubAgentTools returns the tool registry a sub-agent is constructed with: the parent's
// full tool set by default (ADR 0005 — the caller may narrow per task; the default is the
// parent's set), MINUS the sub_agent recursion point itself when spawning the child would put
// it AT the depth bound (a depth-(max) sub-agent is never offered sub_agent, so it cannot
// recurse further). A nil parent registry yields nil (a tool-less sub-agent — the parent had
// no tools to delegate).
//
// The result is always ≤ the parent's tools: it is built from the parent registry's own names
// via Subset, so it can never name a tool the parent lacks (a privilege expansion is
// structurally impossible — ADR 0005).
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
		if t.Name() == tools.SubAgentToolName && childDepth >= maxSubAgentDepth {
			continue
		}
		names = append(names, t.Name())
	}
	return a.tools.Subset(names...)
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
