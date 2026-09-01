package main

// The announced-paths floor: in Auto, every path apogee ANNOUNCES to the model must be usable by
// every tool, with nobody asked.
//
// Two independently correct changes shipped in v0.18.2 broke exactly that and each passed its own
// item's tests — a skill header naming a folder the read tools then refused, an orientation line
// naming a scratch dir the dangerous-action guard then prompted on. What neither could fail was a
// test that reads the announcement off the wire and hands it straight back: the fixtures here
// script the model to use the path it was told, verbatim, over the host shapes the announcement
// actually meets (a dotfiles-symlinked home, a symlinked workspace), and assert that no approval
// pane was raised on the way.
//
// This file owns the suite's shared fixtures — the symlinked-home builder, symlinkTo, and the
// in-process pane counter that stands in for paneTrace, which reads a pty run's trace file and so
// has nothing to answer with under the in-process driver.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// announcedMarker is the line the fixture skill's bundled file carries: what a read of that file
// through the announced path has to come back with, and what the grep goes looking for. It is
// distinctive enough that finding it in a tool result cannot be an accident.
const announcedMarker = "APOGEE-ANNOUNCED-MARKER-8f21"

// announcedSkillPrompt invokes the fixture skill the way a human does — the inline `/<skill-id>`
// token documented in docs/manual/commands.md — rather than by pre-injecting a header into the
// script. The `files:` path the stub captures is therefore the one the shipped code rendered.
const announcedSkillPrompt = "/announced Please exercise every read tool on your own bundled files."

// symlinkedHome is the host shape the owner's devbox actually has: an apogee home reached through a
// dotfiles symlink, whose skill library is itself a symlink into a repo somewhere else again.
type symlinkedHome struct {
	home string // the SYMLINK spelling of the apogee home — what --config is given
	real string // the directory that symlink resolves to
	repo string // the skill library the home's skills/ symlink points at
	ws   string // the workspace the run edits
}

// TestE2EAnnouncedSkillPathsUnderASymlinkedHome is the invariant over the skill header: the folder
// apogee names on the `files:` line is readable, listable, greppable, findable and copyable through
// that exact spelling, in Auto, with no approval pane raised.
//
// Every argument the model sends is the captured header path rather than a path this test built, so
// the day the header announces something the read tools do not mount, the tools fail here rather
// than in a user's session. The spelling itself is asserted too: the header must keep naming the
// home the operator configured, symlinks and all, and a header that started announcing the resolved
// path would fail this test rather than silently change what is covered.
func TestE2EAnnouncedSkillPathsUnderASymlinkedHome(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "announced-skill"))
	fx := announcedSkillFixture(t, stub)
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIOn(t, drv, stub, fx.home, fx.ws, "--mode", "auto")
	panes := watchApprovalPanes(t, drv)

	submit(drv, announcedSkillPrompt)
	drv.WaitText("All bundled files answered.")
	drv.WaitQuiet(settled)

	// The header announced the configured home's spelling — the symlink — and the model used it.
	wantDir := filepath.Join(fx.home, "skills", "announced")
	assertEveryToolCallNames(t, stub, wantDir)

	// Every one of the five results came back with the bundled file in it, and none of them is the
	// read-scope refusal this whole suite exists to keep out of a user's session.
	results := toolResults(stub)
	if len(results) != 5 {
		t.Fatalf("the run produced %d tool results; want the fixture's five:\n%s",
			len(results), strings.Join(results, "\n---\n"))
	}
	for i, got := range results {
		if strings.Contains(got, "outside the workspace root") {
			t.Errorf("tool result %d was refused as an escape:\n%s", i+1, got)
		}
		if !strings.Contains(got, "a.md") {
			t.Errorf("tool result %d does not name the bundled file:\n%s", i+1, got)
		}
	}
	// The read and the grep are the two that carry the file's own content back.
	for _, i := range []int{0, 3} {
		if !strings.Contains(results[i], announcedMarker) {
			t.Errorf("tool result %d does not carry the marker line:\n%s", i+1, results[i])
		}
	}

	// The copy landed in the workspace with the marker intact — the announced path is not merely
	// readable, its content survives being copied out of the library.
	if got := sess.readWorkspaceFile(filepath.Join("out", "a.md")); !strings.Contains(got, announcedMarker) {
		t.Errorf("the copied bundled file reads %q; want the marker line", got)
	}

	if n := panes(); n != 0 {
		t.Errorf("the run raised %d approval pane(s); an announced path must cost nobody a look", n)
	}
	if un := stub.Unmatched(); len(un) > 0 {
		t.Errorf("the run made %d request(s) the script did not anticipate: %v", len(un), un)
	}
	stub.AssertConsumed(t)

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// announcedUser is the account name the fixture's home hangs under, so the shape it builds is a
// `/home/<user>/.apogee` and not merely an `.apogee` somewhere under a temp root. The difference is
// load-bearing for the scratch fixture: the dangerous-action guard's control-plane rule is anchored
// on a HOME spelling (internal/security's homeAnchor), so under a home the guard cannot recognise
// the rule never fires, the forced look never happens, and a test claiming the announced scratch dir
// escapes it would pass with the escape removed.
const announcedUser = "operator"

// announcedSkillFixture builds the dotfiles-symlinked host shape and returns its parts.
//
// It is the owner's real layout rather than an abstraction of it, because the two symlinks are the
// bug: `<tmp>/home/<user>/.apogee` is a link into a dotfiles checkout, and that checkout's `skills/` is
// itself a link into a third tree. A path announced out of the first is resolved through both
// before any tool can read it, and the run is handed the LINK spelling throughout.
func announcedSkillFixture(t *testing.T, stub *stubllm.Server) symlinkedHome {
	t.Helper()

	tmp := t.TempDir()

	// The home apogee will read, moved into a dotfiles checkout and reached through a link.
	real := filepath.Join(tmp, "dotfiles", "apogee")
	mkdirAll(t, filepath.Dir(real))
	if err := os.Rename(e2eHome(t, stub), real); err != nil {
		t.Fatalf("move the e2e home into the dotfiles checkout: %v", err)
	}
	mkdirAll(t, filepath.Join(tmp, "home", announcedUser))
	home := filepath.Join(tmp, "home", announcedUser, ".apogee")
	linkAt(t, home, real)

	// The skill library, in a third tree, reached from the home through a second link.
	repo := filepath.Join(tmp, "skills-repo")
	skill := filepath.Join(repo, "announced")
	mkdirAll(t, filepath.Join(skill, "prompts"))
	writeFile(t, filepath.Join(skill, "SKILL.md"), announcedSkillBody)
	writeFile(t, filepath.Join(skill, "prompts", "a.md"),
		"# The bundled prompt\n\n"+announcedMarker+"\n")
	linkAt(t, filepath.Join(real, "skills"), repo)

	// The workspace the copy lands in. copy_file writes into an existing folder, so out/ is seeded
	// rather than created mid-run.
	ws := e2eWorkspace(t)
	mkdirAll(t, filepath.Join(ws, "out"))

	return symlinkedHome{home: home, real: real, repo: repo, ws: ws}
}

// announcedSkillBody is the fixture skill: a real SKILL.md, with the {{SKILL_DIR}} token in it so
// the body the model receives is one whose paths the shipped expansion filled in. Its instructions
// match what the script makes the model do, so the fixture reads as a skill rather than as a prop.
const announcedSkillBody = `---
name: announced
description: Exercise every read tool against this skill's own bundled files.
---

# Announced

Your bundled files live in the folder the ` + "`files:`" + ` line above names.

1. ` + "`read_file`" + ` the prompt at {{SKILL_DIR}}/prompts/a.md.
2. ` + "`copy_file`" + ` that same file into the workspace as out/a.md.
3. ` + "`list_dir`" + ` the folder, ` + "`grep`" + ` it for ` + announcedMarker +
	", and `find_files` a.md under it.\n\nUse the dedicated read tools for all of it, never a terminal command.\n"

// announcedScratchPrompt is what the scratch fixture's model is asked, and the phrase
// announced-scratch.yaml keys its one capturing turn on.
const announcedScratchPrompt = "Set up the scratch dir and then the control plane probe."

// announcedScratchEcho is what the scratch command prints when it has done all of its work: the
// last link of an `&&` chain, so a result carrying it is a result where the mkdir and the write
// under the announced path both succeeded.
const announcedScratchEcho = "scratch-ok"

// announcedStandingPrompt is the one config key this fixture's home needs beyond the e2e default.
// The orientation block RIDES ALONG on a standing system message rather than seeding one of its own
// (ADR 0023 §6 amendment), so a home with neither a prompt nor a workspace context file sends no
// system message at all and states no host facts — there would be nothing announced to test. A real
// install always has that message: since ADR 0064 a config stating no prompt at all resolves the
// EMBEDDED default. This fixture states a prompt of its own rather than leaning on that text, so
// what is asserted below stays about the orientation block and not about the default's wording.
const announcedStandingPrompt = "system-prompt-text: |\n  You are apogee, a terminal coding agent.\n"

// TestE2EAnnouncedScratchDirRunsUnpromptedInAuto is the invariant over the orientation block: the
// dir apogee names on its `Scratch dir:` line is writable, in Auto, with nobody asked — while the
// control plane the same guard rule protects still forces a look.
//
// The two halves are one conversation on purpose. The scratch dir lives UNDER `~/.apogee`, which is
// exactly what the dangerous-action guard's Tier-2 rule matches, so for one release a model doing
// precisely what the orientation told it was stopped to ask permission on every scratch-routed
// command. Asserting only that nobody was asked would be passed by an approver that had stopped
// asking altogether; the `touch ~/.apogee/guard-probe.txt` in the same run is the positive control
// that makes the silence mean something.
func TestE2EAnnouncedScratchDirRunsUnpromptedInAuto(t *testing.T) {
	// The control command names `~`, and a run that somehow let it through would touch the
	// developer's own apogee home. It is denied below, but the home it would reach is a temp one
	// either way.
	guardHome(t)
	installFenceableConfiner(t)

	stub := stubllm.New(t, loadScript(t, "announced-scratch"))
	fx := announcedSkillFixture(t, stub)
	appendHomeConfig(t, fx.home, announcedStandingPrompt)
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIOn(t, drv, stub, fx.home, fx.ws, "--mode", "auto")
	panes := watchApprovalPanes(t, drv)

	submit(drv, announcedScratchPrompt)

	// The FIRST pane the run raises carries the whole negative claim. Nothing but the line below
	// answers a pane in this fixture, so the scratch command has to have run — unasked — for the
	// conversation to have reached the control at all; and a guard that forced a look at the
	// announced scratch dir would raise ITS pane first, which is what the command assertions here
	// tell apart.
	pane := paneText(awaitApprovalPane(drv))
	if !strings.Contains(pane, forcedReason) {
		t.Errorf("the pane does not read %q; the control plane no longer forces a look:\n%s", forcedReason, pane)
	}
	if !strings.Contains(pane, "guard-probe.txt") {
		t.Errorf("the first pane of the run is not the control plane's:\n%s", pane)
	}
	if strings.Contains(pane, "TMPDIR") {
		t.Errorf("the announced scratch dir raised an approval pane; a path apogee itself named "+
			"must cost nobody a look:\n%s", pane)
	}
	decide(drv, "d")
	drv.WaitText("Both commands answered.")
	drv.WaitQuiet(settled)

	// The orientation announced the configured home's spelling — the symlink — and the model wrote
	// through it. Reading the path back off the command the model SENT is reading the announcement
	// itself: the script captured it out of that request's own system prompt.
	scratch := announcedScratchDir(t, stub)
	if got, want := filepath.Dir(scratch), filepath.Join(fx.home, "scratch"); got != want {
		t.Errorf("the orientation announced the scratch dir as %s; want this session's dir under "+
			"the configured home %s", scratch, want)
	}

	results := toolResults(stub)
	if len(results) != 2 {
		t.Fatalf("the run produced %d tool results; want the fixture's two:\n%s",
			len(results), strings.Join(results, "\n---\n"))
	}
	if !strings.Contains(results[0], announcedScratchEcho) {
		t.Errorf("the scratch command did not run to its end:\n%s", results[0])
	}
	if !strings.Contains(results[1], "denied by approver") {
		t.Errorf("the denied control call did not come back as a denial:\n%s", results[1])
	}

	// And the write landed in the REAL tree the home symlink points at, not merely somewhere that
	// spells the same.
	probe := filepath.Join(fx.real, "scratch", filepath.Base(scratch), "tmp", "probe")
	if _, err := os.Stat(probe); err != nil {
		t.Errorf("the scratch write did not reach %s: %v", probe, err)
	}

	if n := panes(); n != 1 {
		t.Errorf("the run raised %d approval pane(s); want exactly the control plane's one", n)
	}
	if un := stub.Unmatched(); len(un) > 0 {
		t.Errorf("the run made %d request(s) the script did not anticipate: %v", len(un), un)
	}
	stub.AssertConsumed(t)

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// installFenceableConfiner keeps this suite's question about who was ASKED rather than about the
// machine it runs on. Auto on a backend that cannot fence the filesystem gates every terminal
// command through Approval instead (ADR 0012, "confine if you can, gate if you can't"), so a
// container without landlock would raise panes for a reason that has nothing to do with an
// announced path.
//
// A host that CAN fence takes its own real backend, so the command below is really confined. One
// that cannot takes the caps-only stand-in the headless tests already use: it reports FSWrite and
// confines nothing, which is the right trade for a fixture asserting PROMPTING. The fence itself
// has its own suite (confinement_e2e_test.go).
func installFenceableConfiner(t *testing.T) {
	t.Helper()

	if platform.NewConfiner().Capabilities().FSWrite {
		return
	}
	previous := newConfiner
	newConfiner = func() apogee.Confiner { return fenceableHost }
	t.Cleanup(func() { newConfiner = previous })
}

// announcedScratchExport lifts the scratch dir back out of the command the model sent. The `\S+` is
// greedy and the path has no spaces in it, so it ends at the LAST `/tmp ` in the export — the one
// the fixture appended — rather than at the temp root the path itself starts under.
var announcedScratchExport = regexp.MustCompile(`export TMPDIR=(\S+)/tmp `)

// announcedScratchDir is the scratch path the run's terminal command carried: what the script
// captured out of the orientation on that request and handed straight back, so reading it here
// reads what apogee ANNOUNCED rather than what this test could have built for itself.
func announcedScratchDir(t *testing.T, stub *stubllm.Server) string {
	t.Helper()

	for _, call := range toolCalls(stub) {
		if match := announcedScratchExport.FindStringSubmatch(call.Arguments); match != nil {
			return match[1]
		}
	}
	t.Fatalf("no terminal call exported TMPDIR; the run never used the announced scratch dir")
	return ""
}

// announcedWorkspacePrompt is what the workspace fixture's model is asked, and the phrase
// announced-workspace.yaml keys its first tool turn on.
const announcedWorkspacePrompt = "Use every workspace tool on the project tree."

// announcedWorkspaceEdit is the line the edit leaves behind in the seeded file, and
// announcedWorkspaceWrite the line the write leaves in the file it creates. Each is what a read of
// the REAL tree has to come back with, which is a stronger claim than the tool's own receipt: a
// write that landed anywhere but where the project actually lives would still report success
// against the name it was given.
const (
	announcedWorkspaceEdit  = "APOGEE-ANNOUNCED-EDIT-4b93"
	announcedWorkspaceWrite = "APOGEE-ANNOUNCED-WRITE-2c07"
)

// announcedWorkspaceCanary is a file this test seeds into the real tree and tells nobody about. The
// listing's receipt has to name it: the script never mentions it, so a `list_dir` that answered
// from anywhere but the directory the announced name resolves to could not have invented it.
const announcedWorkspaceCanary = "canary-9d4e.txt"

// TestE2EAnnouncedWorkspaceThroughASymlink is the invariant over the orientation's `Workspace:`
// line on the host shape macOS hands every user by default: a project tree reached through a
// symlink, so the name apogee announces and the path the filesystem resolves it to differ in every
// byte after the temp root.
//
// The workspace is the one root every tool measures against, and the two sides of that measurement
// are spelled differently here on purpose. A read tool that resolved the announced path and then
// compared it against the CONFIGURED root would call the workspace's own files an escape; a write
// tool that compared the other way round would refuse to create a file in the project. The chain
// below reads, writes, edits, lists and cats through the announced spelling alone — the last of
// those from a confined subprocess, whose fence is built from the same name — and asserts that not
// one of them was refused, and that nobody was asked.
//
// Every receipt is judged against the real tree rather than against its own wording: the listing has
// to name a canary file the script never mentions, and each write is read back out of the directory
// the link resolves to. A tool that answered from the wrong root would leave those checks with
// nothing to find.
func TestE2EAnnouncedWorkspaceThroughASymlink(t *testing.T) {
	installFenceableConfiner(t)

	// The tree the project really lives in, and the name apogee is given for it.
	tree := e2eWorkspace(t)
	writeFile(t, filepath.Join(tree, announcedWorkspaceCanary), "seeded before the run\n")
	ws := symlinkTo(t, tree)

	stub := stubllm.New(t, loadScript(t, "announced-workspace"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIIn(t, drv, stub, ws, announcedStandingPrompt, "--mode", "auto")
	panes := watchApprovalPanes(t, drv)

	submit(drv, announcedWorkspacePrompt)
	drv.WaitText("Every workspace tool answered.")
	drv.WaitQuiet(settled)

	// The orientation announced the configured spelling — the link — and every call the model made
	// used it. A run whose orientation had started naming the resolved tree would fail here rather
	// than quietly leave the symlinked shape untested.
	assertEveryToolCallNames(t, stub, ws)

	results := toolResults(stub)
	if len(results) != 5 {
		t.Fatalf("the run produced %d tool results; want the fixture's five:\n%s",
			len(results), strings.Join(results, "\n---\n"))
	}
	// What the run actually left in the tree the link points at. The two write receipts are judged
	// against these bytes rather than against their own wording: a tool that had measured the
	// announced spelling against the resolved root and written somewhere else would still report
	// success against the name it was handed.
	wrote := readTreeFile(t, tree, "b.txt")
	edited := readTreeFile(t, tree, "a.txt")

	// Each result is its own tool's success receipt. A tool that had measured the announced spelling
	// against the resolved root would answer with the read-scope refusal instead, which is the
	// failure this whole suite exists to keep out of a user's session.
	wants := [][]string{
		// read_file gave the seeded file back, before the edit replaced it.
		{"hello"},
		// write_file created a file in the project, and counted the bytes that landed on disk.
		{fmt.Sprintf("wrote %d bytes to ", len(wrote)), "b.txt"},
		// edit_existing_file replaced the seeded file's content.
		{"updated ", "a.txt"},
		// list_dir saw all three files, the canary the script never mentions included.
		{"a.txt", "b.txt", announcedWorkspaceCanary},
		// And the confined subprocess read the edit back through the link.
		{announcedWorkspaceEdit},
	}
	for i, want := range wants {
		if strings.Contains(results[i], "outside the workspace root") {
			t.Errorf("tool result %d was refused as an escape:\n%s", i+1, results[i])
			continue
		}
		for _, text := range want {
			if !strings.Contains(results[i], text) {
				t.Errorf("tool result %d does not read as a success — no %q in it:\n%s",
					i+1, text, results[i])
			}
		}
	}

	// And each write reached the tree the link points at, not merely a path that spells the same.
	if !strings.Contains(wrote, announcedWorkspaceWrite) {
		t.Errorf("the real workspace holds %q as b.txt; want the written content", wrote)
	}
	if !strings.Contains(edited, announcedWorkspaceEdit) {
		t.Errorf("the real workspace holds %q as a.txt; want the edited content", edited)
	}

	if n := panes(); n != 0 {
		t.Errorf("the run raised %d approval pane(s); an announced path must cost nobody a look", n)
	}
	if un := stub.Unmatched(); len(un) > 0 {
		t.Errorf("the run made %d request(s) the script did not anticipate: %v", len(un), un)
	}
	stub.AssertConsumed(t)

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// announcedSeatCalls are the two `run_on` arguments the model must end up sending — the two values
// the orientation's Delegations line names, in the order it names them. They are spelled here as
// they ride the wire so that a value the fixture merely echoed and a value it re-invented cannot
// both satisfy the check.
var announcedSeatCalls = []string{`"run_on":"session"`, `"run_on":"sub-agents-server"`}

// TestE2EAnnouncedDelegationSeatsAreAcceptedVerbatim is the invariant over the Delegations line
// (ADR 0069): every `run_on` value apogee names in the orientation is a value the sub_agent tool
// then accepts, and the delegation runs where it says.
//
// It is the same shape as the rest of this suite and for the same reason. The line is an
// ANNOUNCEMENT — the model is told "run_on \"sub-agents-server\" = qwen on grunt-box" and has no
// other source for the word — so a schema that drifted from the prose (an enum re-spelled, a value
// dropped, a clause the renderer stopped emitting) would be an instruction apogee itself refuses.
// The fixture never spells the two values: it lifts them out of the system text of the very request
// it is answering (seat-session.yaml's captures) and hands them straight back, so the day the line
// changes this test fails rather than quietly testing a constant of its own.
func TestE2EAnnouncedDelegationSeatsAreAcceptedVerbatim(t *testing.T) {
	run := launchSeatSession(t, seatChoiceModel)

	// Routing has to be in force before the far value can mean anything: an ask that finds no
	// target falls back, which would prove the fallback rather than the acceptance.
	awaitNotice(t, run.drv, "sub-agents: routing to "+seatTargetServer)
	submit(run.drv, seatAnnouncedPrompt)
	run.drv.WaitText(seatWrapUp)
	run.drv.WaitQuiet(settled)

	// What the model sent is what it was told, in the order it was told.
	calls := toolCalls(run.session)
	if len(calls) != 2 {
		t.Fatalf("the run issued %d tool calls; want the fixture's two delegations", len(calls))
	}
	for i, want := range announcedSeatCalls {
		if !strings.Contains(calls[i].Arguments, want) {
			t.Errorf("delegation %d reads %s; want the announced seat %s", i+1, calls[i].Arguments, want)
		}
	}

	// And both were honoured rather than refused: one child on each server, and no result carrying
	// the engine's own refusal of a value it does not know.
	if got := childRequests(run.session, seatNearTask); got != 1 {
		t.Errorf("the session server answered %d of the near child's requests; want 1", got)
	}
	if got := childRequests(run.target, seatFarTask); got != 1 {
		t.Errorf("the sub-agents server answered %d of the far child's requests; want 1", got)
	}
	for i, res := range toolResults(run.session) {
		if strings.Contains(res, "invalid run_on") {
			t.Errorf("delegation %d was refused the seat the orientation named it:\n%s", i+1, res)
		}
	}
	run.quit(t)
}

// ----------------------------------------------------------------------------
// Shared fixtures for the announced-paths suite
// ----------------------------------------------------------------------------

// symlinkTo makes a symlink to target in a temp dir of its own and returns the LINK's path — the
// spelling a test hands apogee when what is under test is that apogee answers about the path it was
// GIVEN rather than the one the filesystem resolves it to.
func symlinkTo(t *testing.T, target string) string {
	t.Helper()

	link := filepath.Join(t.TempDir(), filepath.Base(target)+"-link")
	linkAt(t, link, target)
	return link
}

// linkAt makes one symlink at link pointing at target.
func linkAt(t *testing.T, link, target string) {
	t.Helper()

	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("link %s -> %s: %v", link, target, err)
	}
}

// mkdirAll creates dir and its parents.
func mkdirAll(t *testing.T, dir string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
}

// writeFile writes one fixture file, creating nothing above it.
func writeFile(t *testing.T, path, body string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// readTreeFile reads one file back out of the tree a workspace symlink points at — the REAL
// directory, named without going through the link, so a tool that wrote to the announced spelling
// and a tool that wrote to the resolved one cannot both satisfy it by accident.
func readTreeFile(t *testing.T, tree, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(tree, name))
	if err != nil {
		t.Fatalf("read %s back from the real workspace: %v", name, err)
	}
	return string(body)
}

// paneWatchInterval is how often the pane counter reads the frame. An approval pane stands until it
// is answered, and these fixtures answer none, so any interval short of the suite's own timeout
// catches one — this is small enough that a test asserting "no pane BEFORE the next request" is
// asking about the moment it thinks it is.
const paneWatchInterval = 5 * time.Millisecond

// watchApprovalPanes counts the approval panes a driven run raises, and returns the reader for that
// count. It is the in-process counterpart of [paneTrace]: the trace file paneTrace reads is written
// by --tui-trace, which only a pty run passes (launchTUI's own note), so an in-process fixture has
// no cumulative paint to count panes out of and watches the frame instead.
//
// It counts RISING EDGES rather than frames, so a pane that stands over many polls is one pane, and
// a second pane raised after the first is answered is two. The watcher stops at cleanup, before the
// suite's leak check runs.
func watchApprovalPanes(t *testing.T, drv *tuitest.Driver) func() int {
	t.Helper()

	var (
		mu    sync.Mutex
		count int
		up    bool
	)
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(paneWatchInterval)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
			}
			_, _, showing := drv.Frame().Find(approvalMarker)
			mu.Lock()
			if showing && !up {
				count++
			}
			up = showing
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		close(stop)
		<-done
	})
	return func() int {
		mu.Lock()
		defer mu.Unlock()
		return count
	}
}

// toolResults is every tool-result message the stub was sent, in the order the run produced them —
// what a claim about what a TOOL answered is made against, since the transcript shows the same text
// clipped inside a collapsed block. A result is told by its ToolCallID, which only a tool message
// carries.
func toolResults(stub *stubllm.Server) []string {
	var out []string
	seen := make(map[string]bool)
	for _, req := range stub.Requests() {
		for _, msg := range req.Messages {
			if msg.ToolCallID == "" || seen[msg.ToolCallID] {
				continue
			}
			seen[msg.ToolCallID] = true
			out = append(out, msg.Content)
		}
	}
	return out
}

// toolCalls is every tool call the run issued, in the order it issued them and each seen once —
// what a claim about the ARGUMENTS the model sent is made against. A request carries the whole
// conversation so far, so the same call arrives again on every later request; the id tells them
// apart.
func toolCalls(stub *stubllm.Server) []stubllm.ToolCall {
	var out []stubllm.ToolCall
	seen := make(map[string]bool)
	for _, req := range stub.Requests() {
		for _, msg := range req.Messages {
			for _, call := range msg.ToolCalls {
				if seen[call.ID] {
					continue
				}
				seen[call.ID] = true
				out = append(out, call)
			}
		}
	}
	return out
}

// assertEveryToolCallNames fails unless the run issued tool calls at all and every one of them
// carries want in its arguments — the assertion that the model used the announced path verbatim
// rather than a path the fixture could have supplied itself.
func assertEveryToolCallNames(t *testing.T, stub *stubllm.Server, want string) {
	t.Helper()

	calls := toolCalls(stub)
	if len(calls) == 0 {
		t.Fatalf("the run issued no tool calls at all; the announced path was never exercised")
	}
	for _, call := range calls {
		if !strings.Contains(call.Arguments, want) {
			t.Errorf("the %s call reads %s; want the announced path %s",
				call.Name, call.Arguments, want)
		}
	}
}
