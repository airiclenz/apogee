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
// than left a bare literal, because those are the seams a real view passes: the body's kind is
// settled where its lines are (toolBody), and a fixture that skipped them would describe a view the
// presenter never produces — the round-trip's DeepEqual would then pass or fail on the fixture's
// shortcut instead of on the codec.
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
// and it comes back with every view field, its pairing key, its done mark and its body's settled
// kind intact.
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
// that was and stays workspace-relative. The wire carries no mark for whose words a summary is
// (branchSummary is the presenter's side of the seam) and needs none, because decode escape-strips a
// replayed card and respells nothing.
func TestTranscriptCodecReplaysAPromotedSummaryAsShown(t *testing.T) {
	t.Parallel()
	tr := &transcript{ws: newWorkspaceRoot("/home/me/proj")}
	tr.addToolCall(domain.ToolCall{ID: "c1", Tool: "terminal",
		Arguments: []byte(`{"command":"cat /home/me/proj/paths.txt"}`)}, 0)
	tr.addToolResult(domain.ToolResult{CallID: "c1", Content: "/home/me/proj/docs/plan.md\n"}, 0)

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
	// The strip removes only the ESC byte, leaving the surrounding text intact.
	if got[0].text != "hithere" {
		t.Errorf("stripped user text = %q; want %q", got[0].text, "hithere")
	}
}

// TestTranscriptCodecSettlesTheBodyKindOnDecode proves the derived body kind survives a resume
// even though the wire never carries it: the blob stores each line's own Kind and nothing above
// it, so decode has to put the lines back through newToolBody rather than seating them in a
// toolBody of its own, or a resumed diff would come back describing itself as plain. The collapsed
// paint no longer varies by kind — one house budget caps every body (collapsedBodyCap) — so what
// is asserted here is the constructor seam holding across the wire, not a cap.
func TestTranscriptCodecSettlesTheBodyKindOnDecode(t *testing.T) {
	t.Parallel()
	const bodyLines = 23
	body := make([]detailLine, 0, bodyLines)
	for range bodyLines {
		body = append(body, detailLine{Kind: detailDiffAdded, Text: "+ added"})
	}
	tr := &transcript{entries: []entry{{
		kind: entryToolCall, callID: "c1", done: true,
		tool: toolView{
			Label: "View Diff", Verb: "diffing", Target: "main.go", name: "view_diff",
			Summary: namedSummary(detailLine{Text: "+23 -0"}), Details: newToolBody(body),
		},
	}}}

	data, err := encodeTranscript(tr)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	got, err := decodeTranscript(data)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("decoded %d entries; want the one tool call", len(got))
	}
	if !got[0].tool.Details.isDiff() {
		t.Error("decoded diff body did not settle as a diff body")
	}
	if got := got[0].tool.Details.len(); got != bodyLines {
		t.Errorf("decoded body has %d lines, want the whole %d", got, bodyLines)
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
