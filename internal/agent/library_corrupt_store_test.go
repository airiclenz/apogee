package agent

// Construction-path coverage for the corrupt-store degrade branch (post-v1.3.0 review-fixes item 10a;
// ADR 0015 Realisation "a corrupt or absent store degrades to an empty store with wire.go's exact
// os.Stderr notice"). The engine's single build path (buildEnabledMechanisms in loop.go) Loads a
// Library store only when `library` is armed and, on a soft Load error, degrades to an empty store and
// surfaces the failure to stderr rather than blocking startup. The existing temp-dir tests only cover
// the happy (absent → empty) path; this pins the CORRUPT-bytes soft-error path end to end.

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// captureStderr swaps the process os.Stderr for a pipe, runs f, and returns everything f wrote to
// stderr. The caller must NOT be a parallel test: os.Stderr is a process-global, so this is only
// race-free during the sequential test phase (the cmd/apogee/wire_test.go precedent).
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
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

	// Drive an Exchange so the armed library Mechanism's inject hook actually runs over the empty store.
	runExchange(t, a, "review the parser and summarize what it does")

	// The library Mechanism books a fire only when it injects; the degraded empty store yields no entries,
	// so nothing from the corrupt content reaches the system prompt.
	if n := fireCountFor(sink.events, "library"); n != 0 {
		t.Errorf("library fired %d time(s); a degraded empty store must inject nothing from the corrupt content", n)
	}
}
