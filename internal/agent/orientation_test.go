package agent

// The engine-owned orientation block (orientation.go): what it states, what it omits, and the
// RIDE-ALONG rule that keeps the "send no system prompt" configuration byte-identical. The
// assertions pin the block's structure — its header and each bullet's label with the exact path
// it names — rather than the whole prose sentence, so the wording may be tightened while the
// facts and their shape stay pinned.

import (
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
func TestOrientation_NoTemplateAndNoContextFilesSeedsNothing(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.WorkspaceDir = orientationWorkspaceDir
	cfg.ScratchDir = orientationScratchDir

	a := newProfileAgent(t, cfg, &recordingResponder{reply: "All done."})

	if got := a.standingSystem(); got != "" {
		t.Errorf("standingSystem() = %q, want \"\" (nothing configured seeds nothing)", got)
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
// config it inherits, so the sub-agent is oriented by the same facts with no wiring of its own.
func TestOrientation_SubAgentInheritsTheBlock(t *testing.T) {
	cfg := orientationConfig(t)
	cfg.ScratchDir = orientationScratchDir

	parent := newProfileAgent(t, cfg, &recordingResponder{reply: "All done."})
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
// THAT test configures, then the engine's own orientation block, then the context-file blocks it
// configures — the wire order standingSystem composes, with either configured part passed as ""
// when the test does not have one. The block's own text is pinned by the tests above; taken from
// the agent under test here, so a seam test keeps asserting the exact bytes of the parts it owns
// without restating text it does not.
func withOrientation(a *Agent, rendered, blocks string) string {
	parts := make([]string, 0, 3)
	if rendered != "" {
		parts = append(parts, rendered)
	}
	if block := a.orientationBlock(); block != "" {
		parts = append(parts, block)
	}
	if blocks != "" {
		parts = append(parts, blocks)
	}
	return strings.Join(parts, "\n\n")
}
