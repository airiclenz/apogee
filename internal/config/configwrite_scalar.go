package config

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ----------------------------------------------------------------------------
// The scalar setting writer (the /settings screen's half)
// ----------------------------------------------------------------------------
//
// The settings surface persists one key per deliberate edit (ADR 0035) into the same file the
// acknowledgement writer (configwrite.go) edits, under the same contract and for the same
// reason: the user owns this file, hand-edits it and reads it back months later, so an edit
// made on their behalf must leave every comment, key order and formatting choice exactly as
// found. The machinery generalises almost whole — parse for positions, splice text, re-parse
// and compare before writing anything — and what is new is the ADDRESSING: a scalar edit names
// its target by registry path (`ui.spinner`), so the registry's row decides what may be
// written, in what shape, and the file's own text decides which line it lands on.
//
// Placement follows ADR 0035's insert-below-example call. The seeded template documents every
// key as a commented example, so a key the user has never set lands directly under the
// paragraph explaining it, where they will find it next time they open the file — not at the
// bottom, where it reads like a footnote to someone else's document. Append-at-end is the
// fallback for a file whose example is gone.
//
// Reset is the same splice in reverse: the active line is REMOVED rather than set to a default
// literal, so the key goes back to being described by the binary's default (and to being
// documented by its commented example) instead of being pinned to today's spelling of it.

// scalarPathDepth is how deep a scalar path may reach: a top-level key, or one key inside one
// block (`ui.spinner`). Every registry row is within it, and a deeper path is refused rather
// than guessed at — the two-line block this writer creates for an absent parent cannot express
// a third level, and inventing one silently is how a config write corrupts a file.
const scalarPathDepth = 2

// SaveConfigSetting writes value as the config file's setting for the registry path key, and
// reports nothing when the file already says exactly that (a re-set is a confirmation, not a
// rewrite). An absent config is seeded from the embedded template first, so an edit never leaves
// a bare fragment where a documented file belongs, and the write is atomic and mode-preserving —
// the acknowledgement writer's contract (configwrite.go), unchanged.
func SaveConfigSetting(path, key, value string) error {
	k, err := writableKey(key)
	if err != nil {
		return err
	}
	if err := validateSettingValue(k, value); err != nil {
		return err
	}
	splice, verify, err := scalarSetEdit(k, value)
	if err != nil {
		return fmt.Errorf("apogee: update config %q: %w", path, err)
	}
	return edit(path, splice, verify)
}

// ResetConfigSetting removes the config file's active line for the registry path key, so the key
// resolves from its default again. A key the file does not set is already at its default: that is
// a no-op, not an error, and nothing is written.
func ResetConfigSetting(path, key string) error {
	k, err := writableKey(key)
	if err != nil {
		return err
	}
	splice, verify := scalarResetEdit(k)
	return edit(path, splice, verify)
}

// validateSettingValue refuses a value the key cannot hold, BEFORE the config file is opened: the
// kind's own check (renderSettingValue — a bool is true or false, an enum is one of its values) and
// then the key's validate hook, which is the check startup already makes for that key
// (Key.Validate). A value refused here has touched nothing at all — not even the seeding read
// (ReadConfigForWrite, configsplice.go) — so "invalid" and "written" can never be the same outcome.
//
// It runs HERE rather than inside the splice for the message's sake: SaveConfigSetting qualifies a
// splice failure with the config's path, which is what a file-shape refusal needs and what a bad
// VALUE does not — the settings pane renders this error inline on the row (internal/tui/settings.go),
// and a leading "update config /long/path/config.yaml:" would push the reason out of the cell. The
// kind check the splice makes again on its own way through is left where it is: a writer that
// trusted its caller would be one refactor away from splicing a value nothing checked.
func validateSettingValue(k Key, value string) error {
	_, want, err := renderSettingValue(k, value)
	if err != nil {
		return fmt.Errorf("apogee: %w", err)
	}
	if k.Validate == nil {
		return nil
	}
	// The hook is asked about the value as the FILE will spell it (trimmed, canonical), not the raw
	// keystrokes: it is the same text the next launch will read back, so a value this accepts is one
	// that run accepts too.
	return k.Validate(want)
}

// writableKey resolves a registry path to the row that describes it, and refuses a key no surface
// may write. Editability is the registry's call, single-homed there: this writer asks rather than
// keeping a second list of what is safe to touch.
func writableKey(key string) (Key, error) {
	k, ok := LookupKey(key)
	if !ok {
		return Key{}, fmt.Errorf("apogee: %q is not a setting apogee knows", key)
	}
	switch {
	case k.GlobalOnly && !k.Editable:
		return Key{}, fmt.Errorf(
			"apogee: %s is not written from the settings surface: it is the confinement acknowledgement, "+
				"which /confine makes deliberately", k.Path)
	case !k.Editable:
		return Key{}, fmt.Errorf("apogee: %s is not a simple value; edit it in config.yaml", k.Path)
	case len(strings.Split(k.Path, ".")) > scalarPathDepth:
		return Key{}, fmt.Errorf("apogee: %s is nested too deeply to write in place; edit it in config.yaml", k.Path)
	}
	return k, nil
}

// setScalarSetting returns the config bytes with key set to value, or nil bytes when the edit would
// not change one — a re-set of what the file already says is a confirmation, not a rewrite. It is
// the write transaction over bytes already in hand (verifiedEdit, configedit.go) rather than against
// a path, which is what the one-time legacy fold needs: the fold stacks this set under its own
// splice before anything reaches the disk (configmigrate.go).
func setScalarSetting(data []byte, k Key, value string) ([]byte, error) {
	splice, verify, err := scalarSetEdit(k, value)
	if err != nil {
		return nil, err
	}
	return verifiedEdit(data, splice, verify)
}

// scalarSetEdit states a set as the transaction's two halves (configedit.go): the splice that
// rewrites the key's active line — or inserts one where the key has none — and the gate the result
// must pass. The value is rendered once, up front, into the text the file will carry AND the value a
// reader will parse back out of it, so the two halves cannot disagree about what was written; a
// value the key's kind does not hold is refused here, with nothing spliced at all.
func scalarSetEdit(k Key, value string) (editSplice, editVerify, error) {
	text, want, err := renderSettingValue(k, value)
	if err != nil {
		return nil, nil, err
	}
	splice := func(_ fileConfig, data []byte) ([]byte, error) {
		updated, err := spliceScalarSet(data, k, text)
		if err != nil {
			return nil, err
		}
		if bytes.Equal(updated, data) {
			return nil, nil // the file already spells the line this way
		}
		return updated, nil
	}
	verify := func(before, after fileConfig, updated []byte) error {
		return verifyScalarEdit(before, after, updated, k, want, true)
	}
	return splice, verify, nil
}

// scalarResetEdit states a reset as the same two halves: the splice REMOVES the key's active line
// rather than setting it to a default literal, so the key goes back to being described by the
// binary's default, and the gate is the set's with the target's absence standing in for its value.
// A key the file does not set is already at its default: the splice reports nothing to do, and
// nothing is written.
func scalarResetEdit(k Key) (editSplice, editVerify) {
	splice := func(_ fileConfig, data []byte) ([]byte, error) {
		return spliceScalarDelete(data, k)
	}
	verify := func(before, after fileConfig, updated []byte) error {
		return verifyScalarEdit(before, after, updated, k, "", false)
	}
	return splice, verify
}

// renderSettingValue turns a value typed at a surface into the text the file will carry and the
// value a reader will parse back out of it, refusing anything the key's kind does not hold.
//
// The text comes from the YAML marshaller, which owns the quoting — so a value that would change
// meaning as a bare scalar (`off`, `true`, `123`, the empty string) comes back quoted and one that
// is unambiguous stays bare, which is how the template writes them too. Surrounding whitespace is
// trimmed: a plain scalar cannot carry it, so keeping it would only make the round-trip check
// below refuse an edit the user meant.
func renderSettingValue(k Key, value string) (string, string, error) {
	v := strings.TrimSpace(value)
	switch k.Kind {
	case KindBool:
		b, err := strconv.ParseBool(v)
		if err != nil {
			return "", "", fmt.Errorf("%s is true or false, not %q", k.Path, value)
		}
		text, err := renderScalar(b)
		return text, strconv.FormatBool(b), err
	case KindInt:
		n, err := strconv.Atoi(v)
		if err != nil {
			return "", "", fmt.Errorf("%s is a whole number, not %q", k.Path, value)
		}
		text, err := renderScalar(n)
		return text, strconv.Itoa(n), err
	case KindFloat:
		// The share goes back into the file in the SHORTEST spelling that reads back as the same
		// number ('g' with precision -1), so `0.2` typed into the pane stays `0.2` on disk rather
		// than becoming 0.20000000000000001 — the round-trip check below would refuse that edit.
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return "", "", fmt.Errorf("%s is a fractional number, not %q", k.Path, value)
		}
		canonical := strconv.FormatFloat(f, 'g', -1, 64)
		text, err := renderScalar(f)
		return text, canonical, err
	case KindEnum:
		if !slices.Contains(k.EnumValues, v) {
			return "", "", fmt.Errorf("%s is one of %s, not %q", k.Path, strings.Join(k.EnumValues, ", "), value)
		}
		text, err := renderScalar(v)
		return text, v, err
	case KindString, KindServer, KindScheme:
		// A server NAME is a plain scalar like any other string; which names are admissible is the
		// `servers:` block's question and is answered where that list is known — at selection, and by
		// the switch seam itself — not by a vocabulary this table could hold (KindServer). A colour
		// scheme's name is the same shape for the same reason (KindScheme): the admissible set is
		// the schemes folder's contents, and the loader answers an unresolvable one with a warning.
		text, err := renderScalar(v)
		return text, v, err
	case KindStringList:
		// The two spellings a surface may offer — the file's own `[a, b]` and the bare `a, b` a human
		// types over it — are one list (ParseSettingList), and it goes back into the file in the
		// canonical one, so a re-set of what is already there rewrites nothing.
		names := ParseSettingList(v)
		text, err := renderScalarList(names)
		return text, listValue(names), err
	case KindText:
		// The one kind whose file text is not a line but a BLOCK, and the block's indentation depends
		// on where in the file its key sits — which is the splice's knowledge and not this function's.
		// So what comes back here is the NORMALIZED VALUE for both halves, and spliceTextBlock renders
		// the block from it. Normalizing is trimming the trailing newlines down to the single one a
		// clip-chomped block scalar yields, which is what a reader takes back out of the file: without
		// it a value typed with two blank lines at the end would come back differing from itself and
		// the edit's own gate (verifyScalarEdit) would refuse it. Nothing else is trimmed — a prompt's leading indentation
		// and its trailing spaces are its own, and a literal block preserves both.
		text := strings.TrimRight(value, "\n")
		if text == "" {
			return "", "", fmt.Errorf("%s has no text to write; reset the key to send no prompt at all", k.Path)
		}
		return text + "\n", text + "\n", nil
	}
	return "", "", fmt.Errorf("%s is not a simple value; edit it in config.yaml", k.Path)
}

// ParseSettingList reads a list value as a surface offers it: the file's own one-line flow spelling
// (`[AGENTS.md, CLAUDE.md]`) or the bare comma-separated text a human types, which are the same list
// with and without its brackets — the edit field is SEEDED with the row's value, so a human correcting
// one name hands the value back still wearing them. Entries are trimmed and empty ones dropped, so a
// trailing comma and the space after it cost nothing.
//
// It is the one parse: the writer renders the file's text from it and the live-apply dispatcher hands
// the engine the list it returns (wire.go), so what the file carries and what the session runs cannot
// be two different readings of the same keystrokes.
func ParseSettingList(value string) []string {
	v := strings.TrimSpace(value)
	if inner, ok := strings.CutPrefix(v, "["); ok {
		v = strings.TrimSuffix(inner, "]")
	}
	parts := strings.Split(v, ",")
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		if name := strings.TrimSpace(part); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// renderScalarList renders a name list as the one-line flow sequence the file carries
// (`[AGENTS.md, CLAUDE.md]`), through the YAML marshaller — which owns the quoting, so a name that
// would mean something else bare (`off`, `12`, one carrying a comma or a colon) comes back quoted and
// the file still parses. An empty list renders as `[]`, the documented second spelling of "off".
//
// A list that will not fit on one line is refused for the reason a scalar that will not is: this
// writer replaces the single line its key sits on, and the marshaller folds a long flow sequence
// across several.
func renderScalarList(names []string) (string, error) {
	node := yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	for _, name := range names {
		item := &yaml.Node{}
		if err := item.Encode(name); err != nil {
			return "", fmt.Errorf("render the value: %w", err)
		}
		node.Content = append(node.Content, item)
	}
	out, err := yaml.Marshal(&node)
	if err != nil {
		return "", fmt.Errorf("render the value: %w", err)
	}
	text := strings.TrimRight(string(out), "\n")
	if strings.Contains(text, "\n") {
		return "", errors.New("the list does not fit on one line")
	}
	return text, nil
}

// renderScalar renders one value as the YAML text for it, quoted by the marshaller where a bare
// scalar would mean something else. A value that needs more than one line is refused: a scalar
// setting occupies exactly the line this writer splices.
func renderScalar(v any) (string, error) {
	out, err := yaml.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("render the value: %w", err)
	}
	text := strings.TrimRight(string(out), "\n")
	if strings.Contains(text, "\n") {
		return "", errors.New("the value does not fit on one line")
	}
	return text, nil
}

// listValue spells a name list the way the template's inline form spells it ("[AGENTS.md]") — the
// CANONICAL spelling a written list is verified against, so the value a reader takes back out of
// the file is the same string that was typed.
//
// It is the spelling the registry rows show a name list in as well (Key.Read): the row's text and
// the value a reader takes back out of the file are one string, or an edit would come back reading
// differently from what was typed.
func listValue(names []string) string { return "[" + strings.Join(names, ", ") + "]" }

// spliceScalarSet rewrites the key's active line, or inserts one where the key has none. A text key
// is the one that occupies more than a line: its block replaces the block already there
// (spliceTextBlock), and an insert puts the whole block where a scalar's single line would have gone.
func spliceScalarSet(data []byte, k Key, text string) ([]byte, error) {
	doc, err := Document(data)
	if err != nil {
		return nil, err
	}
	lines := SplitConfigLines(data)
	t, err := ScalarTargetIn(doc, k)
	if err != nil {
		return nil, err
	}
	if t.IsSet() && t.Kind == KindText {
		return spliceTextBlock(lines, t, text)
	}
	if t.IsSet() {
		head, gap, tail, err := scalarLineParts(lines, t)
		if err != nil {
			return nil, err
		}
		out := slices.Clone(lines)
		out[t.KeyNode.Line-1] = head + gap + text + tail
		return joinConfigLines(out), nil
	}
	block, at, err := scalarInsertion(lines, t, text)
	if err != nil {
		return nil, err
	}
	if at == 0 {
		return joinConfigLines(appendBlock(lines, block)), nil
	}
	return insertAt(lines, block, at, fmt.Sprintf("the place for %s:", t.Key))
}

// spliceScalarDelete removes the key's active line, and the block line above it when that line was
// the block's last active child — a block key left with nothing under it parses as a null value
// rather than as an absent block, which is a different config from the one the reset asked for.
// A key the file does not set returns nil bytes: there is nothing to remove.
func spliceScalarDelete(data []byte, k Key) ([]byte, error) {
	doc, err := Document(data)
	if err != nil {
		return nil, err
	}
	lines := SplitConfigLines(data)
	t, err := ScalarTargetIn(doc, k)
	if err != nil {
		return nil, err
	}
	if !t.IsSet() {
		return nil, nil
	}
	last := t.KeyNode.Line
	if t.Kind == KindText {
		// A text key's value is a BLOCK, so what the reset removes is every line of it — the header
		// line the key sits on and the indented lines under it (textLineParts). Removing only the key's
		// own line would leave the prompt's text behind as a fragment the next parse would choke on.
		_, _, end, err := textLineParts(lines, t)
		if err != nil {
			return nil, err
		}
		last = end
	} else if _, _, _, err := scalarLineParts(lines, t); err != nil {
		return nil, err
	}
	drop := make([]int, 0, last-t.KeyNode.Line+2)
	for n := t.KeyNode.Line; n <= last; n++ {
		drop = append(drop, n)
	}
	if t.ParentKey != nil && len(t.ParentBody.Content) == 2 {
		drop = append(drop, t.ParentKey.Line)
	}
	return deleteLines(lines, drop...)
}

// verifyScalarEdit is the gate every scalar splice passes before it reaches the disk: the result
// must resolve the target path to exactly what the edit intended (or to nothing, for a reset), and
// must agree with the config it started from on every other setting — so a file shape the line
// arithmetic mis-reads surfaces as a refusal rather than as a quietly mangled config.
func verifyScalarEdit(before, after fileConfig, updated []byte, k Key, want string, set bool) error {
	placed, err := scalarChangedOnlyAt(updated, k.Path, want, set)
	switch {
	case err != nil:
		return fmt.Errorf("the edited file would not parse: %w", err)
	case !placed && set:
		return fmt.Errorf("the edit did not put %s where a reader would look for it; edit the file by hand", k.Path)
	case !placed:
		return fmt.Errorf("the edit left %s set; edit the file by hand", k.Path)
	case !sameApartFrom(before, after, k.Path):
		return fmt.Errorf("the edit would have changed more than %s; edit the file by hand", k.Path)
	}
	return nil
}
