package main

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// The four strings type.sh --check regenerated and the golden totals it pinned them to, copied
// here as literals: type.sh is retired with the VHS pipeline, so these must not be read from it.
var typeShGoldens = []struct {
	text    string
	totalMS int64
}{
	{"apogee --mode auto", 775},
	{"the test suite is failing - find the bug, fix it, and prove the tests pass", 3382},
	{"also add a CHANGELOG entry for the fix", 1791},
	{"/undo", 135},
}

// The hero v2 clip's two typed strings (item 13's prompt and queued message), computed once at
// DefaultTypingSeed and pinned rune-wise. The prompt's em dash is one rune in the letter band.
var heroGoldens = []struct {
	text    string
	totalMS int64
}{
	{"tests are failing — send a sub-agent to find out why, then fix it", 3187},
	{"also add a CHANGELOG entry for the fix", 1791},
}

func totalMS(gaps []time.Duration) int64 {
	var total time.Duration
	for _, gap := range gaps {
		total += gap
	}
	return total.Milliseconds()
}

func TestHumanizeTypeShGoldens(t *testing.T) {
	t.Parallel()
	for _, golden := range typeShGoldens {
		t.Run(golden.text, func(t *testing.T) {
			t.Parallel()
			gaps := Humanize(golden.text, DefaultTypingSeed)
			if got := totalMS(gaps); got != golden.totalMS {
				t.Errorf("total = %d ms, want golden %d ms", got, golden.totalMS)
			}
			if want := utf8.RuneCountInString(golden.text) - 1; len(gaps) != want {
				t.Errorf("len(gaps) = %d, want %d (no gap after the last character)", len(gaps), want)
			}
		})
	}
}

func TestHumanizeHeroGoldens(t *testing.T) {
	t.Parallel()
	for _, golden := range heroGoldens {
		t.Run(golden.text, func(t *testing.T) {
			t.Parallel()
			gaps := Humanize(golden.text, DefaultTypingSeed)
			if got := totalMS(gaps); got != golden.totalMS {
				t.Errorf("total = %d ms, want golden %d ms", got, golden.totalMS)
			}
			if want := utf8.RuneCountInString(golden.text) - 1; len(gaps) != want {
				t.Errorf("len(gaps) = %d, want %d rune-wise gaps", len(gaps), want)
			}
		})
	}
}

// TestHumanizeSequenceMatchesTypeSh pins every gap of one string, draw for draw, against the
// Sleep lines type.sh emitted for it — the totals alone could hide two swapped draws.
func TestHumanizeSequenceMatchesTypeSh(t *testing.T) {
	t.Parallel()
	want := []int64{35, 33, 33, 34, 42, 36, 69, 95, 96, 27, 29, 26, 30, 82, 35, 37, 36}
	gaps := Humanize("apogee --mode auto", DefaultTypingSeed)
	if len(gaps) != len(want) {
		t.Fatalf("len(gaps) = %d, want %d", len(gaps), len(want))
	}
	for i, gap := range gaps {
		if gap.Milliseconds() != want[i] {
			t.Errorf("gap %d = %d ms, want %d ms", i, gap.Milliseconds(), want[i])
		}
	}
}

// TestHumanizeProfile asserts the profile the way type.sh --check did: every gap classified by
// the character it follows falls in that character's band, at most two thinking pauses per
// string, and the pooled per-letter mean sits on the letter band's midpoint within 2 ms.
func TestHumanizeProfile(t *testing.T) {
	t.Parallel()
	const pooledMeanToleranceMS = 2.0
	var letterCount, letterSum int64
	for _, golden := range typeShGoldens {
		runes := []rune(golden.text)
		pauses := 0
		for i, gap := range Humanize(golden.text, DefaultTypingSeed) {
			ms := gap.Milliseconds()
			previous := runes[i]
			switch {
			case previous == ' ' && ms >= thinkingPauseMinMS:
				pauses++
				if ms > thinkingPauseMaxMS {
					t.Errorf("%q gap %d: thinking pause %d ms outside %d-%d", golden.text, i, ms, thinkingPauseMinMS, thinkingPauseMaxMS)
				}
			case previous == ' ':
				if ms < spaceGapMinMS || ms > spaceGapMaxMS {
					t.Errorf("%q gap %d: space gap %d ms outside %d-%d", golden.text, i, ms, spaceGapMinMS, spaceGapMaxMS)
				}
			case strings.ContainsRune(punctuationCharacters, previous):
				if ms < punctuationGapMinMS || ms > punctuationGapMaxMS {
					t.Errorf("%q gap %d: punctuation gap %d ms outside %d-%d", golden.text, i, ms, punctuationGapMinMS, punctuationGapMaxMS)
				}
			default:
				if ms < letterGapMinMS || ms > letterGapMaxMS {
					t.Errorf("%q gap %d: letter gap %d ms outside %d-%d", golden.text, i, ms, letterGapMinMS, letterGapMaxMS)
				}
				letterCount++
				letterSum += ms
			}
		}
		if pauses > thinkingPauseLimit {
			t.Errorf("%q takes %d thinking pauses, cap is %d", golden.text, pauses, thinkingPauseLimit)
		}
	}
	if letterCount == 0 {
		t.Fatal("no per-letter draws to pool")
	}
	mean := float64(letterSum) / float64(letterCount)
	target := float64(letterGapMinMS+letterGapMaxMS) / 2
	if mean < target-pooledMeanToleranceMS || mean > target+pooledMeanToleranceMS {
		t.Errorf("pooled per-letter mean %.3f ms over %d draws, want %g ± %g", mean, letterCount, target, pooledMeanToleranceMS)
	}
}

func TestHumanizeEdges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want int
	}{
		{"empty", "", 0},
		{"one rune", "a", 0},
		{"one multibyte rune", "—", 0},
		{"two runes", "ab", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := len(Humanize(tt.text, DefaultTypingSeed)); got != tt.want {
				t.Errorf("len(Humanize(%q)) = %d, want %d", tt.text, got, tt.want)
			}
		})
	}
}

func TestHumanizeRejectsDegenerateSeeds(t *testing.T) {
	t.Parallel()
	for _, seed := range []int64{0, -1, minstdModulus} {
		if err := validTypingSeed(seed); err == nil {
			t.Errorf("validTypingSeed(%d) = nil, want an error", seed)
		}
	}
	if err := validTypingSeed(DefaultTypingSeed); err != nil {
		t.Errorf("validTypingSeed(DefaultTypingSeed) = %v, want nil", err)
	}
	defer func() {
		if recover() == nil {
			t.Error("Humanize with seed 0 did not panic")
		}
	}()
	Humanize("ab", 0)
}
