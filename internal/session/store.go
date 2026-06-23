package session

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The session store (phase-2 detail plan §4 P2.5; §6.1)
// ----------------------------------------------------------------------------

// dirPerm and filePerm scope session state to the owner: snapshots are a private record
// of a conversation, so neither the directory nor the files are group/world readable.
const (
	dirPerm  os.FileMode = 0o700
	filePerm os.FileMode = 0o600
)

// fileLayout is the snapshot filename's timestamp layout: a sortable, filesystem-safe
// UTC stamp (no colons), so a lexical directory listing is chronological. Second
// granularity is sufficient — a snapshot is written once per clean quit (collision-free
// IDs via ulid stay deferred — phase-2 detail plan §2).
const fileLayout = "20060102T150405Z"

// Store persists Agent snapshots as files under a directory (SessionsDir). It owns the
// session-file format and naming so its callers — the composition root today, the bench
// or a session browser later — never duplicate that knowledge. The TUI does not use the
// Store directly: it is handed a narrow saver closure (the host keeps file I/O out of the
// renderer — phase-2 detail plan §3 C5).
type Store struct {
	dir string
	now func() time.Time // injectable so a test controls the generated filename
}

// NewStore returns a Store rooted at dir. The directory is created lazily on the first
// Save — resolveRoots yields paths only, deferring creation to the writer that needs it
// (phase-2 detail plan §3 C7), so an apogee run that never saves touches no disk.
func NewStore(dir string) *Store {
	return &Store{dir: dir, now: time.Now}
}

// Save encodes sess and writes it to a new timestamped file under the store directory,
// creating the directory if needed. It returns the absolute path written so the caller
// can show a resume hint. Snapshots are only valid at a quiescent boundary (ADR 0007);
// enforcing that is the caller's responsibility — Store persists whatever it is given.
func (s *Store) Save(sess domain.Session) (string, error) {
	if err := os.MkdirAll(s.dir, dirPerm); err != nil {
		return "", fmt.Errorf("apogee: create sessions directory %q: %w", s.dir, err)
	}
	data, err := sess.Encode()
	if err != nil {
		return "", fmt.Errorf("apogee: encode session: %w", err)
	}
	path := filepath.Join(s.dir, s.now().UTC().Format(fileLayout)+".json")
	if err := os.WriteFile(path, data, filePerm); err != nil {
		return "", fmt.Errorf("apogee: write session %q: %w", path, err)
	}
	return path, nil
}
