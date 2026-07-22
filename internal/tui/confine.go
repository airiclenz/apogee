package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/probe"
)

// ----------------------------------------------------------------------------
// /confine — routing the blast-radius verb (command.go owns its grammar)
// ----------------------------------------------------------------------------
//
// The command is the sanctioned route to the user's OWN decision about Auto's blast radius
// (ADR 0012, amendment 2026-07-21): the tool may offer it, it may never take it. So every note
// below states the blast radius plainly, says whether the change was session-only or written
// to disk, and never claims to have repaired anything — nothing here is broken when Auto gates
// on a host that cannot fence, it is the ladder doing what the ADR says.
//
// Routing is synchronous and idle-safe like /clear (no worker owns the engine at this point),
// and the report builders below are pure so the wording is table-testable without a Model.

// runConfine routes a parsed /confine line from the idle state. status renders a report and
// changes nothing; off/on assert the requested blast radius on the engine, taking effect on the
// next tool call; --save additionally persists this host's acknowledgement through the binary's
// writer seam. It never launches a worker, so it always returns a nil Cmd.
func (m Model) runConfine(args confineArgs) (tea.Model, tea.Cmd) {
	// The live value the next Resolution would read — what makes "already off" an honest claim
	// rather than an assumption about how the session started.
	was := m.eng.ConfineToWorkspace()

	switch args.action {
	case confineStatus:
		m.transcript.addNote(confineStatusReport(m.opts.Confinement, m.opts.Mode, was))

	case confineOff, confineOn:
		want := args.action == confineOn
		// Asserted unconditionally, even when it is already the live value: the user asked for a
		// state, not a transition, and the setter is idempotent — so the request cannot be lost
		// to a stale read while the wording below still reports what actually changed.
		m.eng.SetConfineToWorkspace(want)
		note := confineOnNote(m.opts.Confinement, m.opts.Mode, was)
		if !want {
			note = confineOffNote(m.opts.Confinement, m.opts.Mode, was)
		}
		if args.save { // parseConfine allows --save only on off
			note += "\n" + m.saveHostAcknowledgement()
		}
		m.transcript.addNote(note)
	}

	m.layout()
	return m, nil
}

// saveHostAcknowledgement drives the binary's config writer for `/confine off --save` and
// renders the one-line outcome: which file records this host, or why nothing was written. A
// failed (or unwired) save never invalidates the session toggle that already happened — the two
// are independent, and saying so is what keeps the confirmation truthful.
func (m Model) saveHostAcknowledgement() string {
	if m.opts.SaveHostAcknowledgement == nil {
		return "not saved: this build cannot write the host acknowledgement\n" +
			"  the change applies to this session only"
	}
	path, err := m.opts.SaveHostAcknowledgement()
	if err != nil {
		return "not saved: " + err.Error() + "\n  the change applies to this session only"
	}
	return fmt.Sprintf("saved: %s acknowledged in %s\n  delete that entry to confine this host again",
		confineHostLabel(m.opts.Confinement), path)
}

// ----------------------------------------------------------------------------
// The rendered notes (pure)
// ----------------------------------------------------------------------------

// confineStatusReport renders `/confine status`: the effective setting, the backend and what it
// can actually enforce on this host, the host id an acknowledgement is recorded against, and the
// mode — because the flag is read by the Auto rung alone, so on the three lower rungs it is a
// statement about a mode that is not running. It closes with the remedy lines only in the case
// that prompts the question: confined, in Auto, on a backend that cannot fence.
func confineStatusReport(info ConfinementInfo, mode domain.Mode, confine bool) string {
	setting := "unconfined — every call runs with your full privileges"
	if confine {
		setting = "confined — auto fences what it can, gates the rest for approval"
	}
	modeLine := modeLabel(mode) + " — read by auto mode only"
	if mode == domain.ModeAuto {
		modeLine = modeLabel(mode)
	}
	lines := []string{
		"/confine — auto mode's blast radius",
		"  setting: " + setting,
		"  backend: " + confineBackendLine(info),
		"  host id: " + confineHostLabel(info),
		"  mode:    " + modeLine,
	}
	if confine && mode == domain.ModeAuto && !info.Caps.FSWrite {
		lines = append(lines,
			"  commands cannot be fenced here, so auto asks approval for each one",
			"  /confine off runs them unconfined for this session (safe ONLY on a",
			"  disposable machine); /confine off --save also remembers this host")
	}
	return strings.Join(lines, "\n")
}

// confineOffNote confirms `/confine off`. The first line is the blast radius the user just took
// on, in the plainest words available; the second qualifies it for the case at hand — a host
// that CAN fence (so this is a deliberate choice, not a workaround), a mode that never reads the
// flag, or the degraded host the command exists for. was is the setting before the line ran, so
// a no-op says so instead of implying something changed.
func confineOffNote(info ConfinementInfo, mode domain.Mode, was bool) string {
	head := "confinement OFF for this session"
	if !was {
		head = "confinement was already off"
	}
	lines := []string{
		head,
		"  auto runs every command unfenced, with your full privileges",
	}
	switch {
	case mode != domain.ModeAuto:
		lines = append(lines, "  the current mode is "+modeLabel(mode)+"; this applies once you reach auto")
	case info.Caps.FSWrite:
		lines = append(lines, "  the "+confineBackendName(info)+" backend here CAN fence commands — you are")
		lines = append(lines, "  choosing to run without it; /confine on restores the fence")
	default:
		lines = append(lines, "  commands are no longer gated for approval; /confine on restores it")
	}
	return strings.Join(lines, "\n")
}

// confineOnNote confirms `/confine on`. It names what returning to the fence costs on a host
// that cannot enforce it — the approval prompt per terminal command — so re-confining is not a
// surprise, and states that a persisted acknowledgement is untouched (only the file's owner
// removes it; nothing here writes).
func confineOnNote(info ConfinementInfo, mode domain.Mode, was bool) string {
	head := "confinement ON for this session"
	if was {
		head = "confinement was already on"
	}
	lines := []string{
		head,
		"  auto fences what it can and gates the rest for approval",
	}
	switch {
	case mode != domain.ModeAuto:
		lines = append(lines, "  the current mode is "+modeLabel(mode)+"; this applies once you reach auto")
	case !info.Caps.FSWrite:
		lines = append(lines, "  the "+confineBackendName(info)+" backend here cannot fence commands, so each one")
		lines = append(lines, "  asks for approval again")
	}
	lines = append(lines, "  any unconfined-hosts: entry in your config is untouched")
	return strings.Join(lines, "\n")
}

// confineBackendLine renders the backend and its capability matrix for the status report, e.g.
// "landlock (fs-write: unavailable · network: unavailable)". The wording comes from
// internal/probe because `apogee probe` states the same matrix off-session (ADR 0021): one
// verdict rendered in two places must not become two verdicts.
func confineBackendLine(info ConfinementInfo) string {
	return probe.CapabilityLine(confineBackendName(info), info.Caps)
}

// confineBackendName is the backend's label, or "unknown backend" when the binary wired none
// (the zero ConfinementInfo) — never an empty string in the middle of a sentence.
func confineBackendName(info ConfinementInfo) string {
	if info.Backend == "" {
		return "unknown backend"
	}
	return info.Backend
}

// confineHostLabel is the host id an acknowledgement is matched against, or "unknown" when the
// binary wired none.
func confineHostLabel(info ConfinementInfo) string {
	if info.HostID == "" {
		return "unknown"
	}
	return info.HostID
}
