package main

import (
	"image/gif"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// renderStoryboard is the render tests' storyboard: beat 1 lasts 1s, beat 2 1.5s.
const renderStoryboard = "testdata/render.yaml"

// saveRenderTake writes a take of renderStoryboard into dir and returns its path: beat 1 runs 2s
// of take (played at 2×), beat 2 1s (played at 1× and frozen for the rest of its 1.5s).
func saveRenderTake(t *testing.T, dir string, cols int) string {
	t.Helper()
	take := &Take{
		Cols: cols, Rows: 4, FPS: 10,
		Snapshots: []Snapshot{
			textSnapshot(0, "$ apogee"),
			textSnapshot(sec, "> tests are failing"),
			textSnapshot(2500*time.Millisecond, "Tests PASS"),
			textSnapshot(3*sec, "Tests PASS", "done"),
		},
		Events: []TakeEvent{beatStart(t, 0, 1), beatStart(t, 2*sec, 2)},
	}
	path := filepath.Join(dir, "rendered.take")
	if err := SaveTake(path, take); err != nil {
		t.Fatalf("SaveTake: %v", err)
	}
	return path
}

func TestRenderDryRunPrintsTheFfmpegLine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	take := saveRenderTake(t, dir, 20)
	gifPath := filepath.Join(dir, "out.gif")

	out, err := runRoot(t, "render", renderStoryboard, take, "-o", gifPath, "--dry-run")

	if err != nil {
		t.Fatalf("render --dry-run: %v\n%s", err, out)
	}
	for _, want := range []string{
		"ffmpeg -y -loglevel error -f rawvideo -pixel_format rgba -video_size 160x",
		"-framerate 10 -i pipe:0",
		`"[0:v]split[a][b];[a]palettegen=max_colors=16[p];[b][p]paletteuse=dither=none"`,
		gifPath,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want the ffmpeg line to carry %q, got:\n%s", want, out)
		}
	}
	if _, err := os.Stat(gifPath); !os.IsNotExist(err) {
		t.Errorf("a dry run wrote %s", gifPath)
	}
}

func TestRenderRefusesATakeOfAnotherFrame(t *testing.T) {
	t.Parallel()
	take := saveRenderTake(t, t.TempDir(), 30)

	_, err := runRoot(t, "render", renderStoryboard, take, "--dry-run")

	if err == nil || !strings.Contains(err.Error(), "recorded at 30×4 cells; the storyboard's frame is 20×4") {
		t.Fatalf("want the frame mismatch named, got %v", err)
	}
}

// TestRenderWritesAGIFOfTheScheduledLength renders the two-beat take for real: the GIF runs the
// beats' summed durations, and the summary's seconds are the schedule's total.
func TestRenderWritesAGIFOfTheScheduledLength(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not on PATH")
	}
	dir := t.TempDir()
	take := saveRenderTake(t, dir, 20)
	gifPath := filepath.Join(dir, "out.gif")

	out, err := runRoot(t, "render", renderStoryboard, take, "-o", gifPath)

	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], gifPath+"  ") || !strings.HasSuffix(lines[0], "  2.5s") {
		t.Fatalf("summary: want `<path>  <size>  2.5s` and two beat rows, got:\n%s", out)
	}
	if !strings.Contains(lines[1], "beat 1  2.00×  2s of take in 1s") || !strings.Contains(lines[2], "beat 2  1.00×  1s of take in 1.5s") {
		t.Errorf("beat rows: want each section's speed, got:\n%s", out)
	}
	f, err := os.Open(gifPath)
	if err != nil {
		t.Fatalf("open the GIF: %v", err)
	}
	defer f.Close() //nolint:errcheck // read-only
	decoded, err := gif.DecodeAll(f)
	if err != nil {
		t.Fatalf("decode the GIF: %v", err)
	}
	var centiseconds int
	for _, delay := range decoded.Delay {
		centiseconds += delay
	}
	if got := time.Duration(centiseconds) * 10 * time.Millisecond; got < 2400*time.Millisecond || got > 2600*time.Millisecond {
		t.Errorf("GIF length: want 2.5s within a frame, got %s over %d frame(s)", got, len(decoded.Delay))
	}
	if bounds := decoded.Image[0].Bounds(); bounds.Dx() != 160 {
		t.Errorf("GIF width: want frame.width 160, got %d", bounds.Dx())
	}
}

func TestSnapshotAt(t *testing.T) {
	t.Parallel()
	snapshots := []Snapshot{{At: sec}, {At: 2 * sec}, {At: 4 * sec}}
	cases := map[time.Duration]int{0: 0, sec: 0, 1999 * time.Millisecond: 0, 2 * sec: 1, 3 * sec: 1, 9 * sec: 2}
	for at, want := range cases {
		if got := snapshotAt(snapshots, at); got != want {
			t.Errorf("snapshotAt(%s): want %d, got %d", at, want, got)
		}
	}
	if got := snapshotAt(nil, 0); got != -1 {
		t.Errorf("snapshotAt on no snapshots: want -1, got %d", got)
	}
}
