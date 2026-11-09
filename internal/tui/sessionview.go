package tui

import (
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/sidepanel"
)

// sessionView is the session view the Model renders, whole: the event-derived half the fold
// writes as Events arrive — the transcript, the /thinking and /advice boards, the status line's
// gauge and clocks, the session's token accounting and the models that answered, its background
// workflows, the per-run activity board and the lifecycle state — and beside it the rest of what a
// session boundary resets and a watch must keep apart: the open run views and the scroll and
// keyboard pointers over the paint, the flash note, the suggestion band's spent set, the session's
// naming and its cached engine boundary.
//
// It is the unit a watch swaps: watch mode parks the owner's sessionView whole (watchState.parked,
// watch.go) and paints the watched run's in its place, and every swap goes through one method
// ([Model.swapSessionView]). A field resetSessionView or resumeLoaded writes belongs here, and
// TestSessionViewHoldsEveryFieldTheBoundariesReset fails until it is (or until the guard's
// allow-list says why it stays on the Model). The Model embeds it anonymously, so every
// m.transcript, m.state and m.ctxUsed site reads as it did before the fields gained this owner.
//
// It holds plain values only — reference headers, small structs and no strings.Builder — so it
// rides the value-copied Model safely (ADR 0011); the one pointer to mutable state, panel, is
// copy-on-write, so no copy ever writes through it.
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

	// panel is the side panel's folded state (ADR 0110; internal/sidepanel): every Event the panel
	// reads, folded by the same foldEventAgainst the transcript is, so live and watch mode paint one
	// panel from one path. It is nil until the first Event the panel reads, and COPY-ON-WRITE:
	// [Model.foldSidePanel] folds into a clone and replaces the pointer, never writes through it, so a
	// discarded Model copy's fold leaves the kept one's panel as it was (ADR 0011). Both session
	// boundaries drop it — /clear (resetSessionView) and an in-TUI restore (resumeLoaded) — and a
	// watch starts its own at nil (clearWatchView).
	panel *sidepanel.State

	// panelLingerUntil is the sub-agents section's linger deadline the side panel last armed a
	// repaint for ([Model.armPanelLinger]), zero while none is armed, and panelLingerGen numbers
	// those arms: a panelLingerMsg from an arm since replaced is dropped. They live beside panel
	// because they answer its clock, and a watch parks them with it, so the owner's tick finds the
	// owner's generation.
	panelLingerUntil time.Time
	panelLingerGen   int

	// mcpServers is the process's MCP server state (ADR 0111 decisions 9 and 10; sidepanel.MCPServers),
	// folded from every domain.MCPServerEvent the view hears — live, from a watched journal, and from
	// the startup seed ([Model.seedMCP]) — by foldEventAgainst. It belongs to the PROCESS, not the conversation: a
	// server stays connected across /clear and an in-TUI restore, so neither boundary
	// (resetSessionView, resumeLoaded) writes it. It lives here rather than on the Model because a
	// watch swaps it with the rest of the view: the watched process's own fold, built from its
	// journal's mcp_server lines, is painted in its place and the owner's is parked untouched. A
	// plain value whose fold replaces its slice whole, so ADR 0011's copy rule holds.
	mcpServers sidepanel.MCPServers

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

	// viewStack is the run views that are open, outermost first, and empty at the top level: which
	// delegation — or which workflow stage, at a stage level — the transcript is painted rooted at,
	// and where the level below each one was parked (runview.go, ADR 0063). The paint's own root
	// lives on the transcript; this is the WALK — what esc and the breadcrumb go back to, and how a
	// nested delegation is unwound one level at a time.
	// A plain slice of plain values, so ADR 0011's copy rule needs no exception.
	viewStack []runView

	// detached says the human scrolled off the tail: refreshViewport holds the scroll position
	// where they put it instead of following new content, until a re-arm event (submit, /clear,
	// /continue, a flushed interjection, a resumed session) clears it. The zero value is
	// attached — a fresh Model follows the tail with no construction help. A plain bool by
	// ADR 0011: the Model is copied by value on every Update.
	detached bool

	// flash is a transient status-line note (e.g. "copied 12 chars") shown after a mouse copy or a
	// refused child message (interject.go) and cleared by flashClearMsg after flashDuration (the
	// Model's flashGen numbers it).
	flash string

	// cursor is navigation mode's KEYBOARD pointer over the paint's click targets (the Model's
	// lineTargets, blockcursor.go) and the side panel's stops: whether the walk is on, which region it
	// stands in, the content line its transcript place is on and the panel stop it remembers. Plain
	// fields by ADR 0011, and a content line rather than a screen row so the
	// highlight travels with the text it names through a scroll or a stream — refreshViewport keeps
	// it standing on a real click surface (blockCursor.clamp) exactly as it keeps that map honest.
	cursor blockCursor

	// spentSkills is the suggestion band's dedup set (suggestband.go; the band's live state is the
	// Model's skillHints): a skill named in the band at the moment a message went out is spent and
	// is never suggested again, so the band cannot nag about the same skill for the whole session.
	// It is filled at send (submit and the staged-interjection path) and emptied at every boundary
	// the conversation itself resets on — /clear and /new (commandrun.go) and an in-TUI /sessions
	// restore (resumeLoaded, sessions.go). A nil map reads as empty, so the zero-value Model needs
	// no construction step. A map is a plain reference header, safe in the value-copied Model
	// (ADR 0011).
	spentSkills map[string]bool

	// Session naming (autotitle.go; ADR 0022 addendum). autoTitleFired latches the one
	// cosmetic naming call a Session record gets — set when it fires, and set UP FRONT on a resumed
	// record, which already has a name. titleTouched records that a human named this session (the
	// browser's `r`, either form of /rename), which drops a late-landing automatic title (never
	// clobber). pendingTitle is the title stash: a title that resolved before the first Save minted
	// an id to rename, held with who asked for it and applied at that save's completion. Its rules —
	// what may be stashed, restashed, flushed or dropped — are the value's own (titleStash), and
	// every site that changes it asks for one of its verbs rather than writing the fields.
	// sessionName is the same machinery's DISPLAY side: the name this session is currently known
	// by, and the one the frame renders. It is what the human last saw decided rather than what the
	// store holds — a rename that never reaches disk leaves the frame naming the session the human
	// named — and "" means "not named yet", which the seam that renders it answers for itself. It
	// carries untrusted text (a model's reply, a resumed record's Meta), so that seam sanitizes;
	// nothing else reads it.
	// Four plain values, safe in the value-copied Model (ADR 0011); startNewSession resets all of
	// them, so the session /clear opens names itself afresh.
	autoTitleFired bool
	titleTouched   bool
	pendingTitle   titleStash
	sessionName    string

	// boundary is the most recent engine snapshot the Model saw taken at a QUIESCENT boundary —
	// a completed Turn's turnSnapshotMsg, the idle capture each worker launch takes
	// (cacheBoundaryAtIdle), or a restored record's own payload — and it is the engine half a
	// progress save pairs with the LIVE transcript while a Turn is still open (progressSave,
	// sessionsave.go). hasBoundary is its presence flag, because a zero domain.Session is itself a
	// legal envelope and so cannot say whether one was ever cached. cacheBoundary is the only site
	// that fills the pair; startNewSession is the only one that empties it.
	//
	// A domain.Session is plain values (a version and an opaque blob), so it rides the
	// value-copied Model like the usage readings above (ADR 0011).
	boundary    domain.Session
	hasBoundary bool
}
