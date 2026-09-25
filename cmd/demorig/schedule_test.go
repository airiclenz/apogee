package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const sec = time.Second

// mustSchedule builds a schedule the test expects to be valid.
func mustSchedule(t *testing.T, spans ...BeatSpan) *Schedule {
	t.Helper()
	schedule, err := NewSchedule(spans)
	if err != nil {
		t.Fatalf("NewSchedule: %v", err)
	}
	return schedule
}

func TestScheduleMapsOutputToSource(t *testing.T) {
	t.Parallel()
	type probe struct {
		out    time.Duration
		source time.Duration
		beat   int
	}
	tests := []struct {
		name   string
		spans  []BeatSpan
		length time.Duration
		probes []probe
	}{
		{
			name:   "slow beat compressed",
			spans:  []BeatSpan{{Beat: 1, Start: 2 * sec, End: 12 * sec, Duration: 5 * sec}},
			length: 5 * sec,
			probes: []probe{{0, 2 * sec, 1}, {2500 * time.Millisecond, 7 * sec, 1}, {4 * sec, 10 * sec, 1}},
		},
		{
			name:   "short beat frozen on its last frame",
			spans:  []BeatSpan{{Beat: 1, Start: 1 * sec, End: 3 * sec, Duration: 5 * sec}},
			length: 5 * sec,
			probes: []probe{{1 * sec, 2 * sec, 1}, {3 * sec, 3*sec - time.Nanosecond, 1}, {4900 * time.Millisecond, 3*sec - time.Nanosecond, 1}},
		},
		{
			name:   "hold respected at 1x",
			spans:  []BeatSpan{{Beat: 1, Start: 0, End: 10 * sec, Duration: 4 * sec, Hold: 2 * sec}},
			length: 4 * sec,
			probes: []probe{{1 * sec, 1 * sec, 1}, {2 * sec, 2 * sec, 1}, {3 * sec, 6 * sec, 1}},
		},
		{
			name: "cut beat removed",
			spans: []BeatSpan{
				{Beat: 1, Start: 0, End: 2 * sec, Duration: 2 * sec},
				{Beat: 2, Start: 2 * sec, End: 20 * sec, Cut: true},
				{Beat: 3, Start: 20 * sec, End: 23 * sec, Duration: 3 * sec},
			},
			length: 5 * sec,
			probes: []probe{{1 * sec, 1 * sec, 1}, {2 * sec, 20 * sec, 3}, {4 * sec, 22 * sec, 3}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			schedule := mustSchedule(t, tc.spans...)
			if schedule.Length != tc.length {
				t.Errorf("length: want %s, got %s", tc.length, schedule.Length)
			}
			for _, p := range tc.probes {
				source, beat, ok := schedule.Source(p.out)
				if !ok || source != p.source || beat != p.beat {
					t.Errorf("Source(%s): want (%s, beat %d), got (%s, beat %d, ok=%v)", p.out, p.source, p.beat, source, beat, ok)
				}
			}
			if _, _, ok := schedule.Source(schedule.Length); ok {
				t.Errorf("Source(Length): want absent")
			}
		})
	}
}

func TestScheduleLengthExactAt24FPS(t *testing.T) {
	t.Parallel()
	schedule := mustSchedule(t,
		BeatSpan{Beat: 1, Start: 0, End: 9 * sec, Duration: 3300 * time.Millisecond, Hold: 500 * time.Millisecond},
		BeatSpan{Beat: 2, Start: 9 * sec, End: 10 * sec, Duration: 2250 * time.Millisecond},
		BeatSpan{Beat: 3, Start: 10 * sec, End: 40 * sec, Cut: true},
		BeatSpan{Beat: 4, Start: 40 * sec, End: 41 * sec, Duration: 1 * sec},
	)
	const fps = 24
	want := 3300*time.Millisecond + 2250*time.Millisecond + sec
	if schedule.Length != want {
		t.Fatalf("length: want %s, got %s", want, schedule.Length)
	}
	frames := schedule.Frames(fps)
	clip := time.Duration(len(frames)) * sec / fps
	frame := sec / fps
	if diff := (clip - want).Abs(); diff > frame {
		t.Errorf("clip runs %s for %d frames, want %s to within a frame", clip, len(frames), want)
	}
	for index, f := range frames {
		if index > 0 && f.Source < frames[index-1].Source {
			t.Fatalf("frame %d: source runs backwards (%s after %s)", index, f.Source, frames[index-1].Source)
		}
		if f.Beat == 3 {
			t.Fatalf("frame %d shows the cut beat", index)
		}
	}
}

func TestScheduleWarnsAboveSixX(t *testing.T) {
	t.Parallel()
	schedule := mustSchedule(t,
		BeatSpan{Beat: 1, Start: 0, End: 12 * sec, Duration: 2 * sec},
		BeatSpan{Beat: 7, Start: 12 * sec, End: 26 * sec, Duration: 2 * sec},
	)
	if len(schedule.Warnings) != 1 || !strings.Contains(schedule.Warnings[0], "beat 7") {
		t.Errorf("warnings: want one naming beat 7 (7×), got %q", schedule.Warnings)
	}
}

func TestScheduleOutputInvertsSource(t *testing.T) {
	t.Parallel()
	schedule := mustSchedule(t,
		BeatSpan{Beat: 1, Start: 1 * sec, End: 4 * sec, Duration: 5 * sec},
		BeatSpan{Beat: 2, Start: 4 * sec, End: 34 * sec, Duration: 7 * sec, Hold: 1 * sec},
		BeatSpan{Beat: 3, Start: 34 * sec, End: 50 * sec, Cut: true},
		BeatSpan{Beat: 4, Start: 50 * sec, End: 60 * sec, Duration: 4 * sec},
	)
	for _, frame := range schedule.Frames(24) {
		section := schedule.Sections[frame.Beat-1]
		if section.fits() && frame.Out-section.OutStart >= section.length() {
			continue // a frozen frame: not a played instant
		}
		out, ok := schedule.Output(frame.Source)
		if !ok || (out-frame.Out).Abs() > time.Microsecond {
			t.Fatalf("Output(Source(%s) = %s): want %s, got %s (ok=%v)", frame.Out, frame.Source, frame.Out, out, ok)
		}
	}
	if _, ok := schedule.Output(40 * sec); ok {
		t.Errorf("Output(40s) inside the cut beat: want absent")
	}
	if _, ok := schedule.Output(500 * time.Millisecond); ok {
		t.Errorf("Output(0.5s) before the first beat: want absent")
	}
	if out, ok := schedule.Output(60 * sec); !ok || out != schedule.Length {
		t.Errorf("Output(take end): want %s, got %s (ok=%v)", schedule.Length, out, ok)
	}
}

func TestScheduleClickInsideAFastBeatHasAnOutputTime(t *testing.T) {
	t.Parallel()
	// Beat 2 plays 30 s in 10 s: 3×. A click at source 5 s lands 3 s into it, 1 s on its output.
	schedule := mustSchedule(t,
		BeatSpan{Beat: 1, Start: 0, End: 2 * sec, Duration: 2 * sec},
		BeatSpan{Beat: 2, Start: 2 * sec, End: 32 * sec, Duration: 10 * sec},
	)
	if out, ok := schedule.Output(5 * sec); !ok || out != 3*sec {
		t.Errorf("Output(5s): want 3s, got %s (ok=%v)", out, ok)
	}
	// An instant between two sampled frames still maps: 5.007 s → 3.002333 s, on no 24 fps frame.
	if out, ok := schedule.Output(5*sec + 7*time.Millisecond); !ok || out != 3*sec+2333333*time.Nanosecond {
		t.Errorf("Output(5.007s): want 3.002333333s, got %s (ok=%v)", out, ok)
	}
}

func TestScheduleRejectsBadSpans(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		spans []BeatSpan
		want  string
	}{
		{"overlap", []BeatSpan{{Beat: 1, End: 3 * sec, Duration: sec}, {Beat: 2, Start: 2 * sec, End: 4 * sec, Duration: sec}}, "overlaps"},
		{"backwards", []BeatSpan{{Beat: 1, Start: 3 * sec, End: 2 * sec, Duration: sec}}, "before it starts"},
		{"no duration", []BeatSpan{{Beat: 1, End: 3 * sec}}, "duration"},
		{"hold not under duration", []BeatSpan{{Beat: 1, End: 3 * sec, Duration: sec, Hold: sec}}, "hold"},
		{"every beat cut", []BeatSpan{{Beat: 1, End: 3 * sec, Cut: true}}, "every beat is cut"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewSchedule(tc.spans); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want an error containing %q, got %v", tc.want, err)
			}
		})
	}
}

// beatStart is the beat-start event the engine logs for a beat.
func beatStart(t *testing.T, at time.Duration, beat int) TakeEvent {
	t.Helper()
	detail, err := json.Marshal(EngineEventDetail{Beat: beat})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return TakeEvent{At: at, Kind: EventBeatStart, Detail: string(detail)}
}

func TestScheduleSpansFromTake(t *testing.T) {
	t.Parallel()
	board := &Storyboard{Beats: []Beat{
		{ID: 1, Duration: 2 * sec},
		{ID: 2, Duration: 4 * sec, Hold: sec},
		{ID: 3, Cut: true},
	}}
	take := &Take{
		Snapshots: []Snapshot{{At: 0}, {At: 9 * sec}},
		Events: []TakeEvent{
			beatStart(t, 500*time.Millisecond, 1),
			{At: 600 * time.Millisecond, Kind: EventClick, Detail: `{"beat":1}`},
			beatStart(t, 3*sec, 2),
			beatStart(t, 7*sec, 3),
			{At: 8 * sec, Kind: EventActionEnd, Detail: `{"beat":3}`},
		},
	}
	spans, err := SpansFrom(board, take)
	if err != nil {
		t.Fatalf("SpansFrom: %v", err)
	}
	want := []BeatSpan{
		{Beat: 1, Start: 500 * time.Millisecond, End: 3 * sec, Duration: 2 * sec},
		{Beat: 2, Start: 3 * sec, End: 7 * sec, Duration: 4 * sec, Hold: sec},
		{Beat: 3, Start: 7 * sec, End: 9 * sec, Cut: true},
	}
	if len(spans) != len(want) {
		t.Fatalf("spans: want %+v, got %+v", want, spans)
	}
	for index := range want {
		if spans[index] != want[index] {
			t.Errorf("span %d: want %+v, got %+v", index, want[index], spans[index])
		}
	}

	take.Events = take.Events[:1]
	if _, err := SpansFrom(board, take); err == nil || !strings.Contains(err.Error(), "beat 2 never started") {
		t.Errorf("a missing beat: want it named, got %v", err)
	}
}
