package agent

// The engine-owned delegate report block (delegatereport.go): WHO carries it, WHERE it sits, and
// the two invariants it must not break — a top-level session's standing content is untouched to
// the byte, and the ride-along rule still leaves a child with neither configured source seeding
// nothing at all. The assertions take the block's text from the exported const rather than
// restating it: it is a contract line a Driver asserts too (apogee.DelegateReportBlock), and one
// copy of the bytes is the point.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// delegateReportConfig returns the minimum that seeds a standing system message from BOTH
// configured sources at once — a prompt template and one workspace context file — so a child's
// whole composition can be asserted in order.
func delegateReportConfig(t *testing.T) domain.Config {
	t.Helper()
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "AGENTS.md", "Run make check before committing.")
	cfg := contextSeamConfig(t, &recordingSink{}, dir, "AGENTS.md")
	cfg.SystemPrompt = "You are apogee working in {{workspace}}."
	cfg.ScratchDir = orientationScratchDir
	return cfg
}

// delegateOn builds a parent on cfg and spawns one delegate from it, returning both.
func delegateOn(t *testing.T, cfg domain.Config) (parent, child *Agent) {
	t.Helper()
	parent = newProfileAgent(t, cfg, &recordingResponder{reply: "All done."})
	child, err := parent.newChildAgent("call_sub", "the delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	return parent, child
}

// TestDelegateReport_RidesBetweenTheOrientationAndTheContextBlocks is the item's core acceptance:
// a delegate's standing content carries the block exactly once, after the engine's orientation
// block and ahead of the workspace's own — the ratified position, and the same F-19 reasoning the
// orientation's has: every engine-owned part precedes the repo-controlled text.
func TestDelegateReport_RidesBetweenTheOrientationAndTheContextBlocks(t *testing.T) {
	t.Parallel()

	_, child := delegateOn(t, delegateReportConfig(t))

	got := child.standingSystem()

	if n := strings.Count(got, DelegateReportBlock); n != 1 {
		t.Fatalf("a delegate's standing content carries the block %d times, want exactly 1:\n%q", n, got)
	}
	blockAt := strings.Index(got, DelegateReportBlock)
	if at := strings.Index(got, orientationHeaderText); at < 0 || at > blockAt {
		t.Errorf("the block does not follow the orientation block:\n%q", got)
	}
	if at := strings.Index(got, contextFileHeader+"AGENTS.md"); at < 0 || at < blockAt {
		t.Errorf("the block does not precede the workspace context blocks:\n%q", got)
	}
	// The whole composition, joined the way standingSystem joins every other part: the parts
	// either side are pinned by their own tests, so what this adds is that the block slots between
	// them by one blank line and disturbs no byte of either.
	want := child.systemPrompt() + "\n\n" + child.orientationBlock() + "\n\n" +
		DelegateReportBlock + "\n\n" + child.contextBlocks()
	if got != want {
		t.Errorf("a delegate's standing content =\n%q\nwant\n%q", got, want)
	}
}

// TestDelegateReport_TopLevelSessionIsUntouched: the block is child-only, so the standing content
// of the session the human talks to is byte-identical to what it was before the block existed.
func TestDelegateReport_TopLevelSessionIsUntouched(t *testing.T) {
	t.Parallel()

	parent, _ := delegateOn(t, delegateReportConfig(t))

	got := parent.standingSystem()

	if strings.Contains(got, delegateReportFence) {
		t.Errorf("a top-level session's standing content carries the delegate block:\n%q", got)
	}
	want := parent.systemPrompt() + "\n\n" + parent.orientationBlock() + "\n\n" + parent.contextBlocks()
	if got != want {
		t.Errorf("a top-level session's standing content =\n%q\nwant the prompt, the orientation and the blocks alone\n%q", got, want)
	}
}

// TestDelegateReport_RidesAlongAndNeverSeedsAlone pins the ride-along rule (ADR 0023 §6) on the
// delegate side: the block is composed in only when a configured source already seeded the
// message, so a child of a session with no prompt template and no context files still seeds
// nothing at all — the anchor `use-default-prompt: false` buys, and with it the Bypass floor.
// Since ADR 0064 ships the default template embedded, that key is the only posture that reaches
// this state, which makes the anchor more load-bearing rather than less.
func TestDelegateReport_RidesAlongAndNeverSeedsAlone(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(&recordingSink{})
	cfg.WorkspaceDir = orientationWorkspaceDir
	cfg.ScratchDir = orientationScratchDir

	responder := &recordingResponder{reply: "All done."}
	parent := newProfileAgent(t, cfg, responder)
	child, err := parent.newChildAgent("call_sub", "the delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}

	if got := child.standingSystem(); got != "" {
		t.Errorf("standingSystem() = %q, want \"\" — the block never seeds a message of its own", got)
	}

	// And on the wire, which is where the promptless run is actually promised.
	if err := child.Submit(domain.UserInput{Text: "the delegated task"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := child.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if n := countSystemMessages(responder.last.Messages); n != 0 {
		t.Errorf("a delegate's wire request has %d system messages, want none: %+v", n, responder.last.Messages)
	}
}

// TestDelegateReport_EveryDepthAboveZeroCarriesIt: depth is the whole gate, so a grandchild —
// whose reply is consumed by another agent exactly as a child's is — carries the same block.
func TestDelegateReport_EveryDepthAboveZeroCarriesIt(t *testing.T) {
	t.Parallel()

	_, child := delegateOn(t, delegateReportConfig(t))
	grandchild, err := child.newChildAgent("call_sub_sub", "the nested task", "")
	if err != nil {
		t.Fatalf("newChildAgent (depth 2): %v", err)
	}

	if grandchild.depth != 2 {
		t.Fatalf("grandchild depth = %d, want 2", grandchild.depth)
	}
	if n := strings.Count(grandchild.standingSystem(), DelegateReportBlock); n != 1 {
		t.Errorf("a depth-2 delegate carries the block %d times, want exactly 1:\n%q",
			n, grandchild.standingSystem())
	}
}

// TestDelegateReport_RoutedAndUnroutedChildrenAlike: a delegate is a delegate whichever box it
// runs on, so latching a Sub-agent server changes nothing about the block or its position — the
// routing decision is about WHERE the work runs, never about what the child is told its reply is
// for (ADR 0045/0069).
func TestDelegateReport_RoutedAndUnroutedChildrenAlike(t *testing.T) {
	t.Parallel()

	cfg := delegateReportConfig(t)
	parent := newProfileAgent(t, cfg, &recordingResponder{reply: "All done."})

	unrouted, err := parent.newChildAgent("call_sub", "the delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgent (unrouted): %v", err)
	}
	parent.SetDelegationTarget(routedTarget())
	routed, err := parent.newChildAgent("call_sub_routed", "the delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgent (routed): %v", err)
	}

	if routed.cfg.Model == unrouted.cfg.Model {
		t.Fatalf("the routed child was not built from the target: both name %q", routed.cfg.Model)
	}
	for name, child := range map[string]*Agent{"unrouted": unrouted, "routed": routed} {
		got := child.standingSystem()
		if n := strings.Count(got, DelegateReportBlock); n != 1 {
			t.Errorf("the %s child carries the block %d times, want exactly 1:\n%q", name, n, got)
		}
		if at, files := strings.Index(got, DelegateReportBlock), strings.Index(got, contextFileHeader+"AGENTS.md"); at < 0 || files < at {
			t.Errorf("the %s child's block is not ahead of the workspace blocks:\n%q", name, got)
		}
	}
}

// TestDelegateReport_DoesNotContradictTheWrapUpDirective: a delegate stopped at its step cap
// carries BOTH texts on one request, so they must tell one story. The standing block speaks about
// the FINAL reply without naming which Turn is last — true on the first Turn and true again on the
// wrap-up one — and the directive is the only text that says the menu is gone and this reply is
// the last. Neither claims the other's ground.
func TestDelegateReport_DoesNotContradictTheWrapUpDirective(t *testing.T) {
	t.Parallel()

	cfg := delegateReportConfig(t)
	responder := &recordingResponder{reply: "All done."}
	parent := newProfileAgent(t, cfg, responder)
	child, err := parent.newChildAgent("call_sub", "the delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	child.stepCap = 3
	child.wrapUp = true // the one tool-less closing Turn (Agent.finishAtStepCap)

	got := seedSystemMessage(t, child, responder, "the delegated task")

	if !strings.Contains(got, DelegateReportBlock) {
		t.Errorf("the wrap-up request dropped the standing block:\n%q", got)
	}
	if directive := fmt.Sprintf(wrapUpDirectiveFormat, child.stepCap); !strings.Contains(got, directive) {
		t.Errorf("the wrap-up request is missing the directive %q:\n%q", directive, got)
	}
	if strings.Contains(DelegateReportBlock, wrapUpMarker) {
		t.Errorf("the standing block claims the tools are withdrawn; only the wrap-up directive may say that:\n%q",
			DelegateReportBlock)
	}
	if strings.Contains(DelegateReportBlock, "only remaining reply") {
		t.Errorf("the standing block claims this reply is the last; it rides on EVERY child request:\n%q",
			DelegateReportBlock)
	}
}
