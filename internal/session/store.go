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
	"sync"
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

// ErrInvalidID is returned when a session id cannot address a file inside the store: an id is
// joined with the store directory to form a path, so anything that is not a single, safe
// filename component would let a record's own contents choose where Apogee writes and deletes.
// A record read from an untrusted file declares its id, which makes this a security boundary,
// not a tidiness check.
var ErrInvalidID = errors.New("apogee: invalid session id")

// maxIDLen bounds an id so that <id>.json stays inside the 255-byte name limit every
// mainstream filesystem enforces. Minted ids are 25 bytes; the slack is for legacy stems.
const maxIDLen = 200

// validateID reports whether id is usable as this store's filename stem: non-empty, no longer
// than maxIDLen, a single path component (no separator of either OS, no ".", "..", traversal,
// or absolute path), not dot-prefixed, and free of control and bidi-formatting characters. Minted
// ids (NewID) and legacy filename stems both satisfy it; a planted "../../.claude/settings" does
// not.
func validateID(id string) error {
	switch {
	case id == "":
		return fmt.Errorf("%w: empty", ErrInvalidID)
	case len(id) > maxIDLen:
		return fmt.Errorf("%w %q: longer than %d bytes", ErrInvalidID, id, maxIDLen)
	case strings.HasPrefix(id, "."):
		return fmt.Errorf("%w %q: must not start with a dot", ErrInvalidID, id)
	case id != filepath.Base(id) || strings.ContainsAny(id, `/\`):
		return fmt.Errorf("%w %q: must be a single filename component", ErrInvalidID, id)
	case strings.ContainsFunc(id, func(r rune) bool { return r < 0x20 || r == 0x7f || bidiControl(r) }):
		return fmt.Errorf("%w %q: contains a control or bidi-formatting character", ErrInvalidID, id)
	}
	return nil
}

// bidiControl reports whether r is one of the Unicode bidirectional formatting characters — the
// embeddings and overrides U+202A-U+202E, the isolates U+2066-U+2069, and the two marks U+200E and
// U+200F. They reorder the glyphs around them while leaving the bytes alone, so an id carrying one
// names a file whose displayed stem is not the stem on disk: the history browser's row, the
// deletion confirmation and the resumed path would read in an order the record does not have.
// Refused here rather than stripped, because an id is an identity, not prose — the same posture as
// the newline and the NUL beside it.
//
// Deliberately the bidi set and NOT the whole of unicode.Cf, matching the display seam this backs
// onto (internal/tui.stripEscapes) and internal/title.strippableControl; Cf also holds U+200D ZWJ
// and U+00AD soft hyphen, which are legitimate in text a person wrote. The seams where untrusted
// bytes ARRIVE or are STORED as prose (internal/tools' neuterInert, internal/library's sanitize)
// drop all of Cf on purpose — that asymmetry is intended, not an inconsistency to repair.
func bidiControl(r rune) bool {
	return r == '\u200e' || r == '\u200f' || // LRM, RLM
		(r >= '\u202a' && r <= '\u202e') || // LRE, RLE, PDF, LRO, RLO
		(r >= '\u2066' && r <= '\u2069') // LRI, RLI, FSI, PDI
}

// Meta is the browsable summary of one stored session — everything the history browser
// shows without decoding the conversation.
//
// ScheduleID and ScheduleName are the schedule identity (ADR 0033): both empty on an
// ordinary session, both set on a record that is one Firing of a Schedule. They are
// deliberately NOT a RecordVersion bump — the addition is compatible in both directions,
// since an older build ignores the unknown keys on load and simply never writes them.
type Meta struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	Workspace    string    `json:"workspace,omitempty"` // resolved root; "" on legacy records
	Model        string    `json:"model,omitempty"`
	ScheduleID   string    `json:"scheduleID,omitempty"`   // "" unless this record is a Firing
	ScheduleName string    `json:"scheduleName,omitempty"` // the Schedule's display name
	UserMsgs     int       `json:"userMsgs"`
	CtxUsed      int       `json:"ctxUsed,omitempty"` // last observed context fill, for the gauge relight
	// Usage is the MAIN agent's cumulative token accounting as of this Save — the latest reading
	// its Driver took off a UsageEvent, not a sum the store computed. It is restored on reopen, so
	// a session says what it has spent instead of reporting nothing until its next completion. A
	// sub-agent's totals are not here: they ride in the Driver's own transcript blob, on the run
	// head each child filled. Like ScheduleID above it is deliberately NOT a RecordVersion bump —
	// a record written before it existed decodes to the zero Usage (a pre-feature session simply
	// reports zero), and an older build ignores a key it cannot place.
	Usage Usage `json:"usage,omitzero"`
}

// Usage is one agent's cumulative token accounting over a session: how many completions it
// accounted for and the prompt, completion and total tokens they carried. It mirrors the
// Cumulative* fields an engine UsageEvent stamps (domain.UsageEvent) and keeps their latest-wins
// rule — the emitting agent owns the running sum, an observer only ever holds its newest reading —
// so storing it is a copy of that reading rather than a total the store re-derives.
type Usage struct {
	Calls            int `json:"calls,omitempty"`
	PromptTokens     int `json:"promptTokens,omitempty"`
	CompletionTokens int `json:"completionTokens,omitempty"`
	TotalTokens      int `json:"totalTokens,omitempty"`
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

// Store persists session Records as id-addressed files under a directory (the sessions root its caller resolves). It
// owns the on-disk format and naming so its callers — the composition root's host adapter,
// the history browser — never duplicate that knowledge. Writes are atomic (temp file +
// rename), so a crash never leaves a truncated record behind, and serialized (mu), so two
// concurrent writers never roll one another's record back.
type Store struct {
	dir string
	now func() time.Time // injectable so a test controls Rename's UpdatedAt stamp

	// mu serializes the three writers — Save, Rename and Delete — against each other. Rename is
	// why it exists: its Load→retitle→Save is a read-modify-write over the WHOLE record, so a Save
	// landing between its read and its write is silently rolled back, taking a complete Turn (the
	// engine Session and the TUI transcript blob alike) off disk. The TUI serializes its own record
	// writes at the fold layer as well; this is the floor under every caller in the process,
	// including a store two goroutines reach without going through that fold.
	//
	// The readers (List, Load, LoadPath) stay unguarded: atomicWrite renames a complete file into
	// place, so a reader sees either the whole old record or the whole new one, never a torn one.
	mu sync.Mutex
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
// version stamp — cadence and metadata policy live with the caller. An id that is not a safe
// filename component is refused with ErrInvalidID: the write must stay inside s.dir even when
// the id travelled in from a record the user resumed by path.
func (s *Store) Save(rec Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(rec)
}

// save is Save's body without the store lock, so Rename can hold that lock across its whole
// read-modify-write instead of dropping it between the read and the write. Callers must hold s.mu.
func (s *Store) save(rec Record) error {
	if err := validateID(rec.Meta.ID); err != nil {
		return err
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
// envelope on the fly. The returned Record.Session is always resumable by the engine. An id
// that is not a safe filename component is refused with ErrInvalidID rather than read from
// outside the store — LoadPath is the deliberate way to read a record by path.
func (s *Store) Load(id string) (Record, error) {
	if err := validateID(id); err != nil {
		return Record{}, err
	}
	return s.loadPath(filepath.Join(s.dir, id+".json"))
}

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

// Delete removes the record file for id. An id that is not a safe filename component is
// refused with ErrInvalidID, so a record's declared id can never aim the browser's delete at a
// file outside the store.
func (s *Store) Delete(id string) error {
	if err := validateID(id); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(filepath.Join(s.dir, id+".json")); err != nil {
		return fmt.Errorf("apogee: delete session %q: %w", id, err)
	}
	return nil
}

// Rename sets a stored session's Title and re-stamps its UpdatedAt, persisting through the
// same atomic write. Renaming a legacy bare envelope upgrades it to a wrapped record.
//
// The read and the write happen under ONE hold of the store lock, which is the point: a Save that
// slipped between them would be overwritten by the record this call read before it — reverting the
// session by a whole Turn, since Save replaces the record wholesale (audit 2026-08-01).
func (s *Store) Rename(id, title string) error {
	if err := validateID(id); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadPath(filepath.Join(s.dir, id+".json"))
	if err != nil {
		return err
	}
	rec.Meta.Title = title
	rec.Meta.UpdatedAt = s.now().UTC()
	return s.save(rec)
}

// decodeRecord turns raw file bytes into a Record and refuses one whose id could not address a
// file inside a store. The id is the only Meta field that becomes a path — every later Save and
// Delete names <id>.json — so a file that declares a hostile id is rejected here, at the door,
// rather than trusted by the writers downstream. List soft-skips such a file like any other it
// cannot decode.
func decodeRecord(data []byte, path string) (Record, error) {
	rec, err := decodeAnyRecord(data, path)
	if err != nil {
		return Record{}, err
	}
	if err := validateID(rec.Meta.ID); err != nil {
		return Record{}, fmt.Errorf("apogee: session %q: %w", path, err)
	}
	return rec, nil
}

// decodeAnyRecord decodes either on-disk shape. It first sniffs for the recordVersion
// key: present means a wrapper (version-checked here — this layer's forward-reject);
// absent but carrying a bare domain.Session's Version/State keys means a pre-plan file,
// wrapped with synthetic metadata so legacy sessions still list, load, and resume.
func decodeAnyRecord(data []byte, path string) (Record, error) {
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
