package tui

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/schedule"
)

// ----------------------------------------------------------------------------
// The transcript codec (session-system plan §3)
// ----------------------------------------------------------------------------

// mixedEntries is a scrollback covering every persisted entry kind and the tricky corners a
// resume must repaint exactly: the skill tokens LOCATED on a user send, a nested (depth>0) assistant block,
// an enriched tool-call card (summary + coloured details + done, carrying its unexported name),
// a standalone tool result, a recovered error, a neutral note, and a presented document with a
// domain Method. It is the fixture behind the round-trip and exclusion tests.
//
// The tool card's body is built through newToolBody and the card finished through sanitize rather
// than left a bare literal, because those are the seams a real view passes (toolBody), and a
// fixture that skipped them would describe a view the presenter never produces — the round-trip's
// DeepEqual would then pass or fail on the fixture's shortcut instead of on the codec.
func mixedEntries() []entry {
	toolCard := toolView{
		Label: "Read File", Verb: "reading", Target: "main.go", name: "read_file",
		Summary: namedSummary(detailLine{Text: "1 - 100"}),
		Details: newToolBody([]detailLine{
			{Kind: detailDiffAdded, Text: "+ added line"},
			{Kind: detailDiffRemoved, Text: "- removed line"},
			{Kind: detailPlain, Text: "  context"},
		}),
	}
	toolCard.sanitize()
	return []entry{
		{kind: entryUser, text: "/reviewer read main.go", skillSpans: []skillSpan{{start: 0, end: 9}}},
		{kind: entryAssistant, text: "Reading it now.", depth: 1},
		{kind: entryToolCall, callID: "c1", done: true, tool: toolCard},
		{kind: entryToolResult, text: "error: boom", depth: 2},
		{kind: entryError, text: "loop: recovered fault"},
		{kind: entryNote, text: "cancelled"},
		{
			kind: entryPresented,
			presented: presentedView{
				Title: "Report", Path: "out/report.md", Location: "http://localhost:9/report.md",
				Method: domain.PresentServed, Reason: "",
			},
		},
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
		`{"kind":"toolCall","callID":"c1","done":true,"tool":{"label":"Read File","verb":"reading",` +
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
			Label: "Read File", Verb: "reading", Target: "main.go", name: "read_file",
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
// file: a sub-agent head is solo by rule (presentToolCall, design call 3 — a head blocks a whole
// delegation and never becomes a row in someone's list), so a blob written before Solo rode the
// wire must still replay as one block per delegation. Hand-written bytes with no "solo" member,
// because the case IS an old file: a re-encode would carry today's true and prove nothing.
//
// Two records rather than one, and span-less ones — no nested entries behind either head — because
// that is the shape the painter's own span rule cannot catch (a delegation refused at the depth
// bound) and the shape a stale false would fold into "✦ Sub-Agent (2)".
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

	want := strings.Join([]string{
		"✦ Sub-Agent",
		"  ┕ survey the tests ⋯ refused",
		"",
		"✦ Sub-Agent",
		"  ┕ survey the docs ⋯ refused",
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
			`"details":[{"text":"Ship it?"},{"text":"[x] Yes"},{"text":"[ ] No"}]}},` +
			`{"kind":"toolCall","callID":"a2","done":true,"tool":{"label":"Ask User","verb":"asking",` +
			`"target":"Tag it?","name":"ask_user","summary":{"text":"No"},` +
			`"details":[{"text":"Tag it?"},{"text":"[ ] Yes"},{"text":"[x] No"}]}}` +
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
			groupMemberLine("  ┕ Ship it? ⋯ Yes"),
			"    +3 more lines",
			"",
			"✦ Ask User",
			groupMemberLine("  ┕ Tag it? ⋯ No"),
			"    +3 more lines",
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
	tr.pending = "half-typed answer the user never saw committed"
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
// everything collapsed. It holds for every kind that OWNS the state (hasBlockState), which is why
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
		Arguments: []byte(`{"command":"cat /home/me/proj/paths.txt"}`)}, runRef{})
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
			Arguments: []byte(`{"command":"cat /home/me/proj/paths.txt"}`)}, runRef{})
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
			Arguments: []byte(`{"command":"cat /home/me/proj/paths.txt"}`)}, runRef{})
		tr.addToolResult(domain.ToolResult{CallID: "c1", Content: "/home/me/proj/docs/plan.md\n"}, runRef{})

		data, err := encodeTranscript(tr)
		if err != nil {
			t.Fatalf("encodeTranscript: %v", err)
		}
		if want := `"stat":"1 line"`; !strings.Contains(string(data), want) {
			t.Errorf("wire blob does not carry %s:\n%s", want, data)
		}
		got, err := decodeTranscript(data)
		if err != nil {
			t.Fatalf("decodeTranscript: %v", err)
		}
		if len(got) != 1 || got[0].tool.stat != "1 line" {
			t.Fatalf("decoded %+v; want the one call with its typed stat", got)
		}
		// The fixture is only worth anything if the guard actually bites at this width.
		if live := renderPlain(tr, narrow); !strings.Contains(live, "1 line") {
			t.Fatalf("fixture: the guard does not demote at width %d:\n%s", narrow, live)
		}
		if replayed, live := renderPlain(&transcript{entries: got}, narrow), renderPlain(tr, narrow); replayed != live {
			t.Errorf("the block changed shape across the round trip:\n--- replayed ---\n%s\n--- live ---\n%s",
				replayed, live)
		}
	})

	t.Run("a line the block worded stays unquoted and writes no member", func(t *testing.T) {
		card := toolView{
			Label: "Read File", Verb: "reading", Target: "main.go", name: "read_file",
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
	if !errors.Is(err, ErrTranscriptVersion) {
		t.Fatalf("decodeTranscript error = %v; want ErrTranscriptVersion", err)
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

// TestTranscriptCodecStripsEscapesOnDecode proves the defence-in-depth strip: ESC bytes salted
// through every text field of a stored blob are removed on the way back in, across the entry body,
// the tool card (label/target/summary/details/name), a firing block's stored answer
// and prompt, and the presented view — a disk file (which could have been hand-edited) can never
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
// The pair is ADDITIVE within transcriptVersion and needs no bump, on the wireEnvelope rule: a run
// that never reported writes neither member, and a blob written before they existed decodes to the
// zero pair — which is the nothing-to-say case the summary line already hides, so no migration.
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
// The member census is the guard on that claim. This item added display state to toolView, and the
// cheapest way to have got it onto the screen after a resume would have been to widen the wire;
// the two field lists below are what makes such a widening a failing test rather than a silent
// format change (both structs are inside transcriptVersion 1, so every member is forever).
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

	t.Run("the wire structs gained no members", func(t *testing.T) {
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
			"CtxUsed", "CtxLimit", "SkillSpans", "Tool", "Presented",
		}
		if got := fields(wireEntry{}); !slices.Equal(got, wantEntry) {
			t.Errorf("wireEntry members = %v, want %v — widening the wire needs its own decision", got, wantEntry)
		}
		wantTool := []string{"Label", "Verb", "Target", "Name", "Solo", "Stat", "Summary", "Details"}
		if got := fields(wireToolView{}); !slices.Equal(got, wantTool) {
			t.Errorf("wireToolView members = %v, want %v — widening the wire needs its own decision", got, wantTool)
		}
	})
}

// TestTranscriptCodecRoundTripsTheSpawningCallID pins the run identity a delegated entry keeps
// across a resume (ADR 0039): the id of the sub_agent call that spawned the agent whose event
// folded into it, which is what tells two concurrent children's blocks apart once several run at
// once — depth cannot, because siblings share it. The member is additive within transcriptVersion,
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
				Label: "Read File", Verb: "reading", Target: "main.go", name: "read_file",
				Summary: namedSummary(detailLine{Text: "1 - 10"}),
				Details: newToolBody([]detailLine{{Kind: detailDiffAdded, Text: "+x"}}),
			},
		},
		entry{kind: entryPresented, presented: presentedView{Title: "Report", Path: "out/report.md", Method: domain.PresentShown}},
		entry{kind: entryNote, text: "cancelled"},
	)
	tr.pending = "typing" // excluded from the wire
	tr.streaming = true

	data, err := encodeTranscript(tr)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	const golden = `{"version":1,"entries":[` +
		`{"kind":"user","text":"hi"},` +
		`{"kind":"assistant","text":"hello","depth":1},` +
		`{"kind":"toolCall","callID":"c1","done":true,"tool":{"label":"Read File","verb":"reading","target":"main.go","name":"read_file","summary":{"text":"1 - 10"},"details":[{"kind":1,"text":"+x"}]}},` +
		`{"kind":"presented","presented":{"title":"Report","path":"out/report.md","method":"shown"}},` +
		`{"kind":"note","text":"cancelled"}` +
		`]}`
	if string(data) != golden {
		t.Errorf("golden wire shape mismatch:\n got = %s\nwant = %s", data, golden)
	}
}
