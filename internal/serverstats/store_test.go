package serverstats

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/provider"
)

const (
	testServer   = "local"
	testEndpoint = "http://127.0.0.1:8080/v1"
)

// statsPath is a stats file path inside a fresh directory that does not exist yet, so the tests
// also cover the lazy directory creation.
func statsPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "state", "server-stats.jsonl")
}

// keyedSample is a completed attempt filed under server and model, with index as its marker.
func keyedSample(server, model string, index int) Sample {
	return Sample{
		Server:       server,
		Endpoint:     testEndpoint,
		Model:        model,
		At:           time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC),
		RequestID:    "req",
		Index:        index,
		TTFB:         100 * time.Millisecond,
		TTFT:         time.Second,
		Last:         3 * time.Second,
		Duration:     3 * time.Second,
		OutputTokens: 200,
		Outcome:      provider.AttemptOK,
	}
}

// appendAll appends every sample through store, failing the test on the first error.
func appendAll(t *testing.T, store *Store, samples ...Sample) {
	t.Helper()
	for _, s := range samples {
		if err := store.Append(s); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
}

// rawLines is the stats file's lines, trailing newline dropped.
func rawLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read stats file: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func TestAppendLoadRoundTrip(t *testing.T) {
	t.Parallel()
	path := statsPath(t)
	store := Open(path)
	want := keyedSample(testServer, "qwen", 3)
	want.Outcome = "http_503"
	appendAll(t, store, want, keyedSample("other", "qwen", 0))

	got, err := store.Load(testServer, testEndpoint)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("Load() = %+v, want [%+v]", got, want)
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	t.Parallel()
	path := statsPath(t)
	got, err := Open(path).Load(testServer, testEndpoint)
	if err != nil || len(got) != 0 {
		t.Fatalf("Load() = %v, %v; want empty, nil", got, err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Errorf("Open created the stats directory; want it created lazily by Append")
	}
}

func TestAppendKeepsStatsPrivate(t *testing.T) {
	t.Parallel()
	path := statsPath(t)
	appendAll(t, Open(path), keyedSample(testServer, "qwen", 0))
	for p, want := range map[string]os.FileMode{path: filePerm, filepath.Dir(path): dirPerm} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %q: %v", p, err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%q perm = %o, want %o", p, got, want)
		}
	}
}

func TestLoadSkipsCorruptLines(t *testing.T) {
	t.Parallel()
	path := statsPath(t)
	store := Open(path)
	appendAll(t, store, keyedSample(testServer, "qwen", 0))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, filePerm)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString("{not json\n\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	appendAll(t, store, keyedSample(testServer, "qwen", 1))

	got, err := Open(path).Load(testServer, testEndpoint)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 2 || got[0].Index != 0 || got[1].Index != 1 {
		t.Fatalf("Load() = %+v, want the two well-formed samples", got)
	}
}

func TestSummaryFallsBackToLastModel(t *testing.T) {
	t.Parallel()
	store := Open(statsPath(t))
	for i := range 5 {
		appendAll(t, store, keyedSample(testServer, "qwen", i))
	}
	appendAll(t, store, keyedSample(testServer, "llama", 0))

	bound, err := store.Summary(testServer, testEndpoint, "qwen")
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if bound.Model != "qwen" || bound.Total != 5 || bound.NoData {
		t.Errorf("bound Summary = %+v, want qwen over 5 samples", bound)
	}
	fallback, err := store.Summary(testServer, testEndpoint, "")
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if fallback.Model != "llama" || fallback.Total != 1 || !fallback.NoData {
		t.Errorf("fallback Summary = %+v, want llama over 1 sample", fallback)
	}
}

func TestOpenTrimsKeepingLastFiftyPerKey(t *testing.T) {
	t.Parallel()
	path := statsPath(t)
	store := Open(path)
	busy := slackFactor*keepPerKey + 1
	for i := range busy {
		appendAll(t, store, keyedSample(testServer, "qwen", i))
	}
	appendAll(t, store, keyedSample(testServer, "llama", 0), keyedSample("quiet", "qwen", 0))

	got, err := Open(path).Load(testServer, testEndpoint)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != keepPerKey+1 {
		t.Fatalf("after trim Load() holds %d samples, want %d", len(got), keepPerKey+1)
	}
	for i, s := range got[:keepPerKey] {
		if want := busy - keepPerKey + i; s.Model != "qwen" || s.Index != want {
			t.Fatalf("sample %d = %s#%d, want qwen#%d", i, s.Model, s.Index, want)
		}
	}
	if got[keepPerKey].Model != "llama" {
		t.Errorf("last sample model = %q, want llama", got[keepPerKey].Model)
	}
	if quiet, _ := Open(path).Load("quiet", testEndpoint); len(quiet) != 1 {
		t.Errorf("quiet key holds %d samples after trim, want 1", len(quiet))
	}
	if n := len(rawLines(t, path)); n != keepPerKey+2 {
		t.Errorf("trimmed file holds %d lines, want %d", n, keepPerKey+2)
	}
}

func TestOpenDoesNotRewriteManyKeysAtCap(t *testing.T) {
	t.Parallel()
	path := statsPath(t)
	store := Open(path)
	for k := range 6 {
		for i := range keepPerKey {
			appendAll(t, store, keyedSample(fmt.Sprintf("server-%d", k), "qwen", i))
		}
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	Open(path)
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Errorf("Open rewrote a 6-key × %d-line file; want it left in place", keepPerKey)
	}
}

func TestAppendAfterAnotherHandlesTrimLandsInRenamedFile(t *testing.T) {
	t.Parallel()
	path := statsPath(t)
	early := Open(path)
	for i := range slackFactor*keepPerKey + 1 {
		appendAll(t, early, keyedSample(testServer, "qwen", i))
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	Open(path) // a second handle trims by temp+rename
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if os.SameFile(before, after) {
		t.Fatalf("the second Open did not trim; the test needs a renamed file")
	}

	appendAll(t, early, keyedSample(testServer, "qwen", 9999))
	got, err := Open(path).Load(testServer, testEndpoint)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != keepPerKey+1 || got[len(got)-1].Index != 9999 {
		t.Fatalf("Load() holds %d samples ending %+v; want %d ending with index 9999",
			len(got), got[len(got)-1], keepPerKey+1)
	}
}

func TestConcurrentAppendsStayParseable(t *testing.T) {
	t.Parallel()
	path := statsPath(t)
	const perWriter = 100
	var wg sync.WaitGroup
	for w := range 2 {
		wg.Add(1)
		go func(store *Store, server string) {
			defer wg.Done()
			for i := range perWriter {
				if err := store.Append(keyedSample(server, "qwen", i)); err != nil {
					t.Errorf("Append: %v", err)
					return
				}
			}
		}(Open(path), fmt.Sprintf("writer-%d", w))
	}
	wg.Wait()

	lines := rawLines(t, path)
	if len(lines) != 2*perWriter {
		t.Fatalf("file holds %d lines, want %d", len(lines), 2*perWriter)
	}
	for i, line := range lines {
		var rec record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line %d is not parseable: %v: %q", i, err, line)
		}
	}
}
