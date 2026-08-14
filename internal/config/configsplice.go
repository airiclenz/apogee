package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// ----------------------------------------------------------------------------
// The splice machinery every config writer shares
// ----------------------------------------------------------------------------
//
// Every edit apogee makes to config.yaml — a host acknowledgement, a remembered choice on a
// `servers:` entry, a /settings key, an entry's key source, and the one-time legacy fold — is the
// same write under one contract (ADR 0035): the file is the user's, hand-edited and read back
// months later, so a line written on their behalf must leave every comment, key order, indentation
// and edit of theirs exactly as found. That rules out the obvious implementation — unmarshal, set,
// re-marshal — which would hand back a file with every word of its documentation silently deleted,
// and leaves a TEXTUAL splice guided by the positions the parser reports.
//
// What lives here is the part of that write which belongs to no one writer: read the file (seeding
// it from the template first), parse it for positions, find a key in the node tree, cut and rejoin
// the text around it, verify the result against the original, and put it on disk atomically. What
// each writer keeps for itself is its ADDRESSING — which keys it may touch at all, and how it names
// the place one of them sits in — and the rendering of the value. Nothing in this file knows the
// name of a single config key.
//
// The verification is the load-bearing half. A splice is re-parsed and compared against the config
// it started from, whole and field by field apart from the path being written (sameApartFrom), so a
// file shape the line arithmetic mis-reads surfaces as a refusal — "edit the file by hand" — rather
// than as a config the user quietly cannot get back.

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

// appendBlock puts a new top-level block at the end of the file, separated from whatever is
// already there by one blank line.
func appendBlock(lines, block []string) []string {
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
		block = append([]string{""}, block...)
	}
	return append(lines, block...)
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

// ReadConfigForWrite seeds the config from the embedded template if it is not there yet and reads
// it back — the state the setting writers splice from: SaveConfigSetting and ResetConfigSetting
// (configwrite_scalar.go), SaveMechanismSetting (configwrite_mechanism.go) and SaveServerEntrySetting
// (configwrite.go). The server-entry key-source edits are the one exception among the splicers:
// they start from readConfigForEntryEdit (configwrite_keysource.go), which deliberately does not
// seed.
//
// One caller does not splice at all, and completes the list: externalEdit.spec
// (cmd/apogee/settingsedit.go) reads for the seed and for the bytes it locates the key's line in,
// so the file the human is about to open in $EDITOR exists and the return trip's baseline — taken
// after the seed — does not report the whole template back as an edit they made.
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

// indentLine pads a rendered setting to the column it belongs in.
func indentLine(indent int, text string) string {
	return strings.Repeat(" ", indent) + text
}
