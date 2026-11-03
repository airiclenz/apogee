package tui

import "github.com/airiclenz/apogee/internal/domain"

// sessionView is the event-derived half of the session view the Model renders: the transcript, the
// /thinking and /advice boards, the status line's gauge and clocks, the session's token accounting
// and the models that answered, its background workflows, the per-run activity board and the
// lifecycle state. It is what the fold writes as Events arrive, and the unit a watch swaps: watch
// mode parks the owner's sessionView whole beside the rest of the parked view (parkedSession,
// watch.go) and paints the watched run's in its place. The Model embeds it anonymously, so every
// m.transcript, m.state and m.ctxUsed site reads as it did before the fields gained this owner.
//
// It holds plain values only — reference headers, small structs and no strings.Builder — so it
// rides the value-copied Model safely (ADR 0011).
type sessionView struct {
	// transcript is the conversation as the view holds it: the entries, their run blocks and the
	// display preferences a reset keeps (transcript.go).
	transcript transcript

	// thinking is the session's thinking as the view RETAINS it — one bounded, escape-stripped
	// record per agent per Turn of the chunks domain.ReasoningEvent reveals, completed records
	// newest last beside the in-flight one for each run currently thinking (thinking.go). It is
	// what the /thinking pane reads and composes its rows out of; the pane keeps nothing of its
	// own. What the status line says about reasoning it still says from the ARRIVAL of the event
	// (acts, below), never from these bytes.
	thinking thinkingBoard

	// advice is the /advice pane's board (advicepane.go): every advise firing the session's agents
	// handed their model — a user `advise:` entry's fenced text, the context-fill notice's rung — as
	// the fold recorded them off domain.ReactionFiredEvent, oldest first, bounded to the newest
	// maxAdviceRecords. It sits BESIDE the transcript like the Model's wire ring: a firing is not a
	// conversation entry and disturbs no entry pairing, and it is rebuilt rather than appended into,
	// so it rides safely in the value-copied Model (ADR 0011).
	advice []adviceRecord

	// liveStats is what the status line reports about the conversation the session is holding right
	// now: the context gauge's fill and its estimate, the generation clock, and the last completion's
	// throughput. It is EMBEDDED anonymously (the promptEditor posture), so m.ctxUsed, m.genStart and
	// m.tokPerSec read exactly as they always did while the four of them gain one owner — and with it the one
	// reset every session boundary spells out by hand (liveStats.reset).
	liveStats

	// usage is the MAIN agent's cumulative token accounting for the session — the latest reading
	// its Depth-0 UsageEvents carried (domain.Usage, read by usageReading), including the maintenance events the
	// gauge above skips. It is persisted with the session record and restored on reopen, so a
	// resumed session reports what it has already spent. A sub-agent's totals live on its own run
	// head instead (transcript.applyUsage), which is where the per-agent grouping already is.
	usage domain.Usage

	// usageBase is the accounting the session record carried when this session was (re)opened: the
	// main agent's stored totals on either resume path (replayResumed, resumeLoaded), and the zero
	// value on a fresh launch or after /clear (resetSessionView). It exists because the ENGINE's
	// cumulative reading restarts at zero on every session boundary — a fresh Agent on --resume,
	// RestoreSession's reset on a browser restore, ClearContext's on /clear — so the
	// usage fold adds that reading ON TOP of this base instead of letting it replace what the record
	// carried in (foldStats). Without it a resumed session's spend collapses to whatever the first
	// post-resume reading says. The offset is the renderer's own because it is the Driver that seeds
	// a view from a record: the engine never sees one.
	usageBase domain.Usage

	// delegateUsage is the delegate half of a RESUMED record's accounting (session.Meta), and only
	// ever a fallback: a live run head's own reading replaces it the moment one reports, so the two
	// are never added together (delegateUsageTotal). It exists for the record whose scrollback did
	// not come back — a legacy or undecodable blob replays no run blocks — where the sum the record
	// stored is the only thing left that knows a delegate spent anything at all.
	delegateUsage domain.Usage

	// servedModels is every distinct model id the upstream has answered with in this session, in
	// the order first seen — the ServedModel each UsageEvent carries, at EVERY depth (a routed
	// delegation's own model, ADR 0045, is one the session was answered by), folded as a set
	// (foldStats). It is a record fact the renderer keeps because it is the one that sees the
	// readings: it is seeded from a resumed record (ResumedSession.ServedModels, resumeLoaded),
	// saved with every snapshot beside the tallies (savePayload), painted by /usage as `served:`,
	// and zeroed with the tallies on /clear (resetSessionView). Nil until a reply names a model.
	servedModels []string

	// sessionCurrency is the resumed record's own currency label (ADR 0093, amendment 2026-10-04):
	// the label its priced amount was written in, which outlives a `currency:` changed since. It is
	// an OVERRIDE only — "" reads the configured label live ([Model.currency]) — seated by either
	// resume path from the label the binary or session.Meta resolved (replayResumed, resumeLoaded),
	// escape-stripped there because it is disk input, and dropped on /clear (resetSessionView).
	// While it names a label other than the configured one, every later reading counts its calls
	// as unpriced ([Model.countedUsage]): their cost was reckoned in the configured currency, and
	// adding it to an amount in another would misstate both.
	sessionCurrency string

	// workflows is the session's background workflows as their events fold (workflow.go, ADR 0089):
	// each live one's name and item tally, and the runs it spawned. Its events go there and to the
	// Inspector only — never the transcript, the activity board or the stall clock — which is what
	// keeps the conversation's own view, and a stop key, off work that runs beside it.
	workflows backgroundWorkflows

	// acts is the live activity board the status line renders while a worker runs — thinking,
	// responding, a named tool, retrying, compacting, stopping (activity.go) — one slot per RUN
	// rather than one for the session, keyed by the run identity (runActivities). It is derived
	// from the Event stream (foldActivity) plus the transitions no Event announces, and it is
	// deliberately NOT a uiState: the lifecycle machine (state, below) is untouched by it (ADR 0011).
	//
	// One slot per run is what a fan-out needs: concurrent delegates (ADR 0039) used to overwrite
	// one another in a single slot, so the row jittered between whichever child spoke last. The
	// board keeps each run's phrase and clock apart, and runningPhrase composes the ONE sentence
	// the row has room for out of them. A map, like the Model's spentSkills, and safe on the same terms
	// (runActivities).
	acts runActivities

	// state is the lifecycle: the machine the keys route on (uiState). A watch parks it with the
	// rest of the view, so the owner's run state stands aside while the watched run's is painted.
	state uiState
}
