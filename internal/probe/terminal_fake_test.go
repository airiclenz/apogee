package probe

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// ----------------------------------------------------------------------------
// A scripted terminal
// ----------------------------------------------------------------------------
//
// The probe's product is a measurement of a terminal, so the only way to prove the measurement is
// to hand it a terminal whose answers are known in advance. fakeTerminal is that terminal: its Out
// side parses what the probe writes and moves a cursor by the rules it was scripted with, and its
// Read side hands back the replies a real terminal would send. It is a small VT model, not an
// emulator — it tracks the cursor and only the things the probe measures (tab stops, the
// last-column wrap, mode 2027, the addressing capabilities), and knows nothing about cells,
// colours or scrollback. It has one reason to change: the probe asks a new question.

// wrapMode is what a scripted terminal does when a character lands in the final column.
type wrapMode int

const (
	// wrapDeferred is xterm's semantics and the one ultraviolet's renderer emits against: the
	// cursor stays in the last column with a wrap pending until the next character is written.
	wrapDeferred wrapMode = iota
	// wrapImmediate is the divergence the 2026-08 ghosting investigation ended on: the cursor is
	// already a row down before the next character is written.
	wrapImmediate
)

// fakeCaps is what the scripted terminal actually does with the sequences the capability section
// measures. It is deliberately not xtermCaps: the section's finding is a disagreement between what
// a terminal implements and what TERM grants the painter, so a test has to script the two apart.
type fakeCaps struct {
	cha, vpa, rep bool
}

// fakeTerminal is the scripted terminal itself. Every knob is a fact about a real terminal
// somebody has met: one that acknowledges mode 2027 without honouring it, one whose tab stops are
// not every 8, one that wraps early, one that answers in pieces.
type fakeTerminal struct {
	width, height int
	// row and col are 1-based, the numbering CUP and DSR-CPR both use.
	row, col int
	// pending is the deferred wrap: a character has filled the last column and the cursor has not
	// moved yet.
	pending bool

	// stops are the columns a tab lands on, ascending.
	stops []int
	// modes is what DECRQM answers per mode; an absent mode answers modeNotRecognized.
	modes map[int]int
	caps  fakeCaps

	wrap wrapMode
	// honours2027 is whether SM/RM for mode 2027 actually moves what DECRQM then reports. A false
	// one is the terminal that acknowledges the mode without taking it.
	honours2027 bool
	// answersDECRQM is whether the terminal answers the mode query at all.
	answersDECRQM bool
	// resetsStops is whether DECST8C moves the tab stops back to the every-8 model.
	resetsStops bool
	// chunk caps how many bytes one Read hands back; 0 hands back everything queued. A reply
	// split across reads is what a busy pty delivers, and the probe has to wait for the rest.
	chunk int
	// widthOf is how many columns a run of printable text advances the cursor by — the terminal's
	// own width table, which the glyph sweep sets against the painter's.
	widthOf func(string) int

	// partial holds a sequence that is still arriving, so a write cut mid-CSI is waited for.
	partial []byte
	replies []byte
	written []byte
}

// newAgreeingTerminal builds the terminal the painter believes in: mode 2027 answered and
// honoured, tab stops every 8 that DECST8C confirms, a deferred last-column wrap, and exactly the
// addressing an xterm* TERM grants — CHA and VPA, but not REP, which xtermCaps drops for every
// xterm (terminal.go:1055). A terminal that offered REP would be a capability the painter does not
// know about, which is a finding rather than agreement.
func newAgreeingTerminal() *fakeTerminal {
	f := &fakeTerminal{
		width: 120, height: 29,
		row: 1, col: 1,
		modes:         map[int]int{2026: modeReset, 2027: modeReset},
		caps:          fakeCaps{cha: true, vpa: true},
		wrap:          wrapDeferred,
		honours2027:   true,
		answersDECRQM: true,
		resetsStops:   true,
	}
	f.stops = stopsEvery(8, f.width)
	f.widthOf = f.painterWidth
	return f
}

// painterWidth measures text the way a terminal that honours mode 2027 does: in grapheme clusters
// while the mode is set, by wcwidth while it is not. The glyph sweep runs twice precisely to catch
// a terminal whose measurement does NOT move with the mode.
func (f *fakeTerminal) painterWidth(text string) int {
	if f.modes[2027] == modeSet || f.modes[2027] == modePermanentlySet {
		return ansi.GraphemeWidth.StringWidth(text)
	}
	return ansi.WcWidth.StringWidth(text)
}

// fakeReplyTimeout is the per-reply deadline the tests hand the probe. Every terminal in this
// package answers from memory without sleeping, so a reply that is coming arrives on the first
// read and the deadline is never actually waited out — a generous one costs a passing run
// nothing. It has to be generous: await takes its deadline before the first read, so a
// one-millisecond bound can be spent by nothing more than the scheduler parking the goroutine.
// A full parallel suite did exactly that once, mid-probe, and turned a measured glyph section
// into "no answer"; a deadline in whole seconds cannot be lost that way.
const fakeReplyTimeout = 30 * time.Second

// inputs is the TerminalInputs a measurement session talks to this terminal through. Fd is 0
// because there is no console to read cells out of, which is what a non-Windows host looks like
// and is why the screen read-back rows report "unverified".
func (f *fakeTerminal) inputs(term string) TerminalInputs {
	return TerminalInputs{
		Out:     f,
		Read:    f.Read,
		Width:   f.width,
		Height:  f.height,
		Fd:      0,
		TERM:    term,
		Timeout: fakeReplyTimeout,
	}
}

// Write is the terminal's Out side: it records every byte, then interprets as much of the stream
// as is complete. A trailing partial sequence is held rather than discarded — a probe that writes
// a CSI in two calls must still be understood.
func (f *fakeTerminal) Write(p []byte) (int, error) {
	f.written = append(f.written, p...)
	f.partial = append(f.partial, p...)
	f.interpret()
	return len(p), nil
}

// Read is the terminal's input side: whatever it has queued to say, at most chunk bytes of it. An
// empty queue answers nothing at once — the probe's timeout is what a silent terminal costs, and a
// fake that slept would spend it on every test.
func (f *fakeTerminal) Read(time.Duration) []byte {
	if len(f.replies) == 0 {
		return nil
	}
	n := len(f.replies)
	if f.chunk > 0 && f.chunk < n {
		n = f.chunk
	}
	out := append([]byte(nil), f.replies[:n]...)
	f.replies = append(f.replies[:0], f.replies[n:]...)
	return out
}

// interpret consumes the buffered stream: printable runs move the cursor, complete CSI sequences
// are applied, and an incomplete one at the tail is left for the next write. The probe writes
// nothing but text and CSIs, so an ESC that starts no complete CSI is always a sequence still
// arriving.
func (f *fakeTerminal) interpret() {
	for len(f.partial) > 0 {
		esc := bytes.IndexByte(f.partial, 0x1b)
		if esc == -1 {
			f.print(string(f.partial))
			f.partial = f.partial[:0]
			return
		}
		if esc > 0 {
			f.print(string(f.partial[:esc]))
			f.partial = f.partial[esc:]
			continue
		}
		seq, ok := nextCSI(f.partial, 0)
		if !ok {
			return
		}
		f.apply(seq)
		f.partial = f.partial[seq.end:]
	}
}

// print writes a run of printable text, tab by tab: a hard tab jumps to the next stop, everything
// between two tabs advances by the terminal's own width table.
func (f *fakeTerminal) print(text string) {
	for text != "" {
		if text[0] == '\t' {
			f.tab()
			text = text[1:]
			continue
		}
		run := text
		if at := strings.IndexByte(text, '\t'); at >= 0 {
			run, text = text[:at], text[at:]
		} else {
			text = ""
		}
		f.advance(run)
	}
}

// advance moves the cursor over one run of printable text. The last column is where the two wrap
// semantics differ, so a run that reaches past it is what decides the wrap section's verdict.
func (f *fakeTerminal) advance(run string) {
	if run == "" {
		return
	}
	if f.pending {
		f.pending = false
		f.moveTo(f.row+1, 1)
	}
	end := f.col + f.widthOf(run)
	switch {
	case end <= f.width:
		f.col = end
	case f.wrap == wrapImmediate:
		f.moveTo(f.row+1, end-f.width)
	default:
		f.col, f.pending = f.width, true
	}
}

// tab jumps to the next stop, or to the last column when no stop is left — a tab can never move
// the cursor off the row.
func (f *fakeTerminal) tab() {
	f.pending = false
	for _, stop := range f.stops {
		if stop > f.col {
			f.moveTo(f.row, stop)
			return
		}
	}
	f.moveTo(f.row, f.width)
}

// moveTo parks the cursor inside the screen and discharges any pending wrap: every explicit move
// re-anchors the cursor, which is why the renderer wants absolute addressing in the first place. A
// row past the bottom stays on the bottom row, the way a scrolled screen leaves it.
func (f *fakeTerminal) moveTo(row, col int) {
	f.row, f.col, f.pending = min(max(row, 1), f.height), min(max(col, 1), f.width), false
}

// apply is what the terminal does with one complete CSI sequence. Anything not listed is ignored,
// which is a real terminal's behaviour and keeps the model to the questions the probe asks.
func (f *fakeTerminal) apply(seq csiSeq) {
	if seq.private == '?' {
		f.applyPrivate(seq)
		return
	}
	switch seq.final {
	case 'H': // CUP
		f.moveTo(csiParam(seq.params, 0, 1), csiParam(seq.params, 1, 1))
	case 'G': // CHA
		if f.caps.cha {
			f.moveTo(f.row, csiParam(seq.params, 0, 1))
		}
	case 'd': // VPA
		if f.caps.vpa {
			f.moveTo(csiParam(seq.params, 0, 1), f.col)
		}
	case 'b': // REP — the last graphic character again, which IS visible from the cursor
		if f.caps.rep {
			f.moveTo(f.row, f.col+csiParam(seq.params, 0, 1))
		}
	case 'n': // DSR
		if csiParam(seq.params, 0, 0) == 6 {
			f.queue(fmt.Sprintf(csi+"%d;%dR", f.row, f.col))
		}
	}
	// EL (K), ED (J), ECH (X) and ICH (@) change cells without moving the cursor, so a model that
	// tracks only the cursor has nothing to do with them.
}

// applyPrivate handles the DEC private sequences: the mode query and the two mode settings the
// probe cares about, and the tab-stop reset. The alternate screen and the cursor visibility are
// accepted and ignored — nothing the probe measures can see them.
func (f *fakeTerminal) applyPrivate(seq csiSeq) {
	mode := csiParam(seq.params, 0, 0)
	switch {
	case seq.final == 'p' && seq.inter == "$": // DECRQM
		if f.answersDECRQM {
			f.queue(fmt.Sprintf(csi+"?%d;%d$y", mode, f.modes[mode]))
		}
	case seq.final == 'h' || seq.final == 'l': // SM / RM
		if mode == 2027 && f.honours2027 {
			f.modes[2027] = modeSet
			if seq.final == 'l' {
				f.modes[2027] = modeReset
			}
		}
	case seq.final == 'W': // DECST8C
		if f.resetsStops {
			f.stops = stopsEvery(8, f.width)
		}
	}
}

// queue holds a reply for the next Read.
func (f *fakeTerminal) queue(reply string) {
	f.replies = append(f.replies, reply...)
}

// stopsEvery renders the tab stops of a terminal whose stops are every n columns from column 1 —
// the every-8 model the renderer moves against, and the every-4 one that disagrees with it.
func stopsEvery(n, width int) []int {
	stops := make([]int, 0, width/n)
	for col := 1 + n; col <= width; col += n {
		stops = append(stops, col)
	}
	return stops
}

// csiParam reads one parameter of a CSI sequence, falling back the way a terminal does for a
// parameter that was omitted or empty.
func csiParam(params string, index, fallback int) int {
	parts := strings.Split(params, ";")
	if index >= len(parts) {
		return fallback
	}
	value, err := strconv.Atoi(parts[index])
	if err != nil {
		return fallback
	}
	return value
}

// lastMode2027Request returns the last SM/RM the probe wrote for mode 2027 — the one that decides
// which setting the user's terminal is left in. The probe restores the mode it found, so this is
// how a test proves it put the terminal back.
func lastMode2027Request(written string) (string, bool) {
	set, reset := strings.LastIndex(written, setMode2027), strings.LastIndex(written, resetMode2027)
	switch {
	case set < 0 && reset < 0:
		return "", false
	case set > reset:
		return setMode2027, true
	default:
		return resetMode2027, true
	}
}
