package notice

import (
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/format"
)

// ContextNotice is one composed line plus the one bit a Driver needs to route it. Anomaly marks
// the lines that report something WRONG — a file present but unreadable, standing content that
// has outgrown its Budget share — as against the plain record of what loaded. A Driver that
// shows everything ignores the flag; one that only reports trouble (the daemon's log) keeps the
// Anomaly notices and drops the rest.
type ContextNotice struct {
	Text    string
	Anomaly bool
}

// ContextFileNotices composes the session notice for a workspace's context files, in the order a
// reader meets them: the line naming every file that loaded and its size, then one line per file
// that is present but unreadable (the loud skip), then — when the standing system content has
// outgrown the window share allocated to it — the advisory warning that says so.
//
// A report with no files at all yields NO notices, the oversize warning included: a repo with
// none of the configured names is the common case and stays completely silent, and a warning
// about standing content is not the place to break that silence.
//
// The names trace to config and the errors to the filesystem, so a Driver rendering these to a
// terminal strips escapes from every text it gets back — this package composes, it does not
// sanitise.
func ContextFileNotices(r domain.ContextFilesReport) []ContextNotice {
	if len(r.Files) == 0 {
		return nil
	}

	loaded := make([]string, 0, len(r.Files))
	unreadable := make([]ContextNotice, 0, len(r.Files))
	for _, f := range r.Files {
		if f.Err != "" {
			unreadable = append(unreadable, ContextNotice{Text: "context: " + f.Name + " unreadable — " + f.Err, Anomaly: true})
			continue
		}
		loaded = append(loaded, f.Name+" ("+format.Bytes(f.Bytes)+")")
	}

	notices := make([]ContextNotice, 0, len(unreadable)+2)
	if len(loaded) > 0 {
		// The loaded line leads, so the unreadable ones read as exceptions to it.
		notices = append(notices, ContextNotice{Text: "context: " + strings.Join(loaded, ", ")})
	}
	notices = append(notices, unreadable...)
	if r.Oversize() {
		notices = append(notices, ContextNotice{
			Text: "standing system content ~" + format.TokensFine(r.StandingTokens) +
				" tokens exceeds its Budget share (~" + format.TokensFine(r.SystemShare) +
				") — trim context files, the task list or the system prompt",
			Anomaly: true,
		})
	}
	return notices
}
