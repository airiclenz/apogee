package config

import (
	"errors"
	"fmt"
	"os"
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
// by its `name:`. The rest is the one write transaction (configedit.go), with two turns of it these
// edits take that no other writer does: the read does NOT seed, because an entry has to be in the
// file already for there to be anything to rewrite; and the gate asks one question more, that the
// rewritten list still passes ValidateServers — the whole point of the edit is to swap which key
// source the entry declares, and the exactly-one rule is what makes that swap legal.

// The key-source keys these edits write, and the `name:` they address an entry by — spelled as
// ServerEntry tags them, since the writer matches them in the node tree.
const (
	entryNameKey      = "name"
	apiKeyKey         = "api-key"
	apiKeyCmdKey      = "api-key-cmd"
	plaintextKeyOKKey = "plaintext-key-ok"
)

// keySourceNoun is what these two edits tell their gate they were placing. Both write a
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
	splice, verify := entryEdit(name, keySourceNoun, func(data []byte, entry ServerEntry) ([]byte, ServerEntry, error) {
		return setEntryKeyCommand(data, entry, cmd)
	})
	return editFrom(path, readConfigForEntryEdit, splice, verify)
}

// SaveServerPlaintextKeyOK records `plaintext-key-ok: true` on the `servers:` entry named name — the
// "never for this entry" answer to the migration offer, which is a per-entry acknowledgement that
// this key stays in the file (ADR 0035's deliberate-edit grain, one entry at a time).
//
// The marker is appended to the entry's block, or its existing line is rewritten when the entry
// already spells the key as false. An entry that already says true is left alone.
func SaveServerPlaintextKeyOK(path, name string) error {
	splice, verify := entryEdit(name, keySourceNoun, setEntryPlaintextKeyOK)
	return editFrom(path, readConfigForEntryEdit, splice, verify)
}

// entryApply is the half of an entry edit that is the writer's own: given the config bytes and the
// entry as the file carries it, it returns the edited bytes together with the entry the result must
// hold in its place — or nil bytes when the file already says it, which is a confirmation rather
// than a rewrite.
type entryApply func(data []byte, entry ServerEntry) ([]byte, ServerEntry, error)

// entryEdit states one edit of the `servers:` entry named name as the write transaction's two halves
// (configedit.go): the splice locates the entry, hands it to apply and takes back the bytes plus the
// entry the result must read as, and the gate holds the re-parsed file against exactly that.
//
// The located index and the wanted entry travel from the one half to the other in the closure,
// because the gate's question is about what THIS splice did — and the transaction asks it only when
// the splice produced bytes, so there is always an answer to give.
//
// what names the thing the edit was placing, in the caller's own words ("the key source", "the
// model"): the shape serves every entry writer, and only the caller knows which line a refusal has
// to name.
func entryEdit(name, what string, apply entryApply) (editSplice, editVerify) {
	var at int
	var want ServerEntry
	splice := func(before fileConfig, data []byte) ([]byte, error) {
		found, err := serverEntryAt(before, name)
		if err != nil {
			// A file that is not settings at all parses into the ZERO config, whose list answers
			// "which entry, then?" with "no servers at all" — true of the value, misleading about
			// the file. The decoder's own error is the honest one there, so the locate step asks
			// for it before refusing (configedit.go holds it back for exactly this choice).
			if parseErr := yaml.Unmarshal(data, new(fileConfig)); parseErr != nil {
				return nil, parseErr
			}
			return nil, err
		}
		updated, entry, err := apply(data, before.Servers[found])
		if err != nil || updated == nil {
			return nil, err
		}
		at, want = found, entry
		return updated, nil
	}
	verify := func(before, after fileConfig, _ []byte) error {
		return verifyEntryEdit(before, after, at, want, what)
	}
	return splice, verify
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

// setEntryKeyCommand returns the config bytes with entry's `api-key:` line replaced by an
// `api-key-cmd:` line — and the entry that leaves behind — or nil bytes when it already reads that
// way. The command's text comes from the YAML marshaller, which owns the quoting — so a command
// carrying a `#`, a colon or a leading quote lands as a value a reader takes back out unchanged
// rather than as a syntax break.
func setEntryKeyCommand(data []byte, entry ServerEntry, command string) ([]byte, ServerEntry, error) {
	if entry.APIKey == "" && entry.APIKeyCmd == command {
		return nil, ServerEntry{}, nil // already pointed at this command: a confirmation, not a rewrite
	}
	text, err := renderScalar(command)
	if err != nil {
		return nil, ServerEntry{}, err
	}
	updated, err := spliceEntryKeyCommand(data, entry.Name, text)
	if err != nil {
		return nil, ServerEntry{}, err
	}
	entry.APIKey, entry.APIKeyCmd = "", command
	return updated, entry, nil
}

// setEntryPlaintextKeyOK returns the config bytes with entry marked `plaintext-key-ok: true`, or nil
// bytes when it already is.
func setEntryPlaintextKeyOK(data []byte, entry ServerEntry) ([]byte, ServerEntry, error) {
	if entry.PlaintextKeyOK {
		return nil, ServerEntry{}, nil
	}
	updated, err := spliceEntryPlaintextKeyOK(data, entry.Name)
	if err != nil {
		return nil, ServerEntry{}, err
	}
	entry.PlaintextKeyOK = true
	return updated, entry, nil
}

// serverEntryAt reports where the entry named name stands in the config as a reader parses it — the
// locate step every entry edit begins with — and the refusal for a name the list does not carry.
func serverEntryAt(before fileConfig, name string) (int, error) {
	at := slices.IndexFunc(before.Servers, func(s ServerEntry) bool { return s.Name == name })
	if at < 0 {
		return 0, fmt.Errorf(
			"it has no servers: entry named %q — it configures %s; edit the file by hand",
			name, configuredEntryNames(before.Servers))
	}
	return at, nil
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

// verifyEntryEdit is the gate an entry splice passes before it reaches the disk: the result must
// hold before's `servers:` list with the entry at `at` changed to exactly want and every other entry
// untouched, must agree with before on every setting outside the list, and must still LOAD — the
// exactly-one key-source rule is what an edit that swaps sources has to leave satisfied, and asking
// ValidateServers is how this writer knows it did. Every comparison is between PARSED states — the
// config the edit started from and the re-parse the transaction hands over — so the file's bytes
// stay the splice's business and never reach the gate.
//
// what names the thing the edit was supposed to place, in the caller's own words ("the key source",
// "the model"), because the gate serves every entry writer: the refusal has to say what did not land
// where the reader would look, and only the caller knows which line that was.
func verifyEntryEdit(before, after fileConfig, at int, want ServerEntry, what string) error {
	switch {
	case !serversChangedOnlyAt(before.Servers, after.Servers, at, want):
		return fmt.Errorf(
			"the edit did not put %s on the %q entry where a reader would look for it; "+
				"edit the file by hand", what, want.Name)
	case !sameApartFrom(before, after, serversKey):
		return errors.New("the edit would have changed more than the servers: list; edit the file by hand")
	}
	if err := ValidateServers(after.Servers); err != nil {
		return fmt.Errorf("the edited file would not load: %w", err)
	}
	return nil
}
