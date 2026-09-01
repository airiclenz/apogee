package main

// T-16 of the v0.17.1 release checklist — "live state follows the running session" — as tests.
//
// It was manual because every claim in it is about state that moves WHILE a session runs, from
// somewhere the session is not: a rung reached at the keyboard, a `/confine` line typed into the
// prompt, a second process saving `config.yaml`, a launcher moving the session to another server.
// Three of those a driver simply does; the fourth is the one seam this item adds
// ([liveLauncherOps]), because the rebind a profile load ends in runs through the real wiring and a
// stub host would bypass the very move the step is about.
//
// What the tests assert is always the same shape: the surface is asked what it holds RIGHT NOW, and
// the answer has to be the engine's, never the snapshot the session booted with.

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	llamalauncher "github.com/airiclenz/llama-launcher/launcher"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The words the footer, the notes and the settings rows are read for. They are internal constants
// over in internal/tui (model.go's confinedWord/unconfinedWord/gatedWord, confine.go's note heads,
// settingswatcher.go's configWatchAppliedNote, settings.go's two markers) restated here because
// cmd/apogee cannot import them — which is the point: they are what the checklist promises a human
// will read, so a rename over there has to fail here.
const (
	askBeforeMarker = "◐ ask before"
	autoMarker      = "⏵⏵ auto"
	planMarker      = "⊞ plan"

	confinedFooterWord = "confined"
	gatedFooterWord    = "gated"
	unconfinedFooter   = autoMarker + " · unconfined"

	confineStatusHead = "/confine — auto mode's blast radius"
	confineOffHead    = "confinement OFF for this session"
	confineOnHead     = "confinement ON for this session"
	cannotFenceHere   = "commands cannot be fenced here"

	appliedNote = "config changed on disk — applied: "
	watchedMark = " ~"
	editedMark  = " *"

	// The two live-appliable keys the watcher step saves, and the block that sets them. They are
	// registry paths, which is what the applied-keys note names them by.
	watchedSchemeKey  = settingKeyColorScheme
	watchedCompactKey = "auto-compact"
	watchedConfigBody = "ui:\n  color-scheme: light\nauto-compact: false\n"

	// fanoutTask is the lead of every delegated task testdata/stubllm/fanout.yaml hands a child, and
	// is how a child's request is told from the parent's in the moved server's log.
	fanoutTask = "Count the"
)

// liveStateSize is the terminal these tests read the footer at. A hundred and forty columns rather
// than the usual hundred for the reason T-10's auto test takes the same width: the footer truncates
// the workdir cell from the left and DROPS the mode marker beside it when the row runs out of room,
// which a temp workspace path under a long test name manages at a hundred (item 11's note).
var liveStateSize = tuitest.Size{W: 140, H: 30}

// TestE2ELiveStateFollowsTheRunningSession is T-16 steps 1 to 10: the footer's Auto marker, the
// `/confine` verbs that move it, the two settings rows that must read the engine rather than the
// boot snapshot, and the one note a save on disk leaves behind.
func TestE2ELiveStateFollowsTheRunningSession(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "livestate"))
	drv := tuitest.NewDriver(t, liveStateSize)
	sess := launchTUI(t, drv, stub)
	waitIdle(drv)
	drv.WaitQuiet(settled)

	// Step 1 — the session boots on ask-before, and no confinement word rides that marker:
	// confinement attaches to Auto alone, so a word on any other rung would name a fence nothing
	// reads.
	booted := footerRow(t, drv.Frame())
	if !strings.Contains(booted, askBeforeMarker) {
		t.Fatalf("the session did not boot on ask-before: %q", booted)
	}
	for _, word := range []string{confinedFooterWord, gatedFooterWord} {
		if strings.Contains(booted, word) {
			t.Errorf("the ask-before marker carries the confinement word %q: %q", word, booted)
		}
	}

	// Step 2 — the Auto rung, and the word this HOST earns. Which word that is, is not the test's to
	// decide: `apogee probe` answers it for the machine the suite is running on, and the footer has
	// to say the same thing.
	word := hostConfinementWord(t, sess)
	reachRung(t, drv, autoMarker)
	if got, want := footerRow(t, drv.Frame()), autoMarker+" · "+word; !strings.Contains(got, want) {
		t.Fatalf("the Auto marker reads %q; want it to carry %q", got, want)
	}

	// Step 3 — and `/confine status` agrees with it, in its own words: a host that cannot fence says
	// so in the report exactly as the footer says it in one word.
	submit(drv, "/confine status")
	drv.WaitText(confineStatusHead)
	drv.WaitQuiet(settled)
	report := flatten(drv.Frame().String())
	if !strings.Contains(report, "setting: confined") {
		t.Errorf("the /confine report does not read the live setting back:\n%s", drv.Frame())
	}
	if got := strings.Contains(report, cannotFenceHere); got != (word == gatedFooterWord) {
		t.Errorf("the /confine report says %q = %v while the footer says %q", cannotFenceHere, got, word)
	}

	// Step 4 — the marker moves the moment `/confine` does, and the word it moves to is the one state
	// where Auto runs with the user's full privileges, painted in the error tone to say so.
	submit(drv, "/confine off")
	drv.WaitText(confineOffHead)
	drv.WaitQuiet(settled)
	off := drv.Frame()
	if got := footerRow(t, off); !strings.Contains(got, unconfinedFooter) {
		t.Fatalf("the marker still reads %q after /confine off", got)
	}
	assertErrorTone(t, off, unconfinedFooter)

	// Step 5 — and back, in the mode's own colour rather than the error tone.
	submit(drv, "/confine on")
	drv.WaitText(confineOnHead)
	drv.WaitQuiet(settled)
	on := drv.Frame()
	if got := footerRow(t, on); !strings.Contains(got, autoMarker+" · "+word) {
		t.Fatalf("the marker reads %q after /confine on; want the word %q back", got, word)
	}
	assertNoErrorTone(t, on, autoMarker+" · "+word)

	// Steps 6 and 7 — the pane reads the ENGINE. `mode` is the rung Shift-Tab reached, not the
	// ask-before the session booted in, and `confine-to-workspace` is what `/confine` last set, with
	// the pointer cell that says this row is not edited here.
	openSettings(drv)
	if got := settingsValue(t, drv, settingKeyMode); got != "auto" {
		t.Errorf("the mode row reads %q; want the live rung %q", got, "auto")
	}
	confine := settingsRow(t, drv, settingKeyConfineToWorkspace)
	if !strings.Contains(confine, "true") || !strings.Contains(confine, pointerConfine) {
		t.Errorf("the confine-to-workspace row reads %q; want the live value and %q", confine, pointerConfine)
	}

	// Step 8 — close, move one rung, reopen: a row still reading `auto` is the boot snapshot the fix
	// took away.
	closePane(drv, settingsHint)
	reachRung(t, drv, planMarker)
	openSettings(drv)
	if got := settingsValue(t, drv, settingKeyMode); got != "plan" {
		t.Errorf("the reopened mode row reads %q; want %q", got, "plan")
	}
	tuitest.Golden(t, "t16-settings-rows", drv.Frame(), goldenRedactions(sess)...)

	// The same frame the golden just recorded, read for ONE claim a golden cannot make on its own: a
	// registered config key with no row is invisible to the user, so the `system-prompt-layers:` key
	// (ADR 0067) has to be painted — and painted with the $EDITOR affordance, since a list of prose
	// blocks has no in-pane field to write it in.
	assertPanePaintsRow(t, drv.Frame(), "system-prompt-layers", pointerExternalEdit)

	closePane(drv, settingsHint)

	// Step 9 — a second process saves the config file. Exactly ONE line lands, and it names the keys
	// that moved: not one line per key, and not silence.
	appendHomeConfig(t, sess.Home(), watchedConfigBody)
	drv.WaitText(appliedNote)
	drv.WaitQuiet(settled)
	applied := drv.Frame()
	if n := rowsContaining(applied, appliedNote); n != 1 {
		t.Errorf("a save on disk left %d applied-keys lines; want exactly one:\n%s", n, applied)
	}
	note := rowContaining(t, applied, appliedNote)
	for _, key := range []string{watchedSchemeKey, watchedCompactKey} {
		if !strings.Contains(note, key) {
			t.Errorf("the applied-keys note %q does not name %q", note, key)
		}
	}

	// Step 10 — the rows the file moved wear the watcher's ` ~`, and an edit made in the pane over
	// one of them flips it to the ` *` that says this session wrote it.
	openSettings(drv)
	for _, key := range []string{watchedSchemeKey, watchedCompactKey} {
		if row := settingsRow(t, drv, key); !strings.HasSuffix(row, watchedMark) {
			t.Errorf("the row %q carries no %q after a save on disk moved it", row, watchedMark)
		}
	}
	settingsGoDown(t, drv, watchedCompactKey)
	pressAndRepaint(drv, tuitest.Enter)
	if row := settingsCursor(drv); !strings.HasSuffix(row, editedMark) {
		t.Errorf("the row %q keeps the watcher's mark after an edit in the pane", row)
	}
	closePane(drv, settingsHint)

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2ELiveStateLauncherMoveKeepsTheSessionWorking is T-16 steps 11 and 12: `/model` lists the
// launcher's Launch profiles, picking one that serves ELSEWHERE moves the session onto it, and the
// moved session still delegates.
//
// The launcher is the fake the bridge's own unit tests use, swapped in at [liveLauncherOps] — the
// seam this item adds — so the whole of the real wiring runs: the picker's rows are assembled by
// launchProfiles, the load resolves through the facade's own Config, and the rebind goes through
// sessionMover exactly as a `/server` switch does. What is faked is the launcher, and nothing above
// it.
//
// Step 12 is asserted as "the fan-out still happens", not as "more than one child at a time": the
// width a moved session fans out at resolves from the new entry's pin and the server's own
// `total_slots`, and stubllm answers no `/props`, so the cap is 1 whatever the move did. The
// re-follow itself is unit-covered (upstream_test.go), as the checklist's own Part line says.
func TestE2ELiveStateLauncherMoveKeepsTheSessionWorking(t *testing.T) {
	first := stubllm.New(t, loadScript(t, "livestate"))
	second := stubllm.New(t, loadScript(t, "fanout"))
	fake := installFakeLauncher(t, first, second)

	drv := tuitest.NewDriver(t, liveStateSize)
	sess := launchTUIOn(t, drv, first, launcherHome(t, first), "")
	waitIdle(drv)
	drv.WaitQuiet(settled)

	// Step 11 — the picker is the launcher's own list: the profile on the session's own server, and
	// the one that serves somewhere else, which is the one that moves anything.
	submit(drv, "/model")
	drv.WaitText("switch model")
	drv.WaitQuiet(settled)
	for _, name := range []string{"alpha", "beta"} {
		if _, _, ok := drv.Frame().Find(name); !ok {
			t.Fatalf("the picker does not list the Launch profile %q:\n%s", name, drv.Frame())
		}
	}

	// The load BLOCKS until this test lets it through, which is the only way the footer's own
	// readout for it is a frame anyone can catch: a fake that returned at once would be finished
	// before the first repaint.
	pressAndRepaint(drv, tuitest.Down)
	drv.Press(tuitest.Enter)
	drv.WaitText("loading beta")
	close(fake.release)

	// The session ends up bound to the profile's server, and the request log is what says so: the
	// next prompt is answered by the SECOND stub and the first is never asked again.
	drv.WaitGone("loading beta")
	drv.WaitQuiet(settled)
	before := len(first.Requests())
	submit(drv, "Hello from the new server.")
	drv.WaitFor(func() bool { return len(second.Requests()) > 0 },
		tuitest.Awaiting("the moved session to ask the profile's server"))
	if got := len(first.Requests()); got != before {
		t.Errorf("the first server was asked %d more times after the move", got-before)
	}

	// Step 12 — and the moved session still fans out: three delegations in one reply, all three
	// carried out against the new server.
	submit(drv, "Fan out three delegations.")
	drv.WaitFor(func() bool { return childRequests(second, fanoutTask) >= 3 },
		tuitest.Awaiting("all three delegates to run against the new server"))
	// The request log leads the screen: a child's request is made before the block reporting it is
	// committed, so the LAST delegate's block is waited for rather than read off the frame the
	// request count just unblocked.
	drv.WaitText("gamma")
	drv.WaitQuiet(settled)
	fanned := drv.Frame()
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if _, _, ok := fanned.Find(name); !ok {
			t.Errorf("the fan-out has no block for the delegate %q:\n%s", name, fanned)
		}
	}

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// ----------------------------------------------------------------------------
// The helpers
// ----------------------------------------------------------------------------

// hostConfinementWord is the word THIS host earns in the footer's Auto marker, read off `apogee
// probe` exactly as the checklist's own preconditions tell a human to read it: a backend that can
// fence terminal commands is `confined`, one that cannot is `gated`.
func hostConfinementWord(t *testing.T, sess *e2eSession) string {
	t.Helper()

	report := runProbe(t, newProbeCommand(), sess.Home(), sess.Workspace())
	if strings.Contains(reportLine(t, report, "auto:"), "NOT eligible") {
		return gatedFooterWord
	}
	return confinedFooterWord
}

// reachRung walks the mode ladder with Shift-Tab until the footer's marker is the one asked for.
//
// It walks by the MARKER rather than by a count of presses because the ladder's order is the
// ladder's business: the checklist says three presses reach Auto and two do on today's ordering, and
// a test that pinned the count would fail on a reordering that broke nothing.
func reachRung(t *testing.T, drv *tuitest.Driver, marker string) {
	t.Helper()

	// A ceiling on the walk rather than an expectation: how many rungs there are is the ladder's.
	const maxRungs = 8

	for range maxRungs {
		if strings.Contains(footerRow(t, drv.Frame()), marker) {
			return
		}
		pressAndRepaint(drv, tuitest.ShiftTab)
	}
	t.Fatalf("the ladder never reached %q (it stopped on %q)", marker, footerRow(t, drv.Frame()))
}

// openSettings opens the settings pane and waits for it to be there.
func openSettings(drv *tuitest.Driver) {
	submit(drv, "/settings")
	drv.WaitText(settingsHint)
	drv.WaitQuiet(settled)
}

// assertPanePaintsRow reads the settings pane out of a painted frame and fails unless exactly one
// row names key and that row carries want. It reads the frame it is HANDED rather than walking the
// pane, so it can check a claim about the very screen a golden recorded without moving the cursor
// that golden captured.
func assertPanePaintsRow(t *testing.T, frame tuitest.Frame, key, want string) {
	t.Helper()

	var painted []string
	for _, line := range strings.Split(frame.String(), "\n") {
		if strings.Contains(line, key+" ") {
			painted = append(painted, line)
		}
	}
	if len(painted) != 1 {
		t.Fatalf("the settings pane paints %d rows naming %q; want exactly one:\n%s", len(painted), key, frame)
	}
	if !strings.Contains(painted[0], want) {
		t.Errorf("the %q row reads %q; want it to carry %q", key, strings.TrimSpace(painted[0]), want)
	}
}

// settingsRow walks the pane to the row naming key and returns it, marker and all.
func settingsRow(t *testing.T, drv *tuitest.Driver, key string) string {
	t.Helper()

	settingsGoDown(t, drv, key)
	return settingsCursor(drv)
}

// settingsGoDown is [settingsGoTo] the other way round. The keys this file asks for sit in the
// order the pane already walks — `mode` two rows below the top, then the confinement, interface and
// session rows below it — so ↓ reaches each of them in a handful of presses where ↑ would walk the
// whole registry backwards for every one. The list wraps, so the walk always terminates.
func settingsGoDown(t *testing.T, drv *tuitest.Driver, key string) {
	t.Helper()

	// A ceiling on the walk rather than an expectation: the registry decides how many rows there are.
	const maxRows = 100

	for range maxRows {
		if strings.HasPrefix(settingsCursor(drv), "❯ "+key+" ") {
			return
		}
		if !stepSettings(drv, tuitest.Down) {
			break
		}
	}
	t.Fatalf("the settings list never highlighted %q (it stopped on %q)", key, settingsCursor(drv))
}

// settingsValue is the VALUE cell of the row naming key: the first field after the key itself, which
// is what a claim about what a row "reads" is about.
func settingsValue(t *testing.T, drv *tuitest.Driver, key string) string {
	t.Helper()

	row := settingsRow(t, drv, key)
	// "❯", the key, then the value — a row with no value cell at all has nothing to report.
	fields := strings.Fields(row)
	if len(fields) < 3 {
		t.Fatalf("the %q row carries no value cell: %q", key, row)
	}
	return fields[2]
}

// rowsContaining counts the rows of a frame holding want. It is how "exactly one line" is asserted:
// the claim is about how many the note is, and a Contains would answer for any number at all.
func rowsContaining(f tuitest.Frame, want string) int {
	n := 0
	for _, row := range f.Rows() {
		if strings.Contains(row, want) {
			n++
		}
	}
	return n
}

// launcherHome writes an apogee home whose one server entry has the launcher integration ON. It is
// spelled out here rather than taken from [e2eHome] because `llama-launcher:` sits INSIDE the
// `servers:` entry (ADR 0029 D4) and no line appended to the file afterwards can reach in there.
//
// The value is `auto` — the launcher's own default config path, taken verbatim. Nothing reads that
// path: the fake below answers loadConfig with a parsed fixture whatever it is handed, which is
// exactly what keeps this test off whatever launcher config the developer's machine may hold.
func launcherHome(t *testing.T, stub *stubllm.Server) string {
	t.Helper()

	home := t.TempDir()
	body := "servers:\n  - name: probe-target\n    endpoint: " + stub.URL +
		"\n    model: " + stub.Model + "\n    llama-launcher: auto\nserver: probe-target\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write the launcher home's config: %v", err)
	}
	return home
}

// installFakeLauncher swaps the composition's launcher seam for a scripted fake with two Launch
// profiles — one serving where the session already is, one serving at the SECOND stub — and restores
// the production adapter when the test ends.
func installFakeLauncher(t *testing.T, here, elsewhere *stubllm.Server) *gatedLauncher {
	t.Helper()

	cfg := launcherFixture(t, []string{"alpha.gguf", "beta.gguf"}, `
servers:
  llamacpp: true
defaults:
  server: llamacpp
  host: 127.0.0.1
profiles:
  alpha:
    model: alpha.gguf
    port: `+strconv.Itoa(stubPort(t, here.URL))+`
  beta:
    model: beta.gguf
    port: `+strconv.Itoa(stubPort(t, elsewhere.URL))+`
`)
	fake := &gatedLauncher{
		fakeLauncher: &fakeLauncher{cfg: cfg, loadResult: &llamalauncher.RunningInstance{
			Backend: "llamacpp", Host: "127.0.0.1", Port: stubPort(t, elsewhere.URL),
			ActiveProfile: "beta"}},
		release: make(chan struct{}),
	}
	was := liveLauncherOps
	liveLauncherOps = fake
	t.Cleanup(func() { liveLauncherOps = was })
	return fake
}

// gatedLauncher is [fakeLauncher] with the one blocking verb held open until the test says so. A
// real profile load takes seconds to minutes (the facade's contract), and the footer's
// "loading <name>…" readout only exists while it runs — so a fake that returned immediately would
// make the one frame the step is about unobservable rather than fast.
type gatedLauncher struct {
	*fakeLauncher
	release chan struct{}
}

func (g *gatedLauncher) loadProfile(cfg *llamalauncher.Config, p *llamalauncher.ResolvedProfile,
	restart bool, progress func(string), notice func(string)) (*llamalauncher.RunningInstance, bool, error) {
	<-g.release
	return g.fakeLauncher.loadProfile(cfg, p, restart, progress, notice)
}

// stubPort is the port a stub server is listening on — what a Launch profile has to name for the
// session to land on that server when it follows the profile.
func stubPort(t *testing.T, rawURL string) int {
	t.Helper()

	_, port, err := net.SplitHostPort(strings.TrimPrefix(rawURL, "http://"))
	if err != nil {
		t.Fatalf("read the stub's port from %q: %v", rawURL, err)
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("the stub's port %q is not a number: %v", port, err)
	}
	return n
}
