package tui

import (
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/testtime"
)

// The bytes each mode writes, spelled out rather than rebuilt through notifySequence, so the tests
// pin the wire form a terminal reads.
const (
	wantBell            = "\a"
	wantOSC9Approval    = "\x1b]9;apogee: approval needed\a"
	wantOSC9Question    = "\x1b]9;apogee: question\a"
	wantOSC9TurnDone    = "\x1b]9;apogee: turn finished\a"
	wantOSC9TurnFailed  = "\x1b]9;apogee: turn failed\a"
	wantTmuxOSC9Approve = "\x1bPtmux;\x1b\x1b]9;apogee: approval needed\a\x1b\\"
)

// latchBackstops is how many members of a decision pane's batch never answer within a test: the
// decision latch's backstop tick, which parks for approvalArmBackstop (decisionLatchCmds).
const latchBackstops = 1

// notifyModel is a ready, idle test model under the given ui.notify mode, outside tmux whatever the
// environment running the test says.
func notifyModel(t *testing.T, mode domain.NotifyMode) Model {
	t.Helper()
	m := newTestModel(t)
	m.opts.UI.Notify = mode
	m.notifyTmux = false
	return m
}

// rawWrites runs cmd's whole tree and returns the bytes of every tea.RawMsg it writes, in the order
// they arrived. The root runs in place — a fold returns a batch, or one write, both of which answer
// at once — and every member under it runs on a goroutine of its own, as
// TestModelApprovalFoldAsksTheTerminalForItsDrainMarker runs them, because a decision pane's batch
// carries its latch's backstop tick: the collection ends once all but blocking members have
// answered, so it never waits that tick out.
func rawWrites(t *testing.T, cmd tea.Cmd, blocking int) []string {
	t.Helper()
	if cmd == nil {
		return nil
	}
	results := make(chan tea.Msg, 16)
	results <- cmd()
	outstanding := 1
	launch := func(c tea.Cmd) {
		outstanding++
		go func() { results <- c() }()
	}
	cmdType := reflect.TypeOf(tea.Cmd(nil))
	var writes []string
	budget := testtime.Budget(time.Second)
	deadline := time.After(budget)
	for outstanding > 0 && (outstanding > blocking || len(results) > 0) {
		select {
		case msg := <-results:
			outstanding--
			if raw, ok := msg.(tea.RawMsg); ok {
				if seq, isText := raw.Msg.(string); isText {
					writes = append(writes, seq)
				}
				continue
			}
			// A batch or a sequence: its members run too.
			members := reflect.ValueOf(msg)
			if !members.IsValid() || members.Kind() != reflect.Slice || members.Type().Elem() != cmdType {
				continue
			}
			for i := range members.Len() {
				if member, _ := members.Index(i).Interface().(tea.Cmd); member != nil {
					launch(member)
				}
			}
		case <-deadline:
			t.Fatalf("%d of the Cmd tree's members still running after %v; want at most %d", outstanding, budget, blocking)
		}
	}
	return writes
}

// assertWrites fails unless got carries exactly want, in order.
func assertWrites(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("terminal writes = %q, want %q", got, want)
	}
}

// The builder is the wire form: BEL for the bell, OSC 9 with the text for osc9, the two for both,
// nothing for off or the empty value, and the OSC 9 — never the bell, which tmux passes on itself —
// wrapped in tmux's passthrough while $TMUX is set.
func TestNotifySequenceSpellsEachMode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		mode domain.NotifyMode
		tmux bool
		want string
	}{
		{"empty", "", false, ""},
		{"off", domain.NotifyOff, false, ""},
		{"off in tmux", domain.NotifyOff, true, ""},
		{"bell", domain.NotifyBell, false, wantBell},
		{"bell in tmux", domain.NotifyBell, true, wantBell},
		{"osc9", domain.NotifyOSC9, false, wantOSC9Approval},
		{"osc9 in tmux", domain.NotifyOSC9, true, wantTmuxOSC9Approve},
		{"both", domain.NotifyBoth, false, wantBell + wantOSC9Approval},
		{"both in tmux", domain.NotifyBoth, true, wantBell + wantTmuxOSC9Approve},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := notifySequence(tc.mode, tc.tmux, notifyApprovalText); got != tc.want {
				t.Errorf("notifySequence(%q, tmux %v) = %q, want %q", tc.mode, tc.tmux, got, tc.want)
			}
		})
	}
}

// An approval pane and an ask pane each tell the terminal they opened, the way ui.notify says, and
// the empty value a hand-built Options carries reads as off.
func TestNotifyDecisionPaneOpensPerMode(t *testing.T) {
	t.Parallel()
	approval := func() tea.Msg {
		return approvalReqMsg{
			Request: domain.ApprovalRequest{Tool: "write_file", Reason: "write", CacheKey: ordinaryGateKey},
			Reply:   make(chan domain.ApprovalDecision, 1),
		}
	}
	question := func() tea.Msg {
		return askReqMsg{Request: domain.AskRequest{Question: "Which?"}, Reply: make(chan domain.AskAnswer, 1)}
	}
	cases := []struct {
		name string
		mode domain.NotifyMode
		msg  func() tea.Msg
		want []string
	}{
		{"approval empty", "", approval, nil},
		{"approval off", domain.NotifyOff, approval, nil},
		{"approval bell", domain.NotifyBell, approval, []string{wantBell}},
		{"approval osc9", domain.NotifyOSC9, approval, []string{wantOSC9Approval}},
		{"approval both", domain.NotifyBoth, approval, []string{wantBell + wantOSC9Approval}},
		{"ask empty", "", question, nil},
		{"ask off", domain.NotifyOff, question, nil},
		{"ask bell", domain.NotifyBell, question, []string{wantBell}},
		{"ask osc9", domain.NotifyOSC9, question, []string{wantOSC9Question}},
		{"ask both", domain.NotifyBoth, question, []string{wantBell + wantOSC9Question}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := notifyModel(t, tc.mode)
			startStubWorker(t, &m)
			_, cmd := stepCmd(t, m, tc.msg())
			assertWrites(t, rawWrites(t, cmd, latchBackstops), tc.want...)
		})
	}
}

// Inside tmux the pane's OSC 9 goes out wrapped for passthrough.
func TestNotifyWrapsTheOSC9ForTmux(t *testing.T) {
	t.Parallel()
	m := notifyModel(t, domain.NotifyOSC9)
	m.notifyTmux = true
	startStubWorker(t, &m)
	_, cmd := stepCmd(t, m, approvalReqMsg{
		Request: domain.ApprovalRequest{Tool: "write_file", CacheKey: ordinaryGateKey},
		Reply:   make(chan domain.ApprovalDecision, 1),
	})
	assertWrites(t, rawWrites(t, cmd, latchBackstops), wantTmuxOSC9Approve)
}

// A request that folds after the human answered one in the same Exchange — a queued child's gate
// right behind the one just ruled on — finds them at the keyboard and opens silent; the next
// Exchange's first request notifies again.
func TestNotifyAnsweredDecisionSilencesTheNextRequest(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		first  tea.Msg
		answer tea.KeyPressMsg
	}{
		{
			name: "approval answered",
			first: approvalReqMsg{
				Request: domain.ApprovalRequest{Tool: "write_file", CacheKey: ordinaryGateKey},
				Reply:   make(chan domain.ApprovalDecision, 1),
			},
			answer: keyRune('a'),
		},
		{
			name: "question answered",
			first: askReqMsg{
				Request: domain.AskRequest{Question: "Which?", Choices: []string{"yes", "no"}},
				Reply:   make(chan domain.AskAnswer, 1),
			},
			answer: keyEnter(),
		},
	}
	next := func() approvalReqMsg {
		return approvalReqMsg{
			Request: domain.ApprovalRequest{Tool: "edit_file", CacheKey: ordinaryGateKey},
			Reply:   make(chan domain.ApprovalDecision, 1),
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := notifyModel(t, domain.NotifyBell)
			startStubWorker(t, &m)
			m, cmd := stepCmd(t, m, tc.first)
			assertWrites(t, rawWrites(t, cmd, latchBackstops), wantBell)

			m = step(t, armed(m), tc.answer)
			if m.state != stateRunning {
				t.Fatalf("state = %v after the answer, want running", m.state)
			}
			m, cmd = stepCmd(t, m, next())
			if m.state != stateAwaitingApproval {
				t.Fatalf("state = %v, want the queued approval open", m.state)
			}
			assertWrites(t, rawWrites(t, cmd, latchBackstops))

			m = step(t, armed(m), keyRune('a'))
			m = step(t, m, exchangeDoneMsg{Result: domain.StepResult{Status: domain.StatusExchangeComplete}})
			startStubWorker(t, &m)
			_, cmd = stepCmd(t, m, next())
			assertWrites(t, rawWrites(t, cmd, latchBackstops), wantBell)
		})
	}
}

// A background workflow's prompt notifies on its first offer; the re-offer after an esc, at the next
// Exchange's end, stays silent.
func TestNotifyBackgroundPromptOnItsFirstOfferOnly(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{workflowPrompts: []domain.WorkflowPrompt{bgApproval(3)}}
	m := newTestModelEng(t, eng, testOpts)
	m.opts.UI.Notify = domain.NotifyBell
	m.notifyTmux = false
	m = foldEvents(t, m, bgPhase(domain.WorkflowStarted))

	m, cmd := stepCmd(t, m, eventMsg{Event: bgWaiting()})
	if m.state != stateAwaitingApproval || m.pending == nil || m.pending.Workflow == nil {
		t.Fatalf("state %v; want the waiting approval offered at idle", m.state)
	}
	assertWrites(t, rawWrites(t, cmd, latchBackstops), wantBell)

	m = step(t, m, keyEsc())
	if m.state != stateIdle {
		t.Fatalf("state = %v after esc, want idle with the prompt back in the queue", m.state)
	}
	startStubWorker(t, &m)
	m, cmd = stepCmd(t, m, exchangeDoneMsg{Result: domain.StepResult{Status: domain.StatusExchangeComplete}})
	if m.state != stateAwaitingApproval || m.pending == nil || m.pending.Workflow == nil {
		t.Fatalf("state %v; want the dismissed approval offered again at the Exchange's end", m.state)
	}
	assertWrites(t, rawWrites(t, cmd, latchBackstops))
}

// ^a in /workflows opens a prompt the human asked for, so it rings nothing even on its first open.
func TestNotifyCtrlAOpenFromWorkflowsIsSilent(t *testing.T) {
	t.Parallel()
	m, _ := waitingWorkflowsModel(t, bgApproval(3))
	m.opts.UI.Notify = domain.NotifyBoth
	m.notifyTmux = false
	m = workflowsKeyStep(t, m, keyEnter())
	if m.workflowsPane.level != workflowsAtDetail {
		t.Fatalf("precondition: the view is at level %d, want the detail", m.workflowsPane.level)
	}

	m, cmd := stepCmd(t, m, keyCtrl('a'))
	if m.state != stateAwaitingApproval || m.pending == nil || m.pending.Workflow == nil {
		t.Fatalf("state %v; want ^a to open the waiting approval", m.state)
	}
	assertWrites(t, rawWrites(t, cmd, latchBackstops))
}

// A Turn that ends under its own power tells a blurred window — `turn finished`, or `turn failed`
// for a faulted Exchange and a loop error — while a focused window, a stop and a /compact hear
// nothing.
func TestNotifyTurnEndOnlyWhileBlurred(t *testing.T) {
	t.Parallel()
	done := exchangeDoneMsg{Result: domain.StepResult{Status: domain.StatusExchangeComplete}}
	faulted := exchangeDoneMsg{Result: domain.StepResult{Status: domain.StatusExchangeComplete, Faulted: true}}
	cases := []struct {
		name  string
		focus []tea.Msg
		msg   tea.Msg
		want  []string
	}{
		{"done while focused", nil, done, nil},
		{"done after blur and refocus", []tea.Msg{tea.BlurMsg{}, tea.FocusMsg{}}, done, nil},
		{"done while blurred", []tea.Msg{tea.BlurMsg{}}, done, []string{wantOSC9TurnDone}},
		{"faulted while blurred", []tea.Msg{tea.BlurMsg{}}, faulted, []string{wantOSC9TurnFailed}},
		{"faulted while focused", nil, faulted, nil},
		{"loop error while blurred", []tea.Msg{tea.BlurMsg{}}, errMsg{Err: errors.New("boom")}, []string{wantOSC9TurnFailed}},
		{"cancel while blurred", []tea.Msg{tea.BlurMsg{}}, cancelledMsg{}, nil},
		{"compact while blurred", []tea.Msg{tea.BlurMsg{}}, compactDoneMsg{}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := notifyModel(t, domain.NotifyOSC9)
			startStubWorker(t, &m)
			for _, msg := range tc.focus {
				m = step(t, m, msg)
			}
			_, cmd := stepCmd(t, m, tc.msg)
			assertWrites(t, rawWrites(t, cmd, 0), tc.want...)
		})
	}
}

// The laid-out frame asks the terminal for its focus reports, which the Turn-end notify reads.
func TestNotifyFrameAsksForFocusReports(t *testing.T) {
	t.Parallel()
	if !newTestModel(t).View().ReportFocus {
		t.Error("the laid-out frame does not set ReportFocus: a blurred window could never be told a Turn ended")
	}
}
