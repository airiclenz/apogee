package agent

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tasklist"
)

// The engine's conversation storage is domain.Conversation: it is already library-complete
// (role-tagged messages with tool calls + tool-call IDs, the FIFO deferred-action queue, and
// per-message Extra wire fields), so the loop appends to it directly and the snapshot
// serializes it without a parallel throwaway type.
//
// Domain owns the outer Session envelope and its Version; the engine owns the opaque
// Session.State payload (ADR 0010). agentState is that payload's v1 schema — the complete
// quiescent-boundary state of the loop that Config does not re-supply on Resume:
//
//   - conversation : the role-tagged message history (with tool-call/result pairing and
//     per-message Extra wire fields) plus the pending Outcome{Defer} queue, serialized by
//     domain.Conversation itself.
//   - turnIndex    : the 0-based index of the next Turn, so Resume continues the Exchange at
//     the right Turn rather than re-zeroing it (the P0.6 gap P1.6 closes).
//   - inExchange   : whether a multi-Turn Exchange is mid-flight, so a resumed Agent rejects
//     a Submit that would corrupt an open Exchange and the next Step continues it.
//   - exchangeStart: the conversation boundary the open Exchange began at, so a resumed host
//     that discards a cancelled Exchange (AbortExchange) rolls back to the right boundary
//     rather than wiping unrelated history. Load-bearing, not legacy: the boundary cannot be
//     re-derived from the conversation (ADR 0017 §2's recorded fallback — Agent.exchangeBoundary),
//     so it keeps round-tripping.
//   - pendingInput : input Submitted but not yet consumed by a Step, so a Submit→Snapshot→
//     Resume sequence does not silently drop the queued message.
//   - tasks        : the model's task list (ADR 0072) — the checklist it wrote through the
//     task_list tool, so a resumed session still knows what is left. It is omitempty, which
//     is what makes the field additive in BOTH directions and leaves domain.SessionVersion at
//     1 (the session.Meta ScheduleID precedent): a snapshot written before the list existed
//     simply lacks the key and restores an empty list, and an older binary reading a newer
//     snapshot preserves nothing it does not understand only in the payload it rewrites — a
//     concern the list is deliberately cheap enough to accept, being sentences the model can
//     restate.
//
// The per-message Interjected marker rides the conversation's own marshal as an omitempty
// sibling, so it needs NO SessionVersion bump in either direction: a snapshot written before
// the marker existed simply lacks the key (decoding false — no message was ever interjected),
// and an older binary reading a newer snapshot preserves it as an unknown wire field and
// writes it back untouched (domain.Message's Extra passthrough).
//
// The live delegates (Approver, Confiner, EventSink), the resolved tool registry and
// Reaction set, and the allow-for-session approval cache are deliberately NOT serialized:
// Resume re-supplies the delegates and the sets afresh (ADR 0001), and a resumed Session
// re-confirms allow-for-session grants rather than silently carrying a prior process's write
// authorizations — the safer default for a human-in-the-loop gate. This is v1; a later schema
// that needs the cache adds it under a new SessionVersion.
//
// The undo journal (Agent.journal, ADR 0051) is on that withheld list for a stronger reason
// than any of the above: it is LIVE HOST STATE, not session state (ADR 0022 §8). Its records
// describe files as this process left them, and a snapshot restored on another machine — or
// on this one after the workspace moved on — would hand the human pre-images to write over
// bytes that are no longer the agent's. What a resumed Session reverts from is therefore never
// read out of the record: the Driver reopens the session's own snapshot store — keyed by session
// id, and checked against the workspace it imaged — and hands the journal it loaded over
// (SetJournal), so `/undo` reaches an earlier process's writes without the record ever carrying
// them (ADR 0074). Where no store opens, the journal holds this process's writes and no others.
//
// The console registry (Agent.consoles, ADR 0059) is withheld on that same ground, taken to its
// limit: what it holds are RUNNING PROCESSES, which cannot be written into a file at all. A
// resumed Session therefore has no Consoles, and an id the model remembers from the snapshot's
// own run addresses nothing — which the tools report as an unknown id, the whole truth about a
// Console whose process is gone.
//
// The task list (Agent.tasks, ADR 0072) is the counter-example that makes that rule readable: it
// IS in the payload above, because what it holds is not a live resource of this process but the
// sentences the model wrote about its own work — nothing that can go stale against a filesystem,
// nothing that has to be running to mean anything, and precisely what a resumed session has the
// least other way of getting back.
type agentState struct {
	Conversation  *domain.Conversation `json:"conversation"`
	TurnIndex     int                  `json:"turnIndex"`
	InExchange    bool                 `json:"inExchange,omitempty"`
	ExchangeStart int                  `json:"exchangeStart,omitempty"`
	PendingInput  *domain.UserInput    `json:"pendingInput,omitempty"`
	Tasks         []tasklist.Item      `json:"tasks,omitempty"`
}

// encodeState serializes the Agent's quiescent-boundary state into a Session.State payload.
// It marshals the conversation through a pointer so domain.Conversation's MarshalJSON (a
// pointer method) runs — a value field would emit its unexported fields as an empty object.
func (a *Agent) encodeState() (json.RawMessage, error) {
	turns := a.turns.snapshot()
	state, err := json.Marshal(agentState{
		Conversation:  &a.conv,
		TurnIndex:     turns.index,
		InExchange:    turns.inExchange,
		ExchangeStart: turns.exchangeStart,
		PendingInput:  turns.pendingInput,
		Tasks:         a.tasks.Items(),
	})
	if err != nil {
		return nil, fmt.Errorf("apogee: encode session state: %w", err)
	}
	return state, nil
}

// restoreSnapshot version-checks snap and swaps its payload into the Agent's loop state. It is
// the shared core of the two restore paths — Resume (a fresh Agent, via resumeAgent) and
// RestoreSession (a live one): a snapshot newer than this build understands is refused
// (ErrSessionVersion) before any state is touched, then restoreState decodes the payload into a
// temporary and applies it only on a clean decode. So a corrupt or future-version snapshot
// returns an error and leaves the caller's live conversation untouched.
func (a *Agent) restoreSnapshot(snap domain.Session) error {
	if snap.Version > domain.SessionVersion {
		return domain.ErrSessionVersion
	}
	return a.restoreState(snap.State)
}

// restoreState rebuilds the Agent's loop state from a Session.State payload. An empty payload
// RESTORES the zero state (a freshly-snapshotted, never-stepped Agent) rather than returning
// early, so both restore paths end in the same place — see the branch below for why the
// difference is load-bearing on the live one. It decodes into a temporary agentState and
// mutates the Agent only after a clean unmarshal, so a malformed payload returns an error with
// no partial swap — the atomicity restoreSnapshot relies on.
//
// The restored conversation is normalized to zero leading system messages first
// (dropLeadingSystem): per ADR 0023 the configured system prompt is a request projection and no
// COMMITTED message may be RoleSystem, so a well-formed apogee snapshot never carries one and this
// is a no-op on the happy path. A legacy or hand-edited snapshot that does carry one would
// otherwise put two system messages on the wire, because buildRequest unconditionally prepends the
// freshly rendered standing content and the wire seam folds the tool block into the FIRST system
// message only. Enforcing the invariant here — the one seam where outside bytes become history —
// keeps every later reader (the request projection, the next snapshot) clean.
func (a *Agent) restoreState(state json.RawMessage) error {
	st, err := decodeState(state)
	if err != nil {
		return err
	}
	// The task list is restored FIRST and through its own validator, so a snapshot carrying more
	// tasks — or a longer one — than the caps allow is a DECODE ERROR rather than a list silently
	// truncated behind the model's back. Replace leaves the held list untouched when it refuses,
	// so a failure here keeps restoreSnapshot's no-partial-swap promise: nothing else has moved
	// yet. It is also unconditional, which is what CLEARS the list on a restore from a snapshot
	// that carries no tasks (a nil slice empties it) — the reason RestoreSession needs no reset
	// of its own beside the console close it does perform.
	if err := a.tasks.Replace(st.Tasks); err != nil {
		return fmt.Errorf("apogee: decode session state: task list: %w", err)
	}
	exchangeStart := st.ExchangeStart
	if st.Conversation != nil {
		// Dropping messages shifts the rest of the history down, so the cached rollback boundary
		// moves with it — otherwise AbortExchange on a normalized legacy snapshot would roll back
		// one message too far. Clamped at 0: a boundary inside the dropped prefix becomes the
		// start of the conversation.
		if dropped := dropLeadingSystem(st.Conversation); dropped > 0 {
			exchangeStart = max(exchangeStart-dropped, 0)
		}
		a.conv = *st.Conversation
	}
	// The lifecycle is put back whole — and its latches cleared with it: a fold that faulted or
	// saturated against the outgoing history, and the context-fill climb over it, judged a
	// conversation this swap just replaced (turnLifecycle.restore).
	a.turns.restore(turnSnapshot{
		index:         st.TurnIndex,
		inExchange:    st.InExchange,
		exchangeStart: exchangeStart,
		pendingInput:  st.PendingInput,
	})
	// Retained delegations belong to the session this swap replaced, and none crosses into the
	// restored one (ADR 0086 D1): Resume starts from none and a live restore empties the outgoing
	// session's.
	a.retained.clear()
	return nil
}

// decodeState unmarshals a Session.State payload into an agentState without touching any Agent —
// the shared decode of the two readers of the payload, restoreState (which then applies it) and
// CutSession (which rewrites it). An empty payload is not "nothing to decode". On the LIVE restore
// path (RestoreSession) the Agent still holds the OUTGOING session, so treating it as a no-op would
// leave that session's conversation, Turn counters, pending input and task list standing
// underneath the incoming session's file — a half-restore with no error for the caller to see, and
// one the /sessions flow would then redirect saves into. What an empty payload MEANS is a
// never-stepped Agent, so the zero agentState — with an empty conversation — is what it decodes to.
// apogee's own Snapshot never writes one (encodeState always emits a conversation); a hand-edited,
// truncated or foreign session file can, and this is the seam that reads it.
func decodeState(state json.RawMessage) (agentState, error) {
	var st agentState
	if len(state) == 0 {
		st.Conversation = domain.NewConversation(nil)
	} else if err := json.Unmarshal(state, &st); err != nil {
		return agentState{}, fmt.Errorf("apogee: decode session state: %w", err)
	}
	if err := checkRestoredShape(st.Conversation); err != nil {
		return agentState{}, err
	}
	if err := checkRestoredStructure(&st); err != nil {
		return agentState{}, err
	}
	return st, nil
}

// The shape bounds a restored conversation must sit inside. A session file is untrusted input —
// it is bytes on disk that any process with the user's privileges may have rewritten, and what
// decodeState turns it into is COMMITTED history: messages the request projection sends to the
// provider under roles apogee itself never wrote. So the decode seam checks the shape before the
// swap, exactly as internal/session's store checks maxRecordBytes before it reads a record.
//
// Both numbers sit deliberately ABOVE what apogee's own tools can commit, because decodeState is
// also the fork primitive behind CutSession: a threshold a legitimate session could cross would
// make that session unresumable AND unforkable at once, permanently. maxRestoredMessageBytes is
// therefore maxFileReadBytes (internal/tools/tools.go), the ceiling every read tool already
// enforces on the file bodies it commits — a read_file with an explicit end_line over a one-line
// file is byte-uncapped and survives the tool-result clamp whole, so a 10 MiB message is a shape
// apogee itself produces and must keep restoring.
const (
	// maxRestoredMessages bounds the message count of a restored conversation.
	maxRestoredMessages = 4096
	// maxRestoredMessageBytes bounds a single restored message — its content plus the arguments
	// of any tool calls it carries. It is internal/tools' maxFileReadBytes (10 MiB); the two
	// constants live in different packages and are kept equal by the comment above, not by the
	// compiler.
	maxRestoredMessageBytes = 10 * 1024 * 1024
)

// ErrSnapshotRefused is the sentinel every refusal at this seam wraps — the SHAPE refusals
// (checkRestoredShape: a role outside the four domain.Role constants, more messages than
// maxRestoredMessages, a single message past maxRestoredMessageBytes) and the STRUCTURE ones
// (checkRestoredStructure: a payload spelling the engine's own furniture, or an oversized pending
// input). It is returned BEFORE any state is swapped in, so a refused payload leaves the live
// session — its conversation, task list, consoles and usage tally — exactly as it was, on
// --resume, on RestoreSession and on CutSession alike.
var ErrSnapshotRefused = errors.New("apogee: session snapshot refused")

// checkRestoredShape reports whether conv is a shape apogee could have written. Three refusals,
// all of them wrapping ErrSnapshotRefused:
//
//   - a role outside the four domain.Role constants. domain.Message.UnmarshalJSON assigns Role
//     from the wire with no enum check, so a hand-edited payload can name any string at all —
//     and the wire projections pass the roles they are given.
//   - more than maxRestoredMessages messages, or a single message past maxRestoredMessageBytes.
//   - a RoleSystem message past the conversation's LEADING run of them. Per ADR 0023 no committed
//     message may be RoleSystem; the leading run is tolerated and dropped by dropLeadingSystem,
//     because a legacy snapshot carries one there and normalizing it is lossless. A system message
//     further in is neither: the Anthropic wire seam hoists it into the system prompt, so a crafted
//     [user, system, assistant, ...] history would put attacker text where the engine's own
//     standing instructions live. That one is REFUSED rather than stripped — silently editing a
//     session's history is not a repair the human asked for.
func checkRestoredShape(conv *domain.Conversation) error {
	if conv == nil {
		return nil
	}
	if n := conv.Len(); n > maxRestoredMessages {
		return fmt.Errorf(
			"apogee: decode session state: %d messages exceeds the %d-message limit: %w",
			n, maxRestoredMessages, ErrSnapshotRefused,
		)
	}
	var bad error
	leading := true
	conv.Range(func(i int, m domain.Message) bool {
		switch m.Role {
		case domain.RoleSystem:
			if !leading {
				bad = fmt.Errorf(
					"apogee: decode session state: message %d is a system message past the leading run: %w",
					i, ErrSnapshotRefused,
				)
			}
		case domain.RoleUser, domain.RoleAssistant, domain.RoleTool:
			leading = false
		default:
			bad = fmt.Errorf(
				"apogee: decode session state: message %d has role %q, not one of the four roles: %w",
				i, string(m.Role), ErrSnapshotRefused,
			)
		}
		if bad == nil {
			if n := messageBytes(m); n > maxRestoredMessageBytes {
				bad = fmt.Errorf(
					"apogee: decode session state: message %d is %d bytes, over the %d-byte limit: %w",
					i, n, maxRestoredMessageBytes, ErrSnapshotRefused,
				)
			}
		}
		return bad == nil
	})
	return bad
}

// messageBytes is the size checkRestoredShape bounds: the message's content plus the arguments of
// any tool calls it carries, the two fields a payload can make arbitrarily large. The rest of a
// Message is ids, roles and flags.
func messageBytes(m domain.Message) int {
	n := len(m.Content)
	for _, tc := range m.ToolCalls {
		n += len(tc.Arguments)
	}
	return n
}

// CutSession returns a copy of snap with its last dropExchanges Exchanges removed and the loop
// state normalised to an idle boundary — the engine's fork primitive (the session-fork feature
// composes it with a transcript prefix and a fresh record id; ADR 0001's bench fork deep-copies
// the whole Session instead). It is pure over the opaque State: snap is decoded, rewritten and
// re-encoded, and the caller's value is never touched.
//
// The cut counts FROM THE END, never by ordinal from the start: an Exchange's opening is a RoleUser
// message that is not an Interjection (domain.CurrentExchange's rule), and the overflow bridge a
// fold appends is an opening like any other — so after a fold the openings walked backwards match
// the engine's own boundaries exactly, where an ordinal counted from the start would name a
// message the summary folded away. Walking backwards, the last dropExchanges openings are dropped
// together with everything after them, so the history ends at the surviving opening's Exchange
// end; dropExchanges == 0 leaves the message history untouched. Either way the result is
// normalised to a clean boundary: the deferred-correction queue is cleared, no Exchange is open
// (InExchange false, ExchangeStart 0), no input is pending, and the task list is empty — a fork at
// the newest prompt clears the checklist exactly like a fork at an earlier one. The Turn counter
// carries over, so the child's Turn numbering continues the parent's.
//
// A negative dropExchanges, or one that would drop every opening, is refused with an error naming
// both counts; a snapshot newer than this build understands is refused with ErrSessionVersion,
// exactly as Resume refuses it.
func CutSession(snap domain.Session, dropExchanges int) (domain.Session, error) {
	if snap.Version > domain.SessionVersion {
		return domain.Session{}, domain.ErrSessionVersion
	}
	st, err := decodeState(snap.State)
	if err != nil {
		return domain.Session{}, err
	}
	conv := st.Conversation
	if conv == nil {
		conv = domain.NewConversation(nil)
	}
	openings := exchangeOpenings(conv)
	if dropExchanges < 0 || dropExchanges >= len(openings) {
		return domain.Session{}, fmt.Errorf(
			"apogee: cut session: cannot drop %d of %d exchanges", dropExchanges, len(openings),
		)
	}
	if dropExchanges > 0 {
		conv.DropRange(openings[len(openings)-dropExchanges], conv.Len())
	}
	conv.ClearDeferred()
	state, err := json.Marshal(agentState{
		Conversation:  conv,
		TurnIndex:     st.TurnIndex,
		InExchange:    false,
		ExchangeStart: 0,
		PendingInput:  nil,
		Tasks:         nil,
	})
	if err != nil {
		return domain.Session{}, fmt.Errorf("apogee: encode session state: %w", err)
	}
	return domain.Session{Version: domain.SessionVersion, State: state}, nil
}

// exchangeOpenings returns the indices, in order, of every message in conv that opens an Exchange
// — a RoleUser message that is not an Interjection, the same rule domain.CurrentExchange applies
// to find the LAST one.
func exchangeOpenings(conv *domain.Conversation) []int {
	var openings []int
	conv.Range(func(i int, m domain.Message) bool {
		if m.Role == domain.RoleUser && !m.Interjected {
			openings = append(openings, i)
		}
		return true
	})
	return openings
}

// dropLeadingSystem removes conv's leading RoleSystem messages and reports how many it dropped —
// the restore-seam enforcement of ADR 0023's "no system message in committed history" invariant.
// Only the LEADING run is dropped because only the leading run ever reaches here: a RoleSystem
// message further in is REFUSED by checkRestoredShape before the payload is applied at all. This
// once left such a message alone deliberately, on the ground that Conversation.PrefixEnd tolerates
// one in a request; what that reasoning missed is that the payload is untrusted input, and the
// wire seam hoists a mid-history system message into the system prompt. Normalizing the leading
// run is lossless — it is the position buildRequest's seeded message and the wire seam's
// tool-block fold collide with — so it stays a drop, and the rest is a refusal.
func dropLeadingSystem(conv *domain.Conversation) int {
	n := 0
	for n < conv.Len() && conv.At(n).Role == domain.RoleSystem {
		n++
	}
	if n > 0 {
		conv.DropRange(0, n)
	}
	return n
}

// checkRestoredStructure reports whether any content a session file carries spells the engine's
// OWN furniture. It is the second half of the ingestion guard: checkRestoredShape bounds the
// shape a payload may have, this bounds what the content inside that shape may say.
//
// The threat it closes is unattributable injection. Everything it walks reaches the model as text
// apogee itself appears to have written — a committed message body, a task row the standing block
// renders under the engine's own header, a deferred correction the loop injects as an
// unattributed USER message at the role-safe position (domain.Request.InjectContext), and the
// pending input a resumed session submits as the human's own words. A line of any of them opening
// with a context-file header or footer, the delegate report's opening sentence, an advice fence or
// an engine-note fence is a stranger's text dressed as the harness's, and the ratified call
// (apogee-mre) is refusal of the WHOLE payload rather than a silent rewrite: this is a session
// file apogee did not write, not a repo file to be fenced.
//
// The list it checks against (restoredFences, standingblocks.go) deliberately EXCLUDES the task
// list block's opening and the orientation header. Both are committed verbatim by ordinary,
// default-on behaviour — every task_list result renders the first — so refusing them would make
// an ordinary session unresumable and unforkable at once, which is a far larger hole than the one
// it would close.
//
// pendingInput is the one field that is BOUNDED as well as checked: it is not part of the
// conversation, so checkRestoredShape's per-message cap never saw it, yet a restore submits it as
// a message. maxRestoredMessageBytes is therefore the right ceiling — the same one a message it
// is about to become is held to. Its FileRefs are not checked here: decodeState is Agent-less and
// workspace-blind, and an escaping ref is already fenced by security.SafeOpen when readFileRef
// opens it (loop.go).
func checkRestoredStructure(st *agentState) error {
	if st.Conversation != nil {
		var bad error
		st.Conversation.Range(func(i int, m domain.Message) bool {
			if fence, forged := forgesRestoredStructure(m.Content); forged {
				bad = fmt.Errorf(
					"apogee: decode session state: message %d opens a line with %q, which apogee never commits: %w",
					i, fence, ErrSnapshotRefused,
				)
			}
			return bad == nil
		})
		if bad != nil {
			return bad
		}
		if err := checkRestoredDeferred(st.Conversation); err != nil {
			return err
		}
	}
	for i, task := range st.Tasks {
		if fence, forged := forgesRestoredStructure(task.Text); forged {
			return fmt.Errorf(
				"apogee: decode session state: task %d opens a line with %q, which apogee never commits: %w",
				i, fence, ErrSnapshotRefused,
			)
		}
	}
	if st.PendingInput != nil {
		if n := len(st.PendingInput.Text); n > maxRestoredMessageBytes {
			return fmt.Errorf(
				"apogee: decode session state: pending input is %d bytes, over the %d-byte limit: %w",
				n, maxRestoredMessageBytes, ErrSnapshotRefused,
			)
		}
		if fence, forged := forgesRestoredStructure(st.PendingInput.Text); forged {
			return fmt.Errorf(
				"apogee: decode session state: pending input opens a line with %q, which apogee never commits: %w",
				fence, ErrSnapshotRefused,
			)
		}
	}
	return nil
}

// checkRestoredDeferred checks the conversation's queued deferred corrections — the strings
// conversationJSON round-trips so a deferring Reaction's injection survives a snapshot boundary,
// and which the loop then hands to Request.InjectContext as an unattributed user message. The
// queue has no read-only accessor, so the check drains it and puts it back in FIFO order; conv is
// decodeState's own temporary at this point, so nothing else can observe the round trip, and a
// refusal discards the whole temporary anyway.
func checkRestoredDeferred(conv *domain.Conversation) error {
	injects, ok := conv.TakeDeferred()
	if !ok {
		return nil
	}
	defer func() {
		for _, inject := range injects {
			conv.Defer(inject)
		}
	}()
	for i, inject := range injects {
		if fence, forged := forgesRestoredStructure(inject); forged {
			return fmt.Errorf(
				"apogee: decode session state: deferred correction %d opens a line with %q, which apogee never commits: %w",
				i, fence, ErrSnapshotRefused,
			)
		}
	}
	return nil
}
