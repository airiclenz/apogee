package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/platform"
	"gopkg.in/yaml.v3"
)

// ----------------------------------------------------------------------------
// The host-acknowledgement config writer (the `/confine off --save` half)
// ----------------------------------------------------------------------------
//
// `/confine off --save` persists one line of user intent — "this machine is disposable" (ADR
// 0012, amendment 2026-07-21) — into a file the user owns, hand-edits, and reads back months
// later. TODO constraint 4 requires that write to be visible and reversible, which rules out the
// obvious implementation: unmarshal into fileConfig, append an entry, re-marshal. yaml.v3 hangs
// comments off nodes, and the seeded template (internal/config/defaults/config.yaml) is comments apart
// from its one active key — so a re-marshal would hand the user back a file with a setting or two
// in it, having silently deleted every word of documentation they started with.
//
// So the edit is textual, guided by the parsed node positions: the entry is rendered by the YAML
// marshaller (which owns quoting and escaping) and spliced into the existing bytes, leaving every
// other byte — comments, key order, indentation, the user's own edits — exactly as found. The
// result is re-parsed and compared against the original before anything is written, so a file
// shape the line arithmetic mis-reads fails loudly instead of corrupting a config.

// unconfinedHostsKey is the top-level config key the acknowledgement list lives under — the same
// spelling as fileConfig's yaml tag, named here because the writer matches it in the node tree.
const unconfinedHostsKey = "unconfined-hosts"

// acknowledgedDateLayout is the `acknowledged:` field's format: a plain calendar date, since the
// field exists for the human reading the file back, and nothing resolves off it.
const acknowledgedDateLayout = "2006-01-02"

// listIndent is the column the writer indents a list item to when it creates the block itself
// (an existing list is matched to its own indentation instead).
const listIndent = 2

// hostAcknowledgementNote is the `note:` written into a new entry: what put the line there and how
// to take it back out, since the entry outlives the session that wrote it.
const hostAcknowledgementNote = "added by /confine off --save; delete to confine this machine again"

// hostAcknowledgementHeader is the comment written above a freshly created `unconfined-hosts:`
// block, so a user who meets the key for the first time in their own file learns what it does
// without going looking. An existing list gets no injected commentary — it is the user's.
const hostAcknowledgementHeader = "# Machines acknowledged as disposable: on a host listed here, auto mode runs UNCONFINED\n" +
	"# — nothing is fenced and nothing asks. Added by /confine off --save; delete an entry to\n" +
	"# confine that machine again."

// saveHostAcknowledgement records hostID in the `unconfined-hosts:` list of the config file at
// path, and reports the file written and the entry that now names this machine (so the
// confirmation can say what changed and how to undo it).
//
// A host with no identity to record is refused rather than written: on a machine that supplies
// neither a hostname nor a machine id, platform.HostID() is the same value on every such machine,
// so the entry would acknowledge a class of hosts instead of this one (the resolution refuses to
// match it for the same reason). The session toggle is unaffected — it never reaches disk.
//
// It is idempotent: a hostID the list already names returns that existing entry and writes
// nothing, so a repeated `--save` cannot accumulate duplicates. An absent config is seeded from
// the embedded template first, so `--save` never leaves a bare fragment where a documented file
// belongs. The write is atomic (temp + rename in the same directory) and preserves the file's
// mode — a config may hold endpoint details, so a rewrite must not widen its permissions.
func saveHostAcknowledgement(path, hostID string, now time.Time) (string, UnconfinedHost, error) {
	id := strings.TrimSpace(hostID)
	if id == "" {
		return "", UnconfinedHost{}, errors.New(
			"apogee: cannot save the host acknowledgement: this host has no id to record")
	}
	if platform.IsUnidentifiedHostID(id) {
		return "", UnconfinedHost{}, errors.New(
			"apogee: cannot save the host acknowledgement: this machine reports neither a hostname nor a " +
				"machine id, so the recorded id would name every such machine rather than this one — " +
				"/confine off still applies to this session")
	}
	if path == "" {
		return "", UnconfinedHost{}, errors.New(
			"apogee: cannot save the host acknowledgement: no config file path is known")
	}
	if _, err := seedConfig(path, defaultConfigYAML); err != nil {
		return "", UnconfinedHost{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", UnconfinedHost{}, fmt.Errorf("apogee: read config %q: %w", path, err)
	}

	entry := UnconfinedHost{ID: id, Acknowledged: now.Format(acknowledgedDateLayout), Note: hostAcknowledgementNote}
	updated, recorded, err := insertHostAcknowledgement(data, entry)
	if err != nil {
		return "", UnconfinedHost{}, fmt.Errorf("apogee: update config %q: %w", path, err)
	}
	if updated == nil { // already acknowledged: the save is a confirmation, not a second entry
		return path, recorded, nil
	}
	if err := writeConfigAtomically(path, updated); err != nil {
		return "", UnconfinedHost{}, err
	}
	return path, recorded, nil
}

// HostAcknowledgementSaver adapts the writer to the TUI's Options.SaveHostAcknowledgement seam.
// The renderer learns only which file now records this host — it already knows the id from
// Options.Confinement, and the on-disk format stays the binary's business (the Options.Sessions
// precedent).
func HostAcknowledgementSaver(path, hostID string) func() (string, error) {
	return func() (string, error) {
		written, _, err := saveHostAcknowledgement(path, hostID, time.Now())
		return written, err
	}
}

// insertHostAcknowledgement splices entry into the config bytes and returns the new file content.
// A config whose list already names entry.ID is left alone: the returned content is nil (nothing
// to write) and the reported entry is the one already on disk, which is what makes a repeated
// `--save` idempotent.
//
// The splice is verified before it is returned: the result must parse, must carry exactly the old
// list plus this entry, and must leave every other setting untouched — so an exotic file shape
// surfaces as an error rather than as a quietly mangled config.
func insertHostAcknowledgement(data []byte, entry UnconfinedHost) ([]byte, UnconfinedHost, error) {
	var before fileConfig
	if err := yaml.Unmarshal(data, &before); err != nil {
		return nil, UnconfinedHost{}, err
	}
	for _, h := range before.UnconfinedHosts {
		if strings.TrimSpace(h.ID) == entry.ID {
			return nil, h, nil
		}
	}

	updated, err := spliceHostAcknowledgement(data, entry)
	if err != nil {
		return nil, UnconfinedHost{}, err
	}
	var after fileConfig
	if err := yaml.Unmarshal(updated, &after); err != nil {
		return nil, UnconfinedHost{}, fmt.Errorf("the edited file would not parse: %w", err)
	}
	if !hostsAppended(before.UnconfinedHosts, after.UnconfinedHosts, entry) ||
		!sameApartFrom(before, after, unconfinedHostsKey) {
		return nil, UnconfinedHost{}, errors.New(
			"the edit would have changed more than the unconfined-hosts list; add the entry by hand")
	}
	return updated, entry, nil
}

// spliceHostAcknowledgement inserts the rendered entry into data's lines. There are three shapes
// to meet, in the order they are common: no list at all (the key is absent or, in the seeded
// template, still commented out) — append a documented block; a list with items — append an item
// to it, matched to its own indentation; the bare key with nothing under it — start the list.
//
// A flow-style list ([...]) has no line to append to, and a multi-document file would hide the
// entry in a document apogee never reads; both refuse rather than guess.
func spliceHostAcknowledgement(data []byte, entry UnconfinedHost) ([]byte, error) {
	doc, err := Document(data)
	if err != nil {
		return nil, err
	}
	lines := SplitConfigLines(data)
	value, keyLine := unconfinedHostsNode(doc)

	switch {
	case value == nil:
		block, err := renderHostBlock(entry)
		if err != nil {
			return nil, err
		}
		return joinConfigLines(appendBlock(lines, block)), nil

	case value.Style&yaml.FlowStyle != 0:
		return nil, errors.New("its unconfined-hosts: list is written in flow style ([...]); add the entry by hand")

	case value.Kind == yaml.SequenceNode && len(value.Content) > 0:
		item, err := renderHostEntry(entry, value.Column-1)
		if err != nil {
			return nil, err
		}
		return insertAt(lines, item, maxNodeLine(value.Content[len(value.Content)-1]), "its unconfined-hosts: list")

	default: // `unconfined-hosts:` with nothing under it — a null value, not a list yet
		item, err := renderHostEntry(entry, listIndent)
		if err != nil {
			return nil, err
		}
		return insertAt(lines, item, keyLine, "its unconfined-hosts: key")
	}
}

// Document decodes the config's single YAML document node, or nil when the file holds no
// document at all — empty, or nothing but comments, the shape of a config whose every setting the
// user has commented out (the seeded template keeps one key active, so it decodes to a document).
// A second document is refused: yaml.Unmarshal reads only the first, so an entry appended to the
// last one would be written and never read.
func Document(data []byte) (*yaml.Node, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := decoder.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, err
	}
	var second yaml.Node
	switch err := decoder.Decode(&second); {
	case err == nil:
		return nil, errors.New("it holds more than one YAML document, and apogee reads only the first; edit the file by hand")
	case !errors.Is(err, io.EOF):
		return nil, err
	}
	return &doc, nil
}

// unconfinedHostsNode returns the value node of the top-level `unconfined-hosts:` key and the line
// its key sits on. A nil value node means the key is absent — the common case, since the template
// ships it commented out.
func unconfinedHostsNode(doc *yaml.Node) (*yaml.Node, int) {
	if doc == nil || len(doc.Content) == 0 {
		return nil, 0
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, 0
	}
	key, value := mappingEntry(root, unconfinedHostsKey)
	if key == nil {
		return nil, 0
	}
	return value, key.Line
}

// mappingEntry finds one key of a block mapping and returns its key and value nodes, or two nils
// when the mapping does not have it. The first match wins, which is also the only match a config
// apogee can read has: a duplicate key fails the parse both writers do before they splice.
func mappingEntry(mapping *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i], mapping.Content[i+1]
		}
	}
	return nil, nil
}

// maxNodeLine is the last line the node's subtree reaches — for a list item whose fields each sit
// on their own line, the line to append the next item after.
func maxNodeLine(n *yaml.Node) int {
	last := n.Line
	for _, c := range n.Content {
		if l := maxNodeLine(c); l > last {
			last = l
		}
	}
	return last
}

// renderHostEntry renders one list item through the YAML marshaller — which owns the quoting, so
// no field can smuggle a syntax break into the file — indented to the given column.
func renderHostEntry(entry UnconfinedHost, indent int) ([]string, error) {
	out, err := yaml.Marshal([]UnconfinedHost{entry})
	if err != nil {
		return nil, fmt.Errorf("render the acknowledgement entry: %w", err)
	}
	pad := strings.Repeat(" ", indent)
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		lines = append(lines, pad+l)
	}
	return lines, nil
}

// renderHostBlock renders a whole `unconfined-hosts:` block — the explanatory comment, the key,
// and the first item — for a config that has no list yet.
func renderHostBlock(entry UnconfinedHost) ([]string, error) {
	item, err := renderHostEntry(entry, listIndent)
	if err != nil {
		return nil, err
	}
	block := append(strings.Split(hostAcknowledgementHeader, "\n"), unconfinedHostsKey+":")
	return append(block, item...), nil
}

// appendBlock puts a new top-level block at the end of the file, separated from whatever is
// already there by one blank line.
func appendBlock(lines, block []string) []string {
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
		block = append([]string{""}, block...)
	}
	return append(lines, block...)
}

// insertAt splices insert into lines after the 1-based line number at, which must name a line the
// file actually has — a position outside it means the node tree and the text disagree, which is a
// refusal, not something to clamp into place. subject names what pointed at that line, so the
// refusal says which part of the file was mis-read.
func insertAt(lines, insert []string, at int, subject string) ([]byte, error) {
	if at < 1 || at > len(lines) {
		return nil, fmt.Errorf("%s points at line %d, which is outside the file", subject, at)
	}
	out := make([]string, 0, len(lines)+len(insert))
	out = append(out, lines[:at]...)
	out = append(out, insert...)
	out = append(out, lines[at:]...)
	return joinConfigLines(out), nil
}

// SplitConfigLines splits the file into lines without a trailing empty element, so a rejoin plus
// one closing newline reproduces the file exactly. A blank file has no lines at all.
func SplitConfigLines(data []byte) []string {
	text := string(data)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// joinConfigLines rejoins the lines, always ending the file with a newline.
func joinConfigLines(lines []string) []byte {
	return []byte(strings.Join(lines, "\n") + "\n")
}

// hostsAppended reports whether after is exactly before plus entry, appended last — the shape a
// splice must produce. Anything else (a reordered, dropped, or altered neighbour) is a mis-read.
func hostsAppended(before, after []UnconfinedHost, entry UnconfinedHost) bool {
	if len(after) != len(before)+1 {
		return false
	}
	for i := range before {
		if after[i] != before[i] {
			return false
		}
	}
	return after[len(after)-1] == entry
}

// sameApartFrom reports whether two parsed configs agree on every setting but the ones at the given
// dotted registry paths — the guarantee that a textual splice touched nothing else. Each path is
// blanked in both copies and what is left is compared whole, so a key the line arithmetic clipped,
// reordered or re-typed shows up as a difference even though the writer never meant to touch it.
//
// One path is the ordinary case, an edit being one setting; the legacy migration passes two,
// because folding the retired keys writes `servers:` and `server:` as a single change.
//
// A path the schema does not have reports false: the comparison it was asked for cannot be made,
// and a verification step that cannot verify must refuse.
func sameApartFrom(before, after fileConfig, paths ...string) bool {
	b, a := before, after
	for _, path := range paths {
		if !zeroConfigPath(reflect.ValueOf(&b).Elem(), path) || !zeroConfigPath(reflect.ValueOf(&a).Elem(), path) {
			return false
		}
	}
	return reflect.DeepEqual(b, a)
}

// zeroConfigPath blanks the field at a dotted yaml path — `ui.spinner`, `endpoint` — in the struct
// v addresses, and reports whether the schema has that path at all.
//
// A block reached through a pointer is COPIED before its leaf is blanked, so the caller's own
// parsed config is never mutated through the shared pointer, and a block left holding nothing but
// zero fields is set back to nil. That last step is what makes the comparison honest across an
// insert: a `ui:` block the writer created for this one key must compare equal to the absent block
// it replaced, while a block that still holds another setting stays non-nil and any difference in
// it is still caught.
func zeroConfigPath(v reflect.Value, path string) bool {
	head, rest, nested := strings.Cut(path, ".")
	field, ok := fieldByYAMLTag(v, head)
	if !ok {
		return false
	}
	if !nested {
		field.Set(reflect.Zero(field.Type()))
		return true
	}
	if field.Kind() != reflect.Pointer || field.Type().Elem().Kind() != reflect.Struct {
		return false
	}
	if field.IsNil() { // nothing to blank, but the rest of the path must still exist in the schema
		return zeroConfigPath(reflect.New(field.Type().Elem()).Elem(), rest)
	}
	block := reflect.New(field.Type().Elem())
	block.Elem().Set(field.Elem())
	field.Set(block)
	if !zeroConfigPath(block.Elem(), rest) {
		return false
	}
	if block.Elem().IsZero() {
		field.Set(reflect.Zero(field.Type()))
	}
	return true
}

// fieldByYAMLTag finds the struct field a yaml key names — the same tags the decoder reads, so the
// schema stays the single description of what a config file may hold.
func fieldByYAMLTag(v reflect.Value, tag string) (reflect.Value, bool) {
	typ := v.Type()
	for i := range typ.NumField() {
		if name, _, _ := strings.Cut(typ.Field(i).Tag.Get("yaml"), ","); name == tag {
			return v.Field(i), true
		}
	}
	return reflect.Value{}, false
}

// writeConfigAtomically replaces path's contents through a temp file in the same directory and a
// rename, so an interrupted write leaves the old config intact rather than a truncated one. The
// existing file mode is carried over: a config may hold endpoint details, so a rewrite must never
// widen its permissions.
func writeConfigAtomically(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("apogee: stat config %q: %w", path, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("apogee: create a temporary config beside %q: %w", path, err)
	}
	name := tmp.Name()
	defer os.Remove(name) // a no-op once the rename below has moved it into place

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("apogee: write %q: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("apogee: close %q: %w", name, err)
	}
	if err := os.Chmod(name, info.Mode().Perm()); err != nil {
		return fmt.Errorf("apogee: preserve the mode of %q: %w", path, err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("apogee: replace %q: %w", path, err)
	}
	return nil
}

// ----------------------------------------------------------------------------
// The per-entry setting writer (the remembered-model-choice half)
// ----------------------------------------------------------------------------
//
// Remembering a model choice is one line written into one `servers:` entry: the picked model id into
// a plain server's `model:` key, or the committed Launch profile into a launcher-fronted entry's
// `launch-profile:` key, so the next launch comes back where the user left off. The ADDRESSING is
// the key-source writer's exactly — a key inside a list item, picked out of the list by its
// `name:` — so the machinery above serves this whole: parse for positions, splice text, re-parse and
// compare, and refuse a rewritten list that would no longer load.
//
// What is new is the ALLOW-LIST. Each writer above spells its own key, while this one takes the key
// from its caller, so the caller is checked before the file is even opened: a writer that trusted its
// caller with a key name would be one refactor away from rewriting an entry's endpoint.
//
// It is set-only. A recorded choice is a record of what the user picked, so forgetting it is an edit
// of their own file rather than something apogee does on their behalf — which leaves no meaning for
// an empty value, and it is refused instead.

// The `servers:` entry keys this writer may address — the model a plain multi-model server should
// come back on, and the Launch profile a launcher-fronted one should. Spelled as ServerEntry tags
// them, since the writer matches them in the node tree.
const (
	entryModelKey         = "model"
	entryLaunchProfileKey = "launch-profile"
)

// entrySetting is one writable per-entry key: its spelling in the file, the value an entry already
// holds for it (what makes a re-set a no-op), and how it stands on the entry the splice is verified
// against. Reading and writing the field are two halves of one row, so the allow-list cannot come
// apart from the schema it addresses.
type entrySetting struct {
	Key string
	get func(ServerEntry) string
	set func(*ServerEntry, string)
}

// entrySettings is the whole of what apogee writes into a `servers:` entry on the user's behalf. It
// is deliberately two rows: every other key on an entry describes the server rather than records a
// choice made at the keyboard, and those stay the user's to edit.
var entrySettings = []entrySetting{
	{
		Key: entryModelKey,
		get: func(s ServerEntry) string { return s.Model },
		set: func(s *ServerEntry, value string) { s.Model = value },
	},
	{
		Key: entryLaunchProfileKey,
		get: func(s ServerEntry) string { return s.LaunchProfile },
		set: func(s *ServerEntry, value string) { s.LaunchProfile = value },
	},
}

// SaveServerEntrySetting writes value as the setting key of the `servers:` entry named name, and
// reports nothing when the entry already says exactly that (a re-set is a confirmation, not a
// rewrite). The entry's other lines, the comments around it and every sibling entry come back
// byte-identical; an absent config is seeded from the embedded template first, and the write is
// atomic and mode-preserving — the writers above's contract, unchanged.
//
// Anything the edit cannot do surgically is refused with the "by hand" idiom rather than guessed at,
// and the file is left exactly as it was: a key outside the allow-list, an empty value, a name the
// list does not carry, a shape the text and the node tree would disagree about, and a result that
// would no longer load (ValidateServers) — a `launch-profile:` on an entry with no launcher to
// actuate it is refused there rather than written.
func SaveServerEntrySetting(path, name, key, value string) error {
	setting, err := writableEntrySetting(key)
	if err != nil {
		return err
	}
	v := strings.TrimSpace(value)
	if v == "" {
		return fmt.Errorf(
			"apogee: cannot write %s: on the %q server entry: there is no value to record, and this "+
				"writer does not clear a key — remove the line by hand instead", key, name)
	}
	data, err := ReadConfigForWrite(path)
	if err != nil {
		return err
	}
	updated, err := setEntrySetting(data, name, setting, v)
	if err != nil {
		return fmt.Errorf("apogee: update config %q: %w", path, err)
	}
	if updated == nil {
		return nil
	}
	return writeConfigAtomically(path, updated)
}

// writableEntrySetting resolves a key to the row that describes it, and refuses one this writer may
// not touch — BEFORE the config file is opened, so "refused" and "written" can never be the same
// outcome. The message names what apogee does write, since a caller that asked for the wrong key is
// a defect in the binary rather than in the user's file.
func writableEntrySetting(key string) (entrySetting, error) {
	at := slices.IndexFunc(entrySettings, func(s entrySetting) bool { return s.Key == key })
	if at < 0 {
		names := make([]string, len(entrySettings))
		for i, s := range entrySettings {
			names[i] = s.Key + ":"
		}
		return entrySetting{}, fmt.Errorf(
			"apogee: %q is not a servers: entry setting apogee writes: it writes %s, and every other key "+
				"on an entry is the user's own", key, joinAnd(names))
	}
	return entrySettings[at], nil
}

// setEntrySetting returns the config bytes with the named entry's key set to value, or nil bytes when
// the entry already reads that way. The value's text comes from the YAML marshaller, which owns the
// quoting — so a model id or a profile name a bare scalar would misread (`off`, `1.5`, one carrying a
// colon or a `#`) lands as a value a reader takes back out unchanged.
func setEntrySetting(data []byte, name string, setting entrySetting, value string) ([]byte, error) {
	before, at, err := serverEntryAt(data, name)
	if err != nil {
		return nil, err
	}
	if setting.get(before.Servers[at]) == value {
		return nil, nil // already what the file says: a confirmation, not a rewrite
	}
	text, err := renderScalar(value)
	if err != nil {
		return nil, err
	}
	updated, err := spliceEntrySetting(data, name, setting.Key, text)
	if err != nil {
		return nil, err
	}
	want := before.Servers[at]
	setting.set(&want, value)
	return verifiedEntrySplice(data, updated, before, at, want)
}

// spliceEntrySetting writes the key into the entry's block: over the line the entry already spells it
// on — keeping the user's own indentation, the gap they aligned the value with and any end-of-line
// note — or, when the entry has no such line, as a new last child of the block at the indentation its
// siblings use.
//
// scalarLineParts does the reading of an existing line, exactly as it does for a top-level setting, so
// the shapes it refuses are refused here too: a value that runs past its key's line, a block scalar, a
// text and a node tree that disagree about where the key sits. Each one would leave part of the old
// value behind. The insertion point is the last line the entry's subtree reaches, which is where the
// next sibling key would go; an entry whose last value the node tree cannot measure would put that
// line inside it instead, and the verification below is what catches that.
func spliceEntrySetting(data []byte, name, key, text string) ([]byte, error) {
	lines, entry, err := serverEntryNode(data, name)
	if err != nil {
		return nil, err
	}
	if keyNode, valueNode := mappingEntry(entry, key); keyNode != nil {
		t := ScalarTarget{Key: key, Kind: KindString, KeyNode: keyNode, ValueNode: valueNode}
		head, gap, tail, err := scalarLineParts(lines, t)
		if err != nil {
			return nil, err
		}
		out := slices.Clone(lines)
		out[keyNode.Line-1] = head + gap + text + tail
		return joinConfigLines(out), nil
	}
	line := []string{indentLine(entry.Column-1, key+": "+text)}
	return insertAt(lines, line, maxNodeLine(entry), fmt.Sprintf("its %q entry", name))
}
