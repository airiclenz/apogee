package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
				return startPlanBackground(t, a, planArgs)
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
			live := snapshotLiveFolder(t, cfg.ScratchDir, id, up, tc.item)

			answer := tc.reissue(t, a, sink, log)

			live.assertRefused(t, answer, tc.lead, sink)
		})
	}

	// A delegate's own background set is empty: its blocking fan_out must still see the root's.
	t.Run("a delegate", func(t *testing.T) {
		t.Parallel()
		sink := newLockedSink()
		cfg := backgroundWorkflowConfig(t, sink)
		cfg.Delegation.MaxDepth = 2 // at depth 1 a child holds fan_out only under a bound above the default
		_ = cfg.Tools.Register(tools.NewSubAgent())
		planArgs := fanOutArgsJSON("alpha")
		started := make(chan struct{})
		up := (&workflowResponder{}).
			route("please delegate", nil, subAgentCallScript("sa1", "redo the sweep")).
			route("please delegate", nil, contentScript("done")).
			route("redo the sweep", nil, toolCallScript("fo2", tools.FanOutToolName, planArgs)).
			route("redo the sweep", nil, contentScript("refused")).
			route("check alpha", signalThenWait(started, nil), cancelledScript()).
			route("check alpha", nil, finishScript("f2", "alpha is fine"))
		a := newBackgroundParent(t, cfg, up)
		id := startPlanBackground(t, a, planArgs)
		awaitClosed(t, started, "the background run's item child")
		live := snapshotLiveFolder(t, cfg.ScratchDir, id, up, "check alpha")

		runInput(t, a, domain.UserInput{Text: "please delegate"})

		live.assertRefused(t, nestedCallResult(t, lockedEvents(sink), "fo2").Content, fanOutRunFailedPrefix, sink)
	})

	// A background run's item child runs under backgroundHost, whose own set is empty too. The
	// re-issued plan queues behind that run on the one server, which keeps it live and untouched.
	t.Run("a background item child", func(t *testing.T) {
		t.Parallel()
		sink := newLockedSink()
		cfg := recipeConfig(t, sink, sweepRecipe("sweep", "beta"))
		cfg.Delegation.MaxDepth = 2
		planArgs := fanOutArgsJSON("alpha")
		started, release, refused := make(chan struct{}), make(chan struct{}), make(chan struct{})
		up := (&workflowResponder{}).
			route("sweep beta", signalThenWait(started, release), toolCallScript("fo2", tools.FanOutToolName, planArgs)).
			route("sweep beta", signalThenWait(refused, nil), cancelledScript()).
			route("check alpha", nil, cancelledScript()).
			route("check alpha", nil, finishScript("f2", "alpha is fine"))
		a := newBackgroundParent(t, cfg, up)
		launchBackground(t, a, "sweep")
		awaitClosed(t, started, "the sweep run's item child")
		id := startPlanBackground(t, a, planArgs)
		if !a.background.isLive(id) {
			t.Fatalf("workflow %s is not live behind the sweep run", id)
		}
		live := snapshotLiveFolder(t, cfg.ScratchDir, id, up, "check alpha")

		close(release)
		awaitClosed(t, refused, "the item child's answered fan_out")

		live.assertRefused(t, nestedCallResult(t, lockedEvents(sink), "fo2").Content, fanOutRunFailedPrefix, sink)
	})
}

// startPlanBackground starts the fan_out plan args in the background on a and returns its id.
func startPlanBackground(t *testing.T, a *Agent, args string) string {
	t.Helper()
	plan, refusal := parseFanOutPlan(json.RawMessage(args))
	if refusal != "" {
		t.Fatalf("parseFanOutPlan: %s", refusal)
	}
	id, err := a.startBackground(workflowLaunch{plan: plan, call: domain.ToolCall{ID: "bg", Tool: tools.FanOutToolName}})
	if err != nil {
		t.Fatalf("startBackground: %v", err)
	}
	return id
}

// nestedCallResult is the delegate's tool result that answered callID.
func nestedCallResult(t *testing.T, events []domain.Event, callID string) domain.ToolResult {
	t.Helper()
	for _, e := range events {
		if re, ok := e.(domain.ToolResultEvent); ok && re.Depth > 0 && re.Result.CallID == callID {
			return re.Result
		}
	}
	t.Fatalf("no delegate result answered %s", callID)
	return domain.ToolResult{}
}

// liveFolder is a live background workflow's folder as it stood before a blocking re-issue: its
// status.json bytes, how often its item was asked, and how many workflow folders existed.
type liveFolder struct {
	id         string
	scratch    string
	statusPath string
	status     []byte
	up         *workflowResponder
	item       string
	asked      int
	folders    int
}

// snapshotLiveFolder records the live workflow id's folder ahead of a re-issue.
func snapshotLiveFolder(t *testing.T, scratch, id string, up *workflowResponder, item string) liveFolder {
	t.Helper()
	statusPath := filepath.Join(scratch, "workflows", id, "status.json")
	status, err := os.ReadFile(statusPath)
	if err != nil {
		t.Fatalf("read status.json: %v", err)
	}
	return liveFolder{
		id: id, scratch: scratch, statusPath: statusPath, status: status,
		up: up, item: item, asked: up.askedCount(item), folders: len(workflowFolders(t, scratch)),
	}
}

// assertRefused checks a re-issue was refused naming the live run behind lead, and left its folder
// as it stood: no item asked again, no new folder, status.json unchanged, and no foreground phase
// reported for it.
func (l liveFolder) assertRefused(t *testing.T, answer, lead string, sink *lockedSink) {
	t.Helper()
	if want := lead + fmt.Sprintf(workflowAlreadyRunningFormat, l.id); !strings.Contains(answer, want) {
		t.Errorf("re-issue answered\n%s\nwant it to carry %q", answer, want)
	}
	if n := l.up.askedCount(l.item); n != l.asked {
		t.Errorf("%s asked %d times, want the %d before the re-issue", l.item, n, l.asked)
	}
	if folders := workflowFolders(t, l.scratch); len(folders) != l.folders {
		t.Errorf("workflow folders = %v, want the %d before the re-issue", folders, l.folders)
	}
	after, err := os.ReadFile(l.statusPath)
	if err != nil {
		t.Fatalf("re-read status.json: %v", err)
	}
	if !bytes.Equal(l.status, after) {
		t.Errorf("status.json changed under the refused re-issue:\n%s\nwas\n%s", after, l.status)
	}
	for _, event := range lockedEvents(sink) {
		if phase, ok := event.(domain.WorkflowPhaseEvent); ok && phase.Workflow == l.id && !phase.Background {
			t.Errorf("the refused re-issue reported %+v", phase)
		}
	}
}

// TestLaunch_WorkspaceReadsStayInsideTheWorkspaceRoot pins the Runner's workspace to its root
// (security.RootFS): a fan_out whose source or context file is a symlink out of the workspace, or
// an absolute symlink even into it, fails naming that source and no child is asked about what lies
// behind it; a relative symlink that stays inside still reads.
func TestLaunch_WorkspaceReadsStayInsideTheWorkspaceRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	t.Parallel()

	for _, tc := range []struct {
		name string
		// arrange lays the workspace out and returns the fan_out's `over` and `context`.
		arrange func(t *testing.T, workspace string) (over any, contextFiles []string)
		// want is the text the fan_out answer carries; leaked is a child's routing key that
		// must never be asked, "" when the case expects the run to read and ask "check alpha".
		want   string
		leaked string
	}{
		{
			name: "lines: an escaping symlink",
			arrange: func(t *testing.T, workspace string) (any, []string) {
				mustSymlink(t, writeOutsideSecret(t), filepath.Join(workspace, "items.txt"))
				return map[string]string{"lines": "items.txt"}, nil
			},
			want: "lines: items.txt", leaked: "HOST SECRET",
		},
		{
			name: "lines: an absolute symlink into the workspace",
			arrange: func(t *testing.T, workspace string) (any, []string) {
				writeWorkspaceFile(t, workspace, "real.txt", "alpha")
				mustSymlink(t, filepath.Join(workspace, "real.txt"), filepath.Join(workspace, "items.txt"))
				return map[string]string{"lines": "items.txt"}, nil
			},
			want: "lines: items.txt", leaked: "check alpha",
		},
		{
			name: "files: a prefix that escapes",
			arrange: func(t *testing.T, workspace string) (any, []string) {
				mustSymlink(t, filepath.Dir(writeOutsideSecret(t)), filepath.Join(workspace, "docs"))
				return map[string]string{"files": "docs/*.txt"}, nil
			},
			want: "files: docs/*.txt", leaked: "secret.txt",
		},
		{
			name: "files: a prefix that is an absolute symlink into the workspace",
			arrange: func(t *testing.T, workspace string) (any, []string) {
				writeWorkspaceFile(t, workspace, "real/a.txt", "a")
				mustSymlink(t, filepath.Join(workspace, "real"), filepath.Join(workspace, "docs"))
				return map[string]string{"files": "docs/*.txt"}, nil
			},
			want: "files: docs/*.txt", leaked: "docs/a.txt",
		},
		{
			name: "split: a directory that escapes",
			arrange: func(t *testing.T, workspace string) (any, []string) {
				mustSymlink(t, filepath.Dir(writeOutsideSecret(t)), filepath.Join(workspace, "docs"))
				return map[string]string{"split": "docs"}, nil
			},
			want: "split: docs", leaked: "secret.txt",
		},
		{
			name: "a context file that escapes",
			arrange: func(t *testing.T, workspace string) (any, []string) {
				mustSymlink(t, writeOutsideSecret(t), filepath.Join(workspace, "ctx.md"))
				return []string{"alpha"}, []string{"ctx.md"}
			},
			want: `context file "ctx.md"`, leaked: "check alpha",
		},
		{
			name: "lines: a relative symlink inside the workspace",
			arrange: func(t *testing.T, workspace string) (any, []string) {
				writeWorkspaceFile(t, workspace, "real.txt", "alpha")
				mustSymlink(t, "real.txt", filepath.Join(workspace, "items.txt"))
				return map[string]string{"lines": "items.txt"}, nil
			},
			want: "#1 alpha — ok — alpha is fine",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sink := &recordingSink{}
			cfg := workflowConfig(t, sink)
			cfg.Context.MaxContextTokens = 65536 // room for a split: source to size its parts by
			over, contextFiles := tc.arrange(t, cfg.WorkspaceDir)
			args, err := json.Marshal(map[string]any{
				"task": fanOutTask, "over": over, "context": contextFiles,
				"returns": map[string]string{"count": "int"},
			})
			if err != nil {
				t.Fatal(err)
			}
			up := (&workflowResponder{}).
				route("please fan out", nil, toolCallScript("fo1", tools.FanOutToolName, string(args))).
				route("please fan out", nil, contentScript("all done")).
				route("check alpha", nil, finishScript("f1", "alpha is fine"))
			if tc.leaked != "" && tc.leaked != "check alpha" {
				up.route(tc.leaked, nil, finishScript("f2", "read it"))
			}

			runWorkflowParent(t, context.Background(), cfg, up, "please fan out")

			got := callResult(t, sink.events, "fo1")
			if !strings.Contains(got.Content, tc.want) || strings.Contains(got.Content, "HOST SECRET") {
				t.Errorf("fan_out answer = %q (error %t), want it to carry %q and never the outside bytes",
					got.Content, got.IsError, tc.want)
			}
			if tc.leaked == "" {
				if got.IsError || up.askedCount("check alpha") != 1 {
					t.Errorf("in-root symlink: answer error %t, children asked %d; want the item read and run",
						got.IsError, up.askedCount("check alpha"))
				}
				return
			}
			if !got.IsError {
				t.Errorf("fan_out answer is not an error: %q", got.Content)
			}
			if asked := up.askedCount(tc.leaked); asked != 0 {
				t.Errorf("a child was asked about %q %d times, want never", tc.leaked, asked)
			}
		})
	}
}

// mustSymlink links name to target, failing the test when the link cannot be made.
func mustSymlink(t *testing.T, target, name string) {
	t.Helper()
	if err := os.Symlink(target, name); err != nil {
		t.Fatal(err)
	}
}
