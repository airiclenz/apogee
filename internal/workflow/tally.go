package workflow

import (
	"strconv"
	"strings"
)

// The words a tally line is built from.
const (
	tallyItemsWord      = "items "
	tallyOKWord         = "ok "
	tallyPartialWord    = "partial "
	tallyBlockedWord    = "blocked "
	tallyUnfinishedWord = "unfinished "
	tallyResumedWord    = "resumed "
	tallyConfirmedWord  = "confirmed "
	tallyRefutedWord    = "refuted "
	tallyUnclearWord    = "unclear "
)

// TallyOf is a run's item tally, the one every surface reports a finished workflow by: the sum of
// its fan-out stages' tallies, a skipped fan-out left out. Verify, merge, script and ask outcomes
// are not items. A repeat stage's re-runs replace the stage they repeat in Result.Stages, so a
// repeated fan-out counts its latest round only. Each stage's own Tally is summed as it stands — its
// Items are never recounted — so the resumed count and the verify verdicts carry through.
func TallyOf(result Result) Tally {
	var tally Tally
	for _, stage := range result.Stages {
		if stage.Kind != StageFanout || stage.Phase == PhaseSkipped {
			continue
		}
		tally.add(stage.Tally)
	}
	return tally
}

// TallyOfStatus is the item tally a workflow's status.json shows so far: its fan-out stages' items,
// a skipped fan-out left out, counted by the receipt a finished item holds and as unfinished until
// then. status.json records neither which items an earlier run supplied nor the verify verdicts, so
// the resumed and verdict counts stay zero.
func TallyOfStatus(status RunStatus) Tally {
	var tally Tally
	for _, stage := range status.Stages {
		if stage.Kind != StageFanout || stage.Phase == PhaseSkipped {
			continue
		}
		for _, item := range stage.Items {
			tally.countOutcome(item.Phase, item.Receipt)
		}
	}
	return tally
}

// Total is how many items the tally counts: ok, partial, blocked and unfinished. Resumed items are
// already among the finished ones, and a verdict is about an item already counted.
func (t Tally) Total() int {
	return t.OK + t.Partial + t.Blocked + t.Unfinished
}

// Line renders the tally as one line — `items N · ok A · partial B · blocked C`, then the
// unfinished count when there is one, then `confirmed · refuted · unclear` when a verify checked
// any item. It never shows the resumed count: that is the per-stage totals line's alone.
func (t Tally) Line() string {
	return t.render(false)
}

// render renders the tally's segments in their fixed order, the resumed count among them only
// when hasResumed and there is one.
func (t Tally) render(hasResumed bool) string {
	parts := []string{
		tallyItemsWord + strconv.Itoa(t.Total()),
		tallyOKWord + strconv.Itoa(t.OK),
		tallyPartialWord + strconv.Itoa(t.Partial),
		tallyBlockedWord + strconv.Itoa(t.Blocked),
	}
	if t.Unfinished > 0 {
		parts = append(parts, tallyUnfinishedWord+strconv.Itoa(t.Unfinished))
	}
	if hasResumed && t.Resumed > 0 {
		parts = append(parts, tallyResumedWord+strconv.Itoa(t.Resumed))
	}
	if t.Confirmed+t.Refuted+t.Unclear > 0 {
		parts = append(parts,
			tallyConfirmedWord+strconv.Itoa(t.Confirmed),
			tallyRefutedWord+strconv.Itoa(t.Refuted),
			tallyUnclearWord+strconv.Itoa(t.Unclear),
		)
	}
	return strings.Join(parts, totalSeparator)
}

// add adds other's every count to t.
func (t *Tally) add(other Tally) {
	t.OK += other.OK
	t.Partial += other.Partial
	t.Blocked += other.Blocked
	t.Unfinished += other.Unfinished
	t.Resumed += other.Resumed
	t.Confirmed += other.Confirmed
	t.Refuted += other.Refuted
	t.Unclear += other.Unclear
}

// countOutcome counts one item by how it stands: by its receipt's status once it is done with
// one, unfinished otherwise — a status other than ok or partial counts as blocked.
func (t *Tally) countOutcome(phase Phase, receipt *Receipt) {
	if receipt == nil || phase != PhaseDone {
		t.Unfinished++
		return
	}
	switch receipt.Status {
	case StatusOK:
		t.OK++
	case StatusPartial:
		t.Partial++
	default:
		t.Blocked++
	}
}

// StateKind is which of the three ways a workflow stands that State reports.
type StateKind int

// The state kinds. A workflow this session's background manager holds is queued or running in the
// background; any other — a blocking fan_out's, or a background run that has ended — stands at
// the phase its status.json records, running included when a run ended without settling it.
const (
	// StateQueued is a background workflow waiting behind another on its server.
	StateQueued StateKind = iota
	// StateBackground is a background workflow running now.
	StateBackground
	// StateRecorded is a workflow no manager holds, at its recorded phase (State.Phase).
	StateRecorded
)

// State is how a workflow stands: its kind and, for StateRecorded only, the phase status.json
// records.
type State struct {
	Kind  StateKind
	Phase Phase
}

// StateOf is where the workflow that info describes stands, as a listing shows it: queued when
// the manager marks it Queued (whether or not it also marks it Background), running in the
// background when it marks it Background, else at its recorded phase. A folder no manager holds is
// never StateBackground, even when its status.json still says running.
func StateOf(info Info) State {
	switch {
	case info.Queued:
		return State{Kind: StateQueued}
	case info.Background:
		return State{Kind: StateBackground}
	default:
		return State{Kind: StateRecorded, Phase: info.Status.Phase}
	}
}

// ItemStatusWord is the one word an item line leads with: the receipt's status for an item done
// with one, the item's phase (pending, running, stopped, …) otherwise.
func ItemStatusWord(phase Phase, receipt *Receipt) string {
	if phase == PhaseDone && receipt != nil {
		return string(receipt.Status)
	}
	return string(phase)
}
