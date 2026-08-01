package agent

import (
	"encoding/json"
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
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
//     per-message Extra wire fields) plus the pending ActionDefer queue, serialized by
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
//
// The per-message Interjected marker rides the conversation's own marshal as an omitempty
// sibling, so it needs NO SessionVersion bump in either direction: a snapshot written before
// the marker existed simply lacks the key (decoding false — no message was ever interjected),
// and an older binary reading a newer snapshot preserves it as an unknown wire field and
// writes it back untouched (domain.Message's Extra passthrough).
//
// The live delegates (Approver, Confiner, EventSink), the resolved tool/Mechanism
// registries, and the allow-for-session approval cache are deliberately NOT serialized:
// Resume re-supplies the delegates and registries afresh (ADR 0001), and a resumed Session
// re-confirms allow-for-session grants rather than silently carrying a prior process's write
// authorizations — the safer default for a human-in-the-loop gate. This is v1; a later schema
// that needs the cache adds it under a new SessionVersion.
type agentState struct {
	Conversation  *domain.Conversation `json:"conversation"`
	TurnIndex     int                  `json:"turnIndex"`
	InExchange    bool                 `json:"inExchange,omitempty"`
	ExchangeStart int                  `json:"exchangeStart,omitempty"`
	PendingInput  *domain.UserInput    `json:"pendingInput,omitempty"`
}

// encodeState serializes the Agent's quiescent-boundary state into a Session.State payload.
// It marshals the conversation through a pointer so domain.Conversation's MarshalJSON (a
// pointer method) runs — a value field would emit its unexported fields as an empty object.
func (a *Agent) encodeState() (json.RawMessage, error) {
	state, err := json.Marshal(agentState{
		Conversation:  &a.conv,
		TurnIndex:     a.turns.index,
		InExchange:    a.turns.inExchange,
		ExchangeStart: a.exchangeBoundary(),
		PendingInput:  a.pendingInput,
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
// leaves the zero state (a freshly-snapshotted, never-stepped Agent). It decodes into a
// temporary agentState and mutates the Agent only after a clean unmarshal, so a malformed
// payload returns an error with no partial swap — the atomicity restoreSnapshot relies on.
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
	if len(state) == 0 {
		return nil
	}
	var st agentState
	if err := json.Unmarshal(state, &st); err != nil {
		return fmt.Errorf("apogee: decode session state: %w", err)
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
	a.turns.index = st.TurnIndex
	a.turns.inExchange = st.InExchange
	a.turns.exchangeStart = exchangeStart
	a.pendingInput = st.PendingInput
	return nil
}

// dropLeadingSystem removes conv's leading RoleSystem messages and reports how many it dropped —
// the restore-seam enforcement of ADR 0023's "no system message in committed history" invariant.
// Only the LEADING run is dropped: that is the position buildRequest's seeded message and the wire
// seam's tool-block fold collide with, and it leaves a hook-authored mid-history system message
// (which Conversation.PrefixEnd deliberately still tolerates in a request) alone.
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
