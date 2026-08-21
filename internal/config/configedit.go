package config

import (
	"fmt"
	"maps"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// ----------------------------------------------------------------------------
// The one config-write transaction
// ----------------------------------------------------------------------------
//
// Every edit apogee makes to config.yaml runs the same transaction, and it is written here once:
// seed the file from the embedded template if it is not there yet and read it, splice the text the
// caller located, re-parse the result, hold it against the config it started from, and replace the
// file atomically with its mode preserved — or refuse, leaving the file exactly as it was. That is
// configsplice.go's contract (ADR 0035) stated as a sequence rather than as a habit six writers
// were each trusted to keep.
//
// What a writer keeps for itself is a triple and nothing else: LOCATE — which key, entry or
// catalogue id it addresses, and what the file already says about it; SPLICE — the text it cuts in;
// VERIFY — the shape the result must have, in that writer's own wording. The ordering of those
// steps, and the decision whether anything is written at all, belong here, so a writer cannot get
// the order wrong and a change to the contract is one edit rather than six.
//
// There are two levels because two kinds of caller need them. [edit] is the whole transaction
// against a PATH, which is what a setting writer wants. [verifiedEdit] is the same splice-and-verify
// over BYTES already in hand, which is what the one-time legacy fold wants: it stacks its own splice
// under the scalar writer's before anything reaches the disk (configmigrate.go).
//
// One ordering is deliberate, and it is why the config the edit starts from is parsed the way it is
// below: a SPLICE's own shape refusal wins over the decoder's type error. A file the parser cannot
// read as settings at all — a top level that is a list, a block key holding a scalar — is better
// refused by the splice, which can say WHICH part of the file it could not read, than by yaml's
// "cannot unmarshal !!seq into config.fileConfig". So the starting config is parsed FIRST, because a
// splice needs it (what the file already says is what makes a re-set a no-op), but the parse's error
// is held back until the splice has had its say.

// editSplice cuts one writer's edit into the config text. It is handed the config as a reader parses
// it — what the file already says — and the raw bytes it splices, and returns the edited bytes.
// Returning nil bytes and no error means the file already says what was asked for: there is nothing
// to verify and nothing to write, and that is a confirmation rather than an error.
//
// The parsed config is the ZERO fileConfig when the file does not parse into settings at all, so a
// splice that reads it must be one whose own shape checks refuse such a file first — the ordering
// note above.
type editSplice func(before fileConfig, data []byte) ([]byte, error)

// editRead is the transaction's read step, named so a writer can bring its own. Almost every writer
// wants ReadConfigForWrite (configsplice.go), which seeds an absent file from the embedded template
// before reading it. The server-entry key-source edits want the read that deliberately does NOT seed
// (readConfigForEntryEdit, configwrite_keysource.go): they address an entry the file must already
// carry, and a seeded template names no server for them to rewrite.
type editRead func(path string) ([]byte, error)

// editVerify is one writer's gate: the config parsed before and after the splice, plus the edited
// bytes themselves, for the half of a check that must read the file the way any YAML reader sees it
// rather than through fileConfig. It refuses in the writer's own wording — the "edit the file by
// hand" idiom — and a refusal means nothing is written.
type editVerify func(before, after fileConfig, updated []byte) error

// edit runs one config-write transaction against the file at path: seed it from the embedded
// template if it is absent, read it, splice, verify, and replace it atomically with its mode
// preserved (a config may hold endpoint details, so a rewrite must never widen its permissions). A
// splice that reports nothing to do leaves the file untouched and reports no error.
//
// Anything the edit could not do surgically comes back qualified with the config's path — which is
// what a file-shape refusal needs. A caller's OWN checks — a key no surface may write, a value the
// key cannot hold — belong before this call, where they are refused without the file having been
// opened at all, so "refused" and "written" can never be the same outcome.
func edit(path string, splice editSplice, verify editVerify) error {
	return editFrom(path, ReadConfigForWrite, splice, verify)
}

// editFrom is edit with the read step the caller names — the seam a writer whose file must already
// exist reaches for (editRead above). Everything after the read is the same transaction, so which
// bytes an edit starts from is the only part of it a caller decides.
func editFrom(path string, read editRead, splice editSplice, verify editVerify) error {
	data, err := read(path)
	if err != nil {
		return err
	}
	updated, err := verifiedEdit(data, splice, verify)
	if err != nil {
		return fmt.Errorf("apogee: update config %q: %w", path, err)
	}
	if updated == nil {
		return nil
	}
	return writeConfigAtomically(path, updated)
}

// verifiedEdit is the middle of the transaction — parse, splice, re-parse, verify — over bytes a
// caller already holds. It returns the bytes that may be written, or nil bytes when the file
// already says what the edit asked for, so a caller cannot write a splice nothing checked.
func verifiedEdit(data []byte, splice editSplice, verify editVerify) ([]byte, error) {
	var before fileConfig
	// Held back on purpose: a splice's shape refusal names the part of the file it could not read,
	// and that is the better message. See the ordering note at the top of this file.
	parseErr := yaml.Unmarshal(data, &before)

	updated, err := splice(before, data)
	switch {
	case err != nil:
		return nil, err
	case updated == nil: // the file already says it: nothing to write, and nothing to verify
		return nil, nil
	case parseErr != nil:
		return nil, parseErr
	}

	var after fileConfig
	if err := yaml.Unmarshal(updated, &after); err != nil {
		return nil, fmt.Errorf("the edited file would not parse: %w", err)
	}
	if err := verify(before, after, updated); err != nil {
		return nil, err
	}
	return updated, nil
}

// ----------------------------------------------------------------------------
// The shape a verify step is written from
// ----------------------------------------------------------------------------
//
// A verify step asks one question — is this the config I started from, with exactly the one thing I
// asked for changed? — and the answer has two halves. The half every writer shares is sameApartFrom
// (configsplice.go): everything OUTSIDE the container being written compares equal, whole and field
// by field. The half below is the container's own, and there are three shapes of it, one per kind of
// place a setting lives: a scalar at a dotted path, an item in a list, a key in a map. Each reports
// whether the edited config is the original with that one place changed and nothing else moved —
// which is what catches a file shape the line arithmetic mis-read, since a line spliced into
// somebody else's block changes a setting nobody asked to touch.

// scalarChangedOnlyAt reports whether the edited bytes spell the dotted path exactly as the edit
// intended: set to want, or — for a reset — absent, with the key described by the binary's default
// again. It reads the value back the way any YAML reader sees it (scalarAtPath), because a splice
// must put the value where the PARSER looks for it, not merely somewhere that reads correctly in
// the text.
//
// It reports the read's own error rather than a false: bytes that reached this point re-parsed into
// settings once already, so a failure here is a file the comparison cannot be made over at all.
func scalarChangedOnlyAt(updated []byte, path, want string, set bool) (bool, error) {
	got, isSet, err := scalarAtPath(updated, path)
	if err != nil {
		return false, err
	}
	if !set {
		return !isSet, nil
	}
	return isSet && got == want, nil
}

// scalarAtPath reads the value a config file actually holds at a dotted path, the way any YAML
// reader sees it — the round-trip half of the verification. A path that is absent, or that runs
// through a value which is not a block, reports not-set.
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

// serversChangedOnlyAt reports whether after is before with the entry at index at replaced by want
// and nothing else moved — the shape a `servers:` entry splice must produce (serversAppended's rule,
// for an edit in place rather than an append). Entries are compared with reflect.DeepEqual because a
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

// mechanismsChangedOnlyAt reports whether after is before with id set to enabled and nothing else
// moved — the shape a mechanism splice must produce, which is serversChangedOnlyAt's rule over a map
// rather than a list.
func mechanismsChangedOnlyAt(before, after map[string]bool, id string, enabled bool) bool {
	want := maps.Clone(before)
	if want == nil {
		want = make(map[string]bool, 1)
	}
	want[id] = enabled
	return maps.Equal(after, want)
}
