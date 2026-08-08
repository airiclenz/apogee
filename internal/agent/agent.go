package agent

import (
	"context"
	"slices"
	"sync"
	"time"

	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/processing"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/security"
)

// ----------------------------------------------------------------------------
// Construction & lifecycle (ADR 0001)
// ----------------------------------------------------------------------------

// Agent is a single embeddable Apogee agent instance. It owns the loop,
// conversation state, tool dispatch, and Mechanism application. It holds no
// process-global state: every state root is injected through Config, so many
// Agents can run in one process against isolated directories (the property the
// bench relies on for isolation — ADR 0001). The root apogee package re-exports it
// as an alias (type Agent = agent.Agent); its methods are the public surface.
//
// An Agent is not safe for concurrent use by multiple goroutines; drive one Agent
// from one goroutine (Step/Run), and observe it from another only via its EventSink.
// The methods touching loop state fall into three call classes. Idle-only calls (Submit,
// ClearContext, RestoreSession, Compact, Rebind, SwapTools, SetProfile, AbortExchange) need a
// quiescent boundary with no Exchange mid-flight. Between-Steps calls by the goroutine DRIVING the loop
// (Snapshot, Interject) are additionally valid at the boundary between two Steps of an open
// Exchange: that goroutine owns the conversation there, so the boundary itself is the
// synchronization — no lock, and no other goroutine may make the call (ADR 0025). The
// anytime-goroutine-safe class — SetMode, SetConfineToWorkspace, SetBypass,
// SetCompactionEnabled, SetContextFiles and SetParallelAgents — is the exception: each swaps ONE live field
// behind its own mutex, so the host (the settings surface, Shift+Tab, /confine) may call it
// while a Step runs and the change lands at that field's next consumption boundary.
type Agent struct {
	cfg      domain.Config
	upstream provider.Responder        // provider seam (Decision C): fake in tests, real HTTP via New
	registry *domain.MechanismRegistry // catalogued + experimental hooks driving the loop
	tools    *domain.ToolRegistry      // resolved tool set (Config.Tools, or the default registry)
	guards   security.Guards           // always-on, mode-independent guardrails (dangerous-action + circuit-breaker + audit, D6)

	// textParser and stripper are the parse-seam collaborators selected from cfg.Profile at
	// construction (processing.ParserFor): the text-format tool-call parser recovers a call from
	// the visible content of a non-native model, and stripper lifts the inline thinking/harmony
	// channel out of that content. A native, no-inline-thinking profile (the zero value) yields
	// no-op parsers, so the content path is byte-identical to the pre-profile loop. SetProfile
	// re-selects both from a new profile at an idle boundary (the settings surface's
	// `model-profile` edit) — the one door for that, since Rebind deliberately leaves the profile
	// alone; cfg.Profile moves with them, so the emit half (toolInstructions) follows.
	textParser processing.ToolCallParser
	stripper   processing.ContentStripper

	// modeMu guards mode — one of the two fields shared across goroutines (confineToWorkspace
	// below is the other), the deliberate exception to the single-goroutine contract above. The
	// UI cycles the autonomy mode (Shift+Tab → SetMode) while the worker goroutine reads it
	// during dispatch (Mode() in toolMenu / the Resolution); the RWMutex makes that overlap
	// race-free. cfg.Mode stays the immutable construction seed.
	modeMu sync.RWMutex
	mode   domain.Mode // live autonomy mode; seeded from cfg.Mode at construction, swappable via SetMode

	// confineMu guards confineToWorkspace, the second live field the UI may swap while the
	// worker drives a Step (/confine off|on → SetConfineToWorkspace). It is a SIBLING of modeMu,
	// not the same lock, because the two settings are independent: the Resolution reads them as
	// two separate facts (resolutionInput), never as one pair that must be consistent, and each
	// mutex here stays named for the single field it guards. cfg.ConfineToWorkspace stays the
	// immutable construction seed.
	confineMu          sync.RWMutex
	confineToWorkspace bool // live confine-to-workspace flag; seeded from cfg.ConfineToWorkspace, swappable via SetConfineToWorkspace

	// bypassMu, compactionMu and contextFilesMu guard the three settings the settings surface may
	// swap mid-session (SetBypass / SetCompactionEnabled / SetContextFiles). They follow the modeMu
	// pattern to the letter — one mutex per field, named for the single field it guards, because the
	// three are independent facts read at three different boundaries and never as one consistent
	// tuple. Their cfg counterparts (cfg.Bypass, cfg.Context.CompactionEnabled, cfg.ContextFiles)
	// stay the immutable construction seeds, so the whole-struct cfg copy a sub-agent spawn takes
	// (newChildAgent) keeps reading fields nothing ever writes.
	bypassMu sync.RWMutex
	bypass   bool // live Bypass flag; seeded from cfg.Bypass, swappable via SetBypass

	compactionMu sync.RWMutex
	compaction   bool // live auto-Compaction gate; seeded from cfg.Context.CompactionEnabled, swappable via SetCompactionEnabled

	contextFilesMu   sync.RWMutex
	contextFileNames []string // live workspace context-file names; seeded from cfg.ContextFiles, swappable via SetContextFiles

	// parallelAgentsMu guards parallelAgents, a fifth field of the same class and for the same
	// reason: the Parallel agents cap is a property of the SERVER this session is bound to (ADR
	// 0039), and a `/server` switch or a heartbeat that observes another slot count moves it from
	// the host's goroutine while a Step may be running. cfg.ParallelAgents stays the immutable
	// construction seed.
	parallelAgentsMu sync.RWMutex
	parallelAgents   int // live depth-0 fan-out width; seeded from cfg.ParallelAgents, swappable via SetParallelAgents

	// liveMode, when non-nil, is a sub-agent's read-only view of its PARENT's live mode: the
	// parent's effectiveMode accessor, captured at spawn (ADR 0013). The per-call
	// Resolution takes the TIGHTER of this and the child's own spawn mode, so a parent that
	// tightens mid-delegation (Shift+Tab down from Auto to Plan) gates/refuses the still-running
	// child's next call, while a parent loosening can never loosen it. Because the captured
	// accessor is the parent's EFFECTIVE mode, the view COMPOSES transitively: at depth 2 it
	// already folds in the top-level agent's live mode, so a tightening reaches every descendant
	// and no child's privileges can exceed its parent's. It is a closure over the
	// accessor — NOT the shared mode field/mutex — so the child observes the parent's mode
	// race-free but cannot mutate it. nil for a top-level Agent, which then behaves exactly as
	// before (its own mode governs).
	liveMode func() domain.Mode

	// tracker is the per-Session self-regulation state (effectiveness tracking, Adaptive
	// Suppression, the Turn Budget — internal/agent/selfreg.go). It is NOT serialized: Resume
	// rebuilds it fresh via newAgent, the accepted v1 reset-on-resume posture (plan item 3).
	tracker *selfRegulator

	// tokens is the structural token accounting behind the Budget view: a chars→token estimator
	// the loop calibrates against each Turn's server-reported usage (internal/context). Like the
	// tracker it is per-Session and NOT serialized — a resumed Agent recalibrates from its first
	// UsageEvent, reporting the default ratio and a zero Used until then. It is structural, not a
	// Mechanism, so it stays live under Bypass (D5/D6).
	tokens *apogeectx.TokenEstimator

	// now is the request-render clock the configured system prompt's {{datetime}} placeholder
	// reads (ADR 0023). It is a field rather than a direct time.Now call so a test can pin the
	// rendered date — the injectable-clock shape cmd/apogee's sessionHost.now already uses.
	// newAgent seeds it with time.Now; it is never nil on a constructed Agent.
	now func() time.Time

	// contextFiles is the session's cache of discovered workspace context files
	// (Config.ContextFiles — internal/agent/contextfiles.go). It is filled at construction and
	// REFILLED only at a session boundary (ClearContext, a successful RestoreSession), so the
	// standing content a request carries is byte-stable for the life of a session — a
	// mid-session edit to a repo's AGENTS.md lands on the next session, not under a running
	// one. A sub-agent is handed the parent's slice verbatim (newChildAgent), so one session's
	// nested agents all speak from the same bytes. It is not serialized: the resolved-live
	// posture of ADR 0023 §6 means a resumed session re-reads the CURRENT files.
	contextFiles []contextFile

	conv         domain.Conversation // serializable conversation state (ADR 0001)
	pendingInput *domain.UserInput   // queued by Submit, consumed by the next Step
	turns        *turnLifecycle      // owns the Turn/Exchange lifecycle state (index, inExchange, exchangeStart) and, from item 2 on, the exits — internal/agent/turn.go
	compacting   bool                // guards the automatic Compaction trigger against re-entry (item 9)
	compactSat   bool                // saturation latch: a prior auto-fold could not bring history under its allocation, so further automatic folds stand down until the estimate drops back under it (S2)
	approved     map[string]bool     // tools the human allowed for the rest of this Session
	depth        int                 // sub-agent nesting level: 0 = top-level; a sub-agent runs at parent+1 (ADR 0013)
	callID       string              // this Agent's run identity: the id of the sub_agent call that spawned it, stamped on every Event it emits (domain.EventBase.CallID); empty at depth 0
}

// New constructs an Agent from cfg. It validates the configuration — including the
// Auto-mode/Confinement gate (ADR 0004) and the Mechanism ordering graph (ADR 0003,
// a constraint cycle is a startup error) — and returns an error rather than
// silently degrading a misconfigured surface. The root facade forwards apogee.New
// here, binding the real OpenAI-compatible provider client at cfg.Endpoint (P1.1)
// carrying cfg.APIKey — unconditionally, since an empty key sends no auth header.
func New(cfg domain.Config) (*Agent, error) {
	return newAgent(cfg, provider.NewClient(cfg.Endpoint, cfg.Model, provider.WithAPIKey(cfg.APIKey)))
}

// Resume reconstructs an Agent from a prior Session snapshot. Config supplies the
// live delegates (Approver, Confiner, EventSink) and state roots again — only the
// serializable conversation state comes from snap. External connections (MCP,
// network) reconnect fresh; no server-side state is restored (ADR 0008).
func Resume(cfg domain.Config, snap domain.Session) (*Agent, error) {
	return resumeAgent(cfg, snap, provider.NewClient(cfg.Endpoint, cfg.Model, provider.WithAPIKey(cfg.APIKey)))
}

// Close releases the Agent's resources. Because tools are stateless across Turns
// (ADR 0008), there is no live tool state to flush — Close tears down the provider
// client, any MCP connections, and the log sink. The Phase-0 slice holds no such live
// resources (the responder is in-process and hermetic), so Close is a no-op today; it
// exists now so embedders write the correct lifecycle before Phase 1 adds real teardown.
func (a *Agent) Close() error { return nil }

// ----------------------------------------------------------------------------
// Stepping & Turns (ADR 0007)
// ----------------------------------------------------------------------------

// Submit enqueues user input to begin (or continue) an Exchange. It does not run
// the loop; the next Step/Run consumes it. Submitting mid-Exchange is an error.
//
// Submitting with no model bound is also an error (errNoModelBound): construction no longer
// requires Config.Model, so a host may run ahead of its Upstream discovery and bind the model
// later through Rebind (ADR 0024) — this is the gate that keeps a model-less request off the
// wire until it does.
func (a *Agent) Submit(in domain.UserInput) error {
	if a.cfg.Model == "" {
		return errNoModelBound
	}
	if a.pendingInput != nil || a.turns.inExchange {
		return domain.ErrInputPending
	}
	a.pendingInput = &in
	return nil
}

// Step advances the loop exactly one Turn and returns at a quiescent boundary — no
// in-flight stream, no in-flight tool call, conversation state fully serializable
// (ADR 0007). Streaming tokens and Approval prompts happen *inside* a Step (via the
// EventSink and Approver). Snapshot and Resume are valid only at the boundary Step
// returns at.
//
// Cancellation: cancelling ctx abandons the in-flight Upstream call or tool and
// returns at the next quiescent boundary with StepResult.Status == StatusCancelled
// and conversation state left serializable — never half-streamed (ADR 0007).
//
// Recovery: a panic in a tool or Mechanism is caught at that extension boundary,
// converted to an ErrorEvent, and the loop degrades to the quiescent boundary
// rather than unwinding into the host (ADR 0007 / ADR 0002). Step returns a non-nil
// error only for loop-level faults the Agent itself cannot localise.
func (a *Agent) Step(ctx context.Context) (domain.StepResult, error) { return a.step(ctx) }

// Run steps the loop until the Exchange completes (a final no-tool response),
// cancellation, or a loop-level error — a convenience wrapper over Step for hosts
// that do not need Turn-level control. It returns the StepResult of the Step that ended
// the loop (StatusExchangeComplete or StatusCancelled). Each intermediate Turn still
// returns at its own quiescent boundary, so a cancel delivered through ctx is honoured at
// the next boundary exactly as it is under Step. The bench drives Step directly.
//
// Run owns the between-Steps boundaries it loops over, so there is no seam to interject at:
// an embedder that wants to commit a mid-Exchange user message (Interject) drives Step
// itself and calls it between the Steps it makes.
func (a *Agent) Run(ctx context.Context) (domain.StepResult, error) {
	for {
		res, err := a.step(ctx)
		if err != nil || res.Status != domain.StatusTurnComplete {
			return res, err
		}
	}
}

// AbortExchange discards an interrupted Exchange and returns the Agent to a clean quiescent
// boundary that accepts the next Submit. It rolls the conversation back to the boundary the
// Exchange began at — dropping the un-answered user message and any tool Turns committed so
// far — and clears inExchange. It is a no-op when no Exchange is open.
//
// It is the interactive host's counterpart to the Step-driven resume path. After a cancel,
// Step leaves the Exchange OPEN on purpose so a Step-driven host (the bench) re-Steps to
// re-attempt the Turn (see end()'s endCancelled row, turn.go). A host with no resume affordance — the TUI, where Esc
// means "stop, scrap it" — calls this instead, so the next /clear or message is accepted
// rather than rejected with ErrInputPending. Like Snapshot, it is valid only at a quiescent
// boundary: no worker may be driving the Agent when it is called (the host calls it only after
// the worker has returned its cancellation), preserving the single-goroutine contract.
func (a *Agent) AbortExchange() {
	if !a.turns.inExchange {
		return
	}
	a.conv.DropRange(a.exchangeBoundary(), a.conv.Len())
	// The Exchange is scrapped: closeExchange expires any deferred Response Action with it (F6) — a
	// mid-fan-out abort must not leave a stale remaining-items directive queued for the next
	// Exchange's request.
	a.turns.closeExchange()
	a.pendingInput = nil
}

// exchangeBoundary returns the conversation index the open Exchange began at — the rollback
// target AbortExchange drops from and the boundary the snapshot round-trips. It is the ONE
// reader seam over the cached a.turns.exchangeStart (ADR 0017 §2's recorded fallback): the boundary
// canNOT be re-derived as "the index of the last user message" (domain.CurrentExchange),
// because a mid-Exchange truncate_history fold drops the open Exchange's opening user message
// whenever the Exchange already holds keepLastTurns or more assistant messages — the inserted
// user-role gap note then anchors the derivation, and an abort would wrongly drop the note
// (pinned by TestExchangeStartRepairedAfterMidExchangeTruncation). So the cache stays —
// written at step()'s Exchange opening, re-anchored by the S2 repair after a history rewrite
// (loop.go), and restored by restoreState — and every consumer reads it here, so a future
// swap to the derivation has exactly one seam.
func (a *Agent) exchangeBoundary() int { return a.turns.exchangeStart }

// Mode reports the Agent's current autonomy mode. It reads the live mode under the lock, so a
// concurrent SetMode (Shift+Tab from the UI) is observed safely from the worker goroutine.
func (a *Agent) Mode() domain.Mode {
	a.modeMu.RLock()
	defer a.modeMu.RUnlock()
	return a.mode
}

// SetMode changes the autonomy mode for subsequent tool calls. It is safe to call from another
// goroutine while a Step runs: the tool menu (Plan filter) and the per-call Resolution both
// read the mode through Mode() under the same lock, so the change lands on the next read with no
// registry rebuild. A switch to Auto is safe even where fs-confinement is unavailable — the
// subprocess surface gates through Approval ("confine if you can, gate if you can't", ADR 0012),
// so no eligibility precheck is needed here.
func (a *Agent) SetMode(m domain.Mode) {
	a.modeMu.Lock()
	a.mode = m
	a.modeMu.Unlock()
}

// ConfineToWorkspace reports whether Auto's blast radius is currently fenced to the workspace
// (ADR 0012). It reads the live flag under the lock, so a concurrent SetConfineToWorkspace
// (/confine from the UI) is observed safely from the worker goroutine.
func (a *Agent) ConfineToWorkspace() bool {
	a.confineMu.RLock()
	defer a.confineMu.RUnlock()
	return a.confineToWorkspace
}

// SetConfineToWorkspace changes Auto's blast radius for subsequent tool calls: true (the
// default) fences confinable subprocess writes to the workspace and gates what cannot be
// fenced; false is the user's explicit "I am the sandbox" — Auto then runs every call
// unconfined with the user's full privileges, which is safe ONLY on a disposable machine
// (ADR 0012, as amended 2026-07-21). It is the engine half of /confine off|on; the Agent never
// flips it on its own initiative, and the ladder itself is untouched either way.
//
// It is safe to call from another goroutine (the UI) while a Step runs: the per-call Resolution
// reads the flag through ConfineToWorkspace() under the same lock, so the change lands on the
// NEXT tool call with no rebuild — exactly like SetMode. It changes only THIS Session; nothing
// is written to disk (persisting the host acknowledgement is the host's job, not the engine's).
//
// Sub-agents: a child spawned AFTER a toggle inherits the new value (newChildAgent reads the
// live flag at spawn, as it does for the mode); one already mid-flight keeps the value it was
// spawned with, so the toggle can neither loosen nor tighten a running delegation.
func (a *Agent) SetConfineToWorkspace(confine bool) {
	a.confineMu.Lock()
	a.confineToWorkspace = confine
	a.confineMu.Unlock()
}

// SetBypass switches Bypass — Mechanisms off, structure on (ADR 0006) — on or off for the rest
// of the session. It takes effect at the NEXT hook fire: the gate is consulted per catalogued
// Mechanism per hook point (skipUnderBypass, via skipMechanism), so nothing is rebuilt and a
// Turn already mid-flight starts honouring the new value at its next hook point. Off-ramp
// Mechanisms and the structural machinery (Budget, Compaction, the guardrails) are unaffected
// either way — Bypass has never governed them.
//
// It is safe to call from another goroutine (the settings surface) while a Step runs, like
// SetMode. A sub-agent spawned AFTER the switch inherits the new value (newChildAgent reads the
// live flag at spawn); one already mid-flight keeps what it was spawned with.
func (a *Agent) SetBypass(enabled bool) {
	a.bypassMu.Lock()
	a.bypass = enabled
	a.bypassMu.Unlock()
}

// bypassEnabled reports the live Bypass flag under the lock, so the worker goroutine's per-hook
// read is race-free against a concurrent SetBypass. It is the ONE read seam for the flag: cfg.Bypass
// is only the construction seed.
func (a *Agent) bypassEnabled() bool {
	a.bypassMu.RLock()
	defer a.bypassMu.RUnlock()
	return a.bypass
}

// SetCompactionEnabled switches the automatic, budget-driven Compaction trigger (the `auto-compact`
// key) on or off for the rest of the session. It takes effect at the next Exchange boundary: the
// gate is consulted per fold decision (shouldAutoCompact) and by the overflow rescue
// (emergencyFold), so switching it off stops the next automatic fold and switching it on arms it
// again with no rebuild. The on-demand /compact is unaffected — it has never consulted this gate.
//
// It is safe to call from another goroutine (the settings surface) while a Step runs, like SetMode.
// A sub-agent spawned AFTER the switch inherits the new value at spawn.
func (a *Agent) SetCompactionEnabled(enabled bool) {
	a.compactionMu.Lock()
	a.compaction = enabled
	a.compactionMu.Unlock()
}

// compactionEnabled reports the live auto-Compaction gate under the lock, so the fold decision is
// race-free against a concurrent SetCompactionEnabled. cfg.Context.CompactionEnabled is only the
// construction seed.
func (a *Agent) compactionEnabled() bool {
	a.compactionMu.RLock()
	defer a.compactionMu.RUnlock()
	return a.compaction
}

// SetContextFiles replaces the workspace context-file names folded into the standing system
// content. enable false — or an empty list, the second spelling of "off" — installs no names at
// all, exactly as the composition root's resolution collapses the two spellings into one value.
//
// It deliberately does NOT re-read the workspace: the cache moves only at a session boundary
// (construction, ClearContext, RestoreSession — reloadContextFiles), so this session keeps seeding
// byte-identical content and the server's prefix KV cache survives it. The new names are picked up
// by the NEXT /clear or restored session, which is the boundary the host tells the user about.
//
// It is safe to call from another goroutine (the settings surface) while a Step runs, like SetMode:
// the names are copied in and the slice is never mutated in place, so the reload reads a stable
// list. A name that escapes the workspace is refused by the read fence (readContextFile) and
// reported as unreadable, never folded in — the construction-time name gate is the host's to apply
// before calling this.
func (a *Agent) SetContextFiles(enable bool, names []string) {
	var live []string
	if enable && len(names) > 0 {
		live = slices.Clone(names)
	}
	a.contextFilesMu.Lock()
	a.contextFileNames = live
	a.contextFilesMu.Unlock()
}

// SetParallelAgents moves the Parallel agents cap — the width of a depth-0 sub-agent fan-out (ADR
// 0039) — for the rest of the session, or until the session moves to another server. Anything < 2
// means serial, which is the floor this loop has always run at, so a host that resolves nothing
// leaves the engine exactly where it was.
//
// It is the SERVER's number rather than the session's: the host resolves it from the bound entry's
// `parallel-agents:` pin, else that server's advertised slot count, and re-states it whenever the
// session moves or a heartbeat observes a different one. That is why it belongs to the anytime-safe
// class alongside SetMode: the beat that observes a restarted server lands on its own goroutine,
// and the cap is read once per dispatch rather than held across one, so a change lands on the next
// fan-out and never mid-flight.
func (a *Agent) SetParallelAgents(width int) {
	a.parallelAgentsMu.Lock()
	a.parallelAgents = width
	a.parallelAgentsMu.Unlock()
}

// parallelAgentsCap reports the live fan-out width under the lock, so the dispatch read is race-free
// against a concurrent SetParallelAgents. It is the ONE read seam for the cap: cfg.ParallelAgents is
// only the construction seed.
func (a *Agent) parallelAgentsCap() int {
	a.parallelAgentsMu.RLock()
	defer a.parallelAgentsMu.RUnlock()
	return a.parallelAgents
}

// contextFileList reports the live context-file names under the lock. The returned slice is never
// mutated in place — SetContextFiles installs a fresh one — so the caller may read it after the
// lock is dropped. cfg.ContextFiles is only the construction seed.
func (a *Agent) contextFileList() []string {
	a.contextFilesMu.RLock()
	defer a.contextFilesMu.RUnlock()
	return a.contextFileNames
}

// ----------------------------------------------------------------------------
// Sessions (ADR 0001 — snapshot/resume is the user feature; the bench composes fork)
// ----------------------------------------------------------------------------

// Snapshot captures the Agent's conversation state at the current quiescent
// boundary as a copyable, serializable value (ADR 0001/0007). It is valid only at a
// boundary (between Steps). Apogee exposes snapshot/resume; it exposes no fork — the
// bench composes forking by deep-copying a Session and the sandbox directory.
//
// Domain owns the Session envelope and its version; the engine owns the opaque State
// payload, so Snapshot serializes the engine's loop state (conversation + turnIndex +
// inExchange + pending input — internal/agent/state.go) into it (ADR 0010).
func (a *Agent) Snapshot() (domain.Session, error) {
	state, err := a.encodeState()
	if err != nil {
		return domain.Session{}, err
	}
	return domain.Session{Version: domain.SessionVersion, State: state}, nil
}

// ----------------------------------------------------------------------------
// Context controls (/clear, /compact — the chat mini-language seams)
// ----------------------------------------------------------------------------

// ClearContext drops the model-facing conversation history while preserving the rest of
// the loop state — the Turn counter keeps advancing, allow-for-session approvals and the
// autonomy mode survive, and the visible TUI transcript (a separate structure the host
// owns) is untouched. It is the engine half of the /clear command: the model forgets
// prior turns; the human keeps their scrollback. Valid only at a quiescent boundary;
// calling it mid-Exchange is refused (ErrInputPending) so a half-streamed Turn is never
// orphaned. The Agent stays snapshot-safe after it returns.
//
// It is also a session boundary, so the workspace context files are re-read here: the new
// session speaks from whatever the repo's AGENTS.md says NOW. A refused call changes nothing.
func (a *Agent) ClearContext() error {
	if a.turns.inExchange {
		return domain.ErrInputPending
	}
	a.reloadContextFiles()
	a.conv = *domain.NewConversation(nil)
	return nil
}

// RestoreSession swaps a prior Session snapshot into this LIVE Agent, replacing its conversation
// and loop counters (turn index, in-Exchange flag, Exchange boundary, pending input) without a
// rebuild — so the resolved tools, Mechanisms, and MCP wiring stand. It is the in-TUI resume
// primitive: the live-restore counterpart to construction-time Resume, letting the host switch
// sessions without relaunching (ADR 0001's snapshot/resume feature, live variant).
//
// Like ClearContext it is valid only at a quiescent boundary with no worker driving the Agent
// (the TUI calls it at idle); calling it mid-Exchange is refused (ErrInputPending) so a
// half-streamed Turn is never orphaned. A snapshot newer than this build understands is refused
// (ErrSessionVersion) and a malformed payload returns a decode error; in EITHER failure the live
// conversation is left untouched — restoreSnapshot decodes into a temporary and swaps only on
// full success. It does NOT touch the allow-for-session approval cache, the autonomy mode, or the
// confinement flag: those are live host state re-confirmed per ADR 0008, not part of the Session.
//
// A successful restore starts a new session, so the workspace context files are re-read: the
// resolved-live posture of ADR 0023 §6 — the standing content is not serialized, so a resumed
// session speaks from the CURRENT files, not the ones its snapshot was taken under. A REFUSED
// restore (mid-Exchange, or a corrupt/future-version snapshot) leaves the cache untouched
// along with the conversation.
func (a *Agent) RestoreSession(snap domain.Session) error {
	if a.turns.inExchange {
		return domain.ErrInputPending
	}
	if err := a.restoreSnapshot(snap); err != nil {
		return err
	}
	a.reloadContextFiles()
	return nil
}

// InExchange reports whether a multi-Turn Exchange is currently open (mid-flight). It is a
// boundary-only read: the host calls it after a RestoreSession (or at startup after Resume) to
// detect a Session snapshotted mid-task, so it can drive the step-only continue path rather than
// waiting on a Submit the open Exchange would reject. It reads a.turns.inExchange directly and,
// like Snapshot, is meant for use only at a quiescent boundary with no worker driving the Agent.
func (a *Agent) InExchange() bool { return a.turns.inExchange }

// Compact (the /compact command's engine half) lives in compact.go alongside its provider
// adapter and the generative reducer it drives (internal/context.Compact).
