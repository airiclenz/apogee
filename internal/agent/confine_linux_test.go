//go:build linux

package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/skills"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/workflow"
)

// TestMain intercepts the __confined-exec sentinel so this test binary can play the in-child half
// of the landlock re-exec wrapper — the dispatch cmd/apogee's main performs for the product binary
// (confinement-execution-contract §2.3/§6.1), as internal/tools' console_confine_linux_test.go
// does. Without it a live confinement proof in this package would re-exec the test binary as an
// ordinary test run instead of confining anything, because the landlock backend re-execs
// os.Executable().
func TestMain(m *testing.M) {
	if len(os.Args) >= 2 && os.Args[1] == platform.ConfinedExecSentinel() {
		os.Exit(runConfinedExecChild(os.Args[2:]))
	}
	os.Exit(m.Run())
}

// runConfinedExecChild mirrors cmd/apogee's sentinel dispatcher: argv is [<encoded-box>, "--",
// <real argv...>]. On success ApplyLandlockAndExec replaces this process image and never returns.
func runConfinedExecChild(args []string) int {
	if len(args) < 2 || args[1] != "--" {
		fmt.Fprintln(os.Stderr, "confined-exec: malformed argv")
		return 2
	}
	box, err := platform.DecodeConfinedBox(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := platform.ApplyLandlockAndExec(box, args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0 // unreachable on success
}

// livePlanScripts builds a Plan-mode Agent on the host's real Confiner and terminal over a fresh
// workspace, and returns the script runner a recipe's stages use, the workspace and a workflow
// folder under the session scratch dir. It skips where the kernel cannot enforce a box.
func livePlanScripts(t *testing.T, recipe workflow.Recipe) (scripts *recipeScripts, workspace, folder string) {
	t.Helper()
	confiner := platform.NewConfiner()
	if !confiner.Capabilities().FSWrite {
		t.Skip("this host reports no enforceable filesystem confinement; skipping the live proof")
	}
	workspace, scratch := t.TempDir(), t.TempDir()
	folder = filepath.Join(scratch, "workflows", "wf-1")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := baseConfig(&recordingSink{})
	cfg.Mode = domain.ModePlan
	cfg.Confiner = confiner
	cfg.WorkspaceDir = workspace
	cfg.ScratchDir = scratch
	reg := domain.NewToolRegistry()
	_ = reg.Register(tools.NewTerminal(workspace, nil))
	cfg.Tools = reg
	a, err := newAgent(cfg, &workflowResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	return &recipeScripts{agent: a, turn: 1, recipe: recipe}, workspace, folder
}

// TestRecipe_APlanScriptWritesOnlyItsWorkflowFolder is the live proof of the Plan rule (ADR 0012
// amendment 2026-09-27) against the host's real backend: a script stage that writes into its
// workflow folder succeeds, and one that writes into the workspace fails inside the sandbox and
// leaves nothing behind.
func TestRecipe_APlanScriptWritesOnlyItsWorkflowFolder(t *testing.T) {
	t.Parallel()
	scripts, workspace, folder := livePlanScripts(t, scriptRecipe("unused"))
	ctx := context.Background()

	out, err := scripts.RunScript(ctx, workflow.ScriptSpec{
		Workflow: "wf-1", Stage: "inside", Dir: folder,
		Command: "echo kept > {workflow_dir}/note.txt && echo COUNT=1",
	})
	if err != nil || out.ExitCode != 0 || !strings.Contains(out.Stdout, "COUNT=1") {
		t.Fatalf("a write into the workflow folder = %+v, %v; want it to run", out, err)
	}
	if data, err := os.ReadFile(filepath.Join(folder, "note.txt")); err != nil || string(data) != "kept\n" {
		t.Errorf("the workflow folder's file = %q, %v; want the script's write", data, err)
	}

	escape := filepath.Join(workspace, "escape.txt")
	out, err = scripts.RunScript(ctx, workflow.ScriptSpec{
		Workflow: "wf-1", Stage: "outside", Dir: folder,
		Command: "echo leaked > " + shellQuote(escape),
	})
	if err == nil && out.ExitCode == 0 {
		t.Errorf("a write into the workspace ran in Plan mode: %+v", out)
	}
	if _, statErr := os.Stat(escape); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("the workspace write landed (stat err %v); the sandbox must refuse it", statErr)
	}
}

// TestRecipe_TheShippedAuditSplitRunsInPlan runs the shipped audit recipe's split stage, exactly
// as its header spells it, in Plan on a fixture workspace: the stage that used to be refused in
// Plan (ADR 0087 D4, "a read-only audit works in Plan mode") lists the scope into its folder.
func TestRecipe_TheShippedAuditSplitRunsInPlan(t *testing.T) {
	t.Parallel()
	recipe, ok := skills.NewProvider(skills.Sources{UseShippedSkills: true}).Recipe("audit")
	if !ok {
		t.Fatal("the shipped audit recipe is not served")
	}
	var command string
	for _, stage := range recipe.Plan.Stages {
		if stage.Name == "split" {
			command = stage.Run
		}
	}
	if command == "" {
		t.Fatal("the shipped audit recipe has no split stage")
	}
	scripts, workspace, folder := livePlanScripts(t, recipe)
	for name, body := range map[string]string{
		"main.go":      "package main\n\nfunc main() {}\n",
		"main_test.go": "package main\n",
	} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	command = strings.NewReplacer("{scope}", ".", "{focus}", "all").Replace(command)

	out, err := scripts.RunScript(context.Background(), workflow.ScriptSpec{Workflow: "wf-1", Stage: "split", Command: command, Dir: folder})
	if err != nil || out.ExitCode != 0 {
		t.Fatalf("the split stage in Plan = %+v, %v; want it to run", out, err)
	}
	if !strings.Contains(out.Stdout, "files=2") {
		t.Errorf("split receipt = %q, want files=2", out.Stdout)
	}
	if data, err := os.ReadFile(filepath.Join(folder, "scope.txt")); err != nil || string(data) != "main.go\nmain_test.go\n" {
		t.Errorf("scope.txt = %q, %v; want both fixture files", data, err)
	}
}
