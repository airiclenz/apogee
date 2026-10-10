package tui

import (
	"fmt"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The adoption pane (ADR 0096 §4)
// ----------------------------------------------------------------------------
//
// A rule in the Project config grants nothing until the human adopts it: the file is committable, so
// a teammate's commit, a pull or an edit made behind apogee's back is text nobody here said yes to.
// Such a rule is PROPOSED, and this pane is where it is asked about — at start-up, and again at the
// first idle moment after a config file changed mid-session, so a rule never arrives in the middle
// of a turn it would have answered for.
//
// It is the start-up offers' posture (keymigration.go) in every respect but the question: the shared
// picker over three fixed rows, raised unasked under a notice saying why, one rule per pane with the
// rest queued on the overlay, esc ending the round. Esc is "not now" for every rule left, and "not
// now" persists nothing — the rule stays proposed, so the next start-up asks again. "Adopt" and
// "reject" are one call each to a seam the binary owns ([ConfigHost.AdoptRules],
// [ConfigHost.RejectRules]): the adoption record, its fingerprints and the rules in force are the
// binary's business, and a rejected rule is not offered again until its text changes, because the
// record pins the text it answered.
//
// A rule is offered once per session. A watched change re-reads what is proposed
// ([ConfigHost.ProposedRules]) and offers only what no pane has raised yet — a new rule, or one whose
// text changed — so a save of an unrelated key, or apogee's own write of an "Always in this
// project…" rule, does not put a "not now" answer back up.

// The three answers, in the order the rows are offered. The indexes are the offering's, so they are
// what acceptAdoption switches on.
const (
	adoptionAdopt = iota
	adoptionNotNow
	adoptionReject
)

// adoptionState is the session's side of the pane: what is waiting to be offered and what has been.
// Plain slices of plain values, safe in the copied Model (ADR 0011).
type adoptionState struct {
	// waiting are the proposed rules to raise at the next fold that finds the session idle with
	// nothing else up (adoptAfterFold) — the start-up's finding, or a watched change's.
	waiting []domain.AllowRule
	// offered is every rule a pane has raised this session, answered or not, which is what keeps a
	// later change from asking about it again.
	offered []domain.AllowRule
}

// queueAdoption sets the rules waiting to be offered to the proposed rules no pane has raised yet. It
// REPLACES rather than appends: proposed is the whole of what the files propose now, so a rule a
// later save took out again is not offered at all.
func (m *Model) queueAdoption(proposed []domain.AllowRule) {
	waiting := make([]domain.AllowRule, 0, len(proposed))
	for _, rule := range proposed {
		if !slices.Contains(m.adoption.offered, rule) {
			waiting = append(waiting, rule)
		}
	}
	m.adoption.waiting = waiting
}

// openAdoption raises the pane over the waiting rules, the first one asked about and the rest queued
// behind it. It gives way to everything: the start-up offers and the pre-bound ask are up first at
// construction, a turn, an approval or an open pane all hold it, and the rules simply keep waiting —
// the Update tail asks again at every fold (adoptAfterFold), so the pane comes up the moment the
// session is idle with nothing else up. No [ConfigHost] means no pane: whether the question exists
// is decided ABOUT the host before any act is called (ADR 0054 decision 3a).
func (m *Model) openAdoption() {
	if len(m.adoption.waiting) == 0 || m.opts.Config == nil {
		return
	}
	if m.picker.open || m.settings.open || !m.canWake() {
		return
	}
	queue := m.adoption.waiting
	m.adoption.offered = append(m.adoption.offered, queue...)
	m.adoption.waiting = nil
	m.transcript.addNote(adoptionNotice(queue))
	m.picker = picker{open: true, kind: pickerAdoption, adoption: queue}
}

// adoptAfterFold is the Update tail's adoption half: when rules are waiting, it tries the pane now
// (openAdoption). Anything but the Model, or a Model with nothing waiting, passes through untouched.
func adoptAfterFold(next tea.Model) tea.Model {
	m, ok := next.(Model)
	if !ok || len(m.adoption.waiting) == 0 {
		return next
	}
	m.openAdoption()
	return m
}

// refreshAdoption re-reads what the config files propose after a watched change and queues what no
// pane has raised yet. A read that fails is said, and leaves the queue as it stood.
func (m Model) refreshAdoption() Model {
	proposed, err := m.configHostOrNoop().ProposedRules()
	if err != nil {
		m.transcript.addNote("could not read the project's proposed allow rules: " + stripEscapes(err.Error()))
		return m
	}
	m.queueAdoption(proposed)
	return m
}

// adoptionNotice is the line the unasked-for pane comes up under: which rules the Project config
// proposes, and that none of them does anything yet.
func adoptionNotice(rules []domain.AllowRule) string {
	names := make([]string, 0, len(rules))
	for _, rule := range rules {
		names = append(names, adoptionRuleName(rule))
	}
	return fmt.Sprintf("this project's .apogee/config.yaml proposes %s — no proposed rule applies "+
		"until you adopt it", entryNameList(names))
}

// adoptionRuleName spells a rule for a sentence: the prefix a terminal rule matches, or the MCP
// server a server rule names, its text sanitized because it is a repository file's.
func adoptionRuleName(rule domain.AllowRule) string {
	if rule.Kind == domain.AllowRuleMCPServer {
		return "MCP server `" + stripEscapes(rule.Text) + "`"
	}
	return "`" + stripEscapes(rule.Text) + "`"
}

// adoptionTitle names the rule THIS pane is asking about, so a round of several can never be
// answered for the wrong one. A queue that emptied under the pane (which no path reaches) titles
// nothing rather than inventing a rule.
func (m Model) adoptionTitle() string {
	if len(m.picker.adoption) == 0 {
		return ""
	}
	return fmt.Sprintf("allow %s without asking in this project?", adoptionRuleName(m.picker.adoption[0]))
}

// adoptionRows is the offering: three answers, each with a gloss saying what it costs, because
// "adopt" lets calls run unasked and "not now" has to say that the question comes back.
func adoptionRows() []popupRow {
	return []popupRow{
		{"adopt", "— it runs without asking here, until the entry changes"},
		{"not now", "— it stays inert; the offer comes back at the next start-up"},
		{"reject", "— it stays inert, and is not offered again until the entry changes"},
	}
}

// acceptAdoption answers the open pane and moves to the next rule in the round. Each answer is one
// note and at most one seam call — synchronous file work on a keypress the human is waiting on, a
// failure REPORTED (the [SettingsHost.Write] contract) — and the queue advances whatever came of it:
// a failed answer leaves the rule proposed, which is exactly the state "not now" leaves it in.
func (m Model) acceptAdoption(choice int) (tea.Model, tea.Cmd) {
	if len(m.picker.adoption) == 0 {
		return m, nil
	}
	rule, rest := m.picker.adoption[0], m.picker.adoption[1:]

	switch choice {
	case adoptionAdopt:
		m.transcript.addNote(m.answerRuleNote(rule, ConfigHost.AdoptRules,
			"adopted %s — it runs without asking in this project", "could not adopt %s: %s"))
	case adoptionNotNow:
		m.transcript.addNote(fmt.Sprintf("%s stays proposed — the offer comes back at the next start-up",
			adoptionRuleName(rule)))
	case adoptionReject:
		m.transcript.addNote(m.answerRuleNote(rule, ConfigHost.RejectRules,
			"rejected %s — it is not offered again until the entry changes", "could not reject %s: %s"))
	}

	// The whole overlay is replaced rather than edited, so nothing of the answered pane — the filter
	// above all — survives into the next rule's question.
	m.picker = picker{}
	if len(rest) > 0 {
		m.picker = picker{open: true, kind: pickerAdoption, adoption: rest}
	}
	return m, nil
}

// answerRuleNote records one answer about rule through answer and words what came of it: done when
// the record moved, failed — naming the rule and the seam's reason — when it did not.
func (m Model) answerRuleNote(
	rule domain.AllowRule,
	answer func(ConfigHost, []domain.AllowRule) error,
	done, failed string,
) string {
	if err := answer(m.configHostOrNoop(), []domain.AllowRule{rule}); err != nil {
		return fmt.Sprintf(failed, adoptionRuleName(rule), stripEscapes(err.Error()))
	}
	return fmt.Sprintf(done, adoptionRuleName(rule))
}
