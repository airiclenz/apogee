package agent

// Regression cover for the two tool-stage acted probes (R4), driven through the real
// Submit → Step → dispatchTools → runPreToolExecHooks / runPostToolResultHooks path.
//
// Both now read a ToolCallEdit / ToolResultEdit Revision counter, and the post-tool-result
// case is where that matters: the whole-struct compare the counter replaces panicked with
// "comparing uncomparable type …" whenever a summary-carrying result reached a hook that did
// not act — the shape every successful read_file produces (ReadSpan carries the []int of
// located lines), and error_enrichment (the only catalogued post-tool-result Mechanism) never
// acts on a non-error result. No existing test drove a summary-carrying result through
// dispatch, which is why CI never tripped; these do.

import (
	"context"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// locatedReadSummary is the uncomparable summary read_file returns on every successful read:
// LocatedOn is a slice, so a struct compare of two ToolResults carrying one panics.
func locatedReadSummary() domain.ToolSummary {
	return domain.ReadSpan{Start: 1, End: 3, Total: 3, Locate: "main", LocatedOn: []int{1, 2}}
}

// summaryTool is the "probe" tool driveToolExchange calls, returning a result whose Summary
// holds that uncomparable value.
func summaryTool() fakeTool {
	return fakeTool{
		name:     "probe",
		readOnly: true,
		execute: func(_ context.Context, call domain.ToolCall) (domain.ToolResult, error) {
			return domain.ToolResult{
				CallID:  call.ID,
				Content: "package main\nfunc main() {}\n",
				Summary: locatedReadSummary(),
			}, nil
		},
	}
}

// postToolResultMech is a catalogued post-tool-result Mechanism that applies mutate (nil for a
// pure inspect-and-do-nothing hook) and counts its invocations. It is catalogued as an off-ramp
// so neither the Bypass gate nor self-regulation can withdraw it — this fixture probes the
// acted probe, not dispatch gating.
type postToolResultMech struct {
	id     domain.MechanismID
	calls  *int
	mutate func(*domain.ToolResultEdit)
}

func (m postToolResultMech) row() domain.RegisteredMechanism {
	return domain.RegisteredMechanism{
		Descriptor: domain.MechanismDescriptor{ID: m.id, Capability: domain.CapOffRamp},
		Hook:       m,
	}
}

func (m postToolResultMech) PostToolResult(_ context.Context, _ domain.ToolCall, result *domain.ToolResultEdit, _ domain.LoopView) error {
	*m.calls++
	if m.mutate != nil {
		m.mutate(result)
	}
	return nil
}

// preToolExecMech is postToolResultMech's pre-tool-exec twin: a catalogued off-ramp Mechanism
// that applies mutate (nil for a pure inspect-and-do-nothing hook) to the pending call and
// counts its invocations.
type preToolExecMech struct {
	id     domain.MechanismID
	calls  *int
	mutate func(*domain.ToolCallEdit)
}

func (m preToolExecMech) row() domain.RegisteredMechanism {
	return domain.RegisteredMechanism{
		Descriptor: domain.MechanismDescriptor{ID: m.id, Capability: domain.CapOffRamp},
		Hook:       m,
	}
}

func (m preToolExecMech) PreToolExec(_ context.Context, call *domain.ToolCallEdit, _ domain.LoopView) error {
	*m.calls++
	if m.mutate != nil {
		m.mutate(call)
	}
	return nil
}

// firesFor counts the booked fires attributed to id.
func firesFor(events []domain.Event, id domain.MechanismID) int {
	n := 0
	for _, fe := range mechanismFires(events) {
		if fe.Mechanism == id {
			n++
		}
	}
	return n
}

// TestPostToolResultActedProbe pins the acted probe against a summary-carrying result: a
// no-op hook completes without panicking and books nothing, while a hook that touches either
// half of the result — the prose or the structured summary — books its fire.
func TestPostToolResultActedProbe(t *testing.T) {
	const mechID domain.MechanismID = "probe_mech"

	tests := []struct {
		name      string
		mutate    func(*domain.ToolResultEdit)
		wantFires int
	}{
		{
			name:      "no-op hook on an uncomparable summary books no fire",
			mutate:    nil,
			wantFires: 0,
		},
		{
			name:      "a Content mutation books a fire",
			mutate:    func(r *domain.ToolResultEdit) { r.SetContent(r.Content() + "\n[enriched]") },
			wantFires: 1,
		},
		{
			name: "a Summary-only mutation books a fire",
			mutate: func(r *domain.ToolResultEdit) {
				r.SetSummary(domain.ReadSpan{Start: 1, End: 99, Total: 99, LocatedOn: []int{7}})
			},
			wantFires: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The pre-fix compare panics inside Step; recover so one case's crash reports as
			// a failure of that case instead of taking the whole test binary down.
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("dispatch panicked on a summary-carrying tool result: %v", r)
				}
			}()

			sink := &recordingSink{}
			cfg := configWithTools(sink, summaryTool())
			cfg.Mechanisms = domain.NewMechanismRegistry()
			calls := 0
			mustAddMech(t, cfg.Mechanisms, postToolResultMech{id: mechID, calls: &calls, mutate: tt.mutate}.row())

			driveToolExchange(t, cfg)

			if calls != 1 {
				t.Fatalf("post-tool-result hook ran %d times, want 1 (it must reach the acted probe)", calls)
			}
			if got := firesFor(sink.events, mechID); got != tt.wantFires {
				t.Errorf("booked fires = %d, want %d", got, tt.wantFires)
			}
		})
	}
}

// TestPreToolExecActedProbe is the pre-tool-exec half of the same pin: a hook that inspects
// the pending call and leaves it alone books nothing, while one that reshapes it through a
// ToolCallEdit mutator books its fire.
func TestPreToolExecActedProbe(t *testing.T) {
	const mechID domain.MechanismID = "pre_probe_mech"

	tests := []struct {
		name      string
		mutate    func(*domain.ToolCallEdit)
		wantFires int
	}{
		{
			name:      "an inspect-only hook books no fire",
			mutate:    nil,
			wantFires: 0,
		},
		{
			name:      "an Arguments mutation books a fire",
			mutate:    func(c *domain.ToolCallEdit) { c.SetArguments([]byte(`{"max_lines":40}`)) },
			wantFires: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &recordingSink{}
			cfg := configWithTools(sink, summaryTool())
			cfg.Mechanisms = domain.NewMechanismRegistry()
			calls := 0
			mustAddMech(t, cfg.Mechanisms, preToolExecMech{id: mechID, calls: &calls, mutate: tt.mutate}.row())

			driveToolExchange(t, cfg)

			if calls != 1 {
				t.Fatalf("pre-tool-exec hook ran %d times, want 1 (it must reach the acted probe)", calls)
			}
			if got := firesFor(sink.events, mechID); got != tt.wantFires {
				t.Errorf("booked fires = %d, want %d", got, tt.wantFires)
			}
		})
	}
}
