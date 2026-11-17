package console

import "io"

// rowBreakCarryLimit bounds how many bytes of an unfinished escape sequence rowBreakWriter holds
// back for the next write. A real sequence — a cursor move, a window title — is far shorter; past
// the bound the bytes are passed through as they are, so a stray ESC cannot stall the output.
const rowBreakCarryLimit = 4096

// tabStop is the column interval the terminal's default tab stops sit at.
const tabStop = 8

// rowBreakWriter stands in front of a Console's ring on a terminal that paints its screen rather
// than streaming text — a Windows pseudoconsole (ConPTY). ConPTY renders its screen buffer, and
// while it fills the first screen it skips empty rows with a cursor move (ESC[<r>;<c>H, ESC[<n>B)
// instead of writing CR LF; only once the buffer scrolls does it emit real newlines. stripEscapes
// drops a cursor move whole, so without this writer a skipped row reads as no separator at all —
// cmd's banner glued to its prompt.
//
// The writer follows the cursor's row and column through the stream (printed text, wrapping at the
// window's width, CR, LF, backspace, tab and the CSI cursor moves) and, ahead of a cursor move that
// takes the cursor below the lowest row the text has reached so far, inserts one newline per row it
// skips. Every byte it is given is passed through unchanged — the escape sequences are still
// stripped on the way out — so the only change to the stream is the inserted newlines.
//
// Moving up is a repaint, not new output, so it inserts nothing; moving back down to rows already
// reached inserts nothing either. Text printed on a row ABOVE the lowest one reached starts a fresh
// line of output (a newline first, if the current one holds text) and makes that row the lowest
// reached — the shape of a screen cleared and painted again from the top. Same-row moves (a column
// change, an erase, colour) insert nothing.
//
// It is a model of where the cursor goes, not a screen: it holds no cells, and a program that
// paints out of order (a full-screen UI) still reads as the order its bytes came in. Each rune
// counts as one column, so a run of double-width characters wrapping a line can cost one stray
// blank line — never a merged one.
//
// An escape sequence split across two writes is held back until its final byte arrives (up to
// rowBreakCarryLimit), so the inserted newlines always land before the sequence, never inside it.
// flush hands a held-back tail on when the output ends. The writer is not safe for concurrent use:
// a Console's reader goroutine is its only caller.
type rowBreakWriter struct {
	next io.Writer

	// rows and cols are the terminal's size; the cursor stays inside it.
	rows, cols int
	// row and col are the cursor's position, 1-based. col == cols+1 is the pending-wrap state a
	// terminal is in after printing into the last column.
	row, col int
	// reached is the lowest row the text has reached — the row a newline-less move must get past
	// before it skips any row of output.
	reached int
	// lineOpen reports that the output's current line holds text, which a fresh line started by
	// a repaint must not be glued onto.
	lineOpen bool

	// carry is the unfinished escape sequence held back from the previous write.
	carry []byte
	// out is the scratch buffer a write's output is assembled in, reused across writes.
	out []byte
}

// newRowBreakWriter returns a rowBreakWriter forwarding to next for a rows x cols terminal whose
// cursor starts at the top-left.
func newRowBreakWriter(next io.Writer, rows, cols int) *rowBreakWriter {
	return &rowBreakWriter{next: next, rows: rows, cols: cols, row: 1, col: 1, reached: 1}
}

// Write forwards p to next with a newline inserted for every row a cursor move skips. It reports
// all of p written once next has taken the output, so an io.Copy feeding it never sees a short
// write for the bytes it adds or holds back.
func (w *rowBreakWriter) Write(p []byte) (int, error) {
	text := string(w.carry) + string(p)
	w.carry = nil
	out := w.out[:0]
	for index := 0; index < len(text); {
		if text[index] != escape {
			out = w.advance(out, text[index])
			index++
			continue
		}
		rest := text[index:]
		if escapeTruncated(rest) && len(rest) <= rowBreakCarryLimit {
			w.carry = []byte(rest)
			break
		}
		length := escapeSequenceLength(rest)
		out = w.moveCursor(out, rest[:length])
		out = append(out, rest[:length]...)
		index += length
	}
	w.out = out
	if len(out) > 0 {
		if _, err := w.next.Write(out); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// flush forwards an escape sequence still held back — the output ended before its final byte
// arrived — so no byte the terminal produced is lost.
func (w *rowBreakWriter) flush() error {
	if len(w.carry) == 0 {
		return nil
	}
	carry := w.carry
	w.carry = nil
	_, err := w.next.Write(carry)
	return err
}

// advance moves the cursor for one byte of text and appends it to out, starting a fresh line of
// output first when the byte is printed on a row above the lowest one reached.
func (w *rowBreakWriter) advance(out []byte, b byte) []byte {
	switch {
	case b == '\n':
		w.lineDown()
		w.lineOpen = false
	case b == '\r':
		w.col = 1
	case b == '\b':
		w.col = max(w.col-1, 1)
	case b == '\t':
		w.col = min((w.col-1)/tabStop*tabStop+tabStop+1, w.cols)
	case b < 0x20 || b == 0x7f:
		// Another control character: it prints nothing and leaves the cursor where it is.
	case b >= 0x80 && b < 0xc0:
		// A UTF-8 continuation byte: its rune's lead byte already took the column.
	default:
		if w.col > w.cols {
			w.col = 1
			w.lineDown()
		}
		if w.row < w.reached {
			if w.lineOpen {
				out = append(out, '\n')
			}
			w.reached = w.row
		}
		w.col++
		w.lineOpen = true
	}
	return append(out, b)
}

// lineDown moves the cursor one row down — a scroll when it is already on the last row, which
// leaves it there.
func (w *rowBreakWriter) lineDown() {
	w.row = min(w.row+1, w.rows)
	w.reached = max(w.reached, w.row)
}

// moveCursor applies sequence — one whole escape sequence — to the cursor and, when it moves the
// cursor below the lowest row reached, appends a newline per row skipped. A sequence that is not
// a CSI cursor move leaves the cursor and out as they are.
func (w *rowBreakWriter) moveCursor(out []byte, sequence string) []byte {
	final, params, ok := cursorMove(sequence)
	if !ok {
		return out
	}
	first, second := params[0], params[1]
	switch final {
	case 'H', 'f':
		w.row, w.col = first, second
	case 'A':
		w.row -= first
	case 'B', 'e':
		w.row += first
	case 'C', 'a':
		w.col += first
	case 'D':
		w.col -= first
	case 'E':
		w.row, w.col = w.row+first, 1
	case 'F':
		w.row, w.col = w.row-first, 1
	case 'G', '`':
		w.col = first
	case 'd':
		w.row = first
	default:
		return out
	}
	w.row = min(max(w.row, 1), w.rows)
	w.col = min(max(w.col, 1), w.cols)
	for ; w.reached < w.row; w.reached++ {
		out = append(out, '\n')
		w.lineOpen = false
	}
	return out
}

// cursorMove reads a CSI sequence's final byte and its first two numeric parameters, each
// defaulting to 1 when absent or zero (the cursor-move reading of a parameter). ok is false for
// anything else: not a CSI, a private (ESC[?…) or intermediate-carrying sequence, or a parameter
// that is not a plain number.
func cursorMove(sequence string) (final byte, params [2]int, ok bool) {
	if len(sequence) < 3 || sequence[1] != '[' {
		return 0, params, false
	}
	final = sequence[len(sequence)-1]
	params = [2]int{1, 1}
	slot, value := 0, 0
	for _, b := range []byte(sequence[2 : len(sequence)-1]) {
		switch {
		case b >= '0' && b <= '9':
			value = min(value*10+int(b-'0'), 1<<16)
		case b == ';':
			if slot < len(params) && value > 0 {
				params[slot] = value
			}
			slot, value = slot+1, 0
		default:
			return 0, params, false
		}
	}
	if slot < len(params) && value > 0 {
		params[slot] = value
	}
	return final, params, true
}
