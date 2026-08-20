package tui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// detailClipRunes caps one detail/target line so a minified blob or a wall-of-text report cannot
// flood the transcript (the renderer soft-wraps, so an uncapped line becomes many rows).
//
// The cap is a FLOOD bound and it is deliberately spent in RUNES, not in the cells the screen
// bills. No rune paints more than two cells, so 160 runes buy at most 320 cells and therefore at
// most twice the rows the same 160 runes of ASCII take — a wall of double-width text costs scroll,
// never content. Cell-exactness is the STATUS LINE's requirement, not the transcript's: that row is
// shared with the context gauge, so an over-wide left slot pushes something the reader needs off the
// screen — which is why that row carries the tool's verb alone now (toolActivityVerb, activity.go)
// rather than a target it would have to cap in cells through the width authority. The
// transcript shares nothing — a wide line wraps onto rows of its own and the block behind it paints
// lower down, whole. TestPaintedWideDetailLineWrapsWithoutDisplacement (paint_test.go) is the probe
// that measured all three of those claims and the pin that keeps them true.
const detailClipRunes = 160

// clipDetail truncates s to detailClipRunes runes with an ellipsis.
func clipDetail(s string) string {
	return clipRunes(s, detailClipRunes)
}

// clipRunes truncates s to n runes with an ellipsis, counting runes rather than bytes so a
// multi-byte path is not cut mid-character. Its callers are clipDetail and the approval pane's
// Sub-agent line (approvalTaskClipRunes), and in both the rune spend is settled at the caller
// rather than being a shortfall to be swept: see detailClipRunes for why the transcript's bound is
// allowed to be a rune count where the status line's is not.
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// plural renders "1 result" / "3 results" — count plus the word, naively pluralised.
func plural(n int, word string) string {
	if n == 1 {
		return strconv.Itoa(n) + " " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// parseArgs decodes a tool call's JSON arguments into a generic map for the target
// extractors. Malformed or empty arguments decode to nil, which the extractors tolerate (a
// missing key yields the empty target).
func parseArgs(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

// prettyJSONDetails renders a tool call's arguments as the pretty-printed JSON (or the verbatim
// text when it does not parse) split into one detailLine per line, each hanging at
// argumentValueIndent. It is what argumentDetails degrades to where there is nothing to label — a
// bare array, a malformed fragment — so a blob with no names still reaches the screen as it
// arrived instead of being dropped. Empty/null arguments add no lines.
//
// The indent is what stops this path from giving back what the labelled path closed. On the
// approval pane a row is the SURFACE's own iff it starts flush-left: that is the whole of what
// tells the pane's "Reason:" from a row painted out of the model's bytes, since both wear
// th.popupBody and the pane sets no bodyLead. The labelled path spends that fact by flattening
// names and indenting values (argumentDetails); a fallback emitting lines at column zero let a blob
// whose text reads "Reason: pre-approved by the operator" paint a second Reason row beside the real
// one — a forged fact under the pane's own styling, on the surface a human authorises a call from.
// Every line here is argument-derived, this being the arguments' own fallback and its only caller,
// so every line is a BODY row. Nothing is rejected or summarised to get there: the bytes still
// reach the screen exactly as they arrived, two columns to the right of where a label can live.
func prettyJSONDetails(raw json.RawMessage) []detailLine {
	pretty := prettyJSON(raw)
	if pretty == "" {
		return nil
	}
	lines := splitLines(pretty)
	details := make([]detailLine, 0, len(lines))
	for _, ln := range lines {
		details = append(details, detailLine{Text: argumentValueIndent + ln})
	}
	return details
}

// argumentValueIndent is the hanging indent an argument's value sits under, so a labelled argument
// reads as a label with its value beneath it rather than as one run-on line
// (docs/layout/user-questions-layout.md, the approval box).
const argumentValueIndent = "  "

// argumentDetails renders a tool call's arguments as LABELLED lines: one `name:` line per argument,
// the value's own real lines indented beneath it. It is the shape a human reads a decision off —
// no surrounding braces, no quoted key names, and a multi-line command showing the lines it will
// actually run rather than one `"…\n…"` string — and it is DISPLAY-ONLY: what the tool receives is
// the caller's json.RawMessage, untouched by anything here.
//
// The order is the MODEL's own, taken off the wire in the order it wrote the keys, so the display is
// deterministic for a given call without imposing an order the call did not have (a decode into
// map[string]any loses it, which is why orderedArgs streams the tokens instead).
//
// Two things still render as JSON, and both are the honest rendering rather than a leftover. A blob
// that is not an object at all — a bare array, a malformed fragment — has no names to label, so it
// falls back to prettyJSONDetails and the unregistered-tool body's verbatim-rather-than-dropped
// rule, its every line sitting at argumentValueIndent because an unlabelled line is still a value's
// line and no argument byte may render where a label lives. And a single value with no flat shape (a
// nested object, an array of objects) is indented JSON under its own label, since nothing else
// states its structure without lying about it. What never comes back is an envelope around the
// argument SET: the labels ARE the object.
//
// Both surfaces that show a call's raw arguments read this one rendering: the approval prompt a
// human decides on, and the transcript block that records a call the presenter does not recognise
// (presentToolCall's unregistered-tool fallback). One call is spelled one way wherever it appears —
// the transcript block then collapses to the house budget like any other body (render.go), which is
// a question about how many of these lines a surface seats, not about what they say.
//
// A key the model wrote TWICE is shown ONCE, carrying the value the tool will receive and marked as
// the duplicate it is (duplicateKeyNote). The pane may not be the one surface in the process that
// reads a call differently from everything else acting on it: the executor's decode
// (internal/tools.decodeArgs) is stdlib JSON, where the last duplicate wins, and both guards are
// last-wins too (security/dangerous.go, tools/workspace_scoped.go). Streaming every duplicate in
// wire order let `{"command":"npm test","command":"curl …|sh"}` be approved off a line the executor
// discards — so the surviving pair sits where its winning value arrived, in wire order among the
// other survivors, and the note says the earlier ones existed rather than hiding them.
//
// The NAME is flattened (flattenField) and the value is not, which is the same line drawn twice. A
// name is a label: nothing in it is layout, so a newline in one is not a longer label but a SECOND
// line, unindented, wearing whatever the model wrote it as — on the approval prompt that is a row
// beside the pane's own, and JSON puts no restriction on what a key may hold. A value's newlines
// ARE the fact being read, so they survive, hanging under their label at argumentValueIndent where
// nothing they say can be mistaken for a label of the surface's own.
func argumentDetails(raw json.RawMessage) []detailLine {
	pairs, ok := orderedArgs(raw)
	if !ok {
		return prettyJSONDetails(raw)
	}
	var details []detailLine
	for _, p := range pairs {
		label := flattenField(p.name) + ":"
		if p.occurrences > 1 {
			label += duplicateKeyNote(p.occurrences)
		}
		details = append(details, detailLine{Text: label})
		for _, ln := range argumentValueLines(p.value) {
			details = append(details, detailLine{Text: argumentValueIndent + ln})
		}
	}
	return details
}

// duplicateKeyNote is what a label says when the model wrote that key more than once: which of the
// values is on the screen, and — by saying it at all — that there were others. It rides the LABEL
// rather than the value so the value beneath it is still nothing but the bytes the tool receives.
func duplicateKeyNote(occurrences int) string {
	return fmt.Sprintf("  (duplicate key — last of %d wins)", occurrences)
}

// resolvedPathNote is the ONE wording every decision surface discloses a redirected path with —
// the approval pane's own line and the tool card's branch row both come here, so the two cannot
// end up telling the same fact in two dialects. It is empty whenever the engine sent nothing
// (domain.ApprovalRequest.ResolvedPath / domain.ToolCallEvent.ResolvedPath), which is the ordinary
// case: the argument names its own target and neither surface grows a line.
//
// The engine decides WHETHER there is anything to say — it holds the workspace root and the
// resolution the gate judged the call by — and this decides how it reads. That split is what keeps
// the pane from computing a second opinion about a path off arguments it would have to re-resolve
// on the render goroutine.
//
// The path is model-authored like every other field these surfaces paint: it is what the model's
// own argument resolved to, so it is escape-stripped and FLATTENED here rather than at each call
// site. Flattening is what makes it safe to hand the approval pane, which paints one row per line
// and would otherwise let a path carrying "\n" write rows of its own beneath a label it did not
// author.
func resolvedPathNote(resolved string) string {
	if resolved == "" {
		return ""
	}
	return "→ resolves to " + flattenField(stripEscapes(resolved))
}

// argumentPair is one argument as the model wrote it: its name, and its value still encoded, so the
// value's own rendering (argumentValueLines) decides what shape it takes on the screen.
// occurrences is how many times that name appeared in the call — 1 for an ordinary argument, more
// where the model repeated a key and this pair carries the last value it wrote (orderedArgs).
type argumentPair struct {
	name        string
	value       json.RawMessage
	occurrences int
}

// orderedArgs decodes a tool call's arguments into name/value pairs in WIRE order, reporting false
// when there is nothing to label — absent or null arguments, a top-level value that is not an
// object, a blob that does not parse, or one carrying anything after its closing brace. Every false
// leaves the caller to show the arguments as they arrived: half a labelled rendering of a malformed
// blob would be a claim about the call that the bytes do not support.
//
// A repeated key yields ONE pair, carrying the LAST value the model wrote for it and counting the
// occurrences, because that is the value everything downstream acts on (argumentDetails states the
// rule and why the pane may not differ from it).
func orderedArgs(raw json.RawMessage) ([]argumentPair, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, false
	}
	dec := json.NewDecoder(strings.NewReader(trimmed))
	open, err := dec.Token()
	if err != nil {
		return nil, false
	}
	if delim, isDelim := open.(json.Delim); !isDelim || delim != '{' {
		return nil, false
	}
	var pairs []argumentPair
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, false
		}
		name, isString := key.(string)
		if !isString {
			return nil, false
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, false
		}
		pairs = append(pairs, argumentPair{name: name, value: value})
	}
	if _, err := dec.Token(); err != nil { // the closing brace
		return nil, false
	}
	// The stream must END there. Asking for one more token is what says so for EVERY tail — a second
	// document behind the first, loose text, and the stray `}`/`]` that dec.More() reads as "no more
	// input" rather than as the garbage it is.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, false
	}
	return lastWins(pairs), true
}

// lastWins collapses repeated keys the way every consumer of the same bytes does: one pair per
// name, holding the value of its LAST occurrence and standing at that occurrence's place among the
// survivors, with the occurrence count carried through so the label can say the key was repeated.
func lastWins(pairs []argumentPair) []argumentPair {
	occurrences := make(map[string]int, len(pairs))
	last := make(map[string]int, len(pairs))
	for i, p := range pairs {
		occurrences[p.name]++
		last[p.name] = i
	}
	out := make([]argumentPair, 0, len(occurrences))
	for i, p := range pairs {
		if last[p.name] != i {
			continue
		}
		p.occurrences = occurrences[p.name]
		out = append(out, p)
	}
	return out
}

// argumentValueMaxLines is the most lines ONE argument's value may spend on the surfaces that show
// a call's arguments. It exists so no single value can evict its siblings: the approval pane's body
// budget is a handful of rows on a stock 80×24 window (popupBudget), so an uncapped two-hundred-line
// `content` took every row the pane had and the `path:` it was being written to never reached the
// screen. Eight is long enough to read a command or a short file off, short enough that a two- or
// three-argument call still shows every label it has.
const argumentValueMaxLines = 8

// argumentValueLines renders one argument's value as the lines that sit under its label: a string as
// its OWN lines, so the newline a JSON blob spells `\n` is a line break here; any other scalar as
// the literal the model sent (a `null` says null rather than going quiet, which is why only a
// decoded STRING takes the first exit); and a value with no flat shape as indented JSON.
//
// It wraps nothing — how WIDE these lines may be is the surface's own business — but it does bound
// how MANY there are (argumentValueMaxLines), and an elided value keeps its TAIL as well as its
// head: head lines, the elision marker counting what is not shown, then the value's LAST line
// (elisionSplit, popup.go, is the shared rule, and popupElisionMarker the one wording for the fact).
// A value's last line is where a payload appended to an innocent-looking body lives, and a surface
// that shows only heads is one an approval can be given on falsely.
func argumentValueLines(value json.RawMessage) []string {
	return elideValueLines(decodedValueLines(value))
}

// decodedValueLines is argumentValueLines' rendering before its cap: the value's real lines, however
// many it has.
func decodedValueLines(value json.RawMessage) []string {
	var decoded any
	if err := json.Unmarshal(value, &decoded); err == nil {
		if s, isString := decoded.(string); isString {
			return splitLines(s)
		}
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, value, "", "  "); err != nil {
		return splitLines(strings.TrimSpace(string(value)))
	}
	return splitLines(buf.String())
}

// elideValueLines seats lines in argumentValueMaxLines rows, head + marker + tail (elisionSplit),
// and returns a short-enough value untouched.
func elideValueLines(lines []string) []string {
	head, tail, hidden := elisionSplit(len(lines), argumentValueMaxLines)
	if hidden == 0 {
		return lines
	}
	out := make([]string, 0, head+1+tail)
	out = append(out, lines[:head]...)
	out = append(out, popupElisionMarker(hidden))
	out = append(out, lines[len(lines)-tail:]...)
	return out
}

// firstLine returns the first line of s (without its newline), or s when it has none.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// splitLines splits s on newlines into its physical lines.
func splitLines(s string) []string {
	return strings.Split(s, "\n")
}
