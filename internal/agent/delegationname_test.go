package agent

import (
	"context"
	"os"
	"path/filepath"
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
// before names existed. The third row is the same claim for a name the model did NOT supply: a
// delegation named out of band (ADR 0068) is renamed mid-run, and every prompt raised after the
// rename must name it by what it is called NOW rather than by the name it was spawned with.
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
			asker := &askProbeAsker{answer: func(domain.AskRequest) string { return "the blue one" }}
			cfg := subAgentConfig(sink, domain.ModeAskBefore,
				fakeTool{name: "touch_thing", result: "touched"},
				tools.NewAskUser(asker))
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
			// The number the two name fields cannot supply: a named grandchild reads like a named
			// child until the request says how deep the asking run is. It rides the same ctx the
			// task does, so the unnamed row must carry it too.
			if got := asker.seen[0].Depth; got != 1 {
				t.Errorf("AskRequest.Depth = %d, want 1 — the child asking runs one level down", got)
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

// TestPresentIdentity_RidesTheDispatchCtxOntoTheRequest is the presenter half of the same seam: a
// depth-1 child presents a document and the host Presenter must be told WHICH run showed it — the
// nesting depth to draw it at, and the spawning call id that picks the right sibling run when a
// fan-out has several going at once (ADR 0039). Without both, a child's deliverable surfaces as
// though the top-level agent had presented it.
func TestPresentIdentity_RidesTheDispatchCtxOntoTheRequest(t *testing.T) {
	const (
		parentInput = "delegate the write-up"
		childTask   = "write the architecture review"
		spawnCallID = "c1"
	)

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "review.md"), []byte("# review"), 0o600); err != nil {
		t.Fatalf("seed review: %v", err)
	}

	presenter := &recordingPresenter{}
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, tools.NewPresentDocument(root, presenter))

	up := newRoutedResponder().
		route(parentInput, nil, subAgentCallScript(spawnCallID, childTask)).
		route(childTask, nil, toolCallScript("p1", "present_document", `{"path":"review.md"}`)).
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

	if len(presenter.seen) != 1 {
		t.Fatalf("the host presented %d documents, want the child's one", len(presenter.seen))
	}
	req := presenter.seen[0]
	if req.DisplayPath != "review.md" {
		t.Errorf("PresentRequest.DisplayPath = %q, want %q", req.DisplayPath, "review.md")
	}
	if req.Depth != 1 {
		t.Errorf("PresentRequest.Depth = %d, want 1 — the presenting child runs one level down", req.Depth)
	}
	if req.SpawnCallID != spawnCallID {
		t.Errorf("PresentRequest.SpawnCallID = %q, want the spawning call's id %q — depth alone "+
			"cannot pick the run among concurrent siblings", req.SpawnCallID, spawnCallID)
	}
}

// TestPresentIdentity_TopLevelRunPresentsAtDepthZero is the floor beneath it: the same tool called
// by the top-level agent reports depth 0 and no spawning call — honest values for the outermost
// run, so a Driver never has to tell "absent" from "outermost".
func TestPresentIdentity_TopLevelRunPresentsAtDepthZero(t *testing.T) {
	const userInput = "show me the review"

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "review.md"), []byte("# review"), 0o600); err != nil {
		t.Fatalf("seed review: %v", err)
	}

	presenter := &recordingPresenter{}
	cfg := subAgentConfig(&recordingSink{}, domain.ModeAskBefore, tools.NewPresentDocument(root, presenter))

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
