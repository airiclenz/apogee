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
	cfg.ReadMounts.Roots = func() []string { return []string{orientationFirstRoot, orientationSecondRoot} }

	a := newProfileAgent(t, cfg, echoResponder(t, "All done."))

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

	responder := echoResponder(t, "All done.")
	a := newProfileAgent(t, cfg, responder)

	got := seedSystemMessage(t, a, responder, "hi")

	if !strings.Contains(got, scratchBullet(orientationScratchDir)) {
		t.Errorf("the wire's system message is missing the scratch bullet:\n%q", got)
	}
}

// TestOrientation_OmitsFactsTheSessionDoesNotHave: no scratch dir and no read roots leaves the
// workspace bullet alone — an absent fact is omitted, never rendered as an empty path. The one
// other bullet the block carries is the Delegation bounds line: the workspace seeds the default
// roster, which holds sub_agent, and a session that can delegate always has a width to state.
func TestOrientation_OmitsFactsTheSessionDoesNotHave(t *testing.T) {
	cfg := orientationConfig(t) // no ScratchDir, nil ReadMounts.Roots

	a := newProfileAgent(t, cfg, echoResponder(t, "All done."))

	block := a.orientationBlock()

	if !strings.Contains(block, workspaceBullet(orientationWorkspaceDir)) {
		t.Errorf("block is missing the workspace bullet: %q", block)
	}
	for _, unwanted := range []string{"Scratch dir:", "Read-only library roots:"} {
		if strings.Contains(block, unwanted) {
			t.Errorf("block states %q with nothing to name it: %q", unwanted, block)
		}
	}
	if lines := strings.Count(block, "\n") + 1; lines != 3 {
		t.Errorf("block has %d lines, want the header, the workspace bullet and the bounds bullet: %q", lines, block)
	}
}

// TestOrientation_EmptyReadRootsOmitTheLine: a live ReadMounts.Roots func that currently mounts
// nothing is the same as no func at all — the nil guard is not the only one that matters.
func TestOrientation_EmptyReadRootsOmitTheLine(t *testing.T) {
	cfg := orientationConfig(t)
	cfg.ReadMounts.Roots = func() []string { return nil }

	a := newProfileAgent(t, cfg, echoResponder(t, "All done."))

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

	responder := echoResponder(t, "All done.")
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
	if n := countSystemMessages(responder.last().Messages); n != 0 {
		t.Errorf("wire request has %d system messages, want none: %+v", n, responder.last().Messages)
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

	responder := echoResponder(t, "All done.")
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

	loaded := newProfileAgent(t, withFiles, echoResponder(t, "All done."))
	none := newProfileAgent(t, orientationConfig(t), echoResponder(t, "All done."))

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

	parent := newProfileAgent(t, cfg, echoResponder(t, "All done."))
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

	responder := echoResponder(t, "All done.")
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

// scratchLine renders the WHOLE scratch bullet — label, path and the guidance that follows —
// composed from the asset the way the byte-identical guard below composes its block, so a test
// asserting the line's presence asserts the exact wire line and not only its path-bearing half.
func scratchLine(path string) string {
	return fmt.Sprintf(orientationTemplate[orientationScratchLine], path)
}

// scratchConfinedCachesClause is the qualified tail of the scratch bullet, pinned verbatim: the
// subprocess funnel and console_open seed TMPDIR and the Go build cache beneath the scratch dir on
// a CONFINED run only (subprocess.ScratchEnv), so the clause names that condition rather than
// promising the redirect on every rung. A reworded template fails here, not in a model's session.
const scratchConfinedCachesClause = "/tmp may be denied by workspace confinement; " +
	"under workspace confinement TMPDIR and the Go build cache point here."

// TestOrientation_ScratchLineStatesTheConfinedCaches pins the bullet's guidance tail against the
// exact clause the confined-run seed is announced with — a model that reads the line is told where
// its toolchain's temp and cache files land under confinement, and told it only for that case.
func TestOrientation_ScratchLineStatesTheConfinedCaches(t *testing.T) {
	t.Parallel()

	line := scratchLine(orientationScratchDir)

	if !strings.HasSuffix(line, " "+scratchConfinedCachesClause) {
		t.Errorf("the scratch bullet does not end with the confined-caches clause %q:\n%q",
			scratchConfinedCachesClause, line)
	}
}

// rootsLine renders the WHOLE roots bullet — label, paths and the guidance that follows — composed
// from the asset the way scratchLine is, so the clause pin below asserts the exact wire line.
func rootsLine(roots ...string) string {
	return fmt.Sprintf(orientationTemplate[orientationRootsLine], strings.Join(roots, ", "))
}

// rootsToolchainClause is the tail of the roots bullet, pinned verbatim: the line lists the Go
// toolchain's GOROOT and module cache beside the skill libraries (cmd/apogee's probe), and those
// two are trees every `go build` under `terminal` must read — so the tail names the read tools
// and lets a toolchain command read its own roots, where it used to say "never through terminal
// commands" and would have told the model to keep its builds off the trees a build cannot do
// without. A reworded template fails here, not in a model's session.
const rootsToolchainClause = "read them with read_file, list_dir, grep, find_files or copy_file; " +
	"a toolchain command may still read its own roots."

// TestOrientation_RootsLineLetsAToolchainReadItsOwnRoots pins the roots bullet's guidance tail
// against the exact clause the toolchain mount is announced with.
func TestOrientation_RootsLineLetsAToolchainReadItsOwnRoots(t *testing.T) {
	t.Parallel()

	line := rootsLine(orientationFirstRoot, orientationSecondRoot)

	if !strings.HasSuffix(line, " — "+rootsToolchainClause) {
		t.Errorf("the roots bullet does not end with the toolchain clause %q:\n%q",
			rootsToolchainClause, line)
	}
	if !strings.HasPrefix(line, rootsBullet(orientationFirstRoot, orientationSecondRoot)) {
		t.Errorf("the roots bullet no longer opens with its label and paths:\n%q", line)
	}
}

// TestOrientation_EveryModeStatesTheScratchDir: the session scratch dir is writable on every rung
// of the ladder — Plan runs Apogee's own writers there and nowhere else, Ask-Before runs them there
// unprompted (ADR 0012 second loosen, 2026-09-14) — so every mode, Plan included, renders the
// exact scratch line, and the line is byte-equal across modes: the mode is an input of the block
// for the Mode bullet alone (TestOrientation_PlanStatesWhatItWithholds), never for the scratch
// line. Auto is built over a fake Confiner with fs-write caps, the only way past newAgent's Auto
// gate on a host without confinement.
func TestOrientation_EveryModeStatesTheScratchDir(t *testing.T) {
	want := scratchLine(orientationScratchDir)
	for _, mode := range []domain.Mode{domain.ModePlan, domain.ModeAskBefore, domain.ModeAllowEdits, domain.ModeAuto} {
		t.Run(string(mode), func(t *testing.T) {
			cfg := orientationConfig(t)
			cfg.Mode = mode
			cfg.ScratchDir = orientationScratchDir
			cfg.Confiner = &fakeConfiner{caps: capsBoth()}

			a := newProfileAgent(t, cfg, echoResponder(t, "All done."))

			block := a.orientationBlock()
			lines := strings.Split(block, "\n")
			var got string
			for _, line := range lines {
				if strings.HasPrefix(line, "- Scratch dir:") {
					got = line
				}
			}
			if got != want {
				t.Errorf("%s renders the scratch line %q, want %q:\n%q", mode, got, want, block)
			}
		})
	}
}

// TestOrientation_WritingModesStateTheScratchDir: the three rungs above Plan write there
// unprompted — Ask-Before since the ADR 0012 second loosen (2026-09-14), Allow-Edits and Auto
// since the dir existed — so each renders the exact scratch line, guidance and all. Auto is built
// over a fake Confiner with fs-write caps, the only way past newAgent's Auto gate on a host
// without confinement.
func TestOrientation_WritingModesStateTheScratchDir(t *testing.T) {
	for _, mode := range []domain.Mode{domain.ModeAskBefore, domain.ModeAllowEdits, domain.ModeAuto} {
		t.Run(string(mode), func(t *testing.T) {
			cfg := orientationConfig(t)
			cfg.Mode = mode
			cfg.ScratchDir = orientationScratchDir
			cfg.Confiner = &fakeConfiner{caps: capsBoth()}

			a := newProfileAgent(t, cfg, echoResponder(t, "All done."))

			if block := a.orientationBlock(); !strings.Contains(block, scratchLine(orientationScratchDir)) {
				t.Errorf("%s omits the scratch line %q:\n%q", mode, scratchLine(orientationScratchDir), block)
			}
		})
	}
}

// planModeBullet is the Plan-only Mode bullet exactly as it stands on the wire, spelled out here so
// the tests pin the announcement itself rather than borrowing it from the asset they are checking.
// planModeBulletNoScratch is the same bullet with the writers clause dropped — what a Plan session
// with no scratch dir set is told, because its menu offers no writer at all.
const (
	planModeBullet = "- Mode: plan — reads, plus Apogee's own writers into the session scratch dir only; " +
		"terminal, run_tests, python_exec, web_fetch, web_search, http_request and MCP tools are withheld. " +
		"Report what you would run."
	planModeBulletNoScratch = "- Mode: plan — reads; " +
		"terminal, run_tests, python_exec, web_fetch, web_search, http_request and MCP tools are withheld. " +
		"Report what you would run."
)

// modeLine returns the block's `- Mode:` bullet, or "" when the block renders none.
func modeLine(block string) string {
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, "- Mode:") {
			return line
		}
	}
	return ""
}

// TestOrientation_PlanStatesWhatItWithholds: in Plan the block carries the exact Mode bullet — the
// subprocess and network families the menu withholds, and the scratch-dir writers clause exactly
// when a scratch dir is set, which is when planOffers puts those writers on the menu. The bullet
// rides after the Delegations bullet and ahead of the context-files bullet, which stays last.
func TestOrientation_PlanStatesWhatItWithholds(t *testing.T) {
	t.Run("with a scratch dir", func(t *testing.T) {
		dir := t.TempDir()
		writeWorkspaceFile(t, dir, "AGENTS.md", "Run make check before committing.")
		cfg := contextSeamConfig(t, &recordingSink{}, dir, "AGENTS.md")
		cfg.SystemPrompt = "You are apogee working in {{workspace}}."
		cfg.Mode = domain.ModePlan
		cfg.ScratchDir = orientationScratchDir

		a := newProfileAgent(t, cfg, echoResponder(t, "All done."))

		block := a.orientationBlock()
		if got := modeLine(block); got != planModeBullet {
			t.Errorf("Plan renders the Mode bullet %q, want %q:\n%q", got, planModeBullet, block)
		}
		lines := strings.Split(block, "\n")
		if last := lines[len(lines)-1]; !strings.HasPrefix(last, "- Workspace context files follow") {
			t.Errorf("the context-files bullet no longer closes the block:\n%q", block)
		}
		if !strings.Contains(block, scratchLine(orientationScratchDir)) {
			t.Errorf("Plan lost the scratch line beside its Mode bullet:\n%q", block)
		}
	})
	t.Run("without a scratch dir", func(t *testing.T) {
		cfg := orientationConfig(t) // no ScratchDir
		cfg.Mode = domain.ModePlan

		a := newProfileAgent(t, cfg, echoResponder(t, "All done."))

		block := a.orientationBlock()
		if got := modeLine(block); got != planModeBulletNoScratch {
			t.Errorf("Plan without a scratch dir renders the Mode bullet %q, want %q:\n%q",
				got, planModeBulletNoScratch, block)
		}
	})
}

// TestOrientation_WritingModesStateNoModeBullet: the Mode bullet is Plan's alone — the three rungs
// above it withhold nothing the prompt template promises, so they render no bullet at all rather
// than an empty one. Auto is built over a fake Confiner with fs-write caps, the only way past
// newAgent's Auto gate on a host without confinement.
func TestOrientation_WritingModesStateNoModeBullet(t *testing.T) {
	for _, mode := range []domain.Mode{domain.ModeAskBefore, domain.ModeAllowEdits, domain.ModeAuto} {
		t.Run(string(mode), func(t *testing.T) {
			cfg := orientationConfig(t)
			cfg.Mode = mode
			cfg.ScratchDir = orientationScratchDir
			cfg.Confiner = &fakeConfiner{caps: capsBoth()}

			a := newProfileAgent(t, cfg, echoResponder(t, "All done."))

			if block := a.orientationBlock(); modeLine(block) != "" {
				t.Errorf("%s renders a Mode bullet it has no use for:\n%q", mode, block)
			}
		})
	}
}

// TestOrientation_PlanBulletFollowsAModeFlip: the mode is read fresh per request, so a Shift+Tab
// into Plan announces the withheld families on the very next request and one out of it drops them.
func TestOrientation_PlanBulletFollowsAModeFlip(t *testing.T) {
	cfg := orientationConfig(t)
	cfg.Mode = domain.ModeAskBefore
	cfg.ScratchDir = orientationScratchDir

	responder := echoResponder(t, "All done.")
	a := newProfileAgent(t, cfg, responder)

	before := seedSystemMessage(t, a, responder, "hi")
	a.SetMode(domain.ModePlan)
	during := seedSystemMessage(t, a, responder, "again")
	a.SetMode(domain.ModeAskBefore)
	after := seedSystemMessage(t, a, responder, "once more")

	if strings.Contains(before, planModeBullet) {
		t.Errorf("Ask-Before's request carries the Plan bullet:\n%q", before)
	}
	if !strings.Contains(during, planModeBullet) {
		t.Errorf("the request after the flip into Plan is missing the bullet:\n%q", during)
	}
	if strings.Contains(after, planModeBullet) {
		t.Errorf("the request after the flip out of Plan still carries the bullet:\n%q", after)
	}
}

// TestOrientation_TemplateCarriesThePlanLine pins the asset's shape at the constant: the Plan
// template sits at orientationPlanLine, opens with the Mode label and carries exactly the one %s
// the writers clause fills — so a reordered or reworded asset fails here, not in a model's session.
func TestOrientation_TemplateCarriesThePlanLine(t *testing.T) {
	t.Parallel()

	if n := len(orientationTemplate); n != orientationLineCount {
		t.Fatalf("the template has %d lines, want orientationLineCount = %d", n, orientationLineCount)
	}
	line := orientationTemplate[orientationPlanLine]
	if !strings.HasPrefix(line, "- Mode: plan — reads%s;") {
		t.Errorf("the Plan template does not open with the Mode label and its writers slot: %q", line)
	}
	if got := fmt.Sprintf(line, planScratchWritersClause); got != planModeBullet {
		t.Errorf("the Plan template renders %q, want %q", got, planModeBullet)
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
	return newProfileAgent(t, seatOrientationConfig(t), echoResponder(t, "All done."))
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
// seat choice existed, in the order it rendered them, plus the Delegation bounds line the default
// roster's sub_agent earns it (plan 2026-09-20 - 00, item 4) — that line is gated on the tool, not
// on the seat choice, so the plain tool states it too.
//
// The expectation is composed from the asset rather than spelled out, which is this file's standing
// convention (the prose may be tightened; the SHAPE may not): what it pins is that no bullet was
// added, dropped or reordered, and that the Delegations line stays behind its gate.
func TestOrientation_PlainToolStatesNoDelegationsBullet(t *testing.T) {
	cfg := orientationConfig(t) // no injected registry: the default roster's plain sub_agent, no `run_on`
	cfg.ScratchDir = orientationScratchDir
	cfg.ReadMounts.Roots = func() []string { return []string{orientationFirstRoot} }

	a := newProfileAgent(t, cfg, echoResponder(t, "All done."))

	want := strings.Join([]string{
		orientationTemplate[orientationHeaderLine],
		fmt.Sprintf(orientationTemplate[orientationWorkspaceLine], orientationWorkspaceDir),
		fmt.Sprintf(orientationTemplate[orientationScratchLine], orientationScratchDir),
		fmt.Sprintf(orientationTemplate[orientationRootsLine], orientationFirstRoot),
		// baseConfig states no width, no rounds and no step cap: width 1, no ceiling, no cap.
		fmt.Sprintf(orientationTemplate[orientationDelegationBoundsLine],
			"up to 1 run at once; a reply's whole group returns together"),
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

	a := newProfileAgent(t, cfg, echoResponder(t, "All done."))

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
//
// The Delegation bounds line is held to the same rule with one number the beat COULD move — the
// stated width (plan 2026-09-20 - 00, item 4). It is latched per seat (statedDelegationWidth): a
// usable target states the far width once, and neither the beat that finds the far server down nor
// the session-server beat that re-states its own cap beside it — the production heartbeat runs
// SetParallelAgents unconditionally, every Interval — moves it. So the block is rendered AFTER the
// target has latched, and every beat after that must leave it byte-identical.
func TestOrientation_DelegationsBulletIsConstantAcrossABeat(t *testing.T) {
	a := seatOrientationAgent(t)
	a.SetDelegationSeat(fullSeat())
	a.SetDelegationTarget(routedTarget()) // the first usable beat: the far width is stated once

	landed := a.orientationBlock()
	a.SetDelegationTarget(nil) // the beat that finds the far server down
	down := a.orientationBlock()
	a.SetDelegationTarget(routedTarget()) // and the one that finds it back
	up := a.orientationBlock()
	a.SetParallelAgents(a.parallelAgentsCap()) // the session server's beat, re-stating its own cap
	restated := a.orientationBlock()

	if down != landed {
		t.Errorf("a target-down beat moved the block:\ngot  %q\nwant %q", down, landed)
	}
	if up != landed {
		t.Errorf("a target-up beat moved the block:\ngot  %q\nwant %q", up, landed)
	}
	if restated != landed {
		t.Errorf("a SetParallelAgents beat moved the block:\ngot  %q\nwant %q", restated, landed)
	}
	if !strings.Contains(landed, delegationsLabel) {
		t.Fatalf("the block under test states no Delegations bullet at all: %q", landed)
	}
	if want := delegationBoundsLabel + "up to 3 run at once"; !strings.Contains(landed, want) {
		t.Fatalf("the block under test does not state the latched far width %q: %q", want, landed)
	}
}

// TestOrientation_DelegationsBulletReachesTheWire: the bullet is not a rendering curiosity either —
// it travels in the position-0 system message the provider actually receives, which is the only
// place it can do the model any good.
func TestOrientation_DelegationsBulletReachesTheWire(t *testing.T) {
	responder := echoResponder(t, "All done.")
	a := newProfileAgent(t, seatOrientationConfig(t), responder)
	a.SetDelegationSeat(fullSeat())

	got := seedSystemMessage(t, a, responder, "hi")

	if want := `run_on "sub-agents-server" = ` + orientationSeatModel + " on " + orientationSeatName; !strings.Contains(got, want) {
		t.Errorf("the wire's system message is missing the far seat %q:\n%q", want, got)
	}
}

// ----------------------------------------------------------------------------
// The Delegation bounds bullet (plan 2026-09-20 - 00, item 4 — the model is told the numbers)
// ----------------------------------------------------------------------------
//
// The bullet exists so a reply is composed against the width, the fan-out ceiling and the step cap
// the engine enforces rather than discovering them from a refusal. What these pin: the exact line
// with every clause on, the two clauses that drop when their key is off, and the roster gate — a
// delegate at the depth bound holds no sub_agent and is told nothing about a tool it cannot call.

// delegationBoundsLabel is the bullet's label alone — enough to say whether the line is rendered at
// all without restating a clause.
const delegationBoundsLabel = "- Delegation bounds: "

// TestOrientation_DelegationBoundsStateWidthCeilingAndCap is the bullet doing its job, pinned on
// the WIRE rather than on the render: width 4 under two rounds is a ceiling of 8, the delegate cap
// is 80, and the line the provider receives says exactly that — binding text, tested byte for byte.
func TestOrientation_DelegationBoundsStateWidthCeilingAndCap(t *testing.T) {
	cfg := orientationConfig(t) // the default roster: sub_agent on the menu
	cfg.ParallelAgents = 4
	cfg.Delegation.FanOutRounds = 2
	cfg.Delegation.MaxSteps = 80
	responder := echoResponder(t, "All done.")
	a := newProfileAgent(t, cfg, responder)

	got := seedSystemMessage(t, a, responder, "hi")

	want := "- Delegation bounds: up to 4 run at once; a reply may fan out at most 8 — calls past " +
		"that are refused and must be delegated again; a reply's whole group returns together; " +
		"each delegate is capped at 80 Turns (a max_steps above that is clamped)."
	if !strings.Contains(got, want) {
		t.Errorf("the wire's system message is missing the bounds bullet %q:\n%q", want, got)
	}
}

// TestOrientation_DelegationBoundsOmitTheCeilingAndCapClausesWhenOff: `delegate-fanout-rounds: 0`
// is no ceiling and `delegate-max-steps: 0` is no cap, and a bound that is off is DROPPED rather
// than stated as a zero — the block's rule everywhere. The width and the returns-together fact
// stay: a session that can delegate always has both.
func TestOrientation_DelegationBoundsOmitTheCeilingAndCapClausesWhenOff(t *testing.T) {
	cfg := orientationConfig(t)
	cfg.ParallelAgents = 4
	cfg.Delegation.FanOutRounds = 0
	cfg.Delegation.MaxSteps = 0
	a := newProfileAgent(t, cfg, echoResponder(t, "All done."))

	block := a.orientationBlock()

	want := "- Delegation bounds: up to 4 run at once; a reply's whole group returns together."
	if !strings.Contains(block, want) {
		t.Errorf("block is missing the two-clause bounds bullet %q:\n%q", want, block)
	}
}

// TestOrientation_DelegationBoundsLineAbsentWithoutSubAgent: the roster is the gate. A delegate at
// the depth bound is built without sub_agent (defaultSubAgentTools), so its own block states no
// bounds — a bound on a tool it cannot call would be noise — while the parent that spawned it does.
func TestOrientation_DelegationBoundsLineAbsentWithoutSubAgent(t *testing.T) {
	cfg := orientationConfig(t) // Delegation.MaxDepth 0 reads as 1: the child is AT the bound
	cfg.Delegation.FanOutRounds = 2
	cfg.Delegation.MaxSteps = 80
	parent := newProfileAgent(t, cfg, echoResponder(t, "All done."))
	child, err := parent.newChildAgent("call_sub", "a delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}

	if block := parent.orientationBlock(); !strings.Contains(block, delegationBoundsLabel) {
		t.Fatalf("the parent's own block states no Delegation bounds bullet: %q", block)
	}
	if block := child.orientationBlock(); strings.Contains(block, delegationBoundsLabel) {
		t.Errorf("the child's block states bounds on a tool it does not hold: %q", block)
	}
}
