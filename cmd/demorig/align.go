package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/session"
)

// aligner maps the session clock onto the video clock. A session entry's At is the wall-clock
// instant it was committed; the take's clock is the video. The first user entry is the pin: its
// commit is the first prompt's Enter, whose video time the tape's head fixes
// (Align.FirstPromptAt). Every later entry maps through that offset plus PaintLag, the
// commit→paint delay:
//
//	t(at) = align.first_prompt_at + (at − firstUser.At) + paint_lag
//
// The first paint (scene detection) is deliberately not in this formula: it serves only the
// `{video: first-paint}` anchor.
type aligner struct {
	firstPromptAt time.Duration
	paintLag      time.Duration
	firstUserAt   time.Time
}

// newAligner pins the storyboard's alignment to the first user entry of the transcript. A
// transcript without a stamped user entry cannot be aligned.
func newAligner(board *Storyboard, entries []session.Entry) (aligner, error) {
	for index, entry := range entries {
		if entry.Kind != session.EntryKindUser {
			continue
		}
		if entry.At.IsZero() {
			return aligner{}, fmt.Errorf("align: the first user entry (%d) carries no timestamp", index)
		}
		return aligner{
			firstPromptAt: board.Align.FirstPromptAt,
			paintLag:      board.PaintLag,
			firstUserAt:   entry.At,
		}, nil
	}
	return aligner{}, errors.New("align: the session has no user entry to pin to")
}

// videoTime is the take time at which an entry committed at the given instant paints.
func (a aligner) videoTime(at time.Time) time.Duration {
	return a.firstPromptAt + at.Sub(a.firstUserAt) + a.paintLag
}

// firstPaintWindow bounds the scene scan to the head of the take: the first paint sits a few
// seconds in (the launch is typed into the shell first), and nothing later is a first paint.
const firstPaintWindow = 12 * time.Second

// ffmpegFirstPainter is the production FirstPainter: ffmpeg's scene-change detector over the
// head of the take, at the storyboard's threshold. The shell→TUI first paint is the first frame
// whose scene score clears it.
type ffmpegFirstPainter struct {
	Take      string
	Threshold float64
}

// ptsTimePattern captures the presentation time of one frame showinfo reports.
var ptsTimePattern = regexp.MustCompile(`pts_time:\s*([0-9]+(?:\.[0-9]+)?)`)

// FirstPaint runs the scene scan and returns the first reported frame's time. No frame clearing
// the threshold inside the window is an error: the take then has no first paint to trim to.
func (p ffmpegFirstPainter) FirstPaint(ctx context.Context) (time.Duration, error) {
	filter := fmt.Sprintf("select='gt(scene,%g)',showinfo", p.Threshold)
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-t", strconv.FormatFloat(firstPaintWindow.Seconds(), 'f', -1, 64),
		"-i", p.Take,
		"-vf", filter,
		"-f", "null", "-",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("ffmpeg scene scan of %s: %w\n%s", p.Take, err, strings.TrimSpace(stderr.String()))
	}
	match := ptsTimePattern.FindSubmatch(stderr.Bytes())
	if match == nil {
		return 0, fmt.Errorf("no scene change above %g in the first %s of %s", p.Threshold, firstPaintWindow, p.Take)
	}
	return parseSeconds(string(match[1]))
}

// takeDuration reads the take's length through ffprobe.
func takeDuration(ctx context.Context, take string) (time.Duration, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		take,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe duration of %s: %w", take, err)
	}
	return parseSeconds(strings.TrimSpace(string(out)))
}

// parseSeconds turns a decimal seconds field, as ffmpeg and ffprobe print them, into a duration.
func parseSeconds(field string) (time.Duration, error) {
	seconds, err := strconv.ParseFloat(field, 64)
	if err != nil {
		return 0, fmt.Errorf("parse seconds %q: %w", field, err)
	}
	return time.Duration(seconds * float64(time.Second)), nil
}
