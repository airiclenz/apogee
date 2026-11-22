package config

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ----------------------------------------------------------------------------
// The scalar writer's splice machinery
// ----------------------------------------------------------------------------
//
// Split out of configwrite_scalar.go, which keeps the writer core — the entry points, the value
// rendering and the splice drivers. What lives here is what those drivers reach for: where in the
// parsed document a settings key stands (ScalarTarget), how a text key's block is rendered and
// bounded, and where a key the file does not set yet is inserted, including the commented-example
// scan that decides it. The split is by size alone (coding-standards' ~400-line guide) and moved
// nothing else: every function reads and behaves exactly as it did in one file.

// ScalarTarget is where a key stands in the parsed document: the nodes of its active `key: value`
// (nil when the file does not set it) and, for a nested key, the blocks above it — the IMMEDIATE
// parent in the Parent fields, which the side-panel block writer (configwrite_sections.go) builds and
// reads, and the whole chain beside them, which is what lets one writer reach a key at any depth.
type ScalarTarget struct {
	Key        string     // the leaf key as the file spells it
	Kind       Kind       // what the key holds, and with it the value shapes its line may carry
	Parent     string     // the block the key sits in, empty for a top-level key
	KeyNode    *yaml.Node // the leaf key, nil when the key has no active line
	ValueNode  *yaml.Node // its value, nil with keyNode
	ParentKey  *yaml.Node // the block's key, nil when the block is absent
	ParentBody *yaml.Node // the block's value, nil when the block is absent
	ParentNull bool       // the block's key is there with nothing under it yet

	chain   []string      // every block the key sits in, outermost first; empty for a top-level key
	found   []scalarBlock // the leading run of chain the file holds, outermost first
	missing []string      // the rest of chain, which an insert creates under the deepest found block
}

// scalarBlock is one block of a key's parent chain as the file holds it: the block's key and its
// value, which is a null node while the block is written with nothing under it yet.
type scalarBlock struct {
	key  *yaml.Node
	body *yaml.Node
}

// isSet reports whether the file carries an active line for the key — the difference between a
// replace and an insert, and between a reset and a no-op.
func (t ScalarTarget) IsSet() bool { return t.KeyNode != nil }

// ScalarTargetIn locates the key in the parsed document, walking one block per segment of its path
// however deep that path reaches. Shapes it refuses rather than splices, at every level of the walk:
// a top level that is not a mapping of settings, a flow-style mapping (no line to edit — the
// flow-style list refusal spliceHostAcknowledgement makes (configwrite.go), one level down), and
// a block key holding something other than a block. Every one of them means the text and the node
// tree would disagree about where the key's line is.
//
// The walk stops at the first block the file does not hold, or holds with nothing under it: below
// that there are no children to find, and the blocks from there down are the ones an insert creates.
func ScalarTargetIn(doc *yaml.Node, k Key) (ScalarTarget, error) {
	segments := strings.Split(k.Path, ".")
	last := len(segments) - 1
	t := ScalarTarget{Key: segments[last], Kind: k.Kind, chain: segments[:last], missing: segments[:last]}
	if last > 0 {
		t.Parent = segments[last-1]
	}
	root, err := rootMapping(doc)
	if root == nil || err != nil {
		return t, err
	}
	scope := root
	for i, name := range t.chain {
		key, body := mappingEntry(scope, name)
		if key == nil {
			return t, nil // the block is absent: the insert creates it and every block below it
		}
		switch {
		case body.Style&yaml.FlowStyle != 0:
			return t, fmt.Errorf("its %s: block is written in flow style ({...}); edit the file by hand", name)
		case body.Kind != yaml.MappingNode && !isNullNode(body):
			return t, fmt.Errorf("its %s: is not a block of settings; edit the file by hand", name)
		}
		t.found, t.missing = append(t.found, scalarBlock{key: key, body: body}), t.chain[i+1:]
		if i == last-1 {
			t.ParentKey, t.ParentBody, t.ParentNull = key, body, isNullNode(body)
		}
		if isNullNode(body) {
			return t, nil // `ui:` with nothing under it yet: no children to find
		}
		scope = body
	}
	t.KeyNode, t.ValueNode = mappingEntry(scope, t.Key)
	return t, nil
}

// valueFitsOneLine reports whether the target's EXISTING value is one this writer may rewrite in
// place: a value that begins and ends on the key's own line, so replacing that line leaves nothing of
// it behind. What counts as one differs by kind and by nothing else — a plain scalar for every kind
// but the list and the structured kind, for a list a FLOW sequence ([...]) that also opens and closes
// there, and for a structured key (one a gesture writes, Key.Gesture) a flow sequence or a flow map
// ({...}) that does.
//
// A block sequence is refused rather than folded into one line: the file is the user's, and turning
// four lines of theirs into one is a rewrite they did not ask for. It is the same refusal the flow-
// style mapping gets one level up, in the other direction.
func (t ScalarTarget) valueFitsOneLine(line int) error {
	if t.Kind == KindStructured {
		switch {
		case t.ValueNode.Kind != yaml.SequenceNode && t.ValueNode.Kind != yaml.MappingNode:
			return fmt.Errorf("its %s: holds a single value, not a list or a map; edit the file by hand", t.Key)
		case t.ValueNode.Style&yaml.FlowStyle == 0:
			return fmt.Errorf("its %s: value is written one entry per line; edit the file by hand", t.Key)
		case t.ValueNode.Line != line || maxNodeLine(t.ValueNode) != line:
			return fmt.Errorf("its %s: value does not sit on the same line as its key; edit the file by hand", t.Key)
		}
		return nil
	}
	if t.Kind == KindStringList {
		switch {
		case t.ValueNode.Kind != yaml.SequenceNode:
			return fmt.Errorf("its %s: holds a single value, not a list; edit the file by hand", t.Key)
		case t.ValueNode.Style&yaml.FlowStyle == 0:
			return fmt.Errorf("its %s: list is written one item per line; edit the file by hand", t.Key)
		case t.ValueNode.Line != line || maxNodeLine(t.ValueNode) != line:
			return fmt.Errorf("its %s: list does not sit on the same line as its key; edit the file by hand", t.Key)
		}
		return nil
	}
	switch {
	case t.ValueNode.Kind != yaml.ScalarNode:
		return fmt.Errorf("its %s: holds a list or a block, not a single value; edit the file by hand", t.Key)
	case t.ValueNode.Line != line:
		return fmt.Errorf("its %s: value does not sit on the same line as its key; edit the file by hand", t.Key)
	case t.ValueNode.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0:
		return fmt.Errorf("its %s: value is written as a multi-line block; edit the file by hand", t.Key)
	}
	return nil
}

// spliceTextBlock replaces a text key's whole block: the header line the key sits on, rebuilt from the
// user's own indentation and end-of-line note, and the indented lines of the new value under it. It is
// the one splice that spans more than a line, which is the whole of what a block scalar is.
//
// What stands above and below the block is carried over untouched, exactly as the single-line rewrite
// carries over the rest of the file: the paragraph of comments that documents the prompt, and whatever
// follows the last line of the old one.
func spliceTextBlock(lines []string, t ScalarTarget, text string) ([]byte, error) {
	head, tail, end, err := textLineParts(lines, t)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(lines)+lineCount(text))
	out = append(out, lines[:t.KeyNode.Line-1]...)
	out = append(out, head+" "+blockScalarHeader(text, listIndent)+tail)
	out = append(out, indentLines(t.KeyNode.Column-1, textBlockBody(text))...)
	out = append(out, lines[end:]...)
	return joinConfigLines(out), nil
}

// textLineParts is scalarLineParts for a block: the text through the key's colon, any end-of-line note
// on that line, and the LAST line the value occupies — the three things a replacement or a removal of a
// block needs. The gap a scalar keeps is not among them: a block scalar's header is `key: |`, one space
// and an indicator, so there is no alignment to preserve.
//
// The shapes it refuses are the ones where the text and the node tree would disagree about where the
// value ends. A block scalar ends where its indentation does (blockScalarEnd) and that is exact; a
// value written on the key's own line ends there, which is checked rather than assumed — a plain
// scalar may continue onto indented lines under it, and rewriting the first of them would leave the
// rest behind. A `key:` with nothing under it is the empty case and is simply written over.
func textLineParts(lines []string, t ScalarTarget) (head, tail string, end int, err error) {
	line := t.KeyNode.Line
	if line < 1 || line > len(lines) {
		return "", "", 0, fmt.Errorf("its %s: sits on line %d, which is outside the file", t.Key, line)
	}
	raw := lines[line-1]
	indent := t.KeyNode.Column - 1
	if indent < 0 || indent > len(raw) || !strings.HasPrefix(raw[indent:], t.Key+":") {
		return "", "", 0, fmt.Errorf("its %s: line reads unexpectedly at line %d; edit the file by hand", t.Key, line)
	}
	head = raw[:indent+len(t.Key)+1]
	// The note on the header line: a block scalar has value text of its own (the `|`), so the parser
	// hangs the comment off the value — the bare-key fallback is scalarLineParts' and is kept for the
	// same reason, an empty key holding no value node to carry it.
	comment := t.ValueNode.LineComment
	if comment == "" {
		comment = t.KeyNode.LineComment
	}
	if at := strings.LastIndex(raw, comment); comment != "" && at > len(head) {
		for at > 0 && (raw[at-1] == ' ' || raw[at-1] == '\t') {
			at--
		}
		tail = raw[at:]
	}
	switch {
	case isNullNode(t.ValueNode):
		return head, tail, line, nil // `key:` with nothing under it: the header line is the whole of it
	case t.ValueNode.Kind != yaml.ScalarNode:
		return "", "", 0, fmt.Errorf("its %s: holds a list or a block, not a text value; edit the file by hand", t.Key)
	case t.ValueNode.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0:
		return head, tail, blockScalarEnd(lines, line, indent), nil
	case t.ValueNode.Line != line || blockScalarEnd(lines, line, indent) != line:
		return "", "", 0, fmt.Errorf("its %s: value runs past the line its key sits on; edit the file by hand", t.Key)
	}
	return head, tail, line, nil
}

// blockScalarEnd is the last line of the block written under the key on keyLine: YAML's own rule, which
// is that the block runs for as long as the lines are indented deeper than the key. Blank lines are
// passed over rather than ended on — one inside a prompt is part of the prompt — but they do not extend
// the block either, so the blank line a user keeps between the prompt and the paragraph after it stays
// outside the value, which is exactly where a clip-chomped block leaves it.
func blockScalarEnd(lines []string, keyLine, indent int) int {
	end := keyLine
	for i := keyLine; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		if len(lines[i])-len(strings.TrimLeft(lines[i], " ")) <= indent {
			break
		}
		end = i + 1
	}
	return end
}

// textBlockBody is the value as the lines of a block scalar, indented relative to the key they hang
// under. An empty line is written EMPTY rather than as indentation alone: a literal block reads an
// empty line as an empty line whatever its indentation, and trailing whitespace nobody typed is not
// something a writer puts in a file it promised to leave as it found.
func textBlockBody(value string) []string {
	body := strings.Split(strings.TrimRight(value, "\n"), "\n")
	out := make([]string, 0, len(body))
	for _, line := range body {
		if line == "" {
			out = append(out, "")
			continue
		}
		out = append(out, indentLine(listIndent, line))
	}
	return out
}

// blockScalarHeader is the `|` a text value's key line ends with — with an explicit indentation
// indicator (`|2`) where the value's own first line opens with whitespace. A reader detects a block's
// indentation from its first non-empty line, so without the indicator that leading space would be read
// as indentation and silently disappear from the prompt. Every other value gets the plain `|` the
// template itself is written with.
//
// Chomping is left at its default: the body is written with exactly one trailing newline
// (renderSettingValue), which is what clip yields, so there is no indicator to spend on it.
func blockScalarHeader(value string, indent int) string {
	for _, line := range strings.Split(value, "\n") {
		if line == "" {
			continue // a truly empty leading line is no obstacle to the detection
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			return "|" + strconv.Itoa(indent)
		}
		break
	}
	return "|"
}

// lineCount counts the lines of a multi-line value: how much block the writer is about to replace,
// and — since the registry rows carry the display projection (Key.Read) — how a system prompt is
// summarized for a row, its text being far too long to show and its length being what the human
// recognizes it by.
func lineCount(text string) int {
	if text == "" {
		return 0
	}
	return len(strings.Split(strings.TrimSuffix(text, "\n"), "\n"))
}

// scalarInsertion is the lines to insert for a key the file does not set, and the 1-based line to
// insert them after — 0 for "append at the end", the fallback when the file documents the key
// nowhere. Every block of the key's chain the file lacks is created around it, each one
// indentation step under the last (nestedSetting). The four cases, in the order they are common:
//
//   - a top-level key: its own line, directly below its commented example (ADR 0035);
//   - a key whose outermost block is absent: the whole nest of blocks and the key under them,
//     below the commented example block that documents the outermost one — below the WHOLE
//     block, so the created setting sits under its documentation rather than wedged into the
//     middle of it;
//   - a key whose deepest present block is there and populated: the missing blocks and the key
//     joined to that block, after its last line and at the indentation its children use. A
//     commented example inside the block is NOT a candidate here: the key has to land inside the
//     mapping that is already open, and the example is only ever above or below it;
//   - a key whose deepest present block key is there with nothing under it: the first child of
//     that block, one indentation step under the block's own key.
func scalarInsertion(lines []string, t ScalarTarget, text string) ([]string, int, error) {
	setting := nestedSetting(t.missing, settingLines(t, text))
	if len(t.chain) == 0 {
		at, err := CommentedExampleLine(lines, t.Key)
		return setting, at, err
	}
	if len(t.found) == 0 {
		at, err := commentedExampleBlockEnd(lines, t.chain[0])
		return setting, at, err
	}
	deepest := t.found[len(t.found)-1]
	if isNullNode(deepest.body) {
		return indentLines(deepest.key.Column-1+listIndent, setting), deepest.key.Line, nil
	}
	return indentLines(deepest.body.Column-1, setting), maxNodeLine(deepest.body), nil
}

// nestedSetting wraps a rendered setting in the blocks the file lacks, outermost first: each block's
// own `name:` line, and everything under it one indentation step deeper — so `ui.side-panel.show`
// into a file with no `ui:` is `ui:`, `side-panel:` under it and `show:` under that. With no
// missing blocks it is the setting as rendered.
func nestedSetting(missing, setting []string) []string {
	for i := len(missing) - 1; i >= 0; i-- {
		setting = append([]string{missing[i] + ":"}, indentLines(listIndent, setting)...)
	}
	return setting
}

// settingLines is the setting as the file writes it, at the key's own column: one `key: value` line for
// every kind but text, and for text the block-scalar header plus the value's own lines under it. The
// caller indents the whole of it into place, which is what lets one insertion path serve both.
func settingLines(t ScalarTarget, text string) []string {
	if t.Kind != KindText {
		return []string{t.Key + ": " + text}
	}
	return append([]string{t.Key + ": " + blockScalarHeader(text, listIndent)}, textBlockBody(text)...)
}

// indentLines pads a whole rendered setting into its column, leaving empty lines empty — a blank line
// inside a block scalar is blank at every indentation, and padding it out would put whitespace nobody
// typed into the user's file.
func indentLines(indent int, lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			out = append(out, "")
			continue
		}
		out = append(out, indentLine(indent, line))
	}
	return out
}

// CommentedExampleLine finds the line of the key's commented example — the `# key: value` line the
// seeded template documents every key with — and reports 0 when the file has none, so the caller
// appends at the end instead.
//
// A file documenting one key in two places is refused rather than guessed at: "below the example"
// then names two places, and putting an active setting under a paragraph that describes something
// else is the kind of quiet damage this writer exists not to do.
func CommentedExampleLine(lines []string, key string) (int, error) {
	at := 0
	for i, line := range lines {
		indent, name, ok := commentedKey(line)
		if !ok || indent != 0 || name != key {
			continue
		}
		if at != 0 {
			return 0, fmt.Errorf(
				"it comments %s: out in two places (lines %d and %d), so there is no one place below its "+
					"example to put it; add the setting by hand", key, at, i+1)
		}
		at = i + 1
	}
	return at, nil
}

// commentedExampleBlockEnd finds the last line of the commented example block that documents a
// nested key's parent — its `# parent:` line plus the run of comment lines under it — and reports 0
// when the file documents no such block.
func commentedExampleBlockEnd(lines []string, parent string) (int, error) {
	end, err := CommentedExampleLine(lines, parent)
	if end == 0 || err != nil {
		return 0, err
	}
	for end < len(lines) && isCommentLine(lines[end]) {
		end++
	}
	return end, nil
}

// commentedKey reads a commented-out setting line: the `#`, at most one space of separation (the
// template's own style), then the indentation the key would have if the line were active, and the
// key itself. A prose comment is not one of these — its first colon comes after several words, and
// a key never contains a space.
//
// The `#` must start the line, as every example in the template does: a comment that is itself
// indented sits inside somebody's block, and an active setting spliced in below it would join that
// block instead of the top level the key belongs to.
func commentedKey(line string) (int, string, bool) {
	if !strings.HasPrefix(line, "#") {
		return 0, "", false
	}
	text := strings.TrimPrefix(line[1:], " ")
	indent := len(text) - len(strings.TrimLeft(text, " "))
	name, _, ok := strings.Cut(text[indent:], ":")
	if !ok || name == "" || strings.ContainsAny(name, " \t#") {
		return 0, "", false
	}
	return indent, name, true
}

// isCommentLine reports whether a line is a comment — what a commented example block is made of,
// and what a blank line ends.
func isCommentLine(line string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, " \t"), "#")
}

// deleteLines removes the named 1-based lines. A line number outside the file means the node tree
// and the text disagree, which is a refusal rather than something to skip over.
func deleteLines(lines []string, at ...int) ([]byte, error) {
	drop := make(map[int]bool, len(at))
	for _, n := range at {
		if n < 1 || n > len(lines) {
			return nil, fmt.Errorf("it names line %d, which is outside the file", n)
		}
		drop[n] = true
	}
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		if !drop[i+1] {
			out = append(out, line)
		}
	}
	return joinConfigLines(out), nil
}
