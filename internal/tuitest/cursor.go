package tuitest

import (
	"bytes"
	"fmt"
)

// cprFinal is the final byte of a cursor-position report, CSI row ; col R. None of the emulator's
// other answers carries it — DA1 and DA2 end in c, DECRQM in $y, DSR in n, an in-band resize in t,
// and an OSC colour report spells rgb: in lower case — so one count of it over the pumped bytes is
// a count of CPRs, and a report split across two reads is counted once, when its final byte goes
// through.
const cprFinal = 'R'

// cursorAnswers is a driver's tally of the cursor-position reports it has pumped into the program's
// input, and of where that tally stood when the last key went in. Both drivers keep one, under the
// same writeMu that orders keys against answers, because that ordering is what the tally is for:
// Bubble Tea reads its input in order, so a key written after an answer reaches Update after it.
//
// It exists for the decision latch (internal/tui approval.go): a pane's decision keys come alive on
// the SECOND drain-marker answer, and arming paints nothing, so no settle on the screen can tell an
// armed pane from one whose second marker is still in flight. A loaded runner held that round trip
// past the settle window, and the letter typed into the pane was swallowed, as an unarmed letter is.
type cursorAnswers struct {
	pumped int // reports written into the program's input since the driver started
	atKey  int // pumped as it stood when the last key was sent
}

// pump counts the reports in one chunk of the emulator's answers. The caller holds writeMu.
func (c *cursorAnswers) pump(p []byte) { c.pumped += bytes.Count(p, []byte{cprFinal}) }

// keySent marks the tally at a key. The caller holds writeMu.
func (c *cursorAnswers) keySent() { c.atKey = c.pumped }

// sinceKey is how many reports have gone in behind the last key. The caller holds writeMu.
func (c *cursorAnswers) sinceKey() int { return c.pumped - c.atKey }

// PaneArmAnswers is how many cursor-position reports a decision pane's keys arm on: its first drain
// marker leaves ahead of the pane's own frame and only relays, the second arms (internal/tui
// approval.go, foldInputDrained).
const PaneArmAnswers = 2

// awaitingCursorAnswers names the wait for n reports in a timeout message.
func awaitingCursorAnswers(n int) Option {
	return Awaiting(fmt.Sprintf("%d cursor-position answers to reach the program since the last key", n))
}
