// Package refs owns the parse half of apogee's prompt mini-language: the two inline reference
// grammars a user types into a message — the File reference (@path) and the Skill /token —
// located as byte spans in the text they came from.
//
// It answers exactly one question, "what did the human point at, and where in the text": a
// [Span] carries the byte range of the literal token and the name it resolves to (a
// workspace-relative path for an @ref, a skill id for a "/" one). RESOLUTION is the agent's job,
// not this package's — reading a referenced file within the workspace fence and injecting it, or
// prepending a skill body, happens above, and this package never touches a filesystem, a catalog
// or a config. Nothing here allocates beyond the spans it returns, so all of it is table-testable.
//
// The grammar itself is spelled by the doc comments on [FileSpans], [SkillSpans] and [ScanToken],
// which are its specification: a token starts at a word boundary (which is what keeps an email
// address out), runs to whitespace in its bare form or between quotes in its quoted one, admits
// no escape sequences, and never crosses a newline. CONTEXT.md's "File reference (@file)" and
// "Skill" entries are the domain-language side of the same rule.
//
// The grammar lives below every Driver because more than one reader wants it: the TUI both sends
// the names with a message and paints the token ranges in the prompt box, and a Firing's prompt
// (internal/run) resolves the same @refs with no TUI in the process at all. It is the direction
// ADR 0010 lets a shared rule move — a rule two Drivers spell identically belongs under both, not
// inside one of them.
package refs
