package agent

// The engine-owned orientation block (orientation.go): what it states, what it omits, and the
// RIDE-ALONG rule that keeps the "send no system prompt" configuration byte-identical. The
// assertions pin the block's structure — its header and each bullet's label with the exact path
// it names — rather than the whole prose sentence, so the wording may be tightened while the
// facts and their shape stay pinned.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

const (
	orientationWorkspaceDir = "/tmp/apogee-orientation-ws"
	orientationScratchDir   = "/tmp/apogee-orientation-scratch/sess-1"
	orientationFirstRoot    = "/tmp/apogee-orientation-skills"
	orientationSecondRoot   = "/tmp/apogee-orientation-library"
)

// orientationHeaderText is the block's opening line, spelled out here so the tests pin the wire
// shape itself rather than borrowing it from the asset they are checking.
const orientationHeaderText = "Host orientation (harness facts, independent of the prompt above):"

// workspaceBullet, scratchBullet and rootsBullet render the leading, path-bearing half of each
// bullet — the label plus the exact path or paths, up to the em dash the guidance follows.
func workspaceBullet(path string) string { return "- Workspace: " + path + " —" }
func scratchBullet(path string) string   { return "- Scratch dir: " + path + " —" }
func rootsBullet(roots ...string) string {
	return "- Read-only library roots: " + strings.Join(roots, ", ") + " —"
}

// orientationConfig returns a config carrying a prompt template and a workspace — the minimum
// that seeds a standing system message for the block to ride on.
func orientationConfig(t *testing.T) domain.Config {
	t.Helper()
	cfg := baseConfig(&recordingSink{})
	cfg.WorkspaceDir = orientationWorkspaceDir
	cfg.SystemPrompt = "You are apogee working in {{workspace}}."
	return cfg
}

// TestOrientation_RidesDirectlyAfterThePrompt: with a template, a scratch dir, two read roots and
// a workspace context file, the seeded content opens with the rendered prompt, the block follows
// it immediately, and the first context-file header comes only AFTER the block — no workspace
// text ever precedes the engine's own facts. Every bullet names its exact path.
func TestOrientation_RidesDirectlyAfterThePrompt(t *testing.T) {
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "AGENTS.md", "Run make check before committing.")
	cfg := contextSeamConfig(t, &recordingSink{}, dir, "AGENTS.md")
	cfg.SystemPrompt = "You are apogee working in {{workspace}}."
	cfg.ScratchDir = orientationScratchDir
	cfg.ExtraReadRoots = func() []string { return []string{orientationFirstRoot, orientationSecondRoot} }

	a := newProfileAgent(t, cfg, &recordingResponder{reply: "All done."})

	got := a.standingSystem()

	block := a.orientationBlock()
	if !strings.HasPrefix(got, "You are apogee working in "+dir+".\n\n"+block+"\n\n") {
		t.Fatalf("standing content does not read prompt → orientation block → the rest:\n%q", got)
	}
	if headerAt := strings.Index(got, contextFileHeader+"AGENTS.md"); headerAt <= strings.Index(got, orientationHeaderText) {
		t.Errorf("a context-file header precedes the orientation block:\n%q", got)
	}
	if !strings.HasPrefix(block, orientationHeaderText+"\n") {
		t.Errorf("block does not open with the header: %q", block)
	}
	for _, want := range []string{
		workspaceBullet(dir),
		scratchBullet(orientationScratchDir),
		rootsBullet(orientationFirstRoot, orientationSecondRoot),
	} {
		if !strings.Contains(block, want) {
			t.Errorf("block is missing %q:\n%q", want, block)
		}
	}
}

// TestOrientation_ReachesTheWire: the block is not a rendering curiosity — it travels in the
// position-0 system message the provider actually receives.
func TestOrientation_ReachesTheWire(t *testing.T) {
	cfg := orientationConfig(t)
	cfg.ScratchDir = orientationScratchDir

	responder := &recordingResponder{reply: "All done."}
	a := newProfileAgent(t, cfg, responder)

	got := seedSystemMessage(t, a, responder, "hi")

	if !strings.Contains(got, scratchBullet(orientationScratchDir)) {
		t.Errorf("the wire's system message is missing the scratch bullet:\n%q", got)
	}
}

// TestOrientation_OmitsFactsTheSessionDoesNotHave: no scratch dir and no read roots leaves the
// workspace bullet alone — an absent fact is omitted, never rendered as an empty path.
func TestOrientation_OmitsFactsTheSessionDoesNotHave(t *testing.T) {
	cfg := orientationConfig(t) // no ScratchDir, nil ExtraReadRoots

	a := newProfileAgent(t, cfg, &recordingResponder{reply: "All done."})

	block := a.orientationBlock()

	if !strings.Contains(block, workspaceBullet(orientationWorkspaceDir)) {
		t.Errorf("block is missing the workspace bullet: %q", block)
	}
	for _, unwanted := range []string{"Scratch dir:", "Read-only library roots:"} {
		if strings.Contains(block, unwanted) {
			t.Errorf("block states %q with nothing to name it: %q", unwanted, block)
		}
	}
	if lines := strings.Count(block, "\n") + 1; lines != 2 {
		t.Errorf("block has %d lines, want the header plus one bullet: %q", lines, block)
	}
}

// TestOrientation_EmptyReadRootsOmitTheLine: a live ExtraReadRoots func that currently mounts
// nothing is the same as no func at all — the nil guard is not the only one that matters.
func TestOrientation_EmptyReadRootsOmitTheLine(t *testing.T) {
	cfg := orientationConfig(t)
	cfg.ExtraReadRoots = func() []string { return nil }

	a := newProfileAgent(t, cfg, &recordingResponder{reply: "All done."})

	if block := a.orientationBlock(); strings.Contains(block, "Read-only library roots:") {
		t.Errorf("block states read roots the host mounts none of: %q", block)
	}
}

// TestOrientation_NoTemplateAndNoContextFilesSeedsNothing pins the native anchor: the block
// rides along, it never seeds a system message of its own, so the documented "send no system
// prompt" configuration stays byte-identical even with a workspace and a scratch dir wired.
//
// What reaches this state changed above the engine and not in it. Since ADR 0064 a config that
// states no prompt resolves the EMBEDDED default, so an empty Config.SystemPrompt is now what
// `use-default-prompt: false` resolves to rather than what an unconfigured install falls into —
// which makes the anchor MORE load-bearing, not less: it is the only thing that key now buys, and
// the emptiness has to reach the wire, not just standingSystem().
func TestOrientation_NoTemplateAndNoContextFilesSeedsNothing(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.WorkspaceDir = orientationWorkspaceDir
	cfg.ScratchDir = orientationScratchDir

	responder := &recordingResponder{reply: "All done."}
	a := newProfileAgent(t, cfg, responder)

	if got := a.standingSystem(); got != "" {
		t.Errorf("standingSystem() = %q, want \"\" (nothing configured seeds nothing)", got)
	}

	// And on the wire, which is where the promptless run is actually promised: the request opens
	// with the user's own message and carries no system message at all.
	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if n := countSystemMessages(responder.last.Messages); n != 0 {
		t.Errorf("wire request has %d system messages, want none: %+v", n, responder.last.Messages)
	}
}

// TestOrientation_RidesOnContextFilesAlone: the context files are an independent standing
// source, so the block joins a message they seeded with no template configured at all — and with
// no prompt to follow it leads the content outright, ahead of the file's own block.
func TestOrientation_RidesOnContextFilesAlone(t *testing.T) {
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "AGENTS.md", "Run make check before committing.\n")
	cfg := contextSeamConfig(t, &recordingSink{}, dir, "AGENTS.md") // no SystemPrompt
	cfg.ScratchDir = orientationScratchDir

	responder := &recordingResponder{reply: "All done."}
	a := newProfileAgent(t, cfg, responder)

	got := seedSystemMessage(t, a, responder, "hi")

	if !strings.HasPrefix(got, a.orientationBlock()+"\n\n") {
		t.Fatalf("the orientation block does not lead the standing content:\n%q", got)
	}
	if !strings.HasSuffix(got, contextBlock("AGENTS.md", "Run make check before committing.")) {
		t.Errorf("the context block no longer closes the standing content:\n%q", got)
	}
	if !strings.Contains(got, workspaceBullet(dir)) || !strings.Contains(got, scratchBullet(orientationScratchDir)) {
		t.Errorf("the orientation block did not ride along on the context files:\n%q", got)
	}
}

// TestOrientation_NamesTheContextFilesOnlyWhenTheyExist: the fifth bullet speaks about what
// FOLLOWS the block, so it is rendered exactly when the session holds a readable context file —
// and a session with none never claims blocks the model cannot see.
func TestOrientation_NamesTheContextFilesOnlyWhenTheyExist(t *testing.T) {
	const bullet = `- Workspace context files follow under "## Workspace context: <name>" headers:`

	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "AGENTS.md", "Run make check before committing.")
	withFiles := contextSeamConfig(t, &recordingSink{}, dir, "AGENTS.md")
	withFiles.SystemPrompt = "You are apogee working in {{workspace}}."

	loaded := newProfileAgent(t, withFiles, &recordingResponder{reply: "All done."})
	none := newProfileAgent(t, orientationConfig(t), &recordingResponder{reply: "All done."})

	if block := loaded.orientationBlock(); !strings.Contains(block, bullet) {
		t.Errorf("a session holding AGENTS.md does not name the context files: %q", block)
	}
	if block := none.orientationBlock(); strings.Contains(block, bullet) {
		t.Errorf("a session holding no context file still names blocks: %q", block)
	}
}

// TestOrientation_SubAgentInheritsTheBlock: a child renders its own standing content from the
// config it inherits, so the sub-agent is oriented by the same facts with no wiring of its own —
// with the Delegations bullet as the ONE exception, because the seat is a depth-0 offer (ADR 0069
// decision 3) and the child's own tool no longer publishes `run_on`. The parent states the choice;
// the child is never told about a choice it does not have.
func TestOrientation_SubAgentInheritsTheBlock(t *testing.T) {
	cfg := seatOrientationConfig(t)
	cfg.ScratchDir = orientationScratchDir

	parent := newProfileAgent(t, cfg, &recordingResponder{reply: "All done."})
	parent.SetDelegationSeat(fullSeat())
	child, err := parent.newChildAgent("call_sub", "a delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}

	block := child.orientationBlock()

	if !strings.Contains(block, scratchBullet(orientationScratchDir)) {
		t.Errorf("the child's block is missing the parent's scratch bullet: %q", block)
	}
	if !strings.Contains(block, workspaceBullet(orientationWorkspaceDir)) {
		t.Errorf("the child's block is missing the workspace bullet: %q", block)
	}
	if !strings.Contains(parent.orientationBlock(), delegationsLabel) {
		t.Fatalf("the parent's own block states no Delegations bullet: %q", parent.orientationBlock())
	}
	if strings.Contains(block, delegationsLabel) {
		t.Errorf("the child's block offers a seat choice its tool does not publish: %q", block)
	}
}

// TestOrientation_FollowsAScratchDirMove: the inputs are read fresh per request, so the session
// boundary that mints a new scratch dir is named on the very next one.
func TestOrientation_FollowsAScratchDirMove(t *testing.T) {
	const moved = "/tmp/apogee-orientation-scratch/sess-2"
	cfg := orientationConfig(t)
	cfg.ScratchDir = orientationScratchDir

	responder := &recordingResponder{reply: "All done."}
	a := newProfileAgent(t, cfg, responder)

	first := seedSystemMessage(t, a, responder, "hi")
	a.SetScratchDir(moved)
	second := seedSystemMessage(t, a, responder, "again")

	if !strings.Contains(first, scratchBullet(orientationScratchDir)) {
		t.Errorf("the first request's block is missing the seeded scratch dir:\n%q", first)
	}
	if !strings.Contains(second, scratchBullet(moved)) {
		t.Errorf("the second request's block did not follow the scratch dir move:\n%q", second)
	}
	if strings.Contains(second, scratchBullet(orientationScratchDir)) {
		t.Errorf("the second request's block still names the old scratch dir:\n%q", second)
	}
}

// withOrientation returns the standing system content a seam test expects: the rendered prompt
// THAT test configures, then the engine's own orientation block, then — when the agent under test
// is a delegate — the delegate report block, then the agent's task list when it holds one, then
// the context-file blocks the test configures. That is the wire order standingSystem composes,
// with either configured part passed as "" when the test does not have one. Every engine-owned
// block is pinned by tests of its own (here, in delegatereport_test.go and in
// tasklistblock_test.go); taken from the agent under test here, so a seam test keeps asserting
// the exact bytes of the parts it owns without restating text it does not.
func withOrientation(a *Agent, rendered, blocks string) string {
	parts := make([]string, 0, 5)
	if rendered != "" {
		parts = append(parts, rendered)
	}
	if block := a.orientationBlock(); block != "" {
		parts = append(parts, block)
	}
	if block := a.delegateReportBlock(); block != "" {
		parts = append(parts, block)
	}
	if block := a.taskListBlock(); block != "" {
		parts = append(parts, block)
	}
	if blocks != "" {
		parts = append(parts, blocks)
	}
	return strings.Join(parts, "\n\n")
}

// ----------------------------------------------------------------------------
// The Delegations bullet (ADR 0069 — the model picks the seat)
// ----------------------------------------------------------------------------
//
// The bullet exists to make a `run_on` choice an informed one, so what these pin is that it
// describes the two seats and nothing else: the roster is its only gate, a seat with nothing to say
// is dropped rather than rendered empty, and — the load-bearing one — the rendered block is a
// per-session constant (ADR 0023 §6), so the heartbeat that finds the far server down and the beat
// that finds it back move nothing here.

const (
	orientationServerName = "apollo"
	orientationServerDesc = "the orchestrator box, 70B and careful"
	orientationSeatName   = "grunt"
	orientationSeatDesc   = "fast local 27B, search and mechanical edits"
	orientationSeatModel  = "cheap-4b"
)

// delegationsLabel is the bullet's label alone — enough to say whether the line is rendered at all
// without restating a word of its guidance.
const delegationsLabel = "- Delegations: "

// delegationsBullet renders the leading, seat-bearing half of the bullet: the label plus the clause
// list, up to the full stop the fixed guidance follows.
func delegationsBullet(clauses string) string { return delegationsLabel + clauses + "." }

// seatOrientationConfig is orientationConfig with the two halves the bullet needs: a sub_agent tool
// that PUBLISHES `run_on` (the bullet's only gate) and the human's words for the session's own
// server.
func seatOrientationConfig(t *testing.T) domain.Config {
	t.Helper()
	cfg := orientationConfig(t)
	cfg.Tools = seatChoiceRegistry(t)
	cfg.ServerName = orientationServerName
	cfg.ServerDescription = orientationServerDesc
	return cfg
}

// seatOrientationAgent is the agent those tests render: seat choice on the menu, a described
// session seat, and no Sub-agent server installed until a test installs one.
func seatOrientationAgent(t *testing.T) *Agent {
	t.Helper()
	return newProfileAgent(t, seatOrientationConfig(t), &recordingResponder{reply: "All done."})
}

// fullSeat is a Sub-agent server described in all three parts — the shape a host builds from an
// entry that pins a model and carries a description.
func fullSeat() *DelegationSeat {
	return &DelegationSeat{
		Name:        orientationSeatName,
		Description: orientationSeatDesc,
		Model:       orientationSeatModel,
	}
}

// TestOrientation_PlainToolStatesNoDelegationsBullet is the regression guard, pinned as the WHOLE
// rendered block: a session whose sub_agent publishes no `run_on` — every session under
// `sub-agents-choice: fixed`, which is the default — renders exactly the bullets it rendered before
// seat choice existed, in the order it rendered them.
//
// The expectation is composed from the asset rather than spelled out, which is this file's standing
// convention (the prose may be tightened; the SHAPE may not): what it pins is that no bullet was
// added, dropped or reordered, and that the Delegations line stays behind its gate.
func TestOrientation_PlainToolStatesNoDelegationsBullet(t *testing.T) {
	cfg := orientationConfig(t) // no tool registry at all: nothing published `run_on`
	cfg.ScratchDir = orientationScratchDir
	cfg.ExtraReadRoots = func() []string { return []string{orientationFirstRoot} }

	a := newProfileAgent(t, cfg, &recordingResponder{reply: "All done."})

	want := strings.Join([]string{
		orientationTemplate[orientationHeaderLine],
		fmt.Sprintf(orientationTemplate[orientationWorkspaceLine], orientationWorkspaceDir),
		fmt.Sprintf(orientationTemplate[orientationScratchLine], orientationScratchDir),
		fmt.Sprintf(orientationTemplate[orientationRootsLine], orientationFirstRoot),
	}, "\n")

	if got := a.orientationBlock(); got != want {
		t.Errorf("the plain tool's block is no longer byte-identical:\ngot  %q\nwant %q", got, want)
	}
}

// TestOrientation_NamesBothDelegationSeats is the bullet doing its job: each seat rendered as
// "<model> on <entry name> — <description>", the same shape for both so a model is comparing like
// with like, and an absent `run_on` named as the seat it actually equals.
func TestOrientation_NamesBothDelegationSeats(t *testing.T) {
	a := seatOrientationAgent(t)
	a.SetDelegationSeat(fullSeat())

	want := delegationsBullet(
		`run_on "session" = test-model on ` + orientationServerName + " — " + orientationServerDesc +
			`; run_on "sub-agents-server" = ` + orientationSeatModel + " on " + orientationSeatName +
			" — " + orientationSeatDesc +
			"; unset = sub-agents-server")

	if block := a.orientationBlock(); !strings.Contains(block, want) {
		t.Errorf("block is missing the two-seat bullet %q:\n%q", want, block)
	}
}

// TestOrientation_SeatClausesDropWhatTheHostDidNotSupply: every part of a seat is optional and
// independently so, and a part the host never named is omitted rather than rendered as an empty
// word — the block's rule for every other bullet, read for the two seats.
func TestOrientation_SeatClausesDropWhatTheHostDidNotSupply(t *testing.T) {
	for _, tc := range []struct {
		name string
		seat *DelegationSeat
		want string
	}{
		{
			name: "no model pin names the entry alone",
			seat: &DelegationSeat{Name: orientationSeatName, Description: orientationSeatDesc},
			want: orientationSeatName + " — " + orientationSeatDesc,
		},
		{
			name: "no description names the model on the entry",
			seat: &DelegationSeat{Name: orientationSeatName, Model: orientationSeatModel},
			want: orientationSeatModel + " on " + orientationSeatName,
		},
		{
			name: "an entry name on its own is still a seat",
			seat: &DelegationSeat{Name: orientationSeatName},
			want: orientationSeatName,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := seatOrientationAgent(t)
			a.SetDelegationSeat(tc.seat)

			want := `run_on "sub-agents-server" = ` + tc.want + "; unset = sub-agents-server."
			if block := a.orientationBlock(); !strings.Contains(block, want) {
				t.Errorf("block is missing %q:\n%q", want, block)
			}
		})
	}
}

// TestOrientation_NoSeatInstalledDropsTheSubAgentsClause: with no Sub-agent server installed there
// is no far seat to describe, so the bullet names the near one and says an unset `run_on` stays
// there. Nothing is invented to fill the gap, and no availability is implied by its absence.
func TestOrientation_NoSeatInstalledDropsTheSubAgentsClause(t *testing.T) {
	a := seatOrientationAgent(t)
	a.SetDelegationSeat(fullSeat())
	a.SetDelegationSeat(nil)

	block := a.orientationBlock()

	want := delegationsBullet(
		`run_on "session" = test-model on ` + orientationServerName + " — " + orientationServerDesc +
			"; unset = session")
	if !strings.Contains(block, want) {
		t.Errorf("block is missing the session-only bullet %q:\n%q", want, block)
	}
	if strings.Contains(block, `run_on "sub-agents-server" =`) {
		t.Errorf("block describes a Sub-agent server the host installed none of:\n%q", block)
	}
}

// TestOrientation_NoSeatFactsAtAllOmitTheBullet: a Driver that names no server and installs no seat
// has nothing to say about either place, so the bullet is omitted whole rather than rendered as a
// label with an "unset" clause and no seats.
func TestOrientation_NoSeatFactsAtAllOmitTheBullet(t *testing.T) {
	cfg := orientationConfig(t)
	cfg.Tools = seatChoiceRegistry(t)
	cfg.Model = "" // no bound model, no server name, no description: nothing describes the near seat

	a := newProfileAgent(t, cfg, &recordingResponder{reply: "All done."})

	if block := a.orientationBlock(); strings.Contains(block, delegationsLabel) {
		t.Errorf("block states a Delegations bullet with no seat to name:\n%q", block)
	}
}

// TestOrientation_DelegationsBulletFollowsAServerSwitch: the session seat is read from the LIVE
// binding, so the human door that moves the session to another box moves what the model is told
// about it on the very next request — a switch never leaves the line describing the retired server.
func TestOrientation_DelegationsBulletFollowsAServerSwitch(t *testing.T) {
	const (
		movedName = "hermes"
		movedDesc = "the spare box"
	)

	a := seatOrientationAgent(t)

	if err := a.SwitchUpstream(UpstreamSpec{
		Endpoint:          "http://hermes.local:1111",
		ServerName:        movedName,
		ServerDescription: movedDesc,
	}); err != nil {
		t.Fatalf("SwitchUpstream: %v", err)
	}

	block := a.orientationBlock()

	// A switch UNBINDS the model (ADR 0024), so the seat is the new server's name alone until the
	// first Rebind — which is exactly what the model should read while nothing is bound.
	if want := `run_on "session" = ` + movedName + " — " + movedDesc; !strings.Contains(block, want) {
		t.Errorf("block is missing the switched seat %q:\n%q", want, block)
	}
	if strings.Contains(block, orientationServerName) {
		t.Errorf("block still names the retired server:\n%q", block)
	}
}

// TestOrientation_DelegationsBulletIsConstantAcrossABeat is ADR 0023 §6, kept: the Sub-agent
// server's Delegation TARGET is re-stated by the host's heartbeat and goes nil the moment the box
// stops answering, and none of that may reach the standing system message — a prompt that churned
// per beat would cost the prefix cache the very stability the rule promises. An unusable target is
// the delegation result's note to tell, not the prompt's.
func TestOrientation_DelegationsBulletIsConstantAcrossABeat(t *testing.T) {
	a := seatOrientationAgent(t)
	a.SetDelegationSeat(fullSeat())

	landed := a.orientationBlock()
	a.SetDelegationTarget(nil) // the beat that finds the far server down
	down := a.orientationBlock()
	a.SetDelegationTarget(routedTarget()) // and the one that finds it back
	up := a.orientationBlock()

	if down != landed {
		t.Errorf("a target-down beat moved the block:\ngot  %q\nwant %q", down, landed)
	}
	if up != landed {
		t.Errorf("a target-up beat moved the block:\ngot  %q\nwant %q", up, landed)
	}
	if !strings.Contains(landed, delegationsLabel) {
		t.Fatalf("the block under test states no Delegations bullet at all: %q", landed)
	}
}

// TestOrientation_DelegationsBulletReachesTheWire: the bullet is not a rendering curiosity either —
// it travels in the position-0 system message the provider actually receives, which is the only
// place it can do the model any good.
func TestOrientation_DelegationsBulletReachesTheWire(t *testing.T) {
	responder := &recordingResponder{reply: "All done."}
	a := newProfileAgent(t, seatOrientationConfig(t), responder)
	a.SetDelegationSeat(fullSeat())

	got := seedSystemMessage(t, a, responder, "hi")

	if want := `run_on "sub-agents-server" = ` + orientationSeatModel + " on " + orientationSeatName; !strings.Contains(got, want) {
		t.Errorf("the wire's system message is missing the far seat %q:\n%q", want, got)
	}
}
