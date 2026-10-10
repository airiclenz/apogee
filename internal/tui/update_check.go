package tui

import (
	tea "charm.land/bubbletea/v2"
)

// updateNotice is what the boot update check found: the newer release's tag and the command this
// install runs to reach it. The zero value is "nothing to say", and it is what every Options starts
// with, so an unwired host and a check that came back empty paint the same box.
type updateNotice struct {
	latest, command string
}

// updateCheckedMsg carries the [UpdateHost] answer back to the Update loop. Like recallLoadedMsg it
// is a plain report and never an error case: a failed or disabled check is ok=false, and the fold
// then changes nothing.
type updateCheckedMsg struct {
	latest, command string
	ok              bool
}

// Compile-time assertion that the update Msg is a valid tea.Msg (mirroring messages.go).
var _ tea.Msg = updateCheckedMsg{}

// updateCheckCmd builds the Cmd that asks the update host, off the Update loop, whether a newer
// release is published. It captures the host and the session context by value so the closure holds
// no pointer into the value-copied Model (loadRecallCmd's posture). An unwired host returns a nil
// Cmd, so an unwired TUI makes no check at all.
func (m Model) updateCheckCmd() tea.Cmd {
	host, ctx := m.opts.Update, m.parent
	if host == nil {
		return nil
	}
	return func() tea.Msg {
		latest, command, ok := host.CheckForUpdate(ctx)
		return updateCheckedMsg{latest: latest, command: command, ok: ok}
	}
}

// foldUpdateChecked lands the check's answer. A newer release is stored on m.opts — where
// newStartupView reads it, so every later restatement of the box keeps it — and the box already in
// the scrollback is restated in place. Anything else (ok=false, or a host that said ok with no tag)
// changes nothing, which keeps the box byte-identical to an unwired one.
func (m *Model) foldUpdateChecked(msg updateCheckedMsg) {
	if !msg.ok || msg.latest == "" {
		return
	}
	m.opts.updateNotice = updateNotice{latest: msg.latest, command: msg.command}
	m.transcript.refreshStartup(newStartupView(m.opts))
}

// startupVersion composes the start-up box's version value: the clean release version alone, or —
// once a newer release has been found — "<current> → <latest> · <command>" (the command segment
// dropped when the host named none). No current version means there is nothing to point an arrow
// from, so the value stays as it is.
func startupVersion(opts Options) string {
	n := opts.updateNotice
	if n.latest == "" || opts.BaseVersion == "" {
		return opts.BaseVersion
	}
	v := opts.BaseVersion + " → " + n.latest
	if n.command != "" {
		v += " · " + n.command
	}
	return v
}
