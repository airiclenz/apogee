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
	"strconv"
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
// The scalar setting writer (the /settings screen's half)
// ----------------------------------------------------------------------------
//
// The settings surface persists one key per deliberate edit (ADR 0035) into the same file the
// acknowledgement writer above edits, under the same contract and for the same reason: the user
// owns this file, hand-edits it and reads it back months later, so an edit made on their behalf
// must leave every comment, key order and formatting choice exactly as found. The machinery
// generalises almost whole — parse for positions, splice text, re-parse and compare before
// writing anything — and what is new is the ADDRESSING: a scalar edit names its target by
// registry path (`ui.spinner`), so the registry's row decides what may be written, in what
// shape, and the file's own text decides which line it lands on.
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
// the acknowledgement writer's contract above, unchanged.
func SaveConfigSetting(path, key, value string) error {
	k, err := writableKey(key)
	if err != nil {
		return err
	}
	if err := validateSettingValue(k, value); err != nil {
		return err
	}
	data, err := ReadConfigForWrite(path)
	if err != nil {
		return err
	}
	updated, err := setScalarSetting(data, k, value)
	if err != nil {
		return fmt.Errorf("apogee: update config %q: %w", path, err)
	}
	if updated == nil {
		return nil
	}
	return writeConfigAtomically(path, updated)
}

// ResetConfigSetting removes the config file's active line for the registry path key, so the key
// resolves from its default again. A key the file does not set is already at its default: that is
// a no-op, not an error, and nothing is written.
func ResetConfigSetting(path, key string) error {
	k, err := writableKey(key)
	if err != nil {
		return err
	}
	data, err := ReadConfigForWrite(path)
	if err != nil {
		return err
	}
	updated, err := deleteScalarSetting(data, k)
	if err != nil {
		return fmt.Errorf("apogee: update config %q: %w", path, err)
	}
	if updated == nil {
		return nil
	}
	return writeConfigAtomically(path, updated)
}

// validateSettingValue refuses a value the key cannot hold, BEFORE the config file is opened: the
// kind's own check (renderSettingValue — a bool is true or false, an enum is one of its values) and
// then the key's validate hook, which is the check startup already makes for that key
// (Key.Validate). A value refused here has touched nothing at all — not even the seeding read
// below — so "invalid" and "written" can never be the same outcome.
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

// ReadConfigForWrite seeds the config from the embedded template if it is not there yet and reads
// it back — the state every splice below starts from.
func ReadConfigForWrite(path string) ([]byte, error) {
	if path == "" {
		return nil, errors.New("apogee: cannot write a setting: no config file path is known")
	}
	if _, err := seedConfig(path, defaultConfigYAML); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("apogee: read config %q: %w", path, err)
	}
	return data, nil
}

// setScalarSetting returns the config bytes with key set to value, or nil bytes when the edit would
// not change one — a re-set of what the file already says is a confirmation, not a rewrite. The
// splice is verified before it is returned: the result must parse, must resolve the key to the
// value asked for, and must agree with the original on every OTHER setting — so a file shape the
// line arithmetic mis-reads surfaces as an error rather than as a quietly mangled config.
//
// The splice runs before the file is parsed into fileConfig, so a config apogee could not read at
// all is refused by the shape checks — which can say WHICH part of the file they could not
// read — rather than by the decoder's own type error.
func setScalarSetting(data []byte, k Key, value string) ([]byte, error) {
	text, want, err := renderSettingValue(k, value)
	if err != nil {
		return nil, err
	}
	updated, err := spliceScalarSet(data, k, text)
	if err != nil {
		return nil, err
	}
	if bytes.Equal(updated, data) {
		return nil, nil
	}
	return verifiedSplice(data, updated, k, want, true)
}

// deleteScalarSetting returns the config bytes with key's active line removed, or nil bytes when
// the file does not set the key at all. It is verified exactly as a set is, with the target's
// absence standing in for its value.
func deleteScalarSetting(data []byte, k Key) ([]byte, error) {
	updated, err := spliceScalarDelete(data, k)
	if err != nil || updated == nil {
		return nil, err
	}
	return verifiedSplice(data, updated, k, "", false)
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
		// verifiedSplice would refuse the edit. Nothing else is trimmed — a prompt's leading indentation
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

// ScalarTarget is where a key stands in the parsed document: the nodes of its active `key: value`
// (nil when the file does not set it) and, for a nested key, its parent block.
type ScalarTarget struct {
	Key        string     // the leaf key as the file spells it
	Kind       Kind       // what the key holds, and with it the value shapes its line may carry
	Parent     string     // the block the key sits in, empty for a top-level key
	KeyNode    *yaml.Node // the leaf key, nil when the key has no active line
	ValueNode  *yaml.Node // its value, nil with keyNode
	ParentKey  *yaml.Node // the block's key, nil when the block is absent
	ParentBody *yaml.Node // the block's value, nil when the block is absent
	ParentNull bool       // the block's key is there with nothing under it yet
}

// isSet reports whether the file carries an active line for the key — the difference between a
// replace and an insert, and between a reset and a no-op.
func (t ScalarTarget) IsSet() bool { return t.KeyNode != nil }

// childIndent is the column the block's children are indented to, so an inserted child joins the
// block the way the ones already there are written.
func (t ScalarTarget) childIndent() int {
	if t.ParentBody != nil && !t.ParentNull {
		return t.ParentBody.Column - 1
	}
	return listIndent
}

// ScalarTargetIn locates the key in the parsed document. Shapes it refuses rather than splices:
// a top level that is not a mapping of settings, a flow-style mapping (no line to edit — the
// flow-style list refusal above, one level down), and a block key holding something other than
// a block. Every one of them means the text and the node tree would disagree about where the
// key's line is.
func ScalarTargetIn(doc *yaml.Node, k Key) (ScalarTarget, error) {
	head, rest, nested := strings.Cut(k.Path, ".")
	t := ScalarTarget{Key: head, Kind: k.Kind}
	if nested {
		t.Parent, t.Key = head, rest
	}
	root, err := rootMapping(doc)
	if root == nil || err != nil {
		return t, err
	}
	scope := root
	if t.Parent != "" {
		key, body := mappingEntry(root, t.Parent)
		if key == nil {
			return t, nil // the block is absent: the insert creates it
		}
		switch {
		case body.Style&yaml.FlowStyle != 0:
			return t, fmt.Errorf("its %s: block is written in flow style ({...}); edit the file by hand", t.Parent)
		case isNullNode(body):
			t.ParentKey, t.ParentBody, t.ParentNull = key, body, true
			return t, nil // `ui:` with nothing under it yet: no children to find
		case body.Kind != yaml.MappingNode:
			return t, fmt.Errorf("its %s: is not a block of settings; edit the file by hand", t.Parent)
		}
		t.ParentKey, t.ParentBody, scope = key, body, body
	}
	t.KeyNode, t.ValueNode = mappingEntry(scope, t.Key)
	return t, nil
}

// rootMapping returns the document's top-level mapping, or nil for a document that has none —
// an empty file, or one that is nothing but comments, which is the shape of a config whose every
// setting the user has commented out. A top level that is a list or a scalar, or a mapping
// written in flow style, is refused: neither has the block structure a setting's line lives in.
func rootMapping(doc *yaml.Node) (*yaml.Node, error) {
	if doc == nil || len(doc.Content) == 0 {
		return nil, nil
	}
	root := doc.Content[0]
	switch {
	case root.Kind != yaml.MappingNode:
		return nil, errors.New("its top level is not a mapping of settings; edit the file by hand")
	case root.Style&yaml.FlowStyle != 0:
		return nil, errors.New("its top level is written in flow style ({...}); edit the file by hand")
	}
	return root, nil
}

// isNullNode reports whether a node is the empty value a bare `key:` parses to — a value there is
// no text of, so it can be written over but not read from.
func isNullNode(n *yaml.Node) bool {
	return n != nil && n.Kind == yaml.ScalarNode && n.Tag == "!!null"
}

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

// scalarLineParts splits the target's active line into the text through its colon, the gap before
// the value, and any trailing comment — the pieces a replacement rebuilds the line from, keeping
// the user's own indentation, alignment and end-of-line note. Splitting it is also the check that
// the line IS a single plain `key: value`, which is what makes removing it safe: a value spanning
// lines, a block scalar, or a key the text spells differently from the node tree is refused, since
// rewriting one line of it would leave the rest behind.
func scalarLineParts(lines []string, t ScalarTarget) (head, gap, tail string, err error) {
	line := t.KeyNode.Line
	if line < 1 || line > len(lines) {
		return "", "", "", fmt.Errorf("its %s: sits on line %d, which is outside the file", t.Key, line)
	}
	raw := lines[line-1]
	indent := t.KeyNode.Column - 1
	if indent < 0 || indent > len(raw) || !strings.HasPrefix(raw[indent:], t.Key+":") {
		return "", "", "", fmt.Errorf("its %s: line reads unexpectedly at line %d; edit the file by hand", t.Key, line)
	}
	head, gap = raw[:indent+len(t.Key)+1], " "
	if !isNullNode(t.ValueNode) {
		if err := t.valueFitsOneLine(line); err != nil {
			return "", "", "", err
		}
		if start := t.ValueNode.Column - 1; start > len(head) && start <= len(raw) && strings.TrimSpace(raw[len(head):start]) == "" {
			gap = raw[len(head):start]
		}
	}
	// The end-of-line note belongs to the value, except on a bare `key:` — with no value text to
	// hang off, the parser attaches it to the key instead. Either way it is the user's note about
	// this setting, and it stays on the line.
	comment := t.ValueNode.LineComment
	if comment == "" {
		comment = t.KeyNode.LineComment
	}
	if comment != "" {
		if at := strings.LastIndex(raw, comment); at > len(head) {
			for at > 0 && (raw[at-1] == ' ' || raw[at-1] == '\t') {
				at--
			}
			tail = raw[at:]
		}
	}
	return head, gap, tail, nil
}

// valueFitsOneLine reports whether the target's EXISTING value is one this writer may rewrite in
// place: a value that begins and ends on the key's own line, so replacing that line leaves nothing of
// it behind. What counts as one differs by kind and by nothing else — a plain scalar for every kind
// but the list, and for a list a FLOW sequence ([...]) that also opens and closes there.
//
// A block sequence is refused rather than folded into one line: the file is the user's, and turning
// four lines of theirs into one is a rewrite they did not ask for. It is the same refusal the flow-
// style mapping gets one level up, in the other direction.
func (t ScalarTarget) valueFitsOneLine(line int) error {
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

// scalarInsertion is the lines to insert for a key the file does not set, and the 1-based line to
// insert them after — 0 for "append at the end", the fallback when the file documents the key
// nowhere. The four cases, in the order they are common:
//
//   - a top-level key: its own line, directly below its commented example (ADR 0035);
//   - a key whose block is absent: the block line and the key under it, below the commented
//     example block that documents them — below the WHOLE block, so the created setting sits under
//     its documentation rather than wedged into the middle of it;
//   - a key whose block is there and populated: joined to that block, after its last line and at
//     the indentation its siblings use. A commented example inside the block is NOT a candidate
//     here: the key has to land inside the mapping that is already open, and the example is only
//     ever above or below it;
//   - a key whose block key is there with nothing under it: the first child of that block.
func scalarInsertion(lines []string, t ScalarTarget, text string) ([]string, int, error) {
	setting := settingLines(t, text)
	switch {
	case t.Parent == "":
		at, err := CommentedExampleLine(lines, t.Key)
		return setting, at, err
	case t.ParentBody == nil:
		at, err := commentedExampleBlockEnd(lines, t.Parent)
		return append([]string{t.Parent + ":"}, indentLines(listIndent, setting)...), at, err
	case t.ParentNull:
		return indentLines(t.childIndent(), setting), t.ParentKey.Line, nil
	default:
		return indentLines(t.childIndent(), setting), maxNodeLine(t.ParentBody), nil
	}
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

// indentLine pads a rendered setting to the column it belongs in.
func indentLine(indent int, text string) string {
	return strings.Repeat(" ", indent) + text
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

// verifiedSplice is the gate every scalar splice passes before it reaches the disk: the result must
// parse, must resolve the target path to exactly what the edit intended (or to nothing, for a
// reset), and must agree with the original config on every other setting. It returns the verified
// bytes, so a caller cannot forget to check them.
func verifiedSplice(data, updated []byte, k Key, want string, set bool) ([]byte, error) {
	var before fileConfig
	if err := yaml.Unmarshal(data, &before); err != nil {
		return nil, err
	}
	var after fileConfig
	if err := yaml.Unmarshal(updated, &after); err != nil {
		return nil, fmt.Errorf("the edited file would not parse: %w", err)
	}
	got, isSet, err := scalarAtPath(updated, k.Path)
	if err != nil {
		return nil, fmt.Errorf("the edited file would not parse: %w", err)
	}
	switch {
	case set && (!isSet || got != want):
		return nil, fmt.Errorf("the edit did not put %s where a reader would look for it; edit the file by hand", k.Path)
	case !set && isSet:
		return nil, fmt.Errorf("the edit left %s set; edit the file by hand", k.Path)
	case !sameApartFrom(before, after, k.Path):
		return nil, fmt.Errorf("the edit would have changed more than %s; edit the file by hand", k.Path)
	}
	return updated, nil
}

// scalarAtPath reads the value a config file actually holds at a dotted path, the way any YAML
// reader sees it — the round-trip half of the verification: a splice must put the value where the
// PARSER looks for it, not merely somewhere that reads correctly in the text. A path that is
// absent, or that runs through a value which is not a block, reports not-set.
//
// A list of plain values reads back as the one-line spelling the writer renders and the row shows
// (listValue), so a name list is verified the same way a scalar is. A list holding anything else —
// the blocks under `servers:` — is not a value a settings path names at all, and reports not-set.
func scalarAtPath(data []byte, path string) (string, bool, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", false, err
	}
	var node any = doc
	for _, segment := range strings.Split(path, ".") {
		block, ok := node.(map[string]any)
		if !ok {
			return "", false, nil
		}
		if node, ok = block[segment]; !ok {
			return "", false, nil
		}
	}
	switch v := node.(type) {
	case nil: // a bare `key:` — set, with nothing in it
		return "", true, nil
	case map[string]any:
		return "", false, nil
	case []any:
		names := make([]string, 0, len(v))
		for _, item := range v {
			switch item.(type) {
			case nil, map[string]any, []any:
				return "", false, nil
			}
			names = append(names, fmt.Sprint(item))
		}
		return listValue(names), true, nil
	}
	return fmt.Sprint(node), true, nil
}

// listValue spells a name list the way the template's inline form spells it ("[AGENTS.md]") — the
// CANONICAL spelling a written list is verified against, so the value a reader takes back out of
// the file is the same string that was typed. lineCount counts the lines of a multi-line value.
//
// Both are deliberately duplicated from the /settings row renderer (cmd/apogee/settingsrows.go),
// which spells the same two values for the human. They are three lines of string handling with no
// state behind them, and the binary's display projection stays in the binary (ADR 0011, ADR 0043) —
// so the alternative to two copies is an exported string-formatting surface on this package that
// exists only so the renderer can borrow it.
func listValue(names []string) string { return "[" + strings.Join(names, ", ") + "]" }

func lineCount(text string) int {
	if text == "" {
		return 0
	}
	return len(strings.Split(strings.TrimSuffix(text, "\n"), "\n"))
}
