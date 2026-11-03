package tui

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"strconv"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/gitstatus"
	"github.com/airiclenz/apogee/internal/testtime"
)

// fakeGitHost is a scripted [GitStatusHost]. It answers every Status with the scripted snapshot,
// inside flag and error, and counts the calls. With block set, a Status call parks until release is
// closed or its ctx ends, recording the ctx error it saw — the slow read and the exit tests.
type fakeGitHost struct {
	mu       sync.Mutex
	snap     gitstatus.Snapshot
	inside   bool
	err      error
	fetchErr error

	block   bool
	release chan struct{}
	started chan struct{} // one send per Status call, when block is set
	ended   chan error    // the ctx error a blocked call returned with

	statusCalls int
	fetchCalls  int
}

func newFakeGitHost(snap gitstatus.Snapshot) *fakeGitHost {
	return &fakeGitHost{
		snap:    snap,
		inside:  true,
		release: make(chan struct{}),
		started: make(chan struct{}, 8),
		ended:   make(chan error, 8),
	}
}

func (f *fakeGitHost) Status(ctx context.Context, _ string) (gitstatus.Snapshot, bool, error) {
	f.mu.Lock()
	f.statusCalls++
	block, snap, inside, err := f.block, f.snap, f.inside, f.err
	f.mu.Unlock()
	if block {
		f.started <- struct{}{}
		select {
		case <-f.release:
		case <-ctx.Done():
			f.ended <- ctx.Err()
			return gitstatus.Snapshot{}, false, ctx.Err()
		}
	}
	return snap, inside, err
}

func (f *fakeGitHost) Fetch(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetchCalls++
	return f.fetchErr
}

func (f *fakeGitHost) set(apply func(*fakeGitHost)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	apply(f)
}

func (f *fakeGitHost) calls() (status, fetch int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.statusCalls, f.fetchCalls
}

// tracked is a snapshot of a branch with an upstream and some state to count.
func tracked() gitstatus.Snapshot {
	return gitstatus.Snapshot{
		Branch: "main", Upstream: "origin/main", HasAheadBehind: true,
		Ahead: 2, Behind: 1, Dirty: 3, Staged: 1, Unstaged: 2,
		StagedEntries: []string{"M.  a.go"},
	}
}

// gitModel builds a sized Model with host wired over a workspace, the pref set as given.
func gitModel(t *testing.T, host GitStatusHost, fetch bool) Model {
	t.Helper()
	opts := withTestUI(testOpts)
	opts.Workspace = t.TempDir()
	opts.GitStatus = host
	opts.UI.GitFetch = fetch
	m := newModel(context.Background(), &fakeEngine{}, opts, nil)
	return step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
}

// runCmdTree runs cmd — and every Cmd a batch it yields carries — each on a goroutine of its own,
// and streams the Msgs they yield. A tick parks for its whole period, so its Msg comes late or,
// for a long one, never within a test.
func runCmdTree(cmd tea.Cmd) <-chan tea.Msg {
	out := make(chan tea.Msg, 16)
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			msg := c()
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, inner := range batch {
					run(inner)
				}
				return
			}
			out <- msg
		}()
	}
	run(cmd)
	return out
}

// isGitMsg reports whether msg is one of the git chains' own Msgs.
func isGitMsg(msg tea.Msg) bool {
	switch msg.(type) {
	case gitStatusMsg, gitStatusTickMsg, gitFetchMsg, gitFetchTickMsg:
		return true
	}
	return false
}

// gitMsgs returns the git Msgs cmd's tree yields within window.
func gitMsgs(cmd tea.Cmd, window time.Duration) []tea.Msg {
	out := runCmdTree(cmd)
	var got []tea.Msg
	expired := time.After(window)
	for {
		select {
		case msg := <-out:
			if isGitMsg(msg) {
				got = append(got, msg)
			}
		case <-expired:
			return got
		}
	}
}

// awaitGitMsg returns the first Msg of type T cmd's tree yields, failing the test when none lands
// within the budget.
func awaitGitMsg[T tea.Msg](t *testing.T, cmd tea.Cmd, what string) T {
	t.Helper()
	out := runCmdTree(cmd)
	expired := time.After(testtime.Budget(5 * time.Second))
	for {
		select {
		case msg := <-out:
			if typed, ok := msg.(T); ok {
				return typed
			}
		case <-expired:
			var zero T
			t.Fatalf("no %s landed", what)
			return zero
		}
	}
}

// shortWindow is how long a negative check waits for a Cmd that must not have run at once. It is
// a wait for something NOT to happen, so a slow box only makes it more thorough.
var shortWindow = testtime.Fixed(200 * time.Millisecond)

// land runs the read gitStatusCmd issues and folds it, returning the Model and the fold's Cmd.
func land(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	msg := awaitGitMsg[gitStatusMsg](t, m.gitStatusCmd(), "status read")
	return stepCmd(t, m, msg)
}

func TestGitStatusInitIssuesTheFirstRead(t *testing.T) {
	t.Parallel()

	host := newFakeGitHost(tracked())
	m := gitModel(t, host, false)
	if m.git.gen != 1 || !m.git.inFlight {
		t.Fatalf("git = %+v, want generation 1 armed with Init's read in flight", m.git)
	}
	msg := awaitGitMsg[gitStatusMsg](t, m.Init(), "first status read from Init")
	if msg.gen != 1 {
		t.Errorf("first read carries generation %d, want 1", msg.gen)
	}
	m = step(t, m, msg)
	snap, ok := m.gitSnapshot()
	if !ok {
		t.Fatal("the first snapshot did not land in the Model")
	}
	want := countsOnly(tracked())
	if snap.Branch != want.Branch || snap.Ahead != 2 || snap.Behind != 1 || snap.Dirty != 3 || snap.Upstream != want.Upstream {
		t.Errorf("snapshot = %+v, want %+v", snap, want)
	}
	if snap.StagedEntries != nil {
		t.Errorf("the Model kept the entry lists %v; it keeps counts only", snap.StagedEntries)
	}
	if m.git.inFlight {
		t.Error("the landed read left the chain marked in flight")
	}
}

func TestGitStatusErrorKeepsTheLastCounts(t *testing.T) {
	t.Parallel()

	host := newFakeGitHost(tracked())
	m, _ := land(t, gitModel(t, host, false))
	host.set(func(f *fakeGitHost) { f.err = errors.New("git status: timed out") })
	m.git.inFlight = true
	m, _ = land(t, m)

	snap, ok := m.gitSnapshot()
	if !ok || snap.Dirty != 3 || snap.Ahead != 2 || snap.Behind != 1 {
		t.Errorf("after a failed read the snapshot is %+v (ok=%v); want the last good counts kept", snap, ok)
	}
}

func TestGitStatusNotInsideClearsTheSnapshot(t *testing.T) {
	t.Parallel()

	host := newFakeGitHost(tracked())
	m, _ := land(t, gitModel(t, host, false))
	host.set(func(f *fakeGitHost) { f.inside = false })
	m.git.inFlight = true
	m, _ = land(t, m)
	if _, ok := m.gitSnapshot(); ok {
		t.Error("a read that found no work tree kept the old snapshot; the segment must go")
	}
}

// A slow read never overlaps a second one: while it is blocked a tick issues nothing and a Turn end
// only marks the chain dirty, View still renders, and the landing issues exactly one follow-up.
func TestGitStatusSlowReadNeverOverlaps(t *testing.T) {
	t.Parallel()

	host := newFakeGitHost(tracked())
	host.block = true
	m := gitModel(t, host, false)

	landed := make(chan tea.Msg, 1)
	read := m.gitStatusCmd()
	go func() { landed <- read() }()
	select {
	case <-host.started:
	case <-time.After(testtime.Budget(5 * time.Second)):
		t.Fatal("the status read never reached the host")
	}

	if _, cmd := stepCmd(t, m, gitStatusTickMsg{gen: m.git.gen}); cmd != nil {
		t.Error("a tick while a read is in flight issued another read")
	}
	m, cmd := stepCmd(t, m, turnSnapshotMsg{})
	m, cmd2 := stepCmd(t, m, turnSnapshotMsg{})
	if got := gitMsgs(tea.Batch(cmd, cmd2), shortWindow); len(got) != 0 {
		t.Errorf("Turn ends while a read was in flight issued %v; want them coalesced into one follow-up", got)
	}
	if !m.git.dirty {
		t.Error("a Turn end while a read was in flight did not mark the chain dirty")
	}
	_ = m.View() // renders while the read is blocked: no git on the render path

	if status, _ := host.calls(); status != 1 {
		t.Fatalf("%d Status calls while the first was blocked, want 1", status)
	}
	host.set(func(f *fakeGitHost) { f.block = false })
	close(host.release)
	m, cmd = stepCmd(t, m, <-landed)
	follow := awaitGitMsg[gitStatusMsg](t, cmd, "the dirty chain's follow-up read")
	if follow.gen != m.git.gen {
		t.Errorf("follow-up read carries generation %d, want the live %d", follow.gen, m.git.gen)
	}
	if status, _ := host.calls(); status != 2 {
		t.Errorf("%d Status calls, want exactly 2 — the first and one follow-up", status)
	}
	if m.git.dirty {
		t.Error("the follow-up left the chain dirty")
	}
}

// A Turn end while the chain waits on its tick reads now on a fresh generation, which retires the
// pending tick: when both ticks land, only the live one issues a read.
func TestGitStatusTurnEndWhileIdleRetiresThePendingTick(t *testing.T) {
	t.Parallel()

	host := newFakeGitHost(tracked())
	m, _ := land(t, gitModel(t, host, false))
	old := m.git.gen

	m, cmd := stepCmd(t, m, turnSnapshotMsg{})
	if m.git.gen != old+1 {
		t.Fatalf("an idle Turn end left generation %d, want a fresh %d", m.git.gen, old+1)
	}
	m = step(t, m, awaitGitMsg[gitStatusMsg](t, cmd, "the Turn-end read"))

	if _, cmd := stepCmd(t, m, gitStatusTickMsg{gen: old}); cmd != nil {
		t.Error("the retired chain's tick issued a read beside the live chain")
	}
	_, cmd = stepCmd(t, m, gitStatusTickMsg{gen: m.git.gen})
	awaitGitMsg[gitStatusMsg](t, cmd, "the live chain's next read")
	if status, _ := host.calls(); status != 3 {
		t.Errorf("%d Status calls, want 3 — Init's, the Turn end's and the live tick's", status)
	}
}

// After a read that found no work tree, or one that failed, the chain waits out the long backoff:
// no read runs on the (here 10 ms) cadence.
func TestGitStatusMissWaitsOutTheBackoff(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		miss func(*fakeGitHost)
	}{
		{"not inside", func(f *fakeGitHost) { f.inside = false }},
		{"error", func(f *fakeGitHost) { f.err = errors.New("wedged") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host := newFakeGitHost(tracked())
			tc.miss(host)
			opts := withTestUI(testOpts)
			opts.Workspace = t.TempDir()
			opts.GitStatus = host
			opts.GitStatusInterval = 10 * time.Millisecond
			m := newModel(context.Background(), &fakeEngine{}, opts, nil)

			_, cmd := land(t, m)
			if got := gitMsgs(cmd, shortWindow); len(got) != 0 {
				t.Errorf("after a %s read the chain ticked within %v: %v; want the %v backoff", tc.name, shortWindow, got, gitStatusBackoff)
			}
		})
	}

	// The control: a good read on the same cadence does tick.
	opts := withTestUI(testOpts)
	opts.Workspace = t.TempDir()
	opts.GitStatus = newFakeGitHost(tracked())
	opts.GitStatusInterval = 10 * time.Millisecond
	_, cmd := land(t, newModel(context.Background(), &fakeEngine{}, opts, nil))
	awaitGitMsg[gitStatusTickMsg](t, cmd, "the 10 ms tick after a good read")
}

func TestGitStatusWaitStretchesForASlowRead(t *testing.T) {
	t.Parallel()

	if got := gitStatusWait(5*time.Second, time.Second); got != 5*time.Second {
		t.Errorf("a 1 s read waits %v, want the 5 s interval", got)
	}
	if got := gitStatusWait(5*time.Second, 3*time.Second); got != 12*time.Second {
		t.Errorf("a 3 s read waits %v, want 4 × its duration", got)
	}
}

// The fetch chain runs only with the pref on and an upstream to fetch from; a landed fetch asks for
// a status read; the pref switched on arms the chain, and switched off retires it at its next step.
func TestGitStatusFetchNeedsThePrefAndAnUpstream(t *testing.T) {
	t.Parallel()

	noUpstream := tracked()
	noUpstream.Upstream, noUpstream.HasAheadBehind = "", false
	for _, tc := range []struct {
		name  string
		snap  gitstatus.Snapshot
		fetch bool
	}{
		{"pref off", tracked(), false},
		{"no upstream", noUpstream, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host := newFakeGitHost(tc.snap)
			m, cmd := land(t, gitModel(t, host, tc.fetch))
			gitMsgs(cmd, shortWindow)
			if _, fetches := host.calls(); fetches != 0 || m.git.fetchLive {
				t.Errorf("%d fetches, chain live %v; want none", fetches, m.git.fetchLive)
			}
		})
	}

	host := newFakeGitHost(tracked())
	m, cmd := land(t, gitModel(t, host, true))
	fetched := awaitGitMsg[gitFetchMsg](t, cmd, "the first fetch")
	statusBefore, _ := host.calls()
	m, cmd = stepCmd(t, m, fetched)
	awaitGitMsg[gitStatusMsg](t, cmd, "the status read a landed fetch asks for")
	if status, fetches := host.calls(); fetches != 1 || status != statusBefore+1 {
		t.Errorf("after one fetch: %d fetches and %d new reads, want 1 and 1", fetches, status-statusBefore)
	}

	// Switched off, the chain retires at its next tick and fetches no more.
	m, _, _, _, err := m.settingsApplyLocal(domain.UIKeyGitFetch, settingFalse)
	if err != nil {
		t.Fatalf("apply ui.git-fetch false: %v", err)
	}
	m, cmd = stepCmd(t, m, gitFetchTickMsg{gen: m.git.fetchGen})
	if cmd != nil || m.git.fetchLive {
		t.Errorf("a tick after the pref went off fetched (cmd %v) or left the chain live (%v)", cmd != nil, m.git.fetchLive)
	}

	// Switched on again, the apply arms a fresh chain and fetches now.
	_, _, cmd, _, err = m.settingsApplyLocal(domain.UIKeyGitFetch, settingTrue)
	if err != nil {
		t.Fatalf("apply ui.git-fetch true: %v", err)
	}
	awaitGitMsg[gitFetchMsg](t, cmd, "the fetch the pref switched on arms")
	if _, fetches := host.calls(); fetches != 2 {
		t.Errorf("%d fetches, want 2", fetches)
	}
}

func TestGitStatusNilHostArmsNothing(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)
	if m.git.gen != 0 || m.git.inFlight {
		t.Errorf("git = %+v, want the zero state with no host wired", m.git)
	}
	if m.gitStatusCmd() != nil || m.refreshGit() != nil || m.armGitFetch() != nil {
		t.Error("an unwired Model built a git Cmd")
	}
	after, cmd := stepCmd(t, m, turnSnapshotMsg{})
	if got := gitMsgs(cmd, shortWindow); len(got) != 0 {
		t.Errorf("a Turn end on an unwired Model issued %v", got)
	}
	after, cmd = stepCmd(t, after, gitStatusMsg{gen: 0, snap: tracked(), inside: true})
	if _, ok := after.gitSnapshot(); ok || cmd != nil {
		t.Error("a stray status Msg was folded into an unwired Model")
	}
}

// Exiting a program made by Build cancels a read still blocked in the host: the cleanup cancels
// the program context every git call runs under.
func TestGitStatusBuildExitCancelsABlockedRead(t *testing.T) {
	t.Parallel()

	host := newFakeGitHost(tracked())
	host.block = true
	opts := testOpts
	opts.Workspace = t.TempDir()
	opts.GitStatus = host
	program, cleanup, err := Build(context.Background(), &fakeEngine{}, NewBridge(), opts, &syncBuffer{},
		tea.WithInput(nil), tea.WithWindowSize(80, 24))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = program.Run()
	}()
	t.Cleanup(func() {
		program.Kill()
		<-done
	})
	select {
	case <-host.started:
	case <-time.After(testtime.Budget(5 * time.Second)):
		t.Fatal("Init's status read never reached the host")
	}
	program.Quit()
	select {
	case <-done:
	case <-time.After(testtime.Budget(5 * time.Second)):
		t.Fatal("the program did not exit on Quit")
	}
	cleanup()
	select {
	case err := <-host.ended:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("the blocked read ended with %v, want context.Canceled", err)
		}
	case <-time.After(testtime.Budget(5 * time.Second)):
		t.Fatal("the blocked read was still running after the program exited and was cleaned up")
	}
}

// The renderer runs no git itself: its production files import neither the hardened runner nor
// the subprocess layer under it — git runs behind [GitStatusHost], which cmd/apogee backs.
func TestGitStatusImportsNoGitRunner(t *testing.T) {
	t.Parallel()

	banned := map[string]bool{
		"github.com/airiclenz/apogee/internal/gitexec":    true,
		"github.com/airiclenz/apogee/internal/subprocess": true,
	}
	fset := token.NewFileSet()
	for _, path := range packageGoFiles(t, false) {
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imported := range file.Imports {
			importPath, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatalf("unquote import in %s: %v", path, err)
			}
			if banned[importPath] {
				t.Errorf("%s imports %q; git runs behind the GitStatusHost seam", path, importPath)
			}
		}
	}
}
