package main

// The cmd/apogee half of the TUI driver kit (ADR 0062): what it takes to start the REAL composition
// under a tuitest.Driver. The generic half — the terminal, the frames, the waits — is
// internal/tuitest; this file is only the part that cannot live there, because the launcher seam is
// in package main.
//
// A launch here is the whole thing: newRootCommand, the config resolution, the wiring, the Agent,
// the tools, the session store, the Bridge — everything the binary does, short of tui.Run's
// alternate screen and os.Stdout. That is the point of the seam. A test that drove tui.Model over a
// fake engine would prove the renderer renders; this proves apogee works.

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tui"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The terminal every e2e test starts in. Wide enough that the footer says what it has to say
// without truncating, tall enough for a transcript, a pane and the prompt box at once.
var e2eSize = tuitest.Size{W: 100, H: 30}

// e2eSession is one driven run of apogee: the home and workspace it owns, the upstream it talks to,
// and the driver it is being typed into. It outlives a single launch — [e2eSession.Relaunch] starts
// apogee again on the SAME home and workspace, which is how a test asserts anything about what a
// run left behind.
type e2eSession struct {
	t    *testing.T
	home string
	ws   string
	stub *stubllm.Server
	args []string

	drv  *tuitest.Driver
	out  strings.Builder
	done bool
}

// launchTUI starts apogee under drv against stub, in a temp home and a temp workspace of its own,
// and returns once the composition is running. args are extra command-line arguments; --config and
// --workspace are always supplied and must not be repeated.
//
// It never passes --tui-trace: the trace wraps os.Stdout and tui.Build refuses it beside a driver
// output (item 4). The in-process repaint measure is the driver's own screen counters instead.
func launchTUI(t *testing.T, drv *tuitest.Driver, stub *stubllm.Server, args ...string) *e2eSession {
	t.Helper()
	return launchTUIConfigured(t, drv, stub, "", args...)
}

// launchTUIConfigured is launchTUI with extraConfig appended to the home's config.yaml before the
// launch. It is the only way a driven run reaches a FILE-ONLY key: delegate-max-steps (T-04) has no
// flag and no environment variable, and the Agent reads it when it is CONSTRUCTED, so a test that
// needs one cannot set it once the run is up.
func launchTUIConfigured(t *testing.T, drv *tuitest.Driver, stub *stubllm.Server, extraConfig string,
	args ...string) *e2eSession {
	t.Helper()
	return launchTUIIn(t, drv, stub, "", extraConfig, args...)
}

// launchTUIIn is [launchTUIConfigured] in a workspace the CALLER built, rather than the seeded
// scratch one. It exists for the fixtures a test cannot ask for after the fact: T-12's hostile tree
// puts an escape sequence in the workspace ROOT's own name, and a root is named when it is created.
// An empty ws takes the seeded scratch workspace, which is what every other launch wants.
func launchTUIIn(t *testing.T, drv *tuitest.Driver, stub *stubllm.Server, ws, extraConfig string,
	args ...string) *e2eSession {
	t.Helper()

	e2eGuards(t)
	home := e2eHome(t, stub)
	appendHomeConfig(t, home, extraConfig)
	return startSession(t, drv, stub, home, ws, args...)
}

// launchTUIOn is [launchTUIIn] on a HOME the caller wrote, for the one key no helper can add after
// the fact: `llama-launcher:` sits on the session's own `servers:` entry (ADR 0029 D4), so a run
// that has the launcher integration on is a run whose entry said so when it was written.
// [appendHomeConfig] cannot reach inside a list item, and the entry is written before the launch
// that reads it.
func launchTUIOn(t *testing.T, drv *tuitest.Driver, stub *stubllm.Server, home, ws string,
	args ...string) *e2eSession {
	t.Helper()

	e2eGuards(t)
	return startSession(t, drv, stub, home, ws, args...)
}

// e2eGuards is what every driven launch registers before it creates anything: the leak check, the
// ambient-environment refusal, and the fast config watcher.
//
// The leak check goes first so it is the LAST cleanup to run and sees a tree that has already been
// torn down — whatever is still running then is a leak.
func e2eGuards(t *testing.T) {
	t.Helper()

	tuitest.CheckLeaks(t)
	assertNoAmbientApogeeConfig(t)
	driveConfigWatch(t)
}

// startSession builds the session around a home and a workspace and starts its first launch. An
// empty ws takes the seeded scratch one.
func startSession(t *testing.T, drv *tuitest.Driver, stub *stubllm.Server, home, ws string,
	args ...string) *e2eSession {
	t.Helper()

	if ws == "" {
		ws = e2eWorkspace(t)
	}
	s := &e2eSession{t: t, home: home, ws: ws, stub: stub, args: args}
	s.start(drv)
	return s
}

// driveConfigWatch runs the session's config watcher in milliseconds for the length of one test.
//
// The production cadence is a poll a second plus a quarter-second settle (internal/filewatch) — the
// right numbers for a human saving a document, and a second and a half of a test suite's budget for
// every save a driver makes. Every driven launch takes the fast one: a run that never touches
// config.yaml pays nothing for it, and the one test that does (T-16's watcher step) is the reason
// the seam exists.
func driveConfigWatch(t *testing.T) {
	t.Helper()

	was := configWatchTiming
	configWatchTiming = watchTiming{Interval: 50 * time.Millisecond, Settle: 50 * time.Millisecond}
	t.Cleanup(func() { configWatchTiming = was })
}

// appendHomeConfig adds lines to a home's config.yaml. It appends rather than rewrites so the
// `servers:` block e2eHome wrote — the whole reason the run has anything to talk to — survives.
func appendHomeConfig(t *testing.T, home, extra string) {
	t.Helper()

	if extra == "" {
		return
	}
	path := filepath.Join(home, "config.yaml")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the e2e home's config: %v", err)
	}
	if err := os.WriteFile(path, append(body, extra...), 0o600); err != nil {
		t.Fatalf("write the e2e home's config: %v", err)
	}
}

// start runs one launch under drv. The launcher seam is where the driver enters: it hands
// tui.Build its own output and its own program options, and Build appends them last so they win.
func (s *e2eSession) start(drv *tuitest.Driver) {
	s.t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	// Attached before the run so [tuitest.Driver.Kill] has a cancel even if the launcher is never
	// reached — a startup that fails must still be tearable down.
	drv.Attach(nil, cancel)

	launch := func(ctx context.Context, eng tui.Engine, br *tui.Bridge, opts tui.Options) error {
		program, cleanup, err := tui.Build(ctx, eng, br, opts, drv.Output(), drv.ProgramOptions()...)
		if err != nil {
			return err
		}
		defer cleanup()
		drv.Attach(program, cancel)
		_, err = program.Run()
		return err
	}

	cmd := newRootCommand(launch)
	cmd.SetArgs(append([]string{"--config", s.home, "--workspace", s.ws}, s.args...))
	cmd.SetOut(&s.out)
	cmd.SetErr(&s.out)
	go func() { drv.Finished(cmd.ExecuteContext(ctx)) }()

	s.drv = drv
	s.done = false
	s.t.Cleanup(func() {
		s.stop()
		// Closing the driver before the leak check runs is what makes the leak check honest: the
		// screen's answer pump is a tuitest goroutine and would otherwise be reported as the leak.
		drv.Close()
	})
	// The run is up once the first frame has reached the emulator.
	drv.WaitFor(func() bool { return drv.Screen().BytesWritten() > 0 },
		tuitest.Awaiting("apogee's first frame"))
}

// driven is the half of a driver an e2e helper needs: keys in, and the frame and the waits that
// answer them. Both drivers satisfy it — the in-process [tuitest.Driver] and the black-box
// [tuitest.PTYDriver] — which is what lets one helper serve both smoke tests rather than two
// copies of it drifting apart.
type driven interface {
	Type(text string)
	Press(key tuitest.Key)
	Frame() tuitest.Frame
	Screen() *tuitest.Screen
	WaitFor(cond func() bool, opts ...tuitest.Option)
	WaitText(text string)
	WaitGone(text string)
	WaitQuiet(quiet time.Duration)
}

var (
	_ driven = (*tuitest.Driver)(nil)
	_ driven = (*tuitest.PTYDriver)(nil)
)

// ptySession is one driven run of the SHIPPED BINARY: the home and workspace it owns, the pty it
// runs in, and the trace file it writes. It is the black-box counterpart of [e2eSession], and the
// two are deliberately separate types rather than one with a mode flag — what they can observe is
// not the same, and a helper that compiled for both while only working for one is how a black-box
// claim quietly becomes an in-process one.
type ptySession struct {
	t     *testing.T
	home  string
	ws    string
	trace string
	args  []string
	env   []string

	drv *tuitest.PTYDriver
}

// launchPTY starts apogee's built binary under a real pseudo-terminal, in a temp home and a temp
// workspace of its own, and returns once the first frame has reached the emulator. args are extra
// command-line arguments; --config, --workspace and --tui-trace are always supplied and must not be
// repeated. It skips when TestMain could not build the binary.
//
// Unlike [launchTUI] it DOES pass --tui-trace: this is the driver that paints into a real stdout,
// so the trace wraps the terminal it was written for, and [ptySession.TraceBytes] reads it back.
func launchPTY(t *testing.T, stub *stubllm.Server, args ...string) *ptySession {
	t.Helper()
	return launchPTYConfigured(t, stub, "", args...)
}

// launchPTYConfigured is [launchPTY] with extraConfig appended to the home's config.yaml before the
// binary is spawned — the black-box twin of [launchTUIConfigured], and the only way a PTY run
// reaches a file-only key. The Console family is the reason it exists: it ships OFF (ADR 0057), it
// is lifted by a `tools.enabled:` list with no flag behind it, and the tool registry is built once
// at startup, so a run that has Consoles is a run whose config said so before it launched.
func launchPTYConfigured(t *testing.T, stub *stubllm.Server, extraConfig string, args ...string) *ptySession {
	t.Helper()
	return launchPTYWithEnv(t, stub, extraConfig, nil, args...)
}

// launchPTYWithEnv is [launchPTYConfigured] with env appended to the child's WHOLE environment
// ([ptyEnv]) — entries of the `KEY=VALUE` form, applied to every spawn of this session, relaunches
// included.
//
// It exists for the one class of setting a driven run cannot reach any other way: a variable the
// standard library reads ONCE per process. HTTP_PROXY / HTTPS_PROXY / NO_PROXY are that class —
// net/http.ProxyFromEnvironment memoises the environment on first use (envProxyOnce) — so t.Setenv
// inside the test binary reaches nothing that has already made a request, and the only honest way
// to run apogee under an operator's egress proxy is to hand a CHILD the variables before it starts.
func launchPTYWithEnv(t *testing.T, stub *stubllm.Server, extraConfig string, env []string, args ...string) *ptySession {
	t.Helper()

	if e2eBinary == "" {
		t.Skipf("the binary under test was not built: %v", e2eBuildErr)
	}
	// Registered first so it is the last cleanup to run — see launchTUI.
	tuitest.CheckLeaks(t)
	assertNoAmbientApogeeConfig(t)

	home := e2eHome(t, stub)
	appendHomeConfig(t, home, extraConfig)
	s := &ptySession{
		t:     t,
		home:  home,
		ws:    e2eWorkspace(t),
		trace: filepath.Join(t.TempDir(), "tui-trace.txt"),
		args:  args,
		env:   env,
	}
	s.spawn()
	return s
}

// Relaunch starts the BINARY again on the same home and workspace, under a fresh pty — the black-box
// twin of [e2eSession.Relaunch], and the reopen half of every "what did the killed run leave
// behind?" claim (T-03). The previous child is killed first if it is somehow still up; after a
// [tuitest.PTYDriver.Kill] it is already gone and this is a no-op.
//
// The new run gets a trace file of its own. Reusing the first one would append two runs' paint into
// a single stream, and [ptySession.TraceBytes] would then answer about neither.
func (s *ptySession) Relaunch() *tuitest.PTYDriver {
	s.t.Helper()

	s.drv.Kill()
	s.trace = filepath.Join(s.t.TempDir(), "tui-trace.txt")
	s.spawn()
	return s.drv
}

// spawn starts one pty run of the binary on this session's home, workspace and trace path, and
// returns once its first frame has reached the emulator.
func (s *ptySession) spawn() {
	s.t.Helper()

	argv := append([]string{"--config", s.home, "--workspace", s.ws, "--tui-trace", s.trace}, s.args...)
	s.drv = tuitest.NewPTYDriver(s.t, e2eBinary, argv, ptyEnv(s.env...), e2eSize)
	s.drv.WaitFor(func() bool { return s.drv.Screen().BytesWritten() > 0 },
		tuitest.Awaiting("apogee's first frame"))
}

// ptyEnv is the WHOLE environment the launched binary gets: nothing is inherited, so what a driven
// run reads is exactly what this names, and an APOGEE_* variable in the developer's shell cannot
// reach it. HOME is the suite's throwaway one (TestMain's), never the developer's; PATH is there
// because apogee resolves the programs its tools spawn through it.
//
// extra is appended verbatim, so a caller can hand the child a variable this base set has no opinion
// about (the egress run's HTTP_PROXY, T-18) without widening what every other run inherits.
func ptyEnv(extra ...string) []string {
	return append([]string{
		"HOME=" + suiteTempHome,
		"USERPROFILE=" + suiteTempHome,
		"PATH=" + os.Getenv("PATH"),
	}, extra...)
}

// Home is the apogee home this run owns — a temp dir, never the real one.
func (s *ptySession) Home() string { return s.home }

// Workspace is the workspace root the file tools are scoped to.
func (s *ptySession) Workspace() string { return s.ws }

// readWorkspaceFile reads a file from the run's workspace, failing the test if it is not there.
func (s *ptySession) readWorkspaceFile(name string) string {
	s.t.Helper()

	data, err := os.ReadFile(filepath.Join(s.ws, name))
	if err != nil {
		s.t.Fatalf("read %s from the workspace: %v", name, err)
	}
	return string(data)
}

// TraceBytes and TraceFullRepaints are the black-box flicker measure (T-24), read back out of the
// --tui-trace file: how much the renderer painted, and how much of that repainted the world. They
// are NOT comparable with [e2eSession]'s in-process counters — a real pty is in raw mode and the
// in-process driver's output maps LF to CR LF — so a ceiling is pinned per driver.
//
// The file is re-read on every call rather than cached: it grows for as long as the run does, and a
// number taken before the run ended would answer a different question.
func (s *ptySession) TraceBytes() int64 { return s.traceScreen().BytesWritten() }

// TraceFullRepaints is how many traced writes carried a full-screen erase or a cursor-home.
func (s *ptySession) TraceFullRepaints() int { return s.traceScreen().FullRepaints() }

// traceScreen replays the trace into a closed screen — the picture the terminal ended on, and the
// counters that came with it.
func (s *ptySession) traceScreen() *tuitest.Screen {
	s.t.Helper()
	return tuitest.ReplayTrace(s.t, s.trace, e2eSize)
}

// Relaunch starts apogee again on the same home and workspace with a fresh driver — the reopen half
// of every "what did the last run leave behind?" claim. The previous run must have ended.
func (s *e2eSession) Relaunch() *tuitest.Driver {
	s.t.Helper()

	s.stop()
	drv := tuitest.NewDriver(s.t, e2eSize)
	s.start(drv)
	return drv
}

// RelaunchWith is [e2eSession.Relaunch] with extra arguments added to the command line, for this
// launch and every one after it. It exists because the arguments a REOPEN takes are not the ones the
// first run could have carried: --continue names the record the first run wrote, and --resume names
// its id, so neither can be passed at the launch that creates it.
func (s *e2eSession) RelaunchWith(extra ...string) *tuitest.Driver {
	s.t.Helper()

	s.args = append(s.args, extra...)
	return s.Relaunch()
}

// Wait blocks until the run returns and hands back its error — what the command tree returned,
// which for a clean quit is nil.
func (s *e2eSession) Wait() error {
	s.t.Helper()

	select {
	case err := <-s.drv.Done():
		s.done = true
		return err
	case <-time.After(tuitest.DefaultTimeout):
		s.t.Fatal("apogee did not return")
		return nil
	}
}

// Quit ends the run the way a human does and records that it ended.
func (s *e2eSession) Quit() error {
	s.t.Helper()

	err := s.drv.Quit()
	s.done = true
	return err
}

// stop ends a run that is still going, and is a no-op for one that already returned.
func (s *e2eSession) stop() {
	if s.done || s.drv == nil {
		return
	}
	select {
	case <-s.drv.Done():
	default:
		s.drv.Kill()
	}
	s.done = true
}

// Home is the apogee home this session runs in — a temp dir, never the real one.
func (s *e2eSession) Home() string { return s.home }

// Workspace is the workspace root the file tools are scoped to.
func (s *e2eSession) Workspace() string { return s.ws }

// Output is whatever the command tree printed outside the alternate screen (the startup notices,
// and the "Session saved · resume with" line on the way out).
func (s *e2eSession) Output() string { return s.out.String() }

// BytesWritten and FullRepaints are the in-process flicker measure (T-24): what the renderer painted
// into THIS driver's screen. They are not comparable with the PTY driver's trace counters —
// bubbletea maps \n to \r\n when its input is not a tty — so a ceiling is pinned per driver.
func (s *e2eSession) BytesWritten() int64 { return s.drv.Screen().BytesWritten() }

// FullRepaints is how many writes carried a full-screen erase or a cursor-home.
func (s *e2eSession) FullRepaints() int { return s.drv.Screen().FullRepaints() }

// Redactions are the substitutions every golden in this package applies before comparing: the two
// temp roots, the build version, today's date in a session title, and a relative age. Without them
// a golden re-recorded on Tuesday fails on Wednesday, and one recorded on this machine fails on
// every other.
func (s *e2eSession) Redactions() []tuitest.Redaction {
	return []tuitest.Redaction{
		tuitest.Redaction{Pattern: regexp.MustCompile(regexp.QuoteMeta(s.ws)), With: "<ws>"},
		tuitest.Redaction{Pattern: regexp.MustCompile(regexp.QuoteMeta(s.home)), With: "<home>"},
		tuitest.Redaction{Pattern: regexp.MustCompile(regexp.QuoteMeta(apogee.Version())), With: "<version>"},
		tuitest.Redact(`Session \d{4}-\d{2}-\d{2}`, "Session <date>"),
		tuitest.Redact(`\d+ (sec|min|hour|day)s? ago`, "<age>"),
	}
}

// e2eHome writes an apogee home whose one configured server is the stub, and returns it. A nil stub
// leaves an unreachable endpoint, which is a legitimate thing to test and never a hang: nothing
// asks the server anything at startup (ADR 0024 decision 8).
func e2eHome(t *testing.T, stub *stubllm.Server) string {
	t.Helper()

	if stub == nil {
		return upstreamHome(t, "http://127.0.0.1:1")
	}
	return upstreamHome(t, stub.URL, stub.Model)
}

// e2eWorkspace is the scratch workspace a driven run edits: one file with one line in it, which is
// enough for a list, a read and a write to have something to be about.
func e2eWorkspace(t *testing.T) string {
	t.Helper()

	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("seed the e2e workspace: %v", err)
	}
	return ws
}

// assertNoAmbientApogeeConfig neutralises the developer's own environment and pins the rule the
// whole kit rests on: a driven run reads the home this test made and no other. APOGEE_CONFIG in
// particular would silently move the home out from under --config's own resolution order.
func assertNoAmbientApogeeConfig(t *testing.T) {
	t.Helper()

	if home := os.Getenv(config.EnvConfig); home != "" {
		t.Fatalf("%s is set to %q; an e2e run must own its home", config.EnvConfig, home)
	}
	for _, name := range []string{
		config.EnvServer, config.EnvEndpoint, config.EnvModel, config.EnvMode, config.EnvBypass,
		config.EnvWorkspace,
	} {
		t.Setenv(name, "")
	}
}

// readWorkspaceFile reads a file from the session's workspace, failing the test if it is not there.
func (s *e2eSession) readWorkspaceFile(name string) string {
	s.t.Helper()

	data, err := os.ReadFile(filepath.Join(s.ws, name))
	if err != nil {
		s.t.Fatalf("read %s from the workspace: %v", name, err)
	}
	return string(data)
}

// sessionRecords lists the session record files this home holds, newest first is not promised —
// what a caller asserts on is how many there are and how big they got.
func (s *e2eSession) sessionRecords() []os.DirEntry {
	s.t.Helper()

	entries, err := os.ReadDir(filepath.Join(s.home, "sessions"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		s.t.Fatalf("read the session store: %v", err)
	}
	return entries
}

// compile-time proof that the launcher a driven run installs is the launcher type the root command
// takes — the seam, not a lookalike.
var _ launcher = func(context.Context, tui.Engine, *tui.Bridge, tui.Options) error { return nil }

// and that the program options a driver supplies are the options tui.Build appends.
var _ = func(d *tuitest.Driver) []tea.ProgramOption { return d.ProgramOptions() }

// ----------------------------------------------------------------------------
// Reading a driven frame
// ----------------------------------------------------------------------------
//
// The three ways a claim is made against a terminal that is wider and taller than any one
// assertion: one row of it, its whole text with the wrap taken back out, and — for a surface that
// does not fit at all — the pages of it walked in order.

// expandLastBlock opens the last toggleable block in the transcript — the keyboard route to the
// click the checklist describes. ⌥↑ enters the block cursor on the LAST stop and ⏎ toggles what it
// is standing on (blockcursor.go).
//
// It leaves the block cursor afterwards, which is not tidiness: while the cursor is active every
// repaint re-anchors the view on the line it stands on (followBlockCursor), so a ⇟ scrolls and is
// yanked straight back. A test that then walked a tall block would read the same page forever.
func expandLastBlock(drv *tuitest.Driver) {
	painted := drv.Screen().BytesWritten()
	drv.Press(tuitest.AltUp)
	drv.WaitFor(func() bool { return drv.Screen().BytesWritten() > painted },
		tuitest.Awaiting("the block cursor to highlight a block"))
	drv.Press(tuitest.Enter)
	drv.WaitQuiet(settled)
	drv.Press(tuitest.Esc)
	drv.WaitQuiet(settled)
}

// rowContaining is the frame's first row holding want, failing the test with the whole frame when
// no row does. It is how a claim about ONE line — a stats line, a footer — is made against a frame
// rather than against the frame's flattened text.
func rowContaining(t *testing.T, f tuitest.Frame, want string) string {
	t.Helper()

	for _, row := range f.Rows() {
		if strings.Contains(row, want) {
			return row
		}
	}
	t.Fatalf("no row of the frame holds %q:\n%s", want, f)
	return ""
}

// flatten collapses every run of whitespace in text to one space. A frame is wrapped to its width,
// so a SENTENCE the renderer had to break over two rows is only assertable this way; a claim about
// a single row uses rowContaining instead.
func flatten(text string) string { return strings.Join(strings.Fields(text), " ") }

// scrollTranscript walks the transcript from wherever it stands to its end, collecting every row it
// showed on the way, and returns them as one text. An expanded delegation is taller than the
// terminal — the child's whole run stands inside it — so a claim about a line near its end can only
// be made across frames.
//
// It stops when a page repaints into the same picture, which is what the bottom of a transcript
// looks like from outside. ⇟ scrolls the viewport in every state (model.go), block cursor included,
// so the walk costs nothing but the frames it reads.
func scrollTranscript(drv *tuitest.Driver) string {
	// A ceiling on the walk rather than an expectation: how many pages a block spans is the
	// renderer's business.
	const maxPages = 40

	var b strings.Builder
	last := ""
	for range maxPages {
		page := drv.Frame().String()
		if page == last {
			break // the press before this one moved nothing: the walk is at the transcript's end
		}
		last = page
		b.WriteString(page)
		b.WriteByte('\n')

		painted := drv.Screen().BytesWritten()
		drv.Press(tuitest.PgDown)
		// The byte counter, not the quiet check, is what says the press LANDED. A quiet check taken
		// straight after a keystroke passes on a screen that has simply not been painted yet, and a
		// walk built on one reads its first page twice and calls that the end.
		awaitRepaint(drv, painted)
		drv.WaitQuiet(60 * time.Millisecond)
	}
	return b.String()
}

// awaitRepaint blocks until the screen has been painted since it held `painted` bytes, and gives up
// after a bounded wait. It reports nothing: a press that paints nothing is the end of a list, and
// the caller finds that out from the page it reads next.
func awaitRepaint(drv *tuitest.Driver, painted int64) {
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if drv.Screen().BytesWritten() > painted {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
}
