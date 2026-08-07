package tui

import (
	"bytes"
	"sync"

	"github.com/charmbracelet/x/term"
)

// ----------------------------------------------------------------------------
// Declining synchronized output — the mode-2026 question never leaves apogee
// ----------------------------------------------------------------------------
//
// bubbletea asks every terminal whether it supports synchronized output (mode 2026) and, when the
// answer is yes, wraps each frame in BSU/ESU so the terminal can present it atomically. On the
// Windows paths apogee actually runs on, that trade was measured and it is a pure loss: the
// atomicity never arrives, and paying for it costs a flicker mitigation that does.
//
// THE MEASUREMENT (docs/plans/2026-08-06 - 04, finding 34). Captured out of a real pseudoconsole,
// both arms of an A/B on this exact switch: ConPTY forwards apogee's `CSI ?2026h` and `CSI ?2026l`
// back to back as an EMPTY pair, and re-serializes the frame's cells AFTER that window has already
// closed. The two arms' 9260- and 9228-byte captures are byte-identical once the two empty pairs
// are deleted from the first. So the frame is never presented atomically on this path — the window
// the downstream emulator receives contains nothing at all.
//
// WHAT THE TRADE COSTS (finding 33). bubbletea treats synchronized output and cursor hiding as
// alternatives (cursed_renderer.go:526-556): with mode 2026 on, shouldUpdateCursorVis is false in
// steady state and the renderer repositions the cursor and writes cells with the cursor LIVE; with
// it off, every frame carrying updates is wrapped in `CSI ?25l` … `CSI ?25h`. conhost — the one
// measured Windows path that has never ghosted — already runs the second configuration, because it
// answers "not recognized" to the mode-2026 question and bubbletea never enables the mode there.
//
// So declining the question puts the renderer in the configuration the clean path already runs in,
// and gives up an atomicity ConPTY discards on apogee's behalf either way.
//
// THIS IS NOT A CLAIM THAT IT FIXES THE GHOSTING. As of 2026-08-07 the Windows ghost is
// unexplained: H1-H4 are all falsified or retired, and the visual A/B that would show whether the
// empty window is implicated has not been run. This code is justified by finding 34 alone — dead
// weight in the protocol, paid for with a real mitigation. If the A/B comes back positive this
// becomes the fix as well; if it comes back negative, the change is still correct.
//
// WHY A BYTE FILTER. bubbletea exposes no option for any of it: options.go has no
// WithSynchronizedOutput, setSyncdUpdates is unexported, shouldQuerySynchronizedOutput is private,
// and this plan does not carry a fork. The seam that is left is the tea.WithOutput wrapper item 2
// built for --tui-trace — remove the question from the wire and the answer never comes, so
// bubbletea's `case ansi.ModeSynchronizedOutput:` is simply never reached. The mode-2027 question
// travels in the same write and is deliberately left untouched: widthAuthority exists to mirror
// what bubbletea does with that one (width.go), and silencing one side alone would desync them.

// syncQueryProbe is the DECRQM request bubbletea sends to ask about mode 2026. It emits this and
// the mode-2027 request as one string through a single execute call (bubbletea/tea.go:1109-1114 at
// the pinned v2.0.8), which is why the two have to be separated at the byte level rather than by
// declining a write.
//
// Note what this is NOT: `CSI ?2026h` and `CSI ?2026l`, the BSU/ESU frame wrappers, differ from the
// request by their final byte and are not touched. Nothing here would fire on them, and if the mode
// is never enabled the renderer never emits them anyway.
const syncQueryProbe = "\x1b[?2026$p"

// syncQueryProbeBytes is [syncQueryProbe] in the form the scan below works in. It is a var only
// because Go has no byte-slice constants; nothing writes to it.
var syncQueryProbeBytes = []byte(syncQueryProbe)

// syncQueryStripper is a terminal that never passes the mode-2026 question on. Every other byte it
// is given — the mode-2027 question sharing the same write included — reaches the terminal
// unmodified and in order, and every method other than Write is the wrapped terminal's own.
//
// Like tracedOutput it is a term.File in full, and for the identical reason: bubbletea decides
// whether its output IS a terminal by type-asserting it (tty_windows.go:36) and colorprofile.Detect
// asserts the same value (tea.go:1083), so a plain io.Writer here would silently turn off raw mode,
// VT processing, size detection and the cursor optimizations — trading a two-byte protocol question
// for a different program.
type syncQueryStripper struct {
	// out is the terminal underneath: os.Stdout, or the tracedOutput wrapping it when --tui-trace
	// named a file. The stripper sits ABOVE the tracer — bubbletea writes here, this writes there —
	// so a trace records the bytes that actually reached the terminal. Traces are diffed against
	// pseudoconsole captures (findings 33-34); a trace that disagreed with the wire would make that
	// comparison lie.
	out term.File

	// mu guards carry and serialises the write, for the reason tracedOutput holds a mutex too:
	// bubbletea's exec seam hands the same output to a suspended child (exec.go:112), and a
	// concurrent write must not interleave with a half-matched probe.
	mu sync.Mutex

	// carry holds a trailing PARTIAL probe from the previous write until the next one says whether
	// it was one. See Write for why holding those bytes back cannot lose anything.
	carry []byte
}

// syncQueryStripper must be a term.File or it changes the program instead of filtering it.
var _ term.File = (*syncQueryStripper)(nil)

// newSyncQueryStripper returns out with the mode-2026 question filtered out of everything written
// to it.
func newSyncQueryStripper(out term.File) *syncQueryStripper {
	return &syncQueryStripper{out: out}
}

// Write sends p to the terminal with every occurrence of [syncQueryProbe] removed, and reports the
// full length of p as written.
//
// The count is deliberate. Fewer bytes reach the terminal than the caller offered, but all of the
// caller's bytes were accepted and none is pending, so reporting the short count would break the
// io.Writer contract (n < len(p) obliges an error) and make bubbletea treat a successful frame as a
// failed one. A failing underlying write is reported as zero instead: once carry and the removal
// have rewritten the stream, how much of THIS p reached the terminal is no longer recoverable, and
// bubbletea treats any output error as fatal to the frame regardless.
//
// THE CARRY, and why it is safe. A write can in principle end part-way through the probe, which a
// per-write scan would miss and pass to the terminal in two halves. So a trailing run of bytes that
// is a proper prefix of the probe is held back until the next write completes or refutes it.
// Holding them cannot lose anything the terminal could have acted on: every proper prefix of
// `ESC [ ? 2 0 2 6 $` is an INCOMPLETE escape sequence, so a write ending in one is by construction
// a write that stopped mid-sequence — the remainder must follow, or the terminal was going to be
// left mid-sequence whatever this wrapper did.
//
// In practice it never engages. bubbletea appends both DECRQM requests to its output buffer in one
// execute call (tea.go:1214-1218) and flushes that buffer with a single Write (tea.go:1221-1237),
// so the probe arrives whole. The carry is here so that stops being load-bearing.
func (o *syncQueryStripper) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	buf := p
	if len(o.carry) > 0 {
		buf = make([]byte, 0, len(o.carry)+len(p))
		buf = append(buf, o.carry...)
		buf = append(buf, p...)
		o.carry = o.carry[:0]
	}
	if bytes.Contains(buf, syncQueryProbeBytes) {
		buf = bytes.ReplaceAll(buf, syncQueryProbeBytes, nil)
	}
	if k := partialSyncQuerySuffix(buf); k > 0 {
		o.carry = append(o.carry, buf[len(buf)-k:]...)
		buf = buf[:len(buf)-k]
	}
	if len(buf) == 0 {
		return len(p), nil
	}
	if _, err := o.out.Write(buf); err != nil {
		return 0, err
	}
	return len(p), nil
}

// partialSyncQuerySuffix reports how many trailing bytes of b are a proper prefix of the probe, or
// zero when none are. At most one length can ever match, because the probe's ESC occurs only at its
// own position 0, but the loop runs longest-first so that stays a property rather than a premise.
func partialSyncQuerySuffix(b []byte) int {
	longest := len(syncQueryProbeBytes) - 1
	if len(b) < longest {
		longest = len(b)
	}
	for k := longest; k > 0; k-- {
		if bytes.HasSuffix(b, syncQueryProbeBytes[:k]) {
			return k
		}
	}
	return 0
}

// Read delegates to the terminal, exactly as tracedOutput's does and for the same reason:
// term.File is an io.ReadWriteCloser and the assertion this type exists to satisfy is the whole
// interface or none of it. bubbletea reads its input from stdin, not from its output, so nothing
// calls this.
func (o *syncQueryStripper) Read(p []byte) (int, error) { return o.out.Read(p) }

// Close closes nothing, because this wrapper owns nothing: it opens no file and holds no descriptor
// of its own. The terminal belongs to the process, and the trace file — when there is one — is
// closed by [Run] through the *tracedOutput it holds alongside this. Delegating instead would mean
// closing os.Stdout on every untraced run the moment anyone added a defer.
//
// Whatever sits in carry is discarded with it, which is right: carry can only ever hold an
// incomplete escape sequence (see [syncQueryStripper.Write]), and finishing a session by emitting a
// dangling ESC is worse than dropping it.
func (o *syncQueryStripper) Close() error { return nil }

// Fd answers with the wrapped terminal's descriptor, which is os.Stdout's however many wrappers are
// stacked above it. Raw mode, console mode, size and is-this-a-tty all go through it, so answering
// with anything else is the same mistake as not being a term.File at all.
func (o *syncQueryStripper) Fd() uintptr { return o.out.Fd() }
