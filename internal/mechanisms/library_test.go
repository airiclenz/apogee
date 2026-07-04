package mechanisms

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/library"
)

// libFP builds a fingerprint with an explicit confidence tier — the identity the store keys on and
// the inject gate reads.
func libFP(label string, c domain.FingerprintConfidence) domain.ModelFingerprint {
	return domain.ModelFingerprint{Label: label, Confidence: c}
}

// newLibraryMech builds the library Mechanism directly over a store + fingerprint, bypassing the
// catalogue so a test controls both injected collaborators (D3).
func newLibraryMech(st *library.Store, fp domain.ModelFingerprint) *libraryMechanism {
	return &libraryMechanism{store: st, fingerprint: fp}
}

// seedQualifying records the same observation twice so it clears the store's query gate (obs >= 2,
// score >= 0.5) and is a candidate for injection.
func seedQualifying(st *library.Store, fp domain.ModelFingerprint, cat library.Category, tags []string, content string) {
	st.Record(fp, cat, tags, content)
	st.Record(fp, cat, tags, content)
}

// observeResponse builds a post-response working value carrying calls and a real conversation view
// (from a Request) so the observer's LastUser / Fired / Tools reads resolve.
func observeResponse(history []domain.Message, tools []domain.ToolDef, calls ...domain.ToolCall) *domain.Response {
	view := domain.NewRequest("m", history, tools, domain.Budget{}, 0, nil).View()
	return domain.NewResponse("", "", calls, domain.FinishToolCalls, view)
}

// The single catalogue `library` row is realized as one proactive-nudge / strikes-3 Mechanism that
// implements BOTH hooks — pre-request (inject) and post-response (observe). Its proactive-nudge
// Capability is the lever item 2's Bypass gate skips on, so the Library is inert under Bypass.
func TestLibraryDescriptorAndHooks(t *testing.T) {
	t.Parallel()
	m := newLibraryMech(library.NewStore(t.TempDir()), libFP("sha256:m", domain.ConfidenceHigh))
	d := m.Descriptor()
	if d.ID != libraryID {
		t.Errorf("ID = %q, want %q", d.ID, libraryID)
	}
	if d.Capability != domain.CapProactiveNudge {
		t.Errorf("Capability = %q, want proactive-nudge (the Bypass gate's lever)", d.Capability)
	}
	if d.Suppression != domain.SuppressStrikesThree {
		t.Errorf("Suppression = %q, want strikes-3", d.Suppression)
	}
	if o := m.Ordering(); len(o.Before) != 0 || len(o.After) != 0 {
		t.Errorf("Ordering = %+v, want none (catalogue Table A)", o)
	}
	if _, ok := domain.Mechanism(m).(domain.PreRequestHook); !ok {
		t.Error("library does not implement PreRequestHook (the inject half)")
	}
	if _, ok := domain.Mechanism(m).(domain.PostResponseHook); !ok {
		t.Error("library does not implement PostResponseHook (the observe half)")
	}
}

// The catalogue builds library only with a store injected (D3): a nil store is a loud construction
// error, a store builds cleanly.
func TestLibraryBuildRequiresStore(t *testing.T) {
	t.Parallel()
	if _, err := Build(libraryID, Deps{}); !errors.Is(err, errLibraryStoreRequired) {
		t.Errorf("Build(library, no store): err = %v; want errLibraryStoreRequired", err)
	}
	m, err := Build(libraryID, Deps{Library: library.NewStore(t.TempDir()), Fingerprint: libFP("sha256:m", domain.ConfidenceHigh)})
	if err != nil {
		t.Fatalf("Build(library, store): %v", err)
	}
	if m.Descriptor().ID != libraryID {
		t.Errorf("built ID = %q, want %q", m.Descriptor().ID, libraryID)
	}
}

// A high-confidence fingerprint injects its qualifying notes into the system prompt; a second pass is
// a no-op (the marker makes the inject idempotent).
func TestLibraryInjectsAboveConfidenceGateMarkerDeduped(t *testing.T) {
	t.Parallel()
	st := library.NewStore(t.TempDir())
	fp := libFP("sha256:m", domain.ConfidenceHigh)
	seedQualifying(st, fp, library.CategoryBehavioral, []string{"behavioral", "text_instead_of_tool"}, "Always prefer tool calls.")
	m := newLibraryMech(st, fp)

	req := domain.NewRequest("m", []domain.Message{
		{Role: domain.RoleSystem, Content: "SYS"},
		{Role: domain.RoleUser, Content: "update the config file"},
	}, oneTool, domain.Budget{}, 0, nil)

	before := req.Revision()
	if err := m.PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() == before {
		t.Fatal("a high-confidence fingerprint with qualifying notes should have injected")
	}
	sys := req.State().Messages[0].Content
	if !strings.Contains(sys, libraryInjectionMarker) {
		t.Errorf("injection marker %q not in system prompt: %q", libraryInjectionMarker, sys)
	}
	if !strings.Contains(sys, "Always prefer tool calls.") {
		t.Errorf("injected block missing the note content: %q", sys)
	}

	// Second pass: the marker is present, so nothing is re-injected.
	mid := req.Revision()
	if err := m.PreRequest(context.Background(), req); err != nil {
		t.Fatalf("second PreRequest: %v", err)
	}
	if req.Revision() != mid {
		t.Fatal("library re-injected despite its marker already being present")
	}
}

// A low-confidence fingerprint does not inject even with qualifying notes present — the confidence
// gate ("prefer not to inject under uncertainty"). A zero fingerprint is likewise inert.
func TestLibraryConfidenceGateBlocksLowAndZero(t *testing.T) {
	t.Parallel()
	st := library.NewStore(t.TempDir())
	// Seed under the same label so entries exist; the gate, not an empty query, is what blocks inject.
	seedQualifying(st, libFP("sha256:m", domain.ConfidenceHigh), library.CategoryBehavioral, []string{"behavioral", "text_instead_of_tool"}, "Always prefer tool calls.")

	req := func() *domain.Request {
		return domain.NewRequest("m", []domain.Message{
			{Role: domain.RoleSystem, Content: "SYS"},
			{Role: domain.RoleUser, Content: "update the config file"},
		}, oneTool, domain.Budget{}, 0, nil)
	}

	for _, fp := range []domain.ModelFingerprint{libFP("sha256:m", domain.ConfidenceLow), {}} {
		r := req()
		if err := newLibraryMech(st, fp).PreRequest(context.Background(), r); err != nil {
			t.Fatalf("PreRequest(%+v): %v", fp, err)
		}
		if r.Revision() != 0 {
			t.Errorf("fingerprint %+v injected below the confidence gate: %q", fp, r.State().Messages[0].Content)
		}
	}
}

// The intent filter drops analysis-only entries when the request lacks analysis intent, and keeps
// them when it has it (apogee-sim WithRequestIntent / analysisOnlyTags).
func TestLibraryInjectIntentFilter(t *testing.T) {
	t.Parallel()
	st := library.NewStore(t.TempDir())
	fp := libFP("sha256:m", domain.ConfidenceHigh)
	seedQualifying(st, fp, library.CategoryBehavioral, []string{"behavioral", "shallow_exploration"}, "Read files before summarizing.")
	m := newLibraryMech(st, fp)

	// Action-intent request: the analysis-only entry is filtered out, so nothing injects.
	action := domain.NewRequest("m", []domain.Message{
		{Role: domain.RoleSystem, Content: "SYS"},
		{Role: domain.RoleUser, Content: "update the config file"},
	}, oneTool, domain.Budget{}, 0, nil)
	if err := m.PreRequest(context.Background(), action); err != nil {
		t.Fatalf("PreRequest (action): %v", err)
	}
	if action.Revision() != 0 {
		t.Errorf("an analysis-only note injected on an action request: %q", action.State().Messages[0].Content)
	}

	// Analysis-intent request: the entry survives the filter and injects.
	analysis := domain.NewRequest("m", []domain.Message{
		{Role: domain.RoleSystem, Content: "SYS"},
		{Role: domain.RoleUser, Content: "summarize the code in this package"},
	}, oneTool, domain.Budget{}, 0, nil)
	if err := m.PreRequest(context.Background(), analysis); err != nil {
		t.Fatalf("PreRequest (analysis): %v", err)
	}
	if !strings.Contains(analysis.State().Messages[0].Content, "Read files before summarizing.") {
		t.Errorf("analysis-only note should inject on an analysis request: %q", analysis.State().Messages[0].Content)
	}
}

// Injection backs off when the window is nearly full (apogee-sim usage > 0.85), even with qualifying
// notes present.
func TestLibraryInjectContextFullBackoff(t *testing.T) {
	t.Parallel()
	st := library.NewStore(t.TempDir())
	fp := libFP("sha256:m", domain.ConfidenceHigh)
	seedQualifying(st, fp, library.CategoryBehavioral, []string{"behavioral", "text_instead_of_tool"}, "Always prefer tool calls.")

	// ContextLimit 100 tokens, 1 char/token; the messages total > 85 chars → usage > 0.85.
	full := domain.NewRequest("m", []domain.Message{
		{Role: domain.RoleSystem, Content: strings.Repeat("x", 80)},
		{Role: domain.RoleUser, Content: "update the config file now please"},
	}, oneTool, domain.Budget{ContextLimit: 100, CharsPerToken: 1}, 0, nil)

	if err := newLibraryMech(st, fp).PreRequest(context.Background(), full); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if full.Revision() != 0 {
		t.Errorf("library injected into a nearly-full window: %q", full.State().Messages[0].Content)
	}
}

// A note larger than the injection budget is dropped rather than injected (apogee-sim injection-budget
// token cap, deferred to this Mechanism by item 13).
func TestLibraryInjectBudgetCap(t *testing.T) {
	t.Parallel()
	st := library.NewStore(t.TempDir())
	fp := libFP("sha256:m", domain.ConfidenceHigh)
	// ~700 chars at the default 3 chars/token ≈ 233 tokens, over the 200-token budget.
	huge := strings.Repeat("prefer tools. ", 50)
	seedQualifying(st, fp, library.CategoryBehavioral, []string{"behavioral", "text_instead_of_tool"}, huge)

	req := domain.NewRequest("m", []domain.Message{
		{Role: domain.RoleSystem, Content: "SYS"},
		{Role: domain.RoleUser, Content: "update the config file"},
	}, oneTool, domain.Budget{}, 0, nil)
	if err := newLibraryMech(st, fp).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() != 0 {
		t.Errorf("an over-budget note should not inject: %q", req.State().Messages[0].Content)
	}
}

// The observe half records a correction keyed on the fingerprint when the model calls an unknown
// tool.
func TestLibraryObserveRecordsCorrectionKeyedOnFingerprint(t *testing.T) {
	t.Parallel()
	st := library.NewStore(t.TempDir())
	fp := libFP("sha256:m", domain.ConfidenceHigh)
	m := newLibraryMech(st, fp)

	resp := observeResponse(nil, toolMenu(), domain.ToolCall{ID: "c1", Tool: "frobnicate", Arguments: json.RawMessage(`{}`)})
	if _, err := m.PostResponse(context.Background(), resp); err != nil {
		t.Fatalf("PostResponse: %v", err)
	}

	all := st.All()
	if len(all) == 0 {
		t.Fatal("observe recorded nothing for an unknown-tool call")
	}
	found := false
	for _, e := range all {
		if e.ModelLabel != fp.Label {
			t.Errorf("entry keyed on %q; want the fingerprint label %q", e.ModelLabel, fp.Label)
		}
		if e.Category == library.CategoryCorrection && e.HasTag("wrong_tool_name") {
			found = true
		}
	}
	if !found {
		t.Errorf("no wrong_tool_name correction recorded; entries = %+v", all)
	}
}

// The observe half records the shallow-exploration behavioural pattern: listed files, read none, on
// an analysis-intent request.
func TestLibraryObserveRecordsShallowExploration(t *testing.T) {
	t.Parallel()
	st := library.NewStore(t.TempDir())
	fp := libFP("sha256:m", domain.ConfidenceHigh)
	m := newLibraryMech(st, fp)

	tools := []domain.ToolDef{{Name: "list_dir"}}
	history := []domain.Message{{Role: domain.RoleUser, Content: "summarize the code in this package"}}
	resp := observeResponse(history, tools, domain.ToolCall{ID: "c1", Tool: "list_dir", Arguments: json.RawMessage(`{}`)})
	if _, err := m.PostResponse(context.Background(), resp); err != nil {
		t.Fatalf("PostResponse: %v", err)
	}

	all := st.All()
	if len(all) != 1 || all[0].Category != library.CategoryBehavioral || !all[0].HasTag("shallow_exploration") {
		t.Errorf("want one behavioral shallow_exploration entry; got %+v", all)
	}
}

// A zero (unidentified) fingerprint makes the observer inert: nothing is recorded and no store file
// is written.
func TestLibraryObserveZeroFingerprintInert(t *testing.T) {
	t.Parallel()
	st := library.NewStore(t.TempDir())
	m := newLibraryMech(st, domain.ModelFingerprint{})

	resp := observeResponse(nil, toolMenu(), domain.ToolCall{ID: "c1", Tool: "frobnicate", Arguments: json.RawMessage(`{}`)})
	if _, err := m.PostResponse(context.Background(), resp); err != nil {
		t.Fatalf("PostResponse: %v", err)
	}
	if st.Count() != 0 {
		t.Errorf("observe on a zero fingerprint recorded %d entries; want 0", st.Count())
	}
}

// Two Mechanisms rooted at two dirs do not cross-contaminate: each store holds only its own
// fingerprint's observations (decision 11 — isolation falls out of the injected root).
func TestLibraryIsolatedRootsDoNotCrossContaminate(t *testing.T) {
	t.Parallel()
	stA := library.NewStore(t.TempDir())
	stB := library.NewStore(t.TempDir())
	fpA := libFP("sha256:a", domain.ConfidenceHigh)
	fpB := libFP("sha256:b", domain.ConfidenceHigh)

	bad := func() *domain.Response {
		return observeResponse(nil, toolMenu(), domain.ToolCall{ID: "c1", Tool: "frobnicate", Arguments: json.RawMessage(`{}`)})
	}
	if _, err := newLibraryMech(stA, fpA).PostResponse(context.Background(), bad()); err != nil {
		t.Fatalf("observe A: %v", err)
	}
	if _, err := newLibraryMech(stB, fpB).PostResponse(context.Background(), bad()); err != nil {
		t.Fatalf("observe B: %v", err)
	}

	assertOnlyLabel := func(name string, st *library.Store, label string) {
		all := st.All()
		if len(all) == 0 {
			t.Errorf("%s recorded nothing", name)
		}
		for _, e := range all {
			if e.ModelLabel != label {
				t.Errorf("%s holds a foreign entry keyed on %q; want only %q", name, e.ModelLabel, label)
			}
		}
	}
	assertOnlyLabel("store A", stA, fpA.Label)
	assertOnlyLabel("store B", stB, fpB.Label)
}
