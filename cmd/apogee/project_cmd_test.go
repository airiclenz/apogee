package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/muesli/cancelreader"
	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/adoption"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
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

// A yes answers the file as it stands when the yes is typed: a rule a teammate's pull took out while
// its question waited is refused by name and nothing is recorded for it, the next rule is still
// asked about and adopted, and the closing line counts the rule that was gone.
func TestProjectCmdAdoptRefusesARuleTheFileNoLongerHolds(t *testing.T) {
	t.Parallel()
	home, workspace := projectAdoptRoots(t)
	rules := []string{"go test", "make lint"}
	writeProjectAllow(t, workspace, rules...)
	input := &scriptedInput{scripts: [][]string{{"y"}, {"y"}}}
	var once sync.Once
	cmd := newProjectCommandWith(projectAdoptDeps{
		terminal: loginTerminal{openInput: func() (cancelreader.CancelReader, error) {
			once.Do(func() { writeProjectAllow(t, workspace, rules[1]) })
			return input.open()
		}},
		isTerminal: func() bool { return true },
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"adopt", "--config", home, "--workspace", workspace})

	err := cmd.ExecuteContext(context.Background())

	if err != nil {
		t.Fatalf("project adopt: %v\n%s", err, out.String())
	}
	want := []adoption.State{adoption.Proposed, adoption.Adopted}
	got := recordedAnswers(t, home, workspace, rules...)
	for i := range rules {
		if got[i] != want[i] {
			t.Errorf("rule %q is recorded %q, want %q", rules[i], got[i], want[i])
		}
	}
	if !strings.Contains(out.String(), "no longer holds `go test`, so it was not adopted.") {
		t.Errorf("the refusal does not name the rule the file lost:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Adopted 1, rejected 0, left 0 proposed, 1 no longer in the file.") {
		t.Errorf("the closing line does not count the rule that was gone:\n%s", out.String())
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

// The daemon's startup resolution reads no Project config, so a proposed rule is not announced.
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

// ----------------------------------------------------------------------------
// Repo servers and reactions, asked about and fired (ADR 0104 §12)
// ----------------------------------------------------------------------------

// projectFmtReactionYAML is a Project config proposing one reaction whose argv runs past the
// adoption pane's eight-line bound on a value, so a body printed whole is told from a bounded one.
const projectFmtReactionYAML = "reactions:\n  - id: fmt\n    on: [file-changed]\n" +
	"    run: [notify-send, a1, a2, a3, a4, a5, a6, a7, a8, a9, a10]\n"

// projectTwoReactionsYAML is a Project config proposing two observe-lane reactions on the exchange
// boundary, which a test adopts one of.
const projectTwoReactionsYAML = "reactions:\n" +
	"  - id: notify\n    on: [exchange-finished]\n    run: [notify-send, saved]\n" +
	"  - id: lint\n    on: [exchange-finished]\n    run: [notify-send, linted]\n"

// adoptProposalsNamed records as adopted each entry the layered config of the apogee home and
// projectRoot proposes whose handle is among handles — what `apogee project adopt` writes for a
// yes — and fails the test when one of handles is not proposed.
func adoptProposalsNamed(t *testing.T, home, projectRoot string, handles ...string) {
	t.Helper()
	o, err := loadLayeredOptions(config.FilePath(home), projectRoot)
	if err != nil {
		t.Fatalf("load the layered config: %v", err)
	}
	store, err := adoption.New(config.WorkspacesDir(home), projectRoot)
	if err != nil {
		t.Fatalf("open the adoption record: %v", err)
	}
	for _, handle := range handles {
		i := slices.IndexFunc(o.Proposals.Proposed, func(p apogee.Proposal) bool { return p.Handle == handle })
		if i < 0 {
			t.Fatalf("the Project config proposes no entry %q: %+v", handle, o.Proposals.Proposed)
		}
		p := o.Proposals.Proposed[i]
		if err := store.Adopt(adoption.Entry{Kind: string(p.Kind), Handle: p.Handle, Text: p.Text}); err != nil {
			t.Fatalf("Adopt %s: %v", handle, err)
		}
	}
}

// layeredProposals is what the layered config of the apogee home and projectRoot sorts the
// Project config's servers and reactions into now.
func layeredProposals(t *testing.T, home, projectRoot string) config.ProjectProposals {
	t.Helper()
	o, err := loadLayeredOptions(config.FilePath(home), projectRoot)
	if err != nil {
		t.Fatalf("load the layered config: %v", err)
	}
	return o.Proposals
}

// projectRunRecorder stands in for the fenced runner a Firing's Agent binds: it records the id of
// every repo reaction whose `run:` reached it, and runs nothing. Reaction workers call it from
// their own goroutines.
type projectRunRecorder struct {
	mu  sync.Mutex
	ids []string
}

// run is the domain.ProjectRun the recorder is bound as.
func (r *projectRunRecorder) run(_ context.Context, reaction domain.Reaction, _ string, _ []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if reaction.Project {
		r.ids = append(r.ids, reaction.ID)
	}
	return nil
}

// fired is the ids recorded so far, in order.
func (r *projectRunRecorder) fired() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.ids)
}

// A project proposing only a reaction is asked about with the adoption pane's title and its body
// printed whole — every argv element, none elided — and a yes adopts the reaction as it was offered.
func TestProjectCmdAdoptsARepoReaction(t *testing.T) {
	t.Parallel()
	home, workspace := projectAdoptRoots(t)
	writeProjectConfig(t, workspace, projectFmtReactionYAML)

	out, err := runProjectAdopt(t, home, workspace, true, "y")

	if err != nil {
		t.Fatalf("project adopt: %v\n%s", err, out)
	}
	for _, want := range []string{
		"proposes 1 entry:",
		"  use reaction `fmt` in this project?\n",
		"\n    run:\n      - notify-send\n      - a1\n",
		"      - a10\n  adopt? [y]es / [n]ot now / [r]eject: ",
		"Adopted 1, rejected 0, left 0 proposed.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "more lines") {
		t.Errorf("the body was bounded; a terminal prints it whole:\n%s", out)
	}
	adopted := layeredProposals(t, home, workspace).Adopted
	if len(adopted) != 1 || adopted[0].Kind != apogee.ProposalReaction || adopted[0].Handle != "fmt" {
		t.Errorf("adopted after the yes = %+v, want the reaction fmt", adopted)
	}
}

// `apogee project adopt` asks rather than skips, so it never says the unattended Drivers' skipped
// reaction line, even when the reaction is left proposed.
func TestProjectCmdPrintsNoSkippedReactionNotice(t *testing.T) {
	t.Parallel()
	home, workspace := projectAdoptRoots(t)
	writeProjectConfig(t, workspace, projectFmtReactionYAML)
	skipped := skippedReactionsNotice(workspace, layeredProposals(t, home, workspace))
	if skipped == "" {
		t.Fatal("a proposed reaction composes no skipped notice; the test has nothing to look for")
	}

	out, err := runProjectAdopt(t, home, workspace, true, "n")

	if err != nil {
		t.Fatalf("project adopt: %v\n%s", err, out)
	}
	if strings.Contains(out, skipped) || strings.Contains(out, "this run skips") {
		t.Errorf("project adopt said the skipped-reaction line:\n%s", out)
	}
	if proposed := layeredProposals(t, home, workspace).Proposed; len(proposed) != 1 {
		t.Errorf("proposed after not now = %+v, want the reaction still proposed", proposed)
	}
}

// A headless run fires the adopted repo reaction through its Agent's fenced runner, skips the one
// still proposed — off the lanes, named on one stderr line — and asks nothing.
func TestHeadlessFiresOnlyAdoptedRepoReactions(t *testing.T) {
	recorder := &projectRunRecorder{}
	stub := &stubRunner{
		res:        run.Result{SessionID: "s-1", FinalText: "done", Turns: 1},
		projectRun: recorder.run,
		emit: func(sink domain.EventSink) {
			sink.Emit(domain.TurnEvent{Status: domain.StatusExchangeComplete})
		},
	}
	t.Setenv(config.EnvMode, "")
	configDir, workspace := testConfigHomeOn(t, headlessBeatServer(t), ""), t.TempDir()
	writeProjectConfig(t, workspace, projectTwoReactionsYAML)
	adoptProposalsNamed(t, configDir, workspace, "notify")

	cmd := newHeadlessCommandWith(headlessDeps{runner: stub.once})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"--config", configDir, "--workspace", workspace, "a prompt"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("headless: %v\n%s", err, errOut.String())
	}

	if got := recorder.fired(); !slices.Equal(got, []string{"notify"}) {
		t.Errorf("repo reactions fired = %v, want only the adopted notify", got)
	}
	observe := stub.spec.Generation.Observe
	if len(observe) != 1 || observe[0].ID != "notify" || !observe[0].Project {
		t.Errorf("observe lane = %+v, want the adopted repo reaction notify alone", observe)
	}
	if got := strings.Count(errOut.String(), "this run skips"); got != 1 {
		t.Errorf("the skipped-reaction line was said %d times, want once:\n%s", got, errOut.String())
	}
	if !strings.Contains(errOut.String(), "proposes reaction `lint`; this run skips it") ||
		strings.Contains(errOut.String(), "reaction `notify`") {
		t.Errorf("the skipped-reaction line does not name lint alone:\n%s", errOut.String())
	}
}
