package agent

import (
	"encoding/json"
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
)

// message is the loop's internal, serializable conversation message — the throwaway
// Phase-0 stand-in for the concrete Session schema (TDD §5 Sessions row, Phase 1).
// It is unexported and minimal on purpose: snapshot/resume must round-trip it, and a
// plain role+content pair is all the single non-streaming Turn needs. P1.6 replaces
// it with the real serialized state (full messages, deferred actions, loop counters).
type message struct {
	Role    domain.Role `json:"role"`
	Content string      `json:"content"`
}

// conversation is the copyable conversation state the loop appends to and that
// Snapshot serializes into the Session's opaque State payload. It holds no live
// handles (ADR 0001), so a JSON round-trip is a faithful copy — the property
// snapshot/resume and the bench's fork both rely on. The engine owns this payload
// schema (it serializes engine state); domain owns only the Session envelope (ADR 0010).
type conversation struct {
	Messages []message `json:"messages"`
}

// append adds m to the conversation history.
func (c *conversation) append(m message) {
	c.Messages = append(c.Messages, m)
}

// encodeConversation serializes the conversation into a Session.State payload.
func encodeConversation(c conversation) (json.RawMessage, error) {
	state, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("apogee: encode conversation: %w", err)
	}
	return state, nil
}

// decodeConversation rebuilds a conversation from a Session.State payload. An empty
// payload yields an empty conversation (a freshly-snapshotted, never-stepped Agent).
func decodeConversation(state json.RawMessage) (conversation, error) {
	var c conversation
	if len(state) == 0 {
		return c, nil
	}
	if err := json.Unmarshal(state, &c); err != nil {
		return conversation{}, fmt.Errorf("apogee: decode conversation: %w", err)
	}
	return c, nil
}
