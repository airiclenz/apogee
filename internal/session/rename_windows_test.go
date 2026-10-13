//go:build windows

package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/testtime"
)

// A save that renames over a record a reader holds open with os.Open — which shares read and
// write but not delete, so MoveFileEx over it fails — waits for the reader to let go within the
// retry budget and then lands: the save succeeds and the record holds the new payload. Before the
// retry, this is the save that failed when the e2e test's savedTranscript poll read the record
// while the app wrote it.
func TestAtomicWriteRetriesThroughASharingViolation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st := NewStore(dir)
	rec := sampleRecord("held", time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC))
	if err := st.Save(rec); err != nil {
		t.Fatalf("first Save: %v", err)
	}

	reader, err := os.Open(filepath.Join(dir, "held.json"))
	if err != nil {
		t.Fatalf("open the record as a reader: %v", err)
	}
	released := make(chan struct{})
	go func() {
		defer close(released)
		// The hold is semantic — long enough that the save's first rename meets it, well inside
		// renameRetryBudget so the retry outlasts it.
		time.Sleep(testtime.Fixed(300 * time.Millisecond))
		_ = reader.Close()
	}()

	rec.Meta.Title = "saved over a held record"
	if err := st.Save(rec); err != nil {
		t.Fatalf("Save over a record a reader holds open = %v, want it to wait the reader out and land", err)
	}
	<-released

	got, err := st.Load("held")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Meta.Title != rec.Meta.Title {
		t.Errorf("title after the save = %q, want %q", got.Meta.Title, rec.Meta.Title)
	}
}
