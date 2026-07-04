package library

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The Library store (phase-4 detail plan §13; ported from apogee-sim @pin)
// ----------------------------------------------------------------------------

// StoreVersion is the schema version Save stamps and Load accepts. A file whose Version
// exceeds this is from a newer build; Load rejects it as a soft error (ErrStoreVersion) and
// degrades to an empty store rather than bricking the run — the Library is best-effort
// learning, not a user's session (contrast domain.DecodeSession, which hard-rejects).
const StoreVersion = 1

const (
	storeFileName = "library.json"

	// dirPerm and filePerm scope the Library to the owner: learned per-model observations are
	// a private record, so neither the directory nor the file is group/world readable (the
	// same posture as internal/session).
	dirPerm  os.FileMode = 0o700
	filePerm os.FileMode = 0o600

	// defaultTTLHours expires an observation a week after it was created, so a store left
	// running for months does not inject on stale evidence (ported from the sim: 7 days).
	defaultTTLHours = 168

	// defaultMaxEntries bounds the in-memory store so a long-lived Library cannot grow without
	// limit; the lowest-scoring, least-recently-used entries are evicted past the cap.
	defaultMaxEntries = 500

	// minQueryScore and minQueryObservations gate what Query returns: an entry must have been
	// seen at least twice and still score above the prior, so a single stray observation never
	// qualifies for injection (ported from the sim's Query thresholds).
	minQueryScore        = 0.5
	minQueryObservations = 2
)

// ErrStoreVersion is folded into Load's soft error when the on-disk store was written by a
// newer build than this one understands. Load still returns a usable (empty) store.
var ErrStoreVersion = errors.New("apogee: unsupported library store schema version")

// persisted is the on-disk envelope: a schema Version plus the flat list of entries. Storing
// the whole store in one versioned file (rather than a file per entry) mirrors
// domain.Session's envelope-and-version discipline and keeps load/persist process-local.
type persisted struct {
	Version int      `json:"version"`
	Entries []*Entry `json:"entries"`
}

// Store is the file-backed Library: per-fingerprint observations with Bayesian confidence
// counts, rooted at an injected directory. It is process-local — the mutex guards the
// in-memory map for concurrent goroutines within one process, but the store makes no
// cross-process locking claims in v1 (two processes on one dir may last-writer-win). It NEVER
// reaches for an ambient ~/.apogee: the caller supplies dir (ADR 0001).
type Store struct {
	mu      sync.RWMutex
	dir     string
	entries map[string]*Entry
	now     func() time.Time // injectable so a test controls timestamps and TTL
}

// NewStore returns an empty Store rooted at dir. The directory is created lazily on the first
// Save, so an apogee run that never records touches no disk. Call Load to populate it from an
// existing store file.
func NewStore(dir string) *Store {
	return &Store{
		dir:     dir,
		entries: make(map[string]*Entry),
		now:     time.Now,
	}
}

// Load reads the store file under the injected directory into memory, dropping expired
// entries and evicting past the cap. A missing store is not an error (a fresh install has no
// Library yet). An unreadable, malformed, or too-new store degrades to an empty store and is
// returned as a soft error — the caller logs it and proceeds, matching the skills-catalog
// posture (a corrupt Library never signals "unusable"). Load leaves the store empty on any
// soft error, so a partially-parsed file never injects garbage.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries = make(map[string]*Entry)

	data, err := os.ReadFile(s.storePath())
	if errors.Is(err, os.ErrNotExist) {
		return nil // no Library yet — an empty store is the correct, non-error result
	}
	if err != nil {
		return fmt.Errorf("apogee: read library store %q: %w", s.storePath(), err)
	}

	var p persisted
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("apogee: decode library store %q: %w", s.storePath(), err)
	}
	if p.Version > StoreVersion {
		return fmt.Errorf("apogee: library store %q is version %d: %w", s.storePath(), p.Version, ErrStoreVersion)
	}

	now := s.now()
	for _, e := range p.Entries {
		if e == nil || e.ID == "" || e.Expired(now) {
			continue
		}
		s.entries[e.ID] = e
	}
	s.evictExcess()
	return nil
}

// Record adds or reinforces an observation for the fingerprint. A matching entry (same
// fingerprint label, category, and tags) has its observation count bumped and content
// refreshed; otherwise a new entry is created. It persists the store and returns the entry ID.
// A zero fingerprint (unidentified model) is inert: nothing is recorded and the empty ID is
// returned, so a caller that lost model identity never pollutes the Library.
func (s *Store) Record(fp domain.ModelFingerprint, cat Category, tags []string, content string) string {
	if fp.IsZero() {
		return ""
	}

	// Observation content is untrusted, model- or tool-result-derived text (item S4): sanitize it
	// before it ever reaches disk so poison never lands on the store in directive-capable form.
	content = SanitizeContent(content)

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if existing := s.findMatch(fp.Label, cat, tags); existing != nil {
		existing.Observations++
		existing.LastUsed = now
		existing.Content = content
		s.persist()
		return existing.ID
	}

	id := entryID(fp.Label, cat, tags)
	s.entries[id] = &Entry{
		ID:           id,
		Category:     cat,
		ModelLabel:   fp.Label,
		Confidence:   fp.Confidence,
		Tags:         tags,
		Content:      content,
		Observations: 1,
		Successes:    0,
		CreatedAt:    now,
		LastUsed:     now,
		TTLHours:     defaultTTLHours,
	}
	s.evictExcess()
	s.persist()
	return id
}

// RecordSuccess bumps both the observation and success counts for an entry, so its Bayesian
// score falls toward the prior (the model did the opposite of the recorded failure). An
// unknown ID is a no-op. It persists the store.
func (s *Store) RecordSuccess(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.entries[id]
	if !ok {
		return
	}
	e.Observations++
	e.Successes++
	s.persist()
}

// Query returns the entries keyed on fp that still qualify for injection: not expired, seen at
// least minQueryObservations times, and scoring above minQueryScore — sorted by score
// descending. Returned entries are value copies of the locked in-memory state, safe to read
// after the lock is released. A zero fingerprint yields nothing (an unidentified model has no
// keyed evidence). The fingerprint-confidence injection gate is the inject Mechanism's
// decision (phase-4 item 14), not the store's.
func (s *Store) Query(fp domain.ModelFingerprint) []Entry {
	if fp.IsZero() {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	now := s.now()
	var matches []*Entry
	for _, e := range s.entries {
		if e.ModelLabel != fp.Label || e.Expired(now) {
			continue
		}
		if e.Observations < minQueryObservations || e.Score() < minQueryScore {
			continue
		}
		matches = append(matches, e)
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score() != matches[j].Score() {
			return matches[i].Score() > matches[j].Score()
		}
		return matches[i].ID < matches[j].ID // stable tiebreak so query order is deterministic
	})

	out := make([]Entry, 0, len(matches))
	for _, e := range matches {
		out = append(out, *e)
	}
	return out
}

// All returns value copies of every non-expired entry, sorted by ID for a deterministic order.
func (s *Store) All() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := s.now()
	out := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		if !e.Expired(now) {
			out = append(out, *e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Count returns the number of non-expired entries currently held.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := s.now()
	n := 0
	for _, e := range s.entries {
		if !e.Expired(now) {
			n++
		}
	}
	return n
}

// storePath is the single store file under the injected directory. The Store only ever reads
// and writes this path — it never derives a ~/.apogee or any path outside dir (ADR 0001).
func (s *Store) storePath() string { return filepath.Join(s.dir, storeFileName) }

// persist writes the whole in-memory store to disk under the caller-held write lock, creating
// the directory lazily. A write failure is a soft failure: it is surfaced to stderr rather
// than propagated, because a Record that cannot reach disk should not abort the loop — the
// in-memory store stays correct for the rest of the process (the sim's posture at @pin).
func (s *Store) persist() {
	if err := os.MkdirAll(s.dir, dirPerm); err != nil {
		fmt.Fprintf(os.Stderr, "apogee: create library directory %q: %v\n", s.dir, err)
		return
	}

	entries := make([]*Entry, 0, len(s.entries))
	for _, e := range s.entries {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })

	data, err := json.MarshalIndent(persisted{Version: StoreVersion, Entries: entries}, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "apogee: encode library store: %v\n", err)
		return
	}
	if err := os.WriteFile(s.storePath(), data, filePerm); err != nil {
		fmt.Fprintf(os.Stderr, "apogee: write library store %q: %v\n", s.storePath(), err)
	}
}

// findMatch returns the entry with the same fingerprint label, category, and tag set, or nil.
func (s *Store) findMatch(modelLabel string, cat Category, tags []string) *Entry {
	for _, e := range s.entries {
		if e.ModelLabel == modelLabel && e.Category == cat && tagsEqual(e.Tags, tags) {
			return e
		}
	}
	return nil
}

// evictExcess drops the lowest-scoring (then least-recently-used) entries once the store grows
// past defaultMaxEntries, keeping a long-lived Library bounded.
func (s *Store) evictExcess() {
	if len(s.entries) <= defaultMaxEntries {
		return
	}

	all := make([]*Entry, 0, len(s.entries))
	for _, e := range s.entries {
		all = append(all, e)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Score() != all[j].Score() {
			return all[i].Score() < all[j].Score()
		}
		return all[i].LastUsed.Before(all[j].LastUsed)
	})

	for i := 0; i < len(all)-defaultMaxEntries; i++ {
		delete(s.entries, all[i].ID)
	}
}

// SanitizeContent scrubs untrusted observation text into a single-line, directive-inert form
// (item S4). Library entries persist model- and tool-result-derived strings and later re-inject
// them into a system prompt, so an unsanitized entry is a hostile-repo → store → future-system-prompt
// payload channel. It (1) strips control characters — so a stored note carries no ANSI/escape
// sequences or embedded NULs; (2) folds every CR/LF (and any other whitespace) into a single
// space — so a note can never open a fresh system-prompt line and masquerade as an instruction;
// and (3) collapses whitespace runs, trimming the result. It applies no length cap: Store.Record
// never capped content length (the only length cap lived in the mechanism's example-call observer,
// which now records parameter names, not values), and the inject side's token budget remains the
// size bound. It is applied at Record time (defends the store) and again at injection-render time
// (defends pre-existing stores written before this defence landed).
func SanitizeContent(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			// Fold every whitespace rune (incl. CR/LF/tab) into a single space, collapsing runs.
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
		case unicode.IsControl(r):
			// Drop other control characters entirely — they carry no display value.
			continue
		default:
			b.WriteRune(r)
			prevSpace = false
		}
	}
	return strings.TrimSpace(b.String())
}

// entryID is a stable, collision-resistant id for the (fingerprint, category, tags) triple:
// the tags are sorted so tag order does not fork the identity.
func entryID(modelLabel string, cat Category, tags []string) string {
	sorted := append([]string(nil), tags...)
	sort.Strings(sorted)
	raw := fmt.Sprintf("%s:%s:%s", modelLabel, cat, strings.Join(sorted, ","))
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:8])
}

// tagsEqual reports whether two tag sets are equal regardless of order.
func tagsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sa := append([]string(nil), a...)
	sb := append([]string(nil), b...)
	sort.Strings(sa)
	sort.Strings(sb)
	for i := range sa {
		if sa[i] != sb[i] {
			return false
		}
	}
	return true
}
