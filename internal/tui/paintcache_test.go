package tui

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/scheme"
)

// ----------------------------------------------------------------------------
// The block-paint cache (paintcache.go)
// ----------------------------------------------------------------------------

// warm returns a transcript rendered through a live cache, and cold the same one rendered with no
// cache at all. Every test below is the same shape — do something to the transcript, then check
// that what the warm cache hands back is byte-identical to what a cold render produces — so the
// oracle is stated once. cold RENDERS rather than only building: a cache-less transcript is what
// every other test in this package already exercises, so it is the known-good paint.
func warmed(tr *transcript) *transcript {
	tr.paints = newPaintCache()
	return tr
}

// sameRender fails when two renders differ in a single line or a single target mark. The two
// slices are compared element by element rather than with reflect.DeepEqual so a failure names the
// line that moved.
func sameRender(t *testing.T, what string, got, want renderedTranscript) {
	t.Helper()
	if len(got.lines) != len(want.lines) {
		t.Fatalf("%s: %d lines through the cache, %d cold", what, len(got.lines), len(want.lines))
	}
	for i := range want.lines {
		if got.lines[i] != want.lines[i] {
			t.Errorf("%s: line %d = %q through the cache; want %q", what, i, strip(got.lines[i]), strip(want.lines[i]))
		}
	}
	if len(got.targets) != len(want.targets) {
		t.Fatalf("%s: %d targets through the cache, %d cold", what, len(got.targets), len(want.targets))
	}
	for i := range want.targets {
		if got.targets[i] != want.targets[i] {
			t.Errorf("%s: target %d = %+v through the cache; want %+v", what, i, got.targets[i], want.targets[i])
		}
	}
}

// coldRender paints the same entries with no cache behind them — the oracle every warm render is
// checked against. It borrows the entries slice rather than copying it: the renderer only reads.
func coldRender(tr *transcript, th theme, width int, blink bool) renderedTranscript {
	cold := &transcript{
		entries:    tr.entries,
		pending:    tr.pending,
		streaming:  tr.streaming,
		pendingRun: tr.pendingRun,
		ws:         tr.ws,
	}
	return cold.renderView(th, width, blink)
}

// A second render of an untouched transcript is served entirely from the cache and is identical to
// the first — the base case the whole memo rests on. The start-up box is deliberately in the
// fixture: it is the one kind that may never be cached (refreshStartup rewrites it in place), so
// its presence pins that "all hits" means "every block the cache is allowed to hold".
func TestPaintCacheServesAnUnchangedTranscript(t *testing.T) {
	th := newTheme(scheme.Default())
	tr := warmed(feed(
		domain.MessageEvent{Text: "the first answer, long enough to wrap somewhere in here"},
		domain.ToolCallEvent{Call: domain.ToolCall{ID: "1", Tool: "read_file", Arguments: json.RawMessage(`{"path":"a.go"}`)}},
		domain.ToolResultEvent{Result: domain.ToolResult{CallID: "1", Content: "ok"}},
		domain.MessageEvent{Text: "the second answer"},
	))
	tr.addStartup(startupView{Host: "localhost", Model: "test", Version: "0.0.0"})

	first := tr.renderView(th, 80, false)
	sameRender(t, "first render", first, coldRender(tr, th, 80, false))

	before := tr.paints.misses
	second := tr.renderView(th, 80, false)
	sameRender(t, "second render", second, first)

	// One miss and one only: the start-up box, which is never cacheable.
	if got := tr.paints.misses - before; got != 1 {
		t.Errorf("second render took %d misses; want 1 (the uncacheable start-up box)", got)
	}
	if tr.paints.hits == 0 {
		t.Error("second render served no block from the cache")
	}
}

// The key's inputs each force a fresh paint that matches a cold render: a toggled block, a new
// width, and a blink flip over a LIVE block. This is the hit/miss case in both directions — the
// changed block misses, and what it paints is still right.
func TestPaintCacheRepaintsWhenTheKeyMoves(t *testing.T) {
	th := newTheme(scheme.Default())
	tr := warmed(feed(
		domain.MessageEvent{Text: "an answer"},
		domain.ToolCallEvent{Call: domain.ToolCall{ID: "1", Tool: "read_file", Arguments: json.RawMessage(`{"path":"a.go"}`)}},
		domain.ToolResultEvent{Result: domain.ToolResult{CallID: "1", Content: "line one\nline two\nline three\nline four\nline five"}},
	))
	tr.renderView(th, 80, false) // warm

	// The tool block, expanded: a different `expanded` flag, so a different key.
	if !tr.toggleExpanded(1) {
		t.Fatal("entries[1] is not a toggleable block — fixture is wrong")
	}
	sameRender(t, "after a toggle", tr.renderView(th, 80, false), coldRender(tr, th, 80, false))

	// A narrower window re-wraps everything.
	sameRender(t, "after a width change", tr.renderView(th, 40, false), coldRender(tr, th, 40, false))

	// A live call, and the star's two phases. The blink phase reaches only a live block's header,
	// so the two phases must differ here and be right at both.
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "2", Tool: "shell", Arguments: json.RawMessage(`{"command":"ls"}`)}})
	settled := tr.renderView(th, 80, false)
	sameRender(t, "live block, settled phase", settled, coldRender(tr, th, 80, false))
	blinked := tr.renderView(th, 80, true)
	sameRender(t, "live block, blink phase", blinked, coldRender(tr, th, 80, true))
	if equalLines(settled.lines, blinked.lines) {
		t.Error("the blink phase painted a live block identically — the fixture holds no open call")
	}

	// A delegate's context reading, which the collapsed run states on its summary line. It is the key
	// field with no proxy: the reading appends no entry, extends no span and flips no flag, so a key
	// that did not name it would answer with the previous figure — or, on the first reading, with a
	// line that has no fill cell at all.
	subAgentCall(tr, "s1", "survey the tests", 0)
	readCall(tr, "r1", "suite_test.go", 1, 90, 1)
	before := tr.renderView(th, 80, false) // warm the run's block, with no reading on it yet
	subAgentUsage(tr, 1, 12000, 32768)
	first := tr.renderView(th, 80, false)
	sameRender(t, "after the delegate's first reading", first, coldRender(tr, th, 80, false))
	if equalLines(before.lines, first.lines) {
		t.Error("the first context reading changed no line — the fixture's run paints no fill cell")
	}
	subAgentUsage(tr, 1, 18000, 32768)
	second := tr.renderView(th, 80, false)
	sameRender(t, "after the reading moved", second, coldRender(tr, th, 80, false))
	if equalLines(first.lines, second.lines) {
		t.Error("a moved reading served the previous figure — paintKey does not name it")
	}
}

// A grouped run is ONE cached paint over MANY entries, and each of those entries owns a state the
// paint depends on — so the key has to cover every member's expanded bit, not just the head's. It
// does, through spanFlags over the whole span (blockKey), and this is the test that says so: opening
// the LAST member of a run is the case a head-only key would serve stale, since nothing else about
// the block moved.
func TestPaintCacheCoversEveryGroupMemberState(t *testing.T) {
	th := newTheme(scheme.Default())
	tr := warmed(&transcript{})
	for i, c := range [][2]string{
		{"go build ./...", "ok\nbuilt"},
		{"go vet ./...", "clean\nno findings"},
		{"go test ./...", "ok\nPASS"},
	} {
		id := fmt.Sprintf("c%d", i+1)
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: id, Tool: "terminal",
			Arguments: []byte(`{"command":"` + c[0] + `"}`)}})
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: id, Content: c[1]}})
	}
	collapsed := tr.renderView(th, 80, false) // warm

	for member := range 3 {
		if !tr.setExpanded(member, true) {
			t.Fatalf("entries[%d] is not a toggleable block — fixture is wrong", member)
		}
		opened := tr.renderView(th, 80, false)
		sameRender(t, fmt.Sprintf("member %d open", member), opened, coldRender(tr, th, 80, false))
		if equalLines(opened.lines, collapsed.lines) {
			t.Errorf("opening member %d served the collapsed paint; the key does not cover its state", member)
		}
		if !tr.setExpanded(member, false) {
			t.Fatalf("entries[%d] would not close again", member)
		}
	}
	sameRender(t, "closed again", tr.renderView(th, 80, false), collapsed)
}

// A session switch re-uses head indices for a different conversation's entries, and reset is what
// keeps the cache from answering about the old one. Without the clear in transcript.reset this
// test paints the FIRST session's message at index 1.
func TestPaintCacheDoesNotSurviveAReset(t *testing.T) {
	th := newTheme(scheme.Default())
	tr := warmed(&transcript{})
	tr.addStartup(startupView{Host: "localhost", Model: "test"})
	tr.addUser("the first session's prompt", nil)
	tr.renderView(th, 80, false) // warm

	tr.reset()
	tr.addStartup(startupView{Host: "localhost", Model: "test"})
	tr.addUser("a different session entirely", nil)
	sameRender(t, "after a session switch", tr.renderView(th, 80, false), coldRender(tr, th, 80, false))
}

// The equivalence matrix — the guard the whole cache rests on. A memo is only as good as its key,
// and a key that misses an input is not a slow render but a WRONG one: last frame's block still on
// screen under this frame's state. So one transcript is walked through every mutation a live session
// performs — a token appended to the tail, a call joining a folded run, a result landing on an entry
// behind the tail, a block toggled open and shut, a resize, the star's phase over live work, a
// sub-agent run opening and reporting, and the start-up box restated in place — and after EVERY one
// of those steps the warm render is compared byte for byte, line and target mark, against a cold
// render of the same entries. The cold render is the oracle because it is what every other test in
// this package already asserts against.
//
// The script is deliberately CUMULATIVE rather than eight independent fixtures: each mutation lands
// on a cache that the previous ones have filled, which is the only way a stale row can be observed
// at all. Several steps assert mid-way as well, at a state the loop would otherwise skip past.
func TestPaintCacheMatchesAColdRenderThroughEveryMutation(t *testing.T) {
	th := newTheme(scheme.Default())
	tr := warmed(&transcript{})
	tr.addStartup(startupView{Host: "localhost", Model: "connecting…", Version: "0.0.0"})
	tr.addUser("read the two files, then survey the tests", nil)

	// The frame's own two parameters travel with the script: a step may move either, and every check
	// after it renders warm and cold at whatever they now are.
	width, blink := 80, false
	check := func(what string) {
		t.Helper()
		sameRender(t, what, tr.renderView(th, width, blink), coldRender(tr, th, width, blink))
	}
	check("the opening scrollback") // also the cold fill: from here the cache holds every block

	steps := []struct {
		name   string
		mutate func()
	}{{
		name: "a token appended to the streaming tail",
		mutate: func() {
			tr.apply(domain.TokenEvent{Text: "Let me read "})
			check("one token into the stream")
			tr.apply(domain.TokenEvent{Text: "both of them."})
		},
	}, {
		// The first call commits the narration as an entry AND opens the run; the second joins it,
		// so the block at that head covers two entries where it covered one.
		name: "a new call extending a folded run",
		mutate: func() {
			readCall(tr, "c1", "a.go", 1, 40, 0)
			check("the folded run's only member")
			readCall(tr, "c2", "b.go", 1, 12, 0)
		},
	}, {
		// Two calls open at once and the OLDER one lands first: an entry well behind the tail flips
		// done, under a block whose head is a different entry again. Nothing notifies the cache —
		// the flag simply has to be in the key, across the whole span and not just at the head.
		name: "a result arriving for an older entry",
		mutate: func() {
			tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c3", Tool: "read_file", Arguments: json.RawMessage(`{"path":"c.go"}`)}})
			tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c4", Tool: "read_file", Arguments: json.RawMessage(`{"path":"d.go"}`)}})
			check("two calls open in one run")
			tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
				CallID:  "c3",
				Content: "[File: c.go, 3 lines total, showing lines 1-3]\n…",
				Summary: domain.ReadSpan{Start: 1, End: 3, Total: 3},
			}})
		},
	}, {
		// BOTH per-entry view states, on the one entry, because both are in the paint key and each is
		// a level of the same block: the Terminal call opens a super-group's TYPE ROW — the reads
		// before it carry a different label, so the two runs fold under one umbrella — and the member
		// behind that row then opens its own body. A Terminal call keeps a body, so its two states
		// really do paint
		// differently — asserted at each step, because a toggle over something that hides nothing
		// would make this row vacuous.
		name: "a type row and a block expanded and collapsed again",
		mutate: func() {
			runCall(tr, "c5", "go test ./...", "ok   internal/tui\nok   internal/agent\nok   internal/run\nok   internal/schedule\nok   internal/domain\nPASS", 0)
			head := len(tr.entries) - 1
			shut := tr.renderView(th, width, blink)
			if !tr.toggleTypeExpanded(head) {
				t.Fatalf("entries[%d] heads no run — the script's fixture is wrong", head)
			}
			listed := tr.renderView(th, width, blink)
			check("the Run's type row opened")
			if equalLines(shut.lines, listed.lines) {
				t.Error("the type row painted identically open and shut — it lists nothing")
			}
			if !tr.toggleExpanded(head) {
				t.Fatalf("entries[%d] is not a toggleable block — the script's fixture is wrong", head)
			}
			expanded := tr.renderView(th, width, blink)
			check("the Run expanded")
			if equalLines(listed.lines, expanded.lines) {
				t.Error("the Run painted identically open and shut — its output hides nothing")
			}
			tr.toggleExpanded(head)
			tr.toggleTypeExpanded(head)
		},
	}, {
		name:   "a width change",
		mutate: func() { width = 46 },
	}, {
		// c4 never got its result, so the folded run above still holds an open call and its star is
		// the one thing in the paint that follows the phase.
		name: "a blink flip with a live block",
		mutate: func() {
			settled := tr.renderView(th, width, false)
			blink = true
			check("the star's hollow phase")
			if equalLines(settled.lines, tr.renderView(th, width, true).lines) {
				t.Error("the two star phases painted identically — the script holds no open call")
			}
			blink = false
		},
	}, {
		// The head changes PAINTER here without changing kind, depth or flags: a sub-agent call with
		// nothing nested under it yet is an ordinary tool block, and becomes a run's head the moment
		// its first child entry lands. Only paintKey.shape tells those two apart.
		name: "a sub-agent span opening and reporting",
		mutate: func() {
			subAgentCall(tr, "s1", "survey the tests", 0)
			check("a sub-agent call with no span yet")
			readCall(tr, "r1", "suite_test.go", 1, 90, 1)
			check("the run's first nested entry")
			runCall(tr, "r2", "go test -run Suite", "--- FAIL: TestSuite\n    suite_test.go:12: gap\nFAIL", 1)
			check("a second nested block, and the cascading count with it")
			subAgentReport(tr, "s1", "Found 4 gaps\nin the suite", 0)
		},
	}, {
		// A reading from a delegate that is still working: no entry appended, no span extended, no
		// flag flipped — the whole of the movement is two numbers on a head the cache painted twice
		// already, and they are painted (subAgentFill). The run is opened here rather than reusing the
		// one above because that one has reported, and a reported run's figure is history.
		name: "a context reading landing on an open run",
		mutate: func() {
			subAgentCall(tr, "s2", "check the docs", 0)
			readCall(tr, "d1", "README.md", 1, 30, 1)
			quiet := tr.renderView(th, width, blink)
			check("the second run, before its delegate has reported")
			subAgentUsage(tr, 1, 12000, 32768)
			first := tr.renderView(th, width, blink)
			check("the delegate's first reading")
			if equalLines(quiet.lines, first.lines) {
				t.Error("the first reading changed no line — the script's run paints no fill cell")
			}
			subAgentUsage(tr, 1, 18000, 32768)
			if equalLines(first.lines, tr.renderView(th, width, blink).lines) {
				t.Error("a moved reading served the previous figure — paintKey does not name it")
			}
		},
	}, {
		// The one mutation that rewrites an entry's facts in place while touching no flag the key
		// reads. It is why entryStartup is never cached at all — a warm render that served the box
		// from a row would still be saying "connecting…" here, and cold would not.
		name: "the start-up box restated in place",
		mutate: func() {
			tr.refreshStartup(startupView{Host: "localhost", Model: "qwen3.6-27b", Context: "32k", Version: "0.0.0"})
		},
	}}

	for _, s := range steps {
		s.mutate()
		check(s.name)
	}
}

// The reuse property — the reason the cache exists. A long settled scrollback with a reply streaming
// underneath it: one more token must cost the TAIL and nothing else, which is exactly the shape of
// the 100%-GPU report this item answers (ISSUES.md). The miss counter is the instrument, because the
// output alone cannot tell a reused paint from a re-computed identical one.
//
// The streaming tail is not a cached block at all (renderView paints t.pending straight, and it
// changes on every token), so the number a token append must not move is the count of paints
// performed over the COMMITTED entries — and that number is zero.
func TestPaintCacheRepaintsOnlyTheStreamingTail(t *testing.T) {
	th := newTheme(scheme.Default())
	tr := warmed(&transcript{})
	const exchanges = 25 // 25 prompts + 25 answers = 50 settled blocks
	for i := range exchanges {
		tr.addUser(fmt.Sprintf("question %d — what does the fold do here?", i), nil)
		tr.apply(domain.MessageEvent{Text: fmt.Sprintf(
			"Answer %d. The fold turns each `domain.Event` into a transcript entry, and the\n"+
				"renderer turns entries into lines — long enough that re-parsing it is real work.", i)})
	}
	tr.apply(domain.TokenEvent{Text: "the next reply is still "})
	const blocks = 2 * exchanges

	if got := tr.renderView(th, 80, false); len(got.lines) == 0 {
		t.Fatal("the fixture rendered nothing")
	}
	if got := tr.paints.misses; got != blocks {
		t.Fatalf("the cold fill painted %d blocks; want %d (every committed entry, the tail uncached)", got, blocks)
	}

	// One more token, and a repaint. Only the tail moved.
	hits, misses := tr.paints.hits, tr.paints.misses
	tr.apply(domain.TokenEvent{Text: "coming in"})
	appended := tr.renderView(th, 80, false)
	if got := tr.paints.misses - misses; got != 0 {
		t.Errorf("appending one token repainted %d settled blocks; want 0 — only the streaming tail moved", got)
	}
	if got := tr.paints.hits - hits; got != blocks {
		t.Errorf("the repaint served %d blocks from the cache; want all %d", got, blocks)
	}
	sameRender(t, "after one more token", appended, coldRender(tr, th, 80, false))

	// And a second render of the very same state is all hits: the reuse is a steady state, not a
	// one-off that the first repaint's own store happened to satisfy.
	hits, misses = tr.paints.hits, tr.paints.misses
	second := tr.renderView(th, 80, false)
	if got := tr.paints.misses - misses; got != 0 {
		t.Errorf("an identical second render took %d misses; want 0", got)
	}
	if got := tr.paints.hits - hits; got != blocks {
		t.Errorf("an identical second render served %d blocks from the cache; want all %d", got, blocks)
	}
	sameRender(t, "the identical second render", second, appended)
}

// BenchmarkRenderViewStreaming is the evidence line for the item's perf claim: what ONE repaint of a
// long scrollback costs while a reply streams, with the cache behind it and without. The streamed
// token is what puts this render on a hot loop — the sink delivers a batch, refreshViewport repaints,
// and before the cache every settled message up the scrollback was re-parsed as markdown, re-styled
// and re-wrapped for a paint identical to the one it had a frame earlier (precedent for the shape:
// BenchmarkRenderTable, mdtable_test.go).
//
// The tail is held FIXED across iterations rather than grown a token at a time: a growing buffer
// would make late iterations measure a longer transcript instead of the same repaint, and the cost
// under test is the per-frame one, which the pending tail's own length does not change.
func BenchmarkRenderViewStreaming(b *testing.B) {
	th := newTheme(scheme.Default())
	build := func() *transcript {
		tr := &transcript{}
		for i := range 25 {
			tr.addUser(fmt.Sprintf("question %d — what does the fold do here?", i), nil)
			tr.apply(domain.MessageEvent{Text: fmt.Sprintf(
				"Answer %d. The fold turns each `domain.Event` into a transcript entry, and the\n"+
					"renderer turns entries into lines — long enough that re-parsing it is real work.", i)})
		}
		tr.apply(domain.TokenEvent{Text: "the next reply is still coming in"})
		return tr
	}
	repaint := func(b *testing.B, tr *transcript) {
		b.ReportAllocs()
		for b.Loop() {
			if got := tr.renderView(th, 100, false); len(got.lines) == 0 {
				b.Fatal("rendered nothing")
			}
		}
	}

	// Cold: no cache at all — the paint every repaint performed before this item.
	b.Run("cold", func(b *testing.B) { repaint(b, build()) })
	// Warm: the cache filled once outside the timer, so the loop measures the steady state a
	// streaming session actually sits in.
	b.Run("warm", func(b *testing.B) {
		tr := warmed(build())
		tr.renderView(th, 100, false)
		repaint(b, tr)
	})
}

// equalLines is the plain slice comparison the blink assertion needs (the house has no shared one).
func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
