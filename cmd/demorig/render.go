package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// newRenderCommand renders the shipped GIF from a take, the storyboard deciding every section's
// length, hold, zoom and cut.
func newRenderCommand() *cobra.Command {
	var out string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "render <storyboard.yaml> [<take>] [-o out.gif] [--dry-run]",
		Short: "Render the shipped GIF from a take, framed as the storyboard says",
		Long: "render lays each beat of the take onto its section's duration — the hold at 1×, the\n" +
			"rest sped up to fit or frozen on its last frame — rasterizes every frame at the\n" +
			"storyboard's frame, composes the zooms and the click cursor, and streams the frames into\n" +
			"ffmpeg for a per-clip palette encode, then shrinks the GIF with gifsicle when that is on\n" +
			"PATH. The take defaults to <work>/<clip>.take (the work dir following $" + workDirEnv + "),\n" +
			"the output to the storyboard's ship path. --dry-run prints the ffmpeg command line instead\n" +
			"of rendering.",
		Args: cobra.RangeArgs(1, 2),
		RunE: runE(func(cmd *cobra.Command, args []string) error {
			options := renderOptions{Storyboard: args[0], Out: out, DryRun: dryRun}
			if len(args) > 1 {
				options.Take = args[1]
			}
			return renderTake(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), options)
		}),
	}
	cmd.Flags().StringVarP(&out, "output", "o", "", "the GIF to write (default: the storyboard's ship path)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the ffmpeg command line and exit without rendering")
	return cmd
}

// renderOptions is one render's command line. An empty Take is the clip's default take.
type renderOptions struct {
	Storyboard string
	Take       string
	Out        string
	DryRun     bool
}

// The palette encode every render ends with: a per-clip palette of frame.max_colors (much
// cleaner than ffmpeg's default 256-colour quantiser on flat terminal colours) applied with an
// ordered dither.
const (
	ditherMode  = "bayer"
	ditherScale = 3
)

// renderPlan is everything a render needs once the storyboard and the take are read: the
// output clock, the rasterizer, the compositor and where the GIF goes.
type renderPlan struct {
	board      *Storyboard
	take       *Take
	schedule   *Schedule
	rasterizer *Rasterizer
	compositor *Compositor
	out        string
}

// planRender loads the storyboard and the take and builds the schedule, the rasterizer and the
// compositor. The take must have been recorded at the storyboard's frame: its cells are what the
// rasterizer lays out and its targets are resolved on. Frames are rasterized at frame.scale —
// padding and font size multiplied through — so a zoom has pixels to draw on.
func planRender(stderr io.Writer, options renderOptions) (*renderPlan, error) {
	board, err := Load(options.Storyboard)
	if err != nil {
		return nil, err
	}
	path := options.Take
	if path == "" {
		if path, err = takeArg(nil, board.Clip); err != nil {
			return nil, err
		}
	}
	take, err := LoadTake(path)
	if err != nil {
		return nil, err
	}
	frame := board.Frame
	if take.Cols != frame.Cols || take.Rows != frame.Rows {
		return nil, fmt.Errorf("%s was recorded at %d×%d cells; the storyboard's frame is %d×%d — re-record it",
			path, take.Cols, take.Rows, frame.Cols, frame.Rows)
	}
	spans, err := SpansFrom(board, take)
	if err != nil {
		return nil, err
	}
	schedule, err := NewSchedule(spans)
	if err != nil {
		return nil, err
	}
	for _, warning := range schedule.Warnings {
		if _, err := fmt.Fprintln(stderr, "warning:", warning); err != nil {
			return nil, err
		}
	}
	rasterizer, err := NewRasterizer(board.Fonts, Geometry{
		Cols: frame.Cols, Rows: frame.Rows, Padding: frame.Padding * frame.Scale,
		FontSize: frame.FontSize * float64(frame.Scale), LineHeight: frame.LineHeight,
	})
	if err != nil {
		return nil, err
	}
	layout := CellLayout{
		Size: rasterizer.Size(), Padding: frame.Padding * frame.Scale,
		CellWidth: rasterizer.CellWidth(), LineHeight: rasterizer.LineHeight(), Scale: frame.Scale,
	}
	compositor, err := NewCompositor(board, take, schedule, layout)
	if err != nil {
		return nil, err
	}
	out := options.Out
	if out == "" {
		out = board.Ship
	}
	return &renderPlan{board: board, take: take, schedule: schedule, rasterizer: rasterizer, compositor: compositor, out: out}, nil
}

// renderTake renders the GIF and prints its summary, or on a dry run prints the ffmpeg command
// line it would have run.
func renderTake(ctx context.Context, stdout, stderr io.Writer, options renderOptions) error {
	plan, err := planRender(stderr, options)
	if err != nil {
		return err
	}
	argv := ffmpegArgs(plan.compositor.Size(), plan.board.Frame, plan.out)
	if options.DryRun {
		_, err := fmt.Fprintln(stdout, shellLine(argv))
		return err
	}
	if err := plan.encode(ctx, argv); err != nil {
		return err
	}
	if err := optimizeGIF(ctx, plan.out); err != nil {
		return err
	}
	return writeRenderSummary(stdout, plan.out, plan.schedule)
}

// ffmpegArgs is the encode's command line: raw RGBA frames of size at frame.fps in on stdin, the
// palette filtergraph, the GIF out.
func ffmpegArgs(size image.Point, frame Frame, out string) []string {
	graph := fmt.Sprintf("[0:v]split[a][b];[a]palettegen=max_colors=%d[p];[b][p]paletteuse=dither=%s:bayer_scale=%d",
		frame.MaxColors, ditherMode, ditherScale)
	return []string{
		"ffmpeg", "-y", "-loglevel", "error",
		"-f", "rawvideo", "-pixel_format", "rgba",
		"-video_size", fmt.Sprintf("%dx%d", size.X, size.Y),
		"-framerate", strconv.Itoa(frame.FPS),
		"-i", "pipe:0",
		"-filter_complex", graph,
		out,
	}
}

// encode streams every scheduled frame, composed, into ffmpeg as raw RGBA. A snapshot shown by
// consecutive frames is rasterized once.
func (p *renderPlan) encode(ctx context.Context, argv []string) error {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // a fixed program; the arguments are ours
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s: %w", argv[0], err)
	}
	writeErr := p.writeFrames(stdin)
	closeErr := stdin.Close()
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("%s: %w\n%s", argv[0], err, strings.TrimSpace(stderr.String()))
	}
	if writeErr != nil {
		return fmt.Errorf("stream frames to %s: %w", argv[0], writeErr)
	}
	return closeErr
}

// writeFrames writes each output frame's pixels, in order.
func (p *renderPlan) writeFrames(w io.Writer) error {
	shown := -1
	var source *image.RGBA
	for _, frame := range p.schedule.Frames(p.board.Frame.FPS) {
		index := snapshotAt(p.take.Snapshots, frame.Source)
		if index < 0 {
			return fmt.Errorf("the take holds no snapshot to show at %s", frame.Source)
		}
		if index != shown {
			source, shown = p.rasterizer.Frame(p.take.Snapshots[index]), index
		}
		if _, err := w.Write(p.compositor.Compose(frame.Out, source).Pix); err != nil {
			return err
		}
	}
	return nil
}

// snapshotAt is the index of the snapshot on screen at source time t — the last one taken at or
// before it, or the first when t precedes them all — and -1 for a take without snapshots.
func snapshotAt(snapshots []Snapshot, t time.Duration) int {
	if len(snapshots) == 0 {
		return -1
	}
	after := sort.Search(len(snapshots), func(i int) bool { return snapshots[i].At > t })
	return max(after-1, 0)
}

// shellLine joins an argv for a human to read or paste: an argument the shell would split or
// unquote is wrapped in double quotes.
func shellLine(argv []string) string {
	words := make([]string, 0, len(argv))
	for _, arg := range argv {
		if strings.ContainsAny(arg, " '\";()") {
			arg = `"` + arg + `"`
		}
		words = append(words, arg)
	}
	return strings.Join(words, " ")
}

// runQuiet runs one command and reports its stderr only when it fails.
func runQuiet(ctx context.Context, argv []string) error {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w\n%s", argv[0], err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// The gifsicle pass: optional, and typically another 20-40% off with no visible loss.
const (
	gifsicleOptimize = "-O3"
	gifsicleLossy    = "--lossy=80"
)

// optimizeGIF shrinks the GIF in place through gifsicle when it is on PATH, and does nothing
// otherwise. The result replaces the input only once gifsicle has written it whole.
func optimizeGIF(ctx context.Context, path string) error {
	if _, err := exec.LookPath("gifsicle"); err != nil {
		return nil
	}
	optimized := path + ".opt"
	if err := runQuiet(ctx, []string{"gifsicle", gifsicleOptimize, gifsicleLossy, "-o", optimized, path}); err != nil {
		return err
	}
	return os.Rename(optimized, path)
}

// writeRenderSummary prints what was rendered: path, size and the clip's length — the
// schedule's total — then one row per beat with the speed its section plays at.
func writeRenderSummary(w io.Writer, path string, schedule *Schedule) error {
	stat, err := os.Stat(path)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%s  %s  %ss\n", path, humanSize(stat.Size()), number(schedule.Length.Seconds())); err != nil {
		return err
	}
	for _, section := range schedule.Sections {
		row := fmt.Sprintf("  beat %d  cut", section.Beat)
		if !section.Cut {
			row = fmt.Sprintf("  beat %d  %.2f×  %s of take in %s",
				section.Beat, section.Speed, section.length().Round(time.Millisecond), section.Duration)
		}
		if _, err := fmt.Fprintln(w, row); err != nil {
			return err
		}
	}
	return nil
}

// number spells a float in its shortest round-trip form.
func number(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// humanSize spells a byte count the way `du -h` does: one decimal below ten units, whole units
// above, in K, M or G.
func humanSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%dB", bytes)
	}
	suffixes := []string{"K", "M", "G"}
	index := 0
	size := float64(bytes) / unit
	for size >= unit && index < len(suffixes)-1 {
		size /= unit
		index++
	}
	if size < 10 {
		return fmt.Sprintf("%.1f%s", size, suffixes[index])
	}
	return fmt.Sprintf("%.0f%s", size, suffixes[index])
}
