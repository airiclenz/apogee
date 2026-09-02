package mechanisms

import (
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

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
	for _, id := range []domain.MechanismID{"validate", "not_a_mechanism", ""} {
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

// fakeKnown is a stand-in catalogue for the pure key-validation tests: ResolveEnabled only checks a
// `mechanisms:` key against the known set and selects the enabled ones (the engine builds, so no
// constructor is needed here — the unknown-ID cases below drive the REAL catalogue via KnownIDs).
var fakeKnown = []domain.MechanismID{"alpha", "beta", "off"}

// floorPlus is the off-ramp floor with extra ids folded in, sorted — the shape ResolveEnabled hands
// back for a block that enables those ids, spelled once so a case reads as "the floor plus these".
func floorPlus(extra ...domain.MechanismID) []domain.MechanismID {
	out := append(OffRampFloor(nil), extra...)
	slices.Sort(out)
	return out
}

// The floor is exactly the catalogue's CapOffRamp rows, sorted, and an explicit `false` is the only
// thing that removes one: a `true` and an absent key both leave the row standing, and a row of any
// other Capability never joins (ADR 0070's one exception to D1, harvested rather than hand-listed).
func TestOffRampFloorIsTheCapOffRampRows(t *testing.T) {
	t.Parallel()

	var want []domain.MechanismID
	for _, d := range Descriptors() {
		if d.Capability == domain.CapOffRamp {
			want = append(want, d.ID)
		}
	}
	if len(want) == 0 {
		t.Fatal("no CapOffRamp row in the catalogue; the floor would be vacuous")
	}
	for _, block := range []map[string]bool{nil, {}, {"validate": true}, {string(want[0]): true}} {
		if got := OffRampFloor(block); !slices.Equal(got, want) {
			t.Errorf("OffRampFloor(%+v) = %v, want %v", block, got, want)
		}
	}
	if got := OffRampFloor(map[string]bool{string(want[0]): false}); !slices.Equal(got, want[1:]) {
		t.Errorf("OffRampFloor(%q: false) = %v, want %v — an explicit false removes exactly that row",
			want[0], got, want[1:])
	}
}

// An enabled ID is selected; a `false` entry is not. ResolveEnabled returns the enabled IDs in
// sorted canonical order for Config.EnableMechanisms — the engine builds them (ADR 0015 §1) — with
// the off-ramp floor unioned in, so what a block names arrives BESIDE the two default-on off-ramps
// rather than instead of them (ADR 0070).
func TestResolveEnabledEnablesOnlyTrue(t *testing.T) {
	t.Parallel()
	ids, _, err := ResolveEnabled(map[string]bool{"alpha": true, "beta": false}, fakeKnown)
	if err != nil {
		t.Fatalf("ResolveEnabled: %v", err)
	}
	want := append([]domain.MechanismID{"alpha"}, OffRampFloor(nil)...)
	slices.Sort(want)
	if !slices.Equal(ids, want) {
		t.Errorf("ResolveEnabled = %v; want %v (the `false` entry is skipped, the floor is added)", ids, want)
	}
}

// Nothing enabled ⇒ the OFF-RAMP FLOOR and nothing else (ADR 0070): an absent block, an empty one,
// and one that only switches an unrelated row off all resolve to the two off-ramps, because those
// are recovery guarantees rather than small-model tuning. Every other Capability still defaults off
// (D1) — a KNOWN key mapped to false selects nothing, disabled Mechanisms being validated by name.
func TestResolveEnabledDefaultsToTheOffRampFloor(t *testing.T) {
	t.Parallel()
	want := OffRampFloor(nil)
	if len(want) == 0 {
		t.Fatal("OffRampFloor(nil) is empty; the catalogue should carry two off-ramps")
	}
	for _, enabled := range []map[string]bool{nil, {}, {"off": false}} {
		ids, _, err := ResolveEnabled(enabled, fakeKnown)
		if err != nil {
			t.Fatalf("ResolveEnabled(%+v): %v", enabled, err)
		}
		if !slices.Equal(ids, want) {
			t.Errorf("ResolveEnabled(%+v) = %v; want the off-ramp floor %v", enabled, ids, want)
		}
	}
}

// An explicit `false` on an off-ramp is the one thing that takes it off the floor, and it takes only
// that one: the block is validated against the REAL catalogue here, since the floor names real rows
// and a fake known-list would refuse the key as unknown before the floor was ever reached.
func TestResolveEnabledExplicitFalseRemovesOneOffRamp(t *testing.T) {
	t.Parallel()
	floor := OffRampFloor(nil)
	if len(floor) < 2 {
		t.Fatalf("OffRampFloor(nil) = %v; want at least two rows for this case", floor)
	}
	off := floor[0]

	ids, _, err := ResolveEnabled(map[string]bool{string(off): false}, KnownIDs())
	if err != nil {
		t.Fatalf("ResolveEnabled(%q: false): %v", off, err)
	}
	if !slices.Equal(ids, floor[1:]) {
		t.Errorf("ResolveEnabled(%q: false) = %v; want the rest of the floor %v", off, ids, floor[1:])
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

	_, _, err := ResolveEnabled(map[string]bool{"typo": false}, KnownIDs())
	if err == nil {
		t.Fatal(`{"typo": false}: want a startup error, got nil`)
	}
	if !strings.Contains(err.Error(), `"typo"`) {
		t.Errorf("error = %q, want it to name the unknown key", err)
	}
	if !strings.Contains(err.Error(), "validate") {
		t.Errorf("error = %q, want it to list the known catalogue (e.g. %q)", err, "validate")
	}

	// The same key spelled correctly and disabled is fine: validated by name, never enabled. What
	// comes back is the off-ramp floor alone — switching a non-off-ramp row off adds nothing and
	// takes nothing away (ADR 0070).
	ids, _, err := ResolveEnabled(map[string]bool{"validate": false}, KnownIDs())
	if err != nil {
		t.Fatalf(`{"validate": false}: %v`, err)
	}
	if want := OffRampFloor(nil); !slices.Equal(ids, want) {
		t.Errorf(`{"validate": false} = %v; want the off-ramp floor %v (a disabled Mechanism is never enabled)`, ids, want)
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
// Everything alongside it in the block still arms, and so does the off-ramp floor: a retired row is
// not in the catalogue, so it can neither join the floor nor take anything off it (ADR 0070).
func TestResolveEnabledRetiredIDIsDropped(t *testing.T) {
	for _, tt := range []struct {
		name    string
		enabled map[string]bool
		want    []domain.MechanismID
	}{
		{"retired and asked for", map[string]bool{"grammar": true}, OffRampFloor(nil)},
		{"retired and switched off", map[string]bool{"grammar": false}, OffRampFloor(nil)},
		{"retired beside a live row", map[string]bool{"grammar": true, "validate": true}, floorPlus("validate")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var ids []domain.MechanismID
			var err error

			printed := captureStderr(t, func() {
				ids, _, err = ResolveEnabled(tt.enabled, KnownIDs())
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

	_, got, err := ResolveEnabled(map[string]bool{"grammar": true, "validate": true}, KnownIDs())
	if err != nil {
		t.Fatalf("ResolveEnabled: %v", err)
	}

	want := []string{
		`apogee: mechanism "grammar" was retired in ` + RetiredRelease + ` and is ignored; remove it from mechanisms:`,
	}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("ResolveEnabled notices = %q, want %q", got, want)
	}

	for _, quiet := range []map[string]bool{nil, {}, {"grammar": false}, {"validate": true}} {
		_, lines, err := ResolveEnabled(quiet, KnownIDs())
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
