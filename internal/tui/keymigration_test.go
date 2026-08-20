package tui

import (
	"errors"
	"strings"
	"testing"
)

// ----------------------------------------------------------------------------
// The start-up key-migration offer (keymigration.go)
// ----------------------------------------------------------------------------

// fakeKeyWriter stands in for the two composition-root seams the offer answers through. It records
// every entry it was asked about, in order, so a test can assert not just WHAT was written but that
// the answer the human did not give wrote nothing at all. Called synchronously on the test's
// goroutine, so it needs no guard.
type fakeKeyWriter struct {
	migrated []string
	kept     []string
	path     string
	err      error
}

func (f *fakeKeyWriter) migrate(entry string) (string, error) {
	f.migrated = append(f.migrated, entry)
	return f.path, f.err
}

func (f *fakeKeyWriter) keep(entry string) (string, error) {
	f.kept = append(f.kept, entry)
	return f.path, f.err
}

// offerOpts are the Options a start-up hands a session that found plaintext keys it can offer to
// move: the store's name, the entries, and both write seams wired.
func offerOpts(w *fakeKeyWriter, entries ...string) Options {
	opts := testOpts
	opts.KeyMigration = KeyMigrationOffer{StoreName: "macOS Keychain", Entries: entries}
	opts.MigrateKey = w.migrate
	opts.KeepPlaintextKey = w.keep
	return opts
}

// offerModel is a ready 80×24 session that opened with the offer up.
func offerModel(t *testing.T, w *fakeKeyWriter, entries ...string) Model {
	t.Helper()
	return newTestModelEng(t, &fakeEngine{}, offerOpts(w, entries...))
}

// The pane comes up unasked at construction, under a notice naming what was found and where it
// could go instead, and it asks about the FIRST entry with the rest queued behind it.
func TestKeyMigrationOfferOpensWithItsNotice(t *testing.T) {
	w := &fakeKeyWriter{path: "/home/x/.apogee/config.yaml"}
	m := offerModel(t, w, "workstation", "laptop")

	if !m.picker.open || m.picker.kind != pickerKeyMigration {
		t.Fatalf("picker = %+v, want the key-migration offer open at start-up", m.picker)
	}
	if got := m.picker.migration; len(got) != 2 || got[0] != "workstation" {
		t.Errorf("queue = %v, want both entries with workstation first", got)
	}
	if n := m.pickerCount(); n != 3 {
		t.Errorf("rows = %d, want the three answers", n)
	}
	if title := m.pickerTitle(); !strings.Contains(title, "workstation") ||
		!strings.Contains(title, "macOS Keychain") {
		t.Errorf("title = %q, want the entry being asked about and the store", title)
	}
	notes := noteTexts(m)
	if len(notes) == 0 {
		t.Fatal("the pane came up with no notice saying why")
	}
	last := notes[len(notes)-1]
	if !strings.Contains(last, "workstation and laptop") || !strings.Contains(last, "macOS Keychain") {
		t.Errorf("notice = %q, want both entries named and the store", last)
	}
	if len(w.migrated) != 0 || len(w.kept) != 0 {
		t.Errorf("the offer wrote something before it was answered: %+v", w)
	}
}

// No offer without BOTH halves of it — an entry to move and a store to move it into — and none
// without the seam that does the moving. Each is an offer apogee could not complete.
func TestKeyMigrationOfferNeedsAStoreAnEntryAndASeam(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*Options)
	}{
		{"no store", func(o *Options) { o.KeyMigration.StoreName = "" }},
		{"no entries", func(o *Options) { o.KeyMigration.Entries = nil }},
		{"no seam", func(o *Options) { o.MigrateKey = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := offerOpts(&fakeKeyWriter{}, "workstation")
			tc.mut(&opts)

			m := newTestModelEng(t, &fakeEngine{}, opts)

			if m.picker.open {
				t.Errorf("picker = %+v, want no pane where the move could not be completed", m.picker)
			}
		})
	}
}

// The pre-bound ask owns the opening frame. A session with no server is asked THAT question alone —
// the offer costs nothing by waiting, because "not now" is what it already means.
func TestKeyMigrationGivesWayToThePreboundAsk(t *testing.T) {
	w := &fakeKeyWriter{}
	opts := preboundOpts(PreboundFirstBoot, "")
	opts.KeyMigration = KeyMigrationOffer{StoreName: "macOS Keychain", Entries: []string{"workstation"}}
	opts.MigrateKey, opts.KeepPlaintextKey = w.migrate, w.keep
	opts.BindServer = (&fakeBind{}).bind

	m := newTestModelEng(t, &fakeEngine{}, opts)

	if m.picker.kind != pickerServer {
		t.Errorf("picker kind = %v, want the pre-bound server picker to keep the frame", m.picker.kind)
	}
	if len(m.picker.migration) != 0 {
		t.Errorf("the offer queued itself behind the pre-bound ask: %v", m.picker.migration)
	}
}

// "not now" persists nothing at all — not the marker either, since the whole point of the answer is
// that it is not final — and moves on to the next entry.
func TestKeyMigrationNotNowPersistsNothing(t *testing.T) {
	w := &fakeKeyWriter{path: "/home/x/.apogee/config.yaml"}
	m := offerModel(t, w, "workstation", "laptop")

	m = step(t, m, keyDown()) // "not now"
	m = step(t, m, keyEnter())

	if len(w.migrated) != 0 || len(w.kept) != 0 {
		t.Errorf("a declined offer wrote something: migrated=%v kept=%v", w.migrated, w.kept)
	}
	if !m.picker.open || m.picker.kind != pickerKeyMigration {
		t.Fatalf("picker = %+v, want the next entry's pane", m.picker)
	}
	if got := m.picker.migration; len(got) != 1 || got[0] != "laptop" {
		t.Errorf("queue = %v, want the round to have advanced to laptop", got)
	}
	if note := lastNote(m); !strings.Contains(note, "workstation") ||
		!strings.Contains(note, "next start-up") {
		t.Errorf("note = %q, want it to say the entry was left alone and will be asked again", note)
	}
}

// "never for this entry" is the one answer that writes without moving anything: the per-entry
// marker, and a note saying which file records it so the human can take it back.
func TestKeyMigrationNeverRecordsTheMarker(t *testing.T) {
	w := &fakeKeyWriter{path: "/home/x/.apogee/config.yaml"}
	m := offerModel(t, w, "workstation")

	m = step(t, m, keyDown())
	m = step(t, m, keyDown()) // "never for this entry"
	m = step(t, m, keyEnter())

	if len(w.kept) != 1 || w.kept[0] != "workstation" {
		t.Errorf("KeepPlaintextKey calls = %v, want the one entry that was asked about", w.kept)
	}
	if len(w.migrated) != 0 {
		t.Errorf("the never answer moved a key: %v", w.migrated)
	}
	if m.picker.open {
		t.Errorf("picker = %+v, want the round over with one entry answered", m.picker)
	}
	if note := lastNote(m); !strings.Contains(note, "plaintext-key-ok") ||
		!strings.Contains(note, w.path) {
		t.Errorf("note = %q, want the marker and the file that records it", note)
	}
}

// The move goes through the seam and the round advances to the next entry. What the seam DOES with
// it — the store write, the read-back, the rewrite — is the binary's (keymigrate.go's own suite);
// what is proved here is that one answer is one call about one entry.
func TestKeyMigrationMoveCallsTheSeamAndAdvances(t *testing.T) {
	w := &fakeKeyWriter{path: "/home/x/.apogee/config.yaml"}
	m := offerModel(t, w, "workstation", "laptop")

	m = step(t, m, keyEnter()) // "move it" is the first row

	if len(w.migrated) != 1 || w.migrated[0] != "workstation" {
		t.Errorf("MigrateKey calls = %v, want the entry the pane was asking about", w.migrated)
	}
	if got := m.picker.migration; len(got) != 1 || got[0] != "laptop" {
		t.Fatalf("queue = %v, want the pane to have moved on to laptop", got)
	}
	if got := m.picker.filter.value(); got != "" {
		t.Errorf("filter = %q, want the next question asked on a clean pane", got)
	}
	if note := lastNote(m); !strings.Contains(note, "macOS Keychain") ||
		!strings.Contains(note, w.path) {
		t.Errorf("note = %q, want the store the key went into and the file that now reads it", note)
	}
}

// A move that FAILED says so and leaves the entry as it was — a migration that silently did not
// happen would leave the human believing their key had moved. The round still advances: the entry
// keeps its plaintext key, which is the state "not now" leaves it in, so the next start-up asks
// again rather than this one asking twice.
func TestKeyMigrationReportsAFailedMove(t *testing.T) {
	w := &fakeKeyWriter{err: errors.New("the keychain refused the key")}
	m := offerModel(t, w, "workstation")

	m = step(t, m, keyEnter())

	if note := lastNote(m); !strings.Contains(note, "could not move") ||
		!strings.Contains(note, "the keychain refused the key") {
		t.Errorf("note = %q, want the failure and what the store said", note)
	}
	if m.picker.open {
		t.Errorf("picker = %+v, want the round over — the entry is asked about again next start-up", m.picker)
	}
}

// esc ends the whole round: every entry still queued is a "not now", and nothing is written.
func TestKeyMigrationEscEndsTheRound(t *testing.T) {
	w := &fakeKeyWriter{}
	m := offerModel(t, w, "workstation", "laptop")

	m = step(t, m, keyEsc())

	if m.picker.open || len(m.picker.migration) != 0 {
		t.Errorf("picker = %+v, want the round closed outright", m.picker)
	}
	if len(w.migrated) != 0 || len(w.kept) != 0 {
		t.Errorf("esc wrote something: migrated=%v kept=%v", w.migrated, w.kept)
	}
}
