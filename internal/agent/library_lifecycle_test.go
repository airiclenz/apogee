package agent

// Lifecycle coverage for the Library store the engine derives (audit finding C-13, engine half).
// The store writes off the caller's path — recording only marks it dirty and one writer goroutine
// debounces those marks into a whole-file snapshot (internal/library) — so two things have to hold
// at this layer: every builder that reaches one Config.LibraryDir must hold ONE store (three
// snapshots of three memories on one file lose observations), and the Agent that derived it must
// FLUSH it at Close, since Close is the only call that knows the run has ended.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/library"
)

// libraryAgent constructs an Agent with the catalogued `library` Mechanism armed on dir, and
// returns it alongside the store its build opened. The store is parked on cleanup so no writer
// outlives the test's temp directory.
func libraryAgent(t *testing.T, dir string) (*Agent, *library.Store) {
	t.Helper()

	cfg := baseConfig(&recordingSink{})
	cfg.LibraryDir = dir
	cfg.EnableMechanisms = []domain.MechanismID{"library"}

	a, err := newAgent(cfg, echoResponder{reply: "done"})
	if err != nil {
		t.Fatalf("newAgent with a library arm on %q: %v", dir, err)
	}
	if a.library == nil {
		t.Fatal("the Agent holds no Library store; the armed `library` row derives one")
	}
	t.Cleanup(func() { _ = a.library.Close() })
	return a, a.library
}

// storedIDs decodes the store file under dir and returns the entry ids it holds. An absent file
// yields an empty set rather than a failure, so a caller can assert "nothing reached disk".
func storedIDs(t *testing.T, dir string) map[string]bool {
	t.Helper()

	back := library.NewStore(dir)
	if err := back.Load(); err != nil {
		t.Fatalf("load the store back from %q: %v", dir, err)
	}
	ids := make(map[string]bool)
	for _, e := range back.All() {
		ids[e.ID] = true
	}
	return ids
}

// libraryFP is a high-confidence fingerprint for label — the identity a Record files under.
func libraryFP(label string) domain.ModelFingerprint {
	return domain.ModelFingerprint{Label: label, Confidence: domain.ConfidenceHigh}
}

// TestCloseFlushesTheLibraryStore pins the engine half of the write model: an observation recorded
// during a session is on disk once the session's Agent has closed, without the caller ever having
// waited on the filesystem. Before this the Agent held no store at all, so the only thing that ever
// wrote was the writer's debounce timer — and a process exiting between the two lost the run.
func TestCloseFlushesTheLibraryStore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	a, store := libraryAgent(t, dir)
	shared, err := library.Open(dir)
	if err != nil {
		t.Fatalf("Open the same Library directory: %v", err)
	}
	if shared != store {
		t.Fatalf("library.Open(%q) = %p; want the very store the Agent holds (%p)", dir, shared, store)
	}

	id := store.Record(libraryFP("sha256:closed"), library.CategoryCorrection,
		[]string{"read_file"}, "read the file before editing it")

	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if ids := storedIDs(t, dir); !ids[id] {
		t.Errorf("the store on disk = %v after Close; want the observation %q flushed", ids, id)
	}
}

// TestRebindKeepsTheSessionsLibraryStore covers the second builder on one LibraryDir. Rebind
// rebuilds the whole catalogue against the newly bound model, which re-derives the Deps — and a
// second store there would be a second writer rewriting one file from its own memory, with the
// session's Close flushing whichever of the two it happened to hold.
func TestRebindKeepsTheSessionsLibraryStore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	a, before := libraryAgent(t, dir)

	if err := a.Rebind(RebindSpec{
		Model:            "second-model",
		MaxContextTokens: 16384,
		EnableMechanisms: []domain.MechanismID{"library"},
	}); err != nil {
		t.Fatalf("Rebind: %v", err)
	}

	if a.library != before {
		t.Errorf("the rebound session holds %p; want the store the session opened (%p)", a.library, before)
	}

	id := a.library.Record(libraryFP("sha256:rebound"), library.CategoryBehavioral,
		[]string{"text_instead_of_tool"}, "prefer a tool call to prose")

	if err := a.Close(); err != nil {
		t.Fatalf("Close after a Rebind: %v", err)
	}
	if ids := storedIDs(t, dir); !ids[id] {
		t.Errorf("the store on disk = %v; want the rebound registry's observation %q", ids, id)
	}
}

// TestRoutedCatalogueSharesTheSessionsStore covers the third builder: the host resolves a routed
// sub-agent's catalogue through BuildMechanisms (cmd/apogee/delegation.go), which derives Deps with
// no Agent in sight — so the store behind it is reachable by nothing and closable by no one. Sharing
// is what makes it safe: both observations sit in one memory, and the session's Close writes them.
func TestRoutedCatalogueSharesTheSessionsStore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	a, session := libraryAgent(t, dir)
	sessionID := session.Record(libraryFP("sha256:orchestrator"), library.CategoryCorrection,
		[]string{"list_dir"}, "list before you read")

	// The Sub-agent server's own posture: the same Library directory, that server's model.
	routedCfg := baseConfig(&recordingSink{})
	routedCfg.LibraryDir = dir
	routedCfg.Model = "sub-agent-model"
	if _, err := BuildMechanisms(routedCfg, []domain.MechanismID{"library"}); err != nil {
		t.Fatalf("BuildMechanisms for a routed child's catalogue: %v", err)
	}

	routed, err := library.Open(dir)
	if err != nil {
		t.Fatalf("Open the routed catalogue's Library directory: %v", err)
	}
	if routed != session {
		t.Fatalf("the routed catalogue's store = %p; want the session's (%p) — two writers on one file", routed, session)
	}
	routedID := routed.Record(libraryFP("sha256:delegate"), library.CategoryBehavioral,
		[]string{"text_instead_of_tool"}, "answer with a tool call")

	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	ids := storedIDs(t, dir)
	if !ids[sessionID] || !ids[routedID] {
		t.Errorf("the store on disk = %v; want BOTH %q and %q — a second instance would have dropped one",
			ids, sessionID, routedID)
	}
}

// TestChildAgentDoesNotCloseTheStore pins the ownership rule: the session's Agent flushes the
// store, a delegate never does. The store's Close is flush-and-park, so an early flush would be
// harmless — but "the top-level Agent flushes" is the rule that is simple to state, and a delegate
// that wrote mid-session would publish a half-finished run.
//
// The proof needs no timing at all: a regular FILE planted where the store's directory would go
// makes every flush FAIL loudly (the write cannot create its directory), so a Close that flushed
// returns that error and a Close that did not returns nil. Freeing the path again then lets the
// session's own Close publish the observation the delegate left alone.
func TestChildAgentDoesNotCloseTheStore(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "library")

	a, store := libraryAgent(t, dir)

	if err := os.WriteFile(dir, nil, 0o600); err != nil {
		t.Fatalf("block the store's write target: %v", err)
	}
	id := store.Record(libraryFP("sha256:parent"), library.CategoryCorrection,
		[]string{"write_file"}, "read the file before you rewrite it")

	child, err := a.newChildAgent("call_sub", "the delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	if child.depth != 1 {
		t.Fatalf("the spawned child is at depth %d; want 1", child.depth)
	}
	if err := child.Close(); err != nil {
		t.Errorf("a delegate's Close = %v; want nil — the session's store is the parent's to flush", err)
	}

	// A delegate that HOLDS the store is the case the rule is written for: a spawn derives none of
	// its own (its catalogue arrives already built), so ownership has to rest on the depth.
	holding := &Agent{depth: 1, library: store}
	if err := holding.Close(); err != nil {
		t.Errorf("a depth-1 Agent holding the store flushed it (%v); the flush belongs to the session's Close", err)
	}
	// Free the path: the shared instance kept the observation, and the session's Close is what puts
	// it on disk.
	if err := os.Remove(dir); err != nil {
		t.Fatalf("free the store's write target: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("the session's Close: %v", err)
	}
	if ids := storedIDs(t, dir); !ids[id] {
		t.Errorf("the store on disk = %v; want the observation %q the delegates left for the session's Close", ids, id)
	}
}
