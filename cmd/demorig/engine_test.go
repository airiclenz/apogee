//go:build !windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/term"
)

// The fake TUI is this test binary re-executed with fakeTUIEnv naming a directory: it puts its tty
// in raw mode, paints the directory's screen file, and logs every read of its input as one quoted
// line of the input log — so a test sees exactly the writes the program would have read. When the
// directory also holds a next-screen file, the first input it reads repaints the screen with it.
const (
	fakeTUIEnv        = "DEMORIG_FAKE_TUI"
	fakeScreenFile    = "screen.txt"
	fakeNextFile      = "next.txt"
	fakeInputLogFile  = "input.log"
	engineTestCols    = 60
	engineTestRows    = 12
	engineTestTimeout = 5 * time.Second
)

// TestFakeTUIHelper is the fake TUI's entry point in the re-executed binary; in a normal run it
// skips.
func TestFakeTUIHelper(t *testing.T) {
	dir := os.Getenv(fakeTUIEnv)
	if dir == "" {
		t.Skip("the engine tests' fake TUI; runs only when re-executed")
	}
	os.Exit(runFakeTUI(dir))
}

// runFakeTUI is the fake TUI's whole life; it returns when its input closes.
func runFakeTUI(dir string) int {
	if _, err := term.MakeRaw(os.Stdin.Fd()); err != nil {
		fmt.Fprintln(os.Stderr, "fake tui: raw mode:", err)
		return 1
	}
	paint := func(name string) bool {
		screen, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return false
		}
		// Raw mode turns output processing off, so a newline must carry its own return.
		text := strings.ReplaceAll(strings.TrimRight(string(screen), "\n"), "\n", "\r\n")
		_, _ = os.Stdout.WriteString("\x1b[H\x1b[2J" + text)
		return true
	}
	paint(fakeScreenFile)
	inputLog, err := os.OpenFile(filepath.Join(dir, fakeInputLogFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 1
	}
	defer inputLog.Close() //nolint:errcheck // the fake dies with its test
	isRepainted := false
	buf := make([]byte, 256)
	for {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			_, _ = fmt.Fprintf(inputLog, "%q\n", buf[:n])
			if !isRepainted {
				isRepainted = paint(fakeNextFile)
			}
		}
		if err != nil {
			return 0
		}
	}
}

// startFakeTUI runs the fake TUI on screen (and next, when not empty) and returns the terminal
// and the input log's path, once the screen's first row is painted.
func startFakeTUI(t *testing.T, screen, next string) (*Terminal, string) {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, fakeScreenFile), screen)
	if next != "" {
		writeFile(t, filepath.Join(dir, fakeNextFile), next)
	}
	terminal, err := StartTerminal(os.Args[0], []string{"-test.run=^TestFakeTUIHelper$"}, TermOptions{
		Cols: engineTestCols, Rows: engineTestRows, FPS: 30,
		Env: append(os.Environ(), fakeTUIEnv+"="+dir),
	})
	if err != nil {
		t.Fatalf("StartTerminal: %v", err)
	}
	t.Cleanup(func() { terminal.Close() })

	firstRow := strings.TrimSpace(strings.SplitN(screen, "\n", 2)[0])
	wait := Action{Wait: &WaitAction{Screen: regexpQuote(firstRow), Timeout: engineTestTimeout}}
	if err := NewEngine(terminal).RunBeat(context.Background(), Beat{ID: 99, Title: "ready", Do: []Action{wait}}); err != nil {
		t.Fatalf("the fake TUI never painted: %v", err)
	}
	return terminal, filepath.Join(dir, fakeInputLogFile)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// regexpQuote escapes a literal for a wait's regex.
func regexpQuote(text string) string {
	var quoted strings.Builder
	for _, character := range text {
		if strings.ContainsRune(`\.+*?()|[]{}^$`, character) {
			quoted.WriteRune('\\')
		}
		quoted.WriteRune(character)
	}
	return quoted.String()
}

// awaitInputs polls the input log until it holds want reads, and returns them unquoted.
func awaitInputs(t *testing.T, path string, want int) []string {
	t.Helper()
	deadline := time.Now().Add(engineTestTimeout)
	for {
		raw, _ := os.ReadFile(path)
		lines := strings.Fields(string(raw))
		if len(lines) >= want || time.Now().After(deadline) {
			reads := make([]string, 0, len(lines))
			for _, line := range lines {
				read, err := strconv.Unquote(line)
				if err != nil {
					t.Fatalf("input log line %q: %v", line, err)
				}
				reads = append(reads, read)
			}
			if len(reads) != want {
				t.Fatalf("the program read %d writes %q, want %d", len(reads), reads, want)
			}
			return reads
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// awaitStream polls the input log until the program has read want's length in bytes, and fails
// unless what it read is exactly want.
func awaitStream(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(engineTestTimeout)
	for {
		raw, _ := os.ReadFile(path)
		var stream strings.Builder
		for _, line := range strings.Fields(string(raw)) {
			read, err := strconv.Unquote(line)
			if err != nil {
				t.Fatalf("input log line %q: %v", line, err)
			}
			stream.WriteString(read)
		}
		if stream.Len() >= len(want) || time.Now().After(deadline) {
			if stream.String() != want {
				t.Fatalf("the program read %q, want %q", stream.String(), want)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// clickBeat is a one-action beat clicking target times times.
func clickBeat(target Target, times int) Beat {
	return Beat{ID: 1, Title: "click", Do: []Action{{Click: &ClickAction{Target: target, Times: times}}}}
}

// engineEvents decodes the take's engine events of kind.
func engineEvents(t *testing.T, take *Take, kind string) []EngineEventDetail {
	t.Helper()
	var details []EngineEventDetail
	for _, event := range take.Events {
		if event.Kind != kind {
			continue
		}
		var detail EngineEventDetail
		if err := json.Unmarshal([]byte(event.Detail), &detail); err != nil {
			t.Fatalf("%s event detail %q: %v", kind, event.Detail, err)
		}
		details = append(details, detail)
	}
	return details
}

// hudScreen is apogee's bottom chrome in miniature: transcript, the `▔` top rule, the status
// line, a staged `⧖` row, the prompt box, the footer and the `▁` floor.
const hudScreen = `  ctx 99% Auto in the transcript
  deploy here
▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔ title ▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔
  working  ctx 12%
  ⧖ queued — ctx 34%
╭────────────────────────╮
│ Message…               │
╰────────────────────────╯
  Auto 漢 model
▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁`

func TestEngineClickOnLastHitsTheLowerRowWithCompleteReports(t *testing.T) {
	t.Parallel()
	terminal, inputs := startFakeTUI(t, "top line\n\n  deploy\n\n\n  x deploy now\n", "")

	if err := NewEngine(terminal).RunBeat(context.Background(), clickBeat(Target{Text: "deploy", Nth: TargetLast}, 2)); err != nil {
		t.Fatalf("RunBeat: %v", err)
	}

	// "deploy" on row 5 spans columns 4..9: its centre cell is (6, 5), reported 1-based.
	press, release := "\x1b[<0;7;6M", "\x1b[<0;7;6m"
	reads := awaitInputs(t, inputs, 4)
	if want := []string{press, release, press, release}; strings.Join(reads, "|") != strings.Join(want, "|") {
		t.Fatalf("the program read %q, want each report whole and alone: %q", reads, want)
	}
	take := terminal.Close()
	targets := engineEvents(t, take, EventTarget)
	if len(targets) != 1 || *targets[0].Box != (CellBox{X: 4, Y: 5, W: 6, H: 1}) {
		t.Fatalf("target events = %+v, want one box at (4,5) 6×1", targets)
	}
	var clickTimes []time.Duration
	for _, event := range take.Events {
		if event.Kind == EventClick {
			clickTimes = append(clickTimes, event.At)
		}
	}
	if len(clickTimes) != 2 || clickTimes[1]-clickTimes[0] < clickRepeatGap+clickHoldGap {
		t.Fatalf("click times %v, want two clicks at least %s apart", clickTimes, clickRepeatGap+clickHoldGap)
	}
}

// A screen shown for less than a frame is one the sampler can miss. The wait that matched it
// must still land it in the take, inside its beat, or the beat's seen expect judges a take that
// never showed what the rig saw.
func TestEngineWaitRecordsTheScreenItMatched(t *testing.T) {
	t.Parallel()
	// One sample a second, and "ready" is gone after 300ms: the sampler's first tick never sees it.
	terminal, err := StartTerminal("sh", []string{"-c", `printf ready; sleep 0.3; printf '\033[2J\033[Hgone'; sleep 5`},
		TermOptions{Cols: 40, Rows: 5, FPS: 1, Env: os.Environ()})
	if err != nil {
		t.Fatalf("StartTerminal: %v", err)
	}
	t.Cleanup(func() { terminal.Close() })
	beat := Beat{ID: 2, Title: "ends on a wait", Do: []Action{
		{Wait: &WaitAction{Screen: "^ready", Timeout: engineTestTimeout}},
	}}
	if err := NewEngine(terminal).RunBeat(context.Background(), beat); err != nil {
		t.Fatalf("RunBeat: %v", err)
	}
	next := Beat{ID: 3, Title: "after", Do: []Action{
		{Wait: &WaitAction{Screen: "^gone", Timeout: engineTestTimeout}},
	}}
	if err := NewEngine(terminal).RunBeat(context.Background(), next); err != nil {
		t.Fatalf("RunBeat: %v", err)
	}
	take := terminal.Close()

	spans := []BeatSpan{{Beat: 2}, {Beat: 3}}
	for _, event := range take.Events {
		if event.Kind == EventBeatStart {
			var detail EngineEventDetail
			if err := json.Unmarshal([]byte(event.Detail), &detail); err != nil {
				t.Fatal(err)
			}
			spans[detail.Beat-2].Start = event.At
		}
	}
	spans[0].End, spans[1].End = spans[1].Start, takeEnd(take)
	for _, screen := range beatScreens(take.Snapshots, spans)[2] {
		if rowText(screen, 0) == "ready" {
			return
		}
	}
	t.Fatalf("no snapshot of beat 2 shows the \"ready\" its wait matched; the take holds %d snapshot(s)",
		len(take.Snapshots))
}

func TestEngineWaitTimesOutNamingTheBeat(t *testing.T) {
	t.Parallel()
	terminal, _ := startFakeTUI(t, "idle\n", "")
	beat := Beat{ID: 7, Title: "stalls", Do: []Action{
		{Pause: &PauseAction{For: time.Millisecond}},
		{Wait: &WaitAction{Screen: "never shown", Timeout: 200 * time.Millisecond}},
	}}

	err := NewEngine(terminal).RunBeat(context.Background(), beat)

	var beatErr *BeatError
	if !errors.As(err, &beatErr) || beatErr.Beat != 7 || beatErr.Action != 1 {
		t.Fatalf("RunBeat = %v, want a BeatError at beat 7 do[1]", err)
	}
	for _, want := range []string{"beat 7 (stalls)", "do[1]", "timed out after 200ms"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not say %q", err, want)
		}
	}
}

func TestEngineEventsAreMonotonicAndCoverEveryAction(t *testing.T) {
	t.Parallel()
	terminal, inputs := startFakeTUI(t, "before\n", "after typing\n")
	isHumanized := false
	beat := Beat{ID: 3, Title: "prompt", Do: []Action{
		{Type: &TypeAction{Text: "go"}},
		{Wait: &WaitAction{Screen: "after typing", Timeout: engineTestTimeout}},
		{Wait: &WaitAction{Screen: "before", Gone: true, Timeout: engineTestTimeout}},
		{Type: &TypeAction{Text: "fast", Humanize: &isHumanized}},
		{Key: &KeyAction{Name: "esc", Repeat: 2}},
		{Pause: &PauseAction{For: 10 * time.Millisecond}},
		{Click: &ClickAction{Target: Target{Text: "typing"}}},
	}}

	if err := NewEngine(terminal).RunBeat(context.Background(), beat); err != nil {
		t.Fatalf("RunBeat: %v", err)
	}
	// A loaded box may coalesce the unpaced writes into one read, so the stream is compared, not
	// the read boundaries.
	awaitStream(t, inputs, "gofast\x1b\x1b\x1b[<0;9;1M\x1b[<0;9;1m")
	take := terminal.Close()

	var previous time.Duration
	for index, event := range take.Events {
		if event.At < previous {
			t.Fatalf("event %d (%s) at %s precedes the one before it at %s", index, event.Kind, event.At, previous)
		}
		previous = event.At
	}
	starts := engineEvents(t, take, EventBeatStart)
	if len(starts) != 2 || starts[1] != (EngineEventDetail{Beat: 3, Title: "prompt"}) {
		t.Fatalf("beat-start events = %+v, want the ready beat then beat 3", starts)
	}
	if ends := engineEvents(t, take, EventActionEnd); len(ends) != 1+len(beat.Do) {
		t.Fatalf("%d action-end events, want %d", len(ends), 1+len(beat.Do))
	}
	clicks := engineEvents(t, take, EventClick)
	if len(clicks) != 1 || *clicks[0].Cell != [2]int{8, 0} || clicks[0].Form != "click" || clicks[0].Action != 6 {
		t.Fatalf("click events = %+v, want do[6] at cell (8,0)", clicks)
	}
}

func TestEngineFooterTargetResolvesAboveTheFloorHairline(t *testing.T) {
	t.Parallel()
	terminal, inputs := startFakeTUI(t, hudScreen, "")

	if err := NewEngine(terminal).RunBeat(context.Background(), clickBeat(Target{Text: "Auto", Area: AreaFooter}, 1)); err != nil {
		t.Fatalf("RunBeat: %v", err)
	}

	// Row 8 is the footer, directly above the ▁ floor on row 9; "Auto" spans columns 2..5.
	if reads := awaitInputs(t, inputs, 2); reads[0] != "\x1b[<0;4;9M" {
		t.Fatalf("the press was %q, want it on the footer row at column 3", reads[0])
	}
}

func TestEngineStatusTargetResolvesBelowTheTopRule(t *testing.T) {
	t.Parallel()
	terminal, inputs := startFakeTUI(t, hudScreen, "")

	target := Target{Text: `ctx \d+%`, Nth: TargetLast, Area: AreaStatus}
	if err := NewEngine(terminal).RunBeat(context.Background(), clickBeat(target, 1)); err != nil {
		t.Fatalf("RunBeat: %v", err)
	}

	// The status line is row 3, under the ▔ rule; the staged ⧖ row below it holds a later
	// "ctx 34%" that the area must exclude. "ctx 12%" spans columns 11..17.
	if reads := awaitInputs(t, inputs, 2); reads[0] != "\x1b[<0;15;4M" {
		t.Fatalf("the press was %q, want it on the status row at column 14", reads[0])
	}
}

func TestEngineResolveTargetAreasAndNth(t *testing.T) {
	t.Parallel()
	terminal, _ := startFakeTUI(t, hudScreen, "")
	screen := terminal.Current()

	for _, tc := range []struct {
		name   string
		target Target
		want   CellBox
		fails  string
	}{
		{"transcript stays above the top rule", Target{Text: "ctx", Nth: TargetLast, Area: AreaTranscript}, CellBox{X: 2, Y: 0, W: 3, H: 1}, ""},
		{"any reaches the staged row", Target{Text: "ctx", Nth: TargetLast}, CellBox{X: 13, Y: 4, W: 3, H: 1}, ""},
		{"nth counts in reading order", Target{Text: "Auto", Nth: 2}, CellBox{X: 2, Y: 8, W: 4, H: 1}, ""},
		{"a wide glyph counts once", Target{Text: "model"}, CellBox{X: 10, Y: 8, W: 5, H: 1}, ""},
		{"too few matches", Target{Text: "Auto", Nth: 3}, CellBox{}, "only 2 on screen"},
		{"absent", Target{Text: "nowhere", Area: AreaFooter}, CellBox{}, "not on screen"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			box, err := resolveTarget(screen, tc.target)
			if tc.fails != "" {
				if err == nil || !strings.Contains(err.Error(), tc.fails) {
					t.Fatalf("resolveTarget = %+v, %v; want an error saying %q", box, err, tc.fails)
				}
				return
			}
			if err != nil || box != tc.want {
				t.Fatalf("resolveTarget = %+v, %v; want %+v", box, err, tc.want)
			}
		})
	}
}
