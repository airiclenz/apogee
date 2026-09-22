package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tools"
)

// ----------------------------------------------------------------------------
// The delegation name on the prompt surfaces (ADR 0039 decision 5 — one child, one identity)
// ----------------------------------------------------------------------------
//
// A delegated task is a sentence; the optional name is the few words a human recognises the child
// by. The Approval the loop builds itself must carry it, and an UNNAMED delegation must carry
// nothing, because "" is the signal every surface reads as "fall back to the task". The question
// path — the AskRequest the ask_user tool builds one boundary away — carried the same identity
// until 2026-09-15; plan 2026-09-14 - 03, item 5 withholds ask_user (and present_document) from
// every sub-agent, so no child can raise one and the ask half of these tests is retired. The
// carriers the tool read (WithSubAgentTask/Name/Depth, WithSpawnCallID) still ride every child
// call; only the two requests that used to be built under them are gone.

// namedDelegationScript emits one sub_agent call delegating task under the optional short name —
// the Delta script a routedResponder plays; namedDelegationTurn is its stubllm twin.
func namedDelegationScript(id, task, name string) []provider.Delta {
	return toolCallScript(id, tools.SubAgentToolName, subAgentNamedArgs(task, name))
}

// namedDelegationTurn is a turn that emits one sub_agent call delegating task under the optional
// short name.
func namedDelegationTurn(id, task, name string) stubllm.Turn {
	return toolCallTurn(id, tools.SubAgentToolName, subAgentNamedArgs(task, name))
}

// TestDelegationName_RidesApprovalAndAsk drives one named child through the Approval prompt: it
// makes a gated tool call, and the request must name it by the name its spawning call gave it —
// alongside, never instead of, the task. The unnamed row is the floor: the same run with no name
// leaves the request exactly as it was before names existed. The third row is the same claim for a
// name the model did NOT supply: a delegation named out of band (ADR 0068) is renamed mid-run, and
// every prompt raised after the rename must name it by what it is called NOW rather than by the
// name it was spawned with (ADR 0068's rename-reaches-the-prompt row).
//
// The "AndAsk" half of its name is history (2026-09-15, plan 2026-09-14 - 03, item 5): the same
// run used to put a question to the human as well and assert the AskRequest carried the same
// identity, and a child can no longer ask. The name is kept so the test's history stays findable.
func TestDelegationName_RidesApprovalAndAsk(t *testing.T) {
	const (
		parentInput = "delegate the audit"
		childTask   = "audit the config loader"
	)

	tests := []struct {
		label string
		given string
		namer *stubNamer
		want  string
	}{
		{"named delegation", "repo-scout", nil, "repo-scout"},
		{"unnamed delegation falls back to nothing", "", nil, ""},
		{"a generated name reaches both prompts", "", &stubNamer{reply: "Config Loader Audit"}, "Config Loader Audit"},
	}
	for _, tc := range tests {
		t.Run(tc.label, func(t *testing.T) {
			sink := newLockedSink()
			approver := &queueProbeApprover{allow: func(domain.ApprovalRequest) bool { return true }}
			cfg := subAgentConfig(sink, domain.ModeAskBefore,
				fakeTool{name: "touch_thing", result: "touched"})
			cfg.Approver = approver
			// The rename has to have LANDED before the child's first gated call, or the prompt
			// would be built from the name the spawn carried and the row would prove nothing.
			// Gating the child's own first reply is the one place that ordering can be pinned.
			var gate func(context.Context)
			if tc.namer != nil {
				cfg.Namer = tc.namer
				gate = func(context.Context) { sink.awaitRename() }
			}

			up := newRoutedResponder().
				route(parentInput, nil, namedDelegationScript("c1", childTask, tc.given)).
				route(childTask, gate, toolCallScript("t1", "touch_thing", `{}`)).
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
		})
	}
}

// recordingPresenter is a host Presenter that keeps every request it was handed and always answers
// with the baseline rung — the hermetic stand-in for a Driver that owns the presentation ladder.
// It is called from the Agent's own worker goroutine, one presentation at a time in these runs, so
// it needs no lock.
type recordingPresenter struct {
	seen []domain.PresentRequest
}

func (p *recordingPresenter) Present(_ context.Context, req domain.PresentRequest) (domain.PresentOutcome, error) {
	p.seen = append(p.seen, req)
	return domain.PresentOutcome{Method: domain.PresentShown, Location: req.DisplayPath}, nil
}

// IsExecutionCapable: this double records requests; it wires no opener, so it can execute
// nothing of the user's choosing.
func (*recordingPresenter) IsExecutionCapable() bool { return false }

// TestPresentIdentity_TopLevelRunPresentsAtDepthZero pins the presenter half of the identity seam
// at the one depth it is still reached from: the tool called by the top-level agent reports depth 0
// and no spawning call — honest values for the outermost run, so a Driver never has to tell
// "absent" from "outermost". Its depth-1 twin (a child presenting, the request carrying Depth 1 and
// the spawning call id) was retired on 2026-09-15 with plan 2026-09-14 - 03, item 5: present_document
// is withheld from every sub-agent, so a child presentation is no longer reachable.
func TestPresentIdentity_TopLevelRunPresentsAtDepthZero(t *testing.T) {
	const userInput = "show me the review"

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "review.md"), []byte("# review"), 0o600); err != nil {
		t.Fatalf("seed review: %v", err)
	}

	presenter := &recordingPresenter{}
	cfg := subAgentConfig(&recordingSink{}, domain.ModeAskBefore, tools.NewPresentDocument(root, tools.ReadMounts{}, presenter))

	up := newRoutedResponder().
		route(userInput, nil, toolCallScript("p1", "present_document", `{"path":"review.md"}`)).
		route(userInput, nil, contentScript("shown"))

	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: userInput}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(presenter.seen) != 1 {
		t.Fatalf("the host presented %d documents, want the one", len(presenter.seen))
	}
	if got := presenter.seen[0].Depth; got != 0 {
		t.Errorf("PresentRequest.Depth = %d, want 0 for the top-level agent", got)
	}
	if got := presenter.seen[0].SpawnCallID; got != "" {
		t.Errorf("PresentRequest.SpawnCallID = %q, want empty — no sub_agent call spawned the "+
			"top-level agent", got)
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
