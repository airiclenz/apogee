package agent

// Construction-path coverage for the corrupt-store degrade branch (post-v1.3.0 review-fixes item 10a;
// ADR 0015 Realisation "a corrupt or absent store degrades to an empty store with an os.Stderr
// notice"). The engine's single build path (buildEnabledMechanisms in construct.go) derives its Deps
// through deriveDeps, which Loads a Library store only when `library` is armed and, on a soft Load
// error, degrades to an empty store and surfaces the failure to stderr — "%v — library store degraded
// to empty", the consequence appended to Load's already-prefixed error — rather than blocking startup. The existing temp-dir tests only cover
// the happy (absent → empty) path; this pins the CORRUPT-bytes soft-error path end to end.

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// captureStderr swaps the process os.Stderr for a pipe, runs f, and returns everything f wrote to
// stderr. The caller must NOT be a parallel test: os.Stderr is a process-global, so this is only
// race-free during the sequential test phase (the cmd/apogee/wire_helpers_test.go precedent).
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer func() { _ = r.Close() }()
	os.Stderr = w
	// A t.Fatal or t.Skip inside f is a runtime.Goexit, which unwinds past the restore below
	// exactly as a panic does. This cleanup runs on every exit path: it closes the write end — which
	// ends the reader goroutine — and puts the process stderr back. It is idempotent with the
	// happy-path restore, which stays so the captured string is still returned in order.
	t.Cleanup(func() {
		_ = w.Close()
		os.Stderr = orig
	})
	captured := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		captured <- buf.String()
	}()

	f()

	_ = w.Close()
	os.Stderr = orig
	return <-captured
}

// TestCaptureStderrRestoresOnGoexit pins the restore on the exit path the helper used to leak. A
// t.Fatal or t.Skip inside the wrapped call is a runtime.Goexit, which unwinds past the happy-path
// restore exactly as a panic does — t.Skip is that path with no failure to swallow. After the
// subtest returns, the process stderr must be the real one again and the reader goroutine must have
// ended; otherwise every later test writes its diagnostics into a pipe nobody reads.
func TestCaptureStderrRestoresOnGoexit(t *testing.T) {
	// Deliberately NOT parallel: captureStderr swaps the process-global os.Stderr.
	orig := os.Stderr
	before := runtime.NumGoroutine()

	t.Run("the wrapped call bails", func(sub *testing.T) {
		captureStderr(sub, func() { sub.Skip("bail") })
		sub.Fatal("captureStderr returned after a Goexit inside the wrapped call")
	})

	if os.Stderr != orig {
		t.Errorf("os.Stderr = %v after a bailed capture; want the original %v", os.Stderr, orig)
	}
	// The reader ends once the cleanup closes the write end. Poll for `<=`, never `==`: other tests'
	// background goroutines (httptest idle connections) wind down asynchronously and an equality flakes.
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before {
		if time.Now().After(deadline) {
			t.Errorf("goroutines = %d after a bailed capture; want <= the %d before it",
				runtime.NumGoroutine(), before)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// lineContaining returns the first line of out that contains want, trimmed. Isolating the one line
// keeps a per-line assertion honest even when an unrelated notice shares the captured stream.
func lineContaining(out, want string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, want) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// TestEnableMechanisms_CorruptLibraryStoreDegradesToEmpty seeds LibraryDir/library.json with garbage
// bytes, arms EnableMechanisms=["library"], and proves the three parts of the degrade contract: (1)
// construction still succeeds (a broken store never blocks startup), (2) the build path emits the
// degrade notice to os.Stderr exactly once, and (3) the armed library Mechanism then runs over the
// resulting EMPTY store, injecting nothing from the corrupt content.
//
// The "no injection" assertion is non-vacuous by design: the model id is a reachable weight file, so
// its fingerprint resolves ConfidenceHigh — the library inject confidence gate is OPEN — which leaves
// the degraded (empty) store as the SOLE barrier to an injection. A library fire here would therefore
// prove the corrupt bytes leaked into the system prompt. (The confidence gate would mask that with the
// default low-confidence test model.)
func TestEnableMechanisms_CorruptLibraryStoreDegradesToEmpty(t *testing.T) {
	// Deliberately NOT parallel: captureStderr swaps the process-global os.Stderr.
	libDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(libDir, "library.json"), []byte("not json {]["), 0o600); err != nil {
		t.Fatalf("seed corrupt store: %v", err)
	}

	// A reachable weight-file model id resolves to a ConfidenceHigh fingerprint (library.ResolveFingerprint
	// hashes .gguf weights), opening the inject confidence gate so the empty store is the only thing left
	// that can suppress an injection.
	weightPath := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(weightPath, []byte("gguf-weights"), 0o600); err != nil {
		t.Fatalf("seed weight file: %v", err)
	}

	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Model = weightPath
	cfg.LibraryDir = libDir
	cfg.EnableMechanisms = []domain.MechanismID{"library"}

	var (
		a        *Agent
		buildErr error
	)
	stderr := captureStderr(t, func() {
		a, buildErr = newAgent(cfg, echoResponder{reply: "done"})
	})
	if buildErr != nil {
		t.Fatalf("newAgent with a corrupt library store: %v, want a clean degrade-to-empty build", buildErr)
	}

	const notice = "library store degraded to empty"
	if got := strings.Count(stderr, notice); got != 1 {
		t.Errorf("degrade notice appeared %d time(s) (stderr = %q); want exactly 1", got, stderr)
	}

	// The notice carries ONE "apogee: " prefix: Load's errors already arrive prefixed, so the
	// degrade path appends its consequence rather than wrapping them in a second prefix
	// ("apogee: library store degraded to empty: apogee: decode library store …").
	degradeLine := lineContaining(stderr, notice)
	if !strings.HasPrefix(degradeLine, "apogee: ") {
		t.Errorf("degrade notice = %q; want it to start with %q", degradeLine, "apogee: ")
	}
	if got := strings.Count(degradeLine, "apogee: "); got != 1 {
		t.Errorf("degrade notice = %q; want exactly one %q prefix, got %d", degradeLine, "apogee: ", got)
	}

	// Drive an Exchange so the armed library Mechanism's inject hook actually runs over the empty store.
	runExchange(t, a, "review the parser and summarize what it does")

	// The library Mechanism books a fire only when it injects; the degraded empty store yields no entries,
	// so nothing from the corrupt content reaches the system prompt.
	if n := fireCountFor(sink.events, "library"); n != 0 {
		t.Errorf("library fired %d time(s); a degraded empty store must inject nothing from the corrupt content", n)
	}
}
