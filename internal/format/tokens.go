package format

import (
	"math"
	"strconv"
)

const (
	// unitStep is the divisor between two rungs of the ladder. Binary, not decimal: see the
	// package doc for why a context window is a power of two and must read as one.
	unitStep = 1024

	// unitCeiling is the rule itself — no rung ever displays a value of 1000 or more, so a
	// count climbs to the next unit as soon as its rounded spelling would reach it. It also
	// bounds the bare rung, which is just the ladder's implicit bottom step.
	unitCeiling = 1000

	// coarseDecimalLimit is where the coarse form stops spending a decimal place: below ten the
	// tenth still carries information ("1.1M"), at ten and above it is noise ("15M").
	coarseDecimalLimit = 10
)

// unitSuffixes are the ladder's rungs above the bare one, smallest first.
var unitSuffixes = [...]string{"k", "M", "G"}

// Tokens renders a token or context-window count in the coarse form: "" for a non-positive
// count (the unknown sentinel), a bare integer below a thousand, then whole k and — where a
// tenth still says something — one decimal of M or G. It is the default; reach for TokensFine
// only where a count is compared against another.
func Tokens(n int) string { return tokens(n, false) }

// TokensFine renders a count the way a COMPARISON needs it — one decimal in every unit ("4.1k",
// "32.0k", "1.2M") — so the two sides of a narrow overrun do not round to the same spelling. It
// shares Tokens' sentinel, ladder and unit cap.
func TokensFine(n int) string { return tokens(n, true) }

// tokens climbs the ladder and returns the first spelling that stays under the ceiling. The top
// rung is the last word: G has nothing above it, so an absurd count spells out wide rather than
// silently losing its unit.
func tokens(n int, fine bool) string {
	if n <= 0 {
		return ""
	}
	if n < unitCeiling {
		return strconv.Itoa(n)
	}
	v, text := float64(n), ""
	for rung, suffix := range unitSuffixes {
		v /= unitStep
		spelled, shown := spell(v, suffix, places(v, rung, fine))
		text = spelled
		if shown < unitCeiling {
			break
		}
	}
	return text
}

// places reports how many decimal places a value takes at the given rung (0 = k). The fine form
// always keeps one. The coarse form keeps one only above k, and only while the value stays under
// ten and has a decimal worth printing — "1.1M", against "1M", "15M" and "32k".
func places(v float64, rung int, fine bool) int {
	if fine {
		return 1
	}
	if rung == 0 {
		return 0
	}
	if d := round(v, 1); d < coarseDecimalLimit && d != math.Trunc(d) {
		return 1
	}
	return 0
}

// spell renders v with p decimal places and its unit suffix, and reports the number the spelling
// shows. That reported value — not v — is what the ceiling is tested against, because it is the
// spelling that must stay under a thousand: 999.6k rounds to "1000k", and that is the case the
// test has to catch.
func spell(v float64, suffix string, p int) (string, float64) {
	shown := round(v, p)
	return strconv.FormatFloat(shown, 'f', p, 64) + suffix, shown
}

// round rounds v to p decimal places, half away from zero — the arithmetic a reader expects of a
// displayed number, where 1999 becomes "2k" rather than the "1k" truncation would give.
func round(v float64, p int) float64 {
	scale := 1.0
	if p > 0 {
		scale = math.Pow(10, float64(p))
	}
	return math.Round(v*scale) / scale
}
