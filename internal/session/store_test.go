package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// A saved snapshot round-trips: the file Store writes decodes back to an equal Session,
// and Save creates the directory it was pointed at (resolveRoots yields paths only).
func TestStoreSaveRoundTrip(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "sessions") // does not exist yet — Save must create it
	st := NewStore(dir)

	want := domain.Session{Version: domain.SessionVersion, State: json.RawMessage(`{"k":"v"}`)}
	path, err := st.Save(want)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Errorf("saved to %q; want a file under %q", path, dir)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	got, err := domain.DecodeSession(data)
	if err != nil {
		t.Fatalf("DecodeSession: %v", err)
	}
	if got.Version != want.Version || string(got.State) != string(want.State) {
		t.Errorf("round-trip = %+v; want %+v", got, want)
	}
}

// The filename is a sortable, colon-free UTC timestamp, so a lexical directory listing is
// chronological (and valid on every filesystem).
func TestStoreFilenameIsSortableTimestamp(t *testing.T) {
	t.Parallel()
	st := NewStore(t.TempDir())
	st.now = func() time.Time { return time.Date(2026, 6, 23, 18, 30, 5, 0, time.UTC) }

	path, err := st.Save(domain.Session{Version: domain.SessionVersion})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if base := filepath.Base(path); base != "20260623T183005Z.json" {
		t.Errorf("filename = %q; want a sortable timestamp", base)
	}
}

// A non-UTC clock is normalised to UTC so filenames sort regardless of the host timezone.
func TestStoreFilenameIsUTC(t *testing.T) {
	t.Parallel()
	st := NewStore(t.TempDir())
	st.now = func() time.Time {
		return time.Date(2026, 6, 23, 18, 30, 5, 0, time.FixedZone("UTC+5", 5*60*60))
	}
	path, err := st.Save(domain.Session{Version: domain.SessionVersion})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if base := filepath.Base(path); base != "20260623T133005Z.json" {
		t.Errorf("filename = %q; want the UTC-normalised stamp 20260623T133005Z.json", base)
	}
}
