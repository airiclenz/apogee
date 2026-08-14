package config

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ----------------------------------------------------------------------------
// The key-source writer (the startup migration's half)
// ----------------------------------------------------------------------------
//
// Key migration (ADR 0047) offers, once and with consent, to move a plaintext `api-key:` out of the
// config file and into the machine's own secret store — after which the entry has to say where its
// key now comes from. That is one line of the user's file, inside one entry of the `servers:` list,
// and it is written under the same contract configwrite.go's acknowledgement writer and
// configwrite_scalar.go's scalar setting writer are: the file is the user's, so every comment, every
// key order and every other entry comes back byte-identical.
//
// What is new here is the ADDRESSING, one level deeper than the scalar writer reaches: a settings
// path names a key in a block, while these edits name a key in a LIST ITEM, picked out of the list
// by its `name:`. The rest is the established shape — parse for positions, splice text, re-parse and
// compare before writing anything — with one check the others do not make: the rewritten list must
// still pass ValidateServers, because the whole point of the edit is to swap which key source the
// entry declares, and the exactly-one rule is what makes that swap legal.

// The key-source keys these edits write, and the `name:` they address an entry by — spelled as
// ServerEntry tags them, since the writer matches them in the node tree.
const (
	entryNameKey      = "name"
	apiKeyKey         = "api-key"
	apiKeyCmdKey      = "api-key-cmd"
	plaintextKeyOKKey = "plaintext-key-ok"
)

// keySourceNoun is what these two edits tell verifiedEntrySplice they were placing. Both write a
// DECLARATION of where the key comes from rather than one named key — an `api-key:` swapped for an
// `api-key-cmd:`, or the acknowledgement that the literal stays — so the refusal names the thing
// rather than the line.
const keySourceNoun = "the key source"

// SaveServerKeyCommand points the `servers:` entry named name at command as its key source: the
// entry's literal `api-key:` line is replaced, in place, by the `api-key-cmd:` line that runs
// command for the key. It is what a consented key migration persists once the secret is in the
// store and has been read back out of it again (ADR 0047).
//
// An entry already pointed at exactly this command, and carrying no literal beside it, is left
// alone: the write is a confirmation, not a rewrite, so a re-offer cannot churn the file. Anything
// the edit cannot do surgically — an unknown entry, a list written in flow style, an `api-key:`
// value spanning more than its own line — is refused with the writer's "by hand" idiom rather than
// guessed at, and the file is left exactly as it was.
func SaveServerKeyCommand(path, name, command string) error {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return errors.New("apogee: cannot point a server entry at a key command: the command is empty")
	}
	return saveEntryEdit(path, name, func(data []byte, name string) ([]byte, error) {
		return setEntryKeyCommand(data, name, cmd)
	})
}

// SaveServerPlaintextKeyOK records `plaintext-key-ok: true` on the `servers:` entry named name — the
// "never for this entry" answer to the migration offer, which is a per-entry acknowledgement that
// this key stays in the file (ADR 0035's deliberate-edit grain, one entry at a time).
//
// The marker is appended to the entry's block, or its existing line is rewritten when the entry
// already spells the key as false. An entry that already says true is left alone.
func SaveServerPlaintextKeyOK(path, name string) error {
	return saveEntryEdit(path, name, setEntryPlaintextKeyOK)
}

// saveEntryEdit is the shape both key-source edits share: read the file, splice, and write it back
// atomically unless the splice reports there was nothing to change. The splice failure is qualified
// with the config's path, the way every other write here qualifies one — a refusal about a file's
// SHAPE has to say which file.
func saveEntryEdit(path, name string, splice func(data []byte, name string) ([]byte, error)) error {
	data, err := readConfigForEntryEdit(path)
	if err != nil {
		return err
	}
	updated, err := splice(data, name)
	if err != nil {
		return fmt.Errorf("apogee: update config %q: %w", path, err)
	}
	if updated == nil {
		return nil
	}
	return writeConfigAtomically(path, updated)
}

// readConfigForEntryEdit reads the config an entry edit rewrites. It deliberately does NOT seed an
// absent file from the template the way ReadConfigForWrite does: these edits address an entry the
// file must already carry, so a config that is not there has no entry to rewrite, and seeding one
// would answer "which server?" with a template that never named it.
func readConfigForEntryEdit(path string) ([]byte, error) {
	if path == "" {
		return nil, errors.New("apogee: cannot rewrite a server entry: no config file path is known")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("apogee: read config %q: %w", path, err)
	}
	return data, nil
}

// setEntryKeyCommand returns the config bytes with the named entry's `api-key:` line replaced by an
// `api-key-cmd:` line, or nil bytes when the entry already reads that way. The command's text comes
// from the YAML marshaller, which owns the quoting — so a command carrying a `#`, a colon or a
// leading quote lands as a value a reader takes back out unchanged rather than as a syntax break.
func setEntryKeyCommand(data []byte, name, command string) ([]byte, error) {
	before, at, err := serverEntryAt(data, name)
	if err != nil {
		return nil, err
	}
	if before.Servers[at].APIKey == "" && before.Servers[at].APIKeyCmd == command {
		return nil, nil // already pointed at this command: a confirmation, not a rewrite
	}
	text, err := renderScalar(command)
	if err != nil {
		return nil, err
	}
	updated, err := spliceEntryKeyCommand(data, name, text)
	if err != nil {
		return nil, err
	}
	want := before.Servers[at]
	want.APIKey, want.APIKeyCmd = "", command
	return verifiedEntrySplice(updated, before, at, want, keySourceNoun)
}

// setEntryPlaintextKeyOK returns the config bytes with the named entry marked `plaintext-key-ok:
// true`, or nil bytes when it already is.
func setEntryPlaintextKeyOK(data []byte, name string) ([]byte, error) {
	before, at, err := serverEntryAt(data, name)
	if err != nil {
		return nil, err
	}
	if before.Servers[at].PlaintextKeyOK {
		return nil, nil
	}
	updated, err := spliceEntryPlaintextKeyOK(data, name)
	if err != nil {
		return nil, err
	}
	want := before.Servers[at]
	want.PlaintextKeyOK = true
	return verifiedEntrySplice(updated, before, at, want, keySourceNoun)
}

// serverEntryAt parses the config the way apogee reads it and reports the whole parsed file plus the
// index of the entry named name — the sole before-state every verifiedEntrySplice call compares
// against, and the refusal for a name the list does not carry.
func serverEntryAt(data []byte, name string) (fileConfig, int, error) {
	var before fileConfig
	if err := yaml.Unmarshal(data, &before); err != nil {
		return fileConfig{}, 0, err
	}
	at := slices.IndexFunc(before.Servers, func(s ServerEntry) bool { return s.Name == name })
	if at < 0 {
		return fileConfig{}, 0, fmt.Errorf(
			"it has no servers: entry named %q — it configures %s; edit the file by hand",
			name, configuredEntryNames(before.Servers))
	}
	return before, at, nil
}

// configuredEntryNames spells what the `servers:` list DOES carry, for a refusal about a name it does
// not: the message answers "which entry, then?" without sending the reader back to the file. A list
// with nothing in it says so — a config whose `servers:` block is still commented out (the seeded
// template's own state) has no entry for any edit to land on, which is a different thing from a
// misspelled name.
func configuredEntryNames(servers []ServerEntry) string {
	if len(servers) == 0 {
		return "no servers at all"
	}
	names := make([]string, len(servers))
	for i, s := range servers {
		names[i] = strconv.Quote(s.Name)
	}
	return joinAnd(names)
}

// spliceEntryKeyCommand rewrites the entry's `api-key:` line as an `api-key-cmd:` line, keeping the
// user's own indentation, the gap they aligned the value with, and any end-of-line note — the line
// is theirs and only its key and its value are the edit's business.
//
// scalarLineParts does the reading, exactly as it does for a top-level setting, so the shapes it
// refuses are refused here too: a value that runs past its key's line, a block scalar, a text and a
// node tree that disagree about where the key sits. Each one would leave part of the old key behind.
func spliceEntryKeyCommand(data []byte, name, text string) ([]byte, error) {
	lines, entry, err := serverEntryNode(data, name)
	if err != nil {
		return nil, err
	}
	keyNode, valueNode := mappingEntry(entry, apiKeyKey)
	if keyNode == nil {
		return nil, fmt.Errorf(
			"its %q entry has no api-key: line to replace; add the key source by hand", name)
	}
	t := ScalarTarget{Key: apiKeyKey, Kind: KindString, KeyNode: keyNode, ValueNode: valueNode}
	head, gap, tail, err := scalarLineParts(lines, t)
	if err != nil {
		return nil, err
	}
	out := slices.Clone(lines)
	out[keyNode.Line-1] = strings.TrimSuffix(head, apiKeyKey+":") + apiKeyCmdKey + ":" + gap + text + tail
	return joinConfigLines(out), nil
}

// spliceEntryPlaintextKeyOK writes the marker into the entry's block: over the line the entry
// already spells it on, or — the ordinary case — as a new last child of the block, at the
// indentation its siblings use.
//
// The insertion point is the last line the entry's own subtree reaches, which is where the next
// sibling key would go. A block value whose text the node tree cannot measure would put that line
// somewhere inside the entry instead; nothing here tries to detect that, because the verification
// below reads the result back and refuses any edit that changed a value nobody asked it to touch.
func spliceEntryPlaintextKeyOK(data []byte, name string) ([]byte, error) {
	lines, entry, err := serverEntryNode(data, name)
	if err != nil {
		return nil, err
	}
	if keyNode, valueNode := mappingEntry(entry, plaintextKeyOKKey); keyNode != nil {
		t := ScalarTarget{Key: plaintextKeyOKKey, Kind: KindBool, KeyNode: keyNode, ValueNode: valueNode}
		head, gap, tail, err := scalarLineParts(lines, t)
		if err != nil {
			return nil, err
		}
		out := slices.Clone(lines)
		out[keyNode.Line-1] = head + gap + "true" + tail
		return joinConfigLines(out), nil
	}
	marker := []string{indentLine(entry.Column-1, plaintextKeyOKKey+": true")}
	return insertAt(lines, marker, maxNodeLine(entry), fmt.Sprintf("its %q entry", name))
}

// serverEntryNode finds the block mapping of the `servers:` entry named name in the parsed document,
// and returns it with the file's lines — the two views a splice works between.
//
// The shapes it refuses are the ones where those two views would disagree about which line to edit:
// a file with no `servers:` list, a list written in flow style ([...]) or holding something other
// than a list, and an entry written as a flow mapping ({...}), which has no line of its own for a
// key. A name the node tree cannot find although the parsed config carries it is the same class of
// disagreement, and refuses for the same reason.
func serverEntryNode(data []byte, name string) ([]string, *yaml.Node, error) {
	doc, err := Document(data)
	if err != nil {
		return nil, nil, err
	}
	root, err := rootMapping(doc)
	if err != nil {
		return nil, nil, err
	}
	keyNode, list := mappingEntry(root, serversKey)
	switch {
	case keyNode == nil:
		return nil, nil, errors.New("it has no servers: list; edit the file by hand")
	case list.Style&yaml.FlowStyle != 0:
		return nil, nil, errors.New("its servers: list is written in flow style ([...]); edit the file by hand")
	case list.Kind != yaml.SequenceNode:
		return nil, nil, errors.New("its servers: holds something other than a list of servers; edit the file by hand")
	}
	for _, item := range list.Content {
		if _, value := mappingEntry(item, entryNameKey); value == nil || value.Value != name {
			continue
		}
		if item.Style&yaml.FlowStyle != 0 {
			return nil, nil, fmt.Errorf(
				"its %q entry is written in flow style ({...}); edit the file by hand", name)
		}
		return SplitConfigLines(data), item, nil
	}
	return nil, nil, fmt.Errorf("its servers: list has no entry block named %q; edit the file by hand", name)
}

// verifiedEntrySplice is the gate an entry splice passes before it reaches the disk: the result must
// parse, must hold before's `servers:` list with the entry at `at` changed to exactly want and every
// other entry untouched, must agree with before on every setting outside the list, and must still
// LOAD — the exactly-one key-source rule is what an edit that swaps sources has to leave satisfied,
// and asking ValidateServers is how this writer knows it did. Every comparison is between PARSED
// states — the caller's before and the re-parse of updated — so the config's original bytes stay
// the caller's business and never reach the gate.
//
// what names the thing the edit was supposed to place, in the caller's own words ("the key source",
// "the model"), because the gate serves every entry writer: the refusal has to say what did not land
// where the reader would look, and only the caller knows which line that was.
func verifiedEntrySplice(updated []byte, before fileConfig, at int, want ServerEntry, what string) ([]byte, error) {
	var after fileConfig
	if err := yaml.Unmarshal(updated, &after); err != nil {
		return nil, fmt.Errorf("the edited file would not parse: %w", err)
	}
	switch {
	case !serversChangedOnlyAt(before.Servers, after.Servers, at, want):
		return nil, fmt.Errorf(
			"the edit did not put %s on the %q entry where a reader would look for it; "+
				"edit the file by hand", what, want.Name)
	case !sameApartFrom(before, after, serversKey):
		return nil, errors.New("the edit would have changed more than the servers: list; edit the file by hand")
	}
	if err := ValidateServers(after.Servers); err != nil {
		return nil, fmt.Errorf("the edited file would not load: %w", err)
	}
	return updated, nil
}

// serversChangedOnlyAt reports whether after is before with the entry at index at replaced by want
// and nothing else moved — the shape a key-source splice must produce (serversAppended's rule, for
// an edit in place rather than an append). Entries are compared with reflect.DeepEqual because a
// ServerEntry holding a `mechanisms:` map cannot be `==`d.
func serversChangedOnlyAt(before, after []ServerEntry, at int, want ServerEntry) bool {
	if len(after) != len(before) || at < 0 || at >= len(before) {
		return false
	}
	for i := range before {
		expected := before[i]
		if i == at {
			expected = want
		}
		if !reflect.DeepEqual(after[i], expected) {
			return false
		}
	}
	return true
}
