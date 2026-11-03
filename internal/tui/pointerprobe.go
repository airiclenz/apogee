package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// ----------------------------------------------------------------------------
// The OSC 22 capability probe and the all-motion gate (ADR 0106 §2–§4)
// ----------------------------------------------------------------------------
//
// Pointer shapes need hover reports, and hover reports need all-motion tracking — a motion event
// for every cell the mouse crosses, button or not, each one an Update and a View composition. That
// traffic is worth paying only on a terminal that can show the shapes, so apogee asks first: the
// kitty query `OSC 22 ; ?default,text,pointer ST`, answered `OSC 22 ; 1,1,1 ST` by a terminal that
// supports all three names. A terminal that does not know OSC 22 answers nothing, so the query is
// followed by a DA1 request (`CSI c`) every terminal answers, and answers IN ORDER: a DA1 reply
// that arrives while the OSC 22 answer is still outstanding means there will be none.
//
// There is no timer. A terminal that answers neither — a silent pipe, a stalled link to a slow box
// — just leaves the probe pending, which reads as unsupported: the feature stays off and nothing
// on the screen depends on the answer, so waiting forever costs nothing (the slow-box rule).
//
// Both writes go out raw, never wrapped in ansi.TmuxPassthrough: under tmux the query stops at tmux,
// tmux answers the DA1 itself, and the feature stays off (ADR 0106 §4).

// pointerProbe is where the session's one OSC 22 capability question stands. The zero value is
// unasked; a session asks at most once (Init when ui.pointer-shapes is on at start-up, or the
// /settings apply that turns it on), and the verdict it reaches is kept for the rest of the run.
type pointerProbe int

const (
	// pointerProbeUnasked: the setting has been off for the whole session, so no query went out.
	pointerProbeUnasked pointerProbe = iota
	// pointerProbePending: the query is written (or about to be) and neither reply has folded.
	pointerProbePending
	// pointerProbeSupported: the terminal answered 1,1,1 before the DA1 sentinel.
	pointerProbeSupported
	// pointerProbeUnsupported: any other OSC 22 answer, or the DA1 sentinel first.
	pointerProbeUnsupported
)

const (
	// pointerShapeQuery asks whether the terminal knows the three shape names apogee writes, in
	// the order the answer's positional flags come back in.
	pointerShapeQuery = "\x1b]22;?default,text,pointer\x1b\\"

	// pointerShapeReplyPrefix opens every OSC 22 sequence; ultraviolet hands an OSC it does not
	// decode itself to Update whole — introducer, data and terminator — as uv.UnknownOscEvent.
	pointerShapeReplyPrefix = "\x1b]22;"

	// pointerShapeFullSupport is the one answer that opens the gate: all three names supported.
	pointerShapeFullSupport = "1,1,1"

	// diagPointerProbe is the --tui-diag key for the probe's verdict (diagnostics.go): one line
	// when the answer folds, saying which way it went and why.
	diagPointerProbe = "pointer-probe"
)

// pointerShapeTerminators are the ways an OSC answer can end — the 7-bit ST, BEL, and the 8-bit
// ST — stripped before the answer's data is compared.
var pointerShapeTerminators = []string{"\x1b\\", "\a", "\x9c"}

// pointerProbeCmd writes the query and its DA1 sentinel as ONE raw write, so nothing the program
// writes in between can reorder them on the wire: the sentinel only means "no answer" when it
// follows the query.
func pointerProbeCmd() tea.Cmd {
	return tea.Raw(pointerShapeQuery + ansi.RequestPrimaryDeviceAttributes)
}

// probeQueryCmd is Init's half of a start-up probe: newModel has already marked the probe pending
// when the setting was on (Init has a value receiver and cannot), so this only issues the write —
// and nothing on a session whose setting is off.
func (m Model) probeQueryCmd() tea.Cmd {
	if m.pointerProbe != pointerProbePending {
		return nil
	}
	return pointerProbeCmd()
}

// askPointerShapes starts the probe when the session has not asked yet, returning the model with
// the probe pending and the Cmd that writes the query. A session that already asked — pending or
// answered — returns a nil Cmd: one answer per session is kept, and a second query would race the
// first's replies.
func (m Model) askPointerShapes() (Model, tea.Cmd) {
	if m.pointerProbe != pointerProbeUnasked {
		return m, nil
	}
	m.pointerProbe = pointerProbePending
	return m, pointerProbeCmd()
}

// foldPointerShapeReply reads an unrecognised OSC the terminal sent. Only an OSC 22 answer that
// arrives while the probe is pending decides anything; every other OSC, and an answer arriving
// after the sentinel already settled the probe, is ignored.
func (m Model) foldPointerShapeReply(osc string) Model {
	if m.pointerProbe != pointerProbePending || !strings.HasPrefix(osc, pointerShapeReplyPrefix) {
		return m
	}
	answer := strings.TrimPrefix(osc, pointerShapeReplyPrefix)
	for _, terminator := range pointerShapeTerminators {
		answer = strings.TrimSuffix(answer, terminator)
	}
	if answer != pointerShapeFullSupport {
		return m.settlePointerProbe(pointerProbeUnsupported, "unsupported (answered "+answer+")")
	}
	return m.settlePointerProbe(pointerProbeSupported, "supported")
}

// foldPointerProbeSentinel reads a DA1 reply. While the probe is pending it is the sentinel
// arriving ahead of any OSC 22 answer — the terminal does not know the query — and otherwise it
// answers nobody apogee asked.
func (m Model) foldPointerProbeSentinel() Model {
	if m.pointerProbe != pointerProbePending {
		return m
	}
	return m.settlePointerProbe(pointerProbeUnsupported, "unsupported (no answer before DA1)")
}

// settlePointerProbe keeps the verdict for the session and records it for --tui-diag.
func (m Model) settlePointerProbe(verdict pointerProbe, why string) Model {
	m.pointerProbe = verdict
	m.diag.record(diagPointerProbe, why)
	return m
}

// pointerShapesLive reports whether the pointer-shape feature runs right now: the human's setting
// is on AND this terminal confirmed the three shapes. It is the one gate the mouse mode and every
// shape write read.
func (m Model) pointerShapesLive() bool {
	return m.opts.UI.PointerShapes && m.pointerProbe == pointerProbeSupported
}

// mouseMode is the tracking mode the frame asks for: all-motion while the pointer-shape gate is
// open, so hover reaches Update; cell-motion otherwise, so a terminal that cannot show a shape pays
// no motion traffic (ADR 0106 §2). View and the re-assert both read it, so the escapes apogee
// re-sends are always the mode the renderer last wrote.
func (m Model) mouseMode() tea.MouseMode {
	if m.pointerShapesLive() {
		return tea.MouseModeAllMotion
	}
	return tea.MouseModeCellMotion
}
