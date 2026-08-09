package agent

import (
	"context"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/tools"
)

// ----------------------------------------------------------------------------
// The delegation name on the prompt surfaces (ADR 0039 decision 5 — one child, one identity)
// ----------------------------------------------------------------------------
//
// A delegated task is a sentence; the optional name is the few words a human recognises the child
// by. Both prompt paths must carry it — the Approval the loop builds itself and the question the
// ask_user tool builds one boundary away — and an UNNAMED delegation must carry nothing, because ""
// is the signal every surface reads as "fall back to the task".

// namedDelegationScript emits one sub_agent call delegating task under the optional short name.
func namedDelegationScript(id, task, name string) []provider.Delta {
	return toolCallScript(id, tools.SubAgentToolName, subAgentNamedArgs(task, name))
}

// TestDelegationName_RidesApprovalAndAsk drives one named child through BOTH prompt surfaces in a
// single run: it makes a gated tool call and then puts a question to the human, and each request
// must name it by the name its spawning call gave it — alongside, never instead of, the task. The
// unnamed row is the floor: the same run with no name leaves both requests exactly as they were
// before names existed.
func TestDelegationName_RidesApprovalAndAsk(t *testing.T) {
	const (
		parentInput = "delegate the audit"
		childTask   = "audit the config loader"
	)

	tests := []struct {
		label string
		given string
		want  string
	}{
		{"named delegation", "repo-scout", "repo-scout"},
		{"unnamed delegation falls back to nothing", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.label, func(t *testing.T) {
			sink := &recordingSink{}
			approver := &queueProbeApprover{allow: func(domain.ApprovalRequest) bool { return true }}
			asker := &askProbeAsker{answer: func(domain.AskRequest) string { return "the blue one" }}
			cfg := subAgentConfig(sink, domain.ModeAskBefore,
				fakeTool{name: "touch_thing", result: "touched"},
				tools.NewAskUser(asker))
			cfg.Approver = approver

			up := newRoutedResponder().
				route(parentInput, nil, namedDelegationScript("c1", childTask, tc.given)).
				route(childTask, nil, toolCallScript("t1", "touch_thing", `{}`)).
				route(childTask, nil, askUserCallScript("q1", "which one?")).
				route(childTask, nil, contentScript("child done")).
				route(parentInput, nil, contentScript("parent done"))

			a, err := newAgent(cfg, up)
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			if err := a.Submit(domain.UserInput{Text: parentInput}); err != nil {
				t.Fatalf("Submit: %v", err)
			}
			res, err := a.Run(context.Background())
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if res.Status != domain.StatusExchangeComplete {
				t.Fatalf("parent status = %q, want the Exchange to complete", res.Status)
			}

			if len(approver.seen) != 1 {
				t.Fatalf("the human was asked to approve %d calls, want the child's one", len(approver.seen))
			}
			if got := approver.seen[0].SubAgentName; got != tc.want {
				t.Errorf("ApprovalRequest.SubAgentName = %q, want %q", got, tc.want)
			}
			if got := approver.seen[0].SubAgentTask; got != childTask {
				t.Errorf("ApprovalRequest.SubAgentTask = %q, want the delegated task %q — the name "+
					"rides BESIDE the task, it does not replace it", got, childTask)
			}

			if len(asker.seen) != 1 {
				t.Fatalf("the human was asked %d questions, want the child's one", len(asker.seen))
			}
			if got := asker.seen[0].SubAgentName; got != tc.want {
				t.Errorf("AskRequest.SubAgentName = %q, want %q", got, tc.want)
			}
			if got := asker.seen[0].SubAgentTask; got != childTask {
				t.Errorf("AskRequest.SubAgentTask = %q, want the delegated task %q", got, childTask)
			}
		})
	}
}

// TestDelegationName_NormalisedOnTheWayToThePrompts pins that a prompt never has to defend itself
// against a padded or multi-line name: the recursion point normalises once, and what reaches the
// Approver is already the trimmed first line.
func TestDelegationName_NormalisedOnTheWayToThePrompts(t *testing.T) {
	const (
		parentInput = "delegate the audit"
		childTask   = "audit the config loader"
	)

	sink := &recordingSink{}
	approver := &queueProbeApprover{allow: func(domain.ApprovalRequest) bool { return true }}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, fakeTool{name: "touch_thing", result: "touched"})
	cfg.Approver = approver

	up := newRoutedResponder().
		route(parentInput, nil, namedDelegationScript("c1", childTask, "  repo-scout \n and some prose")).
		route(childTask, nil, toolCallScript("t1", "touch_thing", `{}`)).
		route(childTask, nil, contentScript("child done")).
		route(parentInput, nil, contentScript("parent done"))

	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: parentInput}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(approver.seen) != 1 {
		t.Fatalf("the human was asked to approve %d calls, want the child's one", len(approver.seen))
	}
	if got := approver.seen[0].SubAgentName; got != "repo-scout" {
		t.Errorf("ApprovalRequest.SubAgentName = %q, want the trimmed first line %q", got, "repo-scout")
	}
}
