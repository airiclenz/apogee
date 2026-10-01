package agent

import (
	"fmt"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/workflow"
)

// filesRecipe is a one-fanout recipe id over the workspace's `*.txt` files, so a file added after a
// run moves the plan's hash off its folder.
func filesRecipe(id string) workflow.Recipe {
	return workflow.Recipe{
		ID: id,
		Plan: workflow.Plan{Name: id, Stages: []workflow.Stage{{
			Name:    "items",
			Kind:    workflow.StageFanout,
			Task:    "sweep {item}",
			Over:    &workflow.ItemSource{Files: "*.txt"},
			Returns: workflow.ReceiptSpec{"count": "int"},
		}}},
		Dir: "/skills/" + id,
	}
}

func TestLaunch_BackgroundAndResumeSharePlanAndRecipeWiring(t *testing.T) {
	t.Parallel()

	t.Run("each source is wired for its mode", func(t *testing.T) {
		t.Parallel()
		plain := sweepRecipe("plain", "alpha").Plan
		for _, tc := range []struct {
			name     string
			mode     launchMode
			recipe   string
			wantAsks bool
		}{
			{"a blocking plan", launchModeBlocking, "", false},
			{"a background plan", launchModeBackground, "", false},
			{"a blocking recipe", launchModeBlocking, "sweep", true},
			{"a background recipe", launchModeBackground, "sweep", true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				cfg := recipeConfig(t, newLockedSink(), sweepRecipe("sweep", "beta"))
				cfg.Asker = &scriptedAsker{}
				cfg.ParallelAgents = 3
				a := newBackgroundParent(t, cfg, &workflowResponder{})
				launch := workflowLaunch{
					mode: tc.mode, plan: plain, recipe: domain.RecipeLaunch{SkillID: tc.recipe},
					seat: seatSession, call: domain.ToolCall{ID: "c1"},
				}

				built, err := a.buildLaunch(launch)

				if err != nil {
					t.Fatalf("buildLaunch: %v", err)
				}
				if want := a.workflowWidthOn(seatSession); built.runner.Width != want {
					t.Errorf("runner width = %d, want the seat's %d", built.runner.Width, want)
				}
				spawner, ok := built.runner.Spawner.(*workflowSpawner)
				if !ok {
					t.Fatalf("spawner = %T, want a *workflowSpawner", built.runner.Spawner)
				}
				if spawner.seat != seatSession || spawner.children != &a.children {
					t.Errorf("spawner seat %v, children shared %t; want the launch's seat and this Agent's registry",
						spawner.seat, spawner.children == &a.children)
				}
				background := tc.mode == launchModeBackground
				if isHost := spawner.parent != a; isHost != background || built.host != spawner.parent {
					t.Errorf("children spawned off the launch-time host = %t, want %t", isHost, background)
				}
				wantCall := "c1"
				if background {
					wantCall = backgroundCallPrefix + built.id
				}
				if built.observer.background != background || built.observer.call != wantCall {
					t.Errorf("observer background %t call %q; want %t and %q",
						built.observer.background, built.observer.call, background, wantCall)
				}
				switch asker := built.runner.Asker.(type) {
				case nil:
					if tc.wantAsks {
						t.Error("a recipe launch has no asker")
					}
				case backgroundAsker:
					if !background || asker.scope.workflow != built.id {
						t.Errorf("background asker scoped to %q on a %s launch", asker.scope.workflow, tc.name)
					}
				case observedAsker:
					if background {
						t.Error("a background launch asks through the Turn, not the manager's queue")
					}
				default:
					t.Errorf("asker = %T, want none, the manager's queue or the observed Turn asker", asker)
				}
			})
		}
	})

	t.Run("a moved source resumes in a new folder and is refused a re-run", func(t *testing.T) {
		t.Parallel()
		cfg := recipeConfig(t, newLockedSink(), filesRecipe("files"))
		writeWorkspaceFile(t, cfg.WorkspaceDir, "a.txt", "a")
		// Nothing is routed: every item faults, and its retries end it blocked.
		a := newBackgroundParent(t, cfg, &workflowResponder{})
		id := launchBackground(t, a, "files")
		a.background.waitAll()
		writeWorkspaceFile(t, cfg.WorkspaceDir, "b.txt", "b")

		err := a.RerunFailed(id)

		if want := fmt.Sprintf(rerunMovedFormat, id); err == nil || err.Error() != want {
			t.Errorf("RerunFailed of a moved workflow = %v, want %q", err, want)
		}
		if folders := workflowFolders(t, cfg.ScratchDir); len(folders) != 1 {
			t.Errorf("folders after the refused re-run = %v, want the one", folders)
		}
		if err := a.resumeBackground(workflowEntryJSON{ID: id, Recipe: "files"}); err != nil {
			t.Fatalf("resumeBackground: %v", err)
		}
		a.background.waitAll()
		if folders := workflowFolders(t, cfg.ScratchDir); len(folders) != 2 {
			t.Errorf("folders after the resume = %v, want the moved source run in a new one", folders)
		}
	})
}
