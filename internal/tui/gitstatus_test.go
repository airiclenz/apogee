package tui

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"slices"
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
// inside flag and error, counts the calls and records the root each one read. With block set, a Status call parks until release is
// closed or its ctx ends, recording the ctx error it saw — the slow read and the exit tests. A root
// byRoot names answers its own snapshot instead, so a watched workspace's branch reads apart from
// the host's.
type fakeGitHost struct {
	mu       sync.Mutex
	snap     gitstatus.Snapshot
	byRoot   map[string]gitstatus.Snapshot
	inside   bool
	err      error
	fetchErr error

	block   bool
	release chan struct{}
	started chan struct{} // one send per Status call, when block is set
	ended   chan error    // the ctx error a blocked call returned with

	statusCalls int
	fetchCalls  int
	statusRoots []string
	fetchRoots  []string
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

func (f *fakeGitHost) Status(ctx context.Context, root string) (gitstatus.Snapshot, bool, error) {
	f.mu.Lock()
	f.statusCalls++
	f.statusRoots = append(f.statusRoots, root)
	block, snap, inside, err := f.block, f.snap, f.inside, f.err
	if rooted, ok := f.byRoot[root]; ok {
		snap = rooted
	}
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

func (f *fakeGitHost) Fetch(_ context.Context, root string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetchCalls++
	f.fetchRoots = append(f.fetchRoots, root)
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

// roots returns the roots the Status and Fetch calls read so far, in call order.
func (f *fakeGitHost) roots() (status, fetch []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.statusRoots), slices.Clone(f.fetchRoots)
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

// foldAll folds msgs into m in order.
func foldAll(t *testing.T, m Model, msgs []tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		m = step(t, m, msg)
	}
	return m
}

// statusReads is the status reads among msgs.
func statusReads(msgs []tea.Msg) []gitStatusMsg {
	var reads []gitStatusMsg
	for _, msg := range msgs {
		if read, ok := msg.(gitStatusMsg); ok {
			reads = append(reads, read)
		}
	}
	return reads
}

// watchWithWorkspace is a watch fixture whose source names workspace as the watched session's.
func watchWithWorkspace(t *testing.T, workspace string) *watchFixture {
	t.Helper()
	f := newWatchFixture(t, "watched", recordHeadEntries(), 3)
	f.src.workspace = workspace
	return f
}

// TestWatchReadsGitInTheWatchedWorkspace pins the one status chain's root across a watch: it reads
// the watched workspace while watching, keeps that snapshot apart from the host's, runs no fetch,
// reads nothing where the watch names no workspace, and reads the host again at once on leaving —
// a watched read still in flight then landing inert. The Cmds enterWatch and leaveWatch return are
// batches, so they are expanded rather than run as one Msg.
func TestWatchReadsGitInTheWatchedWorkspace(t *testing.T) {
	t.Parallel()

	t.Run("entering and leaving move the chain", func(t *testing.T) {
		t.Parallel()
		host := newFakeGitHost(tracked())
		m := gitModel(t, host, true)
		hostRoot, watchedRoot := m.opts.Workspace, t.TempDir()
		m, _ = land(t, m)
		if !m.git.fetchLive {
			t.Fatal("setup: the host read armed no fetch chain")
		}
		host.set(func(f *fakeGitHost) { f.snap = gitstatus.Snapshot{Branch: "theirs"} })

		m, cmd := m.enterWatch(watchWithWorkspace(t, watchedRoot).src)
		if !m.git.inFlight || m.gitFetchWanted() {
			t.Errorf("after entry inFlight %v, fetch wanted %v; want the watched read in flight and no fetch",
				m.git.inFlight, m.gitFetchWanted())
		}
		m = foldAll(t, m, expandBatch(cmd))
		if status, _ := host.roots(); !slices.Equal(status, []string{hostRoot, watchedRoot}) {
			t.Errorf("status roots = %q; want the host's, then the watched workspace's", status)
		}
		if !m.git.hasSnap || m.git.snap.Branch != "main" {
			t.Errorf("host snapshot = %+v (%v); want the host's last read untouched", m.git.snap, m.git.hasSnap)
		}
		if !m.watch.hasGitSnap || m.watch.gitSnap.Branch != "theirs" {
			t.Errorf("watched snapshot = %+v (%v); want the watched read", m.watch.gitSnap, m.watch.hasGitSnap)
		}

		// The fetch in flight at entry lands: the chain retires and no further fetch runs. Its
		// status refresh reads the watched workspace, and is still in flight when the watch ends.
		m, cmd = stepCmd(t, m, gitFetchMsg{gen: m.git.fetchGen})
		if m.git.fetchLive {
			t.Error("the fetch chain stayed live during the watch")
		}
		var late []gitStatusMsg
		for _, msg := range gitMsgs(cmd, shortWindow) {
			switch msg := msg.(type) {
			case gitFetchMsg, gitFetchTickMsg:
				t.Errorf("the watch issued a fetch step %#v", msg)
			case gitStatusMsg:
				late = append(late, msg)
			}
		}
		if _, fetches := host.calls(); fetches != 0 {
			t.Errorf("fetch calls = %d during the watch; want none", fetches)
		}
		if len(late) != 1 || !m.git.inFlight {
			t.Fatalf("the landed fetch refreshed %d reads, inFlight %v; setup wants one watched read in flight",
				len(late), m.git.inFlight)
		}

		host.set(func(f *fakeGitHost) { f.snap = tracked() })
		m, cmd = stepCmd(t, m, keyEsc())
		if m.watch.isActive || !m.git.inFlight {
			t.Fatalf("after esc watching %v, inFlight %v; want the host read issued at once", m.watch.isActive, m.git.inFlight)
		}
		leaving := expandBatch(cmd)
		if status, _ := host.roots(); status[len(status)-1] != hostRoot {
			t.Errorf("status roots = %q; want the host's read last, on leaving", status)
		}
		m = step(t, m, late[0])
		if !m.git.inFlight || m.git.snap.Branch != "main" {
			t.Errorf("a late watched read folded after the watch: snapshot %+v, inFlight %v", m.git.snap, m.git.inFlight)
		}
		m = foldAll(t, m, leaving)
		if m.git.inFlight || !m.git.fetchLive {
			t.Errorf("after the host read inFlight %v, fetch live %v; want it landed and the fetch chain re-armed",
				m.git.inFlight, m.git.fetchLive)
		}
	})

	t.Run("an unknown workspace reads nothing until esc", func(t *testing.T) {
		t.Parallel()
		host := newFakeGitHost(tracked())
		m := gitModel(t, host, false)
		hostRoot := m.opts.Workspace
		m, _ = land(t, m)

		m, cmd := m.enterWatch(watchWithWorkspace(t, "").src)
		msgs := expandBatch(cmd)
		if m.git.inFlight || len(statusReads(msgs)) != 0 {
			t.Errorf("entry issued %d reads, inFlight %v; want none", len(statusReads(msgs)), m.git.inFlight)
		}
		m = foldAll(t, m, msgs)
		refreshed := m
		if read := refreshed.refreshGit(); read != nil || refreshed.git.inFlight {
			t.Errorf("refreshGit while watching an unknown workspace issued a read (inFlight %v)", refreshed.git.inFlight)
		}
		if ticked, read := m.foldGitStatusTick(gitStatusTickMsg{gen: m.git.gen}); read != nil || ticked.(Model).git.inFlight {
			t.Error("a tick while watching an unknown workspace issued a read")
		}

		m, cmd = stepCmd(t, m, keyEsc())
		reads := statusReads(expandBatch(cmd))
		if !m.git.inFlight || len(reads) != 1 || reads[0].gen != m.git.gen {
			t.Errorf("esc issued %d reads, inFlight %v; want the host read of the live generation", len(reads), m.git.inFlight)
		}
		if status, _ := host.roots(); !slices.Equal(status, []string{hostRoot, hostRoot}) {
			t.Errorf("status roots = %q; want the host's only", status)
		}
	})

	t.Run("a --resume start watch runs one chain at the watched root", func(t *testing.T) {
		t.Parallel()
		host := newFakeGitHost(tracked())
		f := newWatchFixture(t, "", recordHeadEntries(), 0)
		watchedRoot := t.TempDir()
		opts := testOpts
		opts.Workspace, opts.GitStatus = t.TempDir(), host
		opts.Watch = &LiveSession{
			ID: f.src.id, RecordPath: f.src.recordPath, JournalPath: f.src.journalPath, Workspace: watchedRoot,
		}

		m := newTestModelEng(t, watchEngine(), opts)

		if !m.watch.isActive || m.git.gen != 1 || !m.git.inFlight {
			t.Fatalf("watching %v, git %+v; want the watch entered and Init's read armed on generation 1",
				m.watch.isActive, m.git)
		}
		if reads := statusReads(expandBatch(m.startupWatchRead)); len(reads) != 0 {
			t.Errorf("the startup watch read carries %d git reads; want Init's to be the only one", len(reads))
		}
		read := awaitGitMsg[gitStatusMsg](t, m.Init(), "Init's status read")
		<-time.After(shortWindow)
		if status, _ := host.roots(); read.gen != 1 || !slices.Equal(status, []string{watchedRoot}) {
			t.Errorf("Init read generation %d at %q; want one read, generation 1, at the watched root", read.gen, status)
		}
		m = step(t, m, read)
		if !m.watch.hasGitSnap || m.git.hasSnap {
			t.Errorf("watched snapshot held %v, host snapshot held %v; want the read in the watch's slot only",
				m.watch.hasGitSnap, m.git.hasSnap)
		}
	})

	t.Run("a --resume start watch of an unknown workspace reads the host on esc", func(t *testing.T) {
		t.Parallel()
		host := newFakeGitHost(tracked())
		f := newWatchFixture(t, "", recordHeadEntries(), 0)
		opts := testOpts
		opts.Workspace, opts.GitStatus = t.TempDir(), host
		opts.Watch = &LiveSession{ID: f.src.id, RecordPath: f.src.recordPath, JournalPath: f.src.journalPath}

		m := newTestModelEng(t, watchEngine(), opts)

		if !m.watch.isActive || m.git.inFlight || m.gitStatusCmd() != nil {
			t.Fatalf("watching %v, inFlight %v; want the watch entered with no read in flight or to issue",
				m.watch.isActive, m.git.inFlight)
		}
		m, cmd := stepCmd(t, m, keyEsc())
		reads := statusReads(expandBatch(cmd))
		if !m.git.inFlight || len(reads) != 1 {
			t.Fatalf("esc issued %d reads, inFlight %v; want the host read at once", len(reads), m.git.inFlight)
		}
		if status, _ := host.roots(); !slices.Equal(status, []string{opts.Workspace}) {
			t.Errorf("status roots = %q; want the host's only", status)
		}
	})

	t.Run("⏎ resume never waits for the host re-read", func(t *testing.T) {
		t.Parallel()
		f, probe := probedFixture(t)
		host := newFakeGitHost(tracked())
		host.set(func(f *fakeGitHost) { f.block = true })
		t.Cleanup(func() { close(host.release) })
		eng := watchEngine()
		m := newModel(context.Background(), eng, Options{
			Sessions: ownerHostFor(t, f), UI: testUIPrefs, Workspace: t.TempDir(), GitStatus: host,
		}, nil)
		m = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
		m.transcript.addUser("my own question", nil)
		m.cacheBoundary(domain.Session{})
		m = endWatchedOwner(t, m, f, probe, true)
		if !m.isWatchResumable() {
			t.Fatalf("legend %q; setup: the watch did not reach the resumable finish", m.legend())
		}

		_, cmd := stepCmd(t, m, keyEnter())
		out := runCmdTree(cmd)
		budget := time.After(testtime.Budget(5 * time.Second))
		select {
		case <-host.started:
		case <-budget:
			t.Fatal("⏎ issued no host re-read")
		}
		for {
			select {
			case msg := <-out:
				if loaded, ok := msg.(sessionLoadedMsg); ok {
					if loaded.err != nil {
						t.Fatalf("the resume's load failed: %v", loaded.err)
					}
					return
				}
			case <-budget:
				t.Fatal("the record load waited behind the blocked host re-read")
			}
		}
	})
}
