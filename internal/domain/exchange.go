package domain

// The Exchange as a derived domain working value (ADR 0017 §1). The current
// Exchange — one user input through to the final no-tool response (CONTEXT.md)
// — is never cached: its opening is the index of the last RoleUser message that
// OPENS an Exchange, the Exchange the messages strictly after it. The boundary is
// stable across request-scoped injections because InjectContext places injections
// before that same opening or in the system message, never after it.
//
// One user message is deliberately NOT an opening: an Interjection — the human's
// remark committed INTO the running Exchange at a between-Steps boundary — carries
// Message.Interjected, and the derivation skips it. That is what keeps the boundary
// stable now that a user message CAN land mid-Exchange: the opening is the last
// non-interjected RoleUser message, so an interjection joins the current Exchange's
// body instead of starting a new one. InjectContext honours the same skip — it anchors
// its insert on the opening, so a request-scoped injection lands ahead of an
// interjection rather than above it, where it would become the newest opening.

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

// lastMatchIndex returns the index of the last message in c satisfying match, or
// -1. It is THE backward-scan core (ADR 0017), with exactly two callers:
// lastExchangeOpening (the Exchange boundary — CurrentExchange and InjectContext,
// which both anchor on the opening) and lastRoleIndex (the plain role scan —
// lastIndex and conversationView.LastUser). So the domain scans history backwards
// in exactly one place.
func lastMatchIndex(c messageReader, match func(Message) bool) int {
	for i := c.Len() - 1; i >= 0; i-- {
		if match(c.At(i)) {
			return i
		}
	}
	return -1
}

// lastRoleIndex returns the index of the last message with role in c, or -1.
func lastRoleIndex(c messageReader, role Role) int {
	return lastMatchIndex(c, func(m Message) bool { return m.Role == role })
}

// lastExchangeOpening returns the index of the last message in c that OPENS an
// Exchange — a RoleUser message that is not an Interjection — or -1. It is the
// one place the interjection skip lives: everything reading the Exchange boundary
// (CurrentExchange and, through it, the Mechanisms) or inserting relative to it
// (Request.InjectContext) routes here, so a mid-Exchange user message moves no
// boundary and no injection is ever placed above one. lastRoleIndex stays the plain
// role scan: the hooks' LastUser deliberately still reports the most recent user
// message, interjected or not, because a Mechanism asking "what did the human last
// say" wants exactly that.
func lastExchangeOpening(c messageReader) int {
	return lastMatchIndex(c, func(m Message) bool { return m.Role == RoleUser && !m.Interjected })
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
// RoleUser message that is not an Interjection, the Exchange the messages strictly
// after it. With no such user message present there is no current Exchange (Found
// reports false).
func CurrentExchange(c messageReader) ExchangeView {
	return ExchangeView{src: c, userIndex: lastExchangeOpening(c)}
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
