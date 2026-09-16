package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// newRenderCommand cuts the shipped GIF from a take, the storyboard's framing deciding every
// pace, hold, zoom and cut. tools is the seam to ffprobe and ffmpeg's scene scan, so a dry run
// in a test needs neither on PATH.
func newRenderCommand(tools renderTools) *cobra.Command {
	var out string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "render <storyboard.yaml> <take.mp4> <session.json> [-o out.gif] [--dry-run]",
		Short: "Cut the shipped GIF from a take, framed as the storyboard says",
		Long: "render locates the storyboard's beats in the take (as beats does), builds one ffmpeg\n" +
			"filtergraph from their framing — speed, hold, zoom and cut per beat, the head before\n" +
			"the first beat dropped — encodes the GIF through a per-clip palette, and shrinks it\n" +
			"with gifsicle when that is on PATH. The output defaults to the storyboard's ship path.\n" +
			"--dry-run prints the ffmpeg command line instead of running it.",
		Args: cobra.ExactArgs(3),
		RunE: runE(func(cmd *cobra.Command, args []string) error {
			options := renderOptions{Storyboard: args[0], Take: args[1], Session: args[2], Out: out, DryRun: dryRun}
			return renderTake(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), tools, options)
		}),
	}
	cmd.Flags().StringVarP(&out, "output", "o", "", "the GIF to write (default: the storyboard's ship path)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the ffmpeg command line and exit without rendering")
	return cmd
}

// renderOptions is one render's command line.
type renderOptions struct {
	Storyboard string
	Take       string
	Session    string
	Out        string
	DryRun     bool
}

// takeInfo is what the probe reports about a take: its pixel size and its length.
type takeInfo struct {
	Size     Size
	Duration time.Duration
}

// renderTools are the external programs a render reads the take through, behind one seam: the
// probe for its geometry and length, and the scene scan behind the first-paint anchor. The
// ffmpeg adapters are production; a test hands in fixed values. The encode itself (ffmpeg,
// gifsicle) is not behind it — a dry run never reaches it.
type renderTools interface {
	Probe(ctx context.Context, take string) (takeInfo, error)
	Painter(take string, threshold float64) FirstPainter
}

// ffmpegTools is the production renderTools: ffprobe and ffmpeg on PATH.
type ffmpegTools struct{}

// Probe reads the first video stream's width and height and the container's duration.
func (ffmpegTools) Probe(ctx context.Context, take string) (takeInfo, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height:format=duration",
		"-of", "default=noprint_wrappers=1",
		take,
	)
	out, err := cmd.Output()
	if err != nil {
		return takeInfo{}, fmt.Errorf("ffprobe %s: %w", take, err)
	}
	return parseProbe(take, string(out))
}

// parseProbe reads ffprobe's key=value lines; every field must be present and numeric.
func parseProbe(take, out string) (takeInfo, error) {
	fields := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		if key, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			fields[key] = value
		}
	}
	var info takeInfo
	var err error
	if info.Size.Width, err = strconv.Atoi(fields["width"]); err != nil {
		return takeInfo{}, fmt.Errorf("ffprobe %s: width %q: %w", take, fields["width"], err)
	}
	if info.Size.Height, err = strconv.Atoi(fields["height"]); err != nil {
		return takeInfo{}, fmt.Errorf("ffprobe %s: height %q: %w", take, fields["height"], err)
	}
	if info.Duration, err = parseSeconds(fields["duration"]); err != nil {
		return takeInfo{}, fmt.Errorf("ffprobe %s: duration: %w", take, err)
	}
	return info, nil
}

// Painter is the ffmpeg scene scan over the take's head.
func (ffmpegTools) Painter(take string, threshold float64) FirstPainter {
	return ffmpegFirstPainter{Take: take, Threshold: threshold}
}

// renderTake resolves the beats, builds the filtergraph and encodes the GIF, or on a dry run
// prints the ffmpeg command line it would have run. A take narrower than the storyboard's zoom
// assumes (frame.width × frame.scale) still renders, with a warning: the zoom draws on pixels
// that are not there.
func renderTake(ctx context.Context, stdout, stderr io.Writer, tools renderTools, options renderOptions) error {
	board, err := Load(options.Storyboard)
	if err != nil {
		return err
	}
	entries, err := loadEntries(options.Session)
	if err != nil {
		return err
	}
	info, err := tools.Probe(ctx, options.Take)
	if err != nil {
		return err
	}
	painter := tools.Painter(options.Take, board.Align.SceneThreshold)
	times, err := resolveBeats(ctx, board, entries, painter, info.Duration)
	if err != nil {
		return err
	}
	segments := segmentsFrom(board, times, info.Duration)
	graph, err := Build(segments, board.Frame, info.Size)
	if err != nil {
		return err
	}
	if wanted := board.Frame.Width * board.Frame.Scale; info.Size.Width < wanted && hasZoom(segments) {
		_, err := fmt.Fprintf(stderr, "warning: %s is %dpx wide; the storyboard's zoom assumes %d (frame.width %d × scale %d), so it will blur\n",
			options.Take, info.Size.Width, wanted, board.Frame.Width, board.Frame.Scale)
		if err != nil {
			return err
		}
	}
	out := options.Out
	if out == "" {
		out = board.Ship
	}
	argv := ffmpegArgs(options.Take, graph, out)
	if options.DryRun {
		_, err := fmt.Fprintln(stdout, shellLine(argv))
		return err
	}
	if err := runQuiet(ctx, argv); err != nil {
		return err
	}
	if err := optimizeGIF(ctx, out); err != nil {
		return err
	}
	return writeRenderSummary(ctx, stdout, out)
}

// hasZoom reports whether any kept segment zooms.
func hasZoom(segments []Segment) bool {
	for _, segment := range segments {
		if !segment.Frame.Cut && segment.Frame.Zoom != nil {
			return true
		}
	}
	return false
}

// ffmpegArgs is the encode's command line: the take in, the filtergraph, the GIF out.
func ffmpegArgs(take, graph, out string) []string {
	return []string{"ffmpeg", "-y", "-loglevel", "error", "-i", take, "-filter_complex", graph, out}
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

// writeRenderSummary prints the one line the retired render.sh ended with: path, size, duration.
func writeRenderSummary(ctx context.Context, w io.Writer, path string) error {
	stat, err := os.Stat(path)
	if err != nil {
		return err
	}
	duration, err := takeDuration(ctx, path)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s  %s  %ss\n", path, humanSize(stat.Size()), number(duration.Seconds()))
	return err
}

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
