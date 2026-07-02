package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
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
	_ tea.Msg = exchangeDoneMsg{}
	_ tea.Msg = cancelledMsg{}
	_ tea.Msg = errMsg{}
	_ tea.Msg = compactDoneMsg{}
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
