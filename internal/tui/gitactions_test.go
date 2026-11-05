package tui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/gitstatus"
)

// fakeGitActionsHost stands in for cmd/apogee's git actions host: each action answers with its own
// verb's result (or err), and every call is recorded by verb.
type fakeGitActionsHost struct {
	mu    sync.Mutex
	err   error
	calls []string
}

func (f *fakeGitActionsHost) record(verb string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, verb)
	if f.err != nil {
		return "", f.err
	}
	return verb + ": done", nil
}

func (f *fakeGitActionsHost) Fetch(context.Context, string) (string, error) { return f.record("fetch") }
func (f *fakeGitActionsHost) Pull(context.Context, string) (string, error)  { return f.record("pull") }
func (f *fakeGitActionsHost) Push(context.Context, string) (string, error)  { return f.record("push") }
func (f *fakeGitActionsHost) Sync(context.Context, string) (string, error)  { return f.record("sync") }

func (f *fakeGitActionsHost) Branches(context.Context, string) ([]string, error) {
	return []string{"main"}, nil
}

func (f *fakeGitActionsHost) Switch(_ context.Context, _, name string) (string, error) {
	return f.record("switch " + name)
}

func (f *fakeGitActionsHost) Rebase(context.Context, string) (string, error) {
	return f.record("rebase")
}

func (f *fakeGitActionsHost) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// gitActionsModel builds a sized Model with both git seams wired over a workspace and the first
// status read landed, so the segment shows and the git actions have a repository to act on.
func gitActionsModel(t *testing.T, snap gitstatus.Snapshot, actions *fakeGitActionsHost, fetch bool) (Model, *fakeGitHost) {
	t.Helper()
	return gitActionsModelWith(t, snap, actions, func(opts *Options) { opts.UI.GitFetch = fetch })
}

// gitActionsModelWith is gitActionsModel with the Options adjusted by tune before the Model is built;
// a nil actions host leaves the git actions unwired.
func gitActionsModelWith(t *testing.T, snap gitstatus.Snapshot, actions *fakeGitActionsHost, tune func(*Options)) (Model, *fakeGitHost) {
	t.Helper()
	host := newFakeGitHost(snap)
	opts := withTestUI(testOpts)
	opts.Workspace = t.TempDir()
	opts.GitStatus = host
	if actions != nil {
		opts.GitActions = actions
	}
	tune(&opts)
	m := newModel(context.Background(), &fakeEngine{}, opts, nil)
	m = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = land(t, m)
	return m, host
}

// untracked is a snapshot of a branch with no upstream.
func untracked() gitstatus.Snapshot {
	return gitstatus.Snapshot{Branch: "topic"}
}

// gitActionRowLabels is the picker's row labels, in order.
func gitActionRowLabels(m Model) []string {
	var labels []string
	for _, row := range m.gitActionsOffered() {
		labels = append(labels, row.label)
	}
	return labels
}

// gitActionIndex is the offering index of action, failing the test where the picker lists none.
func gitActionIndex(t *testing.T, m Model, action gitAction) int {
	t.Helper()
	for i, row := range m.gitActionsOffered() {
		if row.action == action {
			return i
		}
	}
	t.Fatalf("the git actions picker lists no %s row: %v", action.word(), gitActionRowLabels(m))
	return -1
}

// acceptGitRow opens the picker with /git, highlights action's row and takes it with ⏎, returning
// the Model and the Cmd the accept issued.
func acceptGitRow(t *testing.T, m Model, action gitAction) (Model, tea.Cmd) {
	t.Helper()
	m = sendLine(t, m, "/git")
	if !m.picker.open || m.picker.kind != pickerGitActions {
		t.Fatalf("/git gave picker %+v, want the git actions picker", m.picker)
	}
	m.picker.selected = gitActionIndex(t, m, action)
	return stepCmd(t, m, keyEnter())
}

func TestGitActionSlashGitOpensThePicker(t *testing.T) {
	t.Parallel()
	m, _ := gitActionsModel(t, tracked(), &fakeGitActionsHost{}, false)

	m = sendLine(t, m, "/git")

	if !m.picker.open || m.picker.kind != pickerGitActions {
		t.Fatalf("/git gave picker %+v, want the git actions picker open", m.picker)
	}
	if got := m.gitActionsPickerTitle(); got != "git actions — main" {
		t.Errorf("title = %q, want it to name the branch", got)
	}
}

func TestGitActionRowsFollowTheUpstream(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		snap gitstatus.Snapshot
		want []string
	}{
		{"with an upstream", tracked(),
			[]string{"fetch", "pull", "push", "sync", "switch branch", "rebase onto upstream"}},
		{"without one", untracked(), []string{"fetch", "push", "switch branch"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m, _ := gitActionsModel(t, tc.snap, &fakeGitActionsHost{}, false)

			if got := gitActionRowLabels(m); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("rows = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestGitActionEachActionNotesItsResult: each action that runs from the picker notes `git <verb>…`,
// runs off the loop under the hold, and on landing notes the host's result — or its error with the
// verb said once — releases the hold and asks for a status read.
func TestGitActionEachActionNotesItsResult(t *testing.T) {
	t.Parallel()
	for _, action := range []gitAction{gitActionFetch, gitActionPull, gitActionPush, gitActionSync} {
		for _, fails := range []bool{false, true} {
			name := action.word()
			if fails {
				name += " fails"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				actions := &fakeGitActionsHost{}
				if fails {
					actions.err = errors.New("git " + action.word() + ": remote said no")
				}
				m, _ := gitActionsModel(t, tracked(), actions, false)
				gen := m.git.gen

				m, cmd := acceptGitRow(t, m, action)
				if m.picker.open {
					t.Errorf("the accept left the picker open: %+v", m.picker)
				}
				if got, want := lastNote(m), "git "+action.word()+"…"; got != want {
					t.Errorf("start note = %q, want %q", got, want)
				}
				if !m.engineHolds().holding(holdGitAction) {
					t.Fatal("the action runs without the git action hold")
				}
				done := awaitGitMsg[gitActionDoneMsg](t, cmd, "git action")
				if got := actions.called(); !reflect.DeepEqual(got, []string{action.word()}) {
					t.Errorf("host calls = %v, want [%s]", got, action.word())
				}

				m = step(t, m, done)

				want := "git " + action.word() + ": done"
				if fails {
					want = "git " + action.word() + " failed — remote said no"
				}
				if got := lastNote(m); got != want {
					t.Errorf("result note = %q, want %q", got, want)
				}
				if m.engineHolds().holding(holdGitAction) {
					t.Error("the hold outlived the action's landing")
				}
				if m.git.gen == gen || !m.git.inFlight {
					t.Errorf("git = %+v, want a fresh status read issued after the action", m.git)
				}
			})
		}
	}
}

// TestGitActionRefusedWhileBusy: while a Turn runs, or a decision is pending, /git and a click on the
// git segment note that git actions wait for the Turn to end and open nothing; while an action is
// still running they say so.
func TestGitActionRefusedWhileBusy(t *testing.T) {
	t.Parallel()
	for _, state := range []uiState{stateRunning, stateAwaitingApproval} {
		m, _ := gitActionsModel(t, tracked(), &fakeGitActionsHost{}, false)
		m.state = state

		next, _ := m.runGitCommand()
		if got := next.(Model); got.picker.open || lastNote(got) != gitActionsBusyNote {
			t.Errorf("state %d: /git gave picker %+v and note %q, want the wait note alone",
				state, got.picker, lastNote(got))
		}
		y, x, _ := footerGitCells(t, m)
		if got := step(t, m, leftClick(x, y)); got.picker.open || lastNote(got) != gitActionsBusyNote {
			t.Errorf("state %d: a click on the git segment gave picker %+v and note %q, want the wait note alone",
				state, got.picker, lastNote(got))
		}
	}

	m, _ := gitActionsModel(t, tracked(), &fakeGitActionsHost{}, false)
	m, _ = acceptGitRow(t, m, gitActionPush)
	m = sendLine(t, m, "/git")
	if m.picker.open || lastNote(m) != "git push is still running — git actions wait for it to finish" {
		t.Errorf("/git mid-action gave picker %+v and note %q, want the still-running note", m.picker, lastNote(m))
	}
}

func TestGitActionWithNoGitSaysSo(t *testing.T) {
	t.Parallel()

	t.Run("no actions host", func(t *testing.T) {
		t.Parallel()
		m := gitModel(t, newFakeGitHost(tracked()), false)
		m, _ = land(t, m)
		m = sendLine(t, m, "/git")
		if m.picker.open || lastNote(m) != noGitActionsNote {
			t.Errorf("/git gave picker %+v and note %q, want the no-git note", m.picker, lastNote(m))
		}
	})

	t.Run("no repository apogee can act on", func(t *testing.T) {
		t.Parallel()
		host := newFakeGitHost(gitstatus.Snapshot{})
		host.inside = false
		opts := withTestUI(testOpts)
		opts.Workspace = t.TempDir()
		opts.GitStatus = host
		opts.GitActions = &fakeGitActionsHost{}
		m := newModelSized(t, opts)
		m, _ = land(t, m)
		m = sendLine(t, m, "/git")
		if m.picker.open || lastNote(m) != noGitActionsNote {
			t.Errorf("/git gave picker %+v and note %q, want the no-git note", m.picker, lastNote(m))
		}
	})
}

// newModelSized builds a sized Model over opts.
func newModelSized(t *testing.T, opts Options) Model {
	t.Helper()
	m := newModel(context.Background(), &fakeEngine{}, opts, nil)
	return step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
}

// TestGitActionPromptWaitsMidAction: a message sent while an action runs is refused with the hold's
// note and stays in the box; an idle-only command is queued and runs once the action lands; a wake
// and a workflow resume wait.
func TestGitActionPromptWaitsMidAction(t *testing.T) {
	t.Parallel()
	m, _ := gitActionsModel(t, tracked(), &fakeGitActionsHost{}, false)
	m, cmd := acceptGitRow(t, m, gitActionPull)

	m = sendLine(t, m, "hello")
	if m.state != stateIdle || lastNote(m) != gitActionHoldNote {
		t.Errorf("a prompt mid-action gave state %d and note %q, want idle and the hold's note", m.state, lastNote(m))
	}
	if got := m.input.Value(); got != "hello" {
		t.Errorf("the box holds %q, want the refused message kept", got)
	}
	holds := m.engineHolds()
	if holds.canWake() || holds.canResumeWorkflows() {
		t.Error("a wake or a workflow resume may run mid-action")
	}
	if holds.commandRunnable(parseInput("/clear", nil)) {
		t.Error("an idle-only command may run mid-action")
	}

	m = step(t, m, awaitGitMsg[gitActionDoneMsg](t, cmd, "git action"))
	if !m.engineHolds().canWake() {
		t.Error("a wake still waits once the action has landed")
	}
}

// TestGitActionNoBackgroundFetchMidAction: the background fetch chain's tick issues no fetch while
// an action runs, and an action started while a background fetch is in flight waits for it to land.
func TestGitActionNoBackgroundFetchMidAction(t *testing.T) {
	t.Parallel()

	t.Run("a tick mid-action fetches nothing", func(t *testing.T) {
		t.Parallel()
		m, host := gitActionsModel(t, tracked(), &fakeGitActionsHost{}, true)
		if !m.git.fetchLive {
			t.Fatal("the fetch chain is not live; this case needs one")
		}
		m = step(t, m, gitFetchMsg{gen: m.git.fetchGen}) // the first fetch lands; the chain waits its tick
		_, before := host.calls()
		m, _ = acceptGitRow(t, m, gitActionPush)

		m, cmd := stepCmd(t, m, gitFetchTickMsg{gen: m.git.fetchGen})

		for _, msg := range gitMsgs(cmd, shortWindow) {
			if _, fetched := msg.(gitFetchMsg); fetched {
				t.Fatal("the fetch tick issued a background fetch mid-action")
			}
		}
		if _, after := host.calls(); after != before {
			t.Errorf("fetch calls went from %d to %d mid-action", before, after)
		}
		if !m.git.fetchLive {
			t.Error("the fetch chain retired instead of waiting one more interval")
		}
	})

	t.Run("an action waits for an in-flight fetch", func(t *testing.T) {
		t.Parallel()
		actions := &fakeGitActionsHost{}
		m, _ := gitActionsModel(t, tracked(), actions, true)
		if !m.git.fetchInFlight {
			t.Fatal("no background fetch in flight; this case needs one")
		}

		m, cmd := acceptGitRow(t, m, gitActionPull)
		if cmd != nil || !m.git.actionWaits {
			t.Fatalf("the action was issued beside the in-flight fetch (cmd %v, git %+v)", cmd != nil, m.git)
		}

		m, cmd = stepCmd(t, m, gitFetchMsg{gen: m.git.fetchGen})
		done := awaitGitMsg[gitActionDoneMsg](t, cmd, "the waiting git action")
		if done.action != gitActionPull {
			t.Errorf("the fetch's landing issued %s, want pull", done.action.word())
		}
		m = step(t, m, done)
		if got := lastNote(m); got != "git pull: done" {
			t.Errorf("result note = %q, want the pull's", got)
		}
	})
}

// TestGitActionQuitMidActionExitsAfterCompletion: a quit while an action runs defers its exit, and
// the action's landing fires it.
func TestGitActionQuitMidActionExitsAfterCompletion(t *testing.T) {
	t.Parallel()
	m, _ := gitActionsModel(t, tracked(), &fakeGitActionsHost{}, false)
	m, cmd := acceptGitRow(t, m, gitActionSync)

	m, quit := ctrlCQuit(t, m)
	if isQuit(quit) || !m.quitting {
		t.Fatalf("a quit mid-action exited at once (quit %v, quitting %v), want it deferred", isQuit(quit), m.quitting)
	}

	m, quit = stepCmd(t, m, awaitGitMsg[gitActionDoneMsg](t, cmd, "git action"))
	if !isQuit(quit) {
		t.Fatal("the action's landing did not fire the deferred exit")
	}
	if got := lastNote(m); !strings.HasPrefix(got, "git sync") {
		t.Errorf("last note = %q, want the sync's result noted before the exit", got)
	}
}
