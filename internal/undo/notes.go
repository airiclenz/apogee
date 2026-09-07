package undo

import "fmt"

// ----------------------------------------------------------------------------
// The rendered listing (pure)
// ----------------------------------------------------------------------------
//
// A revert is authorised from what the human READS, so the listing is the authorization surface:
// every recorded path appears with what the step would do to it — restores, deletions and skips
// alike — because a summary the human cannot check is not a disclosure (ADR 0051). The wording
// lives here rather than in a Driver so the TUI's `/undo`, its `/redo` and the unattended
// `apogee undo` verb read as ONE listing instead of three that drifted (ADR 0074).
//
// What a Driver still owns is the VERB. These lines never name a command: the caller heads the
// first one with the form the human typed — "/undo", "/redo", "apogee undo <session-id>" — and
// closes the block with the line that executes it, which is the only part that differs between
// them. That is the whole of the neutrality; the wording of the step itself is fixed here.

// actionColumn is the width every action verb is padded to, so the paths of a listing line up
// under one another whichever verb each row carries.
const actionColumn = 7

// PreviewLines renders a [Step] as the listing a revert is authorised from: the exchange it names,
// then one line per recorded path — the action, the path at the journal's recorded absolute
// spelling (a root-joined named path for an ordinary write, an approved escape's permit-pinned
// target), and, for a skip, the reason that path will be left alone.
//
// Every path is listed, never summarised, and the caller adds the verb and the line that applies
// the step. It is the same listing in both directions: [Journal.Preview] and [Journal.RedoPreview]
// have already classified each path against the file as it is now.
func PreviewLines(step Step) []string {
	lines := make([]string, 0, len(step.Changes)+1)
	lines = append(lines, fmt.Sprintf("exchange %d:", step.Ordinal))
	for _, change := range step.Changes {
		lines = append(lines, pathLine(change.Action, change.Path, change.Reason))
	}
	return lines
}

// ReportLines renders what a revert actually did: the counts, then every path it left alone with
// the reason. The skips are named individually and the successes are only counted, because a skip
// is the one outcome that leaves the human with work to do — the file still holds what the agent
// wrote, and only the reason says whether that was their own edit or a failure.
func ReportLines(report Report) []string {
	lines := make([]string, 0, len(report.Skipped)+1)
	lines = append(lines, fmt.Sprintf("exchange %d: %d restored, %d removed, %d skipped",
		report.Ordinal, len(report.Restored), len(report.Deleted), len(report.Skipped)))
	for _, skipped := range report.Skipped {
		lines = append(lines, pathLine(ActionSkip, skipped.Path, skipped.Reason))
	}
	return lines
}

// NothingLines answers the empty journal: a preview with no group to describe, and a confirmation
// that found none.
//
// reason is the Driver's [Journal] provenance — what the store could not do, as
// snapshot.OpenJournal phrased it ("git not found", "undo-snapshots is off") — and is empty when
// snapshots ARE in force. It is named in the same breath as the emptiness because the two answers
// mean different things: with snapshots the session's writes really are all reachable and there
// were none, while without them the reach is the funnel's alone and this run's alone, so a human
// told only "nothing to undo" would read a narrower journal as a broken one (ADR 0074 decision 2).
func NothingLines(reason string) []string {
	line := "nothing to undo — no agent file writes are recorded for this session"
	if reason != "" {
		line += " (" + reason + ")"
	}
	return []string{line}
}

// pathLine renders one path row, shared by the preview and the report so the two read as one
// listing: the verb in a fixed column, the path, and — for a skip — the reason after it.
func pathLine(action Action, path, reason string) string {
	line := fmt.Sprintf("  %-*s %s", actionColumn, action, path)
	if reason != "" {
		line += " — " + reason
	}
	return line
}
