package mechanisms

import (
	"slices"
	"strings"
	"testing"
)

// unrolledIDs are the spellings the roll must answer "not mine" for: a plausible-looking id that
// was never catalogued, an invented one, and the empty string. They stand where a live catalogue
// row used to stand in these tests — the catalogue is gone, so "on the roll or unknown" is the
// whole question now.
var unrolledIDs = []string{"live_exemplar", "not_a_mechanism", ""}

// The roll names grammar (retired 2026-08-29) and answers IsRetired for it, while an id that was
// never on it answers false — the distinction the tolerant config paths key on.
func TestIsRetiredNamesTheRolledIDsOnly(t *testing.T) {
	t.Parallel()

	if !IsRetired("grammar") {
		t.Errorf("IsRetired(%q) = false, want true — grammar was retired 2026-08-29", "grammar")
	}
	for _, id := range unrolledIDs {
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
	for _, id := range unrolledIDs {
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

	for _, id := range []string{
		"decompose", "stall_nudge", "list_nudge", "tool_use_directive", "guided_decomposition",
		"filehint", "read_loop", "toolfilter", "truncate_history", "error_enrichment", "read_repeat",
		"syntax", "autofix", "library",
	} {
		t.Run(id, func(t *testing.T) {
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

			notices, err := RetiredNotices(map[string]bool{id: true})
			if err != nil {
				t.Fatalf("RetiredNotices(%q): a retired id must be tolerated, got %v", id, err)
			}
			want := `apogee: mechanism "` + id + `" was retired in v0.20.0 and is ignored; remove it from mechanisms:`
			if len(notices) != 1 || notices[0] != want {
				t.Errorf("notices = %q, want [%q]", notices, want)
			}
		})
	}
}

// All SIX rows this wave PROMOTED are on the real roll, each with its release and the Floor-guard
// key that governs the behaviour now — so a saved `mechanisms:` block naming one still starts, and the
// notice it earns names the key rather than telling the user the behaviour is gone. This pins the
// roll itself; the wording is pinned word-for-word below.
func TestPromotedRowsCarryTheirFloorGuardKey(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		id  string
		key string
	}{
		{"validate", "tool-call-repair"},
		{"tool_loop_interceptor", "tool-loop-breaker"},
		{"cached_content_intercept", "read-cache"},
		{"tool_use_enforcer", "tool-use-enforcer"},
		{"empty_response_recovery", "empty-response-recovery"},
		{"tool_result_cap", "tool-result-cap"},
	} {
		t.Run(tc.id, func(t *testing.T) {
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

			notices, err := RetiredNotices(map[string]bool{tc.id: true})
			if err != nil {
				t.Fatalf("RetiredNotices(%q): a promoted id must be tolerated, got %v", tc.id, err)
			}
			if len(notices) != 1 || !strings.Contains(notices[0], tc.key) {
				t.Errorf("notices = %q, want one line naming the floor-guard key %q", notices, tc.key)
			}
		})
	}
}

// RetiredNotices is the door every Driver reaches this package through now that the `mechanisms:`
// key arms nothing (ADR 0076 D11), so the three notice strings a user actually reads are pinned
// here against literals rather than against RetiredRelease lookups: the whole point of the key
// surviving is that yesterday's configuration gets yesterday's sentence, and a literal is the only
// assertion that catches a reworded one.
//
// The PROMOTED-and-switched-off line is the one case where the silence a plain retirement earns
// would mislead: the user wrote a key to turn a behaviour off, it no longer does that, and without
// the line they would be left believing a guard is off when it is on.
func TestRetiredNoticesArePinnedWordForWord(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		enabled map[string]bool
		want    []string
	}{
		{
			name:    "retired outright and asked for",
			enabled: map[string]bool{"grammar": true},
			want: []string{
				`apogee: mechanism "grammar" was retired in v0.18.7 and is ignored; remove it from mechanisms:`,
			},
		},
		{
			name:    "promoted and asked for",
			enabled: map[string]bool{"validate": true},
			want: []string{
				`apogee: mechanism "validate" is the "tool-call-repair" floor guard since v0.20.0 and is on by default; remove it from mechanisms:`,
			},
		},
		{
			name:    "promoted and switched off",
			enabled: map[string]bool{"validate": false},
			want: []string{
				`apogee: mechanism "validate" is the "tool-call-repair" floor guard since v0.20.0; ` +
					`"validate: false" under mechanisms: no longer turns it off — set tool-call-repair: false at the top level`,
			},
		},
		{
			name:    "retired outright and switched off earns no line",
			enabled: map[string]bool{"grammar": false},
			want:    nil,
		},
		{
			name:    "every asked-for id speaks, in sorted spelling",
			enabled: map[string]bool{"validate": true, "grammar": true},
			want: []string{
				`apogee: mechanism "grammar" was retired in v0.18.7 and is ignored; remove it from mechanisms:`,
				`apogee: mechanism "validate" is the "tool-call-repair" floor guard since v0.20.0 and is on by default; remove it from mechanisms:`,
			},
		},
		{
			name:    "no block at all",
			enabled: nil,
			want:    nil,
		},
		{
			name:    "an empty block",
			enabled: map[string]bool{},
			want:    nil,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := RetiredNotices(tt.enabled)

			if err != nil {
				t.Fatalf("RetiredNotices(%v): %v", tt.enabled, err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("RetiredNotices(%v) = %q, want %q", tt.enabled, got, tt.want)
			}
		})
	}
}

// A key that is not on the roll is a loud refusal, and with the catalogue gone the error names
// "(none)" as the known list — the exact tail the empty shipped catalogue already printed, so a
// typo'd key fails startup with the sentence it failed with before. It is refused whichever value
// it carries: the engine never sees these keys at all, so a typo'd DISABLED key would otherwise
// pass unread.
func TestRetiredNoticesUnknownKeyErrorNamesAnEmptyRoster(t *testing.T) {
	t.Parallel()

	for _, value := range []bool{true, false} {
		notices, err := RetiredNotices(map[string]bool{"grammar": true, "nope": value})

		if err == nil {
			t.Fatalf("an unknown key (%v) beside a retired one: want an error, got nil", value)
		}
		const want = `apogee: unknown mechanism "nope"; known: (none)`
		if err.Error() != want {
			t.Errorf("RetiredNotices error = %q, want %q", err, want)
		}
		if notices != nil {
			t.Errorf("RetiredNotices notices = %q on a refused block, want none", notices)
		}
	}
}
