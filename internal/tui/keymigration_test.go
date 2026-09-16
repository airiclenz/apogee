package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ----------------------------------------------------------------------------
// The start-up key-migration offer (keymigration.go)
// ----------------------------------------------------------------------------

// fakeConfigHost is the [ConfigHost] a renderer test hands over: one func per act, wired one member
// at a time, answering the documented unwired degrade for any member a test leaves nil (ADR 0054
// decision 6) — so "an offer with no store to answer it" or "a pane with rows but no editor" is one
// literal, and the degrade a nil host takes is the same one an unwired member takes.
type fakeConfigHost struct {
	saveHostAcknowledgement func() (string, error)
	reloadConfig            func() (ConfigReload, error)
	awaitConfigChange       func(context.Context) bool
	migrateKey              func(entry string) (string, error)
	keepPlaintextKey        func(entry string) (string, error)
	migrateSubAgentsServer  func(entry string) (string, error)
	recordModelChoice       func(model string) (bool, error)
	externalEditSpec        func(path string) (EditorCommand, error)
}

func (h fakeConfigHost) SaveHostAcknowledgement() (string, error) {
	if h.saveHostAcknowledgement == nil {
		return noopConfigHost{}.SaveHostAcknowledgement()
	}
	return h.saveHostAcknowledgement()
}

func (h fakeConfigHost) ReloadConfig() (ConfigReload, error) {
	if h.reloadConfig == nil {
		return noopConfigHost{}.ReloadConfig()
	}
	return h.reloadConfig()
}

func (h fakeConfigHost) AwaitConfigChange(ctx context.Context) bool {
	if h.awaitConfigChange == nil {
		return noopConfigHost{}.AwaitConfigChange(ctx)
	}
	return h.awaitConfigChange(ctx)
}

func (h fakeConfigHost) MigrateKey(entry string) (string, error) {
	if h.migrateKey == nil {
		return noopConfigHost{}.MigrateKey(entry)
	}
	return h.migrateKey(entry)
}

func (h fakeConfigHost) KeepPlaintextKey(entry string) (string, error) {
	if h.keepPlaintextKey == nil {
		return noopConfigHost{}.KeepPlaintextKey(entry)
	}
	return h.keepPlaintextKey(entry)
}

func (h fakeConfigHost) MigrateSubAgentsServer(entry string) (string, error) {
	if h.migrateSubAgentsServer == nil {
		return noopConfigHost{}.MigrateSubAgentsServer(entry)
	}
	return h.migrateSubAgentsServer(entry)
}

func (h fakeConfigHost) RecordModelChoice(model string) (bool, error) {
	if h.recordModelChoice == nil {
		return noopConfigHost{}.RecordModelChoice(model)
	}
	return h.recordModelChoice(model)
}

func (h fakeConfigHost) ExternalEditSpec(path string) (EditorCommand, error) {
	if h.externalEditSpec == nil {
		return noopConfigHost{}.ExternalEditSpec(path)
	}
	return h.externalEditSpec(path)
}

// configSeams is the [ConfigHost] fake opts carries, created on the spot when it carries none —
// serverSeams' idiom, so a helper that wires one member finds the members already there instead
// of replacing them.
func configSeams(opts *Options) *fakeConfigHost {
	host, ok := opts.Config.(*fakeConfigHost)
	if !ok {
		host = &fakeConfigHost{}
		opts.Config = host
	}
	return host
}

// configWriteLog stands in for the three offer answers that write anything — the key move, the
// "never" marker and the sub-agents move. It records every entry it was asked about, in order, so a
// test can assert not just WHAT was written but that the answer the human did not give wrote nothing
// at all. Called synchronously on the test's goroutine, so it needs no guard.
type configWriteLog struct {
	migrated []string
	kept     []string
	moved    []string
	path     string
	err      error
}

func (f *configWriteLog) migrate(entry string) (string, error) {
	f.migrated = append(f.migrated, entry)
	return f.path, f.err
}

func (f *configWriteLog) keep(entry string) (string, error) {
	f.kept = append(f.kept, entry)
	return f.path, f.err
}

func (f *configWriteLog) moveSubAgents(entry string) (string, error) {
	f.moved = append(f.moved, entry)
	return f.path, f.err
}

// offerOpts are the Options a start-up hands a session that found plaintext keys it can offer to
// move: the store's name, the entries, and both write seams wired.
func offerOpts(w *configWriteLog, entries ...string) Options {
	opts := testOpts
	opts.KeyMigration = KeyMigrationOffer{StoreName: "macOS Keychain", Entries: entries}
	seams := configSeams(&opts)
	seams.migrateKey, seams.keepPlaintextKey = w.migrate, w.keep
	return opts
}

// offerModel is a ready 80×24 session that opened with the offer up.
func offerModel(t *testing.T, w *configWriteLog, entries ...string) Model {
	t.Helper()
	return newTestModelEng(t, &fakeEngine{}, offerOpts(w, entries...))
}

// The pane comes up unasked at construction, under a notice naming what was found and where it
// could go instead, and it asks about the FIRST entry with the rest queued behind it.
func TestKeyMigrationOfferOpensWithItsNotice(t *testing.T) {
	t.Parallel()

	w := &configWriteLog{path: "/home/x/.apogee/config.yaml"}
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
// without the host that does the moving: the gate is decided ABOUT the [ConfigHost] before any act is
// called (ADR 0054 decision 3a), so a nil host raises nothing rather than answering a refusal. Each
// is an offer apogee could not complete.
func TestKeyMigrationOfferNeedsAStoreAnEntryAndASeam(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		mut  func(*Options)
	}{
		{"no store", func(o *Options) { o.KeyMigration.StoreName = "" }},
		{"no entries", func(o *Options) { o.KeyMigration.Entries = nil }},
		{"no seam", func(o *Options) { o.Config = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			opts := offerOpts(&configWriteLog{}, "workstation")
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
	t.Parallel()

	w := &configWriteLog{}
	opts := preboundOpts(PreboundFirstBoot, "")
	opts.KeyMigration = KeyMigrationOffer{StoreName: "macOS Keychain", Entries: []string{"workstation"}}
	seams := configSeams(&opts)
	seams.migrateKey, seams.keepPlaintextKey = w.migrate, w.keep
	serverSeams(&opts).bind = (&fakeBind{}).bind

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
	t.Parallel()

	w := &configWriteLog{path: "/home/x/.apogee/config.yaml"}
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
	t.Parallel()

	w := &configWriteLog{path: "/home/x/.apogee/config.yaml"}
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
	t.Parallel()

	w := &configWriteLog{path: "/home/x/.apogee/config.yaml"}
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
	t.Parallel()

	w := &configWriteLog{err: errors.New("the keychain refused the key")}
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
	t.Parallel()

	w := &configWriteLog{}
	m := offerModel(t, w, "workstation", "laptop")

	m = step(t, m, keyEsc())

	if m.picker.open || len(m.picker.migration) != 0 {
		t.Errorf("picker = %+v, want the round closed outright", m.picker)
	}
	if len(w.migrated) != 0 || len(w.kept) != 0 {
		t.Errorf("esc wrote something: migrated=%v kept=%v", w.migrated, w.kept)
	}
}

// ----------------------------------------------------------------------------
// The start-up sub-agents-flag offer (keymigration.go, ADR 0045)
// ----------------------------------------------------------------------------

// subAgentsOfferOpts are the Options a start-up hands a session whose config still carries the
// retired flag: the flagged entries in the file's own order, and the one seam wired.
func subAgentsOfferOpts(f *configWriteLog, entries ...string) Options {
	opts := testOpts
	opts.SubAgentsMigration = entries
	configSeams(&opts).migrateSubAgentsServer = f.moveSubAgents
	return opts
}

// subAgentsOfferModel is a ready 80×24 session that opened with the offer up.
func subAgentsOfferModel(t *testing.T, f *configWriteLog, entries ...string) Model {
	t.Helper()
	return newTestModelEng(t, &fakeEngine{}, subAgentsOfferOpts(f, entries...))
}

// The pane comes up unasked at construction, under a notice naming the entries that carry the
// retired flag, and it asks about the FIRST of them — the name the answer would write as the key.
func TestSubAgentsMigrationOfferOpensWithItsNotice(t *testing.T) {
	t.Parallel()

	f := &configWriteLog{path: "/home/x/.apogee/config.yaml"}
	m := subAgentsOfferModel(t, f, "cheap", "spare")

	if !m.picker.open || m.picker.kind != pickerSubAgentsMigration {
		t.Fatalf("picker = %+v, want the sub-agents offer open at start-up", m.picker)
	}
	if title := m.pickerTitle(); !strings.Contains(title, "cheap") {
		t.Errorf("title = %q, want the first flagged entry named", title)
	}
	if rows := m.pickerOfferingRows(); len(rows) != 2 {
		t.Errorf("rows = %v, want exactly move it and not now — there is no never row", rows)
	}
	notes := strings.Join(noteTexts(m), "\n")
	if !strings.Contains(notes, "sub-agents: true") || !strings.Contains(notes, "cheap and spare") {
		t.Errorf("notices = %q, want the retired key and both entries named", notes)
	}
}

// A config carrying no retired flag — every config written since the root key replaced it — is asked
// nothing, and neither is one whose [ConfigHost] the Driver left unwired: the gate is decided ABOUT
// the host before any act is called (ADR 0054 decision 3a).
func TestSubAgentsMigrationNeedsAnEntryAndASeam(t *testing.T) {
	t.Parallel()

	f := &configWriteLog{}
	if m := newTestModelEng(t, &fakeEngine{}, subAgentsOfferOpts(f)); m.picker.open {
		t.Errorf("picker = %+v, want nothing raised for a config with no retired flag", m.picker)
	}
	opts := subAgentsOfferOpts(f, "cheap")
	opts.Config = nil
	if m := newTestModelEng(t, &fakeEngine{}, opts); m.picker.open {
		t.Errorf("picker = %+v, want nothing raised where the answer could not be carried out", m.picker)
	}
}

// "move it" is one call about the entry the pane named, and the note says the key that now names it
// and the file that carries it. What the seam DOES — the rewrite and the retarget — is the binary's
// (keymigrate.go's own suite).
func TestSubAgentsMigrationMoveCallsTheSeam(t *testing.T) {
	t.Parallel()

	f := &configWriteLog{path: "/home/x/.apogee/config.yaml"}
	m := subAgentsOfferModel(t, f, "cheap", "spare")

	m = step(t, m, keyEnter()) // "move it" is the first row

	if len(f.moved) != 1 || f.moved[0] != "cheap" {
		t.Errorf("MigrateSubAgentsServer calls = %v, want the entry the pane was asking about", f.moved)
	}
	if m.picker.open {
		t.Errorf("picker = %+v, want the one question answered and closed", m.picker)
	}
	if note := lastNote(m); !strings.Contains(note, "sub-agents-server: cheap") ||
		!strings.Contains(note, f.path) {
		t.Errorf("note = %q, want the key it wrote and the file that carries it", note)
	}
}

// A move that FAILED says so, and the file is the seam's business: nothing here claims a routing
// change that did not happen.
func TestSubAgentsMigrationReportsAFailedMove(t *testing.T) {
	t.Parallel()

	f := &configWriteLog{err: errors.New("the servers: list has no entry named \"cheap\"")}
	m := subAgentsOfferModel(t, f, "cheap")

	m = step(t, m, keyEnter())

	if note := lastNote(m); !strings.Contains(note, "could not move") ||
		!strings.Contains(note, "no entry named") {
		t.Errorf("note = %q, want the failure and what the writer said", note)
	}
}

// "not now" and esc are the same answer spelled two ways: nothing is written, and the offer comes
// back at the next start-up because the file still carries the flag.
func TestSubAgentsMigrationDeclinedPersistsNothing(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		key  func() tea.KeyPressMsg
		down bool
	}{
		{name: "not now", key: keyEnter, down: true},
		{name: "esc", key: keyEsc},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := &configWriteLog{}
			m := subAgentsOfferModel(t, f, "cheap")
			if tc.down {
				m = step(t, m, keyDown())
			}
			m = step(t, m, tc.key())

			if len(f.moved) != 0 {
				t.Errorf("a declined offer wrote something: %v", f.moved)
			}
			if m.picker.open {
				t.Errorf("picker = %+v, want the offer closed", m.picker)
			}
		})
	}
}

// The two unasked start-up panes never race. A config carrying BOTH a literal `api-key:` and the
// retired flag opens the key migration — the offer that was already there — and the sub-agents offer
// gives way exactly as the key migration gives way to the pre-bound ask: nothing of it is written,
// and the Options still describe what this start-up found, so the next one asks.
func TestSubAgentsMigrationGivesWayToTheKeyMigration(t *testing.T) {
	t.Parallel()

	w := &configWriteLog{path: "/home/x/.apogee/config.yaml"}
	opts := offerOpts(w, "workstation")
	opts.SubAgentsMigration = []string{"cheap"}
	configSeams(&opts).migrateSubAgentsServer = w.moveSubAgents

	m := newTestModelEng(t, &fakeEngine{}, opts)

	if !m.picker.open || m.picker.kind != pickerKeyMigration {
		t.Fatalf("picker = %+v, want the key migration to keep the frame", m.picker)
	}
	if len(w.moved) != 0 {
		t.Errorf("the offer that gave way wrote something: %v", w.moved)
	}
	if len(m.opts.SubAgentsMigration) != 1 {
		t.Errorf("SubAgentsMigration = %v, want the start-up's finding kept for the next one",
			m.opts.SubAgentsMigration)
	}
}
