package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/heartbeat"
)

// ----------------------------------------------------------------------------
// The worker→Update messages (phase-2 detail plan §3 C1–C4)
// ----------------------------------------------------------------------------

// These are the messages the concurrency seam hands to Bubble Tea's single-threaded
// Update loop. Each is a plain value: tea.Msg is a method-less interface (an alias for
// ultraviolet.Event in bubbletea v2), so any struct satisfies it. The model folds these
// into the transcript and status in P2.2–P2.4; the seam only produces them.
//
// Compile-time assertion that every seam message is a valid tea.Msg keeps the set honest
// if the upstream Msg definition ever tightens.
var (
	_ tea.Msg = eventMsg{}
	_ tea.Msg = approvalReqMsg{}
	_ tea.Msg = askReqMsg{}
	_ tea.Msg = presentedMsg{}
	_ tea.Msg = exchangeDoneMsg{}
	_ tea.Msg = cancelledMsg{}
	_ tea.Msg = errMsg{}
	_ tea.Msg = compactDoneMsg{}
	_ tea.Msg = turnSnapshotMsg{}
	_ tea.Msg = interjectedMsg{}
	_ tea.Msg = saveDoneMsg{}
	_ tea.Msg = heartbeatTickMsg{}
	_ tea.Msg = beatMsg{}
)

// eventMsg carries one engine Event into the Update loop. The teaSink wraps every Event
// the loop emits in an eventMsg and sends it to the program (phase-2 detail plan §3 C2);
// the model renders it (RENDER ONLY — no agent logic).
type eventMsg struct {
	Event domain.Event
}

// approvalReqMsg hands a pending Approval to the Update loop. The uiApprover sends it from
// the worker goroutine and blocks on Reply; the model renders the prompt and, on the
// human's keypress, sends the decision back over Reply (phase-2 detail plan §3 C3). Reply
// is buffered (cap 1) by the sender so the Update loop never blocks replying, and so a
// late reply after a cancel is absorbed rather than leaking a goroutine.
type approvalReqMsg struct {
	Request domain.ApprovalRequest
	Reply   chan domain.ApprovalDecision
}

// askReqMsg hands a pending ask-user question to the Update loop. The uiAsker sends it from
// the worker goroutine and blocks on Reply; the model renders the question and, on the
// human's typed-then-submitted answer, sends it back over Reply (P3.11 — the free-text
// analogue of approvalReqMsg). Reply is buffered (cap 1) by the sender so the Update loop
// never blocks replying, and so a late answer after a cancel is absorbed rather than leaking.
type askReqMsg struct {
	Request domain.AskRequest
	Reply   chan domain.AskAnswer
}

// presentedMsg hands a finished presentation to the Update loop (ADR 0019). The uiPresenter
// sends it from the worker goroutine once it has walked the ladder and does NOT wait for a
// reply — unlike the two rendezvous Msgs above, a presentation asks the human nothing. It IS
// rung 0: the loop appends the transcript entry that carries the document's path to the user,
// which is the rung that always runs and never depends on a mechanism succeeding.
type presentedMsg struct {
	// Title is the model's optional label for the document; empty when it named none.
	Title string
	// Path is the document's workspace-relative path — the plain text the terminal linkifies.
	Path string
	// Location is the served URL when rung 2 carried the document, and empty for every other
	// rung: the path line already says where the document is, so only a URL adds a line.
	Location string
	// Method is the rung actually reached, which decides the entry's closing status line.
	Method domain.PresentMethod
	// Reason is why the ladder stopped where it did, when a rung was tried and did not deliver
	// ("no opener on this machine"). Empty when nothing failed — a rung the host never wired is
	// skipped, not failed.
	Reason string
}

// exchangeDoneMsg is the worker's terminal Msg when the Exchange reached its final no-tool
// boundary (StatusExchangeComplete). The model returns to idle; Result carries the closing
// StepResult for the status line.
type exchangeDoneMsg struct {
	Result domain.StepResult
}

// cancelledMsg is the worker's terminal Msg when a user stop cancelled the Exchange at the
// next quiescent boundary (StatusCancelled). The Session is still resumable (phase-2 detail
// plan §3 C4); the model clears any pending prompt and returns to idle.
type cancelledMsg struct {
	Result domain.StepResult
}

// errMsg is the worker's terminal Msg for a loop-level fault Submit or Step could not
// localise. Recovered tool/Mechanism faults arrive as ErrorEvents through the sink (ADR
// 0007), not here — errMsg is reserved for the rare error the drive loop itself returns.
type errMsg struct {
	Err error
}

// compactDoneMsg is the /compact worker's terminal Msg (startCompact): Err is nil on a
// successful compaction and carries whatever Agent.Compact reported otherwise. Skipped is
// true when the conversation was too small to fold (no upstream call, history untouched) —
// the model then reports "nothing to compact" and leaves the gauge alone rather than falsely
// claiming a compaction. The model records the outcome as a transcript note and returns to
// idle. A cancelled compaction arrives as cancelledMsg instead, not here.
type compactDoneMsg struct {
	Skipped bool
	Err     error
}

// turnSnapshotMsg carries one per-Turn engine snapshot from the worker to the Update loop. The
// worker sends it after each StatusTurnComplete (driveExchange), snapshotting the engine between
// Steps — the single-driver boundary at which Snapshot is valid mid-Exchange (agent.go) — so the
// Model can persist per-Turn and a crash loses at most one Turn. It is sent AFTER that Turn's
// Events: the teaSink delivered them synchronously inside Step before it returned, so the
// transcript the Model holds when this folds is consistent with the snapshot it carries.
type turnSnapshotMsg struct {
	Sess domain.Session
}

// interjectedMsg reports which staged interjections the worker actually COMMITTED into the open
// Exchange at the between-Steps boundary it just passed (ADR 0025). The worker drains its mailbox,
// calls Engine.Interject for each row in FIFO order, and sends this one Msg — carrying the rows
// that landed, in delivery order — before it Steps again, so the transcript records each message
// at the moment the model saw it rather than at the moment it was typed.
//
// It is a REPORT, not a request: the Model reconciles its display rows against items by id,
// moving the delivered ones into the transcript and leaving the rest staged. That is what makes a
// failed delivery degrade to "held" rather than "lost" — a drain that delivered nothing still
// sends the Msg (with no items), and the undelivered rows keep their place in the queue until the
// Exchange's terminal fold rules on them (flushed on a natural completion, held after a stop or a
// fault). An empty drain sends no Msg at all.
type interjectedMsg struct {
	items []queuedInterjection
}

// heartbeatTickMsg re-arms the upstream heartbeat. It is scheduled one heartbeat.Interval after a
// beat LANDED — never on a fixed wall clock — so the observation and the wait can never overlap,
// and its fold issues the next beat Cmd. gen names the chain that scheduled it (the spinner's
// generation guard, spinner.go): the Update loop drops a tick carrying a retired generation, so two
// chains can never beat at once.
type heartbeatTickMsg struct{ gen int }

// beatMsg carries one landed observation of the Upstream into the Update loop — reachability, the
// served model and its window, and the advertised model list (internal/heartbeat). It arrives off
// a Cmd goroutine rather than from the worker, so it can land in ANY state: a beat that fails while
// an Exchange is in flight is deliberately ignored (a live stream is stronger evidence of a
// reachable server than a timed-out probe on a saturated one). gen carries the same generation
// guard heartbeatTickMsg does.
type beatMsg struct {
	gen  int
	beat heartbeat.Beat
}

// saveDoneMsg reports the outcome of one asynchronous SessionHost.Save (the per-Turn and idle
// save Cmds). Err is nil on success; a failure is noted once on the ok→fail transition and then
// swallowed — a save failure must never interrupt the conversation. The Model clears its
// single-flight busy flag on this Msg and dispatches any save that coalesced while it was in
// flight (latest-wins).
type saveDoneMsg struct {
	Err error
}
