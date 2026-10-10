package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee/internal/adoption"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/run"
)

// `apogee project adopt` (project_cmd.go, ADR 0096 §4): the terminal twin of the adoption pane,
// driven over a scripted terminal and real files — a temporary apogee home and a workspace that is
// its own Project root — so what each answer leaves behind is read back from the adoption record.

// writeProjectAllow writes the Project config of root with terminal rules under `allow:`.
func writeProjectAllow(t *testing.T, root string, rules ...string) {
	t.Helper()
	path := config.ProjectFilePath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create the project's .apogee: %v", err)
	}
	body := "allow:\n  terminal:\n"
	for _, r := range rules {
		body += "    - " + r + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write the project config: %v", err)
	}
}

// projectAdoptRoots is a hermetic apogee home with a config naming no server, and a workspace
// outside any repository, which is its own Project root.
func projectAdoptRoots(t *testing.T) (home, workspace string) {
	t.Helper()
	home, workspace = t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("auto-title: true\n"), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	return home, workspace
}

// runProjectAdopt executes `apogee project adopt` over the roots, with a terminal that types the
// scripted answers one per question, and returns what it printed.
func runProjectAdopt(t *testing.T, home, workspace string, isTerminal bool, answers ...string) (string, error) {
	t.Helper()
	input := &scriptedInput{}
	for _, a := range answers {
		input.scripts = append(input.scripts, []string{a})
	}
	cmd := newProjectCommandWith(projectAdoptDeps{
		terminal:   loginTerminal{openInput: input.open},
		isTerminal: func() bool { return isTerminal },
	})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"adopt", "--config", home, "--workspace", workspace})
	err := cmd.ExecuteContext(context.Background())
	return out.String() + errOut.String(), err
}

// recordedAnswers is the adoption record's answer for each terminal rule, in the order given.
func recordedAnswers(t *testing.T, home, workspace string, rules ...string) []adoption.State {
	t.Helper()
	store, err := adoption.New(config.WorkspacesDir(home), workspace)
	if err != nil {
		t.Fatalf("adoption.New: %v", err)
	}
	states := make([]adoption.State, len(rules))
	for i, r := range rules {
		sorted, err := store.Classify([]adoption.Entry{{Kind: string(config.AllowTerminal), Text: r}})
		if err != nil {
			t.Fatalf("Classify: %v", err)
		}
		states[i] = adoption.Proposed
		switch {
		case len(sorted.Adopted) == 1:
			states[i] = adoption.Adopted
		case len(sorted.Rejected) == 1:
			states[i] = adoption.Rejected
		}
	}
	return states
}

// Each proposed rule is asked about in file order: y adopts it, n leaves it proposed, r rejects it,
// and the closing line counts what the answers did.
func TestProjectCmdAdoptAnswersEachProposedRule(t *testing.T) {
	t.Parallel()
	home, workspace := projectAdoptRoots(t)
	rules := []string{"go test", "make lint", "npm run build"}
	writeProjectAllow(t, workspace, rules...)

	out, err := runProjectAdopt(t, home, workspace, true, "y", "n", "r")
	if err != nil {
		t.Fatalf("project adopt: %v\n%s", err, out)
	}

	want := []adoption.State{adoption.Adopted, adoption.Proposed, adoption.Rejected}
	got := recordedAnswers(t, home, workspace, rules...)
	for i := range rules {
		if got[i] != want[i] {
			t.Errorf("rule %q is recorded %q, want %q", rules[i], got[i], want[i])
		}
	}
	for _, r := range rules {
		if !strings.Contains(out, "`"+r+"`") {
			t.Errorf("the questions do not name the rule %q:\n%s", r, out)
		}
	}
	if !strings.Contains(out, "Adopted 1, rejected 1, left 1 proposed.") {
		t.Errorf("the closing line does not count the answers:\n%s", out)
	}

	// The rule left proposed is the only one asked about again; the answered ones stay answered.
	again, err := runProjectAdopt(t, home, workspace, true, "")
	if err != nil {
		t.Fatalf("second project adopt: %v\n%s", err, again)
	}
	if !strings.Contains(again, "`make lint`") || strings.Contains(again, "`go test`") ||
		strings.Contains(again, "`npm run build`") {
		t.Errorf("the second run asked about other than the one proposed rule:\n%s", again)
	}
}

// With stdin not a terminal there is nobody to ask: the command refuses with the instruction, and
// nothing is recorded.
func TestProjectCmdAdoptRefusesANonTerminal(t *testing.T) {
	t.Parallel()
	home, workspace := projectAdoptRoots(t)
	writeProjectAllow(t, workspace, "go test")

	out, err := runProjectAdopt(t, home, workspace, false)
	if err == nil || !strings.Contains(err.Error(), "run `apogee project adopt` from a terminal") {
		t.Fatalf("project adopt without a terminal = %v, want the refusal naming the command\n%s", err, out)
	}
	if got := recordedAnswers(t, home, workspace, "go test"); got[0] != adoption.Proposed {
		t.Errorf("the refused run recorded %q for the rule, want it still proposed", got[0])
	}
}

// A Project config with nothing waiting on an answer is reported as such, terminal or not.
func TestProjectCmdAdoptReportsNothingToAnswer(t *testing.T) {
	t.Parallel()
	home, workspace := projectAdoptRoots(t)
	writeProjectAllow(t, workspace, "go test")
	if _, err := runProjectAdopt(t, home, workspace, true, "y"); err != nil {
		t.Fatalf("project adopt: %v", err)
	}

	out, err := runProjectAdopt(t, home, workspace, false)
	if err != nil {
		t.Fatalf("project adopt with nothing proposed: %v", err)
	}
	if !strings.Contains(out, "proposes no rule that is waiting on an answer") {
		t.Errorf("output = %q, want the nothing-to-answer line", out)
	}
}

// The shipped binary registers `project` with its `adopt` verb.
func TestSubcommandsRegistersProjectCmd(t *testing.T) {
	t.Parallel()
	root := newRootCommand((&recordingLauncher{}).launch, subcommands()...)

	var project *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "project" {
			project = c
		}
	}
	if project == nil {
		t.Fatal("the shipped subcommand set does not register `project`")
	}
	hasAdopt := false
	for _, c := range project.Commands() {
		hasAdopt = hasAdopt || c.Name() == "adopt"
	}
	if !hasAdopt {
		t.Error("`project` does not register the `adopt` child")
	}
}

// assertNoAdoptionNotice fails when an unattended Driver's output says anything about adopting or
// proposed rules: it has nobody to answer, so it stays silent and the rules stay inert.
func assertNoAdoptionNotice(t *testing.T, who, output string) {
	t.Helper()
	lower := strings.ToLower(output)
	if strings.Contains(lower, "adopt") || strings.Contains(lower, "propos") {
		t.Errorf("%s spoke about adoption; output:\n%s", who, output)
	}
}

// A headless run over a Project config that proposes a rule says nothing about it, and the rule is
// not among the rules its engine is handed.
func TestProjectCmdHeadlessIsSilentAboutProposedRules(t *testing.T) {
	stub := &stubRunner{res: run.Result{SessionID: "s-1", FinalText: "done", Turns: 1}}
	t.Setenv(config.EnvMode, "")
	configDir, workspace := testConfigHomeOn(t, headlessBeatServer(t), ""), t.TempDir()
	writeProjectAllow(t, workspace, "go test")

	cmd := newHeadlessCommandWith(headlessDeps{runner: stub.once})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"--config", configDir, "--workspace", workspace, "a prompt"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("headless: %v\n%s", err, errOut.String())
	}

	assertNoAdoptionNotice(t, "headless", out.String()+errOut.String())
	if rules := stub.spec.Config.AllowRules; len(rules) != 0 {
		t.Errorf("headless handed the engine %+v; a proposed rule grants nothing", rules)
	}
}

// The daemon reads no Project config at all, so a proposed rule is neither announced nor read.
func TestProjectCmdDaemonIsSilentAboutProposedRules(t *testing.T) {
	workspace := t.TempDir()
	writeProjectAllow(t, workspace, "go test")
	t.Setenv(config.EnvWorkspace, workspace)
	h := newDaemonHarness(t)

	h.stop()
	wait := h.run(t)
	if err := wait(); err != nil {
		t.Fatalf("daemon: %v\n%s", err, h.errOut.String())
	}
	assertNoAdoptionNotice(t, "the daemon", h.errOut.String())
}
