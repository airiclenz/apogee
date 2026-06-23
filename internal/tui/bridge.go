package tui

import (
	"sync/atomic"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The program seam (phase-2 detail plan §3 C2/C3)
// ----------------------------------------------------------------------------

// programSender is the narrow, goroutine-safe send the seam needs from the running Bubble
// Tea program: *tea.Program.Send(tea.Msg) satisfies it. Depending on this interface rather
// than the concrete *tea.Program lets a test stub stand in for the program, so the whole
// seam (sink, approver, worker) is provable under -race without a real terminal.
type programSender interface {
	Send(tea.Msg)
}

// ----------------------------------------------------------------------------
// Late binding to the running program (resolves the construction chicken-and-egg)
// ----------------------------------------------------------------------------

// Bridge is the late-bound link between the Agent and the running Bubble Tea program. The
// composition root (cmd/apogee) must install an EventSink (required by apogee.New) and an
// Approver into Config *before* it constructs the Agent — but the program those delegates
// push to does not exist until Run starts it. Bridge resolves that chicken-and-egg: the
// root builds a Bridge, installs its Sink and Approver into Config, constructs the Agent,
// then Run binds the live program once it exists (phase-2 detail plan §3 C2/C3).
//
// The Sink and Approver share one late-bound programRef, so a single Bind wires both.
type Bridge struct {
	prog     *programRef
	sink     *teaSink
	approver *uiApprover
}

// NewBridge builds an unbound Bridge whose Sink and Approver are usable immediately as
// Config delegates. They only need the program once the Agent is stepped, which cannot
// happen before Run binds it — so construction is safe even though no program exists yet.
func NewBridge() *Bridge {
	prog := &programRef{}
	return &Bridge{
		prog:     prog,
		sink:     &teaSink{prog: prog},
		approver: &uiApprover{prog: prog},
	}
}

// Sink returns the EventSink the composition root installs in Config.Events (C2).
func (b *Bridge) Sink() domain.EventSink { return b.sink }

// Approver returns the Approver the composition root installs in Config.Approver (C3).
func (b *Bridge) Approver() domain.Approver { return b.approver }

// Bind connects the live program. Run calls it once, before the program processes any
// input that could launch a worker, so every later Emit/Approve reaches a bound program.
func (b *Bridge) Bind(p programSender) { b.prog.bind(p) }

// programRef is a concurrency-safe, late-bound handle to the running program. send runs on
// the worker goroutine (via Emit/Approve); bind runs once on the program goroutine inside
// Run. The atomic pointer makes that hand-off race-free no matter how Bubble Tea schedules
// the worker Cmd, so the seam stays clean under the race detector.
type programRef struct {
	box atomic.Pointer[senderBox]
}

// senderBox boxes the programSender interface so it can live in an atomic.Pointer (which
// needs a concrete pointer element type).
type senderBox struct {
	sender programSender
}

// bind installs the live program. It is called once, inside Run.
func (r *programRef) bind(s programSender) { r.box.Store(&senderBox{sender: s}) }

// send forwards msg to the bound program. Before Bind it is a no-op — which never happens
// in production (Emit/Approve fire only from the worker, launched after Bind) but keeps the
// seam safe to drive in isolation. It never blocks: Send is async (phase-2 detail plan §3
// C2), so a deadlock here is structurally impossible.
func (r *programRef) send(msg tea.Msg) {
	if box := r.box.Load(); box != nil {
		box.sender.Send(msg)
	}
}
