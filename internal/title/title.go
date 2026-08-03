// Package title builds and cleans up the session-naming completion — the cosmetic,
// out-of-band call that gives a Session record a human title instead of the first line
// of the user's first prompt.
//
// It is deliberately two pure functions and nothing else. Prompt renders the request from a
// WINDOW of the user's requests — one entry for the automatic call at first-prompt submit,
// because one is all that exists when it fires; a bounded, budget-capped selection of the
// session's user side when the human asks for a name later. Sanitize turns whatever text came
// back into a title or reports that nothing usable did. Neither one talks to a server, a Session
// record, or the TUI, so both are table-testable and the policy that surrounds them — when to
// fire, whether to apply the result, which title wins — lives entirely with the callers that own
// that state.
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
	"unicode/utf8"

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

// promptExcerptRunes bounds how much of the request rides the naming call when the window holds
// exactly one — the automatic call at first-prompt submit. The title describes the task, and a
// task is almost always stated in the opening sentences; a pasted stack trace or a whole file
// after them adds tokens and queue time without adding signal.
const promptExcerptRunes = 1500

// The shape of the multi-request window an explicitly requested naming call reads. Every count is
// in RUNES, so a CJK session is bounded by the characters the model reads rather than by bytes.
// The opening request and the last windowTailEntries are mandatory and exempt from the budget —
// dropping one of those to satisfy a budget would defeat the selection, and the whole point is to
// see both where the session started and where it ended up. windowBudgetRunes is roughly a 4x
// prefill increase over the single-request form, which is affordable because it is paid only on a
// call the human explicitly asked for and is sitting there waiting on.
const (
	windowEntryRunes  = 400
	windowBudgetRunes = 6000
	windowTailEntries = 3
)

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

// systemInstruction is the naming call's system prompt, shared by both naming forms so the two
// cannot drift apart; it is worded to read correctly for one request as well as for many. It asks
// for the task description alone: the session browser already renders the time and the workspace
// beside the title, so a title that repeats either wastes the only 50 runes the row has (Ratified
// design 6). It asks for the DOMINANT thread rather than a list of the requests, and says outright
// that a session which moved on is named for what it moved to (ADR 0022 addendum): people look for
// a session by what they were last doing, and leaving the bias implicit would buy recency by
// accident — small models answer the last thing they read — rather than by instruction.
const systemInstruction = "You name coding sessions. Read what the user has asked for and reply " +
	"with a short title for the main thread of the work: 3 to 8 words, one line, plain text. " +
	"Name the dominant task rather than listing every request; when the session has moved on to " +
	"a different task, name the task it moved to. " +
	"Describe the task only — never the project, the folder, or the date. " +
	"Reply with the title and nothing else: no quotes, no code fences, no label, no explanation."

// userInstruction closes the user message. The system prompt already said it, but small models
// answer the last thing they read, and repeating the constraint next to the material is what
// keeps the reply to one line.
const userInstruction = "Reply with the title only."

// windowHeader labels the numbered block of requests. Naming the order outright is what lets the
// model read the last entries as the most recent work, which is the half of the instruction the
// numbering alone cannot carry.
const windowHeader = "The user's requests in this session, oldest first:"

// Prompt builds the naming completion for prompts — the window of the user's requests, oldest
// first, that the composition root hands provider.Client.Respond. One request is the automatic
// call at first-prompt submit, which is not a restriction but an identity: one is all that exists
// when it fires. Several is an explicitly requested regeneration, which reads a bounded window of
// the session's user side so a session that moved on can be named for what it moved to (ADR 0022
// addendum). workspaceBase (the workspace directory's basename) and date are CONTEXT for the
// model, never title text; they are here so a bare "fix it" still earns a title that reads
// sensibly, not so they can be echoed back into it.
//
// Model is deliberately left empty: the Client's own configured model wins in buildBody, which
// is what binds the naming call to the session's current server and model (Ratified design 3).
// The request carries no tools and does not stream — it is one round-trip for one line of text.
func Prompt(prompts []string, workspaceBase string, date time.Time) provider.Request {
	temperature, maxTokens := titleTemperature, titleMaxTokens
	return provider.Request{
		Messages: []provider.Message{
			{Role: "system", Content: systemInstruction},
			{Role: "user", Content: userMessage(prompts, workspaceBase, date)},
		},
		Sampling: provider.Sampling{Temperature: &temperature, MaxTokens: &maxTokens},
	}
}

// userMessage renders the naming call's user message from the prompts that carry a request. A
// window of one renders the single-request form the automatic call has always sent, byte for
// byte; several render as the numbered selection below. An empty window renders that same
// single-request form around an empty request: callers guard against calling with nothing to
// name, so this is a contract rather than a path anyone reaches.
func userMessage(prompts []string, workspaceBase string, date time.Time) string {
	requests := carryingText(prompts)
	switch len(requests) {
	case 0:
		return firstRequestMessage("", workspaceBase, date)
	case 1:
		return firstRequestMessage(requests[0], workspaceBase, date)
	default:
		return windowMessage(requests, workspaceBase, date)
	}
}

// carryingText drops the prompts that hold no request. The window arrives from the transcript,
// and an entry that is empty once trimmed is nothing to name: numbering it would give the model a
// blank line to describe and would make the elision marker's count disagree with the numbering.
func carryingText(prompts []string) []string {
	requests := make([]string, 0, len(prompts))
	for _, prompt := range prompts {
		if strings.TrimSpace(prompt) != "" {
			requests = append(requests, prompt)
		}
	}
	return requests
}

// firstRequestMessage renders the single-request form: the workspace and date as labelled
// context, then the bounded excerpt of the request, then the closing instruction. The excerpt is
// fenced off by its own label so a prompt that itself contains instructions reads as quoted
// material rather than as something to obey.
func firstRequestMessage(request, workspaceBase string, date time.Time) string {
	return fmt.Sprintf(
		"Workspace: %s\nDate: %s\n\nThe user's first request:\n%s\n\n%s",
		strings.TrimSpace(workspaceBase),
		date.Format("2006-01-02"),
		excerpt(request),
		userInstruction,
	)
}

// windowMessage renders several requests as a numbered list under the same labelled context,
// oldest first. Each request keeps its ORIGINAL 1-based position in the session's user side, so
// the run the budget dropped shows up as a gap in the numbering, and the single elision marker
// standing in that gap tells the model how many went missing rather than leaving it to infer that
// the session skipped from 1 to 12 on its own.
func windowMessage(requests []string, workspaceBase string, date time.Time) string {
	excerpts := make([]string, len(requests))
	for i, request := range requests {
		excerpts[i] = entryExcerpt(request)
	}

	lines := make([]string, 0, len(excerpts)+8)
	lines = append(lines,
		"Workspace: "+strings.TrimSpace(workspaceBase),
		"Date: "+date.Format("2006-01-02"),
		"",
		windowHeader,
		"",
	)
	previous := -1
	for _, i := range selectWindow(excerpts, windowBudgetRunes) {
		if omitted := i - previous - 1; omitted > 0 {
			lines = append(lines, elisionMarker(omitted))
		}
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, excerpts[i]))
		previous = i
	}
	lines = append(lines, "", userInstruction)

	return strings.Join(lines, "\n")
}

// selectWindow chooses which of excerpts ride the naming call and returns their indices in
// chronological order. excerpts holds at least two entries — userMessage renders fewer with the
// single-request form.
//
// The opening request and the last windowTailEntries are mandatory and exempt from budgetRunes;
// the rest are added newest-first while the running total lasts. The fill STOPS at the first
// request that does not fit rather than skipping it for an older, smaller one: that is what keeps
// the omitted entries a single contiguous run, and so the render to a single elision marker.
//
// budgetRunes is a parameter rather than a read of windowBudgetRunes so the mandatory set's
// exemption can be tested at a budget the per-entry cap is able to exhaust.
func selectWindow(excerpts []string, budgetRunes int) []int {
	tail := len(excerpts) - windowTailEntries
	if tail < 1 {
		tail = 1 // index 0 is mandatory on its own account; never let the tail swallow it
	}

	total := utf8.RuneCountInString(excerpts[0])
	for i := tail; i < len(excerpts); i++ {
		total += utf8.RuneCountInString(excerpts[i])
	}
	oldest := tail
	for i := tail - 1; i >= 1; i-- {
		cost := utf8.RuneCountInString(excerpts[i])
		if total+cost > budgetRunes {
			break
		}
		total += cost
		oldest = i
	}

	included := make([]int, 0, len(excerpts)-oldest+1)
	included = append(included, 0)
	for i := oldest; i < len(excerpts); i++ {
		included = append(included, i)
	}
	return included
}

// elisionOpen opens the marker line that stands where the omitted requests sat. It is a distinct
// prefix so the marker can never be read as one more numbered request.
const elisionOpen = "(… "

// elisionMarker renders the marker for a run of omitted requests, saying how many went missing so
// the numbering gap reads as deliberate.
func elisionMarker(omitted int) string {
	noun := "requests"
	if omitted == 1 {
		noun = "request"
	}
	return fmt.Sprintf("%s%d earlier %s omitted …)", elisionOpen, omitted, noun)
}

// entryExcerpt renders one request of a multi-request window as a single line: whitespace
// collapsed, so a pasted stack trace cannot break the numbering apart, then capped at
// windowEntryRunes so one huge request cannot crowd the rest of the window out.
func entryExcerpt(request string) string {
	return capRunes(collapseWhitespace(request), windowEntryRunes)
}

// excerpt trims s to promptExcerptRunes, the single-request form's far more generous cap.
func excerpt(s string) string {
	return capRunes(strings.TrimSpace(s), promptExcerptRunes)
}

// capRunes trims s to limit RUNES — the characters the model reads, not bytes — marking a cut
// with an ellipsis so the model knows it is reading an opening rather than a whole request.
func capRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return strings.TrimRight(string(runes[:limit]), " \t\n") + "…"
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
	s = stripAffixes(StripEscapes(s))
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

// StripEscapes removes ANSI escape sequences and every remaining control character. The reply is
// untrusted model text that ends up in a rendered session-browser row, so a title can never be
// allowed to carry cursor movement, colour, or an OSC sequence (the same posture as the
// transcript's escape strip). Callers pass a single line, so no newline survives to be lost.
//
// It is exported because it is the one definition of "a title carries no escape sequence and no
// control character", and a title is read outside this package too — the TUI renders the live
// session's name onto the frame, where a control character is a LAYOUT bug as much as a trust one:
// it breaks the measure of a row that is squared to the window. A second, weaker strip written at
// such a seam is exactly how two definitions of the same guarantee drift apart, so every seam calls
// this one. Whitespace controls stay exempt for every consumer alike (strippableControl); each
// collapses them a step later, and all do.
func StripEscapes(s string) string {
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
