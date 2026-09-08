package agent

// Sub-agent spawn under the PRODUCTION Config.Reactions arm (ADR 0076 stage 1, recast off the
// retired enable-list arm). The rest of the package's coverage splits the two concerns:
// enable_mechanisms_test.go arms Reactions but never delegates, and the delegation suites delegate
// but arm nothing. Neither exercises the seam the sub-agent contract names — a spawned sub-agent
// inherits the parent's armed Reactions (subagent.go: childCfg.Reactions =
// inheritedReactions(a.cfg.Reactions)), every one of them unless it opted out with TopLevelOnly.
// These tests arm one Reaction on the parent, drive one real delegation, and prove the child ran
// the inherited set — through New and through Resume, the one construction path — and that the
// TopLevelOnly opt-out is the single thing that keeps a Reaction at Depth 0.

import (
	"context"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// The arm under test is ONE Reaction that acts on every request, so a fire booked at Depth 1
// proves the child ran the parent's set. It is a synthetic double rather than an engine builtin
// because every builtin is on for every agent at every Depth, and so proves nothing about
// inheritance.

// inheritedReactionID names the synthetic Reaction the parent arms and the child must inherit;
// topLevelOnlyReactionID names its opted-out sibling, which must stay at Depth 0.
const (
	inheritedReactionID    = "inherited_probe"
	topLevelOnlyReactionID = "top_level_only_probe"
)

// gdWindow is the discovered context window these delegation tests run under: at 4 chars/token
// (uncalibrated) it allocates ~400 tokens to FileContext and ~960 to History, so a modest ask leaves
// the budget honest without any allocation being close to full.
const gdWindow = 2000

// reactionSubAgentConfig arms the inherited Reaction, wires the sub_agent recursion point plus a
// write_file tool the child can call, and sets the discovered window the delegation is budgeted
// against.
func reactionSubAgentConfig(t *testing.T, sink domain.EventSink) domain.Config {
	t.Helper()
	cfg := subAgentConfig(sink, domain.ModeAskBefore,
		fakeTool{name: "write_file", result: "ok"})
	fired := 0
	cfg.Reactions = []domain.Reaction{recordingReaction(inheritedReactionID, domain.ClassShapeView, &fired)}
	cfg.Context.MaxContextTokens = gdWindow
	return cfg
}

// reactionSubAgentScripts is the run-ordered script the shared responder replays across the parent
// AND its one child: the parent delegates unprompted on a modest opening ask, and the child writes
// a Go file before the child and then the parent each answer. Every request the child makes runs
// the inherited set, so the armed Reaction acts — and books a fire — at Depth 1.
func reactionSubAgentScripts() [][]provider.Delta {
	return [][]provider.Delta{
		subAgentCallScript("s1", "investigate the auth module and report the entry points"), // parent T0: unprompted delegation
		toolCallScript("w0", "write_file", `{"path":"auth.go","content":"package auth\n"}`), // child T0: a write
		contentScript("child: entry points catalogued"),                                     // child T1: final report
		contentScript("parent: synthesized the delegated investigation"),                    // parent T1: final answer
	}
}

// TestReactions_SubAgentSpawnInheritsArmedSet: a parent armed via Config.Reactions delegates once;
// the spawn succeeds, the child nests at Depth 1, and the child fires the Reaction it inherited.
func TestReactions_SubAgentSpawnInheritsArmedSet(t *testing.T) {
	sink := &recordingSink{}
	responder := &captureAllResponder{scripts: reactionSubAgentScripts()}

	a, err := newAgent(reactionSubAgentConfig(t, sink), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "Please look into the login module for me."}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	assertSubAgentInheritedSet(t, res, sink)
}

// TestReactions_SubAgentSpawnInheritsArmedSetOnResume mirrors the arm through Resume: New and
// Resume are one construction path (Reactions are Config, not session state), so a resumed parent
// re-arms the same set and a spawned child inherits it identically. A fresh armed Agent seeds a
// snapshot; Resume re-arms from Config and drives the same delegation.
func TestReactions_SubAgentSpawnInheritsArmedSetOnResume(t *testing.T) {
	seed, err := newAgent(reactionSubAgentConfig(t, &recordingSink{}), echoResponder{reply: "seed"})
	if err != nil {
		t.Fatalf("newAgent (seed): %v", err)
	}
	snap, err := seed.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	sink := &recordingSink{}
	responder := &captureAllResponder{scripts: reactionSubAgentScripts()}
	b, err := resumeAgent(reactionSubAgentConfig(t, sink), snap, responder)
	if err != nil {
		t.Fatalf("resumeAgent: %v", err)
	}
	if err := b.Submit(domain.UserInput{Text: "Please look into the login module for me."}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := b.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	assertSubAgentInheritedSet(t, res, sink)
}

// assertSubAgentInheritedSet checks the three guarantees on a completed delegation: the spawn
// returned no error, the child nested at Depth 1, and the child fired the inherited Reaction.
func assertSubAgentInheritedSet(t *testing.T, res domain.StepResult, sink *recordingSink) {
	t.Helper()

	if res.Status != domain.StatusExchangeComplete {
		t.Fatalf("final status = %q, want the Exchange to complete", res.Status)
	}

	// The spawn succeeded: the sub_agent tool result the parent saw is the child's report, not a
	// construction error. A child that re-armed the parent's Reaction on top of an inherited copy
	// would fail the duplicate-ID gate, surfacing "could not construct sub-agent" here.
	subRes, ok := lastSubAgentResult(sink.events)
	if !ok {
		t.Fatal("no sub_agent tool result — the parent never delegated")
	}
	if subRes.IsError {
		t.Fatalf("sub_agent result is an error (the spawn failed under the Reactions arm): %q", subRes.Content)
	}
	if !strings.Contains(subRes.Content, "entry points catalogued") {
		t.Errorf("sub_agent result = %q, want the child's report back", subRes.Content)
	}

	// The child's events nest at Depth 1 while the parent's stay at Depth 0 (ADR 0013).
	if d := gdMessageEventDepth(sink.events, "child: entry points catalogued"); d != 1 {
		t.Errorf("child report event Depth = %d, want 1 (a real nested sub-agent)", d)
	}
	if d := gdMessageEventDepth(sink.events, "parent: synthesized the delegated investigation"); d != 0 {
		t.Errorf("parent answer event Depth = %d, want 0", d)
	}

	// The child ran the INHERITED set: the armed Reaction acted on the child's own request, booking
	// a fire at Depth 1. A child that inherited nothing books no such fire.
	if !hasFireAtDepth(sink.events, inheritedReactionID, 1) {
		t.Errorf("no %s fire at Depth 1; the child did not run the inherited set. fires=%+v",
			inheritedReactionID, reactionFires(sink.events))
	}
}

// gdMessageEventDepth returns the Depth of the first MessageEvent whose Text equals text, or -1.
func gdMessageEventDepth(events []domain.Event, text string) int {
	for _, e := range events {
		if me, ok := e.(domain.MessageEvent); ok && me.Text == text {
			return me.Depth
		}
	}
	return -1
}

// hasFireAtDepth reports whether a ReactionFiredEvent for id was emitted at the given nesting Depth.
func hasFireAtDepth(events []domain.Event, id string, depth int) bool {
	for _, fe := range reactionFires(events) {
		if fe.Reaction == id && fe.Depth == depth {
			return true
		}
	}
	return false
}

// TestReactions_TopLevelOnlyStaysAtDepthZero is the opt-out half of the same seam, and the one
// capability the Reaction core adds over the membership inheritance it replaces (ADR 0076 stage 1):
// inheritance is the DEFAULT, so a zero-value Reaction reaches the child exactly as before, while a
// Reaction that sets TopLevelOnly is kept at Depth 0 and never runs in a delegated child. Both are
// armed on the SAME parent and both fire at Depth 0, so the only difference the assertion can be
// reading is the flag.
func TestReactions_TopLevelOnlyStaysAtDepthZero(t *testing.T) {
	sink := &recordingSink{}
	responder := &captureAllResponder{scripts: reactionSubAgentScripts()}

	cfg := reactionSubAgentConfig(t, sink)
	topOnlyFired := 0
	topOnly := recordingReaction(topLevelOnlyReactionID, domain.ClassShapeView, &topOnlyFired)
	topOnly.TopLevelOnly = true
	cfg.Reactions = append(cfg.Reactions, topOnly)

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "Please look into the login module for me."}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Both are live at Depth 0 — without this the Depth-1 assertion below could pass on a Reaction
	// that was never armed at all.
	if !hasFireAtDepth(sink.events, inheritedReactionID, 0) {
		t.Errorf("no %s fire at Depth 0; the inherited probe was never armed. fires=%+v",
			inheritedReactionID, reactionFires(sink.events))
	}
	if !hasFireAtDepth(sink.events, topLevelOnlyReactionID, 0) {
		t.Errorf("no %s fire at Depth 0; the opted-out probe was never armed. fires=%+v",
			topLevelOnlyReactionID, reactionFires(sink.events))
	}

	// The child inherited the zero-value one and NOT the opted-out one.
	if !hasFireAtDepth(sink.events, inheritedReactionID, 1) {
		t.Errorf("no %s fire at Depth 1; a zero-value Reaction must be inherited by every child", inheritedReactionID)
	}
	if hasFireAtDepth(sink.events, topLevelOnlyReactionID, 1) {
		t.Errorf("%s fired at Depth 1; TopLevelOnly must keep a Reaction out of every child", topLevelOnlyReactionID)
	}
}
