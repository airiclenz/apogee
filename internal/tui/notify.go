package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Telling the terminal that apogee waits on the human (ui.notify)
// ----------------------------------------------------------------------------
//
// A run that stops on an approval or an ask_user question waits for a human who may have looked
// away, so the pane that opens also tells the terminal: a bell, an OSC 9 desktop notification, both
// or nothing, as `ui.notify` says (domain.NotifyMode). The bytes go out through tea.Raw, the way
// mousereassert.go re-asserts mouse tracking — they are a write to the terminal, not a frame.
//
// It fires on the TRANSITION into awaiting, never on every pane that opens:
//   - the conversation's own approval or question notifies unless the human already answered one
//     earlier in the same Exchange (Model.answeredInExchange) — a queued child's gate that folds
//     right behind the one just answered finds them at the keyboard;
//   - a background workflow's prompt notifies on its FIRST offer only (backgroundWorkflows.offered):
//     the re-offer after an esc, and the ^a open from /workflows the human asked for, stay silent;
//   - the pickers the decision latch also arms (openDecisionLatch) are opened by the human and
//     never notify.
//
// A Turn that ends done or errored notifies too, but only while the terminal reports the window
// blurred (Model.blurred, View's ReportFocus): a stop and a /compact are the human's own act and
// stay silent, and a terminal that reports no focus never reads as blurred.

// The OSC 9 texts, one per thing apogee waits on.
const (
	notifyApprovalText   = "apogee: approval needed"
	notifyQuestionText   = "apogee: question"
	notifyTurnDoneText   = "apogee: turn finished"
	notifyTurnFailedText = "apogee: turn failed"
)

// notifySequence is the bytes mode writes to tell the terminal text: BEL for the bell, an OSC 9
// notification carrying text for osc9 — wrapped for tmux passthrough when tmux is set, since tmux
// swallows an OSC 9 it is not asked to pass on — both for both, and nothing for off or the empty
// zero value (domain.ParseNotifyMode reads "" as off).
func notifySequence(mode domain.NotifyMode, tmux bool, text string) string {
	var b strings.Builder
	if mode == domain.NotifyBell || mode == domain.NotifyBoth {
		b.WriteByte(ansi.BEL)
	}
	if mode == domain.NotifyOSC9 || mode == domain.NotifyBoth {
		seq := ansi.Notify(text)
		if tmux {
			seq = ansi.TmuxPassthrough(seq)
		}
		b.WriteString(seq)
	}
	return b.String()
}

// notifyCmd is the Cmd that tells the terminal text under the live `ui.notify`, or nil when that
// writes nothing.
func (m Model) notifyCmd(text string) tea.Cmd {
	seq := notifySequence(m.opts.UI.Notify, m.notifyTmux, text)
	if seq == "" {
		return nil
	}
	return tea.Raw(seq)
}

// notifyDecisionOpened is the notification owed by a decision pane that has just opened for origin
// (nil for the conversation's own request): a background prompt's first offer marks it offered and
// notifies, its later opens do not; the conversation's own request notifies unless the human has
// already answered one in this Exchange.
func (m *Model) notifyDecisionOpened(origin *domain.WorkflowPrompt, text string) tea.Cmd {
	if origin != nil {
		if m.workflows.offered[origin.ID] {
			return nil
		}
		m.workflows = m.workflows.withOffered(origin.ID)
		return m.notifyCmd(text)
	}
	if m.answeredInExchange {
		return nil
	}
	return m.notifyCmd(text)
}

// notifyTurnEnd is the notification owed by a Turn that ended under its own power — faulted names
// one the loop abandoned — and nil while the window has focus.
func (m Model) notifyTurnEnd(faulted bool) tea.Cmd {
	if !m.blurred {
		return nil
	}
	if faulted {
		return m.notifyCmd(notifyTurnFailedText)
	}
	return m.notifyCmd(notifyTurnDoneText)
}
