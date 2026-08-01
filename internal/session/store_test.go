package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// sampleRecord is a fully-populated record for round-trip and update tests.
func sampleRecord(id string, updated time.Time) Record {
	return Record{
		Meta: Meta{
			ID:        id,
			Title:     "greet the world",
			CreatedAt: updated.Add(-time.Hour),
			UpdatedAt: updated,
			Workspace: "/work/proj",
			Model:     "test-model",
			UserMsgs:  3,
			CtxUsed:   1200,
		},
		Transcript: json.RawMessage(`{"version":1,"entries":[]}`),
		Session:    domain.Session{Version: domain.SessionVersion, State: json.RawMessage(`{"k":"v"}`)},
	}
}

// A saved record round-trips through Load, Save stamps RecordVersion, and Save creates the
// directory it was pointed at (resolveRoots yields paths only).
func TestSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "sessions") // does not exist yet — Save must create it
	st := NewStore(dir)

	want := sampleRecord("20260724T120000Z-aaaa", time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC))
	if err := st.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := st.Load(want.Meta.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.RecordVersion != RecordVersion {
		t.Errorf("RecordVersion = %d, want %d (Save must stamp it)", got.RecordVersion, RecordVersion)
	}
	if got.Meta != want.Meta {
		t.Errorf("Meta round-trip = %+v, want %+v", got.Meta, want.Meta)
	}
	if string(got.Transcript) != string(want.Transcript) {
		t.Errorf("Transcript round-trip = %s, want %s", got.Transcript, want.Transcript)
	}
	if got.Session.Version != want.Session.Version || string(got.Session.State) != string(want.Session.State) {
		t.Errorf("Session round-trip = %+v, want %+v", got.Session, want.Session)
	}
}

// Two Saves under the same id produce exactly one file and the later state wins; List
// reflects the updated UpdatedAt.
func TestSaveUpdatesInPlace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st := NewStore(dir)

	id := "20260724T120000Z-bbbb"
	first := sampleRecord(id, time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC))
	if err := st.Save(first); err != nil {
		t.Fatalf("Save first: %v", err)
	}
	second := first
	second.Meta.Title = "renamed"
	second.Meta.UpdatedAt = first.Meta.UpdatedAt.Add(time.Hour)
	if err := st.Save(second); err != nil {
		t.Fatalf("Save second: %v", err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("update-in-place left %d files, want 1: %v", len(files), files)
	}
	metas, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 1 || metas[0].Title != "renamed" || !metas[0].UpdatedAt.Equal(second.Meta.UpdatedAt) {
		t.Errorf("List after update = %+v, want the later state", metas)
	}
}

// List returns metas newest-first by UpdatedAt.
func TestListSortsByUpdatedDescending(t *testing.T) {
	t.Parallel()
	st := NewStore(t.TempDir())

	base := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	for i, id := range []string{"a", "b", "c"} {
		rec := sampleRecord("id-"+id, base.Add(time.Duration(i)*time.Hour))
		if err := st.Save(rec); err != nil {
			t.Fatalf("Save %s: %v", id, err)
		}
	}

	metas, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 3 {
		t.Fatalf("List returned %d metas, want 3", len(metas))
	}
	for i := 1; i < len(metas); i++ {
		if metas[i-1].UpdatedAt.Before(metas[i].UpdatedAt) {
			t.Errorf("List not sorted descending: %v before %v", metas[i-1].UpdatedAt, metas[i].UpdatedAt)
		}
	}
	if metas[0].ID != "id-c" {
		t.Errorf("newest first = %q, want id-c", metas[0].ID)
	}
}

// A corrupt file in the directory is skipped, not fatal — the browser survives it.
func TestListSkipsCorruptFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st := NewStore(dir)

	if err := st.Save(sampleRecord("good", time.Now().UTC())); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{not json"), filePerm); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stray.json"), []byte(`{"foo":1}`), filePerm); err != nil {
		t.Fatalf("write stray: %v", err)
	}

	metas, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 1 || metas[0].ID != "good" {
		t.Errorf("List = %+v, want only the good record", metas)
	}
}

// A missing store directory lists as empty rather than erroring.
func TestListMissingDirIsEmpty(t *testing.T) {
	t.Parallel()
	st := NewStore(filepath.Join(t.TempDir(), "never-written"))
	metas, err := st.List()
	if err != nil {
		t.Fatalf("List of missing dir: %v", err)
	}
	if len(metas) != 0 {
		t.Errorf("List of missing dir = %+v, want empty", metas)
	}
}

// A pre-plan bare domain.Session file (the {"Version":..,"State":..} shape written before this
// plan) is sniffed and wrapped: it lists with synthetic metadata and loads into a resumable
// Record.Session.
func TestLegacyBareEnvelopeIsWrapped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st := NewStore(dir)

	legacy := domain.Session{Version: domain.SessionVersion, State: json.RawMessage(`{"turnIndex":2}`)}
	data, err := legacy.Encode() // the pre-plan bare-envelope bytes, written under a chosen stem
	if err != nil {
		t.Fatalf("encode legacy envelope: %v", err)
	}
	stem := "20260623T183005Z"
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, stem+".json"), data, filePerm); err != nil {
		t.Fatalf("write legacy envelope: %v", err)
	}

	metas, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("List returned %d metas, want 1", len(metas))
	}
	if metas[0].ID != stem || metas[0].Title != "Session "+stem {
		t.Errorf("legacy meta = %+v, want id/title from the filename stem %q", metas[0], stem)
	}
	if metas[0].Workspace != "" {
		t.Errorf("legacy workspace = %q, want empty (unknown)", metas[0].Workspace)
	}

	got, err := st.Load(stem)
	if err != nil {
		t.Fatalf("Load legacy: %v", err)
	}
	if got.Session.Version != legacy.Version || string(got.Session.State) != string(legacy.State) {
		t.Errorf("legacy Session = %+v, want the resumable original %+v", got.Session, legacy)
	}
	if len(got.Transcript) != 0 {
		t.Errorf("legacy Transcript = %s, want empty (no scrollback recorded)", got.Transcript)
	}
}

// A record whose RecordVersion is from a newer build is refused with ErrRecordVersion.
func TestLoadRejectsFutureRecordVersion(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st := NewStore(dir)

	future := fmt.Sprintf(`{"recordVersion":%d,"meta":{"id":"future"},"session":{"Version":1,"State":null}}`, RecordVersion+1)
	if err := os.WriteFile(filepath.Join(dir, "future.json"), []byte(future), filePerm); err != nil {
		t.Fatalf("write future: %v", err)
	}
	if _, err := st.Load("future"); !errors.Is(err, ErrRecordVersion) {
		t.Errorf("Load(future) err = %v, want ErrRecordVersion", err)
	}
	// List skips what it cannot decode, so a future record does not surface.
	metas, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 0 {
		t.Errorf("List surfaced a future-version record: %+v", metas)
	}
}

// Delete removes the record's file.
func TestDelete(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st := NewStore(dir)
	if err := st.Save(sampleRecord("gone", time.Now().UTC())); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := st.Delete("gone"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gone.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file still present after Delete: %v", err)
	}
}

// Rename persists a new title and re-stamps UpdatedAt, leaving the session resumable.
func TestRenamePersists(t *testing.T) {
	t.Parallel()
	st := NewStore(t.TempDir())
	renameAt := time.Date(2026, 7, 24, 15, 0, 0, 0, time.UTC)
	st.now = func() time.Time { return renameAt }

	rec := sampleRecord("rn", time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC))
	if err := st.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := st.Rename("rn", "a better name"); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	got, err := st.Load("rn")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Meta.Title != "a better name" {
		t.Errorf("title after Rename = %q, want the new title", got.Meta.Title)
	}
	if !got.Meta.UpdatedAt.Equal(renameAt) {
		t.Errorf("UpdatedAt after Rename = %v, want %v", got.Meta.UpdatedAt, renameAt)
	}
	if string(got.Session.State) != string(rec.Session.State) {
		t.Errorf("Rename disturbed the Session payload")
	}
}

// A successful Save leaves no temp file behind — the atomic write cleans up after itself.
func TestSaveLeavesNoTempFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st := NewStore(dir)
	if err := st.Save(sampleRecord("id", time.Now().UTC())); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			t.Errorf("Save left a non-record file behind: %q", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("dir has %d entries, want exactly the one record: %v", len(entries), entries)
	}
}

// unsafeIDs are the ids no store operation may turn into a path: an empty one would write a
// nameless ".json" file, the rest escape the store directory or name something that is not a
// plain record file. Ids reach the store from records the user resumed by path, so these are
// reachable inputs, not hypotheticals.
var unsafeIDs = []struct {
	name string
	id   string
}{
	{"empty", ""},
	{"relative traversal", filepath.Join("..", "..", ".claude", "settings")},
	{"leading traversal", "../evil"},
	{"absolute path", "/etc/apogee-victim"},
	{"nested path", "sub/dir"},
	{"backslash path", `sub\dir`},
	{"dot", "."},
	{"dot dot", ".."},
	{"dot prefixed", ".hidden"},
	{"newline", "two\nlines"},
	{"nul byte", "nul\x00byte"},
	{"over length", strings.Repeat("x", maxIDLen+1)},
}

// Save refuses every id that is not a safe filename component — including the empty one —
// rather than writing outside the store directory.
func TestSaveRejectsUnsafeID(t *testing.T) {
	t.Parallel()
	for _, tc := range unsafeIDs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			st := NewStore(dir)
			if err := st.Save(sampleRecord(tc.id, time.Now().UTC())); !errors.Is(err, ErrInvalidID) {
				t.Errorf("Save(id=%q) err = %v, want ErrInvalidID", tc.id, err)
			}
			entries, err := os.ReadDir(dir)
			if err == nil && len(entries) != 0 {
				t.Errorf("Save(id=%q) wrote %d entries into the store, want none", tc.id, len(entries))
			}
		})
	}
}

// Delete refuses the same ids, so a hostile id can never remove a file outside the store — the
// browser's delete verb is driven by an id a record declared.
func TestDeleteRejectsUnsafeID(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	victim := filepath.Join(home, "settings.json")
	if err := os.WriteFile(victim, []byte(`{"keep":true}`), filePerm); err != nil {
		t.Fatalf("write victim: %v", err)
	}
	st := NewStore(filepath.Join(home, "sessions"))

	for _, tc := range unsafeIDs {
		if err := st.Delete(tc.id); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Delete(%q) err = %v, want ErrInvalidID", tc.id, err)
		}
	}
	// The traversal that would have reached it: <sessions>/../settings.json.
	if err := st.Delete(filepath.Join("..", "settings")); !errors.Is(err, ErrInvalidID) {
		t.Errorf("Delete of a traversal err = %v, want ErrInvalidID", err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Errorf("a file outside the store was deleted through an id: %v", err)
	}
}

// Load refuses an unsafe id rather than reading through it — LoadPath is the deliberate way to
// read a record from outside the store.
func TestLoadRejectsUnsafeID(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "secret.json"), []byte(`{"Version":1,"State":null}`), filePerm); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	st := NewStore(filepath.Join(home, "sessions"))
	if _, err := st.Load(filepath.Join("..", "secret")); !errors.Is(err, ErrInvalidID) {
		t.Errorf("Load of a traversal id err = %v, want ErrInvalidID", err)
	}
}

// A stored file that DECLARES a hostile id is refused at decode: Load surfaces ErrInvalidID and
// List skips it, so the id never reaches a writer. This is the planted-record case — a session
// file shipped in a repo and resumed by path.
func TestDecodeRejectsUnsafeRecordID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st := NewStore(dir)
	if err := st.Save(sampleRecord("20260801T120000Z-cccccccc", time.Now().UTC())); err != nil {
		t.Fatalf("Save legitimate: %v", err)
	}
	planted := fmt.Sprintf(
		`{"recordVersion":%d,"meta":{"id":"../../.claude/settings"},"session":{"Version":1,"State":null}}`,
		RecordVersion)
	if err := os.WriteFile(filepath.Join(dir, "planted.json"), []byte(planted), filePerm); err != nil {
		t.Fatalf("write planted: %v", err)
	}

	if _, err := st.Load("planted"); !errors.Is(err, ErrInvalidID) {
		t.Errorf("Load of a record declaring a traversal id err = %v, want ErrInvalidID", err)
	}
	if _, err := st.LoadPath(filepath.Join(dir, "planted.json")); !errors.Is(err, ErrInvalidID) {
		t.Errorf("LoadPath of a record declaring a traversal id err = %v, want ErrInvalidID", err)
	}
	metas, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 1 || metas[0].ID != "20260801T120000Z-cccccccc" {
		t.Errorf("List = %+v, want only the legitimate record (the planted one soft-skipped)", metas)
	}
}

// NewID is a sortable UTC stamp plus a random hex suffix; the random source is injectable
// so the suffix is pinnable, and the second stamp orders chronologically.
func TestNewID(t *testing.T) {
	t.Parallel()
	when := time.Date(2026, 7, 24, 15, 30, 45, 0, time.UTC)
	id := newID(when, bytes.NewReader([]byte{0xde, 0xad, 0xbe, 0xef}))
	if want := "20260724T153045Z-deadbeef"; id != want {
		t.Errorf("newID = %q, want %q", id, want)
	}

	// Non-UTC clocks are normalised so ids sort regardless of host timezone.
	east := time.Date(2026, 7, 24, 20, 30, 45, 0, time.FixedZone("UTC+5", 5*60*60))
	got := newID(east, bytes.NewReader([]byte{0, 0, 0, 0}))
	if !strings.HasPrefix(got, "20260724T153045Z-") {
		t.Errorf("newID(non-UTC) = %q, want the UTC-normalised stamp prefix", got)
	}

	// Later stamps sort after earlier ones lexically.
	earlier := newID(when, bytes.NewReader([]byte{0, 0, 0, 0}))
	later := newID(when.Add(time.Second), bytes.NewReader([]byte{0, 0, 0, 0}))
	if !(earlier < later) {
		t.Errorf("ids not chronological: %q !< %q", earlier, later)
	}
}
