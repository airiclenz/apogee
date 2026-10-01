package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
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

// liveFolderSurface is one blocking launch surface re-issued onto a workflow a background run still
// drives: its gated item's route key, the background launch that starts the run, the re-issue's
// answer and the refusal line that answer must carry ahead of the already-running text.
type liveFolderSurface struct {
	name    string
	item    string
	args    string
	start   func(t *testing.T, a *Agent) string
	reissue func(t *testing.T, a *Agent, sink *lockedSink, log *requestLog) string
	lead    string
}

// liveFolderSurfaces is every blocking surface: a fan_out plan, a fan_out naming a recipe, a typed
// "/<id>" and a foreground StartRecipe; the recipe ones re-issue the one-item recipe "sweep".
func liveFolderSurfaces() []liveFolderSurface {
	planArgs := fanOutArgsJSON("alpha")
	startRecipe := func(t *testing.T, a *Agent) string {
		t.Helper()
		return launchBackground(t, a, "sweep")
	}
	callAnswer := func(t *testing.T, a *Agent, sink *lockedSink, _ *requestLog) string {
		t.Helper()
		runInput(t, a, domain.UserInput{Text: "please fan out"})
		return callResult(t, lockedEvents(sink), "fo1").Content
	}
	return []liveFolderSurface{
		{
			name: "a fan_out plan", item: "check alpha", args: planArgs,
			start: func(t *testing.T, a *Agent) string {
				t.Helper()
				plan, refusal := parseFanOutPlan(json.RawMessage(planArgs))
				if refusal != "" {
					t.Fatalf("parseFanOutPlan: %s", refusal)
				}
				id, err := a.startBackground(workflowLaunch{
					plan: plan, call: domain.ToolCall{ID: "bg", Tool: tools.FanOutToolName},
				})
				if err != nil {
					t.Fatalf("startBackground: %v", err)
				}
				return id
			},
			reissue: callAnswer,
			lead:    fanOutRunFailedPrefix,
		},
		{
			name: "a fan_out recipe", item: "sweep alpha", args: `{"recipe":"sweep"}`,
			start: startRecipe, reissue: callAnswer, lead: fmt.Sprintf(fanOutRecipeFailed, "sweep", ""),
		},
		{
			name: "a typed /<id>", item: "sweep alpha", start: startRecipe,
			reissue: func(t *testing.T, a *Agent, _ *lockedSink, log *requestLog) string {
				t.Helper()
				runInput(t, a, domain.UserInput{Text: "/sweep", SkillIDs: []string{"sweep"}})
				return log.first(t, "/sweep")
			},
			lead: fmt.Sprintf(recipeRefusalFormat, "sweep", ""),
		},
		{
			name: "a foreground StartRecipe", item: "sweep alpha", start: startRecipe,
			reissue: func(t *testing.T, a *Agent, _ *lockedSink, log *requestLog) string {
				t.Helper()
				if _, err := a.StartRecipe(context.Background(), RecipeLaunch{SkillID: "sweep"}); err != nil {
					t.Fatalf("StartRecipe: %v", err)
				}
				if _, err := a.Run(context.Background()); err != nil {
					t.Fatalf("Run: %v", err)
				}
				return log.first(t, "/sweep")
			},
			lead: fmt.Sprintf(recipeRefusalFormat, "sweep", ""),
		},
	}
}

func TestBlockingLaunchRefusedOnALiveBackgroundFolder(t *testing.T) {
	t.Parallel()

	for _, tc := range liveFolderSurfaces() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sink := newLockedSink()
			cfg := recipeConfig(t, sink, sweepRecipe("sweep", "alpha"))
			started := make(chan struct{})
			// The gated item's second script is what a duplicate child would consume.
			up := (&workflowResponder{}).
				route("/sweep", nil, contentScript("noted")).
				route("please fan out", nil, toolCallScript("fo1", tools.FanOutToolName, tc.args)).
				route("please fan out", nil, contentScript("done")).
				route(tc.item, signalThenWait(started, nil), cancelledScript()).
				route(tc.item, nil, finishScript("f2", "alpha is fine"))
			log := &requestLog{inner: up}
			a := newBackgroundParent(t, cfg, log)
			id := tc.start(t, a)
			awaitClosed(t, started, "the background run's item child")
			statusPath := filepath.Join(cfg.ScratchDir, "workflows", id, "status.json")
			before, err := os.ReadFile(statusPath)
			if err != nil {
				t.Fatalf("read status.json: %v", err)
			}

			answer := tc.reissue(t, a, sink, log)

			if want := tc.lead + fmt.Sprintf(workflowAlreadyRunningFormat, id); !strings.Contains(answer, want) {
				t.Errorf("re-issue answered\n%s\nwant it to carry %q", answer, want)
			}
			if n := up.askedCount(tc.item); n != 1 {
				t.Errorf("%s asked %d times, want only the background run's one child", tc.item, n)
			}
			if folders := workflowFolders(t, cfg.ScratchDir); len(folders) != 1 {
				t.Errorf("workflow folders = %v, want the background run's one", folders)
			}
			after, err := os.ReadFile(statusPath)
			if err != nil {
				t.Fatalf("re-read status.json: %v", err)
			}
			if !bytes.Equal(before, after) {
				t.Errorf("status.json changed under the refused re-issue:\n%s\nwas\n%s", after, before)
			}
			for _, event := range lockedEvents(sink) {
				if phase, ok := event.(domain.WorkflowPhaseEvent); ok && phase.Workflow == id && !phase.Background {
					t.Errorf("the refused re-issue reported %+v", phase)
				}
			}
		})
	}
}
