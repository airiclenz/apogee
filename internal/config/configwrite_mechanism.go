package config

import (
	"errors"
	"fmt"
	"strings"
)

// ----------------------------------------------------------------------------
// The per-mechanism writer (the /settings mechanisms sub-list's half)
// ----------------------------------------------------------------------------
//
// Toggling one catalogued Mechanism is one line written into the `mechanisms:` block of the user's
// own file, and it is written under the contract every other writer here holds to (ADR 0035,
// configsplice.go): parse for positions, splice text, re-parse and compare before anything reaches
// the disk, so every comment, key order and indentation of theirs comes back exactly as found.
//
// What is new is the ADDRESSING, and it is the first that stands OUTSIDE the registry. A scalar
// edit names its target by registry path and the registry describes one row per schema key; the
// `mechanisms:` block is a single structured row, because its children are not a schema at all —
// they are the Mechanism catalogue's ids, which the binary knows (mechanisms.KnownIDs()) and this
// package does not. So this writer is addressed by (id, value), and WHICH ids exist stays the
// caller's question, the same division mechanisms.ResolveEnabled already keeps. What the writer
// checks for itself is narrower and is about the FILE: an id it could not spell as a plain key is
// refused rather than quoted into a line a reader would take back out under a different name.
//
// Toggling a Mechanism off writes `<id>: false` rather than removing the line. The file records
// what the user decided, and an explicit "off" is a decision where an absent key is only the
// default — and the difference is load-bearing, since a non-empty `mechanisms:` block means manual
// control and a Validated set matching the model is then not auto-applied (ADR 0016). A writer that
// deleted the last line would hand that decision back without being asked to.

// mechanismsKey is the top-level config key the manual Mechanism block lives under — the same
// spelling as fileConfig's yaml tag, named here because the writer matches it in the node tree.
const mechanismsKey = "mechanisms"

// SaveMechanismSetting writes `<id>: <enabled>` into the `mechanisms:` block of the config file at
// path, and reports nothing when the file already spells that line (a re-set is a confirmation, not
// a rewrite). A file with no block yet gets one, created under the commented example the seeded
// template documents it with — ADR 0035's insert-below-example call, so a user meeting the key for
// the first time finds it beneath the paragraph explaining it.
//
// An absent config is seeded from the embedded template first, and the write is atomic and
// mode-preserving: the contract configwrite.go's acknowledgement writer and configwrite_scalar.go's
// scalar writer are under, unchanged. Anything the edit cannot do surgically — an id the file
// cannot spell as a plain key, a `mechanisms:` written in flow style or holding something other
// than a block, a result that would no longer parse or that moved a setting nobody asked it to
// touch — is refused with the "by hand" idiom, and the file is left exactly as it was.
func SaveMechanismSetting(path, id string, enabled bool) error {
	name, err := writableMechanismID(id)
	if err != nil {
		return err
	}
	splice, verify := mechanismEdit(name, enabled)
	return edit(path, splice, verify)
}

// writableMechanismID refuses an id this writer cannot put in the file as a plain key, BEFORE the
// config is opened — so "refused" and "written" can never be the same outcome. A catalogue id is an
// identifier (`syntax`), and an id a YAML reader would take back out under a different
// name — one needing quotes, one spanning lines, an empty one — would leave the block naming a
// Mechanism nobody enabled while the one that was asked for stayed off.
func writableMechanismID(id string) (string, error) {
	name := strings.TrimSpace(id)
	if name == "" {
		return "", errors.New("apogee: cannot write a mechanism setting: no mechanism id was given")
	}
	text, err := renderScalar(name)
	if err != nil {
		return "", fmt.Errorf("apogee: cannot write the mechanism %q: %w", id, err)
	}
	if text != name {
		return "", fmt.Errorf(
			"apogee: %q is not a mechanism id apogee can write as a plain mechanisms: key; "+
				"add the line to config.yaml by hand", id)
	}
	return name, nil
}

// mechanismEdit states one mechanism line as the write transaction's two halves (configedit.go):
// the splice that puts `<id>: <enabled>` inside the `mechanisms:` block, and the gate the result
// must pass. An id the block does not NAME is always written, `false` included — an absent key and
// an explicit `false` leave the Mechanism in the same state but not the file in the same one, since
// a block with a line in it is manual control (ADR 0016) — while an id the block already spells that
// way is nothing to write at all.
//
// The splice is the scalar writer's (spliceScalarSet): a mechanism line is a nested `key: value`
// like any other, so the shapes it must meet — no block at all, a bare `mechanisms:`, a populated
// block, and a line already there — are the ones scalarInsertion already reads out of the file.
func mechanismEdit(id string, enabled bool) (editSplice, editVerify) {
	splice := func(before fileConfig, data []byte) ([]byte, error) {
		if was, named := before.Mechanisms[id]; named && was == enabled {
			return nil, nil // the block already says it: a confirmation, not a rewrite
		}
		text, err := renderScalar(enabled)
		if err != nil {
			return nil, err
		}
		return spliceScalarSet(data, mechanismTarget(id), text)
	}
	return splice, func(before, after fileConfig, _ []byte) error {
		return verifyMechanismEdit(before, after, id, enabled)
	}
}

// mechanismTarget is where one mechanism line stands, in the shape the splice machinery reads a
// target from: a bool leaf inside the `mechanisms:` block. It is deliberately NOT a registry row —
// the registry describes the schema and these ids are the catalogue's — so it is built here, for
// the one splice, and lives no longer than the write.
func mechanismTarget(id string) Key {
	return Key{Path: mechanismsKey + "." + id, Kind: KindBool}
}

// verifyMechanismEdit is the gate a mechanism splice passes before it reaches the disk: the result
// must hold the block it started from with this one id set to exactly what was asked for and every
// sibling Mechanism untouched, and must agree with the config it started from on every setting
// outside the block.
//
// It is what catches a file shape the line arithmetic mis-read: a line inserted into somebody
// else's block changes a setting nobody asked to touch, and the comparison is what sees it.
func verifyMechanismEdit(before, after fileConfig, id string, enabled bool) error {
	switch {
	case !mechanismsChangedOnlyAt(before.Mechanisms, after.Mechanisms, id, enabled):
		return fmt.Errorf(
			"the edit did not put %s: %t in the mechanisms: block where a reader would look for it; "+
				"edit the file by hand", id, enabled)
	case !sameApartFrom(before, after, mechanismsKey):
		return errors.New(
			"the edit would have changed more than the mechanisms: block; edit the file by hand")
	}
	return nil
}
