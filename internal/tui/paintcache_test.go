package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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
		root:       tr.root, // the oracle paints the same VIEW, not just the same entries
		// the umbrella's fold inputs: the same preference and threshold; a small umbrella's own
		// fold rides its head entry, which the borrowed entries already carry
		toolsOpen:     tr.toolsOpen,
		toolsFoldOver: tr.toolsFoldOver,
	}
	return cold.renderView(th, width, blink, breadcrumbHint)
}

// A second render of an untouched transcript is served entirely from the cache and is identical to
// the first — the base case the whole memo rests on. The start-up box is deliberately in the
// fixture: it is the one kind that may never be cached (refreshStartup rewrites it in place), so
// its presence pins that "all hits" means "every block the cache is allowed to hold".
func TestPaintCacheServesAnUnchangedTranscript(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	tr := warmed(feed(
		domain.MessageEvent{Text: "the first answer, long enough to wrap somewhere in here"},
		domain.ToolCallEvent{Call: domain.ToolCall{ID: "1", Tool: "read_file", Arguments: json.RawMessage(`{"path":"a.go"}`)}},
		domain.ToolResultEvent{Result: domain.ToolResult{CallID: "1", Content: "ok"}},
		domain.MessageEvent{Text: "the second answer"},
	))
	tr.addStartup(startupView{Host: "localhost", Model: "test", Version: "0.0.0"})

	first := tr.renderView(th, 80, false, breadcrumbHint)
	sameRender(t, "first render", first, coldRender(tr, th, 80, false))

	before := tr.paints.misses
	second := tr.renderView(th, 80, false, breadcrumbHint)
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
	t.Parallel()

	th := newTheme(scheme.Default())
	tr := warmed(feed(
		domain.MessageEvent{Text: "an answer"},
		domain.ToolCallEvent{Call: domain.ToolCall{ID: "1", Tool: "read_file", Arguments: json.RawMessage(`{"path":"a.go"}`)}},
		domain.ToolResultEvent{Result: domain.ToolResult{CallID: "1", Content: "line one\nline two\nline three\nline four\nline five"}},
	))
	tr.renderView(th, 80, false, breadcrumbHint) // warm

	// The tool block, expanded: a different `expanded` flag, so a different key.
	if !tr.toggleExpanded(1) {
		t.Fatal("entries[1] is not a toggleable block — fixture is wrong")
	}
	sameRender(t, "after a toggle", tr.renderView(th, 80, false, breadcrumbHint), coldRender(tr, th, 80, false))

	// A narrower window re-wraps everything.
	sameRender(t, "after a width change", tr.renderView(th, 40, false, breadcrumbHint), coldRender(tr, th, 40, false))

	// A live call, and the star's two phases. The blink phase reaches only a live block's header,
	// so the two phases must differ here and be right at both.
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "2", Tool: "shell", Arguments: json.RawMessage(`{"command":"ls"}`)}})
	settled := tr.renderView(th, 80, false, breadcrumbHint)
	sameRender(t, "live block, settled phase", settled, coldRender(tr, th, 80, false))
	blinked := tr.renderView(th, 80, true, breadcrumbHint)
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
	before := tr.renderView(th, 80, false, breadcrumbHint) // warm the run's block, with no reading on it yet
	subAgentUsage(tr, 1, 12000, 32768)
	first := tr.renderView(th, 80, false, breadcrumbHint)
	sameRender(t, "after the delegate's first reading", first, coldRender(tr, th, 80, false))
	if equalLines(before.lines, first.lines) {
		t.Error("the first context reading changed no line — the fixture's run paints no fill cell")
	}
	subAgentUsage(tr, 1, 18000, 32768)
	second := tr.renderView(th, 80, false, breadcrumbHint)
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
	t.Parallel()

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
	// The run folds under the umbrella, so its members are painted only under an OPEN type row
	// (toolblock.go): opening it is what puts the member states in the paint at all.
	if !tr.setTypeExpanded(0, true) {
		t.Fatal("setTypeExpanded(0, true) = false; want the Terminal run's type row open")
	}
	collapsed := tr.renderView(th, 80, false, breadcrumbHint) // warm

	for member := range 3 {
		if !tr.setExpanded(member, true) {
			t.Fatalf("entries[%d] is not a toggleable block — fixture is wrong", member)
		}
		opened := tr.renderView(th, 80, false, breadcrumbHint)
		sameRender(t, fmt.Sprintf("member %d open", member), opened, coldRender(tr, th, 80, false))
		if equalLines(opened.lines, collapsed.lines) {
			t.Errorf("opening member %d served the collapsed paint; the key does not cover its state", member)
		}
		if !tr.setExpanded(member, false) {
			t.Fatalf("entries[%d] would not close again", member)
		}
	}
	sameRender(t, "closed again", tr.renderView(th, 80, false, breadcrumbHint), collapsed)
}

// A session switch re-uses head indices for a different conversation's entries, and reset is what
// keeps the cache from answering about the old one. Without the clear in transcript.reset this
// test paints the FIRST session's message at index 1.
func TestPaintCacheDoesNotSurviveAReset(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	tr := warmed(&transcript{})
	tr.addStartup(startupView{Host: "localhost", Model: "test"})
	tr.addUser("the first session's prompt", nil)
	tr.renderView(th, 80, false, breadcrumbHint) // warm

	tr.reset()
	tr.addStartup(startupView{Host: "localhost", Model: "test"})
	tr.addUser("a different session entirely", nil)
	sameRender(t, "after a session switch", tr.renderView(th, 80, false, breadcrumbHint), coldRender(tr, th, 80, false))
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
	t.Parallel()

	th := newTheme(scheme.Default())
	tr := warmed(&transcript{})
	tr.addStartup(startupView{Host: "localhost", Model: "connecting…", Version: "0.0.0"})
	tr.addUser("read the two files, then survey the tests", nil)

	// The frame's own two parameters travel with the script: a step may move either, and every check
	// after it renders warm and cold at whatever they now are.
	width, blink := 80, false
	check := func(what string) {
		t.Helper()
		sameRender(t, what, tr.renderView(th, width, blink, breadcrumbHint), coldRender(tr, th, width, blink))
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
			shut := tr.renderView(th, width, blink, breadcrumbHint)
			if !tr.toggleTypeExpanded(head) {
				t.Fatalf("entries[%d] heads no run — the script's fixture is wrong", head)
			}
			listed := tr.renderView(th, width, blink, breadcrumbHint)
			check("the Run's type row opened")
			if equalLines(shut.lines, listed.lines) {
				t.Error("the type row painted identically open and shut — it lists nothing")
			}
			if !tr.toggleExpanded(head) {
				t.Fatalf("entries[%d] is not a toggleable block — the script's fixture is wrong", head)
			}
			expanded := tr.renderView(th, width, blink, breadcrumbHint)
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
			settled := tr.renderView(th, width, false, breadcrumbHint)
			blink = true
			check("the star's hollow phase")
			if equalLines(settled.lines, tr.renderView(th, width, true, breadcrumbHint).lines) {
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
			quiet := tr.renderView(th, width, blink, breadcrumbHint)
			check("the second run, before its delegate has reported")
			subAgentUsage(tr, 1, 12000, 32768)
			first := tr.renderView(th, width, blink, breadcrumbHint)
			check("the delegate's first reading")
			if equalLines(quiet.lines, first.lines) {
				t.Error("the first reading changed no line — the script's run paints no fill cell")
			}
			subAgentUsage(tr, 1, 18000, 32768)
			if equalLines(first.lines, tr.renderView(th, width, blink, breadcrumbHint).lines) {
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
	}, {
		// The umbrella's fold is a paint input no entry carries: the Terminal call made the reads'
		// umbrella two type rows, so a threshold of 1 makes it large once at rest — c4 has been
		// open through every step above (the blink step needs it), so first its result lands — and
		// the preference then folds it to its header and opens it again with no flag flipped and no
		// entry appended (paintKey.large/folded).
		name: "the Tools umbrella folding and opening again",
		mutate: func() {
			tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
				CallID:  "c4",
				Content: "[File: d.go, 2 lines total, showing lines 1-2]\n…",
				Summary: domain.ReadSpan{Start: 1, End: 2, Total: 2},
			}})
			small := tr.renderView(th, width, blink, breadcrumbHint)
			check("the umbrella at rest, below the threshold")
			tr.setToolsFoldOver(1)
			folded := tr.renderView(th, width, blink, breadcrumbHint)
			check("the umbrella folded to its header")
			if equalLines(small.lines, folded.lines) {
				t.Error("the umbrella painted identically small and folded — the threshold moved nothing")
			}
			tr.setToolsOpen(true)
			opened := tr.renderView(th, width, blink, breadcrumbHint)
			check("the large umbrella opened")
			if equalLines(folded.lines, opened.lines) {
				t.Error("the umbrella painted identically folded and open — the preference moved nothing")
			}
			tr.setToolsOpen(false)
			tr.setToolsFoldOver(0)
		},
	}}

	for _, s := range steps {
		s.mutate()
		check(s.name)
	}
}

// The regression the fold's key terms guard: an umbrella's ▶/▼ and the rows beneath it are served
// from the cache, and the two things that move them — the shared preference, and a threshold edit
// under a shut preference that carries the umbrella across the line — flip no entry flag and
// append no entry; a SMALL umbrella's fold flips its head entry's own flag, which the key reads
// through the same fold slot rather than spanFlags. Each is walked warm against a cold oracle, and
// the flip is asserted to have painted differently so a key that ignored it could not pass by
// painting the same thing twice.
func TestPaintCacheRepaintsWhenTheFoldFlips(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	build := func(t *testing.T) *transcript {
		t.Helper()
		tr := warmed(&transcript{toolsFoldOver: 1})
		tr.addUser("read both, then test", nil)
		readCall(tr, "c1", "a.go", 1, 5, 0)
		readCall(tr, "c2", "b.go", 1, 9, 0)
		runCall(tr, "c3", "go test", "ok   a\nPASS", 0)
		return tr
	}
	check := func(t *testing.T, what string, tr *transcript) renderedTranscript {
		t.Helper()
		got := tr.renderView(th, 80, false, breadcrumbHint)
		sameRender(t, what, got, coldRender(tr, th, 80, false))
		return got
	}
	// Every umbrella's header is the fold's target at either size (renderSuperGroup), so the paint
	// is asked for exactly one targetUmbrella line whether it stands folded or open.
	umbrellaTargets := func(t *testing.T, got renderedTranscript, what string) {
		t.Helper()
		headers := 0
		for _, target := range got.targets {
			if target.kind == targetUmbrella {
				headers++
			}
		}
		if headers != 1 {
			t.Errorf("%s: %d targetUmbrella lines; want the header alone", what, headers)
		}
	}

	t.Run("the toolsOpen flip", func(t *testing.T) {
		t.Parallel()

		tr := build(t)
		folded := check(t, "folded at the default", tr)
		if !tr.setToolsOpen(true) {
			t.Fatal("setToolsOpen(true) = false; want the preference to move")
		}
		opened := check(t, "opened by the preference", tr)
		if equalLines(folded.lines, opened.lines) {
			t.Error("the umbrella painted identically folded and open — the fixture is not large")
		}
		tr.setToolsOpen(false)
		if again := check(t, "folded again", tr); !equalLines(folded.lines, again.lines) {
			t.Error("folding again painted something other than the first fold")
		}
	})

	t.Run("a toolsFoldOver change under toolsOpen = false", func(t *testing.T) {
		t.Parallel()

		tr := build(t)
		large := check(t, "large and folded at the default", tr)
		umbrellaTargets(t, large, "large and folded")
		if !tr.setToolsFoldOver(2) {
			t.Fatal("setToolsFoldOver(2) = false; want the threshold to move")
		}
		small := check(t, "small and open under the raised threshold", tr)
		if equalLines(large.lines, small.lines) {
			t.Error("the umbrella painted identically large and small — the raised threshold served the stale ▶")
		}
		umbrellaTargets(t, small, "small and open")
	})

	t.Run("a small umbrella's head-flag flip", func(t *testing.T) {
		t.Parallel()
		const head = 1 // the prompt is entry 0; the umbrella's head is its first call

		tr := build(t)
		tr.setToolsFoldOver(2) // two type rows: at the threshold, so small
		open := check(t, "small and open by default", tr)
		if !tr.setUmbrellaFolded(head, true) {
			t.Fatal("setUmbrellaFolded(head, true) = false; want the head flag to move")
		}
		folded := check(t, "folded on the head flag", tr)
		if equalLines(open.lines, folded.lines) {
			t.Error("the small umbrella painted identically open and folded — the head flag served the stale rows")
		}
		tr.setUmbrellaFolded(head, false)
		if again := check(t, "opened again on the head flag", tr); !equalLines(open.lines, again.lines) {
			t.Error("opening again painted something other than the first open paint")
		}
	})
}

// The reuse property — the reason the cache exists. A long settled scrollback with a reply streaming
// underneath it: one more token must cost the TAIL and nothing else, which is exactly the shape of
// the 100%-GPU report this item answers (the issue register). The miss counter is the instrument,
// because the output alone cannot tell a reused paint from a re-computed identical one.
//
// The streaming tail is not a cached block at all (renderView paints t.pending straight, and it
// changes on every token), so the number a token append must not move is the count of paints
// performed over the COMMITTED entries — and that number is zero.
func TestPaintCacheRepaintsOnlyTheStreamingTail(t *testing.T) {
	t.Parallel()

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

	if got := tr.renderView(th, 80, false, breadcrumbHint); len(got.lines) == 0 {
		t.Fatal("the fixture rendered nothing")
	}
	if got := tr.paints.misses; got != blocks {
		t.Fatalf("the cold fill painted %d blocks; want %d (every committed entry, the tail uncached)", got, blocks)
	}

	// One more token, and a repaint. Only the tail moved.
	hits, misses := tr.paints.hits, tr.paints.misses
	tr.apply(domain.TokenEvent{Text: "coming in"})
	appended := tr.renderView(th, 80, false, breadcrumbHint)
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
	second := tr.renderView(th, 80, false, breadcrumbHint)
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
			if got := tr.renderView(th, 100, false, breadcrumbHint); len(got.lines) == 0 {
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
		tr.renderView(th, 100, false, breadcrumbHint)
		repaint(b, tr)
	})
	// Run view: the same repaint rooted at a delegation, the walk the run view drives
	// (runview.go) — its own blocks served warm, the header and prompt painted per frame.
	b.Run("run view", func(b *testing.B) {
		tr, root := allHitRunViewFixture()
		tr = warmed(tr)
		tr.setRoot(root)
		tr.renderView(th, 100, false, breadcrumbHint)
		repaint(b, tr)
	})
}

// ----------------------------------------------------------------------------
// A cache-hit block allocates nothing large (render.go, resolveBlock / resolveGroup)
// ----------------------------------------------------------------------------

// allHitBlocks is how many blocks each all-hit fixture below paints: the scrollback the base
// measurement was taken over (≈ 28 MB for one all-hit repaint of a run view this long).
const allHitBlocks = 800

// maxAllHitBytesPerBlock is the ceiling one cache-hit block may cost a repaint: nothing a block
// resolves on a hit may be theme-sized (≈ 32 KB) or record-sized per covered entry (≈ 832 B each);
// what remains is the frame's own line and target slices, amortised over its blocks.
const maxAllHitBytesPerBlock = 1 << 10

// allHitFlatFixture is allHitBlocks top-level blocks: prompts and answers, one entry each.
func allHitFlatFixture() *transcript {
	tr := &transcript{}
	for i := range allHitBlocks / 2 {
		tr.addUser(fmt.Sprintf("question %d — what does the fold do here?", i), nil)
		tr.apply(domain.MessageEvent{Text: fmt.Sprintf("answer %d, long enough to wrap at least once at the width painted here", i)})
	}
	return tr
}

// allHitRunViewFixture is one delegation whose span holds allHitBlocks blocks — answers and lone
// reads alternating, so no two reads fold into an umbrella — and the ref of the run a view opens
// on.
func allHitRunViewFixture() (*transcript, runRef) {
	tr := &transcript{}
	tr.addUser("what changed?", nil)
	delegationCall(tr, "", "s1", "repo-scout", "scout the repo", 0)
	for i := range allHitBlocks / 2 {
		tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 1, CallID: "s1"},
			Text: fmt.Sprintf("the child's answer %d", i)})
		delegatedRead(tr, "s1", fmt.Sprintf("r%d", i), fmt.Sprintf("f%d.go", i), 1)
	}
	return tr, runRef{depth: 1, spawn: "s1"}
}

// allHitMultiFixture is allHitBlocks top-level blocks where most cover MANY entries: a two-member
// delegation list (resolveGroup) and a lone collapsed run, each span eight reads deep, and a
// two-read Tools umbrella, among the prompts and answers that keep them apart.
func allHitMultiFixture() *transcript {
	tr := &transcript{}
	delegate := func(spawn string) {
		delegationCall(tr, "", spawn, "scout-"+spawn, "scout "+spawn, 0)
		for k := range 8 {
			delegatedRead(tr, spawn, fmt.Sprintf("%s-r%d", spawn, k), fmt.Sprintf("%s-%d.go", spawn, k), 1)
		}
	}
	for i := range allHitBlocks / 6 {
		tr.addUser(fmt.Sprintf("question %d", i), nil)
		delegate(fmt.Sprintf("a%d", i)) // the group: two adjacent delegations
		delegate(fmt.Sprintf("b%d", i))
		tr.apply(domain.MessageEvent{Text: fmt.Sprintf("answer %d", i)})
		readCall(tr, fmt.Sprintf("u%d-1", i), "x.go", 1, 5, 0) // the umbrella: two adjacent reads
		readCall(tr, fmt.Sprintf("u%d-2", i), "y.go", 1, 5, 0)
		tr.apply(domain.MessageEvent{Text: fmt.Sprintf("more %d", i)})
		delegate(fmt.Sprintf("c%d", i)) // a lone collapsed run
	}
	return tr
}

// allHitRenderBytes is what one all-hit repaint of tr allocates, averaged over a few repaints after
// one that fills the cache, and how many blocks that repaint served — every one of them a hit.
func allHitRenderBytes(t *testing.T, tr *transcript, th theme) (bytes uint64, blocks int) {
	t.Helper()
	const width, repaints = 100, 5
	tr.renderView(th, width, false, breadcrumbHint) // fill the cache
	hits, misses := tr.paints.hits, tr.paints.misses
	var stats runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&stats)
	before := stats.TotalAlloc
	for range repaints {
		runtime.KeepAlive(tr.renderView(th, width, false, breadcrumbHint))
	}
	runtime.ReadMemStats(&stats)
	if got := tr.paints.misses - misses; got != 0 {
		t.Fatalf("the all-hit repaints took %d misses; want 0", got)
	}
	return (stats.TotalAlloc - before) / repaints, (tr.paints.hits - hits) / repaints
}

// A repaint whose blocks all hit the cache costs each block no theme copy and no materialised
// records: resolving a block used to build its paint closure over the ≈ 32 KB theme and the head
// record, both moved to the heap on every call, hit or miss, and a multi-entry block stated one
// ≈ 832 B record per covered entry just to key it. Not parallel: TotalAlloc is process-wide, and a
// neighbour's allocations would be read as this one's.
func TestAllHitRepaintAllocatesLittlePerBlock(t *testing.T) {
	th := newTheme(scheme.Default())
	runView := func() *transcript {
		tr, root := allHitRunViewFixture()
		tr.setRoot(root)
		return tr
	}
	for _, fx := range []struct {
		name  string
		build func() *transcript
	}{
		{"single-entry blocks", allHitFlatFixture},
		{"a run view", runView},
		{"multi-entry blocks", allHitMultiFixture},
	} {
		t.Run(fx.name, func(t *testing.T) {
			tr := warmed(fx.build())
			bytes, blocks := allHitRenderBytes(t, tr, th)
			if blocks < allHitBlocks*3/4 {
				t.Fatalf("the fixture painted %d blocks; want about %d", blocks, allHitBlocks)
			}
			t.Logf("%d blocks, %d B per block", blocks, bytes/uint64(blocks))
			if per := bytes / uint64(blocks); per > maxAllHitBytesPerBlock {
				t.Errorf("an all-hit repaint of %d blocks allocated %d KB, %d B per block; want ≤ %d B",
					blocks, bytes>>10, per, maxAllHitBytesPerBlock)
			}
		})
	}
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

// A row is memoised by head ENTRY INDEX, and an entry has exactly one of those however the paint is
// rooted — so the root has to be in the key or a view opened on a child would be handed the railed
// paint the top level left behind at those very indices. Both directions are checked, because the
// hazard is symmetric: what a view memoises must not be served back to the conversation either.
func TestPaintCacheKeysOnTheRoot(t *testing.T) {
	t.Parallel()

	tr, root := rootedFixture()
	tr = warmed(tr)
	th := newTheme(scheme.Default())

	tr.renderView(th, 80, false, breadcrumbHint) // cold, filling the cache at the top level
	sameRender(t, "the top level served warm", tr.renderView(th, 80, false, breadcrumbHint), coldRender(tr, th, 80, false))

	misses := tr.paints.misses
	tr.setRoot(root)
	sameRender(t, "rooted at the child", tr.renderView(th, 80, false, breadcrumbHint), coldRender(tr, th, 80, false))
	if tr.paints.misses == misses {
		t.Error("the rooted paint drew nothing fresh; want the root in the key, not the top level's rows served back")
	}

	tr.setRoot(runRef{})
	sameRender(t, "back at the top level", tr.renderView(th, 80, false, breadcrumbHint), coldRender(tr, th, 80, false))
}

// ----------------------------------------------------------------------------
// One render of each open pane per frame (model.go, transcriptRows; panes.go, paneSpec.height)
// ----------------------------------------------------------------------------

// TestOverlayPanesRenderOncePerUpdateAndView pins the frame's pane-render budget: with each row of
// the pane table open, one non-pointer Update — an engine Event, a reasoning chunk, a keypress —
// plus the View that follows renders every pane at most once, and a pane still open after the
// Update exactly once (View's). A closed pane's render is asked too, by View's one walk of the
// table, and answers "" at once; it is counted like any other, which is why the ceiling is per row
// and not per open pane. The repaint tail of the Update ([Model.settle]) sizes the transcript clamp
// from the panes' height queries, which render nothing; before them it rendered every open pane
// through frameOverlays, and View and a second layout() rendered them again — the /thinking pane
// three times per reasoning chunk.
//
// It reads the process-wide counter (paneRenders), so it does not run in parallel: the tests that
// do are held until every serial one has finished.
func TestOverlayPanesRenderOncePerUpdateAndView(t *testing.T) {
	msgs := []struct {
		name string
		msg  tea.Msg
	}{
		{"a token event", eventMsg{Event: domain.TokenEvent{Text: "a streamed word "}}},
		{"a reasoning chunk", eventMsg{Event: reasoningAt(runRef{}, 90, "one more reasoning chunk")}},
		{"a keypress", keyDown()},
	}
	for _, fx := range paneFixtures() {
		for _, in := range msgs {
			t.Run(fx.name+"/"+in.name, func(t *testing.T) {
				m := fx.build(t)
				for p := range paneRenders {
					paneRenders[p].Store(0)
				}
				next := step(t, m, in.msg)
				next.View()
				for p := framePane(0); p < paneKinds; p++ {
					got := paneRenders[p].Load()
					if got > 1 {
						t.Errorf("the %s rendered %d times over one Update and its View, want at most once",
							paneSpecs[p].name, got)
					}
					if paneSpecs[p].open(next) && got != 1 {
						t.Errorf("the open %s rendered %d times over one Update and its View, want View's one",
							paneSpecs[p].name, got)
					}
				}
			})
		}
	}
}

// BenchmarkThinkingPaneUpdateAndView times what a reasoning chunk costs the frame with the
// /thinking pane open at its record cap: the Update that folds it (its repaint tail sizing the
// transcript clamp through the panes' height queries) and the View that draws the pane.
func BenchmarkThinkingPaneUpdateAndView(b *testing.B) {
	m := newModel(context.Background(), &fakeEngine{}, testOpts, nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	line := strings.Repeat("reasoning word ", 20) + "\n"
	record := strings.Repeat(line, thinkingRecordCap/len(line)+1)
	for turn := range maxThinkingRecords {
		m.thinking.append(record, runRef{}, turn)
		m.thinking.commit(runRef{})
	}
	m.opts.UI.ShowScrollbar = true
	m.thinkingPane = reportPane{open: true, follow: true}
	m.layout()
	if m.renderReport(thinkingReport) == "" {
		b.Fatal("the frame seated no /thinking pane")
	}
	chunk := eventMsg{Event: reasoningAt(runRef{}, maxThinkingRecords, "another chunk ")}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		next, _ := m.Update(chunk)
		m = next.(Model)
		m.View()
	}
}

// ----------------------------------------------------------------------------
// Widget cells are measured once per painted block (render.go, reserveWidgetCells)
// ----------------------------------------------------------------------------

// widgetCellsFixture is a scrollback whose lines overrun the viewport in BYTES — styled rows, and
// answers filled to the column and ending in VS16 glyphs the widget counts two cells each — so
// every repaint before stored widths had to measure them, and some really are broken by the
// reserve.
func widgetCellsFixture(width int) *transcript {
	tr := &transcript{}
	for i := range 12 {
		tr.addUser(fmt.Sprintf("question %d — what does the fold do here?", i), nil)
		tr.commitAssistant(strings.Repeat("a", width-4)+vs16Warning+vs16Warning, runRef{})
		readCall(tr, fmt.Sprintf("r%d", i), fmt.Sprintf("f%d.go", i), 1, 5, 0)
	}
	return tr
}

// baseReserve is the reserve as it measured before any block stored a width: the same paint with
// its stored cells dropped, so every line is measured where it stands.
func baseReserve(r renderedTranscript, limit int) renderedTranscript {
	r.cells = nil
	return r.reserveWidgetCells(limit)
}

// sameReserve fails when two reserved paints differ in a line, a target, a user-block span or the
// header.
func sameReserve(t *testing.T, what string, got, want renderedTranscript) {
	t.Helper()
	sameRender(t, what, got, want)
	if !slices.Equal(got.userBlocks, want.userBlocks) {
		t.Errorf("%s: userBlocks = %+v; want %+v", what, got.userBlocks, want.userBlocks)
	}
	if got.header != want.header {
		t.Errorf("%s: header = %+v; want %+v", what, got.header, want.header)
	}
}

// A repaint served from the cache measures none of its cached lines in the widget's measure: each
// block stored its lines' widths when it was painted, so the reserve reads them instead of
// re-measuring the scrollback every frame. A block that changes is measured again — its own lines,
// and nothing of the blocks around it. Not parallel: widgetMeasures is process-wide, and a
// neighbour's render would be counted as this one's.
func TestWidgetCellsAreMeasuredOncePerPaintedBlock(t *testing.T) {
	th := newTheme(scheme.Default())
	const width = 60
	limit := width + bodyRightGutter
	tr := warmed(widgetCellsFixture(width))
	paint := func() renderedTranscript { return tr.renderView(th, width, false, breadcrumbHint) }
	measured := func(f func()) int64 {
		before := widgetMeasures.Load()
		f()
		return widgetMeasures.Load() - before
	}

	first := paint()
	overBytes := 0
	for _, ln := range first.lines {
		if len(ln) > limit {
			overBytes++
		}
	}
	if overBytes == 0 {
		t.Fatal("setup: no line overruns the limit in bytes; the fixture measures nothing either way")
	}
	if got := len(baseReserve(first, limit).lines); got == len(first.lines) {
		t.Fatal("setup: the reserve broke no line; the fixture does not exercise a break")
	}

	var repaint renderedTranscript
	if got := measured(func() { repaint = paint().reserveWidgetCells(limit) }); got != 0 {
		t.Errorf("an all-hit repaint measured %d lines; want 0 (the %d byte-overrunning lines are stored)", got, overBytes)
	}
	sameReserve(t, "all-hit repaint", repaint, baseReserve(first, limit))

	// One block more: only its lines are measured, once, when it is painted.
	tr.commitAssistant(strings.Repeat("b", width-4)+vs16Warning+vs16Warning, runRef{})
	var grown renderedTranscript
	got := measured(func() { grown = paint() })
	added := grown.lines[len(first.lines)+1:] // past the separator the new block opened with
	want := 0
	for _, ln := range added {
		if len(ln) > width {
			want++
		}
	}
	if want == 0 || int(got) != want {
		t.Errorf("painting one new block measured %d lines; want its own %d over-width lines", got, want)
	}
	if got := measured(func() { grown.reserveWidgetCells(limit) }); got != 0 {
		t.Errorf("the reserve over the grown paint measured %d lines; want 0", got)
	}
}

// The stored widths change nothing the reserve decides: over wide and narrow viewports, and under
// either painter measure (WcWidth before a terminal answers mode 2027, GraphemeWidth after), the
// reserved lines, targets and spans equal what measuring every line where it stands produced —
// through a cold paint, a warm one and the rooted run view alike.
func TestWidgetCellsReserveAsTheWidgetMeasures(t *testing.T) {
	t.Parallel()
	for _, method := range []struct {
		name   string
		method ansi.Method
	}{{"wcwidth", ansi.WcWidth}, {"grapheme", ansi.GraphemeWidth}} {
		for _, width := range []int{24, 60, 120} {
			t.Run(fmt.Sprintf("%s/%d", method.name, width), func(t *testing.T) {
				t.Parallel()
				th := newTheme(scheme.Default())
				th.measure = widthAuthority{method: method.method}
				for _, limit := range []int{width, width + bodyRightGutter} {
					tr := warmed(widgetCellsFixture(width))
					cold := coldRender(tr, th, width, false)
					tr.renderView(th, width, false, breadcrumbHint) // fill the cache
					warm := tr.renderView(th, width, false, breadcrumbHint)
					sameReserve(t, "warm", warm.reserveWidgetCells(limit), baseReserve(cold, limit))

					view, root := allHitRunViewFixture()
					view = warmed(view)
					view.setRoot(root)
					view.renderView(th, width, false, breadcrumbHint)
					rooted := view.renderView(th, width, false, breadcrumbHint)
					sameReserve(t, "run view", rooted.reserveWidgetCells(limit), baseReserve(rooted, limit))
				}
			})
		}
	}
}

// A measured paint stays measured through the two rebuilds a block can go through after it is
// drawn: railing re-measures every line it had a width for as the railed line, and retargeting —
// which keeps the lines — keeps the widths.
func TestWidgetCellsSurviveRailedAndRetargeted(t *testing.T) {
	t.Parallel()
	th := newTheme(scheme.Default())
	lines := []string{"short", strings.Repeat("x", 30) + vs16Warning}
	p := blockPaint{lines: lines, targets: make([]lineMark, len(lines)), cells: measuredCells(lines, 10)}
	if p.cells[0] != -1 || p.cells[1] != ansi.StringWidth(lines[1]) {
		t.Fatalf("setup: cells = %v; want [-1 %d]", p.cells, ansi.StringWidth(lines[1]))
	}

	railed := p.railed(th, 2)
	wantRailed := []int{-1, ansi.StringWidth(railed.lines[1])}
	if !slices.Equal(railed.cells, wantRailed) {
		t.Errorf("railed cells = %v; want %v", railed.cells, wantRailed)
	}
	if got := p.retargeted(targetTask).cells; !slices.Equal(got, p.cells) {
		t.Errorf("retargeted cells = %v; want %v", got, p.cells)
	}
	if got := (blockPaint{lines: lines}).railed(th, 2).cells; got != nil {
		t.Errorf("an unmeasured paint railed carries cells %v; want none", got)
	}

	// Joining keeps lines and cells in lockstep, whichever side was measured.
	var joined blockPaint
	joined.add([]string{"head"}, targetHeader)
	joined.join(p)
	joined.add([]string{"tail"}, targetNone)
	if len(joined.cells) != len(joined.lines) || joined.cells[0] != -1 || joined.cells[2] != p.cells[1] || joined.cells[3] != -1 {
		t.Errorf("joined cells = %v over %d lines; want [-1 -1 %d -1]", joined.cells, len(joined.lines), p.cells[1])
	}
}

// BenchmarkReserveWidgetCellsAllHit is what the reserve costs a repaint served from the cache: the
// scrollback's stored widths read instead of every overrunning line measured again.
func BenchmarkReserveWidgetCellsAllHit(b *testing.B) {
	th := newTheme(scheme.Default())
	const width = 100
	tr := warmed(widgetCellsFixture(width))
	for range 40 {
		tr.commitAssistant(strings.Repeat("word ", 60), runRef{})
	}
	tr.renderView(th, width, false, breadcrumbHint)
	b.ReportAllocs()
	for b.Loop() {
		tr.renderView(th, width, false, breadcrumbHint).reserveWidgetCells(width + bodyRightGutter)
	}
}
