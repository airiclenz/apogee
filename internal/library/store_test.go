package library

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

func highFP(label string) domain.ModelFingerprint {
	return domain.ModelFingerprint{Label: label, Confidence: domain.ConfidenceHigh}
}

// A recorded observation round-trips through disk: a second store rooted at the same dir Loads
// the same entry, with its Bayesian counts intact.
func TestStoreRecordRoundTrip(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "library") // does not exist yet — Record must create it
	fp := highFP("sha256:abc")

	writeStore := NewStore(dir)
	id := writeStore.Record(fp, CategoryCorrection, []string{"read_file", "missing_param"}, "read the file first")
	writeStore.Record(fp, CategoryCorrection, []string{"read_file", "missing_param"}, "read the file first")
	if id == "" {
		t.Fatal("Record returned an empty id for a valid fingerprint")
	}

	readStore := NewStore(dir)
	if err := readStore.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := readStore.Query(fp)
	if len(got) != 1 {
		t.Fatalf("Query after reload = %d entries; want 1", len(got))
	}
	if got[0].ID != id || got[0].Content != "read the file first" || got[0].Observations != 2 {
		t.Errorf("reloaded entry = %+v; want id %q, the recorded content, and 2 observations", got[0], id)
	}
}

// Recording the same observation twice reinforces one entry (observation count climbs, the
// Bayesian score with it); recording a success drives the score back down below the query gate.
func TestStoreObservationConfidenceUpdates(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := highFP("sha256:model")
	st := NewStore(dir)

	// One observation is below the query gate (needs >= 2); a second reinforcement lifts it in.
	id := st.Record(fp, CategoryBehavioral, []string{"text_instead_of_tool"}, "prefer tool calls")
	if got := st.Query(fp); len(got) != 0 {
		t.Fatalf("a single observation should be below the query gate; got %d", len(got))
	}
	if id2 := st.Record(fp, CategoryBehavioral, []string{"text_instead_of_tool"}, "prefer tool calls"); id2 != id {
		t.Fatalf("reinforcement created a new entry %q; want the same id %q", id2, id)
	}
	got := st.Query(fp)
	if len(got) != 1 || got[0].Observations != 2 {
		t.Fatalf("after two observations: %d entries, observations=%v; want 1 entry with 2 observations", len(got), obsCount(got))
	}
	scoreAfterTwo := got[0].Score()

	// Enough successes drop the score below the injection gate — the entry survives but stops
	// qualifying for Query (the model grew out of the pattern).
	for i := 0; i < 5; i++ {
		st.RecordSuccess(id)
	}
	if q := st.Query(fp); len(q) != 0 {
		t.Errorf("accumulated successes should drop the entry below the query gate; still got %d", len(q))
	}
	if st.Count() != 1 {
		t.Errorf("the entry should survive (not be deleted) after successes; Count = %d", st.Count())
	}
	all := st.All()
	if len(all) != 1 || all[0].Score() >= scoreAfterTwo {
		t.Errorf("successes should lower the score below %v; got %v", scoreAfterTwo, all[0].Score())
	}
}

// A zero fingerprint (unidentified model) is inert: Record writes nothing and Query returns
// nothing, so a lost model identity never pollutes the Library.
func TestStoreZeroFingerprintInert(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st := NewStore(dir)

	if id := st.Record(domain.ModelFingerprint{}, CategoryCorrection, []string{"x"}, "content"); id != "" {
		t.Errorf("Record on a zero fingerprint returned id %q; want empty", id)
	}
	if st.Count() != 0 {
		t.Errorf("a zero-fingerprint Record should write nothing; Count = %d", st.Count())
	}
	if got := st.Query(domain.ModelFingerprint{}); got != nil {
		t.Errorf("Query on a zero fingerprint = %v; want nil", got)
	}
	if _, err := os.Stat(filepath.Join(dir, storeFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("an inert Record should not create the store file; stat err = %v", err)
	}
}

// A missing store is not an error — Load leaves an empty, usable store (a fresh install).
func TestStoreLoadMissingIsEmpty(t *testing.T) {
	t.Parallel()
	st := NewStore(filepath.Join(t.TempDir(), "never-written"))
	if err := st.Load(); err != nil {
		t.Errorf("Load of a missing store should not error; got %v", err)
	}
	if st.Count() != 0 {
		t.Errorf("a missing store should load empty; Count = %d", st.Count())
	}
}

// A corrupt store degrades to empty-with-soft-error: Load returns a non-nil error but the store
// stays usable (empty), matching the skills-catalog posture (never signals "unusable").
func TestStoreLoadCorruptDegradesToEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, storeFileName), []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("seed corrupt store: %v", err)
	}
	st := NewStore(dir)
	if err := st.Load(); err == nil {
		t.Error("Load of a corrupt store should return a soft error")
	}
	if st.Count() != 0 {
		t.Errorf("a corrupt store should degrade to empty; Count = %d", st.Count())
	}
	// The store is still usable after the soft error.
	if id := st.Record(highFP("sha256:x"), CategoryExample, []string{"t"}, "c"); id == "" {
		t.Error("store should stay usable after a corrupt Load")
	}
}

// A store written by a newer schema version is rejected as a soft ErrStoreVersion and degrades
// to empty — the same non-bricking posture as a corrupt file.
func TestStoreLoadNewerVersionRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	data, err := json.Marshal(persisted{Version: StoreVersion + 1, Entries: []*Entry{{ID: "x", ModelLabel: "m"}}})
	if err != nil {
		t.Fatalf("marshal future store: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, storeFileName), data, 0o600); err != nil {
		t.Fatalf("seed future store: %v", err)
	}

	st := NewStore(dir)
	err = st.Load()
	if !errors.Is(err, ErrStoreVersion) {
		t.Errorf("Load of a newer-version store: err = %v; want ErrStoreVersion", err)
	}
	if st.Count() != 0 {
		t.Errorf("a too-new store should degrade to empty; Count = %d", st.Count())
	}
}

// An expired entry is dropped on Load, so a store left running for a week does not inject on
// stale evidence.
func TestStoreLoadDropsExpired(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	old := time.Now().Add(-8 * 24 * time.Hour) // past the 7-day default TTL
	data, err := json.Marshal(persisted{Version: StoreVersion, Entries: []*Entry{{
		ID: "stale", ModelLabel: "m", Category: CategoryCorrection,
		Observations: 5, CreatedAt: old, LastUsed: old, TTLHours: defaultTTLHours,
	}}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, storeFileName), data, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	st := NewStore(dir)
	if err := st.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if st.Count() != 0 {
		t.Errorf("expired entry should be dropped on Load; Count = %d", st.Count())
	}
}

// Every byte the Store writes lands strictly inside the injected directory — it never reaches
// for $HOME or any ambient path (ADR 0001). The assertion snapshots the injected dir before and
// after a Record and requires the only new path to be the store file under it.
func TestStoreWritesStayInsideInjectedDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home) // if the store ever reached for ~/.apogee, it would land here (no t.Parallel with Setenv)
	injected := filepath.Join(t.TempDir(), "library")

	st := NewStore(injected)
	st.Record(highFP("sha256:abc"), CategoryCorrection, []string{"read_file"}, "read first")
	st.Record(highFP("sha256:def"), CategoryBehavioral, []string{"text_instead_of_tool"}, "use tools")

	// The store file exists under the injected dir.
	if _, err := os.Stat(filepath.Join(injected, storeFileName)); err != nil {
		t.Fatalf("store file should exist under the injected dir: %v", err)
	}
	// Nothing was written under HOME.
	assertDirEmpty(t, home)
	// The injected dir holds exactly the one store file.
	entries, err := os.ReadDir(injected)
	if err != nil {
		t.Fatalf("read injected dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != storeFileName {
		t.Errorf("injected dir contents = %v; want only %q", names(entries), storeFileName)
	}
}

func assertDirEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	if len(entries) != 0 {
		t.Errorf("%s should be untouched but contains %v", dir, names(entries))
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func obsCount(entries []Entry) []int {
	out := make([]int, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Observations)
	}
	return out
}
