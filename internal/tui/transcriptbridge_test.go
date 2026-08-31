package tui

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/schedule"
	"github.com/airiclenz/apogee/internal/session"
)

// ----------------------------------------------------------------------------
// The transcript bridge (the TUI half of the neutral codec)
// ----------------------------------------------------------------------------

// mixedEntries is a scrollback covering every persisted entry kind and the tricky corners a
// resume must repaint exactly: the skill tokens LOCATED on a user send, a nested (depth>0) assistant block,
// an enriched tool-call card (summary + coloured details + done, carrying its unexported name),
// a diff-bodied card whose recorded Edit regions ride beside the rows they were rendered into,
// a standalone tool result, a recovered error, a neutral note, and a presented document with a
// domain Method. It is the fixture behind the round-trip and exclusion tests.
//
// The tool card's body is built through newToolBody and the card finished through sanitize rather
// than left a bare literal, because those are the seams a real view passes (toolBody), and a
// fixture that skipped them would describe a view the presenter never produces — the round-trip's
// DeepEqual would then pass or fail on the fixture's shortcut instead of on the codec.
func mixedEntries() []entry {
	toolCard := toolView{
		Label: "Read", Verb: "reading", Target: "main.go", name: "read_file",
		Summary: namedSummary(detailLine{Text: "1 - 100"}),
		Details: newToolBody([]detailLine{
			{Kind: detailDiffAdded, Text: "+ added line"},
			{Kind: detailDiffRemoved, Text: "- removed line"},
			{Kind: detailPlain, Text: "  context"},
		}),
	}
	toolCard.sanitize()
	// The diff card is the other tool shape a record has to bring back whole: a diff-bodied block
	// carrying the change the tool RECORDED (toolView.Regions) beside the stacked rows it was painted
	// as, and the file each region was cut from — the multi-file case, which is the only one whose
	// names are filled. It is written by the one seam that writes them (showFileRegions) for the
	// reason above: a literal would describe a view the presenter never produces.
	diffCard := toolView{
		Label: "Git Diff", Verb: "diffing", Target: "main..HEAD", name: "git_diff_range",
		Summary: namedSummary(detailLine{Text: "2 files"}),
	}
	diffCard.showFileRegions([]diffFileRegions{
		{File: "a.go", Regions: []domain.EditRegion{{
			BeforeStart: 10, AfterStart: 10,
			Leading:  []string{"func main() {"},
			Removed:  []string{"\tprintln(\"old\")"},
			Inserted: []string{"\tprintln(\"new\")"},
			Trailing: []string{"}"},
		}}},
		{File: "b.go", Regions: []domain.EditRegion{{
			BeforeStart: 1, AfterStart: 1,
			Inserted: []string{"package b"},
		}}},
	})
	diffCard.sanitize()
	return []entry{
		{kind: entryUser, text: "/reviewer read main.go", skillSpans: []skillSpan{{start: 0, end: 9}}},
		{kind: entryAssistant, text: "Reading it now.", depth: 1},
		{kind: entryToolCall, callID: "c1", done: true, tool: toolCard},
		{kind: entryToolCall, callID: "c2", done: true, tool: diffCard},
		{kind: entryToolResult, text: "error: boom", depth: 2},
		{kind: entryError, text: "loop: recovered fault"},
		{kind: entryNote, text: "cancelled"},
		{
			// The OPENED rung, deliberately not the served one: a served entry's Location is the
			// one field the codec does NOT round-trip (toWirePresented drops the doc-server URL),
			// and TestTranscriptCodecDropsTheServedURLOnEncode pins that on its own. Every rung
			// but rung 2 leaves Location empty, so this is the shape a record brings back whole.
			kind: entryPresented,
			presented: presentedView{
				Title: "Report", Path: "out/report.md",
				Method: domain.PresentOpened, Reason: "",
			},
		},
	}
}

// TestTranscriptCodecDropsTheServedURLOnEncode proves the doc server's capability token never
// reaches the session record. Rung 2's URL — /d/<32 hex>/<basename> (ADR 0019 §3) — is a grant,
// and the server that honours it is started lazily and closed on shutdown, so a persisted URL is
// dead on every resume and the token is the only thing the record would keep. The entry still
// renders its path and status after a reload; only the link is gone.
func TestTranscriptCodecDropsTheServedURLOnEncode(t *testing.T) {
	t.Parallel()

	const token = "0123456789abcdef0123456789abcdef"
	served := presentedView{
		Title: "Report", Path: "out/report.html",
		Location: "http://192.168.64.2:8080/d/" + token + "/report.html",
		Method:   domain.PresentServed,
	}
	opened := presentedView{Title: "Notes", Path: "out/notes.md", Method: domain.PresentOpened}

	data, err := encodeTranscript(&transcript{entries: []entry{
		{kind: entryPresented, presented: served},
		{kind: entryPresented, presented: opened},
	}})
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	if blob := string(data); strings.Contains(blob, "/d/") || strings.Contains(blob, token) {
		t.Fatalf("the record carries the doc-server URL or its capability token:\n%s", blob)
	}

	got, err := decodeTranscript(data)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("decoded %d entries, want the two presentations", len(got))
	}
	// The rung and the path survive: a resumed served entry still says which rung carried the
	// document and where it lives, it just no longer offers a link nothing would answer.
	if pv := got[0].presented; pv.Method != domain.PresentServed || pv.Path != served.Path || pv.Title != served.Title {
		t.Errorf("served entry = %+v; want the served rung with its path and title intact", pv)
	}
	if pv := got[0].presented; pv.Location != "" {
		t.Errorf("served location = %q; want it dropped", pv.Location)
	}
	if pv := got[1].presented; pv != opened {
		t.Errorf("opened entry = %+v; want %+v — only rung 2 loses a field", pv, opened)
	}
}

// TestTranscriptCodecRoundTrip encodes a transcript covering every persisted kind and asserts
// the decoded entries equal the originals in structure — including the tool card's unexported
// name and its coloured detail lines, the depth on nested blocks, and the send's skill spans.
func TestTranscriptCodecRoundTrip(t *testing.T) {
	t.Parallel()
	tr := &transcript{entries: mixedEntries()}
	data, err := encodeTranscript(tr)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	got, err := decodeTranscript(data)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	want := mixedEntries()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round-trip mismatch:\n got = %+v\nwant = %+v", got, want)
	}
}

// firedBlockEntries is a scrollback holding one finished firing block, built through the fold's own
// seams (addFiring then enrichFiring) rather than as a literal, for the reason mixedEntries gives:
// a fixture that skipped the presenters would describe a block the fold never produces.
//
// The answer is deliberately MULTI-LINE. That is the shape a record has to bring back whole — the
// answer leading the body with the summary slot left empty — whereas a one-line answer is promoted
// onto the branch, and what a resume owes a promoted summary is pinned by
// TestTranscriptCodecReplaysAPromotedSummaryAsShown rather than restated here.
func firedBlockEntries() []entry {
	tr := &transcript{}
	tr.addFiring(schedule.Event{
		Kind: schedule.EventFired, ScheduleID: "sch-1", ScheduleName: "nightly tidy",
		Prompt: "check the log\nand tidy it",
	})
	tr.enrichFiring(schedule.Event{
		Kind: schedule.EventCompleted, ScheduleID: "sch-1", ScheduleName: "nightly tidy",
		Elapsed: 4 * time.Second,
		Outcome: schedule.Outcome{
			RecordID: "s1", Title: "nightly tidy — 14:05",
			FinalText: "found 3 stale entries\nremoved them", Turns: 2,
		},
	})
	return tr.entries
}

// TestTranscriptCodecRoundTripsAFiringBlock proves a finished firing block survives the record: it
// is PERSISTED (a Firing is something that actually happened in this session's lifetime — the
// ADR 0022 addendum's test — so encodeTranscript must write it, under the "schedule" kind string),
// and it comes back with every view field, its pairing key, its done mark and every line of its
// body intact.
func TestTranscriptCodecRoundTripsAFiringBlock(t *testing.T) {
	t.Parallel()
	tr := &transcript{entries: firedBlockEntries()}
	data, err := encodeTranscript(tr)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	if !strings.Contains(string(data), `"kind":"schedule"`) {
		t.Fatalf("the firing block did not reach the wire under its kind string: %s", data)
	}
	got, err := decodeTranscript(data)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	if !reflect.DeepEqual(got, firedBlockEntries()) {
		t.Errorf("round-trip mismatch:\n got = %+v\nwant = %+v", got, firedBlockEntries())
	}
}

// TestTranscriptCodecClosesAnUnfinishedFiringBlock pins the one entry a resume deliberately does not
// replay as stored: a block still open when the record was written comes back CLOSED, wearing the
// block's own account of what happened to it. The Firing died with the TUI that scheduled it
// (ADR 0033) and no completed or failed Event for it will ever arrive, so a replayed block still
// showing the running marker would be a lie no later fold could correct. What it was announced with
// — the prompt — stays.
func TestTranscriptCodecClosesAnUnfinishedFiringBlock(t *testing.T) {
	t.Parallel()
	tr := &transcript{}
	tr.addFiring(schedule.Event{
		Kind: schedule.EventFired, ScheduleID: "sch-1", ScheduleName: "nightly tidy",
		Prompt: "check the log",
	})

	data, err := encodeTranscript(tr)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	got, err := decodeTranscript(data)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	if len(got) != 1 || got[0].kind != entrySchedule {
		t.Fatalf("decoded %+v; want the one firing block", got)
	}
	if !got[0].done {
		t.Error("done = false; a replayed firing block must not claim its run is still going")
	}
	if got[0].tool.Summary.Text != scheduleInterruptedSummary {
		t.Errorf("summary = %q, want %q", got[0].tool.Summary.Text, scheduleInterruptedSummary)
	}
	if want := []string{"prompt: check the log"}; !slices.Equal(firingBody(got[0]), want) {
		t.Errorf("body = %q, want the prompt it was announced with %q", firingBody(got[0]), want)
	}
}

// TestTranscriptCodecClosesEveryInterruptedToolCall pins the general replay rule the firing one is a
// special case of: a record written mid-Turn while a delegation ran (the progress save) stores the
// delegation's head and every call standing under it OPEN, and the work behind them died with the
// engine that was running it. closeInterruptedCalls closes each one with the outcome that actually
// befell it and counts them, so the caller can say so once (progressSavedNote). A call that had
// already settled when the record was written is left exactly as it was written.
func TestTranscriptCodecClosesEveryInterruptedToolCall(t *testing.T) {
	t.Parallel()
	tr := &transcript{}
	readCall(tr, "r1", "a.go", 1, 5, 0) // settled long before the record was written
	subAgentCall(tr, "s1", "survey", 0) // the delegation the record caught mid-run
	tr.apply(domain.ToolCallEvent{      // …and the child call it was standing on
		EventBase: domain.EventBase{Depth: 1},
		Call:      domain.ToolCall{ID: "c1", Tool: "read_file", Arguments: []byte(`{"path":"b.go"}`)},
	})

	data, err := encodeTranscript(tr)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	got, err := decodeTranscript(data)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("decoded %d entries, want the settled read, the head and its child", len(got))
	}
	settled := got[0].tool.Summary.Text

	if closed := closeInterruptedCalls(got); closed != 2 {
		t.Errorf("closed = %d, want 2 (the head and the child beneath it)", closed)
	}
	for _, tc := range []struct {
		name  string
		index int
	}{
		{name: "the delegation head", index: 1},
		{name: "the child call beneath it", index: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := got[tc.index]
			if !e.done {
				t.Error("done = false; a replayed call must not claim its work is still going")
			}
			if e.tool.Summary.Text != interruptedSummary {
				t.Errorf("summary = %q, want %q", e.tool.Summary.Text, interruptedSummary)
			}
		})
	}
	if !got[0].done || got[0].tool.Summary.Text != settled {
		t.Errorf("the settled read came back done=%v summary=%q; want it untouched (true, %q)",
			got[0].done, got[0].tool.Summary.Text, settled)
	}
}

// TestTranscriptCodecInterruptedPassLeavesAFiringBlockAlone keeps the two replay rules apart: the
// firing block is closed per entry on the way in, with the account only it can give
// (scheduleInterruptedSummary), and the general pass must not word over it. It never sees it — a
// firing block is a kind of its own and comes back already done — which is what this pins.
func TestTranscriptCodecInterruptedPassLeavesAFiringBlockAlone(t *testing.T) {
	t.Parallel()
	tr := &transcript{}
	tr.addFiring(schedule.Event{
		Kind: schedule.EventFired, ScheduleID: "sch-1", ScheduleName: "nightly tidy",
		Prompt: "check the log",
	})

	data, err := encodeTranscript(tr)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	got, err := decodeTranscript(data)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}

	if closed := closeInterruptedCalls(got); closed != 0 {
		t.Errorf("closed = %d, want 0; the firing rule had already closed the only entry", closed)
	}
	if got[0].tool.Summary.Text != scheduleInterruptedSummary {
		t.Errorf("summary = %q, want the firing block's own account %q",
			got[0].tool.Summary.Text, scheduleInterruptedSummary)
	}
}

// TestTranscriptCodecDecodesALegacyBlobUnchanged proves the new kind is additive in the direction
// that matters for records already on disk: a v1 blob written before the firing block existed
// decodes exactly as it did then. Hand-written bytes rather than a re-encode, because what is being
// tested is an OLD file meeting a map that has since grown a name.
//
// The same blob carries the RETIRED "skills" member — the display names a send stored while the
// block still painted chips — which no field claims any more. It is ignored rather than refused:
// an old record decodes as the plain send it now paints as, which is the pre-production degrade
// the inline-accent plan accepted in place of a migration.
func TestTranscriptCodecDecodesALegacyBlobUnchanged(t *testing.T) {
	t.Parallel()
	data := []byte(`{"version":1,"entries":[` +
		`{"kind":"user","text":"read main.go","skills":["reviewer"]},` +
		`{"kind":"toolCall","callID":"c1","done":true,"tool":{"label":"Read","verb":"reading",` +
		`"target":"main.go","name":"read_file","summary":{"text":"1 - 100"},"details":[{"kind":1,"text":"+x"}]}},` +
		`{"kind":"note","text":"cancelled"}` +
		`]}`)
	got, err := decodeTranscript(data)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	want := []entry{
		{kind: entryUser, text: "read main.go"},
		{kind: entryToolCall, callID: "c1", done: true, tool: toolView{
			Label: "Read", Verb: "reading", Target: "main.go", name: "read_file",
			Summary: namedSummary(detailLine{Text: "1 - 100"}),
			Details: newToolBody([]detailLine{{Kind: detailDiffAdded, Text: "+x"}}),
		}},
		{kind: entryNote, text: "cancelled"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("legacy blob decoded differently:\n got = %+v\nwant = %+v", got, want)
	}
}

// TestTranscriptCodecReDerivesSubAgentSolo proves the one verdict decode does not take from the
// file: a sub-agent head is solo by rule (presentToolCall, design call 12 — a delegation never
// joins a MIXED super-group, whatever it stands next to), so a blob written before Solo rode the
// wire must still replay refusing that fold. Hand-written bytes with no "solo" member, because the
// case IS an old file: a re-encode would carry today's true and prove nothing.
//
// Two records rather than one, and span-less ones — no nested entries behind either head — because
// that is the shape the painter's own span rule cannot catch (a delegation refused at the depth
// bound) and the shape a stale false would fold into an umbrella's row.
//
// What the two DO fold into is the sub-agent group's own list (subAgentGroup, item 7): grouping
// with each OTHER is a different rule from grouping with everyone, and it is derived off the tool
// name rather than off this flag, so a replayed pair reads as one "✦ Sub-Agent (2)".
func TestTranscriptCodecReDerivesSubAgentSolo(t *testing.T) {
	t.Parallel()
	data := []byte(`{"version":1,"entries":[` +
		`{"kind":"toolCall","callID":"s1","done":true,"tool":{"label":"Sub-Agent","verb":"delegating",` +
		`"target":"survey the tests","name":"sub_agent","summary":{"text":"refused"}}},` +
		`{"kind":"toolCall","callID":"s2","done":true,"tool":{"label":"Sub-Agent","verb":"delegating",` +
		`"target":"survey the docs","name":"sub_agent","summary":{"text":"refused"}}}` +
		`]}`)
	got, err := decodeTranscript(data)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("decoded %d entries; want the 2 sub-agent heads", len(got))
	}
	for i, e := range got {
		if !e.tool.solo {
			t.Errorf("entry %d decoded with solo=false; a sub-agent head is solo by rule, whatever the file says", i)
		}
		if groupable(e.tool) {
			t.Errorf("entry %d is groupable after decode; it would fold into its neighbour's block", i)
		}
	}

	// Both heads decoded DONE, and "refused" is not one of the verdicts apogee reserves for failure
	// (failedSummary), so each row carries the done ✓ — the mark is read off the record's own state
	// and its own wording, which is exactly what a replay has to reproduce.
	want := strings.Join([]string{
		"✦ Sub-Agent (2)",
		"  ┝ survey the tests ✓ ⋯ refused",
		"  ┕ survey the docs ✓ ⋯ refused",
	}, "\n")
	if out := renderPlain(&transcript{entries: got}, 80); out != want {
		t.Errorf("replayed delegations mismatch:\n--- got ---\n%s\n--- want ---\n%s", out, want)
	}
}

// TestTranscriptCodecReDerivesAnsweredQuestionSolo proves the second verdict decode does not take
// from the file — and the one no name settles. An ANSWERED ask_user record is a card the reader
// comes back to (askUserAnswerRecord, design call 3), while a question still awaiting its answer is
// an ordinary pending call that groups like any other; so decode reads the record's footprint —
// done beside the body only the answer hook writes — which is what the live presenter stands on
// (ask_user's registry entry sets no argBody, so those Details can have come from nowhere else).
// The third case is what the pair keeps out: an ERRORED question never reaches that hook, so it is
// body-less and groupable live, and it must replay that way too. Hand-written bytes with no "solo"
// member, because the case IS an old file: a re-encode would carry today's true and prove nothing.
func TestTranscriptCodecReDerivesAnsweredQuestionSolo(t *testing.T) {
	t.Parallel()

	t.Run("answered questions replay as two blocks", func(t *testing.T) {
		data := []byte(`{"version":1,"entries":[` +
			`{"kind":"toolCall","callID":"a1","done":true,"tool":{"label":"Ask User","verb":"asking",` +
			`"target":"Ship it?","name":"ask_user","summary":{"text":"Yes"},` +
			`"details":[{"text":"Ship it?"},{"text":"[✔] Yes"},{"text":"[ ] No"}]}},` +
			`{"kind":"toolCall","callID":"a2","done":true,"tool":{"label":"Ask User","verb":"asking",` +
			`"target":"Tag it?","name":"ask_user","summary":{"text":"No"},` +
			`"details":[{"text":"Tag it?"},{"text":"[ ] Yes"},{"text":"[✔] No"}]}}` +
			`]}`)
		got, err := decodeTranscript(data)
		if err != nil {
			t.Fatalf("decodeTranscript: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("decoded %d entries; want the 2 answered questions", len(got))
		}
		for i, e := range got {
			if !e.tool.solo {
				t.Errorf("entry %d decoded with solo=false; an answered record is solo by rule, whatever the file says", i)
			}
			if groupable(e.tool) {
				t.Errorf("entry %d is groupable after decode; it would fold into its neighbour's block", i)
			}
		}

		want := strings.Join([]string{
			"✦ Ask User",
			groupMemberLine("  ┕ Ship it? ⋯ Yes · +3 more lines"),
			"",
			"✦ Ask User",
			groupMemberLine("  ┕ Tag it? ⋯ No · +3 more lines"),
		}, "\n")
		if out := renderPlain(&transcript{entries: got}, 80); out != want {
			t.Errorf("replayed questions mismatch:\n--- got ---\n%s\n--- want ---\n%s", out, want)
		}
	})

	t.Run("a question awaiting its answer is not forced solo", func(t *testing.T) {
		data := []byte(`{"version":1,"entries":[` +
			`{"kind":"toolCall","callID":"a1","tool":{"label":"Ask User","verb":"asking",` +
			`"target":"Ship it?","name":"ask_user"}},` +
			`{"kind":"toolCall","callID":"a2","tool":{"label":"Ask User","verb":"asking",` +
			`"target":"Tag it?","name":"ask_user"}}` +
			`]}`)
		got, err := decodeTranscript(data)
		if err != nil {
			t.Fatalf("decodeTranscript: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("decoded %d entries; want the 2 pending questions", len(got))
		}
		for i, e := range got {
			if e.tool.solo {
				t.Errorf("entry %d decoded solo; nothing has been answered, so there is no record to stand alone", i)
			}
		}

		want := strings.Join([]string{
			"✦ Ask User (2)",
			"  ┝ Ship it? ⋯",
			"  ┕ Tag it? ⋯",
		}, "\n")
		if out := renderPlain(&transcript{entries: got}, 80); out != want {
			t.Errorf("replayed pending questions mismatch:\n--- got ---\n%s\n--- want ---\n%s", out, want)
		}
	})

	t.Run("errored questions stay groupable and replay as one group", func(t *testing.T) {
		// A question the tool could not put to anyone (its Asker was gone): the result comes back an
		// error, enrichWithResult words the branch and returns before the outcome hook, so the record is
		// done with NO body and never becomes a card. Live it groups; the claim here is that the file
		// says the same thing back.
		const errLine = "error: could not ask the user: asker closed"
		data := []byte(`{"version":1,"entries":[` +
			`{"kind":"toolCall","callID":"a1","done":true,"tool":{"label":"Ask User","verb":"asking",` +
			`"target":"Ship it?","name":"ask_user","summary":{"text":"` + errLine + `"}}},` +
			`{"kind":"toolCall","callID":"a2","done":true,"tool":{"label":"Ask User","verb":"asking",` +
			`"target":"Tag it?","name":"ask_user","summary":{"text":"` + errLine + `"}}}` +
			`]}`)
		got, err := decodeTranscript(data)
		if err != nil {
			t.Fatalf("decodeTranscript: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("decoded %d entries; want the 2 failed questions", len(got))
		}
		for i, e := range got {
			if e.tool.solo {
				t.Errorf("entry %d decoded solo; a failed question kept no record, so it groups like the live one", i)
			}
			if !groupable(e.tool) {
				t.Errorf("entry %d is not groupable after decode; live it is, and the two paints must match", i)
			}
		}

		want := strings.Join([]string{
			"✦ Ask User (2)",
			"  ┝ Ship it? ⋯ " + errLine,
			"  ┕ Tag it? ⋯ " + errLine,
		}, "\n")
		if out := renderPlain(&transcript{entries: got}, 80); out != want {
			t.Errorf("replayed failed questions mismatch:\n--- got ---\n%s\n--- want ---\n%s", out, want)
		}

		// The other half of the claim, from the live side: the same two calls folded through the
		// presenter paint the same group, so a reload does not reshape the scrollback.
		live := &transcript{}
		for _, q := range []struct{ id, question string }{{"a1", "Ship it?"}, {"a2", "Tag it?"}} {
			live.apply(domain.ToolCallEvent{Call: domain.ToolCall{
				ID: q.id, Tool: "ask_user", Arguments: []byte(`{"question":"` + q.question + `"}`)}})
			live.apply(domain.ToolResultEvent{Result: domain.ToolResult{
				CallID: q.id, Content: "could not ask the user: asker closed", IsError: true}})
		}
		if out := renderPlain(live, 80); out != want {
			t.Errorf("live failed questions mismatch:\n--- got ---\n%s\n--- want ---\n%s", out, want)
		}
	})
}

// TestTranscriptCodecExcludesStartupAndPending proves the two things that must never persist: the
// one-time start-up box (opening chrome, re-seeded on resume) and the in-progress pending buffer
// (tokens never committed to an entry were never scrollback).
func TestTranscriptCodecExcludesStartupAndPending(t *testing.T) {
	t.Parallel()
	tr := &transcript{}
	tr.addStartup(startupView{Logo: "APOGEE", Host: "host", Model: "model", Version: "0.8.0"})
	tr.addUser("hello", nil)
	tr.pending = bufOf("half-typed answer the user never saw committed")
	tr.streaming = true

	data, err := encodeTranscript(tr)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	got, err := decodeTranscript(data)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("decoded %d entries; want exactly 1 (the user send)", len(got))
	}
	if got[0].kind != entryUser || got[0].text != "hello" {
		t.Errorf("decoded entry = %+v; want the user send", got[0])
	}
	for _, e := range got {
		if e.kind == entryStartup {
			t.Error("start-up box was serialized; it must be excluded")
		}
		if strings.Contains(e.text, "half-typed") {
			t.Error("pending buffer leaked into a committed entry")
		}
	}
}

// TestTranscriptCodecExcludesEphemeral proves the third exclusion: a display-only note is in the
// transcript the human sees but never in the blob the record keeps, while an ordinary note beside
// it still round-trips. Persisting a re-derived notice is what made resume notes stack up in the
// record, one more on every reopen.
func TestTranscriptCodecExcludesEphemeral(t *testing.T) {
	t.Parallel()
	tr := &transcript{}
	tr.addUser("what is the capital of france", nil)
	tr.addNote("cancelled")
	tr.addEphemeralNote("resumed: france question")

	if len(tr.entries) != 3 {
		t.Fatalf("in-memory entries = %d; want 3 — an ephemeral note is displayed like any other", len(tr.entries))
	}
	if last := tr.entries[2]; last.kind != entryNote || !last.ephemeral {
		t.Errorf("ephemeral note = {kind %v, ephemeral %v}; want {entryNote, true}", last.kind, last.ephemeral)
	}

	data, err := encodeTranscript(tr)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	if strings.Contains(string(data), "resumed:") {
		t.Errorf("the ephemeral note reached the wire: %s", data)
	}
	got, err := decodeTranscript(data)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("decoded %d entries; want 2 (the user send and the persistent note)", len(got))
	}
	if got[1].kind != entryNote || got[1].text != "cancelled" {
		t.Errorf("decoded note = %+v; want the persistent 'cancelled' note", got[1])
	}
	if got[1].ephemeral {
		t.Error("a persistent note decoded as ephemeral; the flag is set by the appender, never by the wire")
	}
}

// TestTranscriptCodecExcludesExpandedState proves the fourth exclusion, and the one the wire form
// gets for free: a block's expanded state is the VIEW's alone (layout.md, "Collapsed and expanded
// blocks"), so an expanded block is stored exactly as a collapsed one and a resumed session paints
// everything collapsed. It holds for every kind that OWNS the state (carriesBlockState), which is why
// the fixture's user send is opened here beside its tool call: a prompt deliberately expanded in one
// session comes back collapsed in the next, like every over-threshold prompt of a replayed
// transcript. The blocks themselves must still round-trip whole — the state is excluded, not the
// entry — so the retained body and the send's skill spans are asserted beside them.
func TestTranscriptCodecExcludesExpandedState(t *testing.T) {
	t.Parallel()
	tr := &transcript{entries: mixedEntries()}
	if !tr.toggleExpanded(2) {
		t.Fatal("toggleExpanded(2) = false; the fixture's tool call did not expand")
	}
	if !tr.toggleExpanded(0) {
		t.Fatal("toggleExpanded(0) = false; the fixture's user send did not expand")
	}

	data, err := encodeTranscript(tr)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	if strings.Contains(string(data), "expanded") {
		t.Errorf("the expanded state reached the wire: %s", data)
	}
	got, err := decodeTranscript(data)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	if !reflect.DeepEqual(got, mixedEntries()) {
		t.Errorf("an expanded entry decoded as something other than its collapsed self:\n got = %+v\nwant = %+v", got, mixedEntries())
	}
	for i := range got {
		if got[i].expanded {
			t.Errorf("entries[%d] decoded expanded; a resumed session paints every block collapsed", i)
		}
	}
}

// TestTranscriptCodecReplaysAPromotedSummaryAsShown pins what a resume owes a card whose summary is
// the tool's own output promoted onto the branch (a one-line `cat`): the stored line is finished
// display text, and it comes back exactly as it was shown, absolute path included — beside a target
// that was and stays workspace-relative. The mark for whose words a summary is rides the wire
// (TestTranscriptCodecRoundTripsTheQuotedSummaryMark), but nothing here depends on it: decode
// escape-strips a replayed card and respells nothing whatever the mark says, which is why a blob
// written before the mark existed replays identically too.
func TestTranscriptCodecReplaysAPromotedSummaryAsShown(t *testing.T) {
	t.Parallel()
	tr := &transcript{ws: newWorkspaceRoot("/home/me/proj")}
	tr.addToolCall(domain.ToolCall{ID: "c1", Tool: "terminal",
		Arguments: []byte(`{"command":"cat /home/me/proj/paths.txt"}`)}, "", runRef{})
	tr.addToolResult(domain.ToolResult{CallID: "c1", Content: "/home/me/proj/docs/plan.md\n"}, runRef{})

	data, err := encodeTranscript(tr)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	got, err := decodeTranscript(data)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	if len(got) != 1 || got[0].kind != entryToolCall {
		t.Fatalf("decoded %+v; want the one tool call", got)
	}
	if want := "/home/me/proj/docs/plan.md"; got[0].tool.Summary.Text != want {
		t.Errorf("replayed summary = %q, want %q — the quoted line was respelled on the way back",
			got[0].tool.Summary.Text, want)
	}
	if want := "cat paths.txt"; got[0].tool.Target != want {
		t.Errorf("replayed target = %q, want %q", got[0].tool.Target, want)
	}
}

// TestTranscriptCodecRoundTripsTheQuotedSummaryMark pins the mark beside the branch line: whose
// words that line is (branchSummary.quoted) is a verdict the presenter reaches on the way IN, and
// the record has to carry it rather than let it decode back as the presenter's own. Three cases,
// because the member is additive: a PROMOTED line comes back quoted, a line the block worded itself
// stays unquoted and writes nothing extra, and a blob predating the member decodes exactly as it
// does today.
//
// Every case asserts the PAINT as well, and that is the point of the fix rather than a side note:
// nothing reads the mark after decode — the replay path runs sanitize and never finishDisplay — so
// carrying it must not move one row. What it buys is fidelity: a summary that comes back claiming
// the wrong authorship is a record that lies to whatever seam reads it next (shortenPaths today).
func TestTranscriptCodecRoundTripsTheQuotedSummaryMark(t *testing.T) {
	t.Parallel()

	t.Run("a promoted line comes back quoted", func(t *testing.T) {
		// A one-line `cat`: the output is promoted onto the branch as it stands (promotedOutput), which
		// is the shape whose summary is the tool's words and not the block's.
		tr := &transcript{ws: newWorkspaceRoot("/home/me/proj")}
		tr.addToolCall(domain.ToolCall{ID: "c1", Tool: "terminal",
			Arguments: []byte(`{"command":"cat /home/me/proj/paths.txt"}`)}, "", runRef{})
		tr.addToolResult(domain.ToolResult{CallID: "c1", Content: "/home/me/proj/docs/plan.md\n"}, runRef{})
		if len(tr.entries) != 1 || !tr.entries[0].tool.Summary.quoted {
			t.Fatalf("fixture: the promoted output carries no quoted mark to travel (%+v)", tr.entries)
		}

		data, err := encodeTranscript(tr)
		if err != nil {
			t.Fatalf("encodeTranscript: %v", err)
		}
		// The member is part of the record now, so pin its name and where it sits — inside the summary
		// object, beside the text it is a statement about.
		if want := `"text":"/home/me/proj/docs/plan.md","quoted":true`; !strings.Contains(string(data), want) {
			t.Errorf("wire blob does not carry %s:\n%s", want, data)
		}
		got, err := decodeTranscript(data)
		if err != nil {
			t.Fatalf("decodeTranscript: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("decoded %d entries; want the one tool call", len(got))
		}
		if !got[0].tool.Summary.quoted {
			t.Error("the replayed summary claims the block's own words; it is the tool's output, promoted")
		}
		if replayed, live := renderPlain(&transcript{entries: got}, 80), renderPlain(tr, 80); replayed != live {
			t.Errorf("the mark changed the paint:\n--- replayed ---\n%s\n--- live ---\n%s", replayed, live)
		}
	})

	// The promotion's OTHER half travels for the same reason the mark does, and this case is where it
	// shows: the typed stat is what the promote-guard swaps into the slot on a narrow row
	// (toolView.stat), and decode never re-runs the presenter that worded it. A record that came back
	// without it could no longer be demoted, so the same session would paint a different shape after
	// a resume at the very widths the guard exists for.
	t.Run("the promotion's typed stat comes back with it", func(t *testing.T) {
		const narrow = 40

		tr := &transcript{ws: newWorkspaceRoot("/home/me/proj")}
		tr.addToolCall(domain.ToolCall{ID: "c1", Tool: "terminal",
			Arguments: []byte(`{"command":"cat /home/me/proj/paths.txt"}`)}, "", runRef{})
		tr.addToolResult(domain.ToolResult{CallID: "c1", Content: "/home/me/proj/docs/plan.md\n"}, runRef{})

		data, err := encodeTranscript(tr)
		if err != nil {
			t.Fatalf("encodeTranscript: %v", err)
		}
		if want := `"stat":"exit 0"`; !strings.Contains(string(data), want) {
			t.Errorf("wire blob does not carry %s:\n%s", want, data)
		}
		got, err := decodeTranscript(data)
		if err != nil {
			t.Fatalf("decodeTranscript: %v", err)
		}
		if len(got) != 1 || got[0].tool.stat.spell() != "exit 0" {
			t.Fatalf("decoded %+v; want the one call with its typed stat", got)
		}
		// The fixture is only worth anything if the guard actually bites at this width.
		if live := renderPlain(tr, narrow); !strings.Contains(live, "exit 0") {
			t.Fatalf("fixture: the guard does not demote at width %d:\n%s", narrow, live)
		}
		if replayed, live := renderPlain(&transcript{entries: got}, narrow), renderPlain(tr, narrow); replayed != live {
			t.Errorf("the block changed shape across the round trip:\n--- replayed ---\n%s\n--- live ---\n%s",
				replayed, live)
		}
	})

	t.Run("a line the block worded stays unquoted and writes no member", func(t *testing.T) {
		card := toolView{
			Label: "Read", Verb: "reading", Target: "main.go", name: "read_file",
			Summary: namedSummary(detailLine{Text: "1 - 100"}),
		}
		card.sanitize()
		tr := &transcript{entries: []entry{{kind: entryToolCall, callID: "c1", done: true, tool: card}}}

		data, err := encodeTranscript(tr)
		if err != nil {
			t.Fatalf("encodeTranscript: %v", err)
		}
		if strings.Contains(string(data), "quoted") {
			t.Errorf("a summary in the block's own words wrote the member anyway: %s", data)
		}
		got, err := decodeTranscript(data)
		if err != nil {
			t.Fatalf("decodeTranscript: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("decoded %d entries; want the one tool call", len(got))
		}
		if got[0].tool.Summary.quoted {
			t.Error("the replayed summary claims to quote the tool; the block worded that line itself")
		}
		if replayed, live := renderPlain(&transcript{entries: got}, 80), renderPlain(tr, 80); replayed != live {
			t.Errorf("the mark changed the paint:\n--- replayed ---\n%s\n--- live ---\n%s", replayed, live)
		}
	})

	t.Run("a blob written before the member decodes unquoted and paints the same", func(t *testing.T) {
		// The same record twice, with and without the member, so the comparison isolates it: an old file
		// must decode as it always did, and the fact it lacks must be worth nothing to the painter.
		const head = `{"version":1,"entries":[{"kind":"toolCall","callID":"c1","done":true,"tool":{` +
			`"label":"Terminal","verb":"running","target":"cat paths.txt","name":"terminal",` +
			`"summary":{"text":"/home/me/proj/docs/plan.md"`
		const tail = `}}}]}`
		legacy, err := decodeTranscript([]byte(head + tail))
		if err != nil {
			t.Fatalf("decodeTranscript(legacy): %v", err)
		}
		marked, err := decodeTranscript([]byte(head + `,"quoted":true` + tail))
		if err != nil {
			t.Fatalf("decodeTranscript(marked): %v", err)
		}
		if len(legacy) != 1 || len(marked) != 1 {
			t.Fatalf("decoded %d and %d entries; want the one tool call each", len(legacy), len(marked))
		}
		if legacy[0].tool.Summary.quoted {
			t.Error("a record predating the member decoded as quoted; unquoted is what every such record meant")
		}
		if !marked[0].tool.Summary.quoted {
			t.Error("the member did not reach the decoded card")
		}
		before, after := renderPlain(&transcript{entries: legacy}, 80), renderPlain(&transcript{entries: marked}, 80)
		if before != after {
			t.Errorf("the mark changed the paint:\n--- without ---\n%s\n--- with ---\n%s", before, after)
		}
	})
}

// TestTranscriptCodecEmptyIsLegacy pins the legacy case: an empty or nil blob (a record written
// before the codec existed, or one with no scrollback) decodes to no entries and no error, so the
// caller degrades to resuming without a replay rather than reporting a fault.
func TestTranscriptCodecEmptyIsLegacy(t *testing.T) {
	t.Parallel()
	for _, data := range [][]byte{nil, {}} {
		got, err := decodeTranscript(data)
		if err != nil {
			t.Errorf("decodeTranscript(%q) error = %v; want nil", data, err)
		}
		if got != nil {
			t.Errorf("decodeTranscript(%q) = %+v; want nil entries", data, got)
		}
	}
}

// TestTranscriptCodecFutureVersionRejected proves a blob from a newer Apogee is refused with the
// layer's own sentinel — never silently misread.
func TestTranscriptCodecFutureVersionRejected(t *testing.T) {
	t.Parallel()
	data := []byte(`{"version":999,"entries":[{"kind":"note","text":"from the future"}]}`)
	got, err := decodeTranscript(data)
	if !errors.Is(err, session.ErrTranscriptVersion) {
		t.Fatalf("decodeTranscript error = %v; want session.ErrTranscriptVersion", err)
	}
	if got != nil {
		t.Errorf("decoded entries = %+v; want nil on a rejected version", got)
	}
}

// TestTranscriptCodecMalformedRejected proves a corrupt blob returns a decode error (which the
// caller degrades to a no-replay note), distinct from the empty legacy case.
func TestTranscriptCodecMalformedRejected(t *testing.T) {
	t.Parallel()
	if _, err := decodeTranscript([]byte("{not json")); err == nil {
		t.Error("decodeTranscript of malformed input returned no error")
	}
}

// TestTranscriptCodecUnknownKindSkipped proves a v1 blob carrying an unrecognised entry kind (a
// future variant, or the deliberately-excluded "startup") drops only that entry and keeps the
// rest — the same tolerate-unknown posture the live event fold takes.
func TestTranscriptCodecUnknownKindSkipped(t *testing.T) {
	t.Parallel()
	data := []byte(`{"version":1,"entries":[` +
		`{"kind":"startup","text":"chrome"},` +
		`{"kind":"future-variant","text":"who knows"},` +
		`{"kind":"note","text":"kept"}` +
		`]}`)
	got, err := decodeTranscript(data)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	if len(got) != 1 || got[0].kind != entryNote || got[0].text != "kept" {
		t.Errorf("decoded = %+v; want only the note entry", got)
	}
}

// TestTranscriptBridgeSkipsAKindTheCodecKeeps pins WHERE that skip lives now that the codec is
// Driver-neutral. [session.DecodeTranscript] hands an unrecognised kind back AS STORED — only a
// consumer knows whether it can do anything with a kind it does not paint — and this package's
// [decodeTranscript] is the consumer that drops it. The two halves are asserted over the one blob,
// so a future move of the rule into the codec (which would silently deny every other Driver the
// entry) fails here rather than in a Driver nobody was looking at.
func TestTranscriptBridgeSkipsAKindTheCodecKeeps(t *testing.T) {
	t.Parallel()
	data := []byte(`{"version":1,"entries":[` +
		`{"kind":"future-variant","text":"who knows"},` +
		`{"kind":"note","text":"kept"}` +
		`]}`)

	wire, wireErr := session.DecodeTranscript(data)
	entries, bridgeErr := decodeTranscript(data)

	if wireErr != nil {
		t.Fatalf("session.DecodeTranscript: %v", wireErr)
	}
	if len(wire) != 2 || wire[0].Kind != "future-variant" {
		t.Errorf("codec returned %+v; want both entries, the unknown kind as stored", wire)
	}
	if bridgeErr != nil {
		t.Fatalf("decodeTranscript: %v", bridgeErr)
	}
	if len(entries) != 1 || entries[0].kind != entryNote {
		t.Errorf("bridge returned %+v; want the unknown kind skipped", entries)
	}
}

// TestTranscriptCodecStripsEscapesOnDecode proves the defence-in-depth strip: ESC bytes salted
// through every text field of a stored blob are removed on the way back in, across the entry body,
// the tool card (label/target/summary/details/name), a firing block's stored answer
// and prompt, the presented view and a delegation head's model id — a disk file (which could have been hand-edited) can never
// smuggle a terminal escape into the transcript.
// The blob is built by encoding entries that carry real ESC bytes: json.Marshal writes them as the
// valid escaped form, exactly the on-disk shape a tampered file would hold.
func TestTranscriptCodecStripsEscapesOnDecode(t *testing.T) {
	t.Parallel()
	esc := string(rune(0x1b)) // the ESC control byte
	tr := &transcript{entries: []entry{
		{kind: entryUser, text: "hi" + esc + "there"},
		{
			kind: entryToolCall, callID: "c1",
			tool: toolView{
				Label: "Read" + esc + "File", Target: "ma" + esc + "in.go", name: "read" + esc + "_file",
				Summary: namedSummary(detailLine{Text: "1" + esc + "2"}),
				Details: newToolBody([]detailLine{{Kind: detailDiffAdded, Text: "+a" + esc + "dd"}}),
			},
		},
		{
			// A FINISHED firing block, so the stored summary is the one under test: an open one is
			// re-worded on decode (closeInterruptedFiring) and would prove nothing about the strip.
			kind: entrySchedule, callID: "sch-1", done: true,
			tool: toolView{
				Label: scheduleBlockLabel, Target: "nightly " + esc + "tidy",
				Summary: namedSummary(detailLine{Text: "the log is cle" + esc + "an"}),
				Details: newToolBody([]detailLine{{Text: "prompt: check " + esc + "the log"}}),
			},
		},
		{
			kind: entryPresented,
			presented: presentedView{
				Title: "Re" + esc + "port", Path: "out/" + esc + "r.md", Reason: "no" + esc + "pe",
				Method: domain.PresentShown,
			},
		},
		{
			// A delegation head wearing the model its child ran on: the id is the SERVER's own
			// text, so a hand-edited record could salt it exactly like any other field.
			kind: entryToolCall, callID: "s1",
			tool:     toolView{Label: "sub_agent", name: "sub_agent"},
			ctxModel: "qwen" + escOSC52,
			ctxUsed:  12000, ctxLimit: 32768,
		},
	}}

	data, err := encodeTranscript(tr)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	if strings.ContainsRune(string(data), 0x1b) {
		t.Fatal("encoded blob holds a raw ESC byte; json.Marshal should have escaped it")
	}
	got, err := decodeTranscript(data)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	for _, e := range got {
		assertNoESC(t, e.text)
		assertNoESC(t, e.tool.Label)
		assertNoESC(t, e.tool.Target)
		assertNoESC(t, e.tool.name)
		assertNoESC(t, e.tool.Summary.Text)
		for _, d := range e.tool.Details.all() {
			assertNoESC(t, d.Text)
		}
		assertNoESC(t, e.presented.Title)
		assertNoESC(t, e.presented.Path)
		assertNoESC(t, e.presented.Reason)
		assertNoESC(t, e.ctxModel)
	}
	// The strip drops the C0 control characters and DEL (keeping \n and \t) and passes every
	// printable rune through — here that is the ESC alone, with the text around it intact.
	if got[0].text != "hithere" {
		t.Errorf("stripped user text = %q; want %q", got[0].text, "hithere")
	}
}

// TestTranscriptCodecRoundTripsSkillTokenSpans proves the record keeps WHERE a message invoked its
// skills — which is now ALL it keeps about them: a resumed session paints the same tokens the live
// one did. Both sent kinds carry them (a flushed send and a delivered interjection), one span per
// occurrence.
//
// It also pins the three degrades the plan accepted instead of a migration: an entry stored before
// the member existed comes back with no spans and paints plain, an entry carrying the retired
// "skills" member decodes as the plain send it now paints as (the member is ignored, not refused),
// and a span that no longer lands in the text it arrives with is dropped rather than slicing out
// of range.
func TestTranscriptCodecRoundTripsSkillTokenSpans(t *testing.T) {
	t.Parallel()
	known := knownSkills("refocus")
	sent := parseInput("/refocus and later /refocus again", known)
	remark := parseInput("no — /refocus first", known)

	tr := &transcript{}
	tr.addUser(sent.text, sent.skillSpans)
	tr.addInterjected(remark.text, remark.skillSpans)

	data, err := encodeTranscript(tr)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	// The member and its field names are part of the record now, so pin them (the leading token of
	// the send is 8 bytes at offset 0).
	if want := `"skillSpans":[{"start":0,"end":8}`; !strings.Contains(string(data), want) {
		t.Errorf("wire blob does not carry %s:\n%s", want, data)
	}
	got, err := decodeTranscript(data)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("decoded %d entries; want the send and the interjection", len(got))
	}
	for i, want := range [][]skillSpan{sent.skillSpans, remark.skillSpans} {
		if !reflect.DeepEqual(got[i].skillSpans, want) {
			t.Errorf("entry %d spans = %v, want %v", i, got[i].skillSpans, want)
		}
		for _, sp := range got[i].skillSpans {
			if tok := got[i].text[sp.start:sp.end]; tok != "/refocus" {
				t.Errorf("entry %d span %v locates %q after the round trip; want the token", i, sp, tok)
			}
		}
	}

	legacy := []byte(`{"version":1,"entries":[{"kind":"user","text":"/refocus please","skills":["Refocus"]}]}`)
	old, err := decodeTranscript(legacy)
	if err != nil {
		t.Fatalf("decodeTranscript(legacy): %v", err)
	}
	// The blob predates spans AND carries the retired display-name member: it must decode as the
	// plain send it now paints as, the unknown member left on the disk it came from.
	if want := []entry{{kind: entryUser, text: "/refocus please"}}; !reflect.DeepEqual(old, want) {
		t.Errorf("a legacy chip-carrying entry decoded as %+v; want the plain send %+v", old, want)
	}

	corrupt := []byte(`{"version":1,"entries":[{"kind":"user","text":"hi","skillSpans":[{"start":0,"end":900}]}]}`)
	bad, err := decodeTranscript(corrupt)
	if err != nil {
		t.Fatalf("decodeTranscript(corrupt): %v", err)
	}
	if bad[0].skillSpans != nil {
		t.Errorf("a span past the end of its text survived decode: %v", bad[0].skillSpans)
	}
}

func assertNoESC(t *testing.T, s string) {
	t.Helper()
	if strings.ContainsRune(s, 0x1b) {
		t.Errorf("text still carries an ESC byte: %q", s)
	}
}

// TestTranscriptCodecRoundTripsASubAgentFill proves the fill a sub-agent run wears survives the
// record: both halves of the pair reach the wire under their own members and come back on the head
// that delegated, so a reopened session still says how much of its window that delegate had filled
// — the reading as it stood when the run reported, not one a later window rebind could rewrite.
//
// The pair is ADDITIVE within session.TranscriptVersion and needs no bump, on the envelope's own
// rule: a run that never reported writes neither member, and a blob written before they existed
// decodes to the zero pair — which is the nothing-to-say case the summary line already hides, so
// no migration.
func TestTranscriptCodecRoundTripsASubAgentFill(t *testing.T) {
	t.Parallel()
	const window = 32768

	t.Run("a reported run carries its fill through the record", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		subAgentUsage(tr, 1, 12000, window)
		subAgentReport(tr, "s1", "tests read", 0)

		data, err := encodeTranscript(tr)
		if err != nil {
			t.Fatalf("encodeTranscript: %v", err)
		}
		// The members are part of the record now, so pin their names and their spelling as integers.
		if want := `"ctxUsed":12000,"ctxLimit":32768`; !strings.Contains(string(data), want) {
			t.Errorf("wire blob does not carry %s:\n%s", want, data)
		}
		got, err := decodeTranscript(data)
		if err != nil {
			t.Fatalf("decodeTranscript: %v", err)
		}
		if len(got) != 1 || got[0].kind != entryToolCall {
			t.Fatalf("decoded %+v; want the one sub-agent run head", got)
		}
		if got[0].ctxUsed != 12000 || got[0].ctxLimit != window {
			t.Errorf("replayed fill = %d/%d, want 12000/%d", got[0].ctxUsed, got[0].ctxLimit, window)
		}
	})

	t.Run("a routed run keeps the window it actually filled", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		// The child ran on the Sub-agent server (ADR 0045): a small window inside a big session.
		subAgentUsageIn(tr, 1, 7000, 131072, 8192)
		subAgentReport(tr, "s1", "tests read", 0)

		data, err := encodeTranscript(tr)
		if err != nil {
			t.Fatalf("encodeTranscript: %v", err)
		}
		got, err := decodeTranscript(data)
		if err != nil {
			t.Fatalf("decodeTranscript: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("decoded %d entries; want the one run head", len(got))
		}
		if got[0].ctxUsed != 7000 || got[0].ctxLimit != 8192 {
			t.Errorf("replayed fill = %d/%d, want 7000/8192 — a resume repaints the target's window,"+
				" not the session's", got[0].ctxUsed, got[0].ctxLimit)
		}
	})

	t.Run("a run that never reported writes neither member", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		subAgentReport(tr, "s1", "tests read", 0)

		data, err := encodeTranscript(tr)
		if err != nil {
			t.Fatalf("encodeTranscript: %v", err)
		}
		if strings.Contains(string(data), "ctx") {
			t.Errorf("an empty fill reached the wire: %s", data)
		}
	})

	t.Run("a blob written before the members decodes to the zero pair", func(t *testing.T) {
		legacy := []byte(`{"version":1,"entries":[{"kind":"toolCall","callID":"s1","done":true,` +
			`"tool":{"label":"Sub-Agent","name":"sub_agent","summary":{"text":"survey the tests"}}}]}`)
		got, err := decodeTranscript(legacy)
		if err != nil {
			t.Fatalf("decodeTranscript(legacy): %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("decoded %d entries; want the one run head", len(got))
		}
		if got[0].ctxUsed != 0 || got[0].ctxLimit != 0 {
			t.Errorf("a blob predating the members decoded a fill of %d/%d; want 0/0 so the cell hides",
				got[0].ctxUsed, got[0].ctxLimit)
		}
	})
}

// TestTranscriptCodecPersistsANamedDelegationAsItsTarget pins how a named delegation survives a
// resume — and how it deliberately does not. What a saved session keeps is the FINISHED header
// text, and on a named call that text already IS the name (subAgentTarget), so the record needs no
// member of its own: Target carries it through encode/decode with no wire change at all. The
// presenter's agentName is the live half — the status line's lookup, and the status line does not
// outlive the process — so it is absent from the blob and empty after a decode, which is the same
// nothing an unnamed delegation says.
//
// The member census is the guard on that claim. Display state added to toolView can always be got
// onto the screen after a resume by widening the wire; the two field lists below are what makes
// such a widening a decision someone took rather than a silent format change (both structs are
// inside session.TranscriptVersion 1, so every member is forever). Task is such a decision — a delegation's
// retained prompt, which the run's expanded body is built from and which no other member could
// carry — and it stands in the list beside the members that reached it the same way. So is
// UsageCachedPromptTokens, added by plan `2026-08-28 - 02` item 10 so a resumed delegate keeps the
// cache share its row reports (usageRow); the plan document is the recorded decision behind it.
func TestTranscriptCodecPersistsANamedDelegationAsItsTarget(t *testing.T) {
	t.Parallel()

	t.Run("a named head replays with the name as its target", func(t *testing.T) {
		tr := &transcript{}
		tr.apply(domain.ToolCallEvent{
			Call: domain.ToolCall{ID: "s1", Tool: "sub_agent",
				Arguments: []byte(`{"name":"test-surveyor","task":"Survey the tests."}`)},
		})
		subAgentReport(tr, "s1", "tests read", 0)
		if got := tr.entries[0].tool.agentName; got != "test-surveyor" {
			t.Fatalf("live head agentName = %q; want the delegation's name before anything is encoded", got)
		}

		data, err := encodeTranscript(tr)
		if err != nil {
			t.Fatalf("encodeTranscript: %v", err)
		}
		if want := `"target":"test-surveyor"`; !strings.Contains(string(data), want) {
			t.Errorf("wire blob does not carry %s:\n%s", want, data)
		}
		if strings.Contains(string(data), "agentName") {
			t.Errorf("the live-only name reached the wire:\n%s", data)
		}

		got, err := decodeTranscript(data)
		if err != nil {
			t.Fatalf("decodeTranscript: %v", err)
		}
		if len(got) != 1 || got[0].kind != entryToolCall {
			t.Fatalf("decoded %+v; want the one sub-agent run head", got)
		}
		if got[0].tool.Target != "test-surveyor" {
			t.Errorf("replayed target = %q; want the name the header was painted with", got[0].tool.Target)
		}
		if got[0].tool.agentName != "" {
			t.Errorf("replayed agentName = %q; the live half is not persisted", got[0].tool.agentName)
		}
	})

	t.Run("the wire structs carry exactly the members that were decided on", func(t *testing.T) {
		fields := func(v any) []string {
			typ := reflect.TypeOf(v)
			out := make([]string, 0, typ.NumField())
			for i := range typ.NumField() {
				out = append(out, typ.Field(i).Name)
			}
			return out
		}
		wantEntry := []string{
			"Kind", "Text", "Depth", "CallID", "SpawnCallID", "Done",
			"CtxUsed", "CtxLimit", "CtxModel",
			"UsageCalls", "UsagePromptTokens", "UsageCachedPromptTokens", "UsageCompletionTokens",
			"UsageTotalTokens",
			"SkillSpans", "Tool", "Presented",
		}
		if got := fields(session.Entry{}); !slices.Equal(got, wantEntry) {
			t.Errorf("session.Entry members = %v, want %v — widening the wire needs its own decision", got, wantEntry)
		}
		wantTool := []string{
			"Label", "Verb", "Target", "Name", "Solo", "Stat", "StatValue", "Task", "Summary",
			"Details", "Regions", "RegionFiles", "Args",
		}
		if got := fields(session.ToolView{}); !slices.Equal(got, wantTool) {
			t.Errorf("session.ToolView members = %v, want %v — widening the wire needs its own decision", got, wantTool)
		}
	})
}

// TestTranscriptCodecRoundTripsTheDelegatedPrompt pins the half of a delegation the record cannot
// re-derive. A run's expanded span opens with the prompt the model wrote (toolView.task), and that
// text lives nowhere else the block can paint FROM: the header keeps one clipped line of it, and
// the arguments it came from ride the record as a stored value nothing paints (session.ToolView.Args),
// so a blob that dropped this member would replay the run without its opening block — the
// scrollback changing shape across a restart.
//
// The prompt travels VERBATIM, newlines and all, because the block renders it as markdown at paint
// time against a width the codec cannot see. A blob written before the member decodes to no prompt,
// which is the additive rule every member of this struct is added under.
func TestTranscriptCodecRoundTripsTheDelegatedPrompt(t *testing.T) {
	t.Parallel()

	const task = "Survey the tests.\n\n- read `render_test.go`\n- report the gaps"

	t.Run("a head replays with the prompt it was given", func(t *testing.T) {
		t.Parallel()
		args, err := json.Marshal(map[string]any{"name": "test-surveyor", "task": task})
		if err != nil {
			t.Fatalf("marshal args: %v", err)
		}
		tr := &transcript{}
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "s1", Tool: subAgentToolName, Arguments: args}})
		subAgentReport(tr, "s1", "tests read", 0)

		data, err := encodeTranscript(tr)
		if err != nil {
			t.Fatalf("encodeTranscript: %v", err)
		}
		got, err := decodeTranscript(data)
		if err != nil {
			t.Fatalf("decodeTranscript: %v", err)
		}

		if len(got) != 1 || got[0].kind != entryToolCall {
			t.Fatalf("decoded %+v; want the one sub-agent run head", got)
		}
		if got[0].tool.task != task {
			t.Errorf("replayed prompt = %q, want the delegated task verbatim %q", got[0].tool.task, task)
		}
	})

	t.Run("only a delegation spends wire on a prompt", func(t *testing.T) {
		t.Parallel()
		tr := &transcript{}
		runCall(tr, "c1", "go test ./...", "ok", 0)

		data, err := encodeTranscript(tr)
		if err != nil {
			t.Fatalf("encodeTranscript: %v", err)
		}

		if strings.Contains(string(data), `"task"`) {
			t.Errorf("a terminal call's blob carries a task member:\n%s", data)
		}
	})

	t.Run("a blob written before the member decodes to no prompt", func(t *testing.T) {
		t.Parallel()
		legacy := []byte(`{"version":1,"entries":[{"kind":"toolCall","callID":"s1","done":true,` +
			`"tool":{"label":"Sub-Agent","verb":"delegating","target":"test-surveyor","name":"sub_agent",` +
			`"solo":true,"summary":{"text":"done"}}}]}`)

		got, err := decodeTranscript(legacy)
		if err != nil {
			t.Fatalf("decodeTranscript(legacy): %v", err)
		}

		if len(got) != 1 {
			t.Fatalf("decoded %d entries; want the one run head", len(got))
		}
		if got[0].tool.task != "" {
			t.Errorf("a blob predating the member decoded a prompt of %q; want none", got[0].tool.task)
		}
	})
}

// TestTranscriptCodecRoundTripsTheSpawningCallID pins the run identity a delegated entry keeps
// across a resume (ADR 0039): the id of the sub_agent call that spawned the agent whose event
// folded into it, which is what tells two concurrent children's blocks apart once several run at
// once — depth cannot, because siblings share it. The member is additive within session.TranscriptVersion,
// so it must be absent from a top-level entry's wire form and must decode to "" from a blob
// written before it existed: such a record had exactly one run in flight at a time, which is what
// an empty identity already means.
func TestTranscriptCodecRoundTripsTheSpawningCallID(t *testing.T) {
	t.Parallel()

	t.Run("a delegated entry carries its spawning call through the record", func(t *testing.T) {
		tr := &transcript{entries: []entry{
			{kind: entryUser, text: "delegate it"},
			{kind: entryAssistant, text: "first child answer", depth: 1, spawnCallID: "c1"},
			{kind: entryAssistant, text: "second child answer", depth: 1, spawnCallID: "c2"},
			{kind: entryAssistant, text: "both done"},
		}}

		data, err := encodeTranscript(tr)
		if err != nil {
			t.Fatalf("encodeTranscript: %v", err)
		}
		// The member is part of the record now: pin its name and the fact the top-level entries
		// write it not at all (two entries, two occurrences).
		if got := strings.Count(string(data), `"spawnCallID"`); got != 2 {
			t.Errorf("wire blob carries %d spawnCallID members, want 2 (the delegated entries only):\n%s", got, data)
		}

		got, err := decodeTranscript(data)
		if err != nil {
			t.Fatalf("decodeTranscript: %v", err)
		}
		if len(got) != len(tr.entries) {
			t.Fatalf("decoded %d entries, want %d", len(got), len(tr.entries))
		}
		for i := range tr.entries {
			if got[i].spawnCallID != tr.entries[i].spawnCallID {
				t.Errorf("entry %d replayed spawnCallID %q, want %q",
					i, got[i].spawnCallID, tr.entries[i].spawnCallID)
			}
		}
	})

	t.Run("a message addressed to a child replays into the same run", func(t *testing.T) {
		// The delivered block is an entryUser at depth (transcript.addUserAt), so BOTH members have
		// to survive for it: without the depth it replays railed at the top level, and without the
		// run it regroups outside its head's span — a message the human sent to one delegate coming
		// back beside the parent's own prompts.
		tr := &transcript{entries: []entry{
			{kind: entryUser, text: "delegate it"},
			{kind: entryUser, text: "check the docs too", depth: 1, spawnCallID: "s1"},
		}}

		data, err := encodeTranscript(tr)
		if err != nil {
			t.Fatalf("encodeTranscript: %v", err)
		}
		got, err := decodeTranscript(data)
		if err != nil {
			t.Fatalf("decodeTranscript: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("decoded %d entries, want the prompt and the delegated message", len(got))
		}
		if got[1].kind != entryUser || got[1].depth != 1 || got[1].spawnCallID != "s1" {
			t.Errorf("the delegated message replayed as %v at depth %d in run %q, want an entryUser at 1 in \"s1\"",
				got[1].kind, got[1].depth, got[1].spawnCallID)
		}
		if got[0].depth != 0 || got[0].spawnCallID != "" {
			t.Errorf("the top-level prompt replayed at depth %d in run %q, want 0 and no run",
				got[0].depth, got[0].spawnCallID)
		}
	})

	t.Run("a blob written before the member decodes to no run identity", func(t *testing.T) {
		legacy := []byte(`{"version":1,"entries":[{"kind":"assistant","text":"child answer","depth":1}]}`)
		got, err := decodeTranscript(legacy)
		if err != nil {
			t.Fatalf("decodeTranscript(legacy): %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("decoded %d entries, want the one nested block", len(got))
		}
		if got[0].spawnCallID != "" {
			t.Errorf("a blob predating the member decoded spawnCallID %q, want empty", got[0].spawnCallID)
		}
		if got[0].depth != 1 || got[0].text != "child answer" {
			t.Errorf("the rest of the legacy entry decoded as %+v, want the nested block unchanged", got[0])
		}
	})
}

// TestTranscriptCodecRoundTripsRecordedRegions pins the half of a diff-bodied block its Details
// cannot bring back. The stacked rows travel as they always did, so an older build replays the body
// unchanged (ADR 0052 §5) — but the SPLIT reading is composed at paint time, from the regions
// themselves, and a record that dropped them would replay as stacked rows a block the live session
// had split: the scrollback changing shape across a restart, which is what the round trip exists to
// prevent.
//
// The file names ride with them for the multi-file case: one name per region, in the regions' own
// order, which is what either reading regroups into per-file sections (regionFileSections). Both
// members are ADDITIVE, so the two halves that matter are asserted beside the round trip — a record
// written before them decodes with no regions and still paints the rows it was written as, and the
// region lines arrive escape-stripped like every other text off disk.
func TestTranscriptCodecRoundTripsRecordedRegions(t *testing.T) {
	t.Parallel()

	t.Run("the regions and the file each was cut from survive the round trip", func(t *testing.T) {
		tv := toolView{
			Label: "Git Diff", Verb: "diffing", Target: "main..HEAD", name: "git_diff_range",
			Summary: namedSummary(detailLine{Text: "+2 −1"}),
		}
		tv.showFileRegions([]diffFileRegions{
			{File: "a.go", Regions: []domain.EditRegion{{
				BeforeStart: 10, AfterStart: 10,
				Leading:  []string{"func main() {"},
				Removed:  []string{"\told()"},
				Inserted: []string{"\tnew()"},
				Trailing: []string{"}"},
			}}},
			{File: "b.go", Regions: []domain.EditRegion{{
				BeforeStart: 1, AfterStart: 1,
				Inserted: []string{"package b"},
			}}},
		})
		tv.sanitize()

		tr := &transcript{entries: []entry{{kind: entryToolCall, callID: "d1", done: true, tool: tv}}}
		data, err := encodeTranscript(tr)
		if err != nil {
			t.Fatalf("encodeTranscript: %v", err)
		}
		if want := `"regionFiles":["a.go","b.go"]`; !strings.Contains(string(data), want) {
			t.Errorf("wire blob does not carry %s:\n%s", want, data)
		}
		if want := `"beforeStart":10`; !strings.Contains(string(data), want) {
			t.Errorf("wire blob does not carry the region's before-file line (%s):\n%s", want, data)
		}

		got, err := decodeTranscript(data)
		if err != nil {
			t.Fatalf("decodeTranscript: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("decoded %d entries; want the one diff card", len(got))
		}
		if !reflect.DeepEqual(got[0].tool.Regions, tv.Regions) {
			t.Errorf("replayed regions:\n got = %+v\nwant = %+v", got[0].tool.Regions, tv.Regions)
		}
		if !slices.Equal(got[0].tool.RegionFiles, tv.RegionFiles) {
			t.Errorf("replayed region files = %v; want %v — the split reading loses its headers without them",
				got[0].tool.RegionFiles, tv.RegionFiles)
		}
		if got[0].tool.Details.len() != tv.Details.len() {
			t.Errorf("replayed body = %d rows; want %d — Details keeps carrying the stacked reading",
				got[0].tool.Details.len(), tv.Details.len())
		}
	})

	t.Run("a record written before the members decodes with no regions and keeps its rows", func(t *testing.T) {
		data := []byte(`{"version":1,"entries":[` +
			`{"kind":"toolCall","callID":"c1","done":true,"tool":{"label":"Edit","verb":"editing",` +
			`"target":"main.go","name":"edit_existing_file","summary":{"text":"+1 −1"},` +
			`"details":[{"kind":2,"text":"- old()"},{"kind":1,"text":"+ new()"}]}}` +
			`]}`)
		got, err := decodeTranscript(data)
		if err != nil {
			t.Fatalf("decodeTranscript: %v", err)
		}
		if got[0].tool.Regions != nil || got[0].tool.RegionFiles != nil {
			t.Errorf("legacy record decoded with regions %+v / files %v; want neither",
				got[0].tool.Regions, got[0].tool.RegionFiles)
		}
		if got[0].tool.Details.len() != 2 {
			t.Errorf("legacy body = %d rows; want the 2 it was written with — a region-less record still paints stacked",
				got[0].tool.Details.len())
		}
	})

	t.Run("region lines and file names are escape-stripped on the way in", func(t *testing.T) {
		data := []byte(`{"version":1,"entries":[` +
			`{"kind":"toolCall","callID":"c1","done":true,"tool":{"label":"Git Diff","verb":"diffing",` +
			`"target":"main..HEAD","name":"git_diff_range","summary":{"text":"+1 −1"},` +
			`"regions":[{"beforeStart":1,"afterStart":1,` +
			`"leading":["\u001b[31mctx"],"removed":["\u001b[1mold()"],"inserted":["new()\u0007"],` +
			`"trailing":["\u001b[0m}"]}],` +
			`"regionFiles":["\u001b[7ma.go"]}}` +
			`]}`)
		got, err := decodeTranscript(data)
		if err != nil {
			t.Fatalf("decodeTranscript: %v", err)
		}
		if len(got[0].tool.Regions) != 1 {
			t.Fatalf("decoded %d regions; want 1", len(got[0].tool.Regions))
		}
		region := got[0].tool.Regions[0]
		for _, line := range slices.Concat(region.Leading, region.Removed, region.Inserted, region.Trailing) {
			assertNoESC(t, line)
		}
		if got := region.Inserted[0]; strings.ContainsRune(got, '\a') {
			t.Errorf("inserted line = %q; a control byte survived the strip", got)
		}
		assertNoESC(t, got[0].tool.RegionFiles[0])
	})
}

// TestTranscriptCodecGoldenV1 pins the exact v1 wire shape: field names, the string kind enum,
// the nested tool card with its name and coloured details, the presented Method as a domain
// string, and omitempty behaviour. A change to any of these — a renamed field, a reordered kind
// constant leaking into the wire, a lost name — breaks this test, which is the point: old files
// must keep decoding.
func TestTranscriptCodecGoldenV1(t *testing.T) {
	t.Parallel()
	tr := &transcript{}
	tr.addStartup(startupView{Logo: "X", Host: "h", Model: "m"}) // excluded from the wire
	tr.entries = append(tr.entries,
		entry{kind: entryUser, text: "hi"},
		entry{kind: entryAssistant, text: "hello", depth: 1},
		entry{
			kind: entryToolCall, callID: "c1", done: true,
			tool: toolView{
				Label: "Read", Verb: "reading", Target: "main.go", name: "read_file",
				Summary: namedSummary(detailLine{Text: "1 - 10"}),
				Details: newToolBody([]detailLine{{Kind: detailDiffAdded, Text: "+x"}}),
			},
		},
		entry{kind: entryPresented, presented: presentedView{Title: "Report", Path: "out/report.md", Method: domain.PresentShown}},
		entry{kind: entryNote, text: "cancelled"},
	)
	tr.pending = bufOf("typing") // excluded from the wire
	tr.streaming = true

	data, err := encodeTranscript(tr)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	const golden = `{"version":1,"entries":[` +
		`{"kind":"user","text":"hi"},` +
		`{"kind":"assistant","text":"hello","depth":1},` +
		`{"kind":"toolCall","callID":"c1","done":true,"tool":{"label":"Read","verb":"reading","target":"main.go","name":"read_file","summary":{"text":"1 - 10"},"details":[{"kind":1,"text":"+x"}]}},` +
		`{"kind":"presented","presented":{"title":"Report","path":"out/report.md","method":"shown"}},` +
		`{"kind":"note","text":"cancelled"}` +
		`]}`
	if string(data) != golden {
		t.Errorf("golden wire shape mismatch:\n got = %s\nwant = %s", data, golden)
	}
}

// TestTranscriptCodecReplaysADelegatedPresentationRailed proves a child's document reopens where it
// was drawn. The entry is committed at the presenting agent's own depth and under its own run
// (transcript.addPresented), and both facts ride the wire on the GENERIC members every entry uses
// (session.Entry.Depth / session.Entry.SpawnCallID) — no presented-specific codec work — so a resumed
// scrollback rails the block exactly as the live one did instead of flattening a delegate's
// presentation onto the human's own conversation.
func TestTranscriptCodecReplaysADelegatedPresentationRailed(t *testing.T) {
	t.Parallel()
	tr := &transcript{}
	tr.addPresented(presentedMsg{
		Title:  "Child report",
		Path:   "out/child.md",
		Method: domain.PresentShown,
		Depth:  1, SpawnCallID: "s1",
	})

	data, err := encodeTranscript(tr)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	got, err := decodeTranscript(data)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("replayed %d entries, want the one presentation", len(got))
	}
	if got[0].kind != entryPresented || got[0].depth != 1 || got[0].spawnCallID != "s1" {
		t.Fatalf("replayed %v at depth %d under run %q; want a presentation at depth 1 under s1",
			got[0].kind, got[0].depth, got[0].spawnCallID)
	}
	if out := plainRender(&transcript{entries: got}); !strings.Contains(out, "│ ▤ Child report") {
		t.Errorf("the replayed presentation is not railed:\n%s", out)
	}
}

// TestTranscriptCodecRoundTripsASubAgentsTotals proves the cumulative accounting a run head wears
// survives the record: the five members reach the wire under their own keys and come back on the
// head that delegated, so a reopened session still reports what each delegate spent — a fact the
// fill beside them cannot give, since it only ever said how full the child's window stood.
//
// The cache share is one of the five for the same reason: the pane draws its column for the WHOLE
// report (usageRows), so a delegate that came back without its share would sit under a header the
// main agent's row still fills — the resumed session answering the same question two ways.
//
// The members are ADDITIVE within session.TranscriptVersion on the session.Entry rule: a run that reported no
// accounting writes none of them, and a blob written before they existed decodes to zero totals —
// the same nothing-to-report state a pre-feature session reopens in, so there is no migration.
func TestTranscriptCodecRoundTripsASubAgentsTotals(t *testing.T) {
	t.Parallel()
	const window = 32768

	t.Run("a reported run carries its totals through the record", func(t *testing.T) {
		t.Parallel()
		totals := usageTotals{
			Calls: 2, PromptTokens: 11000, CachedPromptTokens: 300, CompletionTokens: 1000, TotalTokens: 12000,
		}
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		tr.applyUsage(childUsage("s1", 1, 12000, totals), window, "")
		subAgentReport(tr, "s1", "tests read", 0)

		data, err := encodeTranscript(tr)
		if err != nil {
			t.Fatalf("encodeTranscript: %v", err)
		}
		// The members are part of the record now, so pin their names, their order and their spelling
		// as integers — the share sits inside the prompt count it qualifies, as it does on the pane.
		want := `"usageCalls":2,"usagePromptTokens":11000,"usageCachedPromptTokens":300,` +
			`"usageCompletionTokens":1000,"usageTotalTokens":12000`
		if !strings.Contains(string(data), want) {
			t.Errorf("wire blob does not carry %s:\n%s", want, data)
		}
		got, err := decodeTranscript(data)
		if err != nil {
			t.Fatalf("decodeTranscript: %v", err)
		}
		if len(got) != 1 || got[0].kind != entryToolCall {
			t.Fatalf("decoded %+v; want the one sub-agent run head", got)
		}
		if got[0].usage != totals {
			t.Errorf("replayed totals = %+v, want %+v", got[0].usage, totals)
		}
	})

	t.Run("a run that reported no accounting writes no member", func(t *testing.T) {
		t.Parallel()
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		subAgentUsage(tr, 1, 12000, window) // a fill, from a stream that carried no totals
		subAgentReport(tr, "s1", "tests read", 0)

		data, err := encodeTranscript(tr)
		if err != nil {
			t.Fatalf("encodeTranscript: %v", err)
		}
		if strings.Contains(string(data), "usage") {
			t.Errorf("an empty accounting reached the wire: %s", data)
		}
	})

	t.Run("a blob written before the cache share decodes with none", func(t *testing.T) {
		t.Parallel()
		// A record from a build that already kept the other four: the share is the only member absent,
		// so its zero has to come from the decode rather than from an empty accounting.
		older := []byte(`{"version":1,"entries":[{"kind":"toolCall","callID":"s1","done":true,` +
			`"usageCalls":2,"usagePromptTokens":11000,"usageCompletionTokens":1000,"usageTotalTokens":12000,` +
			`"tool":{"label":"Sub-Agent","name":"sub_agent","summary":{"text":"survey the tests"}}}]}`)
		got, err := decodeTranscript(older)
		if err != nil {
			t.Fatalf("decodeTranscript(older): %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("decoded %d entries; want the one run head", len(got))
		}
		want := usageTotals{Calls: 2, PromptTokens: 11000, CompletionTokens: 1000, TotalTokens: 12000}
		if got[0].usage != want {
			t.Errorf("a blob predating the share decoded totals %+v, want %+v — no share, the rest intact",
				got[0].usage, want)
		}
	})

	t.Run("a blob written before the members decodes to zero totals", func(t *testing.T) {
		t.Parallel()
		legacy := []byte(`{"version":1,"entries":[{"kind":"toolCall","callID":"s1","done":true,` +
			`"ctxUsed":12000,"ctxLimit":32768,` +
			`"tool":{"label":"Sub-Agent","name":"sub_agent","summary":{"text":"survey the tests"}}}]}`)
		got, err := decodeTranscript(legacy)
		if err != nil {
			t.Fatalf("decodeTranscript(legacy): %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("decoded %d entries; want the one run head", len(got))
		}
		if got[0].usage != (usageTotals{}) {
			t.Errorf("a blob predating the members decoded totals %+v; want zeros", got[0].usage)
		}
	})
}

// TestTranscriptCodecRoundTripsARoutedSubAgentsModel proves the model a routed delegation ran on
// survives the record and keeps painting after a resume (ADR 0045): it reaches the wire under its
// own key, comes back on the head that delegated, and is still the last cell of that run's line.
//
// It is ADDITIVE within session.TranscriptVersion on the session.Entry rule: an unrouted run — one that ran on
// the session's own model — writes no member at all, and a blob written before routing existed
// decodes to no model, which is exactly what such a session was.
func TestTranscriptCodecRoundTripsARoutedSubAgentsModel(t *testing.T) {
	t.Parallel()
	const window = 32768

	t.Run("a routed run carries its model through the record and repaints it", func(t *testing.T) {
		t.Parallel()
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		readCall(tr, "c1", "a.go", 1, 5, 1)
		subAgentUsageOn(tr, 1, 12000, window, "qwen3-4b", "gpt-oss-20b")
		subAgentReport(tr, "s1", "tests read", 0)

		data, err := encodeTranscript(tr)
		if err != nil {
			t.Fatalf("encodeTranscript: %v", err)
		}
		if want := `"ctxModel":"qwen3-4b"`; !strings.Contains(string(data), want) {
			t.Errorf("wire blob does not carry %s:\n%s", want, data)
		}
		got, err := decodeTranscript(data)
		if err != nil {
			t.Fatalf("decodeTranscript: %v", err)
		}
		replayed := &transcript{entries: got}
		want := groupMemberLine("  ┕ survey the tests ✓ ⋯ 1 tool call · 12k/32k · tests read · qwen3-4b")
		if branch := strings.Split(renderPlain(replayed, 80), "\n")[1]; branch != want {
			t.Errorf("resumed summary line = %q; want %q", branch, want)
		}
	})

	t.Run("an unrouted run writes no member", func(t *testing.T) {
		t.Parallel()
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		subAgentUsageOn(tr, 1, 12000, window, "gpt-oss-20b", "gpt-oss-20b")
		subAgentReport(tr, "s1", "tests read", 0)

		data, err := encodeTranscript(tr)
		if err != nil {
			t.Fatalf("encodeTranscript: %v", err)
		}
		if strings.Contains(string(data), "ctxModel") {
			t.Errorf("a same-model run reached the wire with a model: %s", data)
		}
	})

	t.Run("a blob written before the member decodes to no model", func(t *testing.T) {
		t.Parallel()
		legacy := []byte(`{"version":1,"entries":[{"kind":"toolCall","callID":"s1","done":true,` +
			`"ctxUsed":12000,"ctxLimit":32768,` +
			`"tool":{"label":"Sub-Agent","name":"sub_agent","summary":{"text":"survey the tests"}}}]}`)
		got, err := decodeTranscript(legacy)
		if err != nil {
			t.Fatalf("decodeTranscript(legacy): %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("decoded %d entries; want the one run head", len(got))
		}
		if got[0].ctxModel != "" {
			t.Errorf("a blob predating the member decoded model %q; want none", got[0].ctxModel)
		}
	})
}

// TestTranscriptCodecGoldenToolArgumentsV1 is the golden half of the arguments member: the exact
// bytes a card with stored arguments writes, beside TestTranscriptCodecGoldenV1's proof that a card
// without them writes what it always did. Two facts are pinned here and nowhere else — the member's
// NAME and its place in the tool object (after every field the card SHOWS, which is what lets an
// older build skip it), and the SHAPE of the value: the model wrote its keys unsorted, and what the
// record keeps is the key-sorted, compact form wireArgs settled on, so the bytes on disk do not
// shift with the order a model happened to spell its call in.
func TestTranscriptCodecGoldenToolArgumentsV1(t *testing.T) {
	t.Parallel()
	tr := &transcript{entries: []entry{{
		kind: entryToolCall, callID: "c1", done: true,
		tool: toolView{
			Label: "Grep", Verb: "searching", Target: "KeyMsg", name: "grep",
			Summary:  namedSummary(detailLine{Text: "3 matches"}),
			argsWire: wireArgs("grep", json.RawMessage(`{"pattern":"KeyMsg","path":"internal/tui/model.go"}`)),
		},
	}}}

	data, err := encodeTranscript(tr)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	const golden = `{"version":1,"entries":[` +
		`{"kind":"toolCall","callID":"c1","done":true,"tool":{"label":"Grep","verb":"searching",` +
		`"target":"KeyMsg","name":"grep","summary":{"text":"3 matches"},` +
		`"args":{"path":"internal/tui/model.go","pattern":"KeyMsg"}}}` +
		`]}`
	if string(data) != golden {
		t.Errorf("golden wire shape mismatch:\n got = %s\nwant = %s", data, golden)
	}
}

// TestTranscriptCodecRoundTripsToolArguments proves the record keeps what each call ASKED, for every
// card the presenter builds rather than for the ones a presenter happened to retain a parsed map
// for. The three cases are the three that could each be the hole: a registered call at the top
// level, a DELEGATE's call one depth down (the run nobody watched go by — the whole reason the
// member exists), and a tool no registry knows, which reaches the wire through presentToolCall's
// other exit. The fourth case is the additive rule: a blob written before the member decodes with
// none rather than failing.
func TestTranscriptCodecRoundTripsToolArguments(t *testing.T) {
	t.Parallel()

	roundTrip := func(t *testing.T, tr *transcript) []entry {
		t.Helper()
		data, err := encodeTranscript(tr)
		if err != nil {
			t.Fatalf("encodeTranscript: %v", err)
		}
		got, err := decodeTranscript(data)
		if err != nil {
			t.Fatalf("decodeTranscript: %v", err)
		}
		return got
	}

	t.Run("a registered call keeps its bounded arguments", func(t *testing.T) {
		t.Parallel()
		call := domain.ToolCall{ID: "c1", Tool: "grep",
			Arguments: json.RawMessage(`{"pattern":"KeyMsg","path":"internal/tui/model.go"}`)}
		tr := &transcript{entries: []entry{{
			kind: entryToolCall, callID: "c1", done: true,
			tool: presentToolCall(call, "", workspaceRoot{}),
		}}}

		got := roundTrip(t, tr)
		if len(got) != 1 {
			t.Fatalf("decoded %d entries, want the one call", len(got))
		}
		const want = `{"path":"internal/tui/model.go","pattern":"KeyMsg"}`
		if string(got[0].tool.argsWire) != want {
			t.Errorf("replayed arguments %s, want %s", got[0].tool.argsWire, want)
		}
	})

	t.Run("a delegate's call carries them one depth down", func(t *testing.T) {
		t.Parallel()
		child := domain.ToolCall{ID: "k1", Tool: "read_file",
			Arguments: json.RawMessage(`{"path":"internal/tui/model.go"}`)}
		tr := &transcript{entries: []entry{
			{kind: entryToolCall, callID: "s1", done: true,
				tool: presentToolCall(domain.ToolCall{ID: "s1", Tool: "sub_agent",
					Arguments: json.RawMessage(`{"task":"survey the tests"}`)}, "", workspaceRoot{})},
			{kind: entryToolCall, callID: "k1", done: true, depth: 1, spawnCallID: "s1",
				tool: presentToolCall(child, "", workspaceRoot{})},
		}}

		got := roundTrip(t, tr)
		if len(got) != 2 {
			t.Fatalf("decoded %d entries, want the head and its child", len(got))
		}
		if want := `{"task":"survey the tests"}`; string(got[0].tool.argsWire) != want {
			t.Errorf("the run head replayed arguments %s, want %s", got[0].tool.argsWire, want)
		}
		if want := `{"path":"internal/tui/model.go"}`; string(got[1].tool.argsWire) != want {
			t.Errorf("the delegate's call replayed arguments %s, want %s", got[1].tool.argsWire, want)
		}
	})

	t.Run("an unregistered tool keeps its arguments too", func(t *testing.T) {
		t.Parallel()
		call := domain.ToolCall{ID: "c1", Tool: "tail_log", Arguments: json.RawMessage(`{"a":"b"}`)}
		tr := &transcript{entries: []entry{{
			kind: entryToolCall, callID: "c1", done: true,
			tool: presentToolCall(call, "", workspaceRoot{}),
		}}}

		got := roundTrip(t, tr)
		if len(got) != 1 {
			t.Fatalf("decoded %d entries, want the one call", len(got))
		}
		if want := `{"a":"b"}`; string(got[0].tool.argsWire) != want {
			t.Errorf("replayed arguments %s, want %s", got[0].tool.argsWire, want)
		}
	})

	t.Run("a blob written before the member decodes with none", func(t *testing.T) {
		t.Parallel()
		legacy := []byte(`{"version":1,"entries":[{"kind":"toolCall","callID":"c1","done":true,` +
			`"tool":{"label":"Read","verb":"reading","target":"main.go","name":"read_file",` +
			`"summary":{"text":"1 - 10"}}}]}`)
		got, err := decodeTranscript(legacy)
		if err != nil {
			t.Fatalf("decodeTranscript(legacy): %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("decoded %d entries, want the one call", len(got))
		}
		if got[0].tool.argsWire != nil {
			t.Errorf("a blob predating the member decoded arguments %s, want none", got[0].tool.argsWire)
		}
	})
}
