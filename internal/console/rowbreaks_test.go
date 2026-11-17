package console

import (
	"runtime"
	"strings"
	"testing"
)

// Recorded ConPTY output (Windows 10.0.26300, a 40x160 pseudoconsole running cmd), the prompt's
// directory shortened to C:\work. The cursor moves are verbatim: their rows are what the screen
// held, so they must stay consistent with the text between them.
const (
	conptyBanner = "\x1b[?9001h\x1b[?1004h\x1b[?25l\x1b[2J\x1b[m\x1b[HMicrosoft Windows [Version 10.0.26300.9711]" +
		"\x1b]0;C:\\WINDOWS\\system32\\cmd.exe\x07\x1b[?25h\x1b[?25l\r\n(c) Microsoft Corporation. All rights reserved." +
		"\x1b[4;1HC:\\work>\x1b]0;Administrator: C:\\WINDOWS\\system32\\cmd.exe\x07\x1b[?25h"
	conptyBannerRead = "Microsoft Windows [Version 10.0.26300.9711]\r\n(c) Microsoft Corporation. All rights reserved." +
		"\n\nC:\\work>"

	// conptyDir follows conptyBanner: `dir` typed at the prompt on row 4, its listing filling the
	// first screen with the blank rows around "Directory of" skipped by cursor moves.
	conptyDir = "dir\r\n Volume in drive C has no label.\r\n\x1b]0;Administrator: C:\\WINDOWS\\system32\\cmd.exe - dir\x07" +
		"\x1b[?25l Volume Serial Number is CE0D-BB1C\x1b[8;1H Directory of C:\\work" +
		"\x1b[10;1H2026-11-17  11:10    <DIR>          .\r\n2026-11-17  10:41    <DIR>          ..\r\n" +
		"2026-09-23  09:54             3 007 ansi.go\r\n               1 File(s)          3 007 bytes\r\n" +
		"               2 Dir(s)  74 366 689 280 bytes free\x1b[16;1HC:\\work>\x1b[?25h"
	conptyDirRead = "dir\r\n Volume in drive C has no label.\r\n Volume Serial Number is CE0D-BB1C\n\n Directory of C:\\work" +
		"\n\n2026-11-17  11:10    <DIR>          .\r\n2026-11-17  10:41    <DIR>          ..\r\n" +
		"2026-09-23  09:54             3 007 ansi.go\r\n               1 File(s)          3 007 bytes\r\n" +
		"               2 Dir(s)  74 366 689 280 bytes free\n\nC:\\work>"
)

// writeRowBreaks feeds chunks, one write each, through a 40x160 rowBreakWriter, flushes it, and
// returns what a Console's read would hand the model: the output with its escapes stripped.
func writeRowBreaks(t *testing.T, chunks ...string) string {
	t.Helper()
	var output strings.Builder
	writer := newRowBreakWriter(&output, windowRows, windowCols)
	for _, chunk := range chunks {
		written, err := writer.Write([]byte(chunk))
		if err != nil || written != len(chunk) {
			t.Fatalf("Write(%q) = %d, %v; want %d, nil", chunk, written, err, len(chunk))
		}
	}
	if err := writer.flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	return stripEscapes(output.String())
}

// TestRowBreakWriterReadsSkippedRowsAsLineBreaks pins the ConPTY defect: a cursor move that skips
// rows reads as that many line breaks, on the first screen and after it has scrolled.
func TestRowBreakWriterReadsSkippedRowsAsLineBreaks(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("abcdefghijklmnopqrstuvwxyz", 8)[:200]
	tests := []struct {
		name   string
		chunks []string
		want   string
	}{
		{
			name:   "a fresh cmd keeps the blank line between banner and prompt",
			chunks: []string{conptyBanner},
			want:   conptyBannerRead,
		},
		{
			name:   "a first-screen dir keeps its blank lines",
			chunks: []string{conptyBanner, conptyDir},
			want:   conptyBannerRead + conptyDirRead,
		},
		{
			name: "a line that wraps the window costs no blank line",
			chunks: []string{conptyBanner, "echo " + long + "& echo next\r\n\x1b[?25l" + long + "\r\nnext" +
				"\x1b[10;1HC:\\work>\x1b[?25h"},
			want: conptyBannerRead + "echo " + long + "& echo next\r\n" + long + "\r\nnext\n\nC:\\work>",
		},
		{
			name: "output that scrolls reads its own newlines once at the bottom row",
			chunks: []string{"\x1b[38;1Hline12\x1b[40;1Hline13\r\x1b]0;cmd - echo  line13\x07\x1b[?25h\n" +
				"\x1b]0;cmd - echo .\x07\nline14\r\n\x1b[40;1HC:\\work>"},
			want: strings.Repeat("\n", 37) + "line12\n\nline13\r\n\nline14\r\nC:\\work>",
		},
		{
			name: "a screen cleared and painted again from the top",
			chunks: []string{"C:\\work>cls& echo x& echo.& echo.& echo y\r\n\x1b[?25l\x1b[H\x1b[K\r\n\x1b[K\x1b[2;1H" +
				"\x1b[3J\x1b[Hx\x1b[4;1Hy\x1b[6;1HC:\\work>"},
			want: "C:\\work>cls& echo x& echo.& echo.& echo y\r\n\r\nx\n\n\ny\n\nC:\\work>",
		},
		{
			name:   "a sequence split across writes still breaks before the text",
			chunks: []string{"one\x1b[3", ";1Htwo"},
			want:   "one\n\ntwo",
		},
		{
			name:   "a relative move down breaks once per row",
			chunks: []string{"one\x1b[2Btwo\x1b[Ethree"},
			want:   "one\n\ntwo\nthree",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := writeRowBreaks(t, test.chunks...); got != test.want {
				t.Errorf("read %q\nwant %q", got, test.want)
			}
		})
	}
}

// TestRowBreakWriterAddsNothingForSameRowOrRepaintMoves pins what must still strip to nothing:
// colour, erases, column moves on the row, and a repaint that moves up and back to rows the
// output has already reached.
func TestRowBreakWriterAddsNothingForSameRowOrRepaintMoves(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		chunks []string
		want   string
	}{
		{
			name:   "colour and erase",
			chunks: []string{"\x1b[31mred\x1b[0m \x1b[1;38;5;208mbright\x1b[m\x1b[K"},
			want:   "red bright",
		},
		{
			name:   "column moves on the same row",
			chunks: []string{"a\x1b[1;10Hb\x1b[3Cc\x1b[2Dd\x1b[20Ge"},
			want:   "abcde",
		},
		{
			name:   "up and back down without printing",
			chunks: []string{"\x1b[5;1Hfive\x1b[2;1H\x1b[?25h\x1b[5;9H more"},
			want:   "\n\n\n\nfive more",
		},
		{
			name:   "a private-mode toggle is not a move",
			chunks: []string{"x\x1b[?25ly\x1b[?1049hz"},
			want:   "xyz",
		},
		{
			name:   "an unfinished sequence at the end is handed on by flush",
			chunks: []string{"tail\x1b[3"},
			want:   "tail[3",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := writeRowBreaks(t, test.chunks...); got != test.want {
				t.Errorf("read %q\nwant %q", got, test.want)
			}
		})
	}
}

// TestRowBreakWriterIsOnlyOnTheWindowsOutputPath pins that POSIX output is untouched: only a
// pseudoconsole's output goes through a rowBreakWriter.
func TestRowBreakWriterIsOnlyOnTheWindowsOutputPath(t *testing.T) {
	t.Parallel()

	_, rowBreaks := newProcess().outputSink(false).(*rowBreakWriter)
	if want := runtime.GOOS == "windows"; rowBreaks != want {
		t.Errorf("output path has a rowBreakWriter = %v, want %v on %s", rowBreaks, want, runtime.GOOS)
	}
}
