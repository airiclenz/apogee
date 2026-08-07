package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/term"
)

// newScreen returns a file standing in for the terminal. *os.File is a term.File, which is exactly
// what makes it usable here: the wrappers under test accept the interface bubbletea asserts, not a
// concrete stdout.
func newScreen(t *testing.T) *os.File {
	t.Helper()
	screen, err := os.Create(filepath.Join(t.TempDir(), "screen.bin"))
	if err != nil {
		t.Fatalf("create screen: %v", err)
	}
	t.Cleanup(func() { _ = screen.Close() })
	return screen
}

// painted returns everything written to a screen created by [newScreen].
func painted(t *testing.T, screen *os.File) string {
	t.Helper()
	raw, err := os.ReadFile(screen.Name())
	if err != nil {
		t.Fatalf("read screen: %v", err)
	}
	return string(raw)
}

// TestSyncQueryStripperDropsOnlyTheModeQuery is the core of the filter: the mode-2026 DECRQM
// request never reaches the terminal, and everything sharing its write does — the mode-2027
// request above all, since widthAuthority mirrors bubbletea's reaction to that one and silencing
// half the pair would desync the two sides.
func TestSyncQueryStripperDropsOnlyTheModeQuery(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		write string
		want  string
	}{
		{
			// bubbletea/tea.go:1109-1114 verbatim: RequestModeSynchronizedOutput +
			// RequestModeUnicodeCore, one execute, one flush, one Write.
			name:  "the pair bubbletea writes, with 2027 surviving",
			write: "\x1b[?2026$p\x1b[?2027$p",
			want:  "\x1b[?2027$p",
		},
		{
			name:  "the query on its own leaves nothing on the wire",
			write: "\x1b[?2026$p",
			want:  "",
		},
		{
			name:  "a query buried in a frame takes only itself",
			write: "\x1b[2;5Hhello\x1b[?2026$p\x1b[m",
			want:  "\x1b[2;5Hhello\x1b[m",
		},
		{
			name:  "every occurrence goes, not just the first",
			write: "\x1b[?2026$pa\x1b[?2026$pb",
			want:  "ab",
		},
		{
			// The BSU/ESU frame wrappers differ from the request by their final byte. If the mode
			// is never enabled the renderer never emits them, but a filter that ate them would
			// corrupt any frame it did see, so the distinction is pinned.
			name:  "the BSU and ESU wrappers are not the query",
			write: "\x1b[?2026h\x1b[48;2;0;0;0mhi\x1b[m\x1b[?2026l",
			want:  "\x1b[?2026h\x1b[48;2;0;0;0mhi\x1b[m\x1b[?2026l",
		},
		{
			// The configuration declining the query puts the renderer back into. It shares the
			// `ESC [ ? 2` prefix with the probe, so it is the sharpest near-miss in the stream.
			name:  "the cursor-visibility pair is untouched",
			write: "\x1b[?25l\x1b[3;1Hhi\x1b[?25h",
			want:  "\x1b[?25l\x1b[3;1Hhi\x1b[?25h",
		},
		{
			name:  "a frame with no query at all is byte-identical",
			write: "\x1b[?1049h\x1b[3J\x1b[2;5H\xe2\xa3\xbf\xe2\xa3\xbf",
			want:  "\x1b[?1049h\x1b[3J\x1b[2;5H\xe2\xa3\xbf\xe2\xa3\xbf",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			screen := newScreen(t)
			stripper := newSyncQueryStripper(screen)
			n, err := stripper.Write([]byte(tc.write))
			if err != nil {
				t.Fatalf("Write(%q): %v", tc.write, err)
			}
			// Every byte offered was accepted, even the ones deliberately not forwarded: a short
			// count without an error breaks io.Writer and would read as a failed frame.
			if n != len(tc.write) {
				t.Errorf("Write(%q) = %d, want %d (the full length offered)", tc.write, n, len(tc.write))
			}
			if got := painted(t, screen); got != tc.want {
				t.Errorf("terminal received %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSyncQueryStripperHandlesAProbeSplitAcrossWrites: bubbletea flushes its whole output buffer in
// one Write, so it never splits the request today — this pins that the filter does not depend on
// that staying true. Every split point is exercised, because a scan that only looked at one write
// would pass the probe through in two halves and the terminal would answer it.
func TestSyncQueryStripperHandlesAProbeSplitAcrossWrites(t *testing.T) {
	t.Parallel()
	for k := 1; k < len(syncQueryProbe); k++ {
		t.Run("split after "+strconv.Itoa(k)+" bytes", func(t *testing.T) {
			t.Parallel()
			screen := newScreen(t)
			stripper := newSyncQueryStripper(screen)
			writes := []string{syncQueryProbe[:k], syncQueryProbe[k:] + "\x1b[?2027$p"}
			for _, w := range writes {
				if n, err := stripper.Write([]byte(w)); err != nil || n != len(w) {
					t.Fatalf("Write(%q) = (%d, %v), want (%d, nil)", w, n, err, len(w))
				}
			}
			if got, want := painted(t, screen), "\x1b[?2027$p"; got != want {
				t.Errorf("terminal received %q, want %q", got, want)
			}
		})
	}
}

// TestSyncQueryStripperReleasesAHeldPrefixTheNextWriteRefutes is the other half of the carry, and
// the one that would lose bytes if it were wrong. A held-back run is only ever a proper prefix of
// the probe — an incomplete escape sequence — and the moment the next write shows it was something
// else, it must reach the terminal ahead of that write's own bytes and in the right order.
func TestSyncQueryStripperReleasesAHeldPrefixTheNextWriteRefutes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		writes []string
		want   string
	}{
		{
			// `ESC [ ? 2` then `5 l`: the hide-cursor sequence arriving in two pieces. Eating it
			// would leave the cursor visible while the renderer paints.
			name:   "a hide-cursor sequence split inside the shared prefix",
			writes: []string{"\x1b[?2", "5l"},
			want:   "\x1b[?25l",
		},
		{
			name:   "a near-complete prefix followed by ordinary text",
			writes: []string{"\x1b[?2026", "hello"},
			want:   "\x1b[?2026hello",
		},
		{
			name:   "a bare escape at the end of a write",
			writes: []string{"hi\x1b", "[2;5H"},
			want:   "hi\x1b[2;5H",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			screen := newScreen(t)
			stripper := newSyncQueryStripper(screen)
			for _, w := range tc.writes {
				if n, err := stripper.Write([]byte(w)); err != nil || n != len(w) {
					t.Fatalf("Write(%q) = (%d, %v), want (%d, nil)", w, n, err, len(w))
				}
			}
			if got := painted(t, screen); got != tc.want {
				t.Errorf("terminal received %q, want %q", got, tc.want)
			}
		})
	}
}

// errScreen is a terminal whose every write fails, so the error path can be pinned.
type errScreen struct{ term.File }

var errScreenWrite = errors.New("screen is gone")

func (errScreen) Write([]byte) (int, error) { return 0, errScreenWrite }

// TestSyncQueryStripperReportsAFailedWriteAsZero: once the carry and the removal have rewritten the
// stream there is no honest count of how much of THIS p reached the terminal, so the wrapper claims
// none of it and hands the error up. bubbletea treats an output error as fatal to the frame either
// way; what must not happen is a non-nil error paired with a count the caller might trust.
func TestSyncQueryStripperReportsAFailedWriteAsZero(t *testing.T) {
	t.Parallel()
	stripper := newSyncQueryStripper(errScreen{})
	n, err := stripper.Write([]byte("\x1b[?2026$p\x1b[?2027$p"))
	if !errors.Is(err, errScreenWrite) {
		t.Errorf("Write error = %v, want %v", err, errScreenWrite)
	}
	if n != 0 {
		t.Errorf("Write = %d bytes on a failed write, want 0", n)
	}
}

// TestSyncQueryStripperIsATermFileOverStdout is the assertion this wrapper most needs, and it is
// item 2's assertion repeated for the same reason: bubbletea and colorprofile both decide "is my
// output a terminal" by type-asserting it to term.File and asking for a descriptor. A wrapper that
// failed either half would silently turn off raw mode, VT processing, size detection and the
// cursor optimizations — a far bigger change than the one it was installed to make.
func TestSyncQueryStripperIsATermFileOverStdout(t *testing.T) {
	t.Parallel()
	var file term.File = newSyncQueryStripper(os.Stdout) // the assertion bubbletea makes
	if got, want := file.Fd(), os.Stdout.Fd(); got != want {
		t.Errorf("Fd() = %d, want stdout's descriptor %d", got, want)
	}
}

// TestSyncQueryStripperCloseOwnsNothing: the wrapper opens no file and holds no descriptor, so
// closing it must leave both the terminal underneath and anything else in the stack usable. [Run]
// closes the trace through the *tracedOutput it holds; nothing here has the right to.
func TestSyncQueryStripperCloseOwnsNothing(t *testing.T) {
	t.Parallel()
	screen := newScreen(t)
	stripper := newSyncQueryStripper(screen)
	if err := stripper.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := screen.WriteString("still open"); err != nil {
		t.Errorf("the terminal was closed with the stripper: %v", err)
	}
}

// TestSyncQueryStripperOverTracedOutputTracesTheStrippedStream pins the composition of the two
// wrappers, in the order [programOutput] stacks them. The trace has to agree with the wire — the
// Windows investigation diffs traces against pseudoconsole captures byte for byte — so the query
// must be gone from the trace as well as from the terminal, and the surviving bytes must be
// identical in both.
func TestSyncQueryStripperOverTracedOutputTracesTheStrippedStream(t *testing.T) {
	t.Parallel()
	screen := newScreen(t)
	tracePath := filepath.Join(t.TempDir(), "trace.txt")
	traced, err := newTracedOutput(screen, tracePath)
	if err != nil {
		t.Fatalf("newTracedOutput: %v", err)
	}
	stripper := newSyncQueryStripper(traced)

	for _, w := range []string{"\x1b[?2026$p\x1b[?2027$p", "\x1b[2;5Hhi"} {
		if n, err := stripper.Write([]byte(w)); err != nil || n != len(w) {
			t.Fatalf("Write(%q) = (%d, %v), want (%d, nil)", w, n, err, len(w))
		}
	}
	if err := traced.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	want := "\x1b[?2027$p\x1b[2;5Hhi"
	if got := painted(t, screen); got != want {
		t.Errorf("terminal received %q, want %q", got, want)
	}
	raw, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	var traceBytes strings.Builder
	for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		unquoted, err := strconv.Unquote(line)
		if err != nil {
			t.Fatalf("trace line is not a quoted Go string: %q (%v)", line, err)
		}
		traceBytes.WriteString(unquoted)
	}
	if got := traceBytes.String(); got != want {
		t.Errorf("trace recorded %q, want the wire's %q", got, want)
	}
}

// TestProgramOutputStacksTheWrappersInOrder covers the four combinations of the two switches, and
// the one that matters is the last: the stripper nearest bubbletea, the tracer beneath it, and the
// *tracedOutput handed back so [Run] can still close the file it opened.
func TestProgramOutputStacksTheWrappersInOrder(t *testing.T) {
	t.Parallel()

	t.Run("neither switch leaves bubbletea on its own output", func(t *testing.T) {
		t.Parallel()
		out, traced, err := programOutput("", false)
		if err != nil {
			t.Fatalf("programOutput: %v", err)
		}
		if out != nil || traced != nil {
			t.Errorf("programOutput(\"\", false) = (%v, %v), want (nil, nil)", out, traced)
		}
	})

	t.Run("declining sync output alone wraps stdout", func(t *testing.T) {
		t.Parallel()
		out, traced, err := programOutput("", true)
		if err != nil {
			t.Fatalf("programOutput: %v", err)
		}
		if traced != nil {
			t.Errorf("a traced output was installed with --tui-trace unset")
		}
		stripper, ok := out.(*syncQueryStripper)
		if !ok {
			t.Fatalf("output is %T, want *syncQueryStripper", out)
		}
		if stripper.out != term.File(os.Stdout) {
			t.Errorf("the stripper wraps %T, want os.Stdout", stripper.out)
		}
	})

	t.Run("tracing alone is item 2 unchanged", func(t *testing.T) {
		t.Parallel()
		out, traced, err := programOutput(filepath.Join(t.TempDir(), "trace.txt"), false)
		if err != nil {
			t.Fatalf("programOutput: %v", err)
		}
		if traced == nil {
			t.Fatal("no traced output was installed with --tui-trace set")
		}
		defer func() { _ = traced.Close() }()
		if out != term.File(traced) {
			t.Errorf("output is %T, want the *tracedOutput itself", out)
		}
	})

	t.Run("both switches put the stripper above the tracer", func(t *testing.T) {
		t.Parallel()
		out, traced, err := programOutput(filepath.Join(t.TempDir(), "trace.txt"), true)
		if err != nil {
			t.Fatalf("programOutput: %v", err)
		}
		if traced == nil {
			t.Fatal("no traced output was installed with --tui-trace set")
		}
		defer func() { _ = traced.Close() }()
		stripper, ok := out.(*syncQueryStripper)
		if !ok {
			t.Fatalf("output is %T, want *syncQueryStripper", out)
		}
		if stripper.out != term.File(traced) {
			t.Errorf("the stripper wraps %T, want the *tracedOutput", stripper.out)
		}
	})
}

// TestProgramOptionsInstallTheStripperWhenSyncOutputIsDeclined: the option list grows by exactly
// the output option, and by nothing else. The negative half — no switch, no wrapper — is pinned by
// TestProgramOptionsInstallNoTraceWhenTheFlagIsUnset, which passes false for exactly that reason.
func TestProgramOptionsInstallTheStripperWhenSyncOutputIsDeclined(t *testing.T) {
	t.Parallel()
	opts, traced, err := programOptions(context.Background(), Options{}, nil, true)
	if err != nil {
		t.Fatalf("programOptions: %v", err)
	}
	if traced != nil {
		t.Errorf("a traced output was installed with --tui-trace unset")
	}
	if len(opts) != 2 { // tea.WithContext plus tea.WithOutput
		t.Errorf("got %d program options, want 2 (context plus output)", len(opts))
	}
}

// TestProgramOptionsComposeTheEnvironmentWithTheStripper: item 5's rule and this item's rule are
// independent options on the same program, and a Windows run switches both on at once. Three
// options, and the trace file still absent.
func TestProgramOptionsComposeTheEnvironmentWithTheStripper(t *testing.T) {
	t.Parallel()
	opts, traced, err := programOptions(context.Background(), Options{}, []string{"TERM=xterm-256color"}, true)
	if err != nil {
		t.Fatalf("programOptions: %v", err)
	}
	if traced != nil {
		t.Errorf("a traced output was installed with --tui-trace unset")
	}
	if len(opts) != 3 { // context, environment, output
		t.Errorf("got %d program options, want 3 (context, environment, output)", len(opts))
	}
}
