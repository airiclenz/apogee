package tui

import (
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/probe"
)

// NoticeKind names one kind of start-up notice the transcript places — what the notice is about, as
// against what it says. The kind is the producer's only decision: whether the notice needs the human
// (its class) and where it comes from (its source) are declared once, in [noticeKinds], on this side
// of the seam, so the TUI Driver owns its own routing and the composers in internal/notice and
// internal/probe keep returning text only (internal/notice/doc.go).
//
// The zero value is reserved for a notice whose producer named no kind, and is classed LOUD: a
// notice nobody classified is never folded away into the session log where the human might miss it.
type NoticeKind int

const (
	// kindUnset is the reserved zero kind: a Notice whose producer set no Kind.
	kindUnset NoticeKind = iota

	// NoticeAutoUnconfined: Auto is running with workspace confinement off (ADR 0012).
	NoticeAutoUnconfined
	// NoticeAutoDegraded: Auto asked for confinement on a host whose backend cannot enforce it.
	NoticeAutoDegraded
	// NoticeFenceResidual: the confinement backend cannot fence some token — the residual
	// disclosure, routine by ADR 0112 amend-1112.
	NoticeFenceResidual
	// NoticeShareGroupConfig: with `sessions.share-group:` set, the shared home exposes the
	// config.yaml.
	NoticeShareGroupConfig
	// NoticeMCPTruncated: an MCP server's offer was cut short at start-up.
	NoticeMCPTruncated
	// NoticeMCPSkipped: an MCP server was skipped at start-up.
	NoticeMCPSkipped
	// NoticeModelBinding: what resolving the newly bound model decided — a profile match, the
	// roster deltas it brings.
	NoticeModelBinding
	// NoticeTrustedHint: the bound model is the configured id the server never advertised.
	NoticeTrustedHint
	// NoticeRosterUnapplied: the bound model's profile roster was refused by the swap door.
	NoticeRosterUnapplied

	// kindContextLoaded: the plain record of which context files loaded.
	kindContextLoaded
	// kindContextAnomaly: a context file present but unreadable, or standing content over its share.
	kindContextAnomaly
	// kindColorScheme: loading the configured colour scheme cost something — an unknown name, an
	// unreadable file, a key whose value is not a colour (Options.ColorSchemeWarnings).
	kindColorScheme
	// kindGitRefusal: git refused to read the host workspace, and the footer's `git refused` marker
	// is explained once ([Model.noteGitRefusal]).
	kindGitRefusal
)

// noticeClass is whether a notice needs the human. A LOUD notice keeps its own note line; a ROUTINE
// one is a record of the start-up the human may read at leisure, gathered into the session log while
// it is open (layout.md, "The start-up box").
type noticeClass int

const (
	classLoud noticeClass = iota
	classRoutine
)

// noticeSource is where a notice comes from, which decides when it is raised again.
type noticeSource int

const (
	// sourceBoundary: composed by the binary for the launch (Options.StartupNotices).
	sourceBoundary noticeSource = iota
	// sourceAmbient: re-derived from the live workspace at every session boundary (context files).
	sourceAmbient
	// sourceBeat: raised by a rebind (RebindResult.Notices) — the session's first-contact beat among
	// them.
	sourceBeat
)

// noticeKindRow is what one [NoticeKind] declares: its class and its source.
type noticeKindRow struct {
	class  noticeClass
	source noticeSource
}

// noticeKinds is the one table every kind's class and source are read from. A kind added to the
// const block above takes its row here; TestNoticeKindClasses pins each.
var noticeKinds = map[NoticeKind]noticeKindRow{
	kindUnset:              {class: classLoud, source: sourceBoundary},
	NoticeAutoUnconfined:   {class: classLoud, source: sourceBoundary},
	NoticeAutoDegraded:     {class: classLoud, source: sourceBoundary},
	NoticeFenceResidual:    {class: classRoutine, source: sourceBoundary},
	NoticeShareGroupConfig: {class: classLoud, source: sourceBoundary},
	NoticeMCPTruncated:     {class: classLoud, source: sourceBoundary},
	NoticeMCPSkipped:       {class: classLoud, source: sourceBoundary},
	NoticeModelBinding:     {class: classRoutine, source: sourceBeat},
	NoticeTrustedHint:      {class: classRoutine, source: sourceBeat},
	NoticeRosterUnapplied:  {class: classLoud, source: sourceBeat},
	kindContextLoaded:      {class: classRoutine, source: sourceAmbient},
	kindContextAnomaly:     {class: classLoud, source: sourceAmbient},
	kindColorScheme:        {class: classLoud, source: sourceAmbient},
	kindGitRefusal:         {class: classRoutine, source: sourceAmbient},
}

// kindRow is k's row in [noticeKinds]; a kind with no row reads as the reserved zero kind's, so an
// undeclared kind is loud.
func kindRow(k NoticeKind) noticeKindRow {
	if row, ok := noticeKinds[k]; ok {
		return row
	}
	return noticeKinds[kindUnset]
}

// classOf is k's class, read through the table.
func classOf(k NoticeKind) noticeClass {
	return kindRow(k).class
}

// transcriptNotes is what the transcript places for n, one note each, in order, where its class puts
// them: the TUI's own wording for a kind whose stderr text does not read right there, and Text as the
// one note for every other kind.
//
// The residual disclosure ([NoticeFenceResidual]) is the one kind worded here: stderr's header over
// indented clause lines becomes one "<backend> cannot fence <token> — <consequence>" note per
// residual, composed by probe.ResidualNotes from the same backend and capabilities the binary built
// the stderr text from (Options.Confinement). The kind attests that the disclosure's gate fired —
// Auto, with confinement asked for — so the gate's inputs are passed as fired. A notice the
// confinement facts cannot word (the zero ConfinementInfo) falls back to Text.
func (m *Model) transcriptNotes(n Notice) []string {
	if n.Kind == NoticeFenceResidual {
		info := m.opts.Confinement
		if notes := probe.ResidualNotes(info.Backend, info.Caps, domain.ModeAuto, true); len(notes) > 0 {
			return notes
		}
	}
	return []string{n.Text}
}
