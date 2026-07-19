package domain

// The Exchange as a derived domain working value (ADR 0017 §1). The current
// Exchange — one user input through to the final no-tool response (CONTEXT.md)
// — is never cached: its opening is the index of the last RoleUser message,
// the Exchange the messages strictly after it. The boundary is stable across
// request-scoped injections because InjectContext places injections before
// that message or in the system message, never after it.

// messageReader is the minimal read surface the Exchange derivation needs.
// It is deliberately unexported (ADR 0017: no root export until an external
// consumer exists) and satisfied by both *Conversation (the engine's committed
// history) and conversationView (the hooks' request view).
type messageReader interface {
	Len() int
	At(i int) Message
}

// Compile-time pins: the two conversation read surfaces satisfy messageReader.
var (
	_ messageReader = (*Conversation)(nil)
	_ messageReader = conversationView{}
)

// messageSlice adapts a raw []Message to messageReader so the slice-based
// helpers (lastIndex) share the single derivation core below.
type messageSlice []Message

func (s messageSlice) Len() int { return len(s) }

func (s messageSlice) At(i int) Message { return s[i] }

// lastRoleIndex returns the index of the last message with role in c, or -1.
// It is THE boundary-derivation core (ADR 0017): CurrentExchange, lastIndex —
// and through it InjectContext and conversationView.LastUser — all route here,
// so the Exchange boundary has exactly one implementation in the domain.
func lastRoleIndex(c messageReader, role Role) int {
	for i := c.Len() - 1; i >= 0; i-- {
		if c.At(i).Role == role {
			return i
		}
	}
	return -1
}

// ExchangeView is the current Exchange derived from a conversation read
// surface: the opening user message and the messages strictly after it. It is
// a working value — construct it where needed with CurrentExchange and let it
// go; it holds the backing reader, so derive it again after a mutation rather
// than keeping one across edits.
type ExchangeView struct {
	src       messageReader
	userIndex int
}

// CurrentExchange derives the current Exchange from c: the opening is the last
// RoleUser message, the Exchange the messages strictly after it. With no user
// message present there is no current Exchange (Found reports false).
func CurrentExchange(c messageReader) ExchangeView {
	return ExchangeView{src: c, userIndex: lastRoleIndex(c, RoleUser)}
}

// Found reports whether an opening user message exists — without one there is
// no current Exchange.
func (e ExchangeView) Found() bool { return e.userIndex >= 0 }

// UserIndex returns the index of the opening user message, or -1 when no user
// message exists.
func (e ExchangeView) UserIndex() int { return e.userIndex }

// After returns copies of the messages strictly after the opening user message
// — the current Exchange's body. It returns nil when no user message exists or
// nothing follows the opening.
func (e ExchangeView) After() []Message {
	if !e.Found() || e.userIndex+1 >= e.src.Len() {
		return nil
	}
	out := make([]Message, 0, e.src.Len()-e.userIndex-1)
	for i := e.userIndex + 1; i < e.src.Len(); i++ {
		out = append(out, e.src.At(i))
	}
	return out
}

// RangeAfter walks the messages strictly after the opening user message
// without allocating, calling fn with each message's index in the backing
// view, until fn returns false. It is a no-op when no user message exists.
func (e ExchangeView) RangeAfter(fn func(i int, m Message) bool) {
	if !e.Found() {
		return
	}
	for i := e.userIndex + 1; i < e.src.Len(); i++ {
		if !fn(i, e.src.At(i)) {
			return
		}
	}
}
