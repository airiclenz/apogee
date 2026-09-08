package config

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/airiclenz/apogee/internal/domain"
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
// Two more retired keys ride along at the bottom of this file: the top-level `llama-launcher:` of
// ADR 0029 decision 4, which moved onto the `servers:` entries, and the global `model-profile:` of
// ADR 0044, which became the per-model `model-profiles:` map. Both are REFUSED rather than folded —
// nobody but the user knows which entry the launcher belonged to, or which models the profile was
// written for — and both are refused FIRST, so a file carrying several retirements is never
// rewritten for one and then stopped for another.

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
// Ahead of all three sit the retirements that are not migrations at all: the top-level
// `llama-launcher:` and `model-profile:` keys, refused before a single byte is read for the fold,
// so a config that carries more than one retired shape is stopped rather than half-rewritten.
//
// mayFoldReactions says whether this read is the STARTUP one, which may rewrite the file, or a live
// re-read under a running session, which may not: apogee never mutates a config file out from under
// a session, so a live re-read of a file still carrying `hooks:` refuses instead of folding it
// (ADR 0076 A6, the stage-2 plan's item 9 decision).
//
// now dates the backup and is injected so a test can name the file it expects.
func migrateLegacyConfig(path string, data []byte, now time.Time, mayFoldReactions bool) ([]byte, string, error) {
	if err := refuseRetiredLauncherKey(path, data); err != nil {
		return nil, "", err
	}
	if err := refuseRetiredProfileKey(path, data); err != nil {
		return nil, "", err
	}
	var lc legacyFileConfig
	if err := yaml.Unmarshal(data, &lc); err != nil {
		return nil, "", fmt.Errorf("apogee: parse config %q: %w", path, err)
	}
	rc, err := readLegacyReactions(data)
	if err != nil {
		return nil, "", reactionsRefusal(path, err)
	}
	if rc.hasHooks && rc.hasReactions {
		return nil, "", bothListsRefusal(path)
	}
	if !mayFoldReactions {
		if rc.hasHooks {
			return nil, "", liveReactionsRefusal(path)
		}
		// The retired `mechanisms:` key still parses, so a live re-read simply leaves it where it
		// is: the strip is a WRITE, and the only thing a session gains by it is a rewritten file.
		rc = legacyReactionsConfig{}
	}
	if lc.isEmpty() && !rc.needsRewrite() {
		return data, "", nil
	}

	// Both folds are applied to the bytes BEFORE anything is written, so a file carrying two
	// retired shapes is backed up once and rewritten once — two passes would leave the second
	// backup colliding with the first on the same second (backUpConfig is O_EXCL).
	updated, entry := data, ServerEntry{}
	if !lc.isEmpty() {
		updated, entry, err = foldLegacyKeys(updated, lc)
		if err != nil {
			return nil, "", legacyRefusal(path, lc, err)
		}
	}
	var fold reactionsFold
	if rc.needsRewrite() {
		updated, fold, err = foldLegacyReactions(updated)
		if err != nil {
			return nil, "", reactionsRefusal(path, err)
		}
	}

	// The backup is written only once the fold is verified, so a refusal leaves the apogee home
	// exactly as it found it — "no write at all" includes the copy.
	backup, err := backUpConfig(path, data, now)
	if err != nil {
		if lc.isEmpty() {
			return nil, "", reactionsRefusal(path, err)
		}
		return nil, "", legacyRefusal(path, lc, err)
	}
	if err := writeConfigAtomically(path, updated); err != nil {
		wrapped := fmt.Errorf("the rewrite could not be written (%v)", err)
		if lc.isEmpty() {
			return nil, "", reactionsRefusal(path, wrapped)
		}
		return nil, "", legacyRefusal(path, lc, wrapped)
	}

	var notes []string
	if !lc.isEmpty() {
		notes = append(notes, migrationNote(path, backup, entry, lc))
	}
	if rc.needsRewrite() {
		notes = append(notes, fold.note(path, backup))
	}
	return updated, strings.Join(notes, "\n"), nil
}

// foldLegacyKeys builds the migrated file: the four legacy lines removed, the entry they describe
// appended to `servers:`, and `server:` pointing at it. It returns the new bytes and the entry that
// was written, or an error naming what stopped it.
//
// It is the write transaction over bytes already in hand (verifiedEdit, configedit.go) rather than
// against a path, because the migration owns the steps around it: the backup its caller writes only
// once the fold is verified, and the read that has already happened by the time the sniff fired.
//
// The file it starts from is read as settings HERE, ahead of the transaction, so a file the parser
// cannot make settings of is refused in the migration's own words — that refusal is one paragraph
// telling a user what to fix by hand, and the transaction would otherwise hold the decoder's error
// back until its splice had spoken (configedit.go's ordering note).
func foldLegacyKeys(data []byte, lc legacyFileConfig) ([]byte, ServerEntry, error) {
	entry := ServerEntry{
		Name:     lc.name(),
		Endpoint: strings.TrimSpace(lc.Endpoint),
		APIKey:   strings.TrimSpace(lc.APIKey),
		Model:    strings.TrimSpace(lc.Model),
	}
	if err := yaml.Unmarshal(data, new(fileConfig)); err != nil {
		return nil, ServerEntry{}, fmt.Errorf("the rest of it does not parse into settings apogee can "+
			"read (%v)", err)
	}
	verify := func(before, after fileConfig, updated []byte) error {
		return verifyFold(before, after, updated, entry)
	}
	updated, err := verifiedEdit(data, foldSplice(entry), verify)
	switch {
	case err != nil:
		return nil, entry, err
	case updated == nil: // the pointer guard rules this out; a nil here would write the fold without it
		return nil, entry, errors.New("the edit did not add a server: line")
	}
	return updated, entry, nil
}

// foldSplice is the fold's own half of the transaction: the four retired lines cut out, the entry
// they describe inserted into `servers:`, and the `server:` pointer stacked under it by the ordinary
// scalar writer.
//
// Two shapes are refused before any line is moved, because neither can be folded without
// overwriting something the user wrote: a quadruple with no endpoint (those keys name no server on
// their own — there is nothing to move them INTO), and a name the list already uses (the fold would
// either collide with that entry or silently rename this one). A file that already sets `server:`
// is refused for the same reason: the pointer this writes would replace a startup choice the user
// made deliberately.
func foldSplice(entry ServerEntry) editSplice {
	return func(before fileConfig, data []byte) ([]byte, error) {
		switch {
		case entry.Endpoint == "":
			return nil, errors.New("there is no endpoint: among them, and a server is its endpoint — " +
				"the entry they would fold into could never be talked to")
		case slices.ContainsFunc(before.Servers, func(s ServerEntry) bool { return s.Name == entry.Name }):
			return nil, fmt.Errorf("its servers: list already has an entry called %q, and one name names "+
				"one server", entry.Name)
		case strings.TrimSpace(before.Server) != "":
			return nil, fmt.Errorf("it already starts on server: %q, which the fold would have to "+
				"repoint at the migrated entry", before.Server)
		}

		doc, err := Document(data)
		if err != nil {
			return nil, err
		}
		root, err := rootMapping(doc)
		if err != nil {
			return nil, err
		}
		if root == nil {
			return nil, errors.New("it holds no settings at all, so the keys are not where the parser found them")
		}
		lines := SplitConfigLines(data)
		drop, err := legacyKeyLines(root, lines)
		if err != nil {
			return nil, err
		}
		block, at, err := serversInsertion(root, lines, entry)
		if err != nil {
			return nil, err
		}
		folded, err := spliceFold(lines, drop, block, at)
		if err != nil {
			return nil, err
		}
		// The pointer rides the ordinary scalar writer: it is one `server: <name>` line, which is
		// exactly what that writer places (below the key's commented example, ADR 0035) and verifies.
		k, ok := LookupKey(serverKey)
		if !ok {
			return nil, errors.New("apogee has no server: setting to point at the entry")
		}
		return setScalarSetting(folded, k, entry.Name)
	}
}

// verifyFold is the gate the migration passes before anything reaches the disk: the rewritten file
// must carry the retired keys nowhere, must hold exactly the old `servers:` list plus this entry
// with `server:` naming it, and must agree with the original on every OTHER setting. It is the
// whole-file statement of "the original with exactly the quadruple folded" — the two splices that
// produced it each verified their own step, and this asks the question of their composition.
//
// The retired keys are read out of the edited BYTES, because they are what fileConfig has no field
// for: the parsed after says nothing about a `model:` the fold was supposed to take away.
func verifyFold(before, after fileConfig, updated []byte, entry ServerEntry) error {
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
		//nolint:staticcheck // ST1005: ends with the server: key's own colon by design.
		return errors.New("the edit would have changed more than servers: and server:")
	}
	return nil
}

// serversAppended reports whether after is exactly before plus entry, appended last — the only
// shape the fold may produce (hostsAppended's rule, one list over).
//
// The comparison goes through reflect.DeepEqual because ServerEntry stopped being comparable when it
// grew the sub-agent posture's `mechanisms:` map (ADR 0045): a struct holding a map cannot be `==`d.
// slices.EqualFunc still carries the head comparison so that a nil `before` and an empty one keep
// reading the same, which a bare DeepEqual over the two slices would not.
func serversAppended(before, after []ServerEntry, entry ServerEntry) bool {
	sameEntry := func(a, b ServerEntry) bool { return reflect.DeepEqual(a, b) }
	return len(after) == len(before)+1 &&
		slices.EqualFunc(before, after[:len(before)], sameEntry) &&
		sameEntry(after[len(after)-1], entry)
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
		t := ScalarTarget{Key: legacy.name, KeyNode: keyNode, ValueNode: valueNode}
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
func serversInsertion(root *yaml.Node, lines []string, entry ServerEntry) ([]string, int, error) {
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
// (ServerEntry's omitempty tags), so the migrated entry says exactly what the four keys said.
func renderServerEntry(entry ServerEntry, indent int) ([]string, error) {
	out, err := yaml.Marshal([]ServerEntry{entry})
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
		_ = f.Close()
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
func migrationNote(path, backup string, entry ServerEntry, lc legacyFileConfig) string {
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
// `servers:` entry the launcher fronts (ServerEntry.LlamaLauncher), so /model offers launch
// profiles only while the session is ON that server and every other entry keeps the models it
// advertises.
//
// The key is refused rather than migrated, because the fold the quadruple gets cannot be written
// here: only the user knows WHICH entry the launcher starts servers for, and a config with three of
// them offers nothing to choose by. Silence is the one answer it must not get — fileConfig no
// longer has the field, so an unrefused key would simply stop being read, and the launcher commands
// would answer "not configured" on a machine whose config still asks for them (ADR 0036's
// refusal-over-silence posture, one key over).

// retiredLauncherKey is that key, spelled as the retired schema spelled it — and as ServerEntry
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
	// the off state (ValidateServers refuses one). So that config's fix is the deletion alone —
	// pasting the value back would hand the user a config the next launch refuses.
	if strings.EqualFold(value, "off") {
		//nolint:staticcheck // ST1005: a refusal that closes with a paragraph break and the fix in prose.
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
	if doc, err := Document(data); err == nil {
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
	entry := ServerEntry{Name: "workstation", Endpoint: "http://192.168.64.1:1111", LlamaLauncher: value}
	item, err := renderServerEntry(entry, listIndent)
	if err != nil {
		// Three strings cannot fail to marshal; if they ever do, the refusal still has to say what
		// the key is called and where it goes.
		return serversKey + ":\n" + strings.Repeat(" ", listIndent) + "- " + retiredLauncherKey + ": " + value + "\n"
	}
	return serversKey + ":\n" + strings.Join(item, "\n") + "\n"
}

// ----------------------------------------------------------------------------
// The retired global `model-profile:` key
// ----------------------------------------------------------------------------
//
// The Model profile used to be one global block: a top-level `model-profile:` key that fixed the
// dialect for the whole session, whatever model was loaded. It is per-MODEL now (ADR 0044) —
// `model-profiles:` maps a pattern the model name contains to the profile it applies — and apogee
// ships the known shapes built in, so most configs need no entry at all.
//
// Like the launcher key one section up it is REFUSED rather than folded: only the user knows which
// models their block was written for, and a fold would have to invent that pattern. Silence is the
// one answer it must not get — fileConfig no longer has the field, so an unrefused key would simply
// stop being read and the session would go back to leaking thinking tags with nothing pointing at
// the block that was meant to strip them.

// retiredProfileKey is that key, spelled as the retired schema spelled it — and as the map's
// entries are spelled one level down, which is the whole of what changed.
const retiredProfileKey = "model-profile"

// profilesKey is the key that replaces it, spelled as fileConfig tags it.
const profilesKey = "model-profiles"

// legacyProfileConfig reads the retired key off a file that still sets it, for legacyLauncherConfig's
// reason one section up: fileConfig no longer has the field, so a plain unmarshal cannot tell a
// config that sets it from one that never did. The node keeps the block itself, which the refusal
// echoes back as the entry to paste.
type legacyProfileConfig struct {
	ModelProfile yaml.Node `yaml:"model-profile"`
}

// refuseRetiredProfileKey stops a config that still carries the retired global key, with the line to
// delete and the map spelling to paste in its place. A file that does not set it returns nil.
//
// It runs beside the launcher refusal, before the quadruple fold reads anything, so a file carrying
// more than one retirement is refused with nothing written.
func refuseRetiredProfileKey(path string, data []byte) error {
	block, line, set := retiredProfileSetting(data)
	if !set {
		return nil
	}
	where := ""
	if line > 0 {
		where = fmt.Sprintf(" on line %d", line)
	}
	// A bare `model-profile:` configured nothing in the first place — there is no block to move, so
	// pasting one back would hand the user a shape they never wrote.
	if block == "" {
		//nolint:staticcheck // ST1005: a refusal that closes with a paragraph break and the fix in prose.
		return fmt.Errorf("apogee: %s still sets the retired global %s: key%s — a profile belongs to a "+
			"MODEL now, so it is keyed by a pattern the model name contains: %s: {\"<pattern>\": {...}}.\n\n"+
			"Delete that line. With no block under it, it configured nothing.", path, retiredProfileKey, where,
			profilesKey)
	}
	return fmt.Errorf("apogee: %s still sets the retired global %s: key%s — a profile belongs to a MODEL "+
		"now, so it is keyed by a pattern the model name contains (a case-insensitive substring), and apogee "+
		"ships the shapes it knows built in.\n\n"+
		"Delete that block and put it back under a pattern that matches the model it was written for — or "+
		"delete it outright, if the built-in table already covers that model:\n\n%s",
		path, retiredProfileKey, where, profilesBlock(block))
}

// retiredProfileSetting reports whether the file sets the retired key, the block it gives it, and the
// 1-based line the key sits on — retiredLauncherSetting's three answers, read the same two ways and
// for the same reason: a key with no value at all is still set, and only the LINE is lost to the
// struct fallback.
func retiredProfileSetting(data []byte) (block string, line int, set bool) {
	if doc, err := Document(data); err == nil {
		if root, err := rootMapping(doc); err == nil && root != nil {
			keyNode, valueNode := mappingEntry(root, retiredProfileKey)
			if keyNode == nil {
				return "", 0, false
			}
			return renderNode(valueNode), keyNode.Line, true
		}
	}
	var lpc legacyProfileConfig
	if err := yaml.Unmarshal(data, &lpc); err != nil {
		return "", 0, false
	}
	if lpc.ModelProfile.IsZero() { // the zero node is the key the file never had
		return "", 0, false
	}
	return renderNode(&lpc.ModelProfile), 0, true
}

// renderNode marshals one value node back to YAML, so the refusal can echo the user's OWN block
// rather than a schema example they then have to translate. A node that will not marshal, and the
// empty value of a bare key, both come back empty — the shape whose fix is the deletion alone.
//
// The indent is the file's own (listIndent) rather than the marshaller's default four, because this
// text is written to be pasted back into a config the user hand-edits.
func renderNode(n *yaml.Node) string {
	if n == nil || isNullNode(n) {
		return ""
	}
	var out strings.Builder
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(listIndent)
	if err := encoder.Encode(n); err != nil {
		return ""
	}
	if err := encoder.Close(); err != nil {
		return ""
	}
	return strings.TrimRight(out.String(), "\n")
}

// profilesBlock renders the fix as a whole `model-profiles:` entry carrying the block the retired key
// had — a paste rather than a schema lookup. The pattern is a placeholder, because it is the one
// thing in the entry that is not already in the file: only the user knows which models the block was
// written for.
func profilesBlock(block string) string {
	indent := strings.Repeat(" ", listIndent)
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + indent + line
		}
	}
	return profilesKey + ":\n" + indent + "\"<pattern>\":\n" + strings.Join(lines, "\n") + "\n"
}

// ----------------------------------------------------------------------------
// The consented `sub-agents:` flag migration (ADR 0045)
// ----------------------------------------------------------------------------
//
// ADR 0045's first shape marked the Sub-agent server with a `sub-agents: true` flag on the entry
// itself. The root `sub-agents-server:` key replaced it, and the flag is now a key ServerEntry has
// no field for — so a config still carrying it reads without complaint and delegates nowhere the
// human meant. The start-up notices that and OFFERS the one edit that fixes it
// (cmd/apogee/keymigrate.go, internal/tui/keymigration.go).
//
// Everything above this line is the ONE write apogee makes unasked; this one is the opposite, and
// the difference is why it sits under its own heading rather than beside the fold. Nothing here runs
// on its own, there is no backup, and a config left alone keeps working exactly as it does today: it
// is ADR 0035's deliberate-edit grain, one answered question and one edit.
//
// Two halves are all the offer needs — which entries still carry the flag, and the single edit that
// drops every one of those lines and writes the root key naming the entry the human chose. Both read
// the RAW YAML, because a key nothing parses cannot be found in a parsed config.

// subAgentsKey is the retired per-entry flag, spelled as the ServerEntry field that used to carry it
// tagged it. Nothing decodes it any more: it is a name this file matches in the node tree.
const subAgentsKey = "sub-agents"

// retiredSubAgentsFlag is one entry still carrying the flag: the name the offer would write as the
// root key, and the two nodes the deletion measures its line from.
type retiredSubAgentsFlag struct {
	name  string
	key   *yaml.Node
	value *yaml.Node
}

// RetiredSubAgentsEntries names the `servers:` entries of the config at path that still spell
// `sub-agents: true`, in the file's own order — the whole of the start-up's detection. The first name
// is the one the offer asks about (the ratified call), and every one of them loses its flag line when
// the offer is taken.
//
// A config that is not there yet has nothing retired in it, which is an answer rather than a failure:
// no file, no names, no error. Anything else that stops the read — unreadable bytes, a file that is
// not one YAML document of settings — comes back as the error it is, and the caller raises no offer;
// a start-up question is never a reason a start fails.
func RetiredSubAgentsEntries(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("apogee: read config %q: %w", path, err)
	}
	flags, err := retiredSubAgentsFlags(data)
	if err != nil {
		return nil, fmt.Errorf("apogee: read config %q: %w", path, err)
	}
	names := make([]string, 0, len(flags))
	for _, flag := range flags {
		names = append(names, flag.name)
	}
	return names, nil
}

// retiredSubAgentsFlags finds those entries in the raw document. The value is DECODED rather than
// string-matched, so the flag is recognised in every spelling yaml itself reads as true — which is
// the only definition of it that was ever in force, since a bool field is what used to parse it.
//
// A nameless entry is skipped: the offer's whole answer is a name to write as the root key, and an
// entry that has none cannot be that answer. ValidateServers has already refused such a file by the
// time anything here runs, so this is the defensive half of a case that does not arise.
//
// Shapes it cannot read — a file that is not a mapping of settings, a `servers:` that is not a list —
// carry no flags rather than an error: the detection asks "is there something to offer", and the
// answer for a file this cannot read is no.
func retiredSubAgentsFlags(data []byte) ([]retiredSubAgentsFlag, error) {
	doc, err := Document(data)
	if err != nil {
		return nil, err
	}
	root, err := rootMapping(doc)
	if err != nil || root == nil {
		return nil, err
	}
	_, list := mappingEntry(root, serversKey)
	if list == nil || list.Kind != yaml.SequenceNode {
		return nil, nil
	}
	var flags []retiredSubAgentsFlag
	for _, item := range list.Content {
		keyNode, valueNode := mappingEntry(item, subAgentsKey)
		var on bool
		if keyNode == nil || valueNode == nil || valueNode.Decode(&on) != nil || !on {
			continue
		}
		_, nameNode := mappingEntry(item, entryNameKey)
		if nameNode == nil || strings.TrimSpace(nameNode.Value) == "" {
			continue
		}
		flags = append(flags, retiredSubAgentsFlag{
			name: strings.TrimSpace(nameNode.Value), key: keyNode, value: valueNode,
		})
	}
	return flags, nil
}

// MigrateSubAgentsServer is the edit the offer's "move it" row makes: every retired
// `sub-agents: true` line is deleted, and `sub-agents-server:` is written naming the entry the human
// answered with. ONE edit, because it is one answer to one question — a file left holding the flag
// AND the key would say two things about where delegations go, and a file that lost the flag without
// gaining the key would silently stop delegating anywhere.
//
// It is the ordinary write transaction (configedit.go) with the entry writers' read, which does not
// seed: the name must be an entry the file already carries, and a seeded template names none. The
// gate is stricter than a scalar write's for the reason the fold's is — two paths change at once, so
// it states BOTH of them and holds everything else byte-for-byte, including the `servers:` list,
// whose entries the deletion must leave exactly as a reader already saw them.
func MigrateSubAgentsServer(path, name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("apogee: cannot record the sub-agents server: no entry was named")
	}
	k, ok := LookupKey(subAgentsServerPath)
	if !ok {
		return errors.New("apogee has no sub-agents-server: setting to point at the entry")
	}
	splice := func(before fileConfig, data []byte) ([]byte, error) {
		if _, err := serverEntryAt(before, name); err != nil {
			return nil, err
		}
		dropped, err := dropRetiredSubAgentsFlags(data)
		if err != nil {
			return nil, err
		}
		base := data
		if dropped != nil {
			base = dropped
		}
		// The key rides the ordinary scalar writer over the bytes the deletion left, so it lands
		// below its own commented example (ADR 0035) and is verified as any other setting is. Its nil
		// — the file already names this entry — leaves the deletion standing as the whole edit, and
		// with nothing deleted either there is nothing to write at all.
		updated, err := setScalarSetting(base, k, name)
		if err != nil {
			return nil, err
		}
		if updated == nil {
			return dropped, nil
		}
		return updated, nil
	}
	verify := func(before, after fileConfig, updated []byte) error {
		return verifySubAgentsMigration(before, after, updated, name)
	}
	return editFrom(path, readConfigForEntryEdit, splice, verify)
}

// dropRetiredSubAgentsFlags returns the config text with every retired flag line removed, or nil
// bytes when it carries none — which the splice above reads as "this half changed nothing".
//
// Each line is read with scalarLineParts before it is deleted, exactly as the legacy fold reads the
// lines it drops: a value that runs past its key's line would leave its tail behind, and a text and
// a node tree that disagree about where the key sits would take somebody else's line. The lines are
// removed in ONE pass over the original, so every line number the node tree reported still means
// what it said (spliceFold's rule, with no insertion to make).
func dropRetiredSubAgentsFlags(data []byte) ([]byte, error) {
	flags, err := retiredSubAgentsFlags(data)
	if err != nil || len(flags) == 0 {
		return nil, err
	}
	lines := SplitConfigLines(data)
	drop := make(map[int]bool, len(flags))
	for _, flag := range flags {
		t := ScalarTarget{Key: subAgentsKey, Kind: KindBool, KeyNode: flag.key, ValueNode: flag.value}
		if _, _, _, err := scalarLineParts(lines, t); err != nil {
			return nil, err
		}
		drop[flag.key.Line] = true
	}
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		if !drop[i+1] {
			out = append(out, line)
		}
	}
	return joinConfigLines(out), nil
}

// verifySubAgentsMigration is the gate: the rewritten file must carry the retired flag nowhere, must
// name the chosen entry in `sub-agents-server:`, must hold the `servers:` list a reader already saw
// entry for entry, and must agree with the original on every other setting.
//
// The flags are read out of the edited BYTES, because they are what fileConfig has no field for — the
// parsed after says nothing about a `sub-agents:` line the deletion was supposed to take away. And
// the whole-file comparison zeroes BOTH changed paths (the fold's two-path precedent): the entry
// writers' single-path gate would refuse the root key as "more than the servers: list", and zeroing
// `servers:` is only safe here because the entries themselves are compared one line above.
func verifySubAgentsMigration(before, after fileConfig, updated []byte, name string) error {
	flags, err := retiredSubAgentsFlags(updated)
	switch {
	case err != nil:
		return fmt.Errorf("the edited file would not read back: %w", err)
	case len(flags) > 0:
		return errors.New("the edit would have left a retired sub-agents: flag behind; edit the file by hand")
	case after.SubAgentsServer != name:
		return fmt.Errorf("the edit did not point sub-agents-server: at %q where a reader would look "+
			"for it; edit the file by hand", name)
	case !sameServers(before.Servers, after.Servers):
		return errors.New("the edit would have changed the servers: list itself; edit the file by hand")
	case !sameApartFrom(before, after, serversKey, subAgentsServerPath):
		return errors.New("the edit would have changed more than the sub-agents: flag and " +
			"sub-agents-server:; edit the file by hand")
	}
	return nil
}

// sameServers reports whether two parsed `servers:` lists are the same list, entry for entry. It is
// serversAppended's comparison without the appended entry, and it goes through reflect.DeepEqual for
// serversAppended's reason: a ServerEntry holding a map cannot be `==`d.
func sameServers(before, after []ServerEntry) bool {
	return slices.EqualFunc(before, after, func(a, b ServerEntry) bool { return reflect.DeepEqual(a, b) })
}

// ----------------------------------------------------------------------------
// The `hooks:` fold and the retired `mechanisms:` key (ADR 0076 A6)
// ----------------------------------------------------------------------------
//
// `hooks:` was the earlier spelling of the user-origin observe lane. ADR 0076 gave that lane one
// name — `reactions:` — and A6 ruled out carrying the old key as an alias: one list, one spelling,
// and the file says which. So the block is FOLDED rather than refused, on the ADR 0036 decision 9
// idiom one key over: the entries are re-rendered under `reactions:` and spliced over the old
// block's line range, the previous bytes are kept as a timestamped sibling, and the change is
// announced once at startup.
//
// The fold is a rename plus three spelling changes and nothing else — `name:` → `id:`, `events:` →
// `on:`, `command:`/`webhook:` → `run:`, and the `approval-waiting` Moment under its ADR 0076 name
// `approval-requested` — so the verify below re-resolves BOTH sides to []domain.Reaction and
// refuses to write unless they are the same list. Comments written INSIDE the old block do not
// survive (the block is re-rendered, not edited line by line); the note says so and the backup
// keeps them.
//
// Riding along is the retired `mechanisms:` key, top-level and per-server. The catalogue it named
// is gone (ADR 0071) and the key arms nothing, so it is STRIPPED rather than refused: a saved
// configuration must not become a refusal, and a key that drives nothing must not keep looking as
// if it does. The successor table below turns each stripped id into the Floor key that governs its
// behaviour now, which is the whole of what the user needs to know.
//
// Both halves run ONLY at startup. A live re-read under a running session refuses instead
// (liveReactionsRefusal): apogee does not rewrite a config file out from under a session.

// retiredMechanismSuccessors maps each retired catalogue id that was PROMOTED to the top-level
// Floor-guard key that governs its behaviour now. It is the config migration table: a literal here
// rather than a lookup into internal/mechanisms, because the notice this feeds is the last thing
// the `mechanisms:` key is read for and it must outlive the package that carried the roll.
//
// An id absent from the map retired outright — there is nothing to point the user at, and the note
// says so once for the whole block rather than row by row.
var retiredMechanismSuccessors = map[string]string{
	"tool_loop_interceptor":    "tool-loop-breaker",
	"validate":                 "tool-call-repair",
	"empty_response_recovery":  "empty-response-recovery",
	"tool_use_enforcer":        "tool-use-enforcer",
	"cached_content_intercept": "read-cache",
	"tool_result_cap":          "tool-result-cap",
}

// hooksKey, reactionsKey and mechanismsKey are the three keys this migration reads and writes,
// spelled once so the sniff, the splice and the notes cannot drift apart. `hooks` is deliberately
// NOT a fileConfig tag any more — the schema has forgotten it, which is exactly why the sniff below
// reads it off the node tree instead.
const (
	hooksKey      = "hooks"
	reactionsKey  = "reactions"
	mechanismsKey = "mechanisms"
)

// blockSpan is one top-level or per-entry block the migration removes: the 1-based line range from
// its `key:` line through the last line of its value, and the value node itself for the callers
// that still need to read it. Ranges rather than single lines because these keys carry BLOCKS — a
// list of hook entries, a map of catalogue ids — and deleting the key line alone would leave the
// body behind as a syntax error.
type blockSpan struct {
	from  int
	to    int
	value *yaml.Node
}

// legacyReactionsConfig is the retired half of the schema this migration answers — and only that
// half: the `hooks:` block ADR 0076 renamed and the `mechanisms:` blocks ADR 0071 emptied. It
// exists for legacyFileConfig's reason one migration over: fileConfig no longer has a `hooks:`
// field, so the block would be silently unread, and `mechanisms:` still parses but drives nothing.
//
// Nothing resolves from it. Its only job is to answer "does this file still carry either shape, and
// where do those lines start and end" — the two facts the fold and the strip both need.
type legacyReactionsConfig struct {
	hasHooks         bool
	hasReactions     bool
	hooks            *blockSpan
	mechanisms       *blockSpan
	serverMechanisms []blockSpan
}

// needsRewrite reports whether the file carries anything this migration has to take out of it.
func (rc legacyReactionsConfig) needsRewrite() bool {
	return rc.hooks != nil || rc.mechanisms != nil || len(rc.serverMechanisms) > 0
}

// readLegacyReactions locates the retired blocks in the file's node tree. The error it reports is
// the one shape a fold cannot be attempted on at all: a top level the splice cannot read — a flow
// mapping, a list, a scalar — in a file that still carries `hooks:`. Silence is the answer that
// must not be given there, because fileConfig has no `hooks:` field any more and an unfolded block
// would simply stop being read (ADR 0036's refusal-over-silence posture).
//
// A file that does not parse as YAML at all is left to the decoder: the caller reads it next and
// reports the parse error in the words every other malformed config gets.
func readLegacyReactions(data []byte) (legacyReactionsConfig, error) {
	doc, err := Document(data)
	if err != nil {
		return legacyReactionsConfig{}, nil
	}
	root, rootErr := rootMapping(doc)
	if rootErr != nil {
		if carriesTopLevelKey(data, hooksKey) {
			return legacyReactionsConfig{}, rootErr
		}
		return legacyReactionsConfig{}, nil
	}
	if root == nil {
		return legacyReactionsConfig{}, nil
	}

	var rc legacyReactionsConfig
	rc.hooks = spanOf(root, hooksKey)
	rc.mechanisms = spanOf(root, mechanismsKey)
	rc.hasHooks = rc.hooks != nil
	_, reactionsValue := mappingEntry(root, reactionsKey)
	rc.hasReactions = reactionsValue != nil && !isNullNode(reactionsValue)

	if _, servers := mappingEntry(root, serversKey); servers != nil && servers.Kind == yaml.SequenceNode {
		for _, entry := range servers.Content {
			if entry.Kind != yaml.MappingNode {
				continue
			}
			if span := spanOf(entry, mechanismsKey); span != nil {
				rc.serverMechanisms = append(rc.serverMechanisms, *span)
			}
		}
	}
	return rc, nil
}

// carriesTopLevelKey reports whether the file sets key at its top level, read WITHOUT the node tree
// — the one question still answerable about a file whose shape the splice cannot work in.
func carriesTopLevelKey(data []byte, key string) bool {
	var top map[string]yaml.Node
	if err := yaml.Unmarshal(data, &top); err != nil {
		return false
	}
	_, ok := top[key]
	return ok
}

// spanOf is the line range key covers inside mapping, or nil when the mapping does not set it. The
// end is the last line any part of the value sits on, so a block value is removed whole.
func spanOf(mapping *yaml.Node, key string) *blockSpan {
	keyNode, value := mappingEntry(mapping, key)
	if keyNode == nil {
		return nil
	}
	to := keyNode.Line
	if value != nil {
		if last := maxNodeLine(value); last > to {
			to = last
		}
	}
	return &blockSpan{from: keyNode.Line, to: to, value: value}
}

// reactionsFold is what one run of this migration did, in the terms the startup note is written
// from: how many `hooks:` entries were folded, whether any of them named the retired
// `approval-waiting` spelling, and which retired catalogue ids were stripped.
type reactionsFold struct {
	folded          int
	renamedApproval bool
	strippedIDs     []string
}

// foldLegacyReactions builds the migrated file: the `hooks:` block re-rendered as `reactions:` in
// its own place, and every `mechanisms:` block — the top-level one and each server's — removed. It
// returns the new bytes and what it did, or an error naming what stopped it.
//
// It is the write transaction over bytes already in hand (verifiedEdit, configedit.go) rather than
// against a path, for foldLegacyKeys' reason: the migration owns the backup and the write around it.
func foldLegacyReactions(data []byte) ([]byte, reactionsFold, error) {
	entries, err := readLegacyHooks(data)
	if err != nil {
		return nil, reactionsFold{}, err
	}
	want, err := toLegacyReactions(entries)
	if err != nil {
		return nil, reactionsFold{}, err
	}

	var fold reactionsFold
	fold.folded = len(entries)
	for _, entry := range entries {
		if slices.Contains(entry.Events, retiredApprovalEvent) {
			fold.renamedApproval = true
		}
	}

	splice := func(before fileConfig, data []byte) ([]byte, error) {
		return spliceReactionsFold(data, entries, &fold)
	}
	verify := func(before, after fileConfig, updated []byte) error {
		return verifyReactionsFold(before, after, updated, want)
	}
	updated, err := verifiedEdit(data, splice, verify)
	switch {
	case err != nil:
		return nil, reactionsFold{}, err
	case updated == nil:
		return nil, reactionsFold{}, errors.New("the edit changed nothing")
	}
	return updated, fold, nil
}

// spliceReactionsFold does the line work in a SINGLE pass over the original lines, so every line
// number the node tree reported still means what it said — applying the deletions one after another
// would shift every position the next one was measured against.
//
// The rendered `reactions:` block goes in where `hooks:` began, so an entry keeps the place in the
// file the user put it in and whatever comments they wrote ABOVE the block still introduce it.
func spliceReactionsFold(data []byte, entries []legacyHookConfig, fold *reactionsFold) ([]byte, error) {
	rc, err := readLegacyReactions(data)
	if err != nil {
		return nil, err
	}
	lines := SplitConfigLines(data)

	dropped := make(map[int]bool)
	drop := func(span *blockSpan) error {
		if span == nil {
			return nil
		}
		if span.from < 1 || span.to > len(lines) || span.to < span.from {
			return fmt.Errorf("one of the retired blocks covers lines %d-%d, which is outside it",
				span.from, span.to)
		}
		for line := span.from; line <= span.to; line++ {
			dropped[line] = true
		}
		return nil
	}
	if err := drop(rc.hooks); err != nil {
		return nil, err
	}
	if err := drop(rc.mechanisms); err != nil {
		return nil, err
	}
	for i := range rc.serverMechanisms {
		if err := drop(&rc.serverMechanisms[i]); err != nil {
			return nil, err
		}
	}
	fold.strippedIDs = strippedMechanismIDs(rc)

	var block []string
	at := 0
	if rc.hooks != nil {
		if block, err = renderReactionsBlock(entries); err != nil {
			return nil, err
		}
		at = rc.hooks.from
	}

	out := make([]string, 0, len(lines)+len(block))
	for i, line := range lines {
		if i+1 == at {
			out = append(out, block...)
		}
		if !dropped[i+1] {
			out = append(out, line)
		}
	}
	return joinConfigLines(out), nil
}

// strippedMechanismIDs is every catalogue id the strip took out, from the top-level block and the
// per-server ones alike, deduplicated and sorted. Sorted because the ids come off maps, and a note
// that reordered between runs would make the one line a user sees unstable.
func strippedMechanismIDs(rc legacyReactionsConfig) []string {
	seen := make(map[string]bool)
	collect := func(span *blockSpan) {
		if span == nil || span.value == nil {
			return
		}
		var ids map[string]bool
		if err := span.value.Decode(&ids); err != nil {
			return
		}
		for id := range ids {
			seen[id] = true
		}
	}
	collect(rc.mechanisms)
	for i := range rc.serverMechanisms {
		collect(&rc.serverMechanisms[i])
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// verifyReactionsFold is the gate this migration passes before anything reaches the disk: the
// rewritten file must carry neither retired key anywhere, its `reactions:` block must resolve to
// exactly the list the `hooks:` block resolved to, and it must agree with the original on every
// OTHER setting.
//
// The retired keys are read out of the edited BYTES, because they are what fileConfig has no field
// for: the parsed after says nothing about a `hooks:` the fold was supposed to take away. The
// servers are compared with their `mechanisms:` maps blanked on both sides, for the same reason —
// that map is what the strip removes, and sameApartFrom cannot reach inside a list.
func verifyReactionsFold(before, after fileConfig, updated []byte, want []domain.Reaction) error {
	rc, err := readLegacyReactions(updated)
	if err != nil {
		return fmt.Errorf("the edited file is no longer a mapping of settings: %w", err)
	}
	got, err := toReactions(after.Reactions)
	if err != nil {
		return fmt.Errorf("the folded reactions: block would not resolve: %w", err)
	}
	switch {
	case rc.needsRewrite():
		return errors.New("the edit would have left one of the retired blocks behind")
	case !reflect.DeepEqual(got, want):
		return errors.New("the folded reactions: block does not fire what the hooks: block fired")
	case !sameApartFrom(withoutServerMechanisms(before), withoutServerMechanisms(after),
		reactionsKey, mechanismsKey):
		//nolint:staticcheck // ST1005: ends with the mechanisms: key's own colon by design.
		return errors.New("the edit would have changed more than hooks:, reactions: and mechanisms:")
	}
	return nil
}

// withoutServerMechanisms copies fc with every server entry's `mechanisms:` map blanked, so the
// whole-file comparison can ask its question about the settings the strip does NOT touch. The
// copy is deep enough to leave the caller's own parsed config alone: the slice is rebuilt rather
// than written through.
func withoutServerMechanisms(fc fileConfig) fileConfig {
	if len(fc.Servers) == 0 {
		return fc
	}
	servers := make([]ServerEntry, len(fc.Servers))
	copy(servers, fc.Servers)
	for i := range servers {
		servers[i].Mechanisms = nil
	}
	fc.Servers = servers
	return fc
}

// ----------------------------------------------------------------------------
// The retired `hooks:` entry shape
// ----------------------------------------------------------------------------

// retiredApprovalEvent is the spelling the approval notice carried before ADR 0076 A6 renamed it —
// `approval-` joined to `waiting`, built rather than written whole so the rename's own sweep for
// the retired name stays clean. It is read here and nowhere else: the fold rewrites it, and after
// the rewrite nothing in apogee accepts it again.
var retiredApprovalEvent = "approval-" + "waiting"

// legacyHookConfig is the on-disk shape of one `hooks:` entry, kept alive HERE and only here for
// legacyFileConfig's reason: the schema has forgotten the key, so the migration is the last reader
// of it and owns the spellings it has to map across.
type legacyHookConfig struct {
	Name       string            `yaml:"name"`
	Events     []string          `yaml:"events"`
	Command    []string          `yaml:"command"`
	Webhook    string            `yaml:"webhook"`
	Headers    map[string]string `yaml:"headers"`
	HeadersEnv map[string]string `yaml:"headers-env"`
	Workspace  string            `yaml:"workspace"`
	Timeout    string            `yaml:"timeout"`
}

// readLegacyHooks decodes the `hooks:` block off the file's node tree. It goes through the node
// rather than a struct tag because fileConfig no longer has that tag and this package's own
// acceptance forbids reintroducing it — the key exists in exactly one place now, the const above.
func readLegacyHooks(data []byte) ([]legacyHookConfig, error) {
	doc, err := Document(data)
	if err != nil {
		return nil, err
	}
	root, err := rootMapping(doc)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, errors.New("it holds no settings at all, so the block is not where the parser found it")
	}
	_, value := mappingEntry(root, hooksKey)
	if value == nil || isNullNode(value) {
		return nil, nil
	}
	var entries []legacyHookConfig
	if err := value.Decode(&entries); err != nil {
		return nil, fmt.Errorf("its hooks: block is not a list of hook entries (%v)", err)
	}
	return entries, nil
}

// toLegacyReactions resolves the retired block to the Reactions it fired, which is the BEFORE side
// of the fold's verify. It maps the one entry shape the old schema had onto the reactionConfig the
// new one has and lets that mapping do the work, so the two sides of the comparison cannot disagree
// about what a `timeout:` defaults to or how a `workspace:` is spelled.
func toLegacyReactions(entries []legacyHookConfig) ([]domain.Reaction, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	mapped := make([]reactionConfig, 0, len(entries))
	for _, entry := range entries {
		converted, err := entry.toReactionConfig()
		if err != nil {
			return nil, err
		}
		mapped = append(mapped, converted)
	}
	return toReactions(mapped)
}

// toReactionConfig is the fold itself, one entry at a time: `name:` becomes `id:`, `events:`
// becomes `on:` with the retired approval spelling rewritten, and the two sibling action keys
// become the one `run:` the new schema has. It refuses an entry that spells both actions or
// neither — the two faults a one-handler value cannot express, and the only rules the old schema
// had that the new one does not.
func (h legacyHookConfig) toReactionConfig() (reactionConfig, error) {
	on := make([]string, 0, len(h.Events))
	for _, event := range h.Events {
		if event == retiredApprovalEvent {
			event = string(domain.MomentApprovalRequested)
		}
		on = append(on, event)
	}

	run, err := h.run()
	if err != nil {
		return reactionConfig{}, err
	}
	return reactionConfig{
		ID:        h.Name,
		On:        on,
		Run:       run,
		Workspace: h.Workspace,
		Timeout:   h.Timeout,
	}, nil
}

// run turns the one action the entry spells into the `run:` value that replaces it: the argv list
// stays a list, and the webhook's URL and its two header maps become the mapping the new schema
// documents. The shapes are the plain []any / map[string]any the decoder would have produced, so
// the converted entry is indistinguishable from one the user wrote by hand.
func (h legacyHookConfig) run() (any, error) {
	hasCommand, hasWebhook := len(h.Command) > 0, strings.TrimSpace(h.Webhook) != ""
	switch {
	case hasCommand && hasWebhook:
		return nil, fmt.Errorf("its hook %q sets both command: and webhook:, and an entry takes exactly one",
			h.Name)
	case !hasCommand && !hasWebhook:
		return nil, fmt.Errorf("its hook %q sets neither command: nor webhook:, and an entry takes exactly one",
			h.Name)
	case hasCommand:
		if len(h.Headers) > 0 || len(h.HeadersEnv) > 0 {
			return nil, fmt.Errorf("its hook %q carries headers on a command:, and those belong to a webhook",
				h.Name)
		}
		argv := make([]any, 0, len(h.Command))
		for _, word := range h.Command {
			argv = append(argv, word)
		}
		return argv, nil
	default:
		run := map[string]any{"url": h.Webhook}
		if len(h.Headers) > 0 {
			run["headers"] = anyMap(h.Headers)
		}
		if len(h.HeadersEnv) > 0 {
			run["headers-env"] = anyMap(h.HeadersEnv)
		}
		return run, nil
	}
}

// anyMap widens a header map to the shape the decoder hands the new schema, so the converted entry
// and a hand-written one are the same value.
func anyMap(in map[string]string) map[string]any {
	out := make(map[string]any, len(in))
	for name, value := range in {
		out[name] = value
	}
	return out
}

// ----------------------------------------------------------------------------
// Rendering the `reactions:` block
// ----------------------------------------------------------------------------

// renderedReaction is what one folded entry looks like written down. The field ORDER is the order
// the key is documented in — id, on, run, workspace, timeout — because this struct IS the renderer,
// and the omitempty tags keep an entry saying exactly what the old one said.
type renderedReaction struct {
	ID        string    `yaml:"id"`
	On        flowWords `yaml:"on,flow"`
	Run       any       `yaml:"run"`
	Workspace string    `yaml:"workspace,omitempty"`
	Timeout   string    `yaml:"timeout,omitempty"`
}

// renderedWebhook is the `run:` mapping written down, with `url:` first because that is the line a
// reader is looking for. A struct rather than the map the converter builds, so the three keys keep
// the order the schema documents them in instead of the marshaller's alphabetical one.
type renderedWebhook struct {
	URL        string            `yaml:"url"`
	Headers    map[string]string `yaml:"headers,omitempty"`
	HeadersEnv map[string]string `yaml:"headers-env,omitempty"`
}

// flowWords is a word list rendered on one line — `[a, b]` — which is how both the template and the
// manual write `on:` and an argv `run:`. It is a type rather than the `flow` tag alone because the
// argv rides an `any` field, where a struct tag has nothing to attach to.
type flowWords []string

// MarshalYAML renders the list as a flow sequence of STRINGS, tagged so a word like `true` or `on`
// comes back out of the file as the word the user wrote rather than as a boolean.
func (w flowWords) MarshalYAML() (any, error) {
	node := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	for _, word := range w {
		node.Content = append(node.Content, &yaml.Node{
			Kind: yaml.ScalarNode, Tag: "!!str", Value: word,
		})
	}
	return node, nil
}

// renderReactionsBlock renders the whole block — the `reactions:` key and its entries — through the
// YAML marshaller, which owns the quoting, so no command word, URL or header value can smuggle a
// syntax break into the file. An empty block renders as the bare key, which is what an empty
// `hooks:` block already was.
func renderReactionsBlock(entries []legacyHookConfig) ([]string, error) {
	if len(entries) == 0 {
		return []string{reactionsKey + ":"}, nil
	}
	rendered := make([]renderedReaction, 0, len(entries))
	for _, entry := range entries {
		converted, err := entry.toReactionConfig()
		if err != nil {
			return nil, err
		}
		rendered = append(rendered, renderedReaction{
			ID:        converted.ID,
			On:        converted.On,
			Run:       renderedRun(entry, converted.Run),
			Workspace: converted.Workspace,
			Timeout:   converted.Timeout,
		})
	}
	out, err := yaml.Marshal(rendered)
	if err != nil {
		return nil, fmt.Errorf("render the migrated reactions: block: %w", err)
	}
	pad := strings.Repeat(" ", listIndent)
	lines := []string{reactionsKey + ":"}
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		lines = append(lines, pad+unquoteOnKey(line))
	}
	return lines, nil
}

// renderedRun is the `run:` value in the shape the file is written in: the argv on one line, the
// webhook as the three-key mapping the schema documents. The CONVERTED value is what the verify
// compares, so this only ever changes how the same value looks written down.
func renderedRun(entry legacyHookConfig, run any) any {
	if _, isWebhook := run.(map[string]any); !isWebhook {
		return flowWords(entry.Command)
	}
	return renderedWebhook{URL: entry.Webhook, Headers: entry.Headers, HeadersEnv: entry.HeadersEnv}
}

// unquoteOnKey writes `on:` the way the schema documents it. The marshaller quotes that key because
// YAML 1.1 reads a bare `on` as a boolean; the decoder reads both spellings back as the same key, so
// this is purely about handing the user a block that matches the one in their own template.
func unquoteOnKey(line string) string {
	const quoted = `"on":`
	trimmed := strings.TrimLeft(line, " ")
	if !strings.HasPrefix(trimmed, quoted) {
		return line
	}
	indent := len(line) - len(trimmed)
	return line[:indent] + "on:" + trimmed[len(quoted):]
}

// ----------------------------------------------------------------------------
// What the user is told
// ----------------------------------------------------------------------------

// note is the one startup line this migration announces itself with: what was folded, what was
// stripped, that the block's own comments are only in the backup now, and where that backup is. It
// is the user's only notice that a file they own was rewritten, so it names the backup in full —
// that is the path they need if they disagree with any of it.
//
// The closing sentence is for the SCRIPTS an entry runs: the environment names and the payload's
// own key changed with the rename, and a notifier that reads the old ones would go quiet without
// ever failing. It is said only when a block was actually folded, since a strip changes nothing a
// script can see.
func (f reactionsFold) note(path, backup string) string {
	var parts []string
	if f.folded > 0 || f.renamedApproval {
		parts = append(parts, fmt.Sprintf("hooks: became reactions: (%d entries)", f.folded))
	}
	if f.renamedApproval {
		parts = append(parts, retiredApprovalEvent+" is now "+string(domain.MomentApprovalRequested))
	}
	if len(f.strippedIDs) > 0 {
		parts = append(parts, "the retired mechanisms: key was dropped ("+f.successorHint()+")")
	}
	parts = append(parts, "comments inside the old block did not survive")

	note := fmt.Sprintf("apogee: rewrote %s — %s; backup at %s.",
		path, strings.Join(parts, "; "), backup)
	if f.folded > 0 {
		note += ` Scripts must read APOGEE_REACTION_* (was APOGEE_HOOK_*) and the payload's ` +
			`"reaction" field (was "hook").`
	}
	return note
}

// successorHint names, for each stripped id that was PROMOTED, the top-level Floor key that governs
// its behaviour now — the one thing a user losing that line actually needs. When none of them was,
// there is nothing to point at and the hint says so instead of listing nothing.
func (f reactionsFold) successorHint() string {
	var moved []string
	for _, id := range f.strippedIDs {
		if successor := retiredMechanismSuccessors[id]; successor != "" {
			moved = append(moved, id+" → "+successor+":")
		}
	}
	if len(moved) == 0 {
		return "the catalogue is empty; the seven Floor keys are the only switches"
	}
	return strings.Join(moved, ", ")
}

// bothListsRefusal is what a file carrying BOTH lists gets. Nothing has been written when this is
// returned: two lists under two names is a hand migration in progress, and finishing it for the
// user would either duplicate an id or reorder a lane they were arranging deliberately.
func bothListsRefusal(path string) error {
	return fmt.Errorf("apogee: %s has both hooks: and reactions: — reactions: is the single list "+
		"(hooks: was its earlier name).\n\n"+
		"apogee did not fold hooks: in for you because reactions: already exists.\n\n"+
		"Move the hooks: entries into reactions: (name: → id:, events: → on:, command: or webhook: "+
		"→ run:) and delete hooks:.", path)
}

// reactionsRefusal is what the fold gives when it cannot be made safely: nothing has been written,
// and the paste-able instruction is a complete answer on its own.
func reactionsRefusal(path string, why error) error {
	return fmt.Errorf("apogee: %s still uses the retired hooks: or mechanisms: keys — reactions: is "+
		"the single observe list, and the mechanism catalogue is gone.\n\n"+
		"apogee did not rewrite it for you because %v.\n\n"+
		"Move the hooks: entries into reactions: (name: → id:, events: → on:, command: or webhook: "+
		"→ run:) and delete hooks: and mechanisms: by hand.", path, why)
}

// liveReactionsRefusal is what a LIVE re-read of a file still carrying `hooks:` gets. The fold is a
// startup act and only a startup act: apogee does not rewrite a config file out from under a
// running session, so the re-read refuses, writes nothing, and leaves the session firing the list
// it already had.
func liveReactionsRefusal(path string) error {
	return fmt.Errorf("apogee: %s still has a hooks: block, and apogee does not rewrite a config "+
		"file while a session is running — hooks: becomes reactions: at startup. Restart apogee to "+
		"let it fold the block in, or move the entries into reactions: yourself (name: → id:, "+
		"events: → on:, command: or webhook: → run:).", path)
}
