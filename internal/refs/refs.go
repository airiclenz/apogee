package refs

import "strings"

// Span is one resolving token of the prompt mini-language, LOCATED in the text: the byte range
// [Start,End) it occupies and the Name it resolves to (a workspace-relative path for an @ref, a
// skill id for a "/" one). Two readers want different halves of that pair — a sender takes the
// names to travel with the message, an accenting renderer takes the ranges to paint — so both
// grammars are scanned in exactly ONE place and the two readers can never disagree about what is
// a token.
type Span struct {
	Start, End int
	Name       string
}

// FileSpans locates the @file references in s. An @-ref is an "@" at the start of s or
// immediately after whitespace — so an email like foo@bar.com (where "@" follows a non-space) is
// not a reference — followed by a token in one of two forms ([ScanToken] owns the grammar):
//
//   - bare: a run of non-whitespace characters, @internal/agent/loop.go;
//   - quoted: @"path with spaces" or @'path with spaces' — both quote characters are
//     accepted, the closing quote ends the token so ordinary text may follow it, and an
//     unterminated quote runs to the end of that line. There are no escape sequences.
//
// The span covers the literal token, quotes included; the name is the path without the "@" and
// without any quotes. A token naming nothing (a bare "@", an empty quoted pair) is skipped, but
// the scan still resumes past it.
func FileSpans(s string) []Span {
	var spans []Span
	for i := 0; i < len(s); i++ {
		if s[i] != '@' {
			continue
		}
		if i > 0 && !IsSpace(s[i-1]) { // not at a word boundary ⇒ not a ref (e.g. an email)
			continue
		}
		path, end := ScanToken(s, i+1)
		if path != "" {
			spans = append(spans, Span{Start: i, End: end, Name: path})
		}
		i = end - 1 // resume scanning past this token (the loop's own i++ lands on end)
	}
	return spans
}

// SkillSpans locates the inline skill references in s. A skill reference is the exact mirror of
// an @file one — the same word-boundary, whitespace-delimited grammar, and the same "the token
// stays in the text" rule — so the two halves of the prompt mini-language read alike:
//
//	/code-audit please check @internal/tui/command.go
//
// The token must start at the beginning of s or immediately after whitespace, and it runs to the
// next whitespace byte. Only a token whose bare name `known` confirms as a catalog ID counts:
// every other slash-prefixed word is ordinary prose, which is what lets a path (/usr/bin), a
// fraction (and/or) or a typo (/code-adit) travel to the model untouched. A nil `known` means no
// catalog is wired, so every token is prose and no span is located. Skill IDs are directory names
// and so never contain whitespace, which is why this grammar needs no quoted form.
func SkillSpans(s string, known func(string) bool) []Span {
	if known == nil {
		return nil
	}
	var spans []Span
	for i := 0; i < len(s); i++ {
		if s[i] != '/' {
			continue
		}
		if i > 0 && !IsSpace(s[i-1]) { // not at a word boundary ⇒ prose (a path, a fraction)
			continue
		}
		end := i + 1
		for end < len(s) && !IsSpace(s[end]) {
			end++
		}
		if id := s[i+1 : end]; id != "" && known(id) {
			spans = append(spans, Span{Start: i, End: end, Name: id})
		}
		i = end - 1 // resume past this token (the loop's own i++ lands on end)
	}
	return spans
}

// Names reduces located tokens to the names they resolve to, de-duplicated in first-seen
// order — so @x and @"x" collapse to one reference, and a skill named twice is invoked once.
func Names(spans []Span) []string {
	var names []string
	seen := map[string]bool{}
	for _, sp := range spans {
		if seen[sp.Name] {
			continue
		}
		seen[sp.Name] = true
		names = append(names, sp.Name)
	}
	return names
}

// FileRefs returns the workspace-relative paths the @file references of s name ([FileSpans] owns
// the grammar), de-duplicated in first-seen order. The literal @token — quotes included — belongs
// in the text the caller sends on, so the model sees what the human pointed at.
func FileRefs(s string) []string {
	return Names(FileSpans(s))
}

// SkillRefs returns the catalog IDs the inline "/" tokens of s name ([SkillSpans] owns the
// grammar), de-duplicated in first-seen order. The literal token belongs in the text too — the
// owner's explicit choice over stripping it: the model sees the invocation the human typed AND
// the skill body the agent prepends for it.
func SkillRefs(s string, known func(string) bool) []string {
	return Names(SkillSpans(s, known))
}

// ScanToken scans the token of an @file reference and reports the referenced path together
// with the offset just past the token. start is the byte immediately after the "@"; the caller
// owns the word-boundary rule.
//
// A token opening with a quote character (" or ') runs to the next occurrence of that same
// character on the same line: the path is the text between the quotes, and the closing quote
// ends the token — anything after it (a comma, more prose) is ordinary text again. An
// unterminated quote runs to the end of the line ("\n") or of s, with the path right-trimmed of
// spaces and tabs: a word-boundary @" is unambiguous intent, and a token never crosses a
// newline, so a stray quote cannot swallow the rest of a multi-line message. There are no
// escape sequences — a path containing " is quoted with ', and vice versa. Any other token is
// the bare form: a run of non-whitespace bytes.
func ScanToken(s string, start int) (string, int) {
	if start >= len(s) {
		return "", start
	}
	if quote := s[start]; quote == '"' || quote == '\'' {
		for j := start + 1; j < len(s); j++ {
			switch s[j] {
			case quote:
				return s[start+1 : j], j + 1
			case '\n':
				return strings.TrimRight(s[start+1:j], " \t"), j
			}
		}
		return strings.TrimRight(s[start+1:], " \t"), len(s)
	}
	j := start
	for j < len(s) && !IsSpace(s[j]) {
		j++
	}
	return s[start:j], j
}

// IsSpace reports whether b is an ASCII whitespace byte used as a token boundary. A newline
// counts, which is what stops either grammar's token from crossing a line.
func IsSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
