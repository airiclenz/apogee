package apogee

import (
	"encoding/json"
	"fmt"
)

// sessionVersion is the schema version Snapshot stamps and Resume/DecodeSession
// accept. P0.6 ships v1; a snapshot whose Version exceeds this is from a newer build
// and is rejected with ErrSessionVersion (ADR 0001 — no silent forward migration).
const sessionVersion = 1

// message is the loop's internal, serializable conversation message — the throwaway
// Phase-0 stand-in for the concrete Session schema (TDD §5 Sessions row, Phase 1).
// It is unexported and minimal on purpose: snapshot/resume must round-trip it, and a
// plain role+content pair is all the single non-streaming Turn needs.
type message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// conversation is the copyable conversation state the loop appends to and that
// Snapshot serializes. It holds no live handles (ADR 0001), so a JSON round-trip is a
// faithful copy — the property snapshot/resume and the bench's fork both rely on.
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
