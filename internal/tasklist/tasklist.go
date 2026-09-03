package tasklist

import (
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"
)

// MaxItems is how many tasks one list may hold. The cap is a context budget, not a
// storage limit: the whole list is re-rendered into the standing system content on
// every request, so a list that grows without bound quietly spends the window the work
// itself needs. Forty rows is far past any checklist a single run decomposes into, and
// a model that wants a forty-first is being told to finish or drop something instead.
const MaxItems = 40

// MaxTextChars is the longest one task's text may be, counted in runes so a
// non-ASCII checklist is measured the way a reader sees it. A task is a label, not a
// paragraph: the plan for HOW to do the work belongs in the reply, and a row long
// enough to hold one would wrap the block into unreadability.
const MaxTextChars = 200

// Fence is the literal opening of the rendered block's header, and the string every
// other layer recognises the block by: the engine lists it among the openings it forges
// (so a workspace file cannot forge one), and a test asserting the list reached the wire
// looks for it. [HeaderFormat] is built FROM it rather than repeating it, so the fence
// and the header can never drift apart.
const Fence = "Task list — "

// HeaderFormat is the block's first line, taking the open count and the done count in
// that order. It states the ownership rule the whole design rests on — the list is the
// model's, and one call carries the COMPLETE list — because the standing block is the
// only place a model reliably re-reads it after compaction.
const HeaderFormat = Fence + "yours to maintain; call task_list with the COMPLETE list to update it (%d open, %d done):"

// doneMarker and openMarker are the row prefixes. They are the checkbox glyphs the
// user-question pane already draws (internal/tui's askCheckedMarker), so a model and a
// human meet the same two shapes throughout the program, and they are the same width as
// each other, so the texts line up without padding.
const (
	doneMarker = "[✔] "
	openMarker = "[ ] "
)

// Item is one task: the text the model wrote and whether it has finished it. The json
// tags are the session-state encoding — `done` is omitted when false, so the common row
// (an open task) costs one key in a snapshot.
type Item struct {
	Text string `json:"text"`
	Done bool   `json:"done,omitempty"`
}

// List is one engine's task list: the model's complete checklist, held as session state
// so it survives compaction and a resume (ADR 0072).
//
// It is guarded by a mutex because delegations fan out concurrently (ADR 0039) and a
// list is reached through the call context rather than held by the tool, so two calls
// can arrive at one list at once. Its zero value is a usable empty list, which is what
// lets an [Item]-less engine render nothing without a construction step.
type List struct {
	mu    sync.Mutex
	items []Item
}

// New returns an empty list, ready to use. It is the constructor the engine builds
// through beside undo.New and console.New; the zero value works just as well, so nothing
// here needs a nil check of its own.
func New() *List {
	return &List{}
}

// Items returns a copy of the held tasks, in order — nil when the list is empty. The
// copy is what makes this safe to hand to a snapshot writer: the caller can marshal,
// sort or mutate the result without reaching back into the list another call may be
// replacing at the same moment.
func (l *List) Items() []Item {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.items) == 0 {
		return nil
	}

	copied := make([]Item, len(l.items))
	copy(copied, l.items)
	return copied
}

// Replace swaps the whole list for items — the only mutation this package offers. A
// whole-list replace is the ratified shape (ADR 0072): there are no item ids, so the
// model never has to carry an identifier across a compaction to amend a row it already
// wrote.
//
// Each text is trimmed and empty ones are dropped, so a model that pads its array with
// blanks gets a clean list rather than blank rows. The result is refused — with a
// descriptive error naming the limit it broke — when it holds more than [MaxItems]
// tasks or when any text runs past [MaxTextChars]. On refusal the held list is
// UNCHANGED: a rejected call leaves the model the list it already had, rather than a
// half-applied one it would have to reconstruct.
//
// Replacing with an empty or nil slice is how the list is cleared, which is also how a
// snapshot carrying no tasks restores.
func (l *List) Replace(items []Item) error {
	cleaned := make([]Item, 0, len(items))
	for index, item := range items {
		text := strings.TrimSpace(item.Text)
		if text == "" {
			continue
		}
		if length := utf8.RuneCountInString(text); length > MaxTextChars {
			return fmt.Errorf(
				"task %d is %d characters long; a task may be at most %d",
				index+1,
				length,
				MaxTextChars,
			)
		}
		cleaned = append(cleaned, Item{Text: text, Done: item.Done})
	}

	if len(cleaned) > MaxItems {
		return fmt.Errorf(
			"the task list holds at most %d tasks; that call carried %d",
			MaxItems,
			len(cleaned),
		)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(cleaned) == 0 {
		l.items = nil
		return nil
	}
	l.items = cleaned
	return nil
}

// Render returns the block the model reads: the [HeaderFormat] line with the open and
// done counts, then one row per task marked done or open, joined by newlines. An empty
// list renders the empty string, never a header saying there is nothing — the block
// rides along on standing content that already exists, and a list nobody has written is
// not worth a line of every request.
func (l *List) Render() string {
	items := l.Items()
	if len(items) == 0 {
		return ""
	}

	open, done := 0, 0
	for _, item := range items {
		if item.Done {
			done++
			continue
		}
		open++
	}

	var block strings.Builder
	fmt.Fprintf(&block, HeaderFormat, open, done)
	for _, item := range items {
		block.WriteString("\n")
		if item.Done {
			block.WriteString(doneMarker)
		} else {
			block.WriteString(openMarker)
		}
		block.WriteString(item.Text)
	}
	return block.String()
}
