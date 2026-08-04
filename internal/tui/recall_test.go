package tui

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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
