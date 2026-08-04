package recall

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// dirPerm and filePerm scope recall state to the owner: recorded prompts are the operator's
// own words — often naming private paths, hosts, or intentions — so neither the directory nor
// the files are group/world readable (internal/session's constants, same reasoning).
const (
	dirPerm  os.FileMode = 0o700
	filePerm os.FileMode = 0o600
)

// maxEntries is how many of the newest entries Load returns and compaction keeps: far more
// than an Up-arrow walk exhausts in a session, small enough that reading the whole file on
// every append stays cheap.
const maxEntries = 1000

// compactAt is the line count past which Append rewrites the file down to maxEntries. Leaving
// maxEntries of slack above the cap makes compaction rare — one rewrite per maxEntries
// appends — rather than one rewrite per append once the cap is reached.
const compactAt = 2 * maxEntries

// nameHexLen is how many hex characters of the workspace digest name its file: short enough
// to keep the directory legible, and a collision is not a correctness problem because every
// record names its workspace and Load drops the strangers.
const nameHexLen = 8

// fileExt is the extension of a workspace's recall file. JSON Lines, not JSON: appending is a
// single write with no read-modify-write of a surrounding envelope.
const fileExt = ".jsonl"

// record is one line of a workspace's recall file. Workspace makes the file self-describing —
// it is what lets Load filter out the records of a digest-colliding stranger — and the JSON
// string encoding of Text keeps a multi-line prompt on one line.
type record struct {
	Workspace string `json:"ws"`
	At        string `json:"t"`
	Text      string `json:"text"`
}

// Store is the file-backed prompt recall: one JSONL file per workspace under an injected
// directory. It is process-local — the mutex serializes this process's appends and loads, but
// the Store makes no cross-process locking claims in v1 (internal/library's posture). It NEVER
// reaches for an ambient ~/.apogee: the caller supplies dir (ADR 0001).
type Store struct {
	mu  sync.Mutex
	dir string
}

// New returns a Store rooted at dir. The directory is created lazily on the first Append, so
// an apogee run that sends nothing touches no disk.
func New(dir string) *Store {
	return &Store{dir: dir}
}

// Append records text as workspace's newest prompt. It writes nothing when text already is
// that workspace's newest entry (consecutive dedup) and compacts the file once it grows past
// compactAt lines. workspace is an absolute path; the caller resolves it (ADR 0001).
func (s *Store) Append(workspace, text string) error {
	ws := normalize(workspace)

	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.path(ws)
	recs, lines, err := readRecords(path)
	if err != nil {
		return err
	}
	if newest, ok := newestFor(recs, ws); ok && newest == text {
		return nil
	}
	rec := record{Workspace: ws, At: time.Now().UTC().Format(time.RFC3339), Text: text}
	line, err := encodeRecord(rec)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, dirPerm); err != nil {
		return fmt.Errorf("apogee: create recall dir %q: %w", s.dir, err)
	}
	if err := appendLine(path, line); err != nil {
		return err
	}
	if lines+1 > compactAt {
		return compact(path, append(recs, rec))
	}
	return nil
}

// Load returns workspace's recorded prompts oldest→newest, at most the newest maxEntries. It
// is total by design — recall is a convenience, never a reason a session fails: a missing file
// loads empty, malformed lines are skipped, and records naming another workspace (a digest
// collision) are filtered out. Only an unreadable file is an error.
func (s *Store) Load(workspace string) ([]string, error) {
	ws := normalize(workspace)

	s.mu.Lock()
	defer s.mu.Unlock()

	recs, _, err := readRecords(s.path(ws))
	if err != nil {
		return nil, err
	}
	texts := make([]string, 0, len(recs))
	for _, rec := range recs {
		if rec.Workspace != ws {
			continue
		}
		texts = append(texts, rec.Text)
	}
	if len(texts) > maxEntries {
		texts = texts[len(texts)-maxEntries:]
	}
	return texts, nil
}

// path is the file a workspace's entries live in: the digest of the normalized path, so the
// filename leaks neither the project's name nor its location.
func (s *Store) path(ws string) string {
	sum := sha256.Sum256([]byte(ws))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:])[:nameHexLen]+fileExt)
}

// normalize is the single spelling of a workspace the store keys and stamps records on, so
// that "/w" and "/w/" are one workspace rather than two files. It cleans only — resolving a
// relative path against a process-wide cwd is the caller's job (ADR 0001).
func normalize(workspace string) string {
	return filepath.Clean(workspace)
}

// newestFor returns the last entry recorded for ws, which is what consecutive dedup compares
// against. It scans backwards past any colliding stranger's records so dedup answers for this
// workspace's view of the file — the same view Load returns.
func newestFor(recs []record, ws string) (string, bool) {
	for i := len(recs) - 1; i >= 0; i-- {
		if recs[i].Workspace == ws {
			return recs[i].Text, true
		}
	}
	return "", false
}

// readRecords reads path and returns its well-formed records in file order plus the raw line
// count (which compaction triggers on, so a file of nothing but junk still gets trimmed). A
// missing file is not an error: a workspace with no recall yet is the common case.
func readRecords(path string) ([]record, int, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("apogee: read recall file %q: %w", path, err)
	}
	if len(data) == 0 {
		return nil, 0, nil
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	recs := make([]record, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		recs = append(recs, rec)
	}
	return recs, len(lines), nil
}

// encodeRecord marshals one record into its on-disk line, newline included.
func encodeRecord(rec record) ([]byte, error) {
	line, err := json.Marshal(rec)
	if err != nil {
		return nil, fmt.Errorf("apogee: encode recall entry: %w", err)
	}
	return append(line, '\n'), nil
}

// appendLine appends one encoded record to path, creating the file if it does not exist.
func appendLine(path string, line []byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, filePerm)
	if err != nil {
		return fmt.Errorf("apogee: open recall file %q: %w", path, err)
	}
	if _, err := f.Write(line); err != nil {
		_ = f.Close()
		return fmt.Errorf("apogee: append recall entry to %q: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("apogee: close recall file %q: %w", path, err)
	}
	return nil
}

// compact rewrites path with only the newest maxEntries records, dropping the malformed lines
// readRecords already skipped. Callers pass the file's full record list including the entry
// just appended.
func compact(path string, recs []record) error {
	if len(recs) > maxEntries {
		recs = recs[len(recs)-maxEntries:]
	}
	var buf bytes.Buffer
	for _, rec := range recs {
		line, err := encodeRecord(rec)
		if err != nil {
			return err
		}
		buf.Write(line)
	}
	return atomicWrite(path, buf.Bytes())
}

// atomicWrite writes data to a temp file in path's directory and renames it into place, so a
// reader never observes a half-compacted file and a crash mid-compaction leaves the previous
// one intact. The temp file is removed on every path except a successful rename.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".apogee-recall-*.tmp")
	if err != nil {
		return fmt.Errorf("apogee: create temp recall file in %q: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(filePerm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("apogee: chmod temp recall file %q: %w", tmpName, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("apogee: write temp recall file %q: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("apogee: close temp recall file %q: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("apogee: rename recall file into %q: %w", path, err)
	}
	return nil
}
