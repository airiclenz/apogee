package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
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
// comments off nodes, and the seeded template (cmd/apogee/defaults/config.yaml) is ENTIRELY
// comments — it parses to no nodes at all — so a re-marshal would hand the user back a file with
// one setting in it, having silently deleted every word of documentation they started with.
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
func saveHostAcknowledgement(path, hostID string, now time.Time) (string, unconfinedHost, error) {
	id := strings.TrimSpace(hostID)
	if id == "" {
		return "", unconfinedHost{}, errors.New(
			"apogee: cannot save the host acknowledgement: this host has no id to record")
	}
	if platform.IsUnidentifiedHostID(id) {
		return "", unconfinedHost{}, errors.New(
			"apogee: cannot save the host acknowledgement: this machine reports neither a hostname nor a " +
				"machine id, so the recorded id would name every such machine rather than this one — " +
				"/confine off still applies to this session")
	}
	if path == "" {
		return "", unconfinedHost{}, errors.New(
			"apogee: cannot save the host acknowledgement: no config file path is known")
	}
	if _, err := seedConfig(path, defaultConfigYAML); err != nil {
		return "", unconfinedHost{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", unconfinedHost{}, fmt.Errorf("apogee: read config %q: %w", path, err)
	}

	entry := unconfinedHost{ID: id, Acknowledged: now.Format(acknowledgedDateLayout), Note: hostAcknowledgementNote}
	updated, recorded, err := insertHostAcknowledgement(data, entry)
	if err != nil {
		return "", unconfinedHost{}, fmt.Errorf("apogee: update config %q: %w", path, err)
	}
	if updated == nil { // already acknowledged: the save is a confirmation, not a second entry
		return path, recorded, nil
	}
	if err := writeConfigAtomically(path, updated); err != nil {
		return "", unconfinedHost{}, err
	}
	return path, recorded, nil
}

// hostAcknowledgementSaver adapts the writer to the TUI's Options.SaveHostAcknowledgement seam.
// The renderer learns only which file now records this host — it already knows the id from
// Options.Confinement, and the on-disk format stays the binary's business (the Options.Save
// precedent).
func hostAcknowledgementSaver(path, hostID string) func() (string, error) {
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
func insertHostAcknowledgement(data []byte, entry unconfinedHost) ([]byte, unconfinedHost, error) {
	var before fileConfig
	if err := yaml.Unmarshal(data, &before); err != nil {
		return nil, unconfinedHost{}, err
	}
	for _, h := range before.UnconfinedHosts {
		if strings.TrimSpace(h.ID) == entry.ID {
			return nil, h, nil
		}
	}

	updated, err := spliceHostAcknowledgement(data, entry)
	if err != nil {
		return nil, unconfinedHost{}, err
	}
	var after fileConfig
	if err := yaml.Unmarshal(updated, &after); err != nil {
		return nil, unconfinedHost{}, fmt.Errorf("the edited file would not parse: %w", err)
	}
	if !hostsAppended(before.UnconfinedHosts, after.UnconfinedHosts, entry) || !sameApartFromHosts(before, after) {
		return nil, unconfinedHost{}, errors.New(
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
func spliceHostAcknowledgement(data []byte, entry unconfinedHost) ([]byte, error) {
	doc, err := configDocument(data)
	if err != nil {
		return nil, err
	}
	lines := splitConfigLines(data)
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
		return insertAt(lines, item, maxNodeLine(value.Content[len(value.Content)-1]))

	default: // `unconfined-hosts:` with nothing under it — a null value, not a list yet
		item, err := renderHostEntry(entry, listIndent)
		if err != nil {
			return nil, err
		}
		return insertAt(lines, item, keyLine)
	}
}

// configDocument decodes the config's single YAML document node, or nil when the file holds no
// document at all — empty, or nothing but comments, which is exactly the seeded template's shape.
// A second document is refused: yaml.Unmarshal reads only the first, so an entry appended to the
// last one would be written and never read.
func configDocument(data []byte) (*yaml.Node, error) {
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
		return nil, errors.New("it holds more than one YAML document, and apogee reads only the first; add the entry by hand")
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
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == unconfinedHostsKey {
			return root.Content[i+1], root.Content[i].Line
		}
	}
	return nil, 0
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
func renderHostEntry(entry unconfinedHost, indent int) ([]string, error) {
	out, err := yaml.Marshal([]unconfinedHost{entry})
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
func renderHostBlock(entry unconfinedHost) ([]string, error) {
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
// refusal, not something to clamp into place.
func insertAt(lines, insert []string, at int) ([]byte, error) {
	if at < 1 || at > len(lines) {
		return nil, fmt.Errorf("its unconfined-hosts: list ends at line %d, which is outside the file", at)
	}
	out := make([]string, 0, len(lines)+len(insert))
	out = append(out, lines[:at]...)
	out = append(out, insert...)
	out = append(out, lines[at:]...)
	return joinConfigLines(out), nil
}

// splitConfigLines splits the file into lines without a trailing empty element, so a rejoin plus
// one closing newline reproduces the file exactly. A blank file has no lines at all.
func splitConfigLines(data []byte) []string {
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
func hostsAppended(before, after []unconfinedHost, entry unconfinedHost) bool {
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

// sameApartFromHosts reports whether two parsed configs agree on every setting but the
// acknowledgement list — the guarantee that a textual splice touched nothing else.
func sameApartFromHosts(before, after fileConfig) bool {
	before.UnconfinedHosts, after.UnconfinedHosts = nil, nil
	return reflect.DeepEqual(before, after)
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
