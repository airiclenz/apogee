package tui

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Prompt recall — the start-up load and the record seam (recall.go)
// ----------------------------------------------------------------------------

// fakeRecallHost scripts the prompt-recall seam: canned entries for the start-up load, an optional
// error for either method, and a record of everything appended. The mutex is there because both
// methods are called off the Update loop on Cmd goroutines in the real program.
type fakeRecallHost struct {
	mu        sync.Mutex
	entries   []string // what LoadPrompts hands back
	appended  []string // every AppendPrompt asked for, in order
	loads     int
	loadErr   error
	appendErr error
}

// fakeRecallHost satisfies the recall seam the Model drives.
var _ RecallHost = (*fakeRecallHost)(nil)

func (h *fakeRecallHost) LoadPrompts() ([]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.loads++
	if h.loadErr != nil {
		return nil, h.loadErr
	}
	return h.entries, nil
}

func (h *fakeRecallHost) AppendPrompt(text string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.appended = append(h.appended, text)
	return h.appendErr
}

func (h *fakeRecallHost) recorded() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.appended...)
}

func (h *fakeRecallHost) loadCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.loads
}

// recallOpts are the display options with a recall host wired.
func recallOpts(host RecallHost) Options {
	opts := testOpts
	opts.Recall = host
	return opts
}

// firstRecall runs Init's Cmd and returns the recallLoadedMsg it yields. Init batches the recall
// read with the first beat, so the batch arm is kept even though an unwired monitor collapses the
// batch to the single read: a Cmd batched on later must not silently stop the load from landing.
func firstRecall(t *testing.T, cmd tea.Cmd) recallLoadedMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("Init returned no Cmd — the recall load never went out")
	}
	switch msg := cmd().(type) {
	case recallLoadedMsg:
		return msg
	case tea.BatchMsg:
		out := make(chan tea.Msg, len(msg))
		for _, c := range msg {
			go func() { out <- c() }()
		}
		deadline := time.After(5 * time.Second)
		for range msg {
			select {
			case landed := <-out:
				if loaded, ok := landed.(recallLoadedMsg); ok {
					return loaded
				}
			case <-deadline:
				t.Fatal("no recallLoadedMsg five seconds after Init — the load never landed")
			}
		}
		t.Fatal("Init's batch carried no recallLoadedMsg — the load never went out")
	default:
		t.Fatalf("Init's Cmd yielded %T, want the recall load", msg)
	}
	return recallLoadedMsg{}
}

// An unwired host is the whole of the degrade: Init issues no read at all and the box's recall
// state stays empty, which is exactly the pre-recall behaviour every hand-built Options relies on.
func TestRecallUnwiredLoadsNothing(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)
	if cmd := m.loadRecallCmd(); cmd != nil {
		t.Error("an unwired recall host issued a load Cmd; nothing can answer it")
	}
	assertNoBatch(t, m.Init())
	if len(m.recall.entries) != 0 {
		t.Errorf("recall entries = %v on an unwired host; want none", m.recall.entries)
	}
	// A stray load Msg must still be inert rather than panic the fold.
	after := step(t, m, recallLoadedMsg{})
	if len(after.recall.entries) != 0 {
		t.Errorf("a stray empty load left entries %v; want none", after.recall.entries)
	}
}

// The start-up load: Init reads the host once, and the entries it returns land in the prompt
// editor's recall state — oldest→newest, exactly as the store handed them over — once the Msg is
// folded through Update.
func TestRecallStartupLoadLandsInEditor(t *testing.T) {
	t.Parallel()

	host := &fakeRecallHost{entries: []string{"oldest", "middle", "newest"}}
	m := newTestModelEng(t, &fakeEngine{}, recallOpts(host))

	loaded := firstRecall(t, m.Init())
	if got := host.loadCount(); got != 1 {
		t.Errorf("host loaded %d times from Init, want exactly 1", got)
	}

	after := step(t, m, loaded)
	want := []string{"oldest", "middle", "newest"}
	if !reflect.DeepEqual(after.recall.entries, want) {
		t.Errorf("recall entries = %v; want %v in the store's own order", after.recall.entries, want)
	}
}

// A failed load is not a failed session: the entries stay empty, no note is printed in front of the
// human's first prompt, and the box still sends.
func TestRecallLoadErrorLeavesTUIFunctional(t *testing.T) {
	t.Parallel()

	host := &fakeRecallHost{entries: []string{"unreachable"}, loadErr: errors.New("boom")}
	m := newTestModelEng(t, &fakeEngine{}, recallOpts(host))

	after := step(t, m, firstRecall(t, m.Init()))
	if len(after.recall.entries) != 0 {
		t.Errorf("a failed load left entries %v; want none", after.recall.entries)
	}
	before := len(after.transcript.entries)

	after = step(t, after, keyRune('h'))
	after, cmd := stepCmd(t, after, keyEnter())
	if cmd == nil || after.state != stateRunning {
		t.Fatalf("state = %v with cmd %v after a failed recall load; the message must send anyway", after.state, cmd)
	}
	if v := after.input.Value(); v != "" {
		t.Errorf("input not cleared after the send: %q", v)
	}
	if len(after.transcript.entries) <= before {
		t.Error("the sent message never reached the transcript after a failed recall load")
	}
}

// Recording is fire-and-forget: the Cmd hands the text to the host and reports nothing, and a host
// that refuses the write is swallowed — the message has already been sent, so a failed record costs
// one ↑ entry and nothing else.
func TestRecallAppendIsFireAndForget(t *testing.T) {
	t.Parallel()

	host := &fakeRecallHost{appendErr: errors.New("disk full")}
	m := newTestModelEng(t, &fakeEngine{}, recallOpts(host))

	cmd := m.appendRecallCmd("remember me")
	if cmd == nil {
		t.Fatal("a wired host produced no append Cmd")
	}
	if msg := cmd(); msg != nil {
		t.Errorf("the append Cmd reported %v; a fire-and-forget write must report nothing", msg)
	}
	if got, want := host.recorded(), []string{"remember me"}; !reflect.DeepEqual(got, want) {
		t.Errorf("host recorded %v; want %v", got, want)
	}

	if cmd := m.appendRecallCmd(""); cmd != nil {
		t.Error("empty text scheduled an append; there is nothing to record")
	}
	unwired := newTestModel(t)
	if cmd := unwired.appendRecallCmd("x"); cmd != nil {
		t.Error("an unwired host scheduled an append; nothing can take the write")
	}
}

// ----------------------------------------------------------------------------
// The walk — what ↑/↓ do in the prompt box
// ----------------------------------------------------------------------------

// recallModel is the box as start-up leaves it in a workspace that has sent before: the seam wired,
// the entries loaded (oldest→newest, as given), idle. Its engine is scripted to complete any drive
// at once (heldModel's posture), so a send's batched Cmds — the record among them — can be drained.
func recallModel(t *testing.T, entries ...string) (Model, *fakeRecallHost) {
	t.Helper()
	host := &fakeRecallHost{entries: entries}
	m := newTestModelEng(t, &fakeEngine{stepFn: scriptedSteps()}, recallOpts(host))
	m = step(t, m, firstRecall(t, m.Init()))
	if len(m.recall.entries) != len(entries) {
		t.Fatalf("precondition: %d entries loaded, want %d", len(m.recall.entries), len(entries))
	}
	return m, host
}

// assertBox states the two things every walk assertion is about at once: what the human sees in the
// box, and whether the arrows still belong to recall.
func assertBox(t *testing.T, m Model, want string, active bool) {
	t.Helper()
	if got := m.input.Value(); got != want {
		t.Errorf("box = %q, want %q", got, want)
	}
	if m.recall.active != active {
		t.Errorf("recall active = %v, want %v", m.recall.active, active)
	}
}

// ↑ on an empty box opens the walk at the NEWEST entry with the caret at its end, and further ↑
// steps older one entry at a time, stopping at the oldest rather than wrapping.
func TestRecallUpWalksOlderAndClampsAtTheOldest(t *testing.T) {
	t.Parallel()

	m, _ := recallModel(t, "oldest", "middle", "newest")

	m = step(t, m, keyUp())
	assertBox(t, m, "newest", true)
	if got, want := m.caretByteOffset(), len("newest"); got != want {
		t.Errorf("caret at byte %d after the recall, want %d — the end of what was loaded", got, want)
	}

	m = step(t, m, keyUp())
	assertBox(t, m, "middle", true)
	m = step(t, m, keyUp())
	assertBox(t, m, "oldest", true)
	m = step(t, m, keyUp())
	assertBox(t, m, "oldest", true) // clamped: the bottom of the walk is not a wrap-around
}

// ↓ steps newer, and one step past the newest empties the box and hands the arrows back — the way
// out of a walk without deleting the recalled line by hand.
func TestRecallDownWalksNewerAndEmptiesPastTheNewest(t *testing.T) {
	t.Parallel()

	m, _ := recallModel(t, "oldest", "middle", "newest")
	for range 3 {
		m = step(t, m, keyUp())
	}
	assertBox(t, m, "oldest", true)

	m = step(t, m, keyDown())
	assertBox(t, m, "middle", true)
	m = step(t, m, keyDown())
	assertBox(t, m, "newest", true)
	m = step(t, m, keyDown())
	assertBox(t, m, "", false) // past the newest: back to the blank box the walk started from
	m = step(t, m, keyDown())
	assertBox(t, m, "", false) // and ↓ is the caret's again, with nothing to recall from an empty box
}

// Any other key ends the mode: the recalled line becomes an ordinary draft and the arrows go back to
// cursor duty inside it (the multi-line entry is what makes that visible).
func TestRecallEndsOnAnyOtherKey(t *testing.T) {
	t.Parallel()

	m, _ := recallModel(t, "first line\nsecond line")
	m = step(t, m, keyUp())
	assertBox(t, m, "first line\nsecond line", true)

	m = step(t, m, keyRune('x'))
	assertBox(t, m, "first line\nsecond linex", false)

	m = step(t, m, keyUp())
	assertBox(t, m, "first line\nsecond linex", false) // the text stands: ↑ edited nothing
	if got := m.input.Line(); got != 0 {
		t.Errorf("caret on logical line %d after ↑ on an ended walk, want 0 — the arrow must move the cursor", got)
	}
}

// ↑ on a box the human has typed into is the caret's, not recall's: a draft is never replaced by a
// recorded line.
func TestRecallLeavesATypedDraftAlone(t *testing.T) {
	t.Parallel()

	m, _ := recallModel(t, "recorded")
	m = step(t, m, keyRune('h'))
	m = step(t, m, keyRune('i'))

	m = step(t, m, keyUp())
	assertBox(t, m, "hi", false)
}

// A recalled "/command" must not pop the autocomplete overlay: the menu claims ↑/↓ before recall
// ever sees them, so opening one would have the walk stolen by its own first entry. The contrast
// pins that this is recall's suppression and not something about the text.
func TestRecallCommandOpensNoAutocomplete(t *testing.T) {
	t.Parallel()

	m, _ := recallModel(t, "/clear")
	m = step(t, m, keyUp())
	assertBox(t, m, "/clear", true)
	if m.autocomplete.active {
		t.Error("a recalled /command popped the autocomplete overlay; it would steal ↑/↓ from the walk")
	}

	typed, _ := recallModel(t, "/clear")
	typed = typeInput(t, typed, "/clear")
	if !typed.autocomplete.active {
		t.Fatal("typing the same line opened no overlay either; the assertion above proves nothing")
	}
}

// ⏎ on a recalled entry sends it exactly as a typed line: the box empties, the Exchange opens, and
// the send is recorded — which the store's consecutive dedup then absorbs, so the walk does not grow
// a duplicate of the line it just handed back.
func TestRecallEnterSubmitsTheRecalledLine(t *testing.T) {
	t.Parallel()

	m, host := recallModel(t, "run the tests")
	m = step(t, m, keyUp())

	m, cmd := stepCmd(t, m, keyEnter())
	if m.state != stateRunning {
		t.Fatalf("state = %v after ⏎ on a recalled line, want running", m.state)
	}
	if v := m.input.Value(); v != "" {
		t.Errorf("box = %q after the send, want empty", v)
	}
	drainCmd(t, m, cmd)

	if got, want := host.recorded(), []string{"run the tests"}; !reflect.DeepEqual(got, want) {
		t.Errorf("host recorded %v; want %v — a re-sent recall is recorded again", got, want)
	}
	if got, want := m.recall.entries, []string{"run the tests"}; !reflect.DeepEqual(got, want) {
		t.Errorf("entries = %v; want %v — the in-memory view dedups as the store does", got, want)
	}
}

// Every send path records: a message, a whole-input /command, and an interjection staged while a
// worker runs. The ask answer is the one that must not (decision 3) — its own test below.
func TestRecallRecordsEverySendPath(t *testing.T) {
	t.Parallel()

	m, host := recallModel(t)
	m.input.SetValue("hello there")
	m, cmd := stepCmd(t, m, keyEnter())
	drainCmd(t, m, cmd)
	if m.state != stateRunning {
		t.Fatalf("precondition: state = %v after the send, want running", m.state)
	}

	m.input.SetValue("and one more thing")
	m, cmd = stepCmd(t, m, keyEnter())
	drainCmd(t, m, cmd)
	if len(m.pendingInterjections) != 1 {
		t.Fatalf("precondition: %d rows staged, want 1", len(m.pendingInterjections))
	}

	want := []string{"hello there", "and one more thing"}
	if got := host.recorded(); !reflect.DeepEqual(got, want) {
		t.Errorf("host recorded %v; want %v (a message, then the interjection)", got, want)
	}
	if got := m.recall.entries; !reflect.DeepEqual(got, want) {
		t.Errorf("entries = %v; want %v — both are recallable without a reload", got, want)
	}

	// The whole-input /command line is an input the human sent too. /version is the reporting verb
	// that runs on the spot, so nothing else about the state has to move for this to be about the record.
	cmdModel, cmdHost := recallModel(t)
	cmdModel.input.SetValue("/version")
	cmdModel, run := stepCmd(t, cmdModel, keyEnter())
	drainCmd(t, cmdModel, run)
	if got, want := cmdHost.recorded(), []string{"/version"}; !reflect.DeepEqual(got, want) {
		t.Errorf("host recorded %v after a /command; want %v", got, want)
	}
}

// The session-reset pair is the carve-out: /clear and /new are sent like any other command line and
// recorded like none of them, because a walk that hands one back arms ⏎ with the one action nothing
// undoes. Neither half of the record moves — not the in-memory list the walk reads, not the disk
// append — and the recallable line sent before each proves the store is otherwise live.
func TestRecallNeverRecordsTheSessionResetPair(t *testing.T) {
	t.Parallel()

	for _, verb := range []string{"/clear", "/new"} {
		t.Run(verb, func(t *testing.T) {
			t.Parallel()

			// /version first, as the recorded-command control: it reports on the spot and leaves the
			// model idle, which is where the reset pair is reachable at all (both are idle-only).
			m, host := recallModel(t)
			m.input.SetValue("/version")
			m, cmd := stepCmd(t, m, keyEnter())
			drainCmd(t, m, cmd)
			if m.state != stateIdle {
				t.Fatalf("precondition: state = %v after /version, want idle", m.state)
			}

			m.input.SetValue(verb)
			m, cmd = stepCmd(t, m, keyEnter())
			drainCmd(t, m, cmd)

			want := []string{"/version"}
			if got := host.recorded(); !reflect.DeepEqual(got, want) {
				t.Errorf("host recorded %v after %s; want %v — the reset verb is never appended", got, verb, want)
			}
			if got := m.recall.entries; !reflect.DeepEqual(got, want) {
				t.Errorf("entries = %v after %s; want %v — nor does it enter the walk in memory", got, verb, want)
			}
		})
	}
}

// The record is in memory the moment the send is: ↑ right after sending hands the line back without
// the store being read a second time (and it does so while the worker runs, where recall is live too).
func TestRecallSentPromptIsRecallableWithoutAReload(t *testing.T) {
	t.Parallel()

	m, host := recallModel(t)
	m.input.SetValue("remember this")
	m, cmd := stepCmd(t, m, keyEnter())
	drainCmd(t, m, cmd)

	m = step(t, m, keyUp())
	assertBox(t, m, "remember this", true)
	if got := host.loadCount(); got != 1 {
		t.Errorf("host loaded %d times; the walk must never re-read the file", got)
	}
}

// An ask answer is not a prompt: it is recorded nowhere, and the question's ↑/↓ keep moving the
// choice highlight rather than opening a walk over the box the answer borrowed.
func TestRecallSkipsAskAnswers(t *testing.T) {
	t.Parallel()

	m, host := recallModel(t, "earlier")
	reply := make(chan domain.AskAnswer, 1)
	m = step(t, m, askReqMsg{
		Request: domain.AskRequest{Question: "which way?", Choices: []string{"left", "right"}},
		Reply:   reply,
	})
	if m.state != stateAwaitingAsk {
		t.Fatalf("precondition: state = %v, want awaitingAsk", m.state)
	}

	m = step(t, m, keyDown())
	assertBox(t, m, "", false) // the arrows are the choice highlight's here, not the walk's
	if m.askSel != 1 {
		t.Errorf("askSel = %d after ↓ at the ask, want 1 — recall must not have taken the key", m.askSel)
	}
	m = step(t, m, keyUp())
	if m.askSel != 0 {
		t.Errorf("askSel = %d after ↑ at the ask, want 0", m.askSel)
	}
	assertBox(t, m, "", false)

	m, cmd := stepCmd(t, m, keyEnter())
	drainCmd(t, m, cmd)
	if got := host.recorded(); len(got) != 0 {
		t.Errorf("the ask answer was recorded as a prompt: %v", got)
	}
	if got, want := m.recall.entries, []string{"earlier"}; !reflect.DeepEqual(got, want) {
		t.Errorf("entries = %v after answering; want %v unchanged", got, want)
	}
}

// The non-key actions end the mode too — a paste and a click IN the box are both acting in it, so
// the recalled line becomes an ordinary draft and the arrows go back to the caret.
func TestRecallEndsOnPasteAndOnAClickInTheBox(t *testing.T) {
	t.Parallel()

	pasted, _ := recallModel(t, "recorded line")
	pasted = step(t, pasted, keyUp())
	pasted = step(t, pasted, tea.PasteMsg{Content: "!"})
	assertBox(t, pasted, "recorded line!", false)

	clicked, _ := recallModel(t, "recorded line")
	clicked = step(t, clicked, keyUp())
	_, y0, _, _ := clicked.inputContentRect()
	clicked = step(t, clicked, leftClick(2, y0))
	assertBox(t, clicked, "recorded line", false)
}
