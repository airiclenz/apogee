package tui

import (
	"reflect"
	"strings"
	"testing"

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
	m := newTestModelEng(t, &fakeEngine{}, adoptionOpts(log, goTestRule, docsRule))

	m = step(t, m, keyEnter()) // "adopt" is the first row

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
	m := newTestModelEng(t, &fakeEngine{}, adoptionOpts(log, goTestRule))

	m = step(t, m, keyDown())
	m = step(t, m, keyDown()) // "reject"
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
	m := newTestModelEng(t, &fakeEngine{}, adoptionOpts(log, goTestRule, docsRule))

	m = step(t, m, keyDown()) // "not now"
	m = step(t, m, keyEnter())
	assertAdoptionPane(t, m, docsRule)
	m = step(t, m, keyEsc())

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
