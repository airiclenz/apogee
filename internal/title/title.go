// Package title builds and cleans up the session-naming completion — the cosmetic,
// out-of-band call that gives a new Session record a human title instead of the first line
// of the user's first prompt.
//
// It is deliberately two pure functions and nothing else. Prompt renders the request; Sanitize
// turns whatever text came back into a title or reports that nothing usable did. Neither one
// talks to a server, a Session record, or the TUI, so both are table-testable and the policy
// that surrounds them — when to fire, whether to apply the result, which title wins — lives
// entirely with the callers that own that state.
//
// The naming call is NOT a Mechanism and NOT a Turn (ADR 0022 addendum, 2026-07-31): it fires
// at no Hook point, never shapes the primary call, emits no events, and nothing breaks when it
// fails. That is why the failure posture here is a quiet ok=false rather than an error: the
// heuristic title the first Save already stamped stands, and a maintenance nicety must never
// nag.
package title

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/airiclenz/apogee/internal/provider"
)

// Naming-call sampling. A low temperature keeps the title descriptive rather than inventive
// (the compaction precedent, agent/compact.go), and the token cap is deliberately generous for
// so short an answer: small models routinely think inline before they answer, and a cap sized
// for the title alone truncates the reply mid-thought — which reads to Sanitize as a failure
// even though the model was on its way to a perfectly good name.
const (
	titleTemperature = 0.2
	titleMaxTokens   = 1024
)

// promptExcerptRunes bounds how much of the first prompt rides the naming call. The title
// describes the task, and a task is almost always stated in the opening sentences; a pasted
// stack trace or a whole file after them adds tokens and queue time without adding signal.
const promptExcerptRunes = 1500

// titleMaxRunes is the longest a title runs before word-boundary truncation. It matches the
// heuristic sessionTitle's cap (internal/tui/model.go) so a generated title and the fallback it
// replaces occupy the same width in the session browser.
const titleMaxRunes = 50

// titleWordBoundaryFloor is the earliest rune index at which truncation will break on a word
// boundary. Below it the boundary would throw away more of the title than the ellipsis saves,
// so a hard cut at titleMaxRunes is preferred — the heuristic's 60% rule, kept identical.
const titleWordBoundaryFloor = titleMaxRunes * 6 / 10

// maxAffixPasses bounds the strip-the-wrapping loop in Sanitize. Models stack these wrappers in
// any order (`"Title: fix the parser"` and `Title: "fix the parser"` are both common), so the
// affix strips run until the string stops changing rather than exactly once; the bound keeps
// that loop obviously terminating.
const maxAffixPasses = 4

// systemInstruction is the naming call's system prompt. It asks for the task description alone:
// the session browser already renders the time and the workspace beside the title, so a title
// that repeats either wastes the only 50 runes the row has (Ratified design 6).
const systemInstruction = "You name coding sessions. Read the user's first request and reply " +
	"with a short title for the work it asks for: 3 to 8 words, one line, plain text. " +
	"Describe the task only — never the project, the folder, or the date. " +
	"Reply with the title and nothing else: no quotes, no code fences, no label, no explanation."

// userInstruction closes the user message. The system prompt already said it, but small models
// answer the last thing they read, and repeating the constraint next to the material is what
// keeps the reply to one line.
const userInstruction = "Reply with the title only."

// Prompt builds the naming completion for firstPrompt — the request the composition root hands
// provider.Client.Respond. workspaceBase (the workspace directory's basename) and date are
// CONTEXT for the model, never title text; they are here so a bare "fix it" still earns a title
// that reads sensibly, not so they can be echoed back into it.
//
// Model is deliberately left empty: the Client's own configured model wins in buildBody, which
// is what binds the naming call to the session's current server and model (Ratified design 3).
// The request carries no tools and does not stream — it is one round-trip for one line of text.
func Prompt(firstPrompt, workspaceBase string, date time.Time) provider.Request {
	temperature, maxTokens := titleTemperature, titleMaxTokens
	return provider.Request{
		Messages: []provider.Message{
			{Role: "system", Content: systemInstruction},
			{Role: "user", Content: userMessage(firstPrompt, workspaceBase, date)},
		},
		Sampling: provider.Sampling{Temperature: &temperature, MaxTokens: &maxTokens},
	}
}

// userMessage renders the naming call's user message: the workspace and date as labelled
// context, then the bounded excerpt of the first prompt, then the closing instruction. The
// excerpt is fenced off by its own label so a prompt that itself contains instructions reads as
// quoted material rather than as something to obey.
func userMessage(firstPrompt, workspaceBase string, date time.Time) string {
	return fmt.Sprintf(
		"Workspace: %s\nDate: %s\n\nThe user's first request:\n%s\n\n%s",
		strings.TrimSpace(workspaceBase),
		date.Format("2006-01-02"),
		excerpt(firstPrompt),
		userInstruction,
	)
}

// excerpt trims s to promptExcerptRunes, marking a cut with an ellipsis so the model knows it is
// reading an opening rather than a whole request.
func excerpt(s string) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= promptExcerptRunes {
		return s
	}
	return strings.TrimRight(string(runes[:promptExcerptRunes]), " \t\n") + "…"
}

// Sanitize turns a raw reply into a session title, reporting ok=false when nothing usable
// survives. It is the single cleanup pipeline for BOTH title sources — the generated title and
// a manual `/rename <text>` — so the two can never disagree about what a title may contain.
//
// The pipeline, in order: strip a leading <think>…</think> block; take the first non-empty
// line, skipping code-fence marker lines as noise (small models fence their output even when
// told not to); strip ANSI and control escapes (the reply is untrusted model text that lands in
// a rendered browser row); strip the wrapping models add — surrounding quotes or backticks, a
// leading comment or heading marker, a leading "Title:" label — until the string stops
// changing; collapse inner whitespace; drop a trailing period; and word-boundary truncate to
// titleMaxRunes with an ellipsis.
//
// An empty result is a failure, not an empty title: the caller keeps whatever title the record
// already has.
func Sanitize(raw string) (string, bool) {
	s := firstContentLine(stripThinking(raw))
	s = stripAffixes(stripEscapes(s))
	s = collapseWhitespace(s)
	s = strings.TrimSpace(strings.TrimSuffix(s, "."))
	if s == "" {
		return "", false
	}
	return truncate(s), true
}

// thinkOpen and thinkClose delimit the reasoning block a thinking model may emit ahead of its
// answer. Only a LEADING block is stripped: a "</think>" appearing later is the model's own
// prose, not a delimiter, and cutting on it would discard the title.
const (
	thinkOpen  = "<think>"
	thinkClose = "</think>"
)

// stripThinking drops a leading <think>…</think> block and returns what follows. An unterminated
// block means the reply is all reasoning and no answer, so everything is dropped and Sanitize
// reports failure — which is correct: there was no title in it.
func stripThinking(s string) string {
	trimmed := strings.TrimSpace(s)
	lower := strings.ToLower(trimmed)
	if !strings.HasPrefix(lower, thinkOpen) {
		return trimmed
	}
	end := strings.Index(lower, thinkClose)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(trimmed[end+len(thinkClose):])
}

// firstContentLine returns the first line carrying something other than whitespace or a code
// fence marker. Fence markers are noise rather than content: a model asked for one bare line
// still wraps it in ``` often enough that treating the marker as the answer would poison a
// large share of titles.
func firstContentLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || fenceMarker(line) {
			continue
		}
		return line
	}
	return ""
}

// fenceMarker reports whether line is a bare code-fence marker — three or more backticks or
// tildes, optionally followed by a language tag. A fence opener followed by ordinary prose
// ("```fix the parser bug") is NOT a marker: that is the title with a stray fence glued to it,
// and the affix strip below takes the backticks off.
func fenceMarker(line string) bool {
	var marker string
	switch {
	case strings.HasPrefix(line, "```"):
		marker = "`"
	case strings.HasPrefix(line, "~~~"):
		marker = "~"
	default:
		return false
	}
	tag := strings.TrimLeft(line, marker)
	return tag == "" || !strings.ContainsAny(tag, " \t")
}

// stripEscapes removes ANSI escape sequences and every remaining control character. The reply is
// untrusted model text that ends up in a rendered session-browser row, so a title can never be
// allowed to carry cursor movement, colour, or an OSC sequence (the same posture as the
// transcript's escape strip). Callers pass a single line, so no newline survives to be lost.
func stripEscapes(s string) string {
	if !strings.ContainsFunc(s, strippableControl) {
		return s // the overwhelmingly common case: nothing to strip, nothing to allocate
	}
	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(runes); i++ {
		switch {
		case runes[i] == 0x1b:
			i += escapeRunes(runes[i:]) - 1
		case strippableControl(runes[i]):
			// dropped
		default:
			b.WriteRune(runes[i])
		}
	}
	return b.String()
}

// strippableControl reports whether r is a control character that must not survive into a title.
// Whitespace controls (a tab, most obviously) are exempt: they carry no escape and collapse into
// a single space one step later, so dropping them here would weld two words together.
func strippableControl(r rune) bool {
	return unicode.IsControl(r) && !unicode.IsSpace(r)
}

// escapeRunes reports how many runes the escape sequence beginning at runes[0] (an ESC) spans:
// a CSI runs to its final byte in @-~, an OSC to a BEL or a string terminator, and anything else
// is a two-character escape. An unterminated sequence swallows the rest of the line, which is
// the safe direction — a half-escape rendered literally is exactly what the strip exists to
// prevent.
func escapeRunes(runes []rune) int {
	if len(runes) < 2 {
		return 1
	}
	switch runes[1] {
	case '[':
		for i := 2; i < len(runes); i++ {
			if runes[i] >= '@' && runes[i] <= '~' {
				return i + 1
			}
		}
		return len(runes)
	case ']':
		for i := 2; i < len(runes); i++ {
			if runes[i] == 0x07 {
				return i + 1
			}
			if runes[i] == 0x1b && i+1 < len(runes) && runes[i+1] == '\\' {
				return i + 2
			}
		}
		return len(runes)
	default:
		return 2
	}
}

// stripAffixes peels the wrapping models put around a one-line answer. Each pass takes off a
// matched quote pair, a leading comment or heading marker, and a leading "Title:" label, and the
// passes repeat until nothing changes so the wrappers can arrive in any order.
func stripAffixes(s string) string {
	s = strings.TrimSpace(s)
	for i := 0; i < maxAffixPasses; i++ {
		before := s
		s = strings.TrimSpace(trimQuotes(s))
		s = strings.TrimSpace(trimMarker(s))
		s = strings.TrimSpace(trimLabel(s))
		if s == before {
			break
		}
	}
	return s
}

// quotePairs maps an opening quote to the closing quote that matches it. Only a MATCHED pair is
// stripped: an unbalanced trailing apostrophe is far more likely to be part of the words than a
// stray delimiter.
var quotePairs = map[rune]rune{
	'"':  '"',
	'\'': '\'',
	'`':  '`',
	'“':  '”',
	'‘':  '’',
	'«':  '»',
}

// trimQuotes removes one matched pair of surrounding quotes or backticks.
func trimQuotes(s string) string {
	runes := []rune(s)
	if len(runes) < 2 {
		return s
	}
	if closer, ok := quotePairs[runes[0]]; ok && runes[len(runes)-1] == closer {
		return string(runes[1 : len(runes)-1])
	}
	return s
}

// trimMarker removes a leading comment marker ("//"), markdown heading marker ("#", "##", …) or
// fence opener glued to the text ("```fix the parser" — a fence that never got its own line, so
// firstContentLine could not treat it as noise). Models reach for all three when asked for a
// bare label.
func trimMarker(s string) string {
	switch {
	case strings.HasPrefix(s, "```"):
		return strings.TrimLeft(s, "`")
	case strings.HasPrefix(s, "~~~"):
		return strings.TrimLeft(s, "~")
	case strings.HasPrefix(s, "//"):
		return strings.TrimPrefix(s, "//")
	case strings.HasPrefix(s, "#"):
		return strings.TrimLeft(s, "#")
	default:
		return s
	}
}

// titleLabel is the label a model prefixes when it answers the question rather than obeying it.
const titleLabel = "title:"

// trimLabel removes a leading, case-insensitive "Title:" label.
func trimLabel(s string) string {
	if len(s) >= len(titleLabel) && strings.EqualFold(s[:len(titleLabel)], titleLabel) {
		return s[len(titleLabel):]
	}
	return s
}

// collapseWhitespace reduces every run of whitespace to a single space and trims the ends, so a
// title laid out with tabs or double spaces occupies one predictable browser row.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// truncate caps s at titleMaxRunes, breaking on the last word boundary past
// titleWordBoundaryFloor and closing with an ellipsis. Everything is counted in RUNES, so a CJK
// title is capped by the characters the browser row shows rather than by its byte length.
func truncate(s string) string {
	runes := []rune(s)
	if len(runes) <= titleMaxRunes {
		return s
	}
	cut := runes[:titleMaxRunes]
	for i := len(cut) - 1; i > titleWordBoundaryFloor; i-- {
		if cut[i] == ' ' {
			cut = cut[:i]
			break
		}
	}
	return string(cut) + "…"
}
