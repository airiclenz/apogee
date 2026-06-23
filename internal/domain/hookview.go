package domain

// Concrete read-only views backing Request.View / Response.View / the tool-stage
// hooks' LoopView argument. They are unexported: a hook receives them only through the
// LoopView / ConversationView interfaces (docs/design/hook-mutation-api.md §2.1). Each
// holds the loop's backing slices by reference but exposes only copies and value
// snapshots, so a hook reading through a view can never mutate loop state — mutation
// is always by index against the owning Request / Conversation.

// loopView is the read-only window onto loop state every hook gets.
type loopView struct {
	messages []Message
	tools    []ToolDef
	budget   Budget
	turn     int
	fired    map[MechanismID]int
}

func (v loopView) Conversation() ConversationView { return conversationView{messages: v.messages} }

func (v loopView) Tools() []ToolDef { return append([]ToolDef(nil), v.tools...) }

func (v loopView) Budget() Budget { return v.budget }

func (v loopView) Turn() int { return v.turn }

func (v loopView) Fired(id MechanismID) int { return v.fired[id] }

// conversationView is the read-only history with tool-call/result pairing helpers.
type conversationView struct {
	messages []Message
}

func (v conversationView) Len() int { return len(v.messages) }

func (v conversationView) At(i int) Message { return v.messages[i] }

func (v conversationView) Range(fn func(i int, m Message) bool) {
	for i := range v.messages {
		if !fn(i, v.messages[i]) {
			return
		}
	}
}

func (v conversationView) LastUser() (Message, int, bool) {
	if i := lastIndex(v.messages, RoleUser); i >= 0 {
		return v.messages[i], i, true
	}
	return Message{}, -1, false
}

func (v conversationView) CallByID(id string) (ToolCall, int, bool) {
	for i := range v.messages {
		for _, call := range v.messages[i].ToolCalls {
			if call.ID == id {
				return call, i, true
			}
		}
	}
	return ToolCall{}, -1, false
}

func (v conversationView) ResultFor(callID string) (Message, int, bool) {
	for i := range v.messages {
		if v.messages[i].Role == RoleTool && v.messages[i].ToolCallID == callID {
			return v.messages[i], i, true
		}
	}
	return Message{}, -1, false
}
