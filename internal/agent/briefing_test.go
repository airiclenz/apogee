package agent

import (
	"context"
	"runtime"
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// briefingRaceRounds is how many times TestBriefing_AnInputRacingABoundarySeesItWhole races an input
// against a boundary: enough for the input to land between a boundary's lane replace and its
// window opening, were the two not one critical section.
const briefingRaceRounds = 2000

// briefingAdviseEntry is an advise entry on session-started, the shape a boundary or a live swap
// hands the briefing.
func briefingAdviseEntry(id string) domain.Reaction {
	return domain.Reaction{ID: id, Class: domain.ClassAdvise, On: []domain.Moment{domain.MomentSessionStarted}}
}

// yieldingLaunch starts nothing and yields the processor: a boundary launches its runs between the
// lane's replace and the window's opening, so the yield widens the span an input could land in were
// the two not one critical section.
func yieldingLaunch(context.Context, int, []*sessionAdviseRun) {
	runtime.Gosched()
}

// TestBriefing_AnInputRacingABoundarySeesItWhole — a boundary's lane replace and its window opening
// are one critical section, so an input racing the boundary sees it wholly before or wholly after.
// Each input records what it saw — the lane's pending runs, read as it arrives — and an input that
// saw the boundary's runs arrived after the boundary began, so it must leave the window shut: a
// live swap afterwards starts nothing. The window's state is read through liveArmed, the module's
// own door.
func TestBriefing_AnInputRacingABoundarySeesItWhole(t *testing.T) {
	t.Parallel()

	for round := range briefingRaceRounds {
		briefing := &sessionBriefing{launch: yieldingLaunch}
		boundary := sessionArmed{advise: []domain.Reaction{briefingAdviseEntry("ci")}}
		var saw []*sessionAdviseRun
		var group sync.WaitGroup
		group.Add(2)
		go func() {
			defer group.Done()
			briefing.boundaryArmed(domain.SessionStartNew, 0, boundary)
		}()
		go func() {
			defer group.Done()
			saw = briefing.lane.waiting()
			briefing.inputArrived()
		}()
		group.Wait()

		launched := 0
		briefing.launch = func(_ context.Context, _ int, runs []*sessionAdviseRun) { launched += len(runs) }
		briefing.liveArmed(sessionArmed{advise: []domain.Reaction{briefingAdviseEntry("late")}})
		briefing.close()

		if len(saw) > 0 && launched > 0 {
			t.Fatalf("round %d: the input saw the boundary's %d runs yet left the window open", round, len(saw))
		}
	}
}
