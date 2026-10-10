package adoption

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/airiclenz/apogee/internal/platform"
)

// dirPerm and filePerm scope the adoption records to the owner: they decide what a repository's
// committed config may grant on this machine, so no other account may read — or, above all,
// write — them.
const (
	dirPerm  os.FileMode = 0o700
	filePerm os.FileMode = 0o600
)

const (
	// recordExt is the extension of a Project root's record file.
	recordExt = ".yaml"

	// recordLockSuffix names the lock beside a record file that serialises its rewrites. Like every
	// platform lock file it is never removed.
	recordLockSuffix = ".lock"

	// configLockExt names the Project config writer's lock for a root, kept beside that root's
	// record so the repository never holds one.
	configLockExt = ".config.lock"

	// lockTimeout bounds how long a writer queues behind another. A holder keeps the lock for one
	// read-rewrite-rename, so a writer still waiting after this long faces a stuck holder.
	lockTimeout = 5 * time.Second
)

// State is the user's recorded answer to one granting entry.
type State string

const (
	// Proposed is an entry with no recorded answer: inert until the user adopts it. It is never
	// written to a record — absence is what spells it.
	Proposed State = "proposed"

	// Adopted is an entry the user accepted: live while its text stays exactly as adopted.
	Adopted State = "adopted"

	// Rejected is an entry the user turned down: inert, and not offered again until it changes.
	Rejected State = "rejected"
)

// Entry is one granting entry of a Project config: the granting key it sits under (Kind — say
// "terminal" or "mcp-servers") and its exact text. The kind is part of the pin, so the same text
// under another key is a different entry.
type Entry struct {
	Kind string
	Text string
}

// Fingerprint is the pin an answer to entry e is recorded under: the hex SHA-256 of its kind, a NUL
// and its text, byte for byte. Nothing is normalised — an entry whose text changes in any way,
// whitespace included, is a new entry (ADR 0096 §4: pinned to the entry's exact content).
func Fingerprint(e Entry) string {
	sum := sha256.Sum256([]byte(e.Kind + "\x00" + e.Text))
	return hex.EncodeToString(sum[:])
}

// Classification sorts a Project config's granting entries by the user's recorded answer, each list
// in the order the entries were given.
type Classification struct {
	Adopted  []Entry
	Proposed []Entry
	Rejected []Entry
}

// record is the on-disk shape of one Project root's answers. Root makes the file self-describing,
// so a record read under the wrong name is refused rather than trusted. Entries keeps every value
// it finds: only "adopted" and "rejected" are answers, and anything else reads as no answer.
type record struct {
	Root    string            `yaml:"root"`
	Entries map[string]string `yaml:"entries,omitempty"`
}

// Store is the adoption record of ONE Project root, bound at construction. Resolving the root, and
// the directory the record lives in, is the caller's job (ADR 0001).
type Store struct {
	root string
	path string
}

// New returns the Store recording answers for projectRoot under dir. The root is resolved to an
// absolute, symlink-free path first, so one folder reached by two spellings shares one record, and
// the same repository at another path asks again. Nothing is written until the first answer.
//
// Errors: an error when dir or projectRoot is empty, or when the root cannot be resolved (a folder
// that does not exist).
func New(dir, projectRoot string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("apogee: adoption store: no record directory is known")
	}
	root, digest, err := key(projectRoot)
	if err != nil {
		return nil, err
	}
	return &Store{root: root, path: filepath.Join(dir, digest+recordExt)}, nil
}

// ConfigLockPath is where the Project config writer for projectRoot keeps its lock: beside the
// root's adoption record under dir, never in the repository. It resolves the root as [New] does.
//
// Errors: as [New].
func ConfigLockPath(dir, projectRoot string) (string, error) {
	if dir == "" {
		return "", errors.New("apogee: project config lock: no record directory is known")
	}
	_, digest, err := key(projectRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, digest+configLockExt), nil
}

// key resolves projectRoot to the absolute, symlink-free path a record is keyed on and returns it
// with its full hex SHA-256 — the record's file name, which leaks neither the project's name nor
// its location.
func key(projectRoot string) (root, digest string, err error) {
	if projectRoot == "" {
		return "", "", errors.New("apogee: adoption store: no Project root is known")
	}
	abs, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", "", fmt.Errorf("apogee: adoption store: resolve Project root %q: %w", projectRoot, err)
	}
	root, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return "", "", fmt.Errorf("apogee: adoption store: resolve Project root %q: %w", projectRoot, err)
	}
	sum := sha256.Sum256([]byte(root))
	return root, hex.EncodeToString(sum[:]), nil
}

// Root is the symlink-resolved Project root the store records answers for.
func (s *Store) Root() string { return s.root }

// Path is the record file the store reads and writes.
func (s *Store) Path() string { return s.path }

// Classify sorts entries by the answers on record. A missing record classifies everything as
// proposed, which is the first run in a fresh clone.
//
// Errors: an error naming the record when it cannot be read, does not parse, or names another
// Project root. A caller that cannot classify must treat every entry as proposed — inert.
func (s *Store) Classify(entries []Entry) (Classification, error) {
	rec, err := s.read()
	if err != nil {
		return Classification{}, err
	}
	var c Classification
	for _, e := range entries {
		switch State(rec.Entries[Fingerprint(e)]) {
		case Adopted:
			c.Adopted = append(c.Adopted, e)
		case Rejected:
			c.Rejected = append(c.Rejected, e)
		default:
			c.Proposed = append(c.Proposed, e)
		}
	}
	return c, nil
}

// Adopt records entries as adopted, replacing any earlier answer to the same text.
//
// Errors: as [Store.Classify], plus an error when the record cannot be locked or written; nothing
// is recorded then.
func (s *Store) Adopt(entries ...Entry) error {
	return s.update(func(rec *record) {
		for _, e := range entries {
			rec.Entries[Fingerprint(e)] = string(Adopted)
		}
	})
}

// Reject records entries as rejected, replacing any earlier answer to the same text.
//
// Errors: as [Store.Adopt].
func (s *Store) Reject(entries ...Entry) error {
	return s.update(func(rec *record) {
		for _, e := range entries {
			rec.Entries[Fingerprint(e)] = string(Rejected)
		}
	})
}

// Forget drops any answer to entries, so they are proposed again — what removing a rule from the
// Project config leaves behind.
//
// Errors: as [Store.Adopt].
func (s *Store) Forget(entries ...Entry) error {
	return s.update(func(rec *record) {
		for _, e := range entries {
			delete(rec.Entries, Fingerprint(e))
		}
	})
}

// update is the one read-modify-write of the record: take the lock beside it, read it, apply
// change, and replace the file atomically — or, when change leaves nothing recorded and there is
// no file yet, write nothing.
func (s *Store) update(change func(*record)) error {
	if err := os.MkdirAll(filepath.Dir(s.path), dirPerm); err != nil {
		return fmt.Errorf("apogee: create adoption directory %q: %w", filepath.Dir(s.path), err)
	}
	release, err := platform.AcquireLockWait(s.path+recordLockSuffix, lockTimeout)
	if err != nil {
		return fmt.Errorf("apogee: lock adoption record %q: %w", s.path, err)
	}
	defer release()

	rec, err := s.read()
	if err != nil {
		return err
	}
	if rec.Entries == nil {
		rec.Entries = make(map[string]string)
	}
	change(&rec)
	data, err := yaml.Marshal(rec)
	if err != nil {
		return fmt.Errorf("apogee: encode adoption record %q: %w", s.path, err)
	}
	return atomicWrite(s.path, data)
}

// read loads the record, or an empty one bound to this root when there is no file yet.
func (s *Store) read() (record, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return record{Root: s.root}, nil
	}
	if err != nil {
		return record{}, fmt.Errorf("apogee: read adoption record %q: %w", s.path, err)
	}
	var rec record
	if err := yaml.Unmarshal(data, &rec); err != nil {
		return record{}, fmt.Errorf("apogee: read adoption record %q: %w", s.path, err)
	}
	if rec.Root == "" && len(rec.Entries) == 0 {
		return record{Root: s.root}, nil // an empty file records nothing
	}
	if rec.Root != s.root {
		return record{}, fmt.Errorf("apogee: adoption record %q belongs to Project root %q, not %q; "+
			"remove it to be asked again", s.path, rec.Root, s.root)
	}
	return rec, nil
}

// atomicWrite writes data to a temp file beside path and renames it into place with filePerm, so a
// reader never observes a half-written record and a crash leaves the previous one intact. The temp
// file is removed on every path except a successful rename.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".apogee-adoption-*.tmp")
	if err != nil {
		return fmt.Errorf("apogee: create temp adoption record in %q: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(filePerm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("apogee: chmod temp adoption record %q: %w", tmpName, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("apogee: write temp adoption record %q: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("apogee: close temp adoption record %q: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("apogee: rename adoption record into %q: %w", path, err)
	}
	return nil
}
