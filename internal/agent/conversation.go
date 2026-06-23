package agent

import (
	"encoding/json"
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
)

// The engine's conversation storage is domain.Conversation (P1.2 adopts it, replacing
// the throwaway P0.6 message/conversation pair). It already carries everything the full
// Turn/Step machine needs — role-tagged messages with tool calls + tool-call IDs (the
// multi-Turn tool loop), the FIFO deferred-action queue (the ActionDefer feed-forward),
// and a JSON round-trip — so the loop appends to it and Snapshot serializes it directly.
// P1.6 owns the Session envelope around this payload (loop counters such as turnIndex,
// the documented v1 schema, preserved per-message Extra wire fields); the conversation
// value itself is already library-complete here.

// encodeConversation serializes the conversation into a Session.State payload. It marshals
// through the pointer so domain.Conversation's MarshalJSON (a pointer method) runs — a
// value would marshal its unexported fields to an empty object.
func encodeConversation(c *domain.Conversation) (json.RawMessage, error) {
	state, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("apogee: encode conversation: %w", err)
	}
	return state, nil
}

// decodeConversation rebuilds a conversation from a Session.State payload. An empty
// payload yields an empty conversation (a freshly-snapshotted, never-stepped Agent).
func decodeConversation(state json.RawMessage) (domain.Conversation, error) {
	var c domain.Conversation
	if len(state) == 0 {
		return c, nil
	}
	if err := json.Unmarshal(state, &c); err != nil {
		return domain.Conversation{}, fmt.Errorf("apogee: decode conversation: %w", err)
	}
	return c, nil
}
