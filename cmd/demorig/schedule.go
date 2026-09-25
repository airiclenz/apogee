package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

// maxSectionSpeed is the fastest a section may play before the schedule warns: past it typing
// and streaming blur, and the beat wants a longer duration or a shorter take.
const maxSectionSpeed = 6

// BeatSpan is one beat's stretch of the take and the section timing the storyboard gives it:
// Start and End are source times (the beat's start, and the next beat's start or the take's
// end), Duration the section's target length in the clip, Hold its leading stretch shown at 1×,
// and Cut whether it is dropped from the clip.
type BeatSpan struct {
	Beat     int
	Start    time.Duration
	End      time.Duration
	Duration time.Duration
	Hold     time.Duration
	Cut      bool
}

// Section is a scheduled beat: its span, where it starts on the output clock, and the Speed its
// stretch after the hold plays at. A beat longer than its duration plays 1× for its hold and
// Speed = (L−H)/(D−H) after it; a beat that fits plays 1× throughout (Speed 1) and freezes its
// last frame for the rest of its duration. A cut section has no output time.
type Section struct {
	BeatSpan
	OutStart time.Duration
	Speed    float64
}

// Schedule maps the take's source clock onto the clip's output clock, section by section.
// Length is the clip's total length — the sum of the non-cut beats' durations — and Warnings
// names every beat that plays faster than maxSectionSpeed.
type Schedule struct {
	Sections []Section
	Length   time.Duration
	Warnings []string
}

// ScheduledFrame is one output frame: its output time, the source time it shows and its beat.
type ScheduledFrame struct {
	Out    time.Duration
	Source time.Duration
	Beat   int
}

// length is the beat's real length in the take.
func (s Section) length() time.Duration { return s.End - s.Start }

// fits reports whether the beat plays at 1× within its duration, freezing for what remains.
func (s Section) fits() bool { return s.length() <= s.Duration }

// lastInstant is the latest source offset inside the beat: its span is half-open, so the
// frame it freezes on is the one just before the next beat starts.
func (s Section) lastInstant() time.Duration {
	return max(0, s.length()-time.Nanosecond)
}

// sourceAt maps an output offset into the section onto a source time.
func (s Section) sourceAt(offset time.Duration) time.Duration {
	switch {
	case s.fits():
		return s.Start + min(offset, s.lastInstant())
	case offset < s.Hold:
		return s.Start + offset
	default:
		played := time.Duration(math.Round(float64(offset-s.Hold) * s.Speed))
		return s.Start + min(s.Hold+played, s.lastInstant())
	}
}

// outputAt maps a source offset into the section onto an output time.
func (s Section) outputAt(offset time.Duration) time.Duration {
	if s.fits() || offset < s.Hold {
		return s.OutStart + offset
	}
	return s.OutStart + s.Hold + time.Duration(math.Round(float64(offset-s.Hold)/s.Speed))
}

// NewSchedule lays the spans end to end on the output clock. Spans must be in source order and
// disjoint, and every kept span needs a positive duration with a hold shorter than it; a
// schedule whose every span is cut is an error, since it has no clip to show.
func NewSchedule(spans []BeatSpan) (*Schedule, error) {
	if err := checkSpans(spans); err != nil {
		return nil, err
	}
	schedule := &Schedule{Sections: make([]Section, 0, len(spans))}
	for _, span := range spans {
		section := Section{BeatSpan: span, OutStart: schedule.Length, Speed: 1}
		if !span.Cut {
			if !section.fits() {
				section.Speed = float64(section.length()-span.Hold) / float64(span.Duration-span.Hold)
			}
			if section.Speed > maxSectionSpeed {
				schedule.Warnings = append(schedule.Warnings, fmt.Sprintf(
					"beat %d plays at %.1f× (%s of take in %s), above %d×",
					span.Beat, section.Speed, section.length(), span.Duration, maxSectionSpeed))
			}
			schedule.Length += span.Duration
		}
		schedule.Sections = append(schedule.Sections, section)
	}
	if schedule.Length == 0 {
		return nil, errors.New("schedule: every beat is cut")
	}
	return schedule, nil
}

// checkSpans rejects a span that runs backwards or starts before the take, a pair out of order
// or overlapping, and a kept span whose duration or hold cannot be scheduled.
func checkSpans(spans []BeatSpan) error {
	for index, span := range spans {
		switch {
		case span.Start < 0:
			return fmt.Errorf("schedule: beat %d starts before the take (%s)", span.Beat, span.Start)
		case span.End < span.Start:
			return fmt.Errorf("schedule: beat %d ends (%s) before it starts (%s)", span.Beat, span.End, span.Start)
		case index > 0 && span.Start < spans[index-1].End:
			return fmt.Errorf("schedule: beat %d (%s) overlaps beat %d (ends %s)",
				span.Beat, span.Start, spans[index-1].Beat, spans[index-1].End)
		case span.Cut:
		case span.Duration <= 0:
			return fmt.Errorf("schedule: beat %d: duration: want >0, got %s", span.Beat, span.Duration)
		case span.Hold < 0 || span.Hold >= span.Duration:
			return fmt.Errorf("schedule: beat %d: hold: want in [0, %s), got %s", span.Beat, span.Duration, span.Hold)
		}
	}
	return nil
}

// Source maps an output time onto the source time it shows and that time's beat. It reports
// false outside [0, Length).
func (s *Schedule) Source(out time.Duration) (source time.Duration, beat int, ok bool) {
	if out < 0 || out >= s.Length {
		return 0, 0, false
	}
	for _, section := range s.Sections {
		if section.Cut || out >= section.OutStart+section.Duration {
			continue
		}
		return section.sourceAt(out - section.OutStart), section.Beat, true
	}
	return 0, 0, false
}

// Output maps a source time onto the output time it first shows at: the continuous inverse of
// [Schedule.Source] — 1× over a hold, divided by the speed after it — defined for every instant
// from the first beat's start to the take's end, a frozen or unsampled one included. It reports
// false for an instant inside a cut beat and for one outside the beats.
func (s *Schedule) Output(source time.Duration) (time.Duration, bool) {
	for index, section := range s.Sections {
		last := index == len(s.Sections)-1
		inside := source >= section.Start &&
			(source < section.End || (source == section.End && (last || section.length() == 0)))
		if !inside {
			continue
		}
		if section.Cut {
			return 0, false
		}
		return section.outputAt(source - section.Start), true
	}
	return 0, false
}

// Frames samples the schedule at fps: frame i sits at output time i/fps, and there are as many
// frames as the clip's length rounds to, so the clip runs its total length to within one frame.
func (s *Schedule) Frames(fps int) []ScheduledFrame {
	if fps <= 0 {
		return nil
	}
	count := int(math.Round(s.Length.Seconds() * float64(fps)))
	frames := make([]ScheduledFrame, 0, count)
	for index := range count {
		out := time.Duration(int64(index) * int64(time.Second) / int64(fps))
		source, beat, ok := s.Source(out)
		if !ok {
			break
		}
		frames = append(frames, ScheduledFrame{Out: out, Source: source, Beat: beat})
	}
	return frames
}

// SpansFrom reads each storyboard beat's span out of the take: a beat starts at its beat-start
// event and runs to the next beat's start, the last beat to the take's end. Every beat must
// have started exactly once.
func SpansFrom(board *Storyboard, take *Take) ([]BeatSpan, error) {
	starts := make(map[int]time.Duration, len(board.Beats))
	for _, event := range take.Events {
		if event.Kind != EventBeatStart {
			continue
		}
		var detail EngineEventDetail
		if err := json.Unmarshal([]byte(event.Detail), &detail); err != nil {
			return nil, fmt.Errorf("schedule: beat-start event at %s: %w", event.At, err)
		}
		if _, seen := starts[detail.Beat]; seen {
			return nil, fmt.Errorf("schedule: beat %d started twice in the take", detail.Beat)
		}
		starts[detail.Beat] = event.At
	}
	spans := make([]BeatSpan, 0, len(board.Beats))
	for _, beat := range board.Beats {
		start, ok := starts[beat.ID]
		if !ok {
			return nil, fmt.Errorf("schedule: beat %d never started in the take", beat.ID)
		}
		spans = append(spans, BeatSpan{
			Beat: beat.ID, Start: start, Duration: beat.Duration, Hold: beat.Hold, Cut: beat.Cut,
		})
	}
	if len(starts) != len(spans) {
		return nil, fmt.Errorf("schedule: the take starts %d beats, the storyboard declares %d", len(starts), len(spans))
	}
	sort.SliceStable(spans, func(a, b int) bool { return spans[a].Start < spans[b].Start })
	end := takeEnd(take)
	for index := range spans {
		spans[index].End = end
		if index+1 < len(spans) {
			spans[index].End = spans[index+1].Start
		}
	}
	return spans, nil
}

// takeEnd is the take's last instant on its clock: its last snapshot or its last event,
// whichever came later.
func takeEnd(take *Take) time.Duration {
	var end time.Duration
	if n := len(take.Snapshots); n > 0 {
		end = take.Snapshots[n-1].At
	}
	if n := len(take.Events); n > 0 {
		end = max(end, take.Events[n-1].At)
	}
	return end
}
