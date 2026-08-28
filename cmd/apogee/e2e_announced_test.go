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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

// announcedSkillFixture builds the dotfiles-symlinked host shape and returns its parts.
//
// It is the owner's real layout rather than an abstraction of it, because the two symlinks are the
// bug: `<tmp>/home/.apogee` is a link into a dotfiles checkout, and that checkout's `skills/` is
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
	mkdirAll(t, filepath.Join(tmp, "home"))
	home := filepath.Join(tmp, "home", ".apogee")
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

// assertEveryToolCallNames fails unless the run issued tool calls at all and every one of them
// carries want in its arguments — the assertion that the model used the announced path verbatim
// rather than a path the fixture could have supplied itself.
func assertEveryToolCallNames(t *testing.T, stub *stubllm.Server, want string) {
	t.Helper()

	calls := 0
	seen := make(map[string]bool)
	for _, req := range stub.Requests() {
		for _, msg := range req.Messages {
			for _, call := range msg.ToolCalls {
				if seen[call.ID] {
					continue
				}
				seen[call.ID] = true
				calls++
				if !strings.Contains(call.Arguments, want) {
					t.Errorf("the %s call reads %s; want the announced path %s",
						call.Name, call.Arguments, want)
				}
			}
		}
	}
	if calls == 0 {
		t.Fatalf("the run issued no tool calls at all; the announced path was never exercised")
	}
}
