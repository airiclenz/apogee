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

	// tempFilePattern names the transient file the writer renames over the store. The dot prefix
	// and .tmp suffix keep it visibly distinct from storeFileName, which is the only name Load
	// ever reads — so a temp file a crash stranded is never mistaken for the store.
	tempFilePattern = ".apogee-library-*.tmp"

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

// persistDebounce is how long the writer goroutine waits after a wake before it snapshots, so a
// burst of Records inside the window costs one whole-file write rather than one per observation.
// It is a variable, not a constant, ONLY so a test can shorten or lengthen it — production never
// reassigns it.
var persistDebounce = 200 * time.Millisecond

// closeFlushTimeout bounds the WHOLE of Close: stopping the writer, joining it, and the final
// write. Past it Close returns errFlushTimedOut and abandons its helper goroutine, so a writer
// parked inside a hung filesystem write can never hold up a session's shutdown. A variable for the
// same test-only reason as persistDebounce.
var closeFlushTimeout = 2 * time.Second

// errFlushTimedOut is what Close returns when the flush did not finish inside closeFlushTimeout.
// The observations recorded since the last write stay in memory only — the accepted cost of a
// best-effort store that must never hang a shutdown.
var errFlushTimedOut = errors.New("apogee: library store: flush did not finish in 2s; the last observations are not on disk")

// persisted is the on-disk envelope: a schema Version plus the flat list of entries. Storing
// the whole store in one versioned file (rather than a file per entry) mirrors
// domain.Session's envelope-and-version discipline and keeps load and flush process-local.
type persisted struct {
	Version int      `json:"version"`
	Entries []*Entry `json:"entries"`
}

// Store is the file-backed Library: per-fingerprint observations with Bayesian confidence
// counts, rooted at an injected directory. It is process-local — the mutex guards the
// in-memory map for concurrent goroutines within one process, but the store makes no
// cross-process locking claims in v1 (two processes on one dir may last-writer-win). It NEVER
// reaches for an ambient ~/.apogee: the caller supplies dir (ADR 0001).
//
// No caller ever writes to disk. Record and RecordSuccess mutate memory and return; one writer
// goroutine debounces those marks into a single whole-file snapshot. Flush publishes now and
// returns the write error; Close flushes and parks the writer under a bounded deadline.
type Store struct {
	mu      sync.RWMutex
	dir     string
	entries map[string]*Entry
	now     func() time.Time // injectable so a test controls timestamps and TTL

	// dirty says memory has moved ahead of disk; running says a writer goroutine is alive; wake,
	// stop and joined are that writer's channels. All are guarded by mu. The writer starts lazily
	// on the first mutation — a store that is only Loaded or Queried never spawns one — and Close
	// PARKS it rather than killing the store: a later Record starts a fresh writer.
	dirty   bool
	running bool
	wake    chan struct{}
	stop    chan struct{}
	joined  chan struct{}

	// writeMu serialises the two publishers — the writer goroutine and a caller's Flush — so their
	// renames can never interleave. Lock order: writeMu outside, mu inside, never the reverse.
	writeMu sync.Mutex

	// write publishes the encoded snapshot (atomicWrite in production) and notify reports a write
	// the writer goroutine could not publish (stderr in production); both are seams a test replaces.
	write  func(path string, data []byte) error
	notify func(error)
}

// NewStore returns an empty Store rooted at dir. The directory is created lazily on the first
// Save, so an apogee run that never records touches no disk. Call Load to populate it from an
// existing store file.
func NewStore(dir string) *Store {
	return &Store{
		dir:     dir,
		entries: make(map[string]*Entry),
		now:     time.Now,
		write:   atomicWrite,
		notify:  func(err error) { fmt.Fprintln(os.Stderr, err) },
	}
}

// openStores is the per-process registry Open hands out: at most one Store per library directory,
// keyed by the cleaned path, guarded by openMu. It is package state deliberately — the three build
// paths that reach one Config.LibraryDir (an Agent's construction, its every Rebind, and a routed
// sub-agent's catalogue, built with no Agent in sight) cannot see each other's Deps, so the
// per-process identity has to live where all three can find it.
var (
	openMu     sync.Mutex
	openStores = make(map[string]*Store)
)

// Open returns THE Store for dir in this process: the first call constructs and Loads it, and every
// later call naming the same directory returns that same instance. Sharing is what keeps the
// whole-file snapshot honest — two Stores on one library.json each rewrite the file from their own
// memory, so the last writer silently drops the other's observations. It is the store-level twin of
// what a catalogue crossing the delegation boundary already does (internal/mechanisms/library.go:
// ForSubAgent shares the STORE while isolating the Mechanism's live state).
//
// The returned error is Load's soft error and is returned ON THE CONSTRUCTING CALL ONLY; every later
// Open returns a nil error. A soft Load error still yields a usable, empty store (see Load), so the
// caller that reports the degrade — deriveDeps in internal/agent/construct.go — prints its notice
// exactly once per process without coordinating with anyone.
//
// NewStore stays the door to a PRIVATE store that shares nothing: a test's fixture, a bench Driver
// seeding one. Closing a SHARED store is safe from any holder, because Close flushes and parks the
// writer rather than ending the store — a later Record starts a fresh one (see Close).
func Open(dir string) (*Store, error) {
	key := filepath.Clean(dir)

	openMu.Lock()
	defer openMu.Unlock()

	if shared, ok := openStores[key]; ok {
		return shared, nil
	}
	shared := NewStore(key)
	openStores[key] = shared
	return shared, shared.Load()
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
// refreshed; otherwise a new entry is created. It returns the entry ID. Nothing is written on the
// caller's goroutine: the observation is marked pending and reaches disk within persistDebounce,
// or at Close.
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
		s.markDirtyLocked()
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
	s.markDirtyLocked()
	return id
}

// RecordSuccess bumps both the observation and success counts for an entry, so its Bayesian
// score falls toward the prior (the model did the opposite of the recorded failure). An
// unknown ID is a no-op. Like Record, it writes nothing on the caller's goroutine.
func (s *Store) RecordSuccess(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.entries[id]
	if !ok {
		return
	}
	e.Observations++
	e.Successes++
	s.markDirtyLocked()
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

// Flush publishes the pending observations now, on the calling goroutine, and returns the write
// error instead of reporting it. It is a no-op when nothing is pending. The loop never needs it —
// the writer goroutine and Close cover the process — but a caller that has just Recorded and wants
// the file on disk (a test, a fixture seeder) calls it.
func (s *Store) Flush() error { return s.writeSnapshot() }

// Close flushes the store and parks its writer; Store satisfies io.Closer. It is idempotent, and
// the store stays usable after it: a later Record starts a fresh writer and the next Close flushes
// again, so an instance shared between sessions or catalogues loses nothing to being closed early.
// The WHOLE of Close — stopping the writer, joining it, and the final write — is bounded by
// closeFlushTimeout; past that deadline Close returns errFlushTimedOut and abandons its helper, so
// a writer parked inside a hung filesystem write can never hold up a shutdown.
func (s *Store) Close() error {
	flushed := make(chan error, 1)
	go func() { flushed <- s.stopAndFlush() }()

	select {
	case err := <-flushed:
		return err
	case <-time.After(closeFlushTimeout):
		return errFlushTimedOut
	}
}

// stopAndFlush stops a running writer, joins it, and writes whatever is still pending. It runs on
// Close's helper goroutine and never on Close's caller, so the join and writeMu are both waits the
// deadline can walk away from.
func (s *Store) stopAndFlush() error {
	s.mu.Lock()
	stop, joined := s.stop, s.joined
	s.running, s.wake, s.stop, s.joined = false, nil, nil, nil
	s.mu.Unlock()

	if stop != nil {
		close(stop)
		<-joined
	}
	return s.writeSnapshot()
}

// markDirtyLocked records that memory has moved ahead of disk and nudges the writer, starting one
// if none is running. The caller holds the write lock. The nudge never blocks: wake is buffered,
// and a wake already queued says everything this one would.
func (s *Store) markDirtyLocked() {
	s.dirty = true
	if !s.running {
		s.running = true
		s.wake, s.stop, s.joined = make(chan struct{}, 1), make(chan struct{}), make(chan struct{})
		go s.writeLoop(s.wake, s.stop, s.joined)
	}

	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// writeLoop is the store's single writer. It takes its channels as parameters rather than reading
// them off the Store, so a writer a timed-out Close abandoned can never steal the wakes meant for
// the writer that replaced it. A write it cannot publish is a soft failure — reported through
// notify, with the store left dirty so the next flush retries — because an observation that cannot
// reach disk must not abort the loop (the sim's posture at @pin).
func (s *Store) writeLoop(wake, stop <-chan struct{}, joined chan<- struct{}) {
	defer close(joined)

	for {
		select {
		case <-stop:
			return
		case <-wake:
		}

		// Coalesce the burst: everything recorded inside the window rides on one snapshot. A stop
		// during it returns at once — Close's own flush is what puts those observations on disk.
		select {
		case <-stop:
			return
		case <-time.After(persistDebounce):
		}

		if err := s.writeSnapshot(); err != nil {
			s.notify(err)
		}
	}
}

// writeSnapshot publishes the in-memory store when it has moved ahead of disk. writeMu serialises
// the two publishers — the writer goroutine and a caller's Flush — so their renames never
// interleave; the encode and the write itself happen outside s.mu, so a slow or hung filesystem
// blocks no Record.
func (s *Store) writeSnapshot() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return nil
	}
	s.dirty = false
	dir, path, entries := s.dir, s.storePath(), s.snapshotLocked()
	s.mu.Unlock()

	if err := s.publish(dir, path, entries); err != nil {
		s.mu.Lock()
		s.dirty = true // the observations are still memory-only; the next flush retries
		s.mu.Unlock()
		return err
	}
	return nil
}

// snapshotLocked copies every entry by VALUE, sorted by ID for a byte-stable file. The copies are
// what the encoder reads, so a Record that bumps an entry's counts while a write is in flight can
// never change the bytes mid-encode. The caller holds the write lock: the snapshot and the dirty
// flag it clears must move together.
func (s *Store) snapshotLocked() []Entry {
	out := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// publish encodes the snapshot and writes it, creating the directory lazily. Its errors carry the
// house "apogee: " prefix exactly once: atomicWrite's already do, so the write error is returned
// unwrapped — wrapping it would double the prefix ("apogee: apogee: rename library store into …").
func (s *Store) publish(dir, path string, entries []Entry) error {
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("apogee: create library directory %q: %w", dir, err)
	}

	refs := make([]*Entry, len(entries))
	for i := range entries {
		refs[i] = &entries[i]
	}
	data, err := json.MarshalIndent(persisted{Version: StoreVersion, Entries: refs}, "", "  ")
	if err != nil {
		return fmt.Errorf("apogee: encode library store: %w", err)
	}
	return s.write(path, data)
}

// atomicWrite writes data to a temp file in path's directory and renames it into place, so a
// crash mid-write can never truncate the store and silently degrade the next Load to empty
// (every flush rewrites the whole store). It mirrors internal/session's writer.
// The temp file is removed on every failure path and never survives a successful rename;
// Load only ever reads storeFileName, so a temp file a hard kill left behind is inert.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, tempFilePattern)
	if err != nil {
		return fmt.Errorf("apogee: create temp library store in %q: %w", dir, err)
	}
	tmpName := tmp.Name()
	// Remove the temp file on every path except a successful rename (where it no longer
	// exists, so Remove is a harmless no-op).
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(filePerm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("apogee: chmod temp library store %q: %w", tmpName, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("apogee: write temp library store %q: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("apogee: close temp library store %q: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("apogee: rename library store into %q: %w", path, err)
	}
	return nil
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
// payload channel. It (1) strips control (Cc), format (Cf), private-use (Co) and surrogate (Cs)
// characters — so a stored note carries no ANSI/escape sequences, embedded NULs, bidi overrides,
// zero-width characters, BOM or soft hyphens; (2) folds every CR/LF (and any other whitespace) into a single
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
		case unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Co, unicode.Cs):
			// Drop control (Cc), format (Cf — bidi overrides, zero-width chars, BOM, soft hyphen),
			// private-use (Co) and surrogate (Cs) runes entirely: they carry no display value, and a
			// format character can smuggle a directive past a newline-only defence (third-review F3).
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
