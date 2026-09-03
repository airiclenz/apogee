package mechanisms

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/domain/domaintest"
)

// historyView builds a read-only LoopView over history — the window the post-tool-result and
// pre-tool-exec hooks in the Wave-3 history-aware family scan for cross-Turn evidence.
func historyView(history []domain.Message) domain.LoopView {
	return domain.NewRequest("m", history, nil, domain.Budget{}, 0, nil).View()
}

// Every member of the history-aware family is a strikes-3 Mechanism, NOT an exempt off-ramp
// (catalogue C1: apogee narrows exempt to the two true off-ramps), so all suppress normally and are
// disabled under Bypass — the item's "all suppress normally (non-exempt)" guarantee. Each resolves
// to its catalogued hook point.
func TestHistoryFamilyDescriptorsNonExempt(t *testing.T) {
	t.Parallel()
	cases := []struct {
		id   domain.MechanismID
		cap  domain.Capability
		hook func(any) bool
	}{
		{errorEnrichmentID, domain.CapResponseRepair, func(h any) bool { _, ok := h.(domain.PostToolResultHook); return ok }},
		{readLoopID, domain.CapProactiveNudge, func(h any) bool { _, ok := h.(domain.PreRequestHook); return ok }},
		{readRepeatID, domain.CapResponseRepair, func(h any) bool { _, ok := h.(domain.PostResponseHook); return ok }},
	}
	for _, c := range cases {
		m := mustBuild(t, c.id)
		d := m.Descriptor
		if d.ID != c.id {
			t.Errorf("Descriptor.ID = %q, want %q", d.ID, c.id)
		}
		if d.Capability != c.cap {
			t.Errorf("%q Capability = %q, want %q", c.id, d.Capability, c.cap)
		}
		if d.Suppression != domain.SuppressStrikesThree {
			t.Errorf("%q Suppression = %q, want strikes-3 (non-exempt)", c.id, d.Suppression)
		}
		if !c.hook(m.Hook) {
			t.Errorf("%q does not implement its catalogued hook interface", c.id)
		}
	}
}

// The re-read family (read_loop, read_repeat) is pairwise-exclusive on the same wasted-read symptom
// (catalogue Table A / C2); its third member is the read-cache Floor guard now (ADR 0071), which
// carries no descriptor and so no edge. In apogee IncompatibleWith is a startup gate, so the two
// co-registered fail ValidateIncompatibilities — at most one is enabled at a time.
func TestReReadFamilyPairwiseIncompatible(t *testing.T) {
	t.Parallel()
	pairs := [][2]domain.MechanismID{
		{readLoopID, readRepeatID},
	}
	for _, p := range pairs {
		reg := domain.NewMechanismRegistry()
		if err := reg.Add(mustBuild(t, p[0])); err != nil {
			t.Fatalf("Add(%q): %v", p[0], err)
		}
		if err := reg.Add(mustBuild(t, p[1])); err != nil {
			t.Fatalf("Add(%q): %v", p[1], err)
		}
		if err := reg.ValidateIncompatibilities(); !errors.Is(err, domain.ErrIncompatibleMechanisms) {
			t.Errorf("ValidateIncompatibilities(%q,%q) = %v, want ErrIncompatibleMechanisms", p[0], p[1], err)
		}
	}
}

// error_enrichment declares no incompatibility, so it co-registers with a re-read-family member
// cleanly (it is not part of the exclusive symptom).
func TestHistoryFamilyCompatibleMembersCoRegister(t *testing.T) {
	t.Parallel()
	reg := domain.NewMechanismRegistry()
	for _, id := range []domain.MechanismID{errorEnrichmentID, readLoopID} {
		if err := reg.Add(mustBuild(t, id)); err != nil {
			t.Fatalf("Add(%q): %v", id, err)
		}
	}
	if err := reg.ValidateIncompatibilities(); err != nil {
		t.Fatalf("ValidateIncompatibilities: %v", err)
	}
	if err := reg.ValidateOrdering(); err != nil {
		t.Fatalf("ValidateOrdering: %v", err)
	}
}

// The post-response cascade resolves to autofix → syntax (repair precedes correction), with
// read_repeat unconstrained beside them: the two rows that used to head this cascade — the
// tool-loop detector and the tool-call validator — were promoted to Floor guards (ADR 0071) and run
// AHEAD of every hook, so no ordering edge expresses their priority any more and read_repeat's own
// Before edges went with them.
func TestPostResponseCascadeOrder(t *testing.T) {
	t.Parallel()
	reg := domain.NewMechanismRegistry()
	for _, id := range []domain.MechanismID{readRepeatID, autofixID, syntaxID} {
		if err := reg.Add(mustBuild(t, id)); err != nil {
			t.Fatalf("Add(%q): %v", id, err)
		}
	}
	if err := reg.ValidateOrdering(); err != nil {
		t.Fatalf("ValidateOrdering: %v", err)
	}
	want := []domain.MechanismID{autofixID, readRepeatID, syntaxID}
	got := reg.Ordered(domain.HookPostResponse)
	if len(got) != len(want) {
		t.Fatalf("Ordered(post-response) has %d mechanisms, want %d", len(got), len(want))
	}
	for i, m := range got {
		if m.Descriptor.ID != want[i] {
			t.Errorf("cascade[%d] = %q, want %q (full order: %v)", i, m.Descriptor.ID, want[i], want)
		}
	}
}

// historyResponse builds a post-response working value with a FULL LoopView — text, tool calls, the
// tool menu, and a conversation history — the shape the history-scanning post-response Mechanisms
// read (they inspect the history through resp.View().Conversation(), unlike the Wave-1 repair
// Mechanisms that need only the response). The view is a real domain.Request view so
// Conversation()/Tools()/LastUser() behave exactly as in the loop.
func historyResponse(history []domain.Message, tools []domain.ToolDef, text string, calls ...domain.ToolCall) *domain.Response {
	view := domain.NewRequest("m", history, tools, domain.Budget{}, 0, nil).View()
	finish := domain.FinishStop
	if len(calls) > 0 {
		finish = domain.FinishToolCalls
	}
	return domain.NewResponse(text, "", calls, finish, view)
}

// readCall is a read_file tool call over path — the read-shaped progress signal the family counts.
// It and the three message helpers below are thin delegates to the shared hook-seam test adapter
// (internal/domain/domaintest, D6): the package keeps its terse fixture vocabulary, the shapes are
// owned in one place, and new tests use domaintest directly.
func readCall(id, path string) domain.ToolCall { return domaintest.ReadCall(id, path) }

// userMsg / assistantCall are terse conversation-history builders for the cross-Turn trigger tables.
func userMsg(content string) domain.Message { return domaintest.UserMessage(content) }
func assistantCall(calls ...domain.ToolCall) domain.Message {
	return domaintest.AssistantCallsMessage(calls...)
}

// toolCallPath reads the file a call targets from the four sim-inherited spellings plus
// destination, the key copy_file and move_file carry instead. The precedence is pinned here:
// destination is read last, so a call carrying both path and destination still reports path.
func TestToolCallPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args string
		want string
	}{
		{name: "path", args: `{"path":"alpha.go"}`, want: "alpha.go"},
		{name: "file_path", args: `{"file_path":"beta.go"}`, want: "beta.go"},
		{name: "filePath", args: `{"filePath":"gamma.go"}`, want: "gamma.go"},
		{name: "filename", args: `{"filename":"delta.go"}`, want: "delta.go"},
		{
			name: "copy_file reports the destination",
			args: `{"source":"origin.go","destination":"copy.go"}`,
			want: "copy.go",
		},
		{
			name: "move_file reports the destination",
			args: `{"source":"origin.go","destination":"moved.go","overwrite":true}`,
			want: "moved.go",
		},
		{
			name: "path keeps precedence over destination",
			args: `{"destination":"copy.go","path":"alpha.go"}`,
			want: "alpha.go",
		},
		{name: "source alone is not a path", args: `{"source":"origin.go"}`, want: ""},
		{name: "arguments are not a JSON object", args: `"alpha.go"`, want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := toolCallPath(json.RawMessage(tc.args))

			if got != tc.want {
				t.Errorf("toolCallPath(%s) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}
