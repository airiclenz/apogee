package mechanisms

import (
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// liveExemplarID names a Mechanism that exists only for these tests: a LIVE catalogue row, in a
// test-private table, standing in for "some row this build still ships". The tests below need one
// to prove the roll and the catalogue are different questions — a live row answers false to
// IsRetired, resolves as enabled, and is listed by an unknown-key error — and naming a real
// shipped row would make the proof expire the moment that row retires. The shipped catalogue is
// on its way to empty (ADR 0071), so the stand-in is permanent rather than a stopgap.
const liveExemplarID domain.MechanismID = "live_exemplar"

// exemplarCatalogue is that stand-in's table, registered through the same seam the production rows
// use so the row is shaped exactly as a real one. It is never the production catalogue: registering
// into that would ship a Mechanism nobody asked for.
var exemplarCatalogue = func() map[domain.MechanismID]row {
	table := map[domain.MechanismID]row{}
	registerIn(table, row{
		descriptor: domain.MechanismDescriptor{
			ID:          liveExemplarID,
			Capability:  domain.CapResponseRepair,
			Suppression: domain.SuppressStrikesThree,
		},
		construct: func(Deps) (any, error) { return struct{}{}, nil },
	})
	return table
}()

// exemplarKnown is the known-ID list ResolveEnabled validates against in these tests — the
// stand-in row alone, which is all a key-validation proof needs.
func exemplarKnown() []domain.MechanismID { return knownIDs(exemplarCatalogue) }

// A retired ID is never also a catalogue row: the two lists partition the IDs this build recognises,
// so a caller that drops the retired ones and refuses the rest cannot silently drop a live Mechanism.
func TestRetiredIDsAreNotInTheCatalogue(t *testing.T) {
	t.Parallel()

	known := KnownIDs()
	for _, id := range RetiredIDs() {
		if slices.Contains(known, id) {
			t.Errorf("%q is both retired and a catalogue row; the roll and the catalogue must be disjoint", id)
		}
	}
}

// The roll names grammar (retired 2026-08-29) and answers IsRetired for it, while a live row and an
// invented ID both answer false — the distinction the tolerant config paths key on.
func TestIsRetiredNamesTheRolledIDsOnly(t *testing.T) {
	t.Parallel()

	if !IsRetired("grammar") {
		t.Errorf("IsRetired(%q) = false, want true — grammar was retired 2026-08-29", "grammar")
	}
	for _, id := range []domain.MechanismID{liveExemplarID, "not_a_mechanism", ""} {
		if IsRetired(id) {
			t.Errorf("IsRetired(%q) = true, want false", id)
		}
	}
}

// RetiredIDs hands out a copy, so a caller that sorts or truncates its answer cannot edit the roll
// every other caller reads.
func TestRetiredIDsIsACopy(t *testing.T) {
	t.Parallel()

	first := RetiredIDs()
	if len(first) == 0 {
		t.Fatal("RetiredIDs() is empty; grammar should be on the roll")
	}
	first[0] = "clobbered"

	if second := RetiredIDs(); slices.Contains(second, "clobbered") {
		t.Errorf("RetiredIDs() returned the roll itself: %v", second)
	}
}

// RetiredRelease and Successor answer PER ID: grammar retired outright in v0.18.7 and has no
// successor, while an ID that is not on the roll at all answers "" to both rather than handing back
// whichever release happened to be last.
func TestRetiredReleaseAndSuccessorAnswerPerID(t *testing.T) {
	t.Parallel()

	if got := RetiredRelease("grammar"); got != "v0.18.7" {
		t.Errorf("RetiredRelease(%q) = %q, want %q", "grammar", got, "v0.18.7")
	}
	if got := Successor("grammar"); got != "" {
		t.Errorf("Successor(%q) = %q, want \"\" — grammar retired outright", "grammar", got)
	}
	for _, id := range []domain.MechanismID{liveExemplarID, "not_a_mechanism", ""} {
		if got := RetiredRelease(id); got != "" {
			t.Errorf("RetiredRelease(%q) = %q, want \"\" — it is not on the roll", id, got)
		}
		if got := Successor(id); got != "" {
			t.Errorf("Successor(%q) = %q, want \"\" — it is not on the roll", id, got)
		}
	}
}

// The rows this wave retired OUTRIGHT are on the real roll too, each with its release and NO
// successor — a `mechanisms:` block still naming one starts, arms nothing, and earns the plain line
// that names the release the user would look up rather than a key that does not exist.
func TestRetiredOutrightRowsCarryTheirReleaseAndNoSuccessor(t *testing.T) {
	t.Parallel()

	for _, id := range []domain.MechanismID{
		"decompose", "stall_nudge", "list_nudge", "tool_use_directive", "guided_decomposition",
	} {
		t.Run(string(id), func(t *testing.T) {
			t.Parallel()

			if !IsRetired(id) {
				t.Fatalf("IsRetired(%q) = false; a retired row must stay on the roll so a saved config still starts", id)
			}
			if got := RetiredRelease(id); got != "v0.20.0" {
				t.Errorf("RetiredRelease(%q) = %q, want %q", id, got, "v0.20.0")
			}
			if got := Successor(id); got != "" {
				t.Errorf("Successor(%q) = %q, want \"\" — the row retired outright, it was not promoted", id, got)
			}

			ids, notices, err := ResolveEnabled(map[string]bool{string(id): true}, exemplarKnown())
			if err != nil {
				t.Fatalf("ResolveEnabled(%q): a retired id must be tolerated, got %v", id, err)
			}
			if len(ids) != 0 {
				t.Errorf("ResolveEnabled armed the retired id %q: %v", id, ids)
			}
			want := `apogee: mechanism "` + string(id) + `" was retired in v0.20.0 and is ignored; remove it from mechanisms:`
			if len(notices) != 1 || notices[0] != want {
				t.Errorf("notices = %q, want [%q]", notices, want)
			}
		})
	}
}

// The rows this wave PROMOTED are on the real roll, each with its release and the Floor-guard key
// that governs the behaviour now — so a saved `mechanisms:` block naming one still starts, and the
// notice it earns names the key rather than telling the user the behaviour is gone. This pins the
// roll itself; the wording is pinned over a synthetic row below.
func TestPromotedRowsCarryTheirFloorGuardKey(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		id  domain.MechanismID
		key string
	}{
		{"validate", "tool-call-repair"},
		{"tool_loop_interceptor", "tool-loop-breaker"},
		{"cached_content_intercept", "read-cache"},
	} {
		t.Run(string(tc.id), func(t *testing.T) {
			t.Parallel()

			if !IsRetired(tc.id) {
				t.Fatalf("IsRetired(%q) = false; a promoted row must stay on the roll so a saved config still starts", tc.id)
			}
			if got := RetiredRelease(tc.id); got != "v0.20.0" {
				t.Errorf("RetiredRelease(%q) = %q, want %q", tc.id, got, "v0.20.0")
			}
			if got := Successor(tc.id); got != tc.key {
				t.Errorf("Successor(%q) = %q, want the floor-guard key %q", tc.id, got, tc.key)
			}

			ids, notices, err := ResolveEnabled(map[string]bool{string(tc.id): true}, exemplarKnown())
			if err != nil {
				t.Fatalf("ResolveEnabled(%q): a promoted id must be tolerated, got %v", tc.id, err)
			}
			if slices.Contains(ids, tc.id) {
				t.Errorf("ResolveEnabled armed the promoted id %q: %v", tc.id, ids)
			}
			if len(notices) != 1 || !strings.Contains(notices[0], tc.key) {
				t.Errorf("notices = %q, want one line naming the floor-guard key %q", notices, tc.key)
			}
		})
	}
}

// withRoll swaps the package roll for the duration of one test and puts it back afterwards, so a
// wording assertion is about the wording rather than about whichever IDs happen to be rolled today.
// The caller must NOT be a parallel test: the roll is a package global, so this is only race-free
// during the sequential test phase (the captureStderr precedent below).
func withRoll(t *testing.T, rows ...retiredRow) {
	t.Helper()
	orig := retired
	retired = rows
	t.Cleanup(func() { retired = orig })
}

// A PROMOTED row — one retired because its behaviour became a Floor guard — earns a notice naming
// the top-level key that governs the behaviour now, in BOTH directions. Asking for it says the
// guard is already on; switching it OFF says the old spelling no longer does that and names the key
// that does, which is the one case where the silence a plain retirement earns would mislead: the
// user would be left believing a guard is off when it is on. A row retired outright keeps the plain
// wording and its own release, and stays silent when set false.
func TestResolveEnabledNoticesNameAPromotedRowsFloorGuardKey(t *testing.T) {
	withRoll(t,
		retiredRow{ID: "grammar", Release: "v0.18.7"},
		retiredRow{ID: "validate", Release: "v0.20.0", Successor: "tool-call-repair"},
	)

	for _, tt := range []struct {
		name    string
		enabled map[string]bool
		want    []string
	}{
		{
			"promoted and asked for",
			map[string]bool{"validate": true},
			[]string{`apogee: mechanism "validate" is the "tool-call-repair" floor guard since v0.20.0 and is on by default; remove it from mechanisms:`},
		},
		{
			"promoted and switched off",
			map[string]bool{"validate": false},
			[]string{`apogee: mechanism "validate" is the "tool-call-repair" floor guard since v0.20.0; "validate: false" under mechanisms: no longer turns it off — set tool-call-repair: false at the top level`},
		},
		{
			"retired outright keeps the plain wording and its own release",
			map[string]bool{"grammar": true},
			[]string{`apogee: mechanism "grammar" was retired in v0.18.7 and is ignored; remove it from mechanisms:`},
		},
		{
			"retired outright and switched off stays silent",
			map[string]bool{"grammar": false},
			nil,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, got, err := ResolveEnabled(tt.enabled, KnownIDs())
			if err != nil {
				t.Fatalf("ResolveEnabled(%v): %v", tt.enabled, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ResolveEnabled(%v) notices = %q, want %q", tt.enabled, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("notice[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// fakeKnown is a stand-in catalogue for the pure key-validation tests: ResolveEnabled only checks a
// `mechanisms:` key against the known set and selects the enabled ones (the engine builds, so no
// constructor is needed here — the unknown-ID cases below drive the REAL catalogue via KnownIDs).
var fakeKnown = []domain.MechanismID{"alpha", "beta", "off"}

// An enabled ID is selected; a `false` entry is not. ResolveEnabled returns the enabled IDs in
// sorted canonical order for Config.EnableMechanisms — the engine builds them (ADR 0015 §1) — and
// nothing beside them: no catalogued row is on by default, so what a block names is exactly what is
// armed, the Floor guards being Config.Floor's own keys (ADR 0071).
func TestResolveEnabledEnablesOnlyTrue(t *testing.T) {
	t.Parallel()
	ids, _, err := ResolveEnabled(map[string]bool{"alpha": true, "beta": false}, fakeKnown)
	if err != nil {
		t.Fatalf("ResolveEnabled: %v", err)
	}
	want := []domain.MechanismID{"alpha"}
	if !slices.Equal(ids, want) {
		t.Errorf("ResolveEnabled = %v; want %v (the `false` entry is skipped, and nothing is floored in)", ids, want)
	}
}

// Nothing enabled ⇒ NOTHING (D1): an absent block, an empty one, and one that only switches a row
// off all resolve to no Mechanisms at all, no catalogued row being on by default since the two
// recoveries that were became Floor guards (ADR 0071). A KNOWN key mapped to false selects nothing, disabled
// Mechanisms being validated by name.
func TestResolveEnabledDefaultsToNothing(t *testing.T) {
	t.Parallel()
	for _, enabled := range []map[string]bool{nil, {}, {"off": false}} {
		ids, _, err := ResolveEnabled(enabled, fakeKnown)
		if err != nil {
			t.Fatalf("ResolveEnabled(%+v): %v", enabled, err)
		}
		if len(ids) != 0 {
			t.Errorf("ResolveEnabled(%+v) = %v; want nothing armed", enabled, ids)
		}
	}
}

// An unknown ENABLED ID is a loud startup error — proven against the real catalogue via KnownIDs, so
// a typo'd `mechanisms:` key fails startup rather than silently vanishing.
func TestResolveEnabledUnknownIDErrors(t *testing.T) {
	t.Parallel()
	_, _, err := ResolveEnabled(map[string]bool{"nope": true}, KnownIDs())
	if err == nil {
		t.Fatal("enabling an unknown mechanism: want an error, got nil")
	}
}

// A typo'd key mapped to FALSE is a startup error too (phase-4-review-fixes item 5): the
// disabled-key validation lives here because the engine only ever sees the ENABLED IDs. The error
// lists the real catalogue's known IDs; a valid disabled key still selects nothing — validated
// against KnownIDs.
func TestResolveEnabledUnknownDisabledKeyErrors(t *testing.T) {
	t.Parallel()

	_, _, err := ResolveEnabled(map[string]bool{"typo": false}, exemplarKnown())
	if err == nil {
		t.Fatal(`{"typo": false}: want a startup error, got nil`)
	}
	if !strings.Contains(err.Error(), `"typo"`) {
		t.Errorf("error = %q, want it to name the unknown key", err)
	}
	if !strings.Contains(err.Error(), string(liveExemplarID)) {
		t.Errorf("error = %q, want it to list the known catalogue (e.g. %q)", err, liveExemplarID)
	}

	// The same key spelled correctly and disabled is fine: validated by name, never enabled. Nothing
	// comes back at all — switching a row off adds nothing, and no catalogued row is on by default.
	ids, _, err := ResolveEnabled(map[string]bool{string(liveExemplarID): false}, exemplarKnown())
	if err != nil {
		t.Fatalf(`{%q: false}: %v`, liveExemplarID, err)
	}
	if len(ids) != 0 {
		t.Errorf(`{%q: false} = %v; want nothing armed (a disabled Mechanism is never enabled)`, liveExemplarID, ids)
	}
}

// An empty catalogue renders "(none)" in the unknown-key error rather than a dangling tail, so the
// message reads the same as the engine's own — the case a Driver hits before anything is ported.
func TestResolveEnabledUnknownIDNamesAnEmptyCatalogue(t *testing.T) {
	t.Parallel()

	_, _, err := ResolveEnabled(map[string]bool{"nope": true}, nil)
	if err == nil {
		t.Fatal("unknown mechanism against an empty catalogue: want an error, got nil")
	}
	if !strings.Contains(err.Error(), "known: (none)") {
		t.Errorf("error = %q, want it to render the empty catalogue as %q", err, "(none)")
	}
}

// A `mechanisms:` key naming a RETIRED Mechanism is DROPPED, not refused: the key was valid at the
// release before the removal, so a config the user never edited must still start. It is dropped
// whichever value it carries, it never reaches Config.EnableMechanisms, and the resolver itself says
// nothing — several of its call paths run with the alt screen up, where stderr paints over the TUI.
// Everything alongside it in the block still arms: a retired row is not in the catalogue, so it can
// neither arm anything nor take anything away.
func TestResolveEnabledRetiredIDIsDropped(t *testing.T) {
	for _, tt := range []struct {
		name    string
		enabled map[string]bool
		want    []domain.MechanismID
	}{
		{"retired and asked for", map[string]bool{"grammar": true}, nil},
		{"retired and switched off", map[string]bool{"grammar": false}, nil},
		{"retired beside a live row", map[string]bool{"grammar": true, string(liveExemplarID): true}, []domain.MechanismID{liveExemplarID}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var ids []domain.MechanismID
			var err error

			printed := captureStderr(t, func() {
				ids, _, err = ResolveEnabled(tt.enabled, exemplarKnown())
			})

			if err != nil {
				t.Fatalf("ResolveEnabled(%v): a retired id must be tolerated, got %v", tt.enabled, err)
			}
			if len(ids) != len(tt.want) {
				t.Fatalf("ResolveEnabled(%v) = %v, want %v", tt.enabled, ids, tt.want)
			}
			for i := range tt.want {
				if ids[i] != tt.want[i] {
					t.Errorf("ResolveEnabled(%v)[%d] = %q, want %q", tt.enabled, i, ids[i], tt.want[i])
				}
			}
			if printed != "" {
				t.Errorf("ResolveEnabled wrote to stderr: %q — the caller decides where the notices go", printed)
			}
		})
	}
}

// The notice names every retired id the block turns ON, one line each, in sorted spelling — and says
// what it is and what to do about it. A retired id set to FALSE earns no line (the user is not asking
// for it), and neither does a live row or an empty block.
func TestResolveEnabledNoticesNameEachRetiredID(t *testing.T) {
	t.Parallel()

	_, got, err := ResolveEnabled(map[string]bool{"grammar": true, string(liveExemplarID): true}, exemplarKnown())
	if err != nil {
		t.Fatalf("ResolveEnabled: %v", err)
	}

	want := []string{
		`apogee: mechanism "grammar" was retired in ` + RetiredRelease("grammar") + ` and is ignored; remove it from mechanisms:`,
	}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("ResolveEnabled notices = %q, want %q", got, want)
	}

	for _, quiet := range []map[string]bool{nil, {}, {"grammar": false}, {string(liveExemplarID): true}} {
		_, lines, err := ResolveEnabled(quiet, exemplarKnown())
		if err != nil {
			t.Fatalf("ResolveEnabled(%v): %v", quiet, err)
		}
		if lines != nil {
			t.Errorf("ResolveEnabled(%v) notices = %q, want no lines", quiet, lines)
		}
	}
}

// A refused block yields no notices: an unknown key stops the resolution before anything is
// tolerated, so a caller that prints the second return value cannot narrate a config it just
// rejected.
func TestResolveEnabledUnknownKeyReturnsNoNotices(t *testing.T) {
	t.Parallel()

	ids, notices, err := ResolveEnabled(map[string]bool{"grammar": true, "nope": true}, KnownIDs())
	if err == nil {
		t.Fatal("an unknown key beside a retired one: want an error, got nil")
	}
	if ids != nil || notices != nil {
		t.Errorf("ResolveEnabled = (%v, %q) on error, want (nil, nil)", ids, notices)
	}
}

// captureStderr swaps the process os.Stderr for a pipe, runs f, and returns everything f wrote to
// stderr. The caller must NOT be a parallel test: os.Stderr is a process-global, so this is only
// race-free during the sequential test phase (the internal/library and cmd/apogee precedent).
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer func() { _ = r.Close() }()
	os.Stderr = w
	// A t.Fatal or t.Skip inside f is a runtime.Goexit, which unwinds past the restore below
	// exactly as a panic does. This cleanup runs on every exit path: it closes the write end — which
	// ends the reader goroutine — and puts the process stderr back. It is idempotent with the
	// happy-path restore, which stays so the captured string is still returned in order.
	t.Cleanup(func() {
		_ = w.Close()
		os.Stderr = orig
	})
	captured := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		captured <- string(b)
	}()

	f()

	_ = w.Close()
	os.Stderr = orig
	return <-captured
}
