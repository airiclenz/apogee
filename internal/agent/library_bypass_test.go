package agent

// Loop-level guard for the item-14 mandate that Bypass leaves a pre-seeded Library store
// byte-for-byte untouched (phase-4-detail-plan §636; second-review fix item 8). The wire path
// DOES Load() the store under Bypass+enabled, so a populated store is live in memory during the
// Exchange — this test proves that driving a full Exchange whose shape WOULD record an
// observation (the shallow-exploration pattern: list files, read none, on an analysis-intent
// request — the positive control is mechanisms.TestLibraryObserveRecordsShallowExploration) still
// writes nothing to disk, because the Bypass gate withdraws the library Mechanism's proactive-nudge
// Capability at both the inject (pre-request) and observe (post-response) hooks.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/library"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/provider"
)

// TestLibrary_BypassLeavesPreSeededStoreUntouched seeds a temp LibraryDir with a populated, valid
// library.json (written through a library.Store so the on-disk format stays canonical), wires a
// registry-backed agent with the catalogued `library` Mechanism enabled AND Config.Bypass on, drives
// an Exchange whose shape would otherwise trigger an observe-time Record, and asserts the store
// file's bytes are identical before and after.
func TestLibrary_BypassLeavesPreSeededStoreUntouched(t *testing.T) {
	dir := t.TempDir()
	fp := domain.ModelFingerprint{Label: "sha256:m", Confidence: domain.ConfidenceHigh}

	// Seed a populated, valid store through a real library.Store so the file is canonical. Two
	// Records make the entry qualifying (obs >= 2), the same shape the store's own tests seed.
	seed := library.NewStore(dir)
	seed.Record(fp, library.CategoryBehavioral, []string{"behavioral", "text_instead_of_tool"}, "Always prefer tool calls.")
	seed.Record(fp, library.CategoryBehavioral, []string{"behavioral", "text_instead_of_tool"}, "Always prefer tool calls.")

	storePath := filepath.Join(dir, "library.json")
	before, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read seeded store: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("seeded store is empty; the test needs a populated store to be meaningful")
	}

	// Mirror the wire path: under Bypass+enabled the store is still Load()ed and injected, so it is
	// live in memory for the whole Exchange.
	loaded := library.NewStore(dir)
	if err := loaded.Load(); err != nil {
		t.Fatalf("Load seeded store: %v", err)
	}
	m, err := mechanisms.Build(domain.MechanismID("library"), mechanisms.Deps{Library: loaded, Fingerprint: fp})
	if err != nil {
		t.Fatalf("Build(library): %v", err)
	}

	sink := &recordingSink{}
	ran := 0
	cfg := configWithTools(sink, fakeTool{name: "list_dir", readOnly: true, ran: &ran, result: "a.go\nb.go"})
	cfg.Bypass = true
	cfg.Mechanisms = domain.NewMechanismRegistry()
	mustAddMech(t, cfg.Mechanisms, m)

	// Turn 0 lists files but reads none on an analysis-intent request — with the library active this
	// records a shallow_exploration entry (and persists); under Bypass it must not.
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", "list_dir", `{}`),
		contentScript("here is the summary"),
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	res := runExchange(t, a, "summarize the code in this package")

	// The observe-triggering shape genuinely ran, so the negative result below is not vacuous.
	if res.Status != domain.StatusExchangeComplete {
		t.Fatalf("Exchange status = %q, want %q", res.Status, domain.StatusExchangeComplete)
	}
	if ran != 1 {
		t.Fatalf("list_dir ran %d times, want 1 (the observe-triggering shape must have executed)", ran)
	}

	after, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read store after Exchange: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("Bypass mutated the pre-seeded library store:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}
