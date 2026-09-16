package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/session"
)

// located is the transcript entry a session anchor selected: its index in the entry list and
// the entry itself. The index is what an ordering expect compares.
type located struct {
	Index int
	Entry session.Entry
}

// findEntry resolves a session anchor over the entries in list order: the kind must match and
// each of text, tool and target that the anchor sets is a prefix match on the entry's text, tool
// label and tool target. Nth picks among the matches — the first by default, the Nth, or the
// last. An anchor that selects nothing is an error spelling the anchor.
func findEntry(entries []session.Entry, a Anchor) (located, error) {
	var matches []located
	for index, entry := range entries {
		if a.matches(entry) {
			matches = append(matches, located{Index: index, Entry: entry})
		}
	}
	switch {
	case len(matches) == 0:
		return located{}, fmt.Errorf("no entry matches %s", a)
	case a.Nth == NthLast:
		return matches[len(matches)-1], nil
	case int(a.Nth) > len(matches):
		return located{}, fmt.Errorf("%s: only %d match(es), nth %d wanted", a, len(matches), a.Nth)
	case a.Nth > 0:
		return matches[a.Nth-1], nil
	default:
		return matches[0], nil
	}
}

// matches reports whether one entry satisfies the anchor's kind and prefix selectors. A tool
// selector on an entry that carries no tool view never matches.
func (a Anchor) matches(entry session.Entry) bool {
	if entry.Kind != a.Kind || !strings.HasPrefix(entry.Text, a.Text) {
		return false
	}
	if a.Tool == "" && a.Target == "" {
		return true
	}
	return entry.Tool != nil &&
		strings.HasPrefix(entry.Tool.Label, a.Tool) &&
		strings.HasPrefix(entry.Tool.Target, a.Target)
}

// String spells the anchor the way the storyboard writes it, for error messages.
func (a Anchor) String() string {
	var fields []string
	add := func(name, value string) {
		if value != "" {
			fields = append(fields, name+": "+value)
		}
	}
	add("kind", a.Kind)
	add("text", a.Text)
	add("tool", a.Tool)
	add("target", a.Target)
	switch {
	case a.Nth == NthLast:
		add("nth", "last")
	case a.Nth > 0:
		add("nth", fmt.Sprint(int(a.Nth)))
	}
	add("video", a.Video)
	if a.Beat != 0 {
		add("beat", fmt.Sprint(a.Beat))
	}
	if a.Offset != 0 {
		add("offset", a.Offset.String())
	}
	return "{" + strings.Join(fields, ", ") + "}"
}

// FirstPainter finds the shell→TUI first paint of a take: the video time of the first
// scene change. The ffmpeg adapter (ffmpegFirstPainter) is the production one; tests hand in a
// fixed value. It serves the `{video: first-paint}` anchor and nothing else — the session clock
// is pinned to the first prompt, never to the paint.
type FirstPainter interface {
	FirstPaint(ctx context.Context) (time.Duration, error)
}

// noEntry is the Index of a beat whose anchor selects no transcript entry.
const noEntry = -1

// BeatTime is one resolved beat: where it starts in the take, and the transcript entry its
// session anchor selected — Index is noEntry for a video or beat anchor.
type BeatTime struct {
	ID    int
	Title string
	At    time.Duration
	Index int
}

// resolveBeats locates every beat of the storyboard in a take. Session anchors resolve first,
// through the entries and the storyboard's alignment; then the video anchors, `first-paint`
// from the painter and `end` from the take's duration; then the beat anchors in id order,
// against the beats already resolved (their own offsets included). Every anchor's offset is
// added last. The result keeps the storyboard's beat order. An anchor that resolves to nothing
// is an error naming its beat.
func resolveBeats(
	ctx context.Context,
	board *Storyboard,
	entries []session.Entry,
	painter FirstPainter,
	duration time.Duration,
) ([]BeatTime, error) {
	align, err := newAligner(board, entries)
	if err != nil {
		return nil, err
	}
	resolved := make(map[int]BeatTime, len(board.Beats))
	place := func(beat Beat, at time.Duration, index int) {
		resolved[beat.ID] = BeatTime{ID: beat.ID, Title: beat.Title, At: at + beat.Anchor.Offset, Index: index}
	}
	for _, beat := range board.Beats {
		if !beat.Anchor.IsSession() {
			continue
		}
		entry, err := findEntry(entries, beat.Anchor)
		if err != nil {
			return nil, fmt.Errorf("beat %d: %w", beat.ID, err)
		}
		if entry.Entry.At.IsZero() {
			return nil, fmt.Errorf("beat %d: %s: entry %d carries no timestamp", beat.ID, beat.Anchor, entry.Index)
		}
		place(beat, align.videoTime(entry.Entry.At), entry.Index)
	}
	for _, beat := range board.Beats {
		if !beat.Anchor.IsVideo() {
			continue
		}
		at, err := videoPoint(ctx, beat.Anchor.Video, painter, duration)
		if err != nil {
			return nil, fmt.Errorf("beat %d: %s: %w", beat.ID, beat.Anchor, err)
		}
		place(beat, at, noEntry)
	}
	for _, beat := range board.Beats {
		if !beat.Anchor.IsBeat() {
			continue
		}
		base, ok := resolved[beat.Anchor.Beat]
		if !ok {
			return nil, fmt.Errorf("beat %d: %s: beat %d is not resolved", beat.ID, beat.Anchor, beat.Anchor.Beat)
		}
		place(beat, base.At, noEntry)
	}
	times := make([]BeatTime, 0, len(board.Beats))
	for _, beat := range board.Beats {
		times = append(times, resolved[beat.ID])
	}
	return times, nil
}

// videoPoint reads one of the take's two fixed points. The painter runs only for first-paint,
// so a storyboard without that anchor never pays for the scene scan.
func videoPoint(ctx context.Context, video string, painter FirstPainter, duration time.Duration) (time.Duration, error) {
	switch video {
	case VideoFirstPaint:
		return painter.FirstPaint(ctx)
	case VideoEnd:
		return duration, nil
	default:
		return 0, fmt.Errorf("unknown video point %q", video)
	}
}
