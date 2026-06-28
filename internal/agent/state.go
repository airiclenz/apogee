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
//     rather than wiping unrelated history.
//   - pendingInput : input Submitted but not yet consumed by a Step, so a Submit→Snapshot→
//     Resume sequence does not silently drop the queued message.
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
		TurnIndex:     a.turnIndex,
		InExchange:    a.inExchange,
		ExchangeStart: a.exchangeStart,
		PendingInput:  a.pendingInput,
	})
	if err != nil {
		return nil, fmt.Errorf("apogee: encode session state: %w", err)
	}
	return state, nil
}

// restoreState rebuilds the Agent's loop state from a Session.State payload. An empty payload
// leaves the zero state (a freshly-snapshotted, never-stepped Agent).
func (a *Agent) restoreState(state json.RawMessage) error {
	if len(state) == 0 {
		return nil
	}
	var st agentState
	if err := json.Unmarshal(state, &st); err != nil {
		return fmt.Errorf("apogee: decode session state: %w", err)
	}
	if st.Conversation != nil {
		a.conv = *st.Conversation
	}
	a.turnIndex = st.TurnIndex
	a.inExchange = st.InExchange
	a.exchangeStart = st.ExchangeStart
	a.pendingInput = st.PendingInput
	return nil
}
