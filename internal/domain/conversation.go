package domain

import "encoding/json"

// ----------------------------------------------------------------------------
// Conversation — the history-rewrite hook's working value
// ----------------------------------------------------------------------------

// Conversation is the serializable conversation state a history-rewrite hook edits.
// It is a cleanly copyable value with no live handles (ADR 0001) — what lets the
// bench fork by deep-copying it and the user resume from a snapshot. Summaries are
// not a separate structure: they are ordinary messages produced by generative
// Compaction (context/) and written back via Replace. A deferred Response Action
// (an Outcome.Defer) is held here (Defer / TakeDeferred) so it survives a snapshot/resume
// boundary.
//
// MarshalJSON / UnmarshalJSON keep the type opaque while persisting it; the v1 wire
// schema persists the message list (with per-message Extra preservation, P1.6) and the
// pending deferred corrections. The engine wraps this payload in its session-state
// envelope (internal/agent/state.go), which adds the loop counters.
type Conversation struct {
	messages []Message
	deferred []string // pending deferred injections, FIFO
	// revision is bumped by each mutator — the acted-fire probe (R4), read via
	// Revision. Runtime-only: it is deliberately NOT serialized (it carries no
	// history, only "did a reaction just mutate me").
	revision int
}

// NewConversation builds a Conversation over a copy of messages (engine seam).
func NewConversation(messages []Message) *Conversation {
	return &Conversation{messages: append([]Message(nil), messages...)}
}

// Len reports the number of messages.
func (c *Conversation) Len() int { return len(c.messages) }

// At returns the message at index i (panics on an out-of-range index, like a slice).
func (c *Conversation) At(i int) Message { return c.messages[i] }

// Range iterates messages until fn returns false.
func (c *Conversation) Range(fn func(i int, m Message) bool) {
	for i := range c.messages {
		if !fn(i, c.messages[i]) {
			return
		}
	}
}

// Messages returns a copy of the message list (engine seam — the loop projects it
// onto the provider wire shape).
func (c *Conversation) Messages() []Message { return append([]Message(nil), c.messages...) }

// PrefixEnd is the index past the leading system messages and the first user message
// — the protected prefix a truncation must keep.
func (c *Conversation) PrefixEnd() int {
	i := 0
	for i < len(c.messages) && c.messages[i].Role == RoleSystem {
		i++
	}
	if i < len(c.messages) && c.messages[i].Role == RoleUser {
		i++
	}
	return i
}

// AssistantBoundaries are the indices of assistant messages — the only safe cut
// points, because a tool result must stay adjacent to the assistant call that
// produced it (strict chat templates).
func (c *Conversation) AssistantBoundaries() []int {
	var b []int
	for i := range c.messages {
		if c.messages[i].Role == RoleAssistant {
			b = append(b, i)
		}
	}
	return b
}

// SetMessageContent edits one message's content in place by index. An out-of-range
// index is a no-op.
//
// Rewriting the content invalidates any advice spans the message carried — the prune
// replaces an old tool result with a stub through this method — so a ledger whose fence the
// new content no longer opens at its offset is dropped rather than left pointing at other bytes.
func (c *Conversation) SetMessageContent(i int, content string) {
	if i < 0 || i >= len(c.messages) {
		return
	}
	c.messages[i].Content = content
	c.messages[i].dropStaleAdvice()
	c.revision++
}

// HasEngineNote reports whether some message still carries an engine note on topic. For a caller
// that lands a note only while none stands (the step- and token-budget notices, internal/agent)
// it IS the latch: there is no separate flag to clear, so a fold, a prune stub or a rollback that
// takes the note away leaves the next result to be noted again. A row counts only while its fence
// still stands at its offset (Message.fenceStands): SetMessageContent already clears a ledger whose
// fence a rewrite lost (dropStaleAdvice), and the same check here keeps a message committed with a
// stale row from reading as noted.
func (c *Conversation) HasEngineNote(topic string) bool {
	for i := range c.messages {
		m := &c.messages[i]
		for _, span := range m.Advice {
			if span.Topic == topic && m.fenceStands(span) {
				return true
			}
		}
	}
	return false
}

// DropRange drops messages in [start, end) — history truncation drops the middle,
// keeping the prefix and a recent tail. Bounds are clamped; an empty range is a no-op.
func (c *Conversation) DropRange(start, end int) {
	if start < 0 {
		start = 0
	}
	if end > len(c.messages) {
		end = len(c.messages)
	}
	if start >= end {
		return
	}
	c.messages = append(c.messages[:start:start], c.messages[end:]...)
	c.revision++
}

// Insert places a message at index i — e.g. a static gap note at a truncation cut.
// i is clamped to [0, Len].
func (c *Conversation) Insert(i int, m Message) {
	c.messages = insertMessage(c.messages, i, m)
	c.revision++
}

// Append adds m to the end of the history — the engine's per-Turn commit of a user,
// assistant, or tool-result message, and the natural primitive a history-rewrite hook
// uses to grow the conversation (a summary, a gap note). It is Insert at Len with a
// name that reads at the call site.
func (c *Conversation) Append(m Message) {
	c.messages = append(c.messages, m)
	c.revision++
}

// Replace swaps the entire message list — generative Compaction writes its
// summarised history back through here. The slice is copied.
func (c *Conversation) Replace(msgs []Message) {
	c.messages = append([]Message(nil), msgs...)
	c.revision++
}

// Defer records a deferred correction (the Inject payload of a deferring Outcome)
// to be injected, role-safe, into the next request. It is held
// in conversation state so it survives a snapshot/resume boundary — the streaming
// feed-forward path (design §4.1).
func (c *Conversation) Defer(inject string) {
	c.deferred = append(c.deferred, inject)
	c.revision++
}

// Revision reports how many mutations have been applied to the Conversation — the
// loop's acted-fire probe (R4, engine seam): hookrun snapshots it around each
// catalogued history-rewrite fire and books the fire only when the counter moved. A
// hook never needs it, and it does not survive a snapshot round-trip.
func (c *Conversation) Revision() int { return c.revision }

// TakeDeferred removes and returns the pending deferred corrections in FIFO order —
// the loop drains them when building the next request and InjectContexts each. ok is
// false when none are pending.
func (c *Conversation) TakeDeferred() (injects []string, ok bool) {
	if len(c.deferred) == 0 {
		return nil, false
	}
	out := c.deferred
	c.deferred = nil
	return out, true
}

// DeferredLen reports how many deferred corrections are currently queued — the loop reads it
// after draining a request to capture the floor a cancelled Turn's own deferrals are truncated
// back to (TruncateDeferred), so a re-attempt restores only the drained injections (F6).
func (c *Conversation) DeferredLen() int { return len(c.deferred) }

// TruncateDeferred drops every deferred correction past the first n, keeping the queue's first n
// entries. n is clamped to [0, len]. The loop calls it when rolling a cancelled Turn back: the
// deferrals the cancelled Turn's own post-response hooks queued die with the Turn, so restoreDeferred
// re-queues the drained injections exactly once rather than atop a contradictory re-derivation (F6).
func (c *Conversation) TruncateDeferred(n int) {
	if n < 0 {
		n = 0
	}
	if n >= len(c.deferred) {
		return
	}
	c.deferred = c.deferred[:n:n]
	c.revision++
}

// ClearDeferred discards all pending deferred corrections. A Deferred Response Action is a decision
// about the NEXT request of the SAME conversation flow, so the loop clears the queue whenever an
// Exchange ends — internal/agent's closeExchange, which runs on the end() dispositions that close
// the Exchange (endExchangeDone, endAbandoned) and from AbortExchange: a stale fan-out directive
// must never survive a fault or abort into the next Exchange (F6).
func (c *Conversation) ClearDeferred() {
	if len(c.deferred) == 0 {
		return
	}
	c.deferred = nil
	c.revision++
}

// conversationJSON is the on-disk shape of a Conversation. Per-message Extra fields
// round-trip through Message's own MarshalJSON/UnmarshalJSON (the unknown wire siblings
// are flattened alongside the known fields), so reasoning_content and the like survive.
type conversationJSON struct {
	Messages []Message `json:"messages"`
	Deferred []string  `json:"deferred,omitempty"`
}

// MarshalJSON serializes the Conversation (messages + pending deferred corrections). Each
// message goes through Message.MarshalJSON, so the advice strip applies here too: a record
// written from a Conversation carries every message's pre-advice content and no span.
func (c *Conversation) MarshalJSON() ([]byte, error) {
	return json.Marshal(conversationJSON{Messages: c.messages, Deferred: c.deferred})
}

// UnmarshalJSON restores a Conversation from its serialized form.
func (c *Conversation) UnmarshalJSON(data []byte) error {
	var j conversationJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	c.messages = j.Messages
	c.deferred = j.Deferred
	return nil
}
