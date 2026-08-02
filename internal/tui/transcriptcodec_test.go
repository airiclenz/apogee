package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The transcript codec (session-system plan §3)
// ----------------------------------------------------------------------------

// mixedEntries is a scrollback covering every persisted entry kind and the tricky corners a
// resume must repaint exactly: skills chips on a user send, a nested (depth>0) assistant block,
// an enriched tool-call card (summary + coloured details + done, carrying its unexported name),
// a standalone tool result, a recovered error, a neutral note, and a presented document with a
// domain Method. It is the fixture behind the round-trip and exclusion tests.
//
// The tool card is finished through sanitize rather than left a bare literal, because that seam is
// where a view's body kind is settled (toolView.hasDiffBody) — a fixture that skipped it would
// describe a view the presenter never produces, and the round-trip's DeepEqual would then pass or
// fail on the fixture's shortcut instead of on the codec.
func mixedEntries() []entry {
	toolCard := toolView{
		Label: "Read File", Verb: "reading", Target: "main.go", name: "read_file",
		Summary: detailLine{Text: "1 - 100"},
		Details: []detailLine{
			{Kind: detailDiffAdded, Text: "+ added line"},
			{Kind: detailDiffRemoved, Text: "- removed line"},
			{Kind: detailPlain, Text: "  context"},
		},
	}
	toolCard.sanitize()
	return []entry{
		{kind: entryUser, text: "read main.go", skills: []string{"reviewer", "go-expert"}},
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
// name and its coloured detail lines, the depth on nested blocks, and the skills.
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
// blocks"), so an expanded tool call is stored exactly as a collapsed one and a resumed session
// paints everything collapsed. The call itself must still round-trip whole — the state is excluded,
// not the block — so the retained body is asserted beside it.
func TestTranscriptCodecExcludesExpandedState(t *testing.T) {
	t.Parallel()
	tr := &transcript{entries: mixedEntries()}
	if !tr.toggleExpanded(2) {
		t.Fatal("toggleExpanded(2) = false; the fixture's tool call did not expand")
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
// skills chips, the tool card (label/target/summary/details/name) and the presented view — a disk
// file (which could have been hand-edited) can never smuggle a terminal escape into the transcript.
// The blob is built by encoding entries that carry real ESC bytes: json.Marshal writes them as the
// valid escaped form, exactly the on-disk shape a tampered file would hold.
func TestTranscriptCodecStripsEscapesOnDecode(t *testing.T) {
	t.Parallel()
	esc := string(rune(0x1b)) // the ESC control byte
	tr := &transcript{entries: []entry{
		{kind: entryUser, text: "hi" + esc + "there", skills: []string{"sa" + esc + "fe"}},
		{
			kind: entryToolCall, callID: "c1",
			tool: toolView{
				Label: "Read" + esc + "File", Target: "ma" + esc + "in.go", name: "read" + esc + "_file",
				Summary: detailLine{Text: "1" + esc + "2"},
				Details: []detailLine{{Kind: detailDiffAdded, Text: "+a" + esc + "dd"}},
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
		for _, s := range e.skills {
			assertNoESC(t, s)
		}
		assertNoESC(t, e.tool.Label)
		assertNoESC(t, e.tool.Target)
		assertNoESC(t, e.tool.name)
		assertNoESC(t, e.tool.Summary.Text)
		for _, d := range e.tool.Details {
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
// it, so decode has to settle the view's kind again or a resumed diff would paint one line plus a
// remainder marker instead of its diffDetailCap lines. Asserted at the cap, which is what the
// reader would have seen go wrong.
func TestTranscriptCodecSettlesTheBodyKindOnDecode(t *testing.T) {
	t.Parallel()
	body := make([]detailLine, 0, diffDetailCap+3)
	for range diffDetailCap + 3 {
		body = append(body, detailLine{Kind: detailDiffAdded, Text: "+ added"})
	}
	tr := &transcript{entries: []entry{{
		kind: entryToolCall, callID: "c1", done: true,
		tool: toolView{
			Label: "View Diff", Verb: "diffing", Target: "main.go", name: "view_diff",
			Summary: detailLine{Text: "+23 -0"}, Details: body,
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
	if !got[0].tool.hasDiffBody {
		t.Error("decoded diff body did not settle as a diff body")
	}
	shown, _, truncated := collapsedDetails(got[0].tool)
	if !truncated || len(shown) != diffDetailCap {
		t.Errorf("decoded diff body paints %d lines (truncated=%v); want the diff cap of %d",
			len(shown), truncated, diffDetailCap)
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
		entry{kind: entryUser, text: "hi", skills: []string{"skill-a"}},
		entry{kind: entryAssistant, text: "hello", depth: 1},
		entry{
			kind: entryToolCall, callID: "c1", done: true,
			tool: toolView{
				Label: "Read File", Verb: "reading", Target: "main.go", name: "read_file",
				Summary: detailLine{Text: "1 - 10"},
				Details: []detailLine{{Kind: detailDiffAdded, Text: "+x"}},
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
		`{"kind":"user","text":"hi","skills":["skill-a"]},` +
		`{"kind":"assistant","text":"hello","depth":1},` +
		`{"kind":"toolCall","callID":"c1","done":true,"tool":{"label":"Read File","verb":"reading","target":"main.go","name":"read_file","summary":{"text":"1 - 10"},"details":[{"kind":1,"text":"+x"}]}},` +
		`{"kind":"presented","presented":{"title":"Report","path":"out/report.md","method":"shown"}},` +
		`{"kind":"note","text":"cancelled"}` +
		`]}`
	if string(data) != golden {
		t.Errorf("golden wire shape mismatch:\n got = %s\nwant = %s", data, golden)
	}
}
