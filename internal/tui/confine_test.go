package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// /confine routing (through the Model, over a fake Engine)
// ----------------------------------------------------------------------------

// degradedHost is the situation the command exists for: a real backend that cannot fence the
// filesystem here, so Auto gates every terminal command (ISSUES.md, 2026-07-21).
var degradedHost = ConfinementInfo{
	Backend: "landlock",
	Caps:    domain.ConfinementCaps{FSWrite: false, NetworkEgress: false},
	HostID:  "devbox-a1b2c3",
}

// capableHost is the same backend on a machine where confinement actually works — /confine off
// is still the user's to make there, it is simply not the degraded case.
var capableHost = ConfinementInfo{
	Backend: "landlock",
	Caps:    domain.ConfinementCaps{FSWrite: true},
	HostID:  "laptop-9f8e7d",
}

// confineOpts builds display options carrying a host situation and an autonomy mode, leaving
// the rest of testOpts alone.
func confineOpts(info ConfinementInfo, mode domain.Mode) Options {
	opts := testOpts
	opts.Mode = mode
	opts.Confinement = info
	return opts
}

// runConfineLine drives one /confine line through the real key path and returns the model plus
// the plain-text View, so a test asserts on what the human actually sees.
func runConfineLine(t *testing.T, eng *fakeEngine, opts Options, line string) (Model, string) {
	t.Helper()
	m := newTestModelEng(t, eng, opts)
	m.input.SetValue(line)
	m, cmd := stepCmd(t, m, keyEnter())
	if cmd != nil {
		t.Error("/confine returned a Cmd; it is synchronous and must not launch a worker")
	}
	if m.state != stateIdle {
		t.Errorf("state = %v, want idle (/confine must not launch a worker)", m.state)
	}
	if v := m.input.Value(); v != "" {
		t.Errorf("input not cleared: %q", v)
	}
	return m, plain(m.View())
}

func TestConfineOffTogglesTheEngineAndStatesTheBlastRadius(t *testing.T) {
	eng := &fakeEngine{confine: true}
	_, view := runConfineLine(t, eng, confineOpts(degradedHost, domain.ModeAuto), "/confine off")

	if got := eng.confinesSet(); len(got) != 1 || got[0] {
		t.Fatalf("SetConfineToWorkspace calls = %v, want exactly [false]", got)
	}
	for _, want := range []string{"confinement OFF", "unfenced", "privileges"} {
		if !strings.Contains(view, want) {
			t.Errorf("confirmation missing %q — the blast radius must be stated:\n%s", want, view)
		}
	}
}

func TestConfineOnTogglesTheEngineBack(t *testing.T) {
	eng := &fakeEngine{confine: false} // an earlier /confine off in this session
	_, view := runConfineLine(t, eng, confineOpts(degradedHost, domain.ModeAuto), "/confine on")

	if got := eng.confinesSet(); len(got) != 1 || !got[0] {
		t.Fatalf("SetConfineToWorkspace calls = %v, want exactly [true]", got)
	}
	if !strings.Contains(view, "confinement ON") {
		t.Errorf("confirmation missing the re-confined line:\n%s", view)
	}
	// Re-confining on a host that cannot fence means the approval prompts come back; saying so
	// is what keeps the next gated command from reading as a regression.
	if !strings.Contains(view, "approval") {
		t.Errorf("confirmation does not warn that commands ask for approval again:\n%s", view)
	}
}

func TestConfineOffWhenAlreadyOffSaysSo(t *testing.T) {
	eng := &fakeEngine{confine: false}
	_, view := runConfineLine(t, eng, confineOpts(degradedHost, domain.ModeAuto), "/confine off")

	// The request is still asserted on the engine (the user asked for a state, not a
	// transition) but the wording must not imply something changed.
	if got := eng.confinesSet(); len(got) != 1 || got[0] {
		t.Fatalf("SetConfineToWorkspace calls = %v, want exactly [false]", got)
	}
	if !strings.Contains(view, "already off") {
		t.Errorf("confirmation does not say confinement was already off:\n%s", view)
	}
}

func TestConfineOnWhenAlreadyOnSaysSo(t *testing.T) {
	eng := &fakeEngine{confine: true}
	_, view := runConfineLine(t, eng, confineOpts(capableHost, domain.ModeAuto), "/confine on")

	if got := eng.confinesSet(); len(got) != 1 || !got[0] {
		t.Fatalf("SetConfineToWorkspace calls = %v, want exactly [true]", got)
	}
	if !strings.Contains(view, "already on") {
		t.Errorf("confirmation does not say confinement was already on:\n%s", view)
	}
}

func TestConfineOffOnACapableHostIsAllowedAndSaysSo(t *testing.T) {
	eng := &fakeEngine{confine: true}
	_, view := runConfineLine(t, eng, confineOpts(capableHost, domain.ModeAuto), "/confine off")

	if got := eng.confinesSet(); len(got) != 1 || got[0] {
		t.Fatalf("SetConfineToWorkspace calls = %v, want exactly [false] (a capable host may still opt out)", got)
	}
	if !strings.Contains(view, "CAN fence") {
		t.Errorf("confirmation does not say this host could have fenced the commands:\n%s", view)
	}
}

func TestConfineStatusReportsAndTouchesNothing(t *testing.T) {
	for _, line := range []string{"/confine", "/confine status"} {
		t.Run(line, func(t *testing.T) {
			eng := &fakeEngine{confine: true}
			_, view := runConfineLine(t, eng, confineOpts(degradedHost, domain.ModeAuto), line)

			if got := eng.confinesSet(); len(got) != 0 {
				t.Errorf("SetConfineToWorkspace calls = %v, want none (status reports only)", got)
			}
			for _, want := range []string{"landlock", "devbox-a1b2c3", "unavailable", "auto"} {
				if !strings.Contains(view, want) {
					t.Errorf("report missing %q:\n%s", want, view)
				}
			}
		})
	}
}

func TestConfineOffSaveDrivesTheWriterSeam(t *testing.T) {
	eng := &fakeEngine{confine: true}
	calls := 0
	opts := confineOpts(degradedHost, domain.ModeAuto)
	opts.SaveHostAcknowledgement = func() (string, error) {
		calls++
		return "/home/u/.apogee/config.yaml", nil
	}
	_, view := runConfineLine(t, eng, opts, "/confine off --save")

	if calls != 1 {
		t.Errorf("SaveHostAcknowledgement calls = %d, want 1", calls)
	}
	if got := eng.confinesSet(); len(got) != 1 || got[0] {
		t.Fatalf("SetConfineToWorkspace calls = %v, want exactly [false]", got)
	}
	for _, want := range []string{"saved:", "devbox-a1b2c3", "/home/u/.apogee/config.yaml", "delete"} {
		if !strings.Contains(view, want) {
			t.Errorf("confirmation missing %q — a save must be visible and reversible:\n%s", want, view)
		}
	}
}

func TestConfineOffSaveFailureLeavesTheSessionToggleStanding(t *testing.T) {
	eng := &fakeEngine{confine: true}
	opts := confineOpts(degradedHost, domain.ModeAuto)
	opts.SaveHostAcknowledgement = func() (string, error) { return "", errors.New("disk on fire") }
	_, view := runConfineLine(t, eng, opts, "/confine off --save")

	if got := eng.confinesSet(); len(got) != 1 || got[0] {
		t.Fatalf("SetConfineToWorkspace calls = %v, want exactly [false] (the toggle is independent of the save)", got)
	}
	for _, want := range []string{"not saved", "disk on fire", "this session only"} {
		if !strings.Contains(view, want) {
			t.Errorf("confirmation missing %q:\n%s", want, view)
		}
	}
}

func TestConfineOffSaveWithoutAWriterSaysNothingWasWritten(t *testing.T) {
	eng := &fakeEngine{confine: true} // opts.SaveHostAcknowledgement is nil
	_, view := runConfineLine(t, eng, confineOpts(degradedHost, domain.ModeAuto), "/confine off --save")

	if got := eng.confinesSet(); len(got) != 1 || got[0] {
		t.Fatalf("SetConfineToWorkspace calls = %v, want exactly [false]", got)
	}
	if !strings.Contains(view, "not saved") {
		t.Errorf("confirmation claims a save that could not happen:\n%s", view)
	}
}

func TestConfineArgumentErrorReportsTheUsageLine(t *testing.T) {
	eng := &fakeEngine{confine: true}
	_, view := runConfineLine(t, eng, confineOpts(degradedHost, domain.ModeAuto), "/confine sideways")

	if got := eng.confinesSet(); len(got) != 0 {
		t.Errorf("SetConfineToWorkspace calls = %v, want none on a parse error", got)
	}
	if !strings.Contains(view, "sideways") || !strings.Contains(view, "usage:") {
		t.Errorf("the transcript does not teach the grammar after a mistyped line:\n%s", view)
	}
}

// ----------------------------------------------------------------------------
// The note builders (pure)
// ----------------------------------------------------------------------------

func TestConfineStatusReportWording(t *testing.T) {
	cases := []struct {
		name    string
		info    ConfinementInfo
		mode    domain.Mode
		confine bool
		want    []string
		absent  []string
	}{
		{
			name: "degraded host in auto carries the remedy", info: degradedHost,
			mode: domain.ModeAuto, confine: true,
			want: []string{"setting: confined", "fs-write: unavailable",
				"devbox-a1b2c3", "/confine off --save"},
		},
		{
			name: "capable host needs no remedy", info: capableHost,
			mode: domain.ModeAuto, confine: true,
			want:   []string{"fs-write: available", "laptop-9f8e7d"},
			absent: []string{"/confine off --save"},
		},
		{
			name: "already unconfined states the privileges", info: degradedHost,
			mode: domain.ModeAuto, confine: false,
			want:   []string{"unconfined", "full privileges"},
			absent: []string{"/confine off --save"}, // nothing to offer; it is already off
		},
		{
			name: "a lower rung says the setting is not read there", info: degradedHost,
			mode: domain.ModeAskBefore, confine: true,
			want:   []string{"ask before", "read by auto mode only"},
			absent: []string{"/confine off --save"},
		},
		{
			name: "an unwired host situation reports unknown, never a blank",
			info: ConfinementInfo{}, mode: domain.ModeAuto, confine: true,
			want: []string{"unknown backend", "host id: unknown"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := confineStatusReport(c.info, c.mode, c.confine)
			for _, want := range c.want {
				if !strings.Contains(got, want) {
					t.Errorf("report missing %q:\n%s", want, got)
				}
			}
			for _, absent := range c.absent {
				if strings.Contains(got, absent) {
					t.Errorf("report should not carry %q:\n%s", absent, got)
				}
			}
		})
	}
}

func TestConfineToggleNotesNeverClaimARepair(t *testing.T) {
	// TODO constraint 1: the wording states the blast radius; it must never read as fixing a
	// malfunction, because nothing is broken — the ladder is doing what ADR 0012 says.
	notes := []string{
		confineOffNote(degradedHost, domain.ModeAuto, true),
		confineOffNote(capableHost, domain.ModeAuto, true),
		confineOffNote(degradedHost, domain.ModePlan, true),
		confineOnNote(degradedHost, domain.ModeAuto, false),
		confineOnNote(capableHost, domain.ModeAuto, false),
		confineStatusReport(degradedHost, domain.ModeAuto, true),
	}
	for _, note := range notes {
		for _, banned := range []string{"fix", "repair", "broken", "bug"} {
			if strings.Contains(strings.ToLower(note), banned) {
				t.Errorf("note claims a repair (%q):\n%s", banned, note)
			}
		}
	}
}

func TestConfineOffNoteOnALowerRungSaysWhereItApplies(t *testing.T) {
	got := confineOffNote(degradedHost, domain.ModeAllowEdits, true)
	if !strings.Contains(got, "allow edits") || !strings.Contains(got, "auto") {
		t.Errorf("note does not say the setting applies in auto:\n%s", got)
	}
}

func TestConfineOnNoteLeavesThePersistedEntryAlone(t *testing.T) {
	got := confineOnNote(degradedHost, domain.ModeAuto, false)
	if !strings.Contains(got, "unconfined-hosts:") || !strings.Contains(got, "untouched") {
		t.Errorf("note does not say a saved acknowledgement is untouched:\n%s", got)
	}
}
