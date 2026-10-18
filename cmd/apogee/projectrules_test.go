package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/adoption"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/filewatch"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/testtime"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/tui"
)

// The four Allow-rule acts of the config host (projectrules.go, ADR 0096 §4), driven the way the
// approval pane and the adoption pane drive them: through configHost, over a wiring whose engine is
// a real Agent in ask-before on a scripted upstream, so "the next identical call runs without
// asking" is the loop's own behaviour rather than a spy's.

// projectRuleCommand is the word prefix the tests save: the first word of the scripted command.
const projectRuleCommand = "echo"

// echoRule is the project rule projectRuleCommand is saved as.
var echoRule = apogee.AllowRule{Kind: apogee.AllowRuleTerminal, Text: projectRuleCommand, Layer: apogee.AllowRuleProject}

// proposalOfRule is rule as ProposedRules reports it: a Proposal carrying the fingerprint its answer
// is recorded under.
func proposalOfRule(rule apogee.AllowRule) apogee.Proposal {
	p := rule.Proposal()
	p.Fingerprint = adoption.Fingerprint(adoption.Entry{Kind: string(rule.Kind), Text: rule.Text})
	return p
}

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
	if err := host.RejectRules([]apogee.Proposal{echoRule.Proposal()}); err != nil {
		t.Fatalf("RejectRules: %v", err)
	}
	assertRuleRecord(t, w, true, adoption.Rejected)
	assertNoReloadedKey(t, host)
	runE2EExchange(t, ctx, w.engine, "run a command")
	if got := approver.requests(); len(got) != 1 {
		t.Errorf("approval requests with the rule rejected = %d, want the gate", len(got))
	}

	if err := host.AdoptRules([]apogee.Proposal{echoRule.Proposal()}); err != nil {
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
	if want := []apogee.Proposal{proposalOfRule(echoRule)}; !reflect.DeepEqual(proposed, want) {
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

// An adoption answers the file as it stands, not as it was offered: a proposed rule an edit took out
// of the Project config while the pane waited is refused by name and stays unanswered, and a rule
// the file still holds, answered in the same breath, is adopted all the same.
func TestProjectRuleAdoptRefusesARuleTheFileNoLongerHolds(t *testing.T) {
	t.Parallel()
	srv := scriptedTerminalModel(t)
	w := projectRuleWiring(t, srv.URL, &e2eApprover{}, &e2eSink{})
	host := configHost{w: w}
	const keptText = "make lint"
	kept := apogee.AllowRule{Kind: apogee.AllowRuleTerminal, Text: keptText, Layer: apogee.AllowRuleProject}
	writeProjectAllow(t, w.roots.project, projectRuleCommand, keptText)
	proposed, err := host.ProposedRules()
	if err != nil {
		t.Fatalf("ProposedRules: %v", err)
	}
	if want := []apogee.Proposal{proposalOfRule(echoRule), proposalOfRule(kept)}; !reflect.DeepEqual(proposed, want) {
		t.Fatalf("ProposedRules = %+v, want %+v", proposed, want)
	}
	writeProjectAllow(t, w.roots.project, keptText)

	err = host.AdoptRules(proposed)

	if err == nil || !strings.Contains(err.Error(), "no longer holds the terminal rule `"+projectRuleCommand+"`") {
		t.Errorf("AdoptRules after the rule left the file = %v, want the refusal naming it", err)
	}
	assertRuleRecord(t, w, false, adoption.Proposed)
	if got := recordedAnswers(t, w.roots.config, w.roots.project, keptText); got[0] != adoption.Adopted {
		t.Errorf("the rule the file still holds is recorded %q, want %q", got[0], adoption.Adopted)
	}
}

// A global config that no longer resolves cannot stop an adoption the Project config backs: the
// re-check reads the project file alone, so the yes is recorded, and only the settling that would
// install it reports the broken global file.
func TestProjectRuleAdoptRecordsAHeldRuleWhenTheGlobalConfigDoesNotResolve(t *testing.T) {
	t.Parallel()
	srv := scriptedTerminalModel(t)
	w := projectRuleWiring(t, srv.URL, &e2eApprover{}, &e2eSink{})
	host := configHost{w: w}
	writeProjectAllow(t, w.roots.project, projectRuleCommand)
	if err := os.WriteFile(w.configPath(), []byte("model: [unclosed\n"), 0o600); err != nil {
		t.Fatalf("break the global config: %v", err)
	}

	err := host.AdoptRules([]apogee.Proposal{echoRule.Proposal()})

	if err == nil || !strings.Contains(err.Error(), "the answer is saved") {
		t.Errorf("AdoptRules over a broken global config = %v, want the settling's error", err)
	}
	assertRuleRecord(t, w, true, adoption.Adopted)
}

// Adoption is a question only about what the Project config proposes: a proposal of no kind it
// proposes — no Allow list, no `mcp-servers:` or `reactions:` entry — is refused by both answers,
// and nothing is written.
func TestProjectRuleActsRefuseAProposalOfNoKnownKind(t *testing.T) {
	t.Parallel()
	srv := scriptedTerminalModel(t)
	w := projectRuleWiring(t, srv.URL, &e2eApprover{}, &e2eSink{})
	host := configHost{w: w}
	stray := apogee.Proposal{Kind: "servers", Text: "make"}

	if err := host.AdoptRules([]apogee.Proposal{stray}); err == nil {
		t.Error("AdoptRules(stray) succeeded, want a refusal")
	}
	if err := host.RejectRules([]apogee.Proposal{stray}); err == nil {
		t.Error("RejectRules(stray) succeeded, want a refusal")
	}
	if _, err := os.Stat(config.ProjectFilePath(w.roots.project)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the project config exists after refused acts (stat: %v)", err)
	}
}

// globalRule is the global rule the tests below hand-write into the global config.
var globalRule = apogee.AllowRule{Kind: apogee.AllowRuleTerminal, Text: "make", Layer: apogee.AllowRuleGlobal}

// writeGlobalRule appends globalRule to the wiring's global config and settles, as a session started
// on that file would hold it: in force, and in the external-edit baseline.
func writeGlobalRule(t *testing.T, w *rootWiring) {
	t.Helper()
	f, err := os.OpenFile(w.configPath(), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open the global config: %v", err)
	}
	_, err = f.WriteString("allow:\n  terminal:\n    - " + globalRule.Text + "\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		t.Fatalf("write the global rule: %v", err)
	}
	if err := w.settleAllowRules(); err != nil {
		t.Fatalf("settleAllowRules: %v", err)
	}
}

// ruleRows is the *Allow rules* section of rows, keyed by row path, as "<state> <source>".
func ruleRows(rows []tui.SettingRow) map[string]string {
	out := map[string]string{}
	for _, r := range rows {
		if r.Kind == tui.SettingRule {
			out[r.Path] = string(r.RuleState) + " " + string(r.Source)
		}
	}
	return out
}

// Removing a global rule — the /settings section's removal — takes its line out of the global config
// and the rule out of the live holder, with apogee's own write kept out of the watcher's report.
func TestProjectRuleRemoveTakesAGlobalRuleOutOfTheGlobalConfig(t *testing.T) {
	t.Parallel()
	srv := scriptedTerminalModel(t)
	w := projectRuleWiring(t, srv.URL, &e2eApprover{}, &e2eSink{})
	host := configHost{w: w}
	writeGlobalRule(t, w)
	if got := w.live.allowRules().Rules; len(got) != 1 || got[0].Text != globalRule.Text {
		t.Fatalf("live rules before the removal = %+v, want the global rule", got)
	}

	if err := host.RemoveRule(globalRule); err != nil {
		t.Fatalf("RemoveRule(global): %v", err)
	}

	data, err := os.ReadFile(w.configPath())
	if err != nil {
		t.Fatalf("read the global config: %v", err)
	}
	if strings.Contains(string(data), "- "+globalRule.Text) {
		t.Errorf("the global config still lists the rule:\n%s", data)
	}
	if !strings.Contains(string(data), "server: probe-target") {
		t.Errorf("the removal changed more than the rule:\n%s", data)
	}
	if got := w.live.allowRules().Rules; len(got) != 0 {
		t.Errorf("live rules after the removal = %+v, want none", got)
	}
	assertNoReloadedKey(t, host)
}

// The /settings section lists from the live holder: a rule added in the approval pane mid-session
// shows at once, a proposed one is adopted from its row's rule, and neither needs the files — the
// section still lists both once the Project config is gone from disk.
func TestProjectRuleSettingsSectionListsFromTheLiveHolder(t *testing.T) {
	t.Parallel()
	srv := scriptedTerminalModel(t)
	w := projectRuleWiring(t, srv.URL, &e2eApprover{}, &e2eSink{})
	host := configHost{w: w}
	settings := settingsHost{allowRules: w.live.allowRules}
	writeGlobalRule(t, w)
	path := config.ProjectFilePath(w.roots.project)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("allow:\n  terminal:\n    - ls\n"), 0o644); err != nil {
		t.Fatalf("write the project config: %v", err)
	}
	if err := w.settleAllowRules(); err != nil {
		t.Fatalf("settleAllowRules: %v", err)
	}

	if err := host.AddProjectRule(apogee.AllowRuleTerminal, projectRuleCommand); err != nil {
		t.Fatalf("AddProjectRule: %v", err)
	}
	want := map[string]string{
		"terminal: make": "live global",
		"terminal: ls":   "proposed project",
		"terminal: echo": "adopted project",
	}
	rows := settings.Rows()
	if got := ruleRows(rows); !reflect.DeepEqual(got, want) {
		t.Fatalf("the section after the approval pane's add = %v, want %v", got, want)
	}

	var proposed tui.SettingRow
	for _, r := range rows {
		if r.Path == "terminal: ls" {
			proposed = r
		}
	}
	if err := host.AdoptRules([]apogee.Proposal{proposed.Rule.Proposal()}); err != nil {
		t.Fatalf("AdoptRules from the section's row: %v", err)
	}
	want["terminal: ls"] = "adopted project"
	if got := ruleRows(settings.Rows()); !reflect.DeepEqual(got, want) {
		t.Errorf("the section after the adoption = %v, want %v", got, want)
	}
	inForce := false
	for _, r := range w.live.allowRules().Rules {
		inForce = inForce || r.Text == "ls"
	}
	if !inForce {
		t.Errorf("the adopted rule is not in force: %+v", w.live.allowRules().Rules)
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove the project config: %v", err)
	}
	if got := ruleRows(settings.Rows()); !reflect.DeepEqual(got, want) {
		t.Errorf("the section with the project config gone = %v, want %v — Rows reads no disk", got, want)
	}
}

// The section's rows come after every registry row, under their own header, so the registry's order
// and sections stand as they are — the repo entries' rows spliced in after the `mcp-servers` and
// `reactions` keys only; a session with no rules has no section at all.
func TestSettingsHostRowsAppendTheAllowRulesSection(t *testing.T) {
	t.Parallel()
	set := config.AllowRules{
		Rules: []config.AllowRule{
			{Kind: config.AllowTerminal, Text: "make", Layer: config.SourceGlobal},
			{Kind: config.AllowMCPServers, Text: "github", Layer: config.SourceProject},
		},
		Proposed: []config.AllowRule{{Kind: config.AllowTerminal, Text: "ls", Layer: config.SourceProject}},
		Rejected: []config.AllowRule{{Kind: config.AllowTerminal, Text: "rm", Layer: config.SourceProject}},
	}
	proposals := config.ProjectProposals{
		Adopted:  []apogee.Proposal{{Kind: apogee.ProposalMCPServer, Handle: "wiki"}},
		Proposed: []apogee.Proposal{{Kind: apogee.ProposalReaction, Handle: "fmt"}},
	}
	host := settingsHost{
		allowRules: func() config.AllowRules { return set },
		proposals:  func() config.ProjectProposals { return proposals },
	}
	rows := host.Rows()
	registry := settingsRows(config.Options{})
	if len(rows) != len(registry)+2+4 {
		t.Fatalf("rows = %d, want the %d registry rows, 2 repo entries and 4 rules", len(rows), len(registry))
	}
	var keys []string
	for _, r := range rows[:len(registry)+2] {
		if r.Kind != tui.SettingProposal {
			keys = append(keys, r.Path)
		}
	}
	for i, r := range registry {
		if keys[i] != r.Path {
			t.Fatalf("registry row %d = %q, want the registry's %q — the rules come after it", i, keys[i], r.Path)
		}
	}
	for _, spliced := range []struct{ after, path string }{
		{"mcp-servers", "mcp-servers: wiki"}, {"reactions", "reactions: fmt"},
	} {
		at := slices.IndexFunc(rows, func(r tui.SettingRow) bool { return r.Path == spliced.after })
		if at < 0 || at+1 >= len(rows) {
			t.Fatalf("no row follows %q", spliced.after)
		}
		if next := rows[at+1]; next.Path != spliced.path || next.Section != rows[at].Section {
			t.Errorf("the row after %q = %+v, want %q in its section %q", spliced.after, next, spliced.path, rows[at].Section)
		}
	}
	type ruleCell struct {
		path   string
		state  tui.SettingRuleState
		source tui.SettingSource
		rule   apogee.AllowRule
	}
	want := []ruleCell{
		{"terminal: make", tui.SettingRuleLive, tui.SettingFromGlobal,
			apogee.AllowRule{Kind: apogee.AllowRuleTerminal, Text: "make", Layer: apogee.AllowRuleGlobal}},
		{"mcp-servers: github", tui.SettingRuleAdopted, tui.SettingFromProject,
			apogee.AllowRule{Kind: apogee.AllowRuleMCPServer, Text: "github", Layer: apogee.AllowRuleProject}},
		{"terminal: ls", tui.SettingRuleProposed, tui.SettingFromProject,
			apogee.AllowRule{Kind: apogee.AllowRuleTerminal, Text: "ls", Layer: apogee.AllowRuleProject}},
		{"terminal: rm", tui.SettingRuleRejected, tui.SettingFromProject,
			apogee.AllowRule{Kind: apogee.AllowRuleTerminal, Text: "rm", Layer: apogee.AllowRuleProject}},
	}
	for i, w := range want {
		r := rows[len(registry)+2+i]
		got := ruleCell{r.Path, r.RuleState, r.Source, r.Rule}
		if got != w || r.Kind != tui.SettingRule || r.Section != allowRulesSection || r.Value != string(w.state) {
			t.Errorf("rule row %d = %+v, want %+v under %q", i, r, w, allowRulesSection)
		}
		if r.Desc == "" {
			t.Errorf("rule row %d has no description", i)
		}
	}

	if got := len((settingsHost{
		allowRules: func() config.AllowRules { return config.AllowRules{} },
		proposals:  func() config.ProjectProposals { return config.ProjectProposals{} },
	}).Rows()); got != len(registry) {
		t.Errorf("rows with no rules = %d, want the %d registry rows alone", got, len(registry))
	}
}

// The `/settings` row of a repo reaction follows the file and the answers recorded through
// configHost — the adoption pane's acts — read off the live holder: proposed once a watched re-read
// finds the file stating it (a re-read that moves no registry row), adopted once adopted, tagged
// project and read-only throughout, and still listed once the Project config is gone from disk,
// because Rows reads no disk.
func TestSettingsHostRepoReactionRowFollowsAnAdopt(t *testing.T) {
	t.Parallel()
	srv := scriptedTerminalModel(t)
	w := projectRuleWiring(t, srv.URL, &e2eApprover{}, &e2eSink{})
	host := configHost{w: w}
	settings := settingsHost{allowRules: w.live.allowRules, proposals: w.live.proposals}
	writeProjectConfig(t, w.roots.project, projectNotifyReactionYAML)
	if _, err := host.ReloadConfig(); err != nil {
		t.Fatalf("ReloadConfig: %v", err)
	}

	row := repoRowOf(t, settings.Rows(), "reactions: notify")
	if row.RuleState != tui.SettingRuleProposed || row.Kind != tui.SettingProposal ||
		row.Source != tui.SettingFromProject || row.Editable || row.ExternalEdit {
		t.Fatalf("the reaction's row before any answer = %+v, want proposed, project, read-only", row)
	}
	if err := host.AdoptRules([]apogee.Proposal{row.Proposal}); err != nil {
		t.Fatalf("AdoptRules from the row's proposal: %v", err)
	}

	row = repoRowOf(t, settings.Rows(), "reactions: notify")
	if row.RuleState != tui.SettingRuleAdopted || row.Value != string(tui.SettingRuleAdopted) {
		t.Errorf("the reaction's row after the adoption = %+v, want adopted", row)
	}
	if err := os.Remove(config.ProjectFilePath(w.roots.project)); err != nil {
		t.Fatalf("remove the project config: %v", err)
	}
	if got := repoRowOf(t, settings.Rows(), "reactions: notify"); got.RuleState != tui.SettingRuleAdopted {
		t.Errorf("the row with the project config gone = %+v, want it unchanged — Rows reads no disk", got)
	}
}

// repoRowOf is the row keyed path among rows, failing the test when there is none.
func repoRowOf(t *testing.T, rows []tui.SettingRow, path string) tui.SettingRow {
	t.Helper()
	i := slices.IndexFunc(rows, func(r tui.SettingRow) bool { return r.Path == path })
	if i < 0 {
		t.Fatalf("no %q row among the settings rows", path)
	}
	return rows[i]
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
	case <-time.After(testtime.Budget(30 * time.Second)):
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
	testtime.Eventually(t, testtime.Budget(30*time.Second), func() bool {
		for _, req := range srv.Requests() {
			if n := len(req.Messages); n > 0 && req.Messages[n-1].Content == projectRuleChildTask {
				return true
			}
		}
		return false
	}, "the child's request to reach the upstream")
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

// ----------------------------------------------------------------------------
// Repo MCP servers and reactions through the adoption acts (ADR 0104 §11)
// ----------------------------------------------------------------------------

// writeProjectConfig writes body as the Project config of root.
func writeProjectConfig(t *testing.T, root, body string) {
	t.Helper()
	path := config.ProjectFilePath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create the project's .apogee: %v", err)
	}
	writeSettingsFixture(t, path, body)
}

// proposedOnly is host's one proposal, failing the test unless there is exactly one, of kind.
func proposedOnly(t *testing.T, host configHost, kind apogee.ProposalKind) []apogee.Proposal {
	t.Helper()
	proposed, err := host.ProposedRules()
	if err != nil {
		t.Fatalf("ProposedRules: %v", err)
	}
	if len(proposed) != 1 || proposed[0].Kind != kind {
		t.Fatalf("ProposedRules = %+v, want the one %s entry", proposed, kind)
	}
	return proposed
}

// A repo server is adopted only as the file holds it at the answer: an edit after the pane offered
// it refuses the yes by name and dials nothing, and the entry as it stands now, adopted, is dialled
// at once — marked as the project's — with no watcher report needed, since apogee's own record
// write re-takes the baseline.
func TestProjectRuleAdoptsARepoServerTheFileHolds(t *testing.T) {
	t.Parallel()
	srv := scriptedTerminalModel(t)
	w := projectRuleWiring(t, srv.URL, &e2eApprover{}, &e2eSink{})
	var dialled [][]mcp.ServerConfig
	fixture := newMCPFixture(&fakeMCPSession{}, "", func(servers []mcp.ServerConfig) (mcpSession, error) {
		dialled = append(dialled, servers)
		return &fakeMCPSession{}, nil
	})
	w.mcpSet, w.toolSet = fixture.set, fixture.tools
	host := configHost{w: w}
	writeProjectConfig(t, w.roots.project, projectWikiServerYAML)
	stale := proposedOnly(t, host, apogee.ProposalMCPServer)
	writeProjectConfig(t, w.roots.project, strings.Replace(projectWikiServerYAML, "wiki.example.com", "wiki.example.org", 1))

	err := host.AdoptRules(stale)

	if err == nil || !strings.Contains(err.Error(), "no longer holds the mcp-server entry `wiki` as it was offered") {
		t.Fatalf("AdoptRules of the stale offer = %v, want the refusal naming the entry", err)
	}
	if len(dialled) != 0 {
		t.Fatalf("a refused adoption dialled %+v", dialled)
	}

	if err := host.AdoptRules(proposedOnly(t, host, apogee.ProposalMCPServer)); err != nil {
		t.Fatalf("AdoptRules: %v", err)
	}
	if len(dialled) != 1 {
		t.Fatalf("dialled %d times, want the one reconnect the adoption drives", len(dialled))
	}
	i := slices.IndexFunc(dialled[0], func(s mcp.ServerConfig) bool { return s.Name == "wiki" })
	if i < 0 || !dialled[0][i].Project || dialled[0][i].Endpoint != "https://wiki.example.org/mcp" {
		t.Errorf("dialled %+v, want the adopted repo wiki server as the file holds it", dialled[0])
	}
	if after, err := host.ProposedRules(); err != nil || len(after) != 0 {
		t.Errorf("ProposedRules after adopting = %+v, %v; want none", after, err)
	}
	assertNoReloadedKey(t, host)
}

// A repo reaction the adoption pane adopts is on the running engine's lanes at once — marked as the
// project's — so it fires from the next Moment on, with no restart and no watcher report.
func TestProjectRuleAdoptArmsTheRepoReaction(t *testing.T) {
	t.Parallel()
	srv := scriptedTerminalModel(t)
	w := projectRuleWiring(t, srv.URL, &e2eApprover{}, &e2eSink{})
	host := configHost{w: w}
	writeProjectConfig(t, w.roots.project, projectNotifyReactionYAML)

	if err := host.AdoptRules(proposedOnly(t, host, apogee.ProposalReaction)); err != nil {
		t.Fatalf("AdoptRules: %v", err)
	}

	w.engine.mu.Lock()
	gen := w.engine.pendingGeneration
	w.engine.mu.Unlock()
	if gen == nil {
		t.Fatal("the engine holds no generation after the adoption, want the layered reactions swapped in")
	}
	if observe := gen.Observe; len(observe) != 1 || observe[0].ID != "notify" || !observe[0].Project {
		t.Errorf("observe lane = %+v, want the adopted repo reaction notify", observe)
	}
	assertNoReloadedKey(t, host)
}
