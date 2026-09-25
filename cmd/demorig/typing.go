package main

import (
	"fmt"
	"strings"
	"time"
)

// The humanized-typing profile, ported from graphics/demo/type.sh so a storyboard's typed text
// keeps the rhythm the VHS takes had. A take types each character, then waits the gap drawn for
// it; the gaps come from four fixed bands so the rhythm can be asserted, and from a seeded
// generator so two machines recording the same storyboard type on identical beats.
//
// The generator is MINSTD (Lehmer, multiplier 16807, modulus 2^31-1) spelled out rather than
// delegated to math/rand, because the sequence is the contract: it must match type.sh draw for
// draw so the goldens that script pinned still hold here.
const (
	// DefaultTypingSeed is the seed type.sh typed every hero string at.
	DefaultTypingSeed = 4242

	minstdMultiplier = 16807
	minstdModulus    = 2147483647

	letterGapMinMS      = 25
	letterGapMaxMS      = 45
	spaceGapMinMS       = 60
	spaceGapMaxMS       = 90
	punctuationGapMinMS = 90
	punctuationGapMaxMS = 140
	thinkingPauseMinMS  = 300
	thinkingPauseMaxMS  = 500

	// punctuationCharacters take the longer gap after them. The set is deliberately ASCII only:
	// an em dash types in the letter band, as it would have to under type.sh's byte table.
	punctuationCharacters = ".,-!"

	// A thinking pause replaces the gap after a space one time in eight, and never more than
	// twice in one string — a third reads as the tool stalling rather than the user thinking.
	thinkingPauseOdds  = 8
	thinkingPauseLimit = 2
)

// minstd is the Lehmer generator type.sh carries in awk. Every product stays under 2^46, so the
// int64 arithmetic is exact.
type minstd struct {
	state int64
}

func (g *minstd) advance() int64 {
	g.state = (minstdMultiplier * g.state) % minstdModulus
	return g.state
}

// draw returns a value in [low, high], inclusive, exactly as type.sh's draw() computes it.
func (g *minstd) draw(low, high int64) int64 {
	return low + g.advance()%(high-low+1)
}

// validTypingSeed reports whether seed is a usable MINSTD state: 0 is a fixed point and the
// modulus maps to 0, so either would type the whole string on one flat interval.
func validTypingSeed(seed int64) error {
	if seed < 1 || seed >= minstdModulus {
		return fmt.Errorf("typing seed must be in 1..%d, got %d", minstdModulus-1, seed)
	}
	return nil
}

// Humanize returns the gap to wait after each character of s but the last: element i is the
// pause between rune i and rune i+1, so a string of n runes yields n-1 gaps and a one-rune or
// empty string yields none. It draws from a fresh generator per call, seeded with seed and with
// its first value discarded, so the rhythm of one string never depends on another's. The band is
// chosen by the rune just typed; a space's gap may instead be a thinking pause, and the odds
// draw for that is consumed on every space whether or not it fires. Iteration is by rune, which
// is byte-identical to type.sh on the ASCII strings it accepted.
//
// Humanize panics on a seed outside 1..2^31-2 — a caller-side contract that storyboard
// validation enforces before any take is typed.
func Humanize(s string, seed int64) []time.Duration {
	if err := validTypingSeed(seed); err != nil {
		panic("demorig: Humanize: " + err.Error())
	}
	runes := []rune(s)
	if len(runes) < 2 {
		return nil
	}

	generator := minstd{state: seed}
	generator.advance() // a small seed would otherwise open on a small gap

	gaps := make([]time.Duration, 0, len(runes)-1)
	thinkingPauses := 0
	for _, character := range runes[:len(runes)-1] {
		var gapMS int64
		switch {
		case character == ' ':
			if generator.draw(1, thinkingPauseOdds) == 1 && thinkingPauses < thinkingPauseLimit {
				gapMS = generator.draw(thinkingPauseMinMS, thinkingPauseMaxMS)
				thinkingPauses++
			} else {
				gapMS = generator.draw(spaceGapMinMS, spaceGapMaxMS)
			}
		case strings.ContainsRune(punctuationCharacters, character):
			gapMS = generator.draw(punctuationGapMinMS, punctuationGapMaxMS)
		default:
			gapMS = generator.draw(letterGapMinMS, letterGapMaxMS)
		}
		gaps = append(gaps, time.Duration(gapMS)*time.Millisecond)
	}
	return gaps
}
