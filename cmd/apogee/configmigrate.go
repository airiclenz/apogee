package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ----------------------------------------------------------------------------
// The one-time legacy migration (ADR 0036 decision 9)
// ----------------------------------------------------------------------------
//
// A config carrying the retired top-level `endpoint:`/`api-key:`/`host-alias:`/`model:` quadruple
// cannot be read by this version: the decoder ignores keys fileConfig no longer has, so the four
// lines that ARE the user's upstream would go silently unread. The legacy sniff (legacyFileConfig)
// catches that, and this file answers it — once — by folding those keys into the `servers:` entry
// and `server:` pointer that replace them.
//
// This is the ONE write in apogee that is not a user act in the moment, and ADR 0036 decision 9 is
// its only authority. Every property that makes it safe is enforced below: the previous bytes are
// copied to a timestamped sibling BEFORE the file is touched; the rewrite is the same textual
// splice-and-verify the rest of configwrite.go uses, so comments, key order and every unrelated
// line survive; the re-parse must equal the original apart from exactly that fold; and any doubt at
// all — a file shape the line arithmetic cannot read, a name the list already uses, a quadruple with
// no endpoint to move — refuses to write anything and falls back to the paste-able hard error.
//
// A file already in the new schema never reaches any of it. The sniff is the only trigger, so a
// config apogee can read is one this file does not open.
//
// A second retired key rides along at the bottom of this file: the top-level `llama-launcher:` of
// ADR 0029 decision 4, which moved onto the `servers:` entries. That one is REFUSED rather than
// folded — nobody but the user knows which entry the launcher belonged to — and it is refused
// FIRST, so a file carrying both retirements is never rewritten for one and then stopped for the
// other.

// legacyKeys are the four retired top-level keys, each with the field of the sniff struct it is
// parsed into. The fold needs BOTH halves — the key NAME to find the line to delete, the VALUE to
// build the entry and to say what moved — and pairing them here keeps a key from being deleted
// without being carried over, or announced without being deleted.
var legacyKeys = []struct {
	name  string
	value func(lc legacyFileConfig) string
}{
	{"endpoint", func(lc legacyFileConfig) string { return lc.Endpoint }},
	{"api-key", func(lc legacyFileConfig) string { return lc.APIKey }},
	{"host-alias", func(lc legacyFileConfig) string { return lc.HostAlias }},
	{"model", func(lc legacyFileConfig) string { return lc.Model }},
}

// serversKey and serverKey are the two keys the fold writes, spelled as fileConfig tags them.
const (
	serversKey = "servers"
	serverKey  = "server"
)

// backupStampLayout dates the backup down to the second: the migration runs once, but a user who
// restores the backup and runs an older binary against it can produce a second one, and two files
// a day apart must not be one file.
const backupStampLayout = "20060102-150405"

// migrateLegacyConfig folds a config still written in the retired schema into the servers-list
// schema, in place, and reports the bytes to resolve from plus the one line that announces the
// change (empty when nothing was migrated).
//
// The three outcomes, in the order they are common: the file is already in the new schema, so its
// bytes come back untouched and nothing is written; the fold succeeds, so the file on disk is the
// migrated one, a `.bak-<timestamp>` sibling holds what it was, and the note names both; the fold
// cannot be made safely, so NOTHING is written and the error carries the ready-to-paste replacement
// (legacyRefusal) — the same answer this refused with before the rewrite existed.
//
// Ahead of all three sits the one retirement that is not a migration at all: the top-level
// `llama-launcher:` key, refused before a single byte is read for the fold, so a config that
// carries both retired shapes is stopped rather than half-rewritten.
//
// now dates the backup and is injected so a test can name the file it expects.
func migrateLegacyConfig(path string, data []byte, now time.Time) ([]byte, string, error) {
	if err := refuseRetiredLauncherKey(path, data); err != nil {
		return nil, "", err
	}
	var lc legacyFileConfig
	if err := yaml.Unmarshal(data, &lc); err != nil {
		return nil, "", fmt.Errorf("apogee: parse config %q: %w", path, err)
	}
	if lc.isEmpty() {
		return data, "", nil
	}
	updated, entry, err := foldLegacyKeys(data, lc)
	if err != nil {
		return nil, "", legacyRefusal(path, lc, err)
	}
	// The backup is written only once the fold is verified, so a refusal leaves the apogee home
	// exactly as it found it — "no write at all" includes the copy.
	backup, err := backUpConfig(path, data, now)
	if err != nil {
		return nil, "", legacyRefusal(path, lc, err)
	}
	if err := writeConfigAtomically(path, updated); err != nil {
		return nil, "", legacyRefusal(path, lc, fmt.Errorf("the rewrite could not be written (%v)", err))
	}
	return updated, migrationNote(path, backup, entry, lc), nil
}

// foldLegacyKeys builds the migrated file: the four legacy lines removed, the entry they describe
// appended to `servers:`, and `server:` pointing at it. It returns the new bytes and the entry that
// was written, or an error naming what stopped it.
//
// Two shapes are refused before any line is moved, because neither can be folded without
// overwriting something the user wrote: a quadruple with no endpoint (those keys name no server on
// their own — there is nothing to move them INTO), and a name the list already uses (the fold would
// either collide with that entry or silently rename this one). A file that already sets `server:`
// is refused for the same reason: the pointer this writes would replace a startup choice the user
// made deliberately.
func foldLegacyKeys(data []byte, lc legacyFileConfig) ([]byte, serverEntry, error) {
	var before fileConfig
	if err := yaml.Unmarshal(data, &before); err != nil {
		return nil, serverEntry{}, fmt.Errorf("the rest of it does not parse into settings apogee can "+
			"read (%v)", err)
	}
	entry := serverEntry{
		Name:     lc.name(),
		Endpoint: strings.TrimSpace(lc.Endpoint),
		APIKey:   strings.TrimSpace(lc.APIKey),
		Model:    strings.TrimSpace(lc.Model),
	}
	switch {
	case entry.Endpoint == "":
		return nil, entry, errors.New("there is no endpoint: among them, and a server is its endpoint — " +
			"the entry they would fold into could never be talked to")
	case slices.ContainsFunc(before.Servers, func(s serverEntry) bool { return s.Name == entry.Name }):
		return nil, entry, fmt.Errorf("its servers: list already has an entry called %q, and one name names "+
			"one server", entry.Name)
	case strings.TrimSpace(before.Server) != "":
		return nil, entry, fmt.Errorf("it already starts on server: %q, which the fold would have to "+
			"repoint at the migrated entry", before.Server)
	}

	doc, err := configDocument(data)
	if err != nil {
		return nil, entry, err
	}
	root, err := rootMapping(doc)
	if err != nil {
		return nil, entry, err
	}
	if root == nil {
		return nil, entry, errors.New("it holds no settings at all, so the keys are not where the parser found them")
	}
	lines := splitConfigLines(data)
	drop, err := legacyKeyLines(root, lines)
	if err != nil {
		return nil, entry, err
	}
	block, at, err := serversInsertion(root, lines, entry)
	if err != nil {
		return nil, entry, err
	}
	folded, err := spliceFold(lines, drop, block, at)
	if err != nil {
		return nil, entry, err
	}
	// The pointer rides the ordinary scalar writer: it is one `server: <name>` line, which is
	// exactly what that writer places (below the key's commented example, ADR 0035) and verifies.
	k, ok := lookupKey(serverKey)
	if !ok {
		return nil, entry, errors.New("apogee has no server: setting to point at the entry")
	}
	updated, err := setScalarSetting(folded, k, entry.Name)
	if err != nil {
		return nil, entry, err
	}
	if updated == nil { // the guard above rules this out; a nil here would write the fold without its pointer
		return nil, entry, errors.New("the edit did not add a server: line")
	}
	if err := verifyFold(data, updated, entry); err != nil {
		return nil, entry, err
	}
	return updated, entry, nil
}

// verifyFold is the gate the migration passes before anything reaches the disk: the rewritten file
// must parse, must carry the retired keys nowhere, must hold exactly the old `servers:` list plus
// this entry with `server:` naming it, and must agree with the original on every OTHER setting. It
// is the whole-file statement of "the original with exactly the quadruple folded" — the two splices
// that produced it each verified their own step, and this asks the question of their composition.
func verifyFold(data, updated []byte, entry serverEntry) error {
	var before, after fileConfig
	if err := yaml.Unmarshal(data, &before); err != nil {
		return err
	}
	if err := yaml.Unmarshal(updated, &after); err != nil {
		return fmt.Errorf("the edited file would not parse: %w", err)
	}
	var lc legacyFileConfig
	if err := yaml.Unmarshal(updated, &lc); err != nil {
		return fmt.Errorf("the edited file would not parse: %w", err)
	}
	switch {
	case !lc.isEmpty():
		return errors.New("the edit would have left one of the retired keys behind")
	case !serversAppended(before.Servers, after.Servers, entry):
		return errors.New("the edit did not put the entry in servers: where a reader would look for it")
	case after.Server != entry.Name:
		return errors.New("the edit did not point server: at the entry")
	case !sameApartFrom(before, after, serversKey, serverKey):
		return errors.New("the edit would have changed more than servers: and server:")
	}
	return nil
}

// serversAppended reports whether after is exactly before plus entry, appended last — the only
// shape the fold may produce (hostsAppended's rule, one list over).
func serversAppended(before, after []serverEntry, entry serverEntry) bool {
	return len(after) == len(before)+1 &&
		slices.Equal(before, after[:len(before)]) &&
		after[len(after)-1] == entry
}

// legacyKeyLines is the 1-based line of each retired key the file actually sets — the lines the
// fold deletes. Each one must be a plain single-line `key: value`, checked with scalarLineParts for
// the reason a scalar edit checks it: a value spanning lines, or a block scalar, would leave its
// tail behind when the key's line goes.
//
// A file the sniff fired on but whose keys are not top-level settings is refused rather than
// half-folded: the two views of the file disagree, and the text is the one being rewritten.
func legacyKeyLines(root *yaml.Node, lines []string) ([]int, error) {
	var drop []int
	for _, legacy := range legacyKeys {
		keyNode, valueNode := mappingEntry(root, legacy.name)
		if keyNode == nil {
			continue
		}
		t := scalarTarget{key: legacy.name, keyNode: keyNode, valueNode: valueNode}
		if _, _, _, err := scalarLineParts(lines, t); err != nil {
			return nil, err
		}
		drop = append(drop, keyNode.Line)
	}
	if len(drop) == 0 {
		return nil, errors.New("the retired keys are not top-level settings of it")
	}
	return drop, nil
}

// serversInsertion is the lines the entry adds and the 1-based line to put them after — 0 for
// "append at the end". The four shapes are spliceHostAcknowledgement's, one list over: no
// `servers:` key (write the key and the entry under it, below the commented example that documents
// the list, ADR 0035); a list with entries (join it, at its own indentation); the bare key with
// nothing under it (start the list); and flow style, which has no line to append to and is refused.
//
// The example is measured to its END (commentedExampleBlockEnd, the same call a nested key's absent
// block makes) because a `servers:` example is several lines of commented list: landing the real
// block after the example's first line would wedge it into the middle of its own documentation.
func serversInsertion(root *yaml.Node, lines []string, entry serverEntry) ([]string, int, error) {
	keyNode, value := mappingEntry(root, serversKey)
	switch {
	case keyNode == nil:
		item, err := renderServerEntry(entry, listIndent)
		if err != nil {
			return nil, 0, err
		}
		at, err := commentedExampleBlockEnd(lines, serversKey)
		if err != nil {
			return nil, 0, err
		}
		return append([]string{serversKey + ":"}, item...), at, nil

	case value.Style&yaml.FlowStyle != 0:
		return nil, 0, errors.New("its servers: list is written in flow style ([...])")

	case value.Kind == yaml.SequenceNode && len(value.Content) > 0:
		item, err := renderServerEntry(entry, value.Column-1)
		if err != nil {
			return nil, 0, err
		}
		return item, maxNodeLine(value.Content[len(value.Content)-1]), nil

	case isNullNode(value):
		item, err := renderServerEntry(entry, listIndent)
		return item, keyNode.Line, err

	default:
		return nil, 0, errors.New("its servers: holds something other than a list of servers")
	}
}

// renderServerEntry renders the entry as one list item through the YAML marshaller — which owns the
// quoting, so no endpoint or api key can smuggle a syntax break into the file — indented to the
// given column. The optional fields are omitted when the legacy config did not set them
// (serverEntry's omitempty tags), so the migrated entry says exactly what the four keys said.
func renderServerEntry(entry serverEntry, indent int) ([]string, error) {
	out, err := yaml.Marshal([]serverEntry{entry})
	if err != nil {
		return nil, fmt.Errorf("render the migrated server entry: %w", err)
	}
	pad := strings.Repeat(" ", indent)
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		lines = append(lines, pad+l)
	}
	return lines, nil
}

// spliceFold applies the fold's deletions and its one insertion in a single pass over the original
// lines, so the line numbers the node tree reported all mean what they said — applying them one
// after another would shift every position the second edit was measured against. at is 1-based and
// names the line to insert after, or 0 to append the block at the end of the file.
func spliceFold(lines []string, drop []int, block []string, at int) ([]byte, error) {
	dropped := make(map[int]bool, len(drop))
	for _, n := range drop {
		if n < 1 || n > len(lines) {
			return nil, fmt.Errorf("one of the retired keys sits on line %d, which is outside it", n)
		}
		dropped[n] = true
	}
	if at < 0 || at > len(lines) {
		return nil, fmt.Errorf("the place for the servers: entry is line %d, which is outside it", at)
	}
	out := make([]string, 0, len(lines)+len(block))
	for i, line := range lines {
		if !dropped[i+1] {
			out = append(out, line)
		}
		if i+1 == at {
			out = append(out, block...)
		}
	}
	if at == 0 {
		out = appendBlock(out, block)
	}
	return joinConfigLines(out), nil
}

// backUpConfig copies the config's current bytes to a timestamped sibling and reports the file it
// wrote. It refuses to overwrite an existing backup (O_EXCL): the file it would replace is somebody
// else's copy of a config, which is the one thing this whole path exists to preserve. The original's
// permissions are carried over — a config may hold an api key, so its copy must not be more
// readable than it is.
func backUpConfig(path string, data []byte, now time.Time) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("apogee could not read its permissions (%v)", err)
	}
	backup := path + ".bak-" + now.Format(backupStampLayout)
	f, err := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return "", fmt.Errorf("the backup %s could not be written (%v)", backup, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return "", fmt.Errorf("the backup %s could not be written (%v)", backup, err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("the backup %s could not be written (%v)", backup, err)
	}
	if err := os.Chmod(backup, info.Mode().Perm()); err != nil {
		return "", fmt.Errorf("the backup %s could not be given the config's own permissions (%v)", backup, err)
	}
	return backup, nil
}

// migrationNote is the one startup line the migration announces itself with: which keys moved,
// what the entry they became is called, and where the file as it was is kept. It is the user's
// only notice that a file they own was rewritten, so it names the backup in full — that is the
// path they need if they disagree with any of it.
func migrationNote(path, backup string, entry serverEntry, lc legacyFileConfig) string {
	var moved []string
	for _, legacy := range legacyKeys {
		if strings.TrimSpace(legacy.value(lc)) != "" {
			moved = append(moved, legacy.name+":")
		}
	}
	return fmt.Sprintf("apogee: migrated %s to the servers: list — %s now describe the server %q, which "+
		"server: starts on; the file as it was is saved as %s",
		path, strings.Join(moved, ", "), entry.Name, backup)
}

// legacyRefusal is what a config in the retired schema gets when the fold cannot be made safely:
// the same refusal apogee gave before the rewrite existed, plus the reason it could not be done for
// them this time. Nothing has been written when this is returned, so the paste-able block is a
// complete answer on its own.
func legacyRefusal(path string, lc legacyFileConfig, why error) error {
	return fmt.Errorf("apogee: %s still uses the retired top-level endpoint:/api-key:/host-alias:/model: "+
		"keys — the servers: list is now the single definition of the servers you run models on.\n\n"+
		"apogee did not fold them in for you because %v.\n\n"+
		"Delete those keys and put this in their place:\n\n%s", path, why, lc.block())
}

// ----------------------------------------------------------------------------
// The retired top-level `llama-launcher:` key
// ----------------------------------------------------------------------------
//
// The launcher used to be one global setting: a top-level `llama-launcher:` key that turned the
// integration on for the whole session, whatever server it was talking to. It now belongs to the
// `servers:` entry the launcher fronts (serverEntry.LlamaLauncher), so /model offers launch
// profiles only while the session is ON that server and every other entry keeps the models it
// advertises.
//
// The key is refused rather than migrated, because the fold the quadruple gets cannot be written
// here: only the user knows WHICH entry the launcher starts servers for, and a config with three of
// them offers nothing to choose by. Silence is the one answer it must not get — fileConfig no
// longer has the field, so an unrefused key would simply stop being read, and the launcher commands
// would answer "not configured" on a machine whose config still asks for them (ADR 0036's
// refusal-over-silence posture, one key over).

// retiredLauncherKey is that key, spelled as the retired schema spelled it — and as serverEntry
// tags it one level down, which is the whole of what changed.
const retiredLauncherKey = "llama-launcher"

// legacyLauncherConfig reads the retired key off a file that still sets it, for the reason
// legacyFileConfig exists one struct over: fileConfig no longer has the field, so a plain unmarshal
// cannot tell a config that sets it from one that never did.
//
// The field is a yaml.Node rather than a string because the two questions asked of it come apart on
// a bare `llama-launcher:`: decoded into a string, that key is the same empty string an absent key
// decodes to, and the refusal would miss the very shape the old key's auto-detect wore. The node
// keeps its own existence — a null scalar the decoder still stores — separate from its text.
type legacyLauncherConfig struct {
	LlamaLauncher yaml.Node `yaml:"llama-launcher"`
}

// refuseRetiredLauncherKey stops a config that still carries the retired top-level key, with the
// line to delete and the entry to paste in its place. A file that does not set it returns nil and
// reaches the rest of the migration untouched — which is every config from here on.
//
// It runs before the quadruple fold reads anything, so a file carrying both retirements is refused
// with nothing written: no rewrite, and no backup either, since a copy is a write too.
func refuseRetiredLauncherKey(path string, data []byte) error {
	value, line, set := retiredLauncherSetting(data)
	if !set {
		return nil
	}
	where := ""
	if line > 0 {
		where = fmt.Sprintf(" on line %d", line)
	}
	// `off` was the old key's disabled spelling, and the per-entry key has no such value: absent IS
	// the off state (validateServers refuses one). So that config's fix is the deletion alone —
	// pasting the value back would hand the user a config the next launch refuses.
	if strings.EqualFold(value, "off") {
		return fmt.Errorf("apogee: %s still sets the retired top-level llama-launcher: key%s — the "+
			"launcher now belongs to the servers: entry it fronts, so it follows the session from server "+
			"to server.\n\n"+
			"Delete that line. An entry with no llama-launcher: key has the launcher off for that server, "+
			"which is what off said.", path, where)
	}
	return fmt.Errorf("apogee: %s still sets the retired top-level llama-launcher: key%s — the launcher "+
		"now belongs to the servers: entry it fronts, so /model offers its launch profiles only while the "+
		"session is on that server, and every other server keeps the models it advertises.\n\n"+
		"Delete that line and put the key on the entry the launcher starts servers for:\n\n%s",
		path, where, launcherEntryBlock(value))
}

// retiredLauncherSetting reports whether the file sets the retired key, the value it gives it, and
// the 1-based line the key sits on. Neither reader may answer the first question with the value: a
// key with no value at all is set, and an empty value is exactly what the old key's auto-detect
// shape looked like, so a config wearing it must be refused like any other.
//
// A file the tree cannot be read from — more than one document, a top level that is not a block
// mapping — falls back to the struct, whose yaml.Node field draws that same present/absent line,
// with 0 for "the line cannot be named". Only the line is lost to the fallback; whether the key is
// set decides a refusal, and that must not depend on the shape of the rest of the file.
func retiredLauncherSetting(data []byte) (value string, line int, set bool) {
	if doc, err := configDocument(data); err == nil {
		if root, err := rootMapping(doc); err == nil && root != nil {
			keyNode, valueNode := mappingEntry(root, retiredLauncherKey)
			if keyNode == nil {
				return "", 0, false
			}
			return strings.TrimSpace(valueNode.Value), keyNode.Line, true
		}
	}
	var llc legacyLauncherConfig
	if err := yaml.Unmarshal(data, &llc); err != nil {
		return "", 0, false
	}
	if llc.LlamaLauncher.IsZero() { // the zero node is the key the file never had
		return "", 0, false
	}
	return strings.TrimSpace(llc.LlamaLauncher.Value), 0, true
}

// launcherEntryBlock renders the fix as a whole `servers:` entry carrying the value the top-level
// key had — a paste rather than a schema lookup. An empty value becomes `auto`: the old key read
// the launcher's own default config when it was given nothing, and `auto` is what that shape is
// called now.
//
// The entry goes through renderServerEntry, the marshaller the fold writes with, which owns the
// quoting: a path that would not survive as a bare scalar comes back quoted rather than as an
// example that does not parse. The name and endpoint are the seeded template's example ones — the
// entry the user must edit is theirs, and this says what shape to give it.
func launcherEntryBlock(value string) string {
	if value == "" {
		value = "auto"
	}
	entry := serverEntry{Name: "workstation", Endpoint: "http://192.168.64.1:1111", LlamaLauncher: value}
	item, err := renderServerEntry(entry, listIndent)
	if err != nil {
		// Three strings cannot fail to marshal; if they ever do, the refusal still has to say what
		// the key is called and where it goes.
		return serversKey + ":\n" + strings.Repeat(" ", listIndent) + "- " + retiredLauncherKey + ": " + value + "\n"
	}
	return serversKey + ":\n" + strings.Join(item, "\n") + "\n"
}
