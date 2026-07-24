package session

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// dirPerm and filePerm scope session state to the owner: records are a private history of
// conversations, so neither the directory nor the files are group/world readable.
const (
	dirPerm  os.FileMode = 0o700
	filePerm os.FileMode = 0o600
)

// fileLayout is the id/filename timestamp layout: a sortable, filesystem-safe UTC stamp
// (no colons) so a lexical directory listing is chronological. Second granularity plus the
// random suffix in NewID keeps ids collision-safe across concurrent instances.
const fileLayout = "20060102T150405Z"

// idRandomBytes is the count of random bytes hex-encoded into a session id's suffix. Four
// bytes (eight hex chars) make same-second collisions across instances negligible.
const idRandomBytes = 4

// RecordVersion is the schema version Save stamps on every wrapper it writes. Decoding a
// higher version fails with ErrRecordVersion — the same reject-forward rule the engine
// applies to domain.SessionVersion, but owned by this layer (only internal/session
// versions the wrapper; the engine versions the Session payload; the TUI versions the
// Transcript blob).
const RecordVersion = 1

// ErrRecordVersion is returned when a stored record's RecordVersion exceeds this build's
// RecordVersion — a record written by a newer Apogee, refused rather than misread.
var ErrRecordVersion = errors.New("apogee: unsupported session record version")

// Meta is the browsable summary of one stored session — everything the history browser
// shows without decoding the conversation.
type Meta struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Workspace string    `json:"workspace,omitempty"` // resolved root; "" on legacy records
	Model     string    `json:"model,omitempty"`
	UserMsgs  int       `json:"userMsgs"`
	CtxUsed   int       `json:"ctxUsed,omitempty"` // last observed context fill, for the gauge relight
}

// Record is the on-disk shape: the metadata wrapper around the two opaque payloads.
// Transcript is the TUI's versioned scrollback blob (opaque here, exactly as
// Session.State is opaque to domain); Session is the untouched engine envelope.
type Record struct {
	RecordVersion int             `json:"recordVersion"`
	Meta          Meta            `json:"meta"`
	Transcript    json.RawMessage `json:"transcript,omitempty"`
	Session       domain.Session  `json:"session"`
}

// Store persists session Records as id-addressed files under a directory (SessionsDir). It
// owns the on-disk format and naming so its callers — the composition root's host adapter,
// the history browser — never duplicate that knowledge. Writes are atomic (temp file +
// rename), so a crash never leaves a truncated record behind.
type Store struct {
	dir string
	now func() time.Time // injectable so a test controls Rename's UpdatedAt stamp
}

// NewStore returns a Store rooted at dir. The directory is created lazily on the first Save
// (resolveRoots yields paths only, deferring creation to the writer that needs it), so an
// apogee run that never saves touches no disk.
func NewStore(dir string) *Store {
	return &Store{dir: dir, now: time.Now}
}

// NewID mints a chronologically-sortable, collision-safe session id of the form
// "20060102T150405Z-xxxxxxxx": a UTC second stamp plus idRandomBytes of hex randomness.
// The stamp keeps a lexical directory listing chronological; the random suffix makes ids
// unique across concurrent instances minting within the same second.
func NewID(now time.Time) string { return newID(now, cryptorand.Reader) }

// newID is the test seam behind NewID: an explicit randomness source lets a test pin the
// suffix. A short read from rand falls back to a zeroed suffix rather than failing — an id
// is best-effort unique, never a correctness gate.
func newID(now time.Time, rand io.Reader) string {
	buf := make([]byte, idRandomBytes)
	_, _ = io.ReadFull(rand, buf)
	return now.UTC().Format(fileLayout) + "-" + hex.EncodeToString(buf)
}

// Save writes rec to <dir>/<rec.Meta.ID>.json atomically (temp file in the same dir +
// os.Rename), creating the directory if needed and stamping RecordVersion. The same ID
// overwrites its own file (update-in-place). Save never mints ids or edits Meta beyond the
// version stamp — cadence and metadata policy live with the caller.
func (s *Store) Save(rec Record) error {
	if rec.Meta.ID == "" {
		return errors.New("apogee: cannot save a session record with an empty id")
	}
	if err := os.MkdirAll(s.dir, dirPerm); err != nil {
		return fmt.Errorf("apogee: create sessions directory %q: %w", s.dir, err)
	}
	rec.RecordVersion = RecordVersion
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("apogee: encode session record: %w", err)
	}
	path := filepath.Join(s.dir, rec.Meta.ID+".json")
	if err := atomicWrite(path, data); err != nil {
		return err
	}
	return nil
}

// List reads every *.json in the store, decodes each to its Meta, and returns them sorted
// UpdatedAt descending. A file that fails to decode — corrupt, or a foreign file that
// wandered into the directory — is skipped, so one bad record never kills the browser. A
// missing store directory is an empty list, not an error.
func (s *Store) List() ([]Meta, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("apogee: read sessions directory %q: %w", s.dir, err)
	}
	metas := make([]Meta, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		rec, err := s.loadPath(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue // soft-skip: corrupt or foreign files must not fail the whole list
		}
		metas = append(metas, rec.Meta)
	}
	sort.SliceStable(metas, func(i, j int) bool {
		return metas[i].UpdatedAt.After(metas[j].UpdatedAt)
	})
	return metas, nil
}

// Load returns the record stored under id (<dir>/<id>.json), wrapping a pre-plan bare
// envelope on the fly. The returned Record.Session is always resumable by the engine.
func (s *Store) Load(id string) (Record, error) { return s.loadPath(filepath.Join(s.dir, id+".json")) }

// LoadPath returns the record at an explicit path — the read path --resume uses when given
// a file rather than an id. Legacy bare envelopes are wrapped identically to Load.
func (s *Store) LoadPath(path string) (Record, error) { return s.loadPath(path) }

// loadPath reads and decodes one record file, shared by List, Load, and LoadPath.
func (s *Store) loadPath(path string) (Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Record{}, fmt.Errorf("apogee: read session %q: %w", path, err)
	}
	return decodeRecord(data, path)
}

// Delete removes the record file for id.
func (s *Store) Delete(id string) error {
	if err := os.Remove(filepath.Join(s.dir, id+".json")); err != nil {
		return fmt.Errorf("apogee: delete session %q: %w", id, err)
	}
	return nil
}

// Rename sets a stored session's Title and re-stamps its UpdatedAt, persisting through the
// same atomic Save. Renaming a legacy bare envelope upgrades it to a wrapped record.
func (s *Store) Rename(id, title string) error {
	rec, err := s.Load(id)
	if err != nil {
		return err
	}
	rec.Meta.Title = title
	rec.Meta.UpdatedAt = s.now().UTC()
	return s.Save(rec)
}

// decodeRecord turns raw file bytes into a Record. It first sniffs for the recordVersion
// key: present means a wrapper (version-checked here — this layer's forward-reject);
// absent but carrying a bare domain.Session's Version/State keys means a pre-plan file,
// wrapped with synthetic metadata so legacy sessions still list, load, and resume.
func decodeRecord(data []byte, path string) (Record, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return Record{}, fmt.Errorf("apogee: decode session %q: %w", path, err)
	}
	if _, wrapped := probe["recordVersion"]; !wrapped {
		return wrapLegacy(data, probe, path)
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return Record{}, fmt.Errorf("apogee: decode session record %q: %w", path, err)
	}
	if rec.RecordVersion > RecordVersion {
		return Record{}, ErrRecordVersion
	}
	return rec, nil
}

// wrapLegacy adapts a pre-plan bare domain.Session file — {"Version":N,"State":...} — into
// a Record: synthetic Meta (id and title from the filename stem, timestamps from the file
// mtime, workspace unknown), empty Transcript (no scrollback was recorded), and the
// untouched Session. A JSON object that is neither a wrapper nor an envelope is rejected so
// stray files are not silently accepted as sessions.
func wrapLegacy(data []byte, probe map[string]json.RawMessage, path string) (Record, error) {
	_, hasVersion := probe["Version"]
	_, hasState := probe["State"]
	if !hasVersion || !hasState {
		return Record{}, fmt.Errorf("apogee: %q is neither a session record nor a legacy envelope", path)
	}
	var sess domain.Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return Record{}, fmt.Errorf("apogee: decode legacy session %q: %w", path, err)
	}
	stem := strings.TrimSuffix(filepath.Base(path), ".json")
	when := legacyTime(path)
	return Record{
		RecordVersion: RecordVersion,
		Meta: Meta{
			ID:        stem,
			Title:     "Session " + stem,
			CreatedAt: when,
			UpdatedAt: when,
		},
		Session: sess,
	}, nil
}

// legacyTime reads a legacy file's mtime as its synthetic created/updated stamp; a stat
// failure degrades to the zero time rather than failing the load.
func legacyTime(path string) time.Time {
	if info, err := os.Stat(path); err == nil {
		return info.ModTime().UTC()
	}
	return time.Time{}
}

// atomicWrite writes data to a temp file in path's directory and renames it into place, so
// a reader never observes a partially-written record and a crash leaves no truncated file.
// The temp file is removed on any failure and never survives a successful rename.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".apogee-session-*.tmp")
	if err != nil {
		return fmt.Errorf("apogee: create temp session file in %q: %w", dir, err)
	}
	tmpName := tmp.Name()
	// Remove the temp file on every path except a successful rename (where it no longer
	// exists, so Remove is a harmless no-op).
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(filePerm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("apogee: chmod temp session file %q: %w", tmpName, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("apogee: write temp session file %q: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("apogee: close temp session file %q: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("apogee: rename session file into %q: %w", path, err)
	}
	return nil
}
