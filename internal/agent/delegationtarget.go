package agent

// The Delegation target latch (ADR 0045). A session may send every delegation to a SECOND
// server — the Sub-agent server, the `servers:` entry the root `sub-agents-server:` key names — so a
// smart model orchestrates the session while a cheaper one, possibly on another box, does the
// delegated grunt work. What that server IS (endpoint, key, model, window, fan-out width, model
// profile, the wire shape it reads a thinking-effort intent in) and what its delegations run WITH
// (Bypass) is discovered by the host's second heartbeat monitor and handed to the
// engine here as one resolved value.
//
// The engine stays wire-silent (ADR 0031): it never learns that a `servers:` list exists, which
// entry the key names, or how a posture map spells itself. It is handed a resolved spec and
// builds a client from it exactly as SwitchUpstream does — the same posture Rebind takes for the
// per-model bindings and SwapTools for the tool set, which is also what keeps routing benchable
// all the way up: a bench Driver drives this setter directly, with no config file in sight.
//
// This file is the LATCH alone — the holder, its setter and its read seam — plus the target's own
// per-field contract, which is where the rule for each cell is written down. What a routed spawn
// then BUILDS from the target lives elsewhere: the Config cells the target states are projected by
// its binding() constructor (serverbinding.go), and the client it dials — with the latch read and
// the seat decided — by the spawn itself, in subagent.go.

import (
	"reflect"
	"sync"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// DelegationTarget is the resolved Sub-agent server a delegation routes to: everything a spawn
// needs to build a child against another server, computed WHOLE by the composition root from the
// flagged entry's pins and its heartbeat's observations (ADR 0045 §3). A latched target means
// routing is ENGAGED; no target — nothing latched yet, the server down, no model bound there —
// means a spawn falls back to the parent's Upstream with the parent's posture, which is the
// behaviour every session had before this existed (ADR 0045 §4).
//
// It is treated as IMMUTABLE once latched: the host resolves a fresh value per beat and installs
// that, so a snapshot taken at a spawn stays valid for the life of the child even as later beats
// replace what the latch holds.
type DelegationTarget struct {
	// Endpoint is the OpenAI-compatible base URL a routed child dials — the flagged entry's
	// `endpoint:`. Required: a target without one cannot be dialed, so the host latches nil
	// instead and the spawn falls back.
	Endpoint string
	// APIKey is that endpoint's bearer token, empty for a server that needs none.
	APIKey string
	// Wire is the protocol family the target server speaks — the flagged entry's `wire:` key,
	// "openai" or "anthropic" (ADR 0078; domain.Config.Wire's spelling, converted at the dial). A
	// routed child dials with the TARGET's own value and never falls back to the parent's: the
	// child is on another server, and a wire is a fact about the server, so "" here means the
	// target names none and folds to openai — the parent's wire says nothing about this box.
	Wire string
	// ServerName is the flagged `servers:` entry's name — the server the routed child dials, in the
	// human's words. It lands on the child's Config as written, "" included, so a routed child is
	// never identified by the name of the parent's server.
	ServerName string
	// RequestExtra is the flagged entry's `request-extra:` passthrough (ADR 0085) — canonical JSON
	// the child's Client merges over every body it sends. The TARGET's own value, "" included: a
	// passthrough is a fact about the server, so a routed child never carries the parent's.
	RequestExtra string
	// StreamIdleTimeout is the target server's stream-idle window — how long a stream from it may
	// stay silent before the routed child's Client cuts it (domain.Config.StreamIdleTimeout): the
	// flagged entry's own `stream-idle-timeout:`, else the root key, as the host resolved it. It
	// bounds the child's dial and the child's fold alike. nil ⇒ the target states no window and the
	// child keeps the parent's; a stated value replaces it as written, 0 included, which turns the
	// cut off for the child.
	StreamIdleTimeout *time.Duration
	// Price is the flagged entry's `price:` (ADR 0093 decision 3): every call a routed child makes
	// is priced at the server it dials, so this is the TARGET's own value, the unpriced zero
	// included — a child routed to an entry with no `price:` counts its calls as unpriced and never
	// borrows the parent's rate (decision 4). An unrouted child inherits the parent's Config, and
	// with it the parent's price, because it calls the parent's server.
	Price domain.ServerPrice
	// Vision is the flagged entry's `vision:` opt-in (domain.Config.Vision): the TARGET's own value,
	// false included, for the price's reason — a routed child is sent images only when the server it
	// dials accepts them. An unrouted child inherits the parent's, because it calls the parent's server.
	Vision bool
	// Model is the model id a routed child sends on the wire — the entry's `model:` pin, else the
	// model its heartbeat observed bound there. Required for the same reason Endpoint is: a
	// delegation that cannot name a model is not a usable target, it is the fallback.
	Model string
	// ContextWindow is Model's bound context window in tokens — the entry's `context-window:` pin,
	// else the observed per-slot window. 0 means the target names NO window (neither pinned nor
	// observed), and it is the one field an unusable value does not make the whole target unusable:
	// the spawn keeps the PARENT's window instead of inheriting the zero, so a routed child is never
	// built windowless — with a dead Budget, no automatic Compaction and readings stamped 0, which
	// sends both Drivers to their "the reading names none" fallback and paints a routed fill against
	// the SESSION's window, the one window that child is not in — over a number nobody supplied. The
	// parent's number is the better wrong answer than none, and it is what an UNROUTED child gets
	// anyway. A NEGATIVE value folds in with 0 rather than meaning something of its own: config
	// refuses a negative pin and a beat cannot observe one, so both spellings say the same thing
	// here. That present-only-when-positive rule is stated once, in binding() (serverbinding.go).
	ContextWindow int
	// WorkingWindow is the room INSIDE ContextWindow a routed child actually works in, in tokens —
	// the flagged entry's `working-window:` pin. There is no observed half to fall back to: a server
	// reports no working room, it is an operator's bound on what a delegation there may spend. 0
	// means the target names none, and — unlike the window above — the zero IS carried to the child
	// as written, for MaxOutputTokens' reason: an absent bound is not a broken child, it is a child
	// working in the whole of the TARGET's window. Inheriting the parent's bound instead would fence
	// a run on this server by a number sized for another one's window.
	WorkingWindow int
	// MaxOutputTokens is the ceiling on ONE reply from this server, in tokens — the flagged entry's
	// `max-output-tokens:` pin (ADR 0046). 0 means the target pins NO cap, and — unlike the window
	// above — the zero is carried to the child as written rather than left to the parent's: a cap of
	// nothing is not a broken child, it is a child that derives its own cap from the window it runs
	// in, which by then is the TARGET's window. Inheriting the parent's pin instead would cap a
	// reply from this server at a number describing another one.
	MaxOutputTokens int
	// ResponseReserveFraction is the share of ContextWindow this server holds back for one reply —
	// the flagged entry's `response-reserve:` override, as written. 0 means the entry states NO
	// share, and — like the window above rather than the cap between them — the zero is NOT carried:
	// the child keeps the share the parent resolved, which is the run's own top-level
	// `response-reserve:` when nobody overrode it. That is the honest fall-through here, because a
	// share is a fraction of whatever window the child ends up in rather than a number describing one
	// server's slot, so the run-wide split is meaningful on the routed server too — where an absent
	// reply CEILING is not, being a token count off the retired server's window.
	ResponseReserveFraction float64
	// ParallelAgents is the RECEIVING server's fan-out width — the cap that bounds a routed
	// depth-0 fan-out and a Reaction's synthesized batch alike (ADR 0039's one width everywhere,
	// sourced from the server actually holding the slots). Anything < 2 means serial.
	ParallelAgents int
	// Profile is the model profile resolved for Model (ADR 0044): how the grunt model speaks the
	// wire, on the two axes of tool-call format and inline thinking channel. The zero value is
	// meaningful — native tool calls, no inline thinking — so a routed child on a model that
	// matches nothing parses exactly as an unprofiled session does.
	Profile domain.ModelProfile
	// EffortDialect is the wire shape THIS server reads a thinking-effort intent in — the flagged
	// entry's `effort-dialect:` pin, else the shape its own heartbeat saw a passive tell for (ADR
	// 0060 §3, where the dialect is a property of the SERVER). Any named value REPLACES the
	// parent's, because a routed child is on another server: an intent expressed in the
	// orchestrator's shape reaches this one as a field it ignores, which is how a routed
	// summariser's "no reasoning" went unheard and spent the whole fold cap thinking.
	//
	// The zero means something different HERE from what it means on a RebindSpec: there it is the
	// wire anchor a caller that names no dialect settles on, here it says this TARGET names none —
	// the entry pins nothing and the server advertised no tell — and the child then keeps the
	// PARENT's dialect, exactly as every delegation did before this field existed.
	EffortDialect provider.EffortDialect
	// Bypass is the flagged entry's `bypass:` posture (ADR 0045 §2). Non-nil REPLACES the
	// inherited value whole: delegations to this server run with this flag whatever the parent's
	// is. nil inherits the parent's LIVE flag at spawn, which is today's rule.
	Bypass *bool
	// Fallback is the Sub-agents seat's Server fallback (ADR 0105): the resolver a routed child
	// consults before it abandons a Turn for a fault that is this server's own, built by the host
	// from the flagged entry's own `fallback:` list. It is ONE instance per seat, carried by every
	// target the host latches for that seat, so N routed siblings faulting on one dead server share
	// one grace window, one list walk and one UpstreamSwitchedEvent. nil — an entry naming no
	// `fallback:` — consults none, and a routed child abandons on the fault exactly as before. A
	// routed child never consults the session's resolver: it is on another seat.
	Fallback FallbackResolver
}

// delegationLatch holds the current Delegation target behind an RWMutex. It is a POINTER on the
// Agent, and newChildAgent hands the child the parent's holder rather than a copy, so one latch
// serves a whole agent tree: a depth-2 spawn reads the same current target its grandparent's next
// spawn would, and the host has exactly one place to push to however deep the tree runs.
//
// The mutex is the whole reason it is a type of its own: the target is written by the host's
// heartbeat goroutine and read by every goroutine that spawns a child — including the workers of
// a depth-0 fan-out, which spawn AT ONCE (ADR 0039) — so the shared-mutable-state rule applies
// and the holder owns the lock for it.
//
// Being the one value a whole tree shares, it also holds the Sub-agents seat's announcement claims
// (claimAnnouncement): every routed sibling moves a holder of its own, so the seat's one
// UpstreamSwitchedEvent per move is claimed here, tree-wide, rather than by any one holder.
//
// It also holds what a routed fallover needs to re-latch the seat (relatch): the resolver the latch
// is armed on, the root Agent's far-width setter, so the move lands on the tree's one latch and on
// the root's stated width whichever depth the landing child sits at, and the re-latch claims that
// let each landing move the latch once. And it carries the root's session-width setter, so a
// session-seat rehome landed at any depth states the fallback's width on the root
// (FallbackAnswer.ParallelAgents).
type delegationLatch struct {
	mu     sync.RWMutex
	target *DelegationTarget
	// armed is the Sub-agents seat resolver a routed fallover may re-latch for: the Fallback of the
	// last non-nil target latched. A nil latch — a target-down beat, which the dead primary's beats
	// are before the grace ends — leaves it standing; the human's `/sub-agents-server` door clears it
	// (disarm), and a relist that latches a target carrying another resolver re-arms it on that one.
	armed FallbackResolver
	// stateWidth is the ROOT Agent's far-width setter (stateFarWidth), bound when the root is built
	// (seedTopLevel) and never written after, so a re-latch from a routed child at any depth states
	// the far width where the orientation block reads it. A session-seated child's empty latch
	// carries the root's too (rootBound), though nothing ever arms that latch to re-latch it.
	stateWidth func(int)
	// setSessionWidth is the ROOT Agent's session-width setter (SetParallelAgents), bound and carried
	// as stateWidth is, so a session-seat rehome landed by any Agent of the tree — a session-seated
	// child on its own empty latch included — states the fallback's width on the root
	// (Agent.stateSessionWidth). nil on a latch no root built.
	setSessionWidth func(int)
	// announced holds every routed fallover already announced in this tree, keyed by the seat and
	// the server it moved to (fallbackClaim); nil until the first claim.
	announced map[fallbackClaim]struct{}
	// relatched holds every routed landing already offered to the latch, keyed as announced is but
	// claimed apart from it: a landing re-latches when its answer is observed (relatch), which a
	// caller cancelled during the wait does without ever moving a holder or announcing; nil until the
	// first claim.
	relatched map[fallbackClaim]struct{}
}

// comparableResolver reports whether resolver's dynamic value can be compared with == and used as a
// map key without panicking (reflect.Value.Comparable, which also looks inside an interface field).
// A nil resolver is not: there is nothing to compare.
func comparableResolver(resolver FallbackResolver) bool {
	return resolver != nil && reflect.ValueOf(resolver).Comparable()
}

// fallbackClaim keys one routed fallover's announcement: the Sub-agents seat that moved and the
// `servers:` entry it moved to (FallbackAnswer.Server).
type fallbackClaim struct {
	// seat stands for the seat that moved: its FallbackResolver — one instance serves one seat — when
	// that value can be a map key, else the moving Agent's own holder, so a resolver of an
	// uncomparable dynamic type (a func adapter, a struct holding a slice) never panics the hash.
	seat   any
	server string
}

// claimFallbackKey builds the claim key for a move onto server by a seat consulting resolver over
// holder: keyed on the resolver when its dynamic value is comparable (reflect.Value.Comparable,
// which also looks inside an interface field), else on the holder.
func claimFallbackKey(resolver FallbackResolver, holder *sharedUpstream, server string) fallbackClaim {
	if comparableResolver(resolver) {
		return fallbackClaim{seat: resolver, server: server}
	}
	return fallbackClaim{seat: holder, server: server}
}

// claimAnnouncement reports true to the first claim of key in this tree and false to every later
// one, across every goroutine.
func (l *delegationLatch) claimAnnouncement(key fallbackClaim) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return claimOnce(&l.announced, key)
}

// claimOnce records key in claims, making the map on first use, and reports whether this was its
// first claim. The caller holds the latch's mutex.
func claimOnce(claims *map[fallbackClaim]struct{}, key fallbackClaim) bool {
	if _, done := (*claims)[key]; done {
		return false
	}
	if *claims == nil {
		*claims = make(map[fallbackClaim]struct{})
	}
	(*claims)[key] = struct{}{}
	return true
}

// set installs target as the current one, nil clearing it. A non-nil target arms the latch on its
// resolver; a nil one leaves the armed resolver standing.
func (l *delegationLatch) set(target *DelegationTarget) {
	l.mu.Lock()
	l.install(target)
	l.mu.Unlock()
}

// install is set's body, for a caller already holding mu.
func (l *delegationLatch) install(target *DelegationTarget) {
	l.target = target
	if target != nil {
		l.armed = target.Fallback
	}
}

// disarm clears the armed resolver, so no routed fallover re-latches until a host latches a usable
// target again — what the human's `/sub-agents-server` door does (SetDelegationSeat).
func (l *delegationLatch) disarm() {
	l.mu.Lock()
	l.armed = nil
	l.mu.Unlock()
}

// isArmedOn reports whether the latch is armed on resolver, under relatch's comparability guard: a
// resolver whose dynamic value cannot be compared is never armed-on.
func (l *delegationLatch) isArmedOn(resolver FallbackResolver) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return comparableResolver(l.armed) && comparableResolver(resolver) && l.armed == resolver
}

// relatch installs target — a routed landing's FallbackAnswer.Target, the landing onto server — when
// resolver, the landing child's seat resolver, is the one the latch is armed on, and states target's
// width on the root (stateWidth). Each (resolver, server) landing is offered once (relatched): the
// first observer of it decides, so a straggler or a later consult of a resolver run that cached its
// switch moves no latch the tree has re-stated since. The claim, the check and the install are one
// critical section, so a beat or a `/sub-agents-server` switch landing beside it is wholly before
// (and decides it) or wholly after. The two resolvers are compared only when both dynamic values are
// comparable — claimFallbackKey's guard — so an embedder's func adapter never panics the compare or
// the claim; such a seat is never re-latched, since the engine cannot tell its resolver is still the
// armed one. It reports whether it re-latched.
func (l *delegationLatch) relatch(resolver FallbackResolver, server string, target *DelegationTarget) bool {
	if !comparableResolver(resolver) {
		return false
	}
	l.mu.Lock()
	first := claimOnce(&l.relatched, fallbackClaim{seat: resolver, server: server})
	same := first && comparableResolver(l.armed) && l.armed == resolver
	if same {
		l.install(target)
	}
	l.mu.Unlock()
	if same && l.stateWidth != nil {
		l.stateWidth(target.ParallelAgents)
	}
	return same
}

// snapshot reports the currently latched target under the read lock, nil meaning none. The value
// behind the pointer is never mutated in place — the host installs a fresh one per beat — so the
// caller may read it long after the lock is dropped.
func (l *delegationLatch) snapshot() *DelegationTarget {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.target
}

// rootBound is an EMPTY latch of its own — no target, no armed resolver, no claims — that still
// writes this latch's root widths (stateWidth, setSessionWidth): what a session-seated child is
// handed, so its nested delegations stay on the session server while a session-seat rehome it lands
// still reaches the root. The setters are bound once by the root (seedTopLevel) before any child can
// share the latch and never written after, so they are read here without the lock. A nil latch
// answers a bare empty one.
func (l *delegationLatch) rootBound() *delegationLatch {
	if l == nil {
		return &delegationLatch{}
	}
	return &delegationLatch{stateWidth: l.stateWidth, setSessionWidth: l.setSessionWidth}
}

// SetDelegationTarget latches the Sub-agent server every delegation in this agent tree routes to,
// or clears it with nil so delegations fall back to running on this session's own Upstream with
// this session's posture (ADR 0045 §4). The host resolves the spec whole — from the flagged
// entry's pins and its own heartbeat's observations — and re-states it on every beat; the engine
// applies what it is handed and reads no config of its own (ADR 0031).
//
// It is deliberately NOT idle-gated, and that is the contrast with Rebind, SwitchUpstream and
// SetProfile: those swap bindings the OPEN Exchange is mid-way through using, so they refuse
// mid-Exchange and wait for a quiescent boundary. This one changes nothing about the running
// session — it changes what the NEXT spawn builds — and beats land whenever the second monitor
// beats, which is squarely mid-Exchange for any delegation-heavy Turn. So it belongs to the
// anytime-goroutine-safe class alongside SetParallelAgents: it swaps the latch behind the latch's
// own lock — and a usable target's far width behind parallelAgentsMu, below — and each spawn
// snapshots whatever is current. A child already running is never re-routed;
// a target that changes mid-fan-out reaches the spawns after it and leaves the ones before it
// alone.
//
// Calling it on a sub-agent is legal but re-routes the WHOLE tree, holder being shared: hosts push
// to the top-level Agent they constructed.
//
// A usable target also STATES the far server's width for the number the engine tells the model
// (statedDelegationWidth, delegationwidth.go): the fan-out ceiling's multiplier and the orientation block's
// bounds. Only a non-nil target writes it — a nil one is a target-down beat, and the stated width
// stands across it — so the model-facing number moves when a cap is first stated and not when the
// far server flaps; the human's `/sub-agents-server` door (SetDelegationSeat) is what forgets it.
func (a *Agent) SetDelegationTarget(target *DelegationTarget) {
	a.delegation.set(target)
	if target != nil {
		a.stateFarWidth(target.ParallelAgents)
	}
}

// delegationTarget snapshots the tree's current Delegation target for one spawn, nil meaning
// routing is not engaged and the caller takes the parent-inheriting path. It is the ONE read seam
// for the latch, so every consumer reads it race-free against the host's next beat.
func (a *Agent) delegationTarget() *DelegationTarget {
	return a.delegation.snapshot()
}
