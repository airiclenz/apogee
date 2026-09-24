// Package serverstats is the cross-session record of how each server entry's upstream attempts
// went (ADR 0085): one JSON line per HTTP attempt in a single file, and the summary a picker row
// shows — ttft p50, tok/s p50 and the failure rate — computed from it. It measures; it never
// routes, and nothing it holds reaches the model.
//
// Every record is keyed by the server entry's name and its redacted endpoint (scheme, host and
// path, as provider.WithServerIdentity stamped it) and names the model that answered, so a
// summary can be filtered to the model a server is bound to. No prompt text, request body or
// key name is ever stored.
package serverstats

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// dirPerm and filePerm scope the stats file to the owner, as internal/recall scopes its own:
// the endpoints and model ids of a user's servers are theirs alone.
const (
	dirPerm  os.FileMode = 0o700
	filePerm os.FileMode = 0o600
)

// keepPerKey is how many of a key's newest samples a trim keeps, and the window a Summary is
// computed over: enough for a stable median, few enough that a server's recent behaviour — not
// last month's — is what a picker row reports.
const keepPerKey = 50

// slackFactor is how far past keepPerKey a file may grow before Open trims it, internal/recall's
// slack rule (compactAt): a trim rewrites the file at most once per (slackFactor−1)×keepPerKey
// appends to a key rather than on every Open once a key reaches the cap.
const slackFactor = 4

// Sample is the measurement of one HTTP attempt against a server entry — the stored mirror of
// provider.Attempt and domain.UpstreamAttemptEvent. Every duration is timed from the attempt's
// send; a zero means that point was never reached.
type Sample struct {
	// Server is the server entry's name; Endpoint its redacted endpoint. Together they are the
	// key a Store files the sample under.
	Server   string
	Endpoint string
	// Model is the model id the server answered with, else the one the request asked for.
	Model string
	// At is when the sample was recorded.
	At time.Time
	// RequestID is shared by every attempt of one model call; Index is the attempt's 0-based
	// position within it.
	RequestID string
	Index     int
	// TTFB is send → first body byte, TTFT send → first model delta, Last send → last model
	// delta, Duration send → the attempt's end.
	TTFB     time.Duration
	TTFT     time.Duration
	Last     time.Duration
	Duration time.Duration
	// OutputTokens is the completion token count the server reported, 0 when it reported none.
	OutputTokens int
	// Outcome is how the attempt ended: "ok", a fault class or "cancelled" (the provider's
	// closed vocabulary).
	Outcome string
}

// record is one line of the stats file: a Sample with its durations in whole milliseconds, so the
// file stays legible to a person reading it and matches the headless upstream_attempt line.
type record struct {
	Server       string `json:"server"`
	Endpoint     string `json:"endpoint"`
	Model        string `json:"model"`
	At           string `json:"t"`
	RequestID    string `json:"request_id"`
	Index        int    `json:"index"`
	TTFBMs       int64  `json:"ttfb_ms"`
	TTFTMs       int64  `json:"ttft_ms"`
	LastMs       int64  `json:"last_ms"`
	DurationMs   int64  `json:"duration_ms"`
	OutputTokens int    `json:"output_tokens"`
	Outcome      string `json:"outcome"`
}

// sampleKey is what a trim keeps keepPerKey samples of: one model on one server entry.
type sampleKey struct {
	server, endpoint, model string
}

func (s Sample) key() sampleKey { return sampleKey{s.Server, s.Endpoint, s.Model} }

// Store is the file-backed stats record: one JSONL file at an injected path. The mutex serializes
// this process's own appends and loads; across processes it relies on O_APPEND writes of one whole
// line each and makes no locking claim — a line lost to a concurrent trim's rename is acceptable
// (ADR 0085). It never reaches for an ambient ~/.apogee: the caller supplies the path (ADR 0001).
//
// Beside the file it keeps an in-memory index — each key's newest keepPerKey samples, read once at
// Open and extended by every successful Append — which is what Summary answers from, so a picker
// asking per draw never touches the disk. Another process's appends reach the index at the next
// Open.
type Store struct {
	mu   sync.Mutex
	path string
	// index holds each key's newest keepPerKey samples, oldest first; lastModel is the model each
	// server entry (name, endpoint) recorded last, the fallback an unbound Summary covers.
	index     map[sampleKey][]Sample
	lastModel map[entryKey]string
}

// entryKey is one server entry: its name and redacted endpoint.
type entryKey struct {
	server, endpoint string
}

// Open returns a Store over path, first trimming the file when it has outgrown its slack: when any
// key holds more than slackFactor×keepPerKey samples, or the line count exceeds slackFactor ×
// (distinct keys × keepPerKey). The trim keeps each key's newest keepPerKey samples in file order,
// drops malformed lines and replaces the file by temp+rename. It is total: a missing file, an
// unreadable one or a failed trim leaves the file as it is and still returns a usable Store —
// stats are a convenience, never a reason a session fails to start. Neither the file nor its
// directory is created until the first Append.
func Open(path string) *Store {
	s := &Store{path: path, index: make(map[sampleKey][]Sample), lastModel: make(map[entryKey]string)}
	s.trimIfOversized()
	return s
}

// Append records one sample as a single line. It opens, appends and closes on every call, so no
// file descriptor outlives the write and an Append after another process's trim lands in the
// renamed file rather than in the unlinked one.
func (s *Store) Append(sample Sample) error {
	line, err := encodeRecord(toRecord(sample))
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("apogee: create server-stats dir %q: %w", dir, err)
	}
	if err := appendLine(s.path, line); err != nil {
		return err
	}
	s.indexSample(sample)
	return nil
}

// Load returns every well-formed sample recorded for the server entry name at endpoint, oldest
// first, across every model it answered with. A missing file loads empty and malformed lines are
// skipped; only an unreadable file is an error.
func (s *Store) Load(name, endpoint string) ([]Sample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	samples, _, err := readSamples(s.path)
	if err != nil {
		return nil, err
	}
	matched := make([]Sample, 0, len(samples))
	for _, sample := range samples {
		if sample.Server == name && sample.Endpoint == endpoint {
			matched = append(matched, sample)
		}
	}
	return matched, nil
}

// Summary summarises the server entry's samples of model (Summarize) from the in-memory index —
// it reads no file, so a picker may ask it on every draw. An empty model summarises the model the
// entry last recorded (LastModel) — the fallback a picker row labels, since the Summary it returns
// names the model it covers.
func (s *Store) Summary(name, endpoint, model string) Summary {
	s.mu.Lock()
	defer s.mu.Unlock()

	if model == "" {
		model = s.lastModel[entryKey{name, endpoint}]
	}
	return Summarize(s.index[sampleKey{name, endpoint, model}], model)
}

// indexSample adds one sample to the in-memory index, dropping its key's oldest once the key holds
// more than keepPerKey. Called with the mutex held.
func (s *Store) indexSample(sample Sample) {
	k := sample.key()
	kept := append(s.index[k], sample)
	if len(kept) > keepPerKey {
		kept = slices.Clone(kept[len(kept)-keepPerKey:])
	}
	s.index[k] = kept
	s.lastModel[entryKey{sample.Server, sample.Endpoint}] = sample.Model
}

// trimIfOversized rewrites the file down to keepPerKey samples per key when it has outgrown the
// slack rule, and fills the in-memory index from what it read either way. Every failure — read,
// encode, temp file, rename — is skipped: the file stays as it was and the next Open tries again.
func (s *Store) trimIfOversized() {
	s.mu.Lock()
	defer s.mu.Unlock()

	samples, lines, err := readSamples(s.path)
	for _, sample := range samples {
		s.indexSample(sample)
	}
	if err != nil || !oversized(samples, lines) {
		return
	}
	var buf bytes.Buffer
	for _, sample := range newestPerKey(samples) {
		line, err := encodeRecord(toRecord(sample))
		if err != nil {
			return
		}
		buf.Write(line)
	}
	_ = atomicWrite(s.path, buf.Bytes())
}

// oversized reports whether the slack rule calls for a trim. The per-key test catches one busy key
// in a file of quiet ones; the line test catches malformed lines piling up. lines counts every
// raw line, malformed ones included, so a file of nothing but junk still gets trimmed.
func oversized(samples []Sample, lines int) bool {
	counts := make(map[sampleKey]int)
	for _, sample := range samples {
		counts[sample.key()]++
		if counts[sample.key()] > slackFactor*keepPerKey {
			return true
		}
	}
	if len(counts) == 0 {
		return lines > 0
	}
	return lines > slackFactor*len(counts)*keepPerKey
}

// newestPerKey keeps each key's newest keepPerKey samples, in file order.
func newestPerKey(samples []Sample) []Sample {
	remaining := make(map[sampleKey]int)
	for _, sample := range samples {
		remaining[sample.key()]++
	}
	kept := make([]Sample, 0, len(samples))
	for _, sample := range samples {
		k := sample.key()
		if remaining[k] <= keepPerKey {
			kept = append(kept, sample)
		}
		remaining[k]--
	}
	return kept
}

// readSamples reads path and returns its well-formed samples in file order plus the raw line
// count. A missing file is not an error: a fresh install has recorded nothing yet.
func readSamples(path string) ([]Sample, int, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("apogee: read server-stats file %q: %w", path, err)
	}
	if len(data) == 0 {
		return nil, 0, nil
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	samples := make([]Sample, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		samples = append(samples, fromRecord(rec))
	}
	return samples, len(lines), nil
}

// toRecord is a Sample's on-disk shape.
func toRecord(s Sample) record {
	return record{
		Server:       s.Server,
		Endpoint:     s.Endpoint,
		Model:        s.Model,
		At:           s.At.UTC().Format(time.RFC3339),
		RequestID:    s.RequestID,
		Index:        s.Index,
		TTFBMs:       s.TTFB.Milliseconds(),
		TTFTMs:       s.TTFT.Milliseconds(),
		LastMs:       s.Last.Milliseconds(),
		DurationMs:   s.Duration.Milliseconds(),
		OutputTokens: s.OutputTokens,
		Outcome:      s.Outcome,
	}
}

// fromRecord is the Sample a line decodes to. An unparseable timestamp leaves At zero rather than
// dropping a sample whose measurements are intact.
func fromRecord(r record) Sample {
	at, _ := time.Parse(time.RFC3339, r.At)
	return Sample{
		Server:       r.Server,
		Endpoint:     r.Endpoint,
		Model:        r.Model,
		At:           at,
		RequestID:    r.RequestID,
		Index:        r.Index,
		TTFB:         time.Duration(r.TTFBMs) * time.Millisecond,
		TTFT:         time.Duration(r.TTFTMs) * time.Millisecond,
		Last:         time.Duration(r.LastMs) * time.Millisecond,
		Duration:     time.Duration(r.DurationMs) * time.Millisecond,
		OutputTokens: r.OutputTokens,
		Outcome:      r.Outcome,
	}
}

// encodeRecord marshals one record into its on-disk line, newline included.
func encodeRecord(rec record) ([]byte, error) {
	line, err := json.Marshal(rec)
	if err != nil {
		return nil, fmt.Errorf("apogee: encode server-stats sample: %w", err)
	}
	return append(line, '\n'), nil
}

// appendLine appends one encoded line to path in a single write, creating the file if needed.
func appendLine(path string, line []byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, filePerm)
	if err != nil {
		return fmt.Errorf("apogee: open server-stats file %q: %w", path, err)
	}
	if _, err := f.Write(line); err != nil {
		_ = f.Close()
		return fmt.Errorf("apogee: append server-stats sample to %q: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("apogee: close server-stats file %q: %w", path, err)
	}
	return nil
}

// atomicWrite writes data to a temp file in path's directory and renames it into place, so a
// reader never observes a half-trimmed file and a crash mid-trim leaves the previous one intact.
// The temp file is removed on every path except a successful rename.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".apogee-server-stats-*.tmp")
	if err != nil {
		return fmt.Errorf("apogee: create temp server-stats file in %q: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(filePerm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("apogee: chmod temp server-stats file %q: %w", tmpName, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("apogee: write temp server-stats file %q: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("apogee: close temp server-stats file %q: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("apogee: rename server-stats file into %q: %w", path, err)
	}
	return nil
}
