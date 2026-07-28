package domain

// The workspace context files' REPORT — what a session actually loaded (Config.ContextFiles)
// and what the standing system content costs against the Budget's own share of the window.
// It is a read-only view the engine composes at a session boundary and the host renders as
// its session notice; nothing here reaches the model. The types live in domain because both
// sides of that seam speak them (ADR 0010), and the one predicate over them — Oversize — is
// pure arithmetic on a domain value, so it lives here rather than in the host's formatting.

// ContextFileNote is one file's line of the session notice: the name as configured plus either
// the size that was loaded or the reason it could not be. Exactly one of Bytes and Err is
// meaningful — a readable file reports its byte size with an empty Err, an unreadable one
// reports the error with a zero size (the LOUD skip: a discovered file that is present but
// unreadable is worth a line, never a startup failure). A file that was missing, empty, or
// whitespace-only produces no note at all: absence is the common case and stays silent.
type ContextFileNote struct {
	Name  string // the workspace-relative name as configured, and the header its content rides under
	Bytes int    // the loaded content's size in bytes; 0 when Err is set
	Err   string // why a present file could not be read; empty when the file loaded
}

// ContextFilesReport is what one session's workspace context files contributed, together with
// the token cost of the standing system content they ride in. Files carries a note per loaded
// or unreadable file in list order (empty when a repo has none of the configured names — the
// common case, which the host reports by saying nothing). StandingTokens is the estimated token
// size of the WHOLE standing system content — the rendered system prompt and the context-file
// blocks, exactly as seeded into a request — and SystemShare is the Budget's SystemPrompt
// allocation, the share that content is meant to fit in. Both are measured at the moment the
// report is taken, so a report taken after the context window bound knows what a report taken
// during a cold start could not; SystemShare is 0 while the window is unknown.
type ContextFilesReport struct {
	Files          []ContextFileNote
	StandingTokens int
	SystemShare    int
}

// Oversize reports whether the standing system content has outgrown the window share allocated
// to it — the single condition behind the host's warning line. An unknown window (a zero share,
// so nothing was allocated) never trips it: the same "no basis to bound" rule the Budget's own
// allocation-gated comparisons keep. It is advisory in the strongest sense — nothing is capped
// or truncated on the strength of it; the human is simply told.
func (r ContextFilesReport) Oversize() bool {
	return r.SystemShare > 0 && r.StandingTokens > r.SystemShare
}
