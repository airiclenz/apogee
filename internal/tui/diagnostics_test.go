package tui

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

// TestTracedOutputIsATermFileOverStdout is the one assertion this seam most needs. bubbletea and
// colorprofile both decide "is my output a terminal" by type-asserting it to term.File and then
// asking it for a descriptor; a wrapper that fails either half silently turns off raw mode, VT
// processing, size detection and the cursor optimizations, so the trace would record a different
// program from the one being debugged.
func TestTracedOutputIsATermFileOverStdout(t *testing.T) {
	traced, err := newTracedOutput(os.Stdout, filepath.Join(t.TempDir(), "trace.txt"))
	if err != nil {
		t.Fatalf("newTracedOutput: %v", err)
	}
	defer func() { _ = traced.Close() }()

	var file term.File = traced // the assertion bubbletea makes, made here so a change breaks a test
	if got, want := file.Fd(), os.Stdout.Fd(); got != want {
		t.Errorf("Fd() = %d, want stdout's descriptor %d", got, want)
	}
}

// TestTracedOutputTeesEveryWriteToTheTraceFile pins the tee: the terminal gets the bytes and the
// trace gets a faithful, re-parsable record of the same bytes, in the same order.
func TestTracedOutputTeesEveryWriteToTheTraceFile(t *testing.T) {
	dir := t.TempDir()
	screen, err := os.Create(filepath.Join(dir, "screen.bin")) // the stand-in terminal
	if err != nil {
		t.Fatalf("create screen: %v", err)
	}
	defer func() { _ = screen.Close() }()
	tracePath := filepath.Join(dir, "trace.txt")
	traced, err := newTracedOutput(screen, tracePath)
	if err != nil {
		t.Fatalf("newTracedOutput: %v", err)
	}

	// Escape sequences, not prose: what the renderer actually emits, and what a naive line-based
	// trace format would mangle.
	writes := []string{"\x1b[?1049h", "\x1b[2;5Hhello", "\x1b[?2026l\n"}
	for _, w := range writes {
		if n, err := traced.Write([]byte(w)); err != nil || n != len(w) {
			t.Fatalf("Write(%q) = (%d, %v), want (%d, nil)", w, n, err, len(w))
		}
	}
	if err := traced.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	painted, err := os.ReadFile(screen.Name())
	if err != nil {
		t.Fatalf("read screen: %v", err)
	}
	if got, want := string(painted), strings.Join(writes, ""); got != want {
		t.Errorf("terminal received %q, want %q", got, want)
	}

	raw, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) != len(writes) {
		t.Fatalf("trace has %d lines, want one per write (%d): %q", len(lines), len(writes), raw)
	}
	for i, line := range lines {
		got, err := strconv.Unquote(line)
		if err != nil {
			t.Fatalf("trace line %d is not a quoted Go string: %q (%v)", i, line, err)
		}
		if got != writes[i] {
			t.Errorf("trace line %d = %q, want %q", i, got, writes[i])
		}
	}
}

// TestTracedOutputCloseLeavesTheTerminalOpen: the wrapper owns the file it opened and nothing
// else. Closing the process's stdout because a diagnostic was switched on would be a far worse
// bug than the one the diagnostic is chasing.
func TestTracedOutputCloseLeavesTheTerminalOpen(t *testing.T) {
	dir := t.TempDir()
	screen, err := os.Create(filepath.Join(dir, "screen.bin"))
	if err != nil {
		t.Fatalf("create screen: %v", err)
	}
	defer func() { _ = screen.Close() }()
	traced, err := newTracedOutput(screen, filepath.Join(dir, "trace.txt"))
	if err != nil {
		t.Fatalf("newTracedOutput: %v", err)
	}
	if err := traced.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := screen.WriteString("still open"); err != nil {
		t.Errorf("the terminal was closed with the trace: %v", err)
	}
}

// TestProgramOptionsInstallNoTraceWhenTheFlagIsUnset is the guard against an always-on wrapper:
// with no --tui-trace the program must be built with exactly the options it has always had.
func TestProgramOptionsInstallNoTraceWhenTheFlagIsUnset(t *testing.T) {
	opts, traced, err := programOptions(context.Background(), Options{}, nil)
	if err != nil {
		t.Fatalf("programOptions: %v", err)
	}
	if traced != nil {
		t.Errorf("a traced output was installed with --tui-trace unset")
	}
	if len(opts) != 1 { // tea.WithContext, and nothing else
		t.Errorf("got %d program options, want 1 (tea.WithContext alone)", len(opts))
	}
}

// TestProgramOptionsInstallTheTraceWhenTheFlagIsSet is the other half: a named path adds the
// output option and hands back the file the caller has to close.
func TestProgramOptionsInstallTheTraceWhenTheFlagIsSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.txt")
	opts, traced, err := programOptions(context.Background(), Options{TracePath: path}, nil)
	if err != nil {
		t.Fatalf("programOptions: %v", err)
	}
	if traced == nil {
		t.Fatal("no traced output was installed with --tui-trace set")
	}
	defer func() { _ = traced.Close() }()
	if len(opts) != 2 { // tea.WithContext plus tea.WithOutput
		t.Errorf("got %d program options, want 2 (context plus output)", len(opts))
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the trace file was not created: %v", err)
	}
}

// TestProgramOptionsReportAnUnopenableTracePath: a bad path is a start-up error the human sees,
// not a silently-dropped flag that leaves them waiting for a file that never appears.
func TestProgramOptionsReportAnUnopenableTracePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-dir", "trace.txt")
	if _, _, err := programOptions(context.Background(), Options{TracePath: path}, nil); err == nil {
		t.Errorf("programOptions(%q) succeeded, want an error", path)
	}
}

// TestDiagLogRecordsTheStartupEnvironment: the four variables that decide how the renderer talks
// to this terminal, with an unset one spelled unambiguously rather than left blank, plus the
// width method the painter starts on.
func TestDiagLogRecordsTheStartupEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diag.txt")
	diag, err := newDiagLog(path)
	if err != nil {
		t.Fatalf("newDiagLog: %v", err)
	}
	env := map[string]string{"COLORTERM": "truecolor", "WT_SESSION": "a-guid"}
	diag.start(func(key string) string { return env[key] }, ansi.WcWidth)
	if err := diag.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := readFileString(t, path)
	for _, want := range []string{
		"TERM: (unset)",
		"COLORTERM: truecolor",
		"WT_SESSION: a-guid",
		"TERM_PROGRAM: (unset)",
		"width-method: wcwidth",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("diag log is missing %q; log was:\n%s", want, got)
		}
	}
}

// TestDiagLogRecordsAModeReportFedThroughUpdate is the seam's real contract: the mode-2027
// answer — the one that decides how the painter measures — reaches the log through the same
// message the width authority already observes, and the resulting width method is recorded
// beside it.
func TestDiagLogRecordsAModeReportFedThroughUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diag.txt")
	diag, err := newDiagLog(path)
	if err != nil {
		t.Fatalf("newDiagLog: %v", err)
	}
	m := newTestModel(t)
	m.diag = diag // Run wires it between newModel and the program; a test wires it the same way
	m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = step(t, m, tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModePermanentlySet})
	if err := diag.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := readFileString(t, path)
	for _, want := range []string{
		"mode 2027: 3 (permanently set)",
		"width-method: grapheme",
		"size: 100x30", // the WindowSizeMsg, proving the top-of-Update hook sees more than one type
	} {
		if !strings.Contains(got, want) {
			t.Errorf("diag log is missing %q; log was:\n%s", want, got)
		}
	}
	if m.th.measure.Method() != ansi.GraphemeWidth {
		t.Errorf("the mode report was consumed by the diag log: measure is %v", m.th.measure.Method())
	}
}

// TestDiagLogSuppressesUnchangedValues: the log is meant to be pasted into an issue, so a size
// re-reported every frame or a mode answered twice the same way must not repeat.
func TestDiagLogSuppressesUnchangedValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diag.txt")
	diag, err := newDiagLog(path)
	if err != nil {
		t.Fatalf("newDiagLog: %v", err)
	}
	for range 3 {
		diag.observe(tea.WindowSizeMsg{Width: 120, Height: 30})
	}
	diag.observe(tea.WindowSizeMsg{Width: 100, Height: 30})
	if err := diag.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := readFileString(t, path)
	if n := strings.Count(got, "size: 120x30"); n != 1 {
		t.Errorf("the unchanged size was recorded %d times, want 1; log was:\n%s", n, got)
	}
	if !strings.Contains(got, "size: 100x30") {
		t.Errorf("the changed size was not recorded; log was:\n%s", got)
	}
}

// TestDiagLogIsNilSafeWhenTheFlagIsUnset: nil is the off state and every observation point runs
// unconditionally, so a nil log that panicked would take down every ordinary session.
func TestDiagLogIsNilSafeWhenTheFlagIsUnset(t *testing.T) {
	var diag *diagLog
	diag.start(os.Getenv, ansi.WcWidth)
	diag.observe(tea.WindowSizeMsg{Width: 80, Height: 24})
	diag.observe(tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeSet})
	diag.record(diagWidthMethod, "wcwidth")
	if err := diag.Close(); err != nil {
		t.Errorf("Close on a nil log = %v, want nil", err)
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}
