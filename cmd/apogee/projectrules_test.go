package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/adoption"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/filewatch"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tools"
)

// The four Allow-rule acts of the config host (projectrules.go, ADR 0096 §4), driven the way the
// approval pane and the adoption pane drive them: through configHost, over a wiring whose engine is
// a real Agent in ask-before on a scripted upstream, so "the next identical call runs without
// asking" is the loop's own behaviour rather than a spy's.

// projectRuleCommand is the word prefix the tests save: the first word of the scripted command.
const projectRuleCommand = "echo"

// echoRule is the project rule projectRuleCommand is saved as.
var echoRule = apogee.AllowRule{Kind: apogee.AllowRuleTerminal, Text: projectRuleCommand, Layer: apogee.AllowRuleProject}

// projectRuleWiring is a rootWiring with what the rule acts reach: a home naming endpoint, a Project
// root that is the workspace, the live-settings holder, the external-edit baseline, and an engine
// bound to a real Agent in ask-before whose every gate goes to approver.
func projectRuleWiring(t *testing.T, endpoint string, approver apogee.Approver, sink apogee.EventSink) *rootWiring {
	t.Helper()
	home := upstreamHome(t, endpoint)
	root := t.TempDir()
	opts := config.Options{ConfigDir: home, Workspace: root}
	w := &rootWiring{
		roots:         stateRoots{config: home, workspace: root, project: root},
		engine:        newLateEngine(domain.ModeAskBefore, false),
		live:          newLiveSettings(opts),
		externalEdits: newExternalEdit(opts, root, noEnvironment),
	}
	t.Cleanup(func() { _ = w.engine.Close() })
	err := w.engine.Bind(func() (*apogee.Agent, error) {
		return apogee.New(apogee.Config{
			Endpoint:     endpoint,
			Model:        "fake",
			Mode:         domain.ModeAskBefore,
			Events:       sink,
			Approver:     approver,
			Tools:        tools.NewDefaultRegistry(root),
			WorkspaceDir: root,
		})
	})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	return w
}

// assertRuleRecord holds the Project config and the adoption record to what an act left behind:
// whether the file lists the rule, and the answer the record holds for it.
func assertRuleRecord(t *testing.T, w *rootWiring, inFile bool, want adoption.State) {
	t.Helper()
	data, err := os.ReadFile(config.ProjectFilePath(w.roots.project))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read the project config: %v", err)
	}
	if got := strings.Contains(string(data), "- "+projectRuleCommand+"\n"); got != inFile {
		t.Errorf("project config lists the rule = %v, want %v:\n%s", got, inFile, data)
	}
	store, err := adoption.New(w.workspacesDir(), w.roots.project)
	if err != nil {
		t.Fatalf("adoption.New: %v", err)
	}
	sorted, err := store.Classify([]adoption.Entry{{Kind: string(config.AllowTerminal), Text: projectRuleCommand}})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	got := adoption.Proposed
	switch {
	case len(sorted.Adopted) == 1:
		got = adoption.Adopted
	case len(sorted.Rejected) == 1:
		got = adoption.Rejected
	}
	if got != want {
		t.Errorf("the record answers %q for the rule, want %q", got, want)
	}
}

// assertNoReloadedKey holds apogee's own write to ADR 0041 decision 8: a re-read right after the act
// reports no key and no notice, so the watcher has no "config changed on disk" line to print.
func assertNoReloadedKey(t *testing.T, host configHost) {
	t.Helper()
	reload, err := host.ReloadConfig()
	if err != nil {
		t.Fatalf("ReloadConfig: %v", err)
	}
	if len(reload.Applied) != 0 || len(reload.Notices) != 0 {
		t.Errorf("ReloadConfig after the act = %+v; want nothing — the write was apogee's own", reload)
	}
}

// Adding a project rule writes the file, adopts its text and answers the next identical call
// without asking; removing it takes both back, and the call asks again.
func TestProjectRuleAddAnswersTheNextCallAndRemoveTakesItBack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv := scriptedTerminalModel(t)
	approver := &e2eApprover{}
	sink := &e2eSink{}
	w := projectRuleWiring(t, srv.URL, approver, sink)
	host := configHost{w: w}

	runE2EExchange(t, ctx, w.engine, "run a command")
	if got := approver.requests(); len(got) != 1 {
		t.Fatalf("approval requests before any rule = %d, want the one ordinary gate", len(got))
	}

	if err := host.AddProjectRule(apogee.AllowRuleTerminal, projectRuleCommand); err != nil {
		t.Fatalf("AddProjectRule: %v", err)
	}
	assertRuleRecord(t, w, true, adoption.Adopted)
	assertNoReloadedKey(t, host)

	approver.reset()
	sink.reset()
	runE2EExchange(t, ctx, w.engine, "run it again")
	if got := approver.requests(); len(got) != 0 {
		t.Errorf("approval requests after the add = %+v, want none — the project rule answers the call", got)
	}
	assertTerminalRan(t, sink, "the call the new rule answered")

	if err := host.RemoveRule(echoRule); err != nil {
		t.Fatalf("RemoveRule: %v", err)
	}
	assertRuleRecord(t, w, false, adoption.Proposed)
	assertNoReloadedKey(t, host)

	approver.reset()
	sink.reset()
	runE2EExchange(t, ctx, w.engine, "and once more")
	if got := approver.requests(); len(got) != 1 {
		t.Errorf("approval requests after the remove = %d, want the gate back", len(got))
	}
}

// A rejection takes an adopted rule out of force while the file keeps it, and a later adoption
// brings it back — the adoption pane's two answers over a rule already in the file.
func TestProjectRuleRejectAndAdoptMoveOnlyTheRecord(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv := scriptedTerminalModel(t)
	approver := &e2eApprover{}
	w := projectRuleWiring(t, srv.URL, approver, &e2eSink{})
	host := configHost{w: w}

	if err := host.AddProjectRule(apogee.AllowRuleTerminal, projectRuleCommand); err != nil {
		t.Fatalf("AddProjectRule: %v", err)
	}
	if err := host.RejectRules([]apogee.AllowRule{echoRule}); err != nil {
		t.Fatalf("RejectRules: %v", err)
	}
	assertRuleRecord(t, w, true, adoption.Rejected)
	assertNoReloadedKey(t, host)
	runE2EExchange(t, ctx, w.engine, "run a command")
	if got := approver.requests(); len(got) != 1 {
		t.Errorf("approval requests with the rule rejected = %d, want the gate", len(got))
	}

	if err := host.AdoptRules([]apogee.AllowRule{echoRule}); err != nil {
		t.Fatalf("AdoptRules: %v", err)
	}
	assertRuleRecord(t, w, true, adoption.Adopted)
	approver.reset()
	runE2EExchange(t, ctx, w.engine, "run it again")
	if got := approver.requests(); len(got) != 0 {
		t.Errorf("approval requests with the rule adopted = %+v, want none", got)
	}
}

// A rule written into the Project config behind apogee's back — a teammate's commit, a pull — is
// what the adoption pane reads as proposed: inert until adopted, and no longer proposed once it is.
func TestProjectRuleAdoptionReadsWhatTheFileProposes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv := scriptedTerminalModel(t)
	approver := &e2eApprover{}
	w := projectRuleWiring(t, srv.URL, approver, &e2eSink{})
	host := configHost{w: w}
	path := config.ProjectFilePath(w.roots.project)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("allow:\n  terminal:\n    - "+projectRuleCommand+"\n"), 0o644); err != nil {
		t.Fatalf("write the project config: %v", err)
	}

	proposed, err := host.ProposedRules()
	if err != nil {
		t.Fatalf("ProposedRules: %v", err)
	}
	if want := []apogee.AllowRule{echoRule}; !reflect.DeepEqual(proposed, want) {
		t.Fatalf("ProposedRules = %+v, want %+v", proposed, want)
	}
	runE2EExchange(t, ctx, w.engine, "run a command")
	if got := approver.requests(); len(got) != 1 {
		t.Errorf("approval requests with the rule proposed = %d, want the gate", len(got))
	}

	if err := host.AdoptRules(proposed); err != nil {
		t.Fatalf("AdoptRules: %v", err)
	}
	if after, err := host.ProposedRules(); err != nil || len(after) != 0 {
		t.Errorf("ProposedRules after adopting = %+v, %v; want none", after, err)
	}
	approver.reset()
	runE2EExchange(t, ctx, w.engine, "run it again")
	if got := approver.requests(); len(got) != 0 {
		t.Errorf("approval requests with the rule adopted = %+v, want none", got)
	}
}

// A global rule is the human's own line and no act here edits it, nor answers for it; nothing is
// written when one is handed over.
func TestProjectRuleActsRefuseAGlobalRule(t *testing.T) {
	t.Parallel()
	srv := scriptedTerminalModel(t)
	w := projectRuleWiring(t, srv.URL, &e2eApprover{}, &e2eSink{})
	host := configHost{w: w}
	global := apogee.AllowRule{Kind: apogee.AllowRuleTerminal, Text: "make", Layer: apogee.AllowRuleGlobal}

	if err := host.RemoveRule(global); err == nil || !strings.Contains(err.Error(), "global config") {
		t.Errorf("RemoveRule(global) = %v, want a refusal naming the global config", err)
	}
	if err := host.AdoptRules([]apogee.AllowRule{global}); err == nil {
		t.Error("AdoptRules(global) succeeded, want a refusal")
	}
	if _, err := os.Stat(config.ProjectFilePath(w.roots.project)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the project config exists after refused acts (stat: %v)", err)
	}
}

// The child's task — what the parent delegates, and so the last message of the child's first
// request — and the gate that holds the child's first reply until the rule is added.
const (
	projectRuleChildTask = "Run the echo command"
	projectRuleChildGate = "rule-added"
)

// scriptedDelegatingModel is an upstream on which the parent delegates once and the child, held on
// projectRuleChildGate, then runs the scripted terminal command and reports. Only the child calls
// `terminal`, so a request ending in a terminal result is the child's.
func scriptedDelegatingModel(t *testing.T) *stubllm.Server {
	t.Helper()
	terminal := []stubllm.ToolCall{{
		ID: "call_child", Name: "terminal", Arguments: fmt.Sprintf(`{"command":%q}`, e2eTerminalCommand),
	}}
	delegate := []stubllm.ToolCall{{
		ID: "call_parent", Name: "sub_agent", Arguments: fmt.Sprintf(`{"task":%q,"name":"runner"}`, projectRuleChildTask),
	}}
	return stubllm.New(t, stubllm.Script{Model: "fake", Turns: []stubllm.Turn{
		{When: &stubllm.Match{ToolResult: "terminal"}, Repeat: true, Text: "The command ran."},
		{When: &stubllm.Match{LastMessage: "^" + projectRuleChildTask + "$"}, Repeat: true,
			Await: projectRuleChildGate, ToolCalls: terminal},
		{When: &stubllm.Match{ToolResult: "sub_agent"}, Repeat: true, Text: e2eFinalMessage},
		{Repeat: true, ToolCalls: delegate},
	}})
}

// A sub-agent already running when the rule is added reads it at its next gate: the child is held
// mid-flight, the rule lands, and the child's terminal call then runs without asking — the tree's
// one shared rule set, reached through the engine seam.
func TestProjectRuleReachesARunningSubAgent(t *testing.T) {
	t.Parallel()
	srv := scriptedDelegatingModel(t)
	approver := &e2eApprover{}
	w := projectRuleWiring(t, srv.URL, approver, &e2eSink{})
	host := configHost{w: w}

	done := make(chan error, 1)
	go func() { done <- runExchange(context.Background(), w.engine, "delegate the command") }()
	waitForChildRequest(t, srv)

	if err := host.AddProjectRule(apogee.AllowRuleTerminal, projectRuleCommand); err != nil {
		t.Fatalf("AddProjectRule: %v", err)
	}
	srv.Release(projectRuleChildGate)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the exchange: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the exchange did not finish")
	}

	for _, req := range approver.requests() {
		if req.Tool == "terminal" {
			t.Errorf("the running child asked for its terminal call: %+v", req)
		}
	}
	if !childRanTheCommand(srv) {
		t.Error("no child request carries the command's output; the child's call did not run")
	}
}

// runExchange is runE2EExchange for a goroutine: one Exchange to its quiescent boundary, reported
// as an error rather than a test failure.
func runExchange(ctx context.Context, engine *lateEngine, text string) error {
	if err := engine.Submit(apogee.UserInput{Text: text}); err != nil {
		return err
	}
	for {
		res, err := engine.Step(ctx)
		switch {
		case err != nil:
			return err
		case res.Status == apogee.StatusTurnComplete:
			continue
		case res.Status != apogee.StatusExchangeComplete:
			return fmt.Errorf("the exchange ended with status %q", res.Status)
		}
		return nil
	}
}

// waitForChildRequest waits until the child's first request has reached the upstream — the child
// is then running, held on its gate.
func waitForChildRequest(t *testing.T, srv *stubllm.Server) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		for _, req := range srv.Requests() {
			if n := len(req.Messages); n > 0 && req.Messages[n-1].Content == projectRuleChildTask {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the child's request never reached the upstream")
}

// childRanTheCommand reports whether a request carries a terminal result holding the command's
// output — only the child calls `terminal`.
func childRanTheCommand(srv *stubllm.Server) bool {
	for _, req := range srv.Requests() {
		for _, m := range req.Messages {
			if m.Role == "tool" && strings.Contains(m.Content, e2eEcho) {
				return true
			}
		}
	}
	return false
}

// awaitAndReload is what the renderer does with a watched change: wait for the report — bounded, so
// a chain that never reports fails rather than hanging — then re-read through the config host.
func awaitAndReload(t *testing.T, host configHost, why string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), configWatchTestDeadline)
	defer cancel()
	if !host.AwaitConfigChange(ctx) {
		t.Fatalf("no change reported for %s", why)
	}
	reload, err := host.ReloadConfig()
	if err != nil {
		t.Fatalf("ReloadConfig after %s: %v", why, err)
	}
	hasMoved := false
	for _, a := range reload.Applied {
		hasMoved = hasMoved || a.Path == settingKeyAllow
	}
	if !hasMoved {
		t.Fatalf("the re-read after %s = %+v; want the allow key reported moved", why, reload)
	}
}

// An adoption made by `apogee project adopt` in another terminal writes only the adoption record;
// the session's watcher on that record reports it, and the re-read puts the rule in force with no
// restart — the next identical call runs without asking.
func TestProjectRuleCLIAdoptionAppliesLiveWithoutARestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv := scriptedTerminalModel(t)
	approver := &e2eApprover{}
	w := projectRuleWiring(t, srv.URL, approver, &e2eSink{})
	host := configHost{w: w}
	writeProjectAllow(t, w.roots.project, projectRuleCommand)
	// The session started with the rule already proposed: that is its baseline, not a change.
	w.externalEdits.refresh()
	path := adoptionWatchPath(config.Options{ConfigDir: w.roots.config}, w.roots)
	if path == "" {
		t.Fatal("no adoption record to watch for a session with a Project config")
	}
	w.adoptionWatch = startConfigWatcher(t, path)

	runE2EExchange(t, ctx, w.engine, "run a command")
	if got := approver.requests(); len(got) != 1 {
		t.Fatalf("approval requests with the rule proposed = %d, want the gate", len(got))
	}

	if out, err := runProjectAdopt(t, w.roots.config, w.roots.project, true, "y"); err != nil {
		t.Fatalf("project adopt: %v\n%s", err, out)
	}
	awaitAndReload(t, host, "an adoption made by the CLI")

	approver.reset()
	runE2EExchange(t, ctx, w.engine, "run it again")
	if got := approver.requests(); len(got) != 0 {
		t.Errorf("approval requests after the CLI adoption = %+v, want none — the rule applies live", got)
	}
}

// A global `allow:` entry written into config.yaml by hand applies as soon as the watcher reports
// the save, and taking it out again — the edit that empties the rule set — puts the gate back.
func TestProjectRuleHandEditedGlobalAllowAppliesLive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv := scriptedTerminalModel(t)
	approver := &e2eApprover{}
	w := projectRuleWiring(t, srv.URL, approver, &e2eSink{})
	host := configHost{w: w}
	original, err := os.ReadFile(w.configPath())
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	w.configWatch = startConfigWatcher(t, w.configPath())

	runE2EExchange(t, ctx, w.engine, "run a command")
	if got := approver.requests(); len(got) != 1 {
		t.Fatalf("approval requests before any rule = %d, want the gate", len(got))
	}

	edited := string(original) + "allow:\n  terminal:\n    - " + projectRuleCommand + "\n"
	writeSettingsFixture(t, w.configPath(), edited)
	awaitAndReload(t, host, "a hand-edited global allow entry")
	approver.reset()
	runE2EExchange(t, ctx, w.engine, "run it again")
	if got := approver.requests(); len(got) != 0 {
		t.Errorf("approval requests after the hand edit = %+v, want none — the global rule applies live", got)
	}

	writeSettingsFixture(t, w.configPath(), string(original))
	awaitAndReload(t, host, "the global allow entry taken out")
	approver.reset()
	runE2EExchange(t, ctx, w.engine, "and once more")
	if got := approver.requests(); len(got) != 1 {
		t.Errorf("approval requests after the entry is gone = %d, want the gate back", len(got))
	}
}

// The adoption-record watcher is one of the run's closers: tearing the session down stops it and
// closes the channel the renderer's wait parks on.
func TestProjectRuleAdoptionWatcherStopsWithTheSession(t *testing.T) {
	t.Parallel()
	watch := filewatch.New(filepath.Join(t.TempDir(), "record.yaml"))
	watch.Start()
	w := &rootWiring{adoptionWatch: watch}

	w.close()

	select {
	case _, ok := <-watch.Changes():
		if ok {
			t.Error("the adoption watcher reported a change at teardown, want its channel closed")
		}
	case <-time.After(configWatchTestDeadline):
		t.Fatal("the adoption watcher is still running after the session closed")
	}
}

// A session watches an adoption record only where there is a Project config to answer for: a run
// that takes no project layer watches none.
func TestProjectRuleAdoptionWatchPathFollowsTheProjectLayer(t *testing.T) {
	t.Parallel()
	home, root := t.TempDir(), t.TempDir()
	roots := stateRoots{config: home, workspace: root, project: root}
	store, err := adoption.New(config.WorkspacesDir(home), root)
	if err != nil {
		t.Fatalf("adoption.New: %v", err)
	}
	if got := adoptionWatchPath(config.Options{ConfigDir: home}, roots); got != store.Path() {
		t.Errorf("adoptionWatchPath = %q, want the root's record %q", got, store.Path())
	}
	if got := adoptionWatchPath(config.Options{ConfigDir: home, GlobalConfigOnly: true}, roots); got != "" {
		t.Errorf("adoptionWatchPath with no project layer = %q, want none", got)
	}
	if got := adoptionWatchPath(config.Options{ConfigDir: home}, stateRoots{config: home}); got != "" {
		t.Errorf("adoptionWatchPath with no Project root = %q, want none", got)
	}
}
