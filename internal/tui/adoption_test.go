package tui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The adoption pane (adoption.go)
// ----------------------------------------------------------------------------

// The proposed rules the tests offer: a terminal prefix and an MCP server, both from the Project
// config.
var (
	goTestRule = domain.AllowRule{Kind: domain.AllowRuleTerminal, Text: "go test", Layer: domain.AllowRuleProject}
	docsRule   = domain.AllowRule{Kind: domain.AllowRuleMCPServer, Text: "docs", Layer: domain.AllowRuleProject}
)

// adoptionLog stands in for the adoption seams: the rules each answer recorded, in order, and what
// a watched change finds proposed. Called synchronously on the test's goroutine, so it needs no guard.
type adoptionLog struct {
	adopted  []domain.AllowRule
	rejected []domain.AllowRule
	proposed []domain.AllowRule
}

// adoptionOpts are the Options of a start-up that found proposed rules: the rules, the two answer
// seams, the re-read a watched change asks for, and a reload that finds no key changed.
func adoptionOpts(log *adoptionLog, proposed ...domain.AllowRule) Options {
	opts := testOpts
	opts.ProposedRules = proposed
	seams := configSeams(&opts)
	seams.adoptRules = func(rules []domain.AllowRule) error {
		log.adopted = append(log.adopted, rules...)
		return nil
	}
	seams.rejectRules = func(rules []domain.AllowRule) error {
		log.rejected = append(log.rejected, rules...)
		return nil
	}
	seams.proposedRules = func() ([]domain.AllowRule, error) { return log.proposed, nil }
	seams.reloadConfig = func() (ConfigReload, error) { return ConfigReload{}, nil }
	return opts
}

// assertAdoptionPane holds the model to the adoption pane asking about want.
func assertAdoptionPane(t *testing.T, m Model, want domain.AllowRule) {
	t.Helper()
	if !m.picker.open || m.picker.kind != pickerAdoption {
		t.Fatalf("picker = %+v, want the adoption pane open", m.picker)
	}
	if len(m.picker.adoption) == 0 || m.picker.adoption[0] != want {
		t.Fatalf("queue = %v, want %v asked about first", m.picker.adoption, want)
	}
}

// armAdoption delivers the open adoption pane's own arming message, standing in for the backstop
// tick its latch scheduled beside the drain marker (openDecisionLatch) — armApproval's twin, read off
// the model's generation rather than waited out.
func armAdoption(t *testing.T, m Model) Model {
	t.Helper()
	m = step(t, m, approvalArmedMsg{seq: m.approvalSeq})
	if !m.picker.isArmed() {
		t.Fatal("adoption pane did not arm on its own approvalArmedMsg")
	}
	return m
}

// assertNothingAnswered holds the round to having recorded no answer at all.
func assertNothingAnswered(t *testing.T, log *adoptionLog) {
	t.Helper()
	if len(log.adopted) != 0 || len(log.rejected) != 0 {
		t.Fatalf("the pane recorded adopted=%v rejected=%v, want no answer taken", log.adopted, log.rejected)
	}
}

// A start-up with a proposed rule opens the pane on it, under a notice naming every proposal, and
// asks about one rule at a time with the rest queued behind it.
func TestAdoptionPaneOpensAtStartUpWithItsNotice(t *testing.T) {
	t.Parallel()

	m := newTestModelEng(t, &fakeEngine{}, adoptionOpts(&adoptionLog{}, goTestRule, docsRule))

	assertAdoptionPane(t, m, goTestRule)
	if n := m.pickerCount(); n != 3 {
		t.Errorf("rows = %d, want adopt, not now and reject", n)
	}
	if title := m.pickerTitle(); !strings.Contains(title, "`go test`") {
		t.Errorf("title = %q, want the rule being asked about", title)
	}
	if note := lastNote(m); !strings.Contains(note, "`go test`") || !strings.Contains(note, "MCP server `docs`") {
		t.Errorf("notice = %q, want both proposals named", note)
	}
}

// No pane without a proposal, and none without the host that records the answer (ADR 0054
// decision 3a).
func TestAdoptionPaneNeedsAProposalAndAHost(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		mut  func(*Options)
	}{
		{"no proposal", func(o *Options) { o.ProposedRules = nil }},
		{"no host", func(o *Options) { o.Config = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := adoptionOpts(&adoptionLog{}, goTestRule)
			tc.mut(&opts)

			m := newTestModelEng(t, &fakeEngine{}, opts)

			if m.picker.open {
				t.Errorf("picker = %+v, want no adoption pane", m.picker)
			}
		})
	}
}

// Adopt is one AdoptRules call about the rule asked about — the call that brings it into force —
// and the round moves on to the next rule.
func TestAdoptionAdoptRecordsTheRuleAndAdvances(t *testing.T) {
	t.Parallel()
	log := &adoptionLog{}
	m := armAdoption(t, newTestModelEng(t, &fakeEngine{}, adoptionOpts(log, goTestRule, docsRule)))

	m = step(t, m, keyUp()) // "adopt", the row above the "not now" the pane opens on
	m = step(t, m, keyEnter())

	if !reflect.DeepEqual(log.adopted, []domain.AllowRule{goTestRule}) {
		t.Errorf("AdoptRules calls = %v, want the one rule asked about", log.adopted)
	}
	if len(log.rejected) != 0 {
		t.Errorf("adopt rejected %v", log.rejected)
	}
	if note := lastNote(m); !strings.Contains(note, "adopted `go test`") {
		t.Errorf("note = %q, want the adoption said", note)
	}
	assertAdoptionPane(t, m, docsRule)
	assertSettled(t, m)
}

// Reject records the rejection, and a later change that still proposes the same text does not
// raise it again; the same rule with its text changed is a new entry, and is asked about.
func TestAdoptionRejectIsNotReofferedUntilTheTextChanges(t *testing.T) {
	t.Parallel()
	log := &adoptionLog{}
	m := armAdoption(t, newTestModelEng(t, &fakeEngine{}, adoptionOpts(log, goTestRule)))

	m = step(t, m, keyDown()) // "reject", the row below the "not now" the pane opens on
	m = step(t, m, keyEnter())

	if !reflect.DeepEqual(log.rejected, []domain.AllowRule{goTestRule}) {
		t.Fatalf("RejectRules calls = %v, want the one rule asked about", log.rejected)
	}
	if m.picker.open {
		t.Fatalf("picker = %+v, want the round over", m.picker)
	}

	log.proposed = []domain.AllowRule{goTestRule}
	m = step(t, m, configChangedMsg{alive: true})
	if m.picker.open {
		t.Fatalf("picker = %+v, want the rejected text not offered again", m.picker)
	}

	edited := domain.AllowRule{Kind: domain.AllowRuleTerminal, Text: "go test -race", Layer: domain.AllowRuleProject}
	log.proposed = []domain.AllowRule{edited}
	m = step(t, m, configChangedMsg{alive: true})
	assertAdoptionPane(t, m, edited)
}

// Not now and esc persist nothing. Esc ends the round: every rule left is a "not now".
func TestAdoptionNotNowAndEscPersistNothing(t *testing.T) {
	t.Parallel()
	log := &adoptionLog{}
	m := armAdoption(t, newTestModelEng(t, &fakeEngine{}, adoptionOpts(log, goTestRule, docsRule)))

	m = step(t, m, keyEnter()) // "not now", where the pane opens
	assertAdoptionPane(t, m, docsRule)
	m = step(t, m, keyEsc()) // esc is outside the latch: the next rule's pane need not have armed

	if m.picker.open {
		t.Errorf("picker = %+v, want esc to end the round", m.picker)
	}
	if len(log.adopted) != 0 || len(log.rejected) != 0 {
		t.Errorf("a deferred round recorded adopted=%v rejected=%v", log.adopted, log.rejected)
	}
}

// A change saved mid-turn is asked about only once the turn has ended, never over it.
func TestAdoptionAfterAMidTurnChangeWaitsForTheTurnToEnd(t *testing.T) {
	t.Parallel()
	log := &adoptionLog{}
	m := newTestModelEng(t, &fakeEngine{}, adoptionOpts(log))
	m.transcript.addUser("run the tests", nil)
	startStubWorker(t, &m)

	log.proposed = []domain.AllowRule{goTestRule}
	m = step(t, m, configChangedMsg{alive: true})
	if m.picker.open {
		t.Fatalf("picker = %+v, want no pane while the turn runs", m.picker)
	}

	m = step(t, m, exchangeDoneMsg{Result: domain.StepResult{Status: domain.StatusExchangeComplete}})
	assertAdoptionPane(t, m, goTestRule)
}

// A change saved while idle is asked about at once.
func TestAdoptionAfterAnIdleChangeOpensAtOnce(t *testing.T) {
	t.Parallel()
	log := &adoptionLog{}
	m := newTestModelEng(t, &fakeEngine{}, adoptionOpts(log))

	log.proposed = []domain.AllowRule{docsRule}
	m = step(t, m, configChangedMsg{alive: true})

	assertAdoptionPane(t, m, docsRule)
}

// The start-up migration offer keeps the opening frame; the adoption pane follows it the moment it
// is answered.
func TestAdoptionGivesWayToTheKeyMigration(t *testing.T) {
	t.Parallel()
	log := &adoptionLog{}
	w := &configWriteLog{path: "/home/x/.apogee/config.yaml"}
	opts := adoptionOpts(log, goTestRule)
	opts.KeyMigration = KeyMigrationOffer{StoreName: "macOS Keychain", Entries: []string{"workstation"}}
	configSeams(&opts).migrateKey = w.migrate

	m := newTestModelEng(t, &fakeEngine{}, opts)
	if !m.picker.open || m.picker.kind != pickerKeyMigration {
		t.Fatalf("picker = %+v, want the key migration first", m.picker)
	}

	m = step(t, m, keyDown()) // "not now"
	m = step(t, m, keyEnter())

	assertAdoptionPane(t, m, goTestRule)
}

// The pane is LATCHED: a ⏎ delivered in the frame it opens — one typed at whatever stood there before
// it — adopts nothing, even with "adopt" highlighted. The highlight still moves, and once the pane
// arms the same ⏎ takes the row.
func TestAdoptionEnterIsDeadUntilArmed(t *testing.T) {
	t.Parallel()
	log := &adoptionLog{}
	m := newTestModelEng(t, &fakeEngine{}, adoptionOpts(log, goTestRule))
	if m.picker.isArmed() {
		t.Fatal("setup: the pane armed itself; there is no latch left to assert against")
	}

	m = step(t, m, keyUp()) // "adopt" — the arrows are outside the latch
	m = step(t, m, keyEnter())

	assertNothingAnswered(t, log)
	assertAdoptionPane(t, m, goTestRule)
	if m.picker.selected != adoptionAdopt {
		t.Fatalf("selected = %d, want the arrow to have moved the highlight onto adopt", m.picker.selected)
	}

	m = armAdoption(t, m)
	m = step(t, m, keyEnter())

	if !reflect.DeepEqual(log.adopted, []domain.AllowRule{goTestRule}) {
		t.Errorf("AdoptRules calls = %v after the pane armed, want the rule asked about", log.adopted)
	}
}

// The pane opens with "not now" highlighted — the one answer that changes nothing — so the ⏎ that
// does get through before the human has looked costs nothing.
func TestAdoptionOpensOnNotNow(t *testing.T) {
	t.Parallel()
	log := &adoptionLog{}
	m := newTestModelEng(t, &fakeEngine{}, adoptionOpts(log, goTestRule))

	if m.picker.selected != adoptionNotNow {
		t.Fatalf("selected = %d at open, want not now (%d)", m.picker.selected, adoptionNotNow)
	}

	m = step(t, armAdoption(t, m), keyEnter())

	assertNothingAnswered(t, log)
	if note := lastNote(m); !strings.Contains(note, "stays proposed") {
		t.Errorf("note = %q, want the ⏎ on the opening row to be a not now", note)
	}
}

// Each rule of a round gets its own pane, and each pane its own latch: the ⏎ that answered the first
// rule cannot answer the second, the first pane's arming tick arms nothing on the second, and the
// second opens on "not now" too.
func TestAdoptionRearmsForTheNextRule(t *testing.T) {
	t.Parallel()
	log := &adoptionLog{}
	m := armAdoption(t, newTestModelEng(t, &fakeEngine{}, adoptionOpts(log, goTestRule, docsRule)))
	firstSeq := m.approvalSeq

	m = step(t, m, keyUp())
	m, cmd := stepCmd(t, m, keyEnter())

	assertAdoptionPane(t, m, docsRule)
	if cmd == nil {
		t.Fatal("the next rule's pane returned no arming Cmd")
	}
	if m.picker.isArmed() {
		t.Fatal("the next rule's pane opened armed")
	}
	if m.picker.selected != adoptionNotNow {
		t.Errorf("selected = %d on the next rule, want not now (%d)", m.picker.selected, adoptionNotNow)
	}

	m = step(t, m, approvalArmedMsg{seq: firstSeq})
	m = step(t, m, keyUp())
	m = step(t, m, keyEnter())

	if !reflect.DeepEqual(log.adopted, []domain.AllowRule{goTestRule}) {
		t.Fatalf("AdoptRules calls = %v, want only the first rule before the second pane armed", log.adopted)
	}

	m = step(t, armAdoption(t, m), keyEnter())

	if want := []domain.AllowRule{goTestRule, docsRule}; !reflect.DeepEqual(log.adopted, want) {
		t.Errorf("AdoptRules calls = %v, want %v once the second pane armed", log.adopted, want)
	}
}

// The pane arms the way the approval pane does: on the terminal's answer to the SECOND drain marker,
// the one the first answer asks from behind the pane's frame. A ⏎ delivered before either answer, or
// between the two, adopts nothing.
func TestAdoptionArmsOnTheDrainedInputPosition(t *testing.T) {
	t.Parallel()
	log := &adoptionLog{}
	m := newTestModelEng(t, &fakeEngine{}, adoptionOpts(log))
	log.proposed = []domain.AllowRule{goTestRule}
	m, open := stepCmd(t, m, configChangedMsg{alive: true})
	assertAdoptionPane(t, m, goTestRule)
	if open == nil {
		t.Fatal("the pane the Update tail raised returned no arming Cmd")
	}
	m = step(t, m, keyUp())

	m = step(t, m, keyEnter())
	assertNothingAnswered(t, log)

	m, relay := stepCmd(t, m, tea.CursorPositionMsg{})
	if m.picker.isArmed() {
		t.Fatal("the pane armed on the answer to a marker that left ahead of its own frame")
	}
	if relay == nil {
		t.Fatal("the first answer asked no second marker from behind the pane's frame")
	}
	m = step(t, m, keyEnter())
	assertNothingAnswered(t, log)

	m = step(t, m, tea.CursorPositionMsg{})
	if !m.picker.isArmed() {
		t.Fatal("the pane did not arm on the answer to the marker asked behind its frame")
	}
	m = step(t, m, keyEnter())

	if !reflect.DeepEqual(log.adopted, []domain.AllowRule{goTestRule}) {
		t.Errorf("AdoptRules calls = %v after the pane armed, want the rule asked about", log.adopted)
	}
}

// The pane newModel raises at start-up takes its latch there, but construction returns no Cmd: Init
// is what asks the drain marker, and the answers to it arm the pane exactly as for one opened later.
// A start-up that raised no latched pane asks no marker at all.
func TestAdoptionStartUpOpenArmsFromInit(t *testing.T) {
	t.Parallel()
	m := newTestModelEng(t, &fakeEngine{}, adoptionOpts(&adoptionLog{}, goTestRule))
	if m.picker.isArmed() {
		t.Fatal("setup: the start-up pane opened armed")
	}

	if !initAsksDrainMarker(t, m) {
		t.Fatal("Init asked no drain marker for the pane newModel raised")
	}
	m = step(t, m, tea.CursorPositionMsg{})
	m = step(t, m, tea.CursorPositionMsg{})
	if !m.picker.isArmed() {
		t.Fatal("the start-up pane did not arm on the answers to its markers")
	}

	if initAsksDrainMarker(t, newTestModelEng(t, &fakeEngine{}, adoptionOpts(&adoptionLog{}))) {
		t.Error("Init asked a drain marker with no latched pane up")
	}
}

// initAsksDrainMarker runs m's Init and reports whether one of the Cmds it batches asks the terminal
// for a cursor position — the drain marker (approvalDrainMarker). Each Cmd runs on its own goroutine
// and the search stops at the first marker, so the latch's backstop tick, which sleeps for
// approvalArmBackstop, is never waited out; half that is how long a marker gets to show up.
func initAsksDrainMarker(t *testing.T, m Model) bool {
	t.Helper()
	marker := reflect.TypeOf(tea.RequestCursorPosition())
	found := make(chan tea.Msg, 32)
	var run func(tea.Cmd)
	run = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		go func() {
			msg := cmd()
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, c := range batch {
					run(c)
				}
				return
			}
			found <- msg
		}()
	}
	run(m.Init())

	deadline := time.After(approvalArmBackstop / 2)
	for {
		select {
		case msg := <-found:
			if reflect.TypeOf(msg) == marker {
				return true
			}
		case <-deadline:
			return false
		}
	}
}
