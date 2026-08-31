package session

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

// goldenV1 pins the exact v1 wire shape of a scrollback blob: field names, their order, the string
// kind enum, the nested tool card with its name, stats, body and diff regions, the presented Method
// as a domain string, and omitempty behaviour throughout. It is the byte form Apogee has always
// written, so a change to any of it — a renamed field, a reordered rendering-hint constant leaking
// into the wire, a lost name — breaks TestTranscriptGoldenV1RoundTrips, which is the point: old
// session files must keep decoding, and this codec's move out of the TUI must not have moved a byte.
const goldenV1 = `{"version":1,"entries":[` +
	`{"kind":"user","text":"/review please","skillSpans":[{"start":0,"end":7}]},` +
	`{"kind":"assistant","text":"on it","depth":1,"spawnCallID":"s1"},` +
	`{"kind":"toolCall","callID":"s1","done":true,"ctxUsed":1200,"ctxLimit":32768,` +
	`"ctxModel":"gpt-oss-20b","usageCalls":2,"usagePromptTokens":11000,"usageCachedPromptTokens":300,` +
	`"usageCompletionTokens":1000,"usageTotalTokens":12000,` +
	`"tool":{"label":"Sub-Agent","verb":"delegating","target":"tests","name":"sub_agent","solo":true,` +
	`"stat":"2 files","statValue":{"counted":true,"n":2,"nounForOne":"file","nounForMany":"files"},` +
	`"task":"survey the tests","summary":{"text":"done","quoted":true},"args":{"prompt":"survey"}}},` +
	`{"kind":"toolCall","callID":"c2","done":true,"tool":{"label":"Edit","verb":"editing",` +
	`"target":"main.go","name":"edit_file","summary":{"kind":2,"gutter":"12","text":"+2 -2",` +
	`"stat":{"added":2,"removed":2}},"details":[{"kind":1,"gutter":"12","text":"+x"},` +
	`{"kind":2,"text":"-y"}],"regions":[{"beforeStart":10,"afterStart":10,"leading":["ctx"],` +
	`"removed":["y"],"inserted":["x"],"trailing":["end"]}],"regionFiles":["main.go"]}},` +
	`{"kind":"toolResult","text":"ok","callID":"c2"},` +
	`{"kind":"error","text":"boom"},` +
	`{"kind":"note","text":"cancelled"},` +
	`{"kind":"presented","presented":{"title":"Report","path":"out/report.md",` +
	`"location":"http://localhost:1/x","method":"shown","reason":"asked"}},` +
	`{"kind":"interjected","text":"wait"},` +
	`{"kind":"schedule","text":"firing","done":true,"tool":{"label":"Firing","summary":{"text":"ok"}}}` +
	`]}`

// goldenV1Entries is what goldenV1 means: the entries a Driver replays from it. Written out in full
// rather than spot-checked, because the blob's whole job is that every member survives the trip.
func goldenV1Entries() []Entry {
	return []Entry{
		{Kind: EntryKindUser, Text: "/review please", SkillSpans: []SkillSpan{{Start: 0, End: 7}}},
		{Kind: EntryKindAssistant, Text: "on it", Depth: 1, SpawnCallID: "s1"},
		{
			Kind: EntryKindToolCall, CallID: "s1", Done: true,
			CtxUsed: 1200, CtxLimit: 32768, CtxModel: "gpt-oss-20b",
			UsageCalls: 2, UsagePromptTokens: 11000, UsageCachedPromptTokens: 300,
			UsageCompletionTokens: 1000, UsageTotalTokens: 12000,
			Tool: &ToolView{
				Label: "Sub-Agent", Verb: "delegating", Target: "tests", Name: "sub_agent", Solo: true,
				Stat:      "2 files",
				StatValue: &StatValue{Counted: true, N: 2, NounForOne: "file", NounForMany: "files"},
				Task:      "survey the tests",
				Summary:   BranchSummary{DetailLine: DetailLine{Text: "done"}, Quoted: true},
				Args:      json.RawMessage(`{"prompt":"survey"}`),
			},
		},
		{
			Kind: EntryKindToolCall, CallID: "c2", Done: true,
			Tool: &ToolView{
				Label: "Edit", Verb: "editing", Target: "main.go", Name: "edit_file",
				Summary: BranchSummary{
					DetailLine: DetailLine{Kind: 2, Gutter: "12", Text: "+2 -2"},
					Stat:       &StatValue{Added: 2, Removed: 2},
				},
				Details: []DetailLine{{Kind: 1, Gutter: "12", Text: "+x"}, {Kind: 2, Text: "-y"}},
				Regions: []EditRegion{{
					BeforeStart: 10, AfterStart: 10,
					Leading:  []string{"ctx"},
					Removed:  []string{"y"},
					Inserted: []string{"x"},
					Trailing: []string{"end"},
				}},
				RegionFiles: []string{"main.go"},
			},
		},
		{Kind: EntryKindToolResult, Text: "ok", CallID: "c2"},
		{Kind: EntryKindError, Text: "boom"},
		{Kind: EntryKindNote, Text: "cancelled"},
		{Kind: EntryKindPresented, Presented: &Presented{
			Title: "Report", Path: "out/report.md", Location: "http://localhost:1/x",
			Method: "shown", Reason: "asked",
		}},
		{Kind: EntryKindInterjected, Text: "wait"},
		{
			Kind: EntryKindSchedule, Text: "firing", Done: true,
			Tool: &ToolView{Label: "Firing", Summary: BranchSummary{DetailLine: DetailLine{Text: "ok"}}},
		},
	}
}

// TestTranscriptGoldenV1RoundTrips is the compatibility guard: the pinned blob decodes to exactly
// the entries it means, and those entries re-encode to exactly the pinned bytes. Both halves matter
// — decoding proves an old file still reads, re-encoding proves this build writes what the previous
// one wrote, so a session saved by one Apogee and reopened by the next is the same scrollback.
func TestTranscriptGoldenV1RoundTrips(t *testing.T) {
	t.Parallel()

	got, err := DecodeTranscript([]byte(goldenV1))
	if err != nil {
		t.Fatalf("DecodeTranscript(goldenV1): %v", err)
	}
	if want := goldenV1Entries(); !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded entries differ from the golden reading:\n got = %#v\nwant = %#v", got, want)
	}

	data, err := EncodeTranscript(got)
	if err != nil {
		t.Fatalf("EncodeTranscript: %v", err)
	}
	if string(data) != goldenV1 {
		t.Errorf("golden wire shape mismatch:\n got = %s\nwant = %s", data, goldenV1)
	}
}

// TestTranscriptEncodesAnEmptyScrollbackAsAnEmptyList pins the one byte-level case a nil slice would
// quietly change: an empty transcript has always written "entries":[], and a codec that wrote null
// instead would be a new wire shape for the commonest record of all.
func TestTranscriptEncodesAnEmptyScrollbackAsAnEmptyList(t *testing.T) {
	t.Parallel()
	data, err := EncodeTranscript(nil)
	if err != nil {
		t.Fatalf("EncodeTranscript(nil): %v", err)
	}
	if want := `{"version":1,"entries":[]}`; string(data) != want {
		t.Errorf("EncodeTranscript(nil) = %s, want %s", data, want)
	}
}

// TestDecodeTranscriptRefusesANewerVersion proves the reject-forward rule this payload owns: a blob
// stamped by a build that knows members this one does not is refused rather than half-read, so the
// caller degrades to resuming with no replay instead of painting a record it misunderstood.
func TestDecodeTranscriptRefusesANewerVersion(t *testing.T) {
	t.Parallel()
	blob := []byte(`{"version":2,"entries":[{"kind":"user","text":"hi"}]}`)
	got, err := DecodeTranscript(blob)
	if !errors.Is(err, ErrTranscriptVersion) {
		t.Fatalf("DecodeTranscript(v%d blob) error = %v, want ErrTranscriptVersion", TranscriptVersion+1, err)
	}
	if got != nil {
		t.Errorf("a refused blob yielded %d entries, want none", len(got))
	}
}

// TestDecodeTranscriptTreatsNoBlobAsNoScrollback covers the legacy and never-recorded records: both
// resume with an empty scrollback rather than an error the caller would have to word a note for.
func TestDecodeTranscriptTreatsNoBlobAsNoScrollback(t *testing.T) {
	t.Parallel()
	for name, data := range map[string][]byte{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := DecodeTranscript(data)
			if err != nil {
				t.Fatalf("DecodeTranscript(%s): %v", name, err)
			}
			if got != nil {
				t.Errorf("DecodeTranscript(%s) = %#v, want nil", name, got)
			}
		})
	}
}

// TestDecodeTranscriptStripsEscapesEverywhereItCanBePainted is the defence-in-depth guard over
// untrusted disk input. A session file can be tampered with — or written by a build with a hole in
// its own strip — and the frame is painted through a cell buffer that HONOURS OSC 8 hyperlinks and
// never resets link state across cells, so one unterminated opener anywhere in a record turns the
// rest of the frame into a clickable link to the attacker's URL.
//
// The fields are enumerated deliberately: the card's own text, the outcome slot's phrase AND the
// nouns its arithmetic spells the total with, every body line including the gutter column, the diff
// regions (tool-recorded file content, which a malicious repo owns every byte of) and the file names
// beside them. Stripping the entry text alone would ship a codec that reads clean while a card's
// target still smuggles an escape.
func TestDecodeTranscriptStripsEscapesEverywhereItCanBePainted(t *testing.T) {
	t.Parallel()
	// The ESC byte rides the fixture as its JSON escape, which is how a tampered file would carry it:
	// a raw 0x1b inside a JSON string is not valid JSON and would never reach the decoder at all.
	const esc = `\u001b[31m`
	blob := []byte(`{"version":1,"entries":[{"kind":"user","text":"` + esc + `hi"},` +
		`{"kind":"toolCall","callID":"c1","ctxModel":"` + esc + `m","tool":{` +
		`"label":"` + esc + `Edit","verb":"` + esc + `editing","target":"` + esc + `main.go",` +
		`"name":"` + esc + `edit_file","stat":"` + esc + `2 files",` +
		`"statValue":{"counted":true,"n":2,"nounForOne":"` + esc + `file","nounForMany":"` + esc + `files"},` +
		`"task":"` + esc + `survey","summary":{"gutter":"` + esc + `12","text":"` + esc + `+2 -2",` +
		`"stat":{"counted":true,"n":1,"nounForOne":"` + esc + `entry"}},` +
		`"details":[{"gutter":"` + esc + `9","text":"` + esc + `+x"}],` +
		`"regions":[{"leading":["` + esc + `ctx"],"removed":["` + esc + `y"],` +
		`"inserted":["` + esc + `x"],"trailing":["` + esc + `end"]}],` +
		`"regionFiles":["` + esc + `main.go"],"args":{"path":"` + esc + `main.go"}}},` +
		`{"kind":"presented","presented":{"title":"` + esc + `R","path":"` + esc + `p",` +
		`"location":"` + esc + `l","reason":"` + esc + `why"}}]}`)

	got, err := DecodeTranscript(blob)
	if err != nil {
		t.Fatalf("DecodeTranscript: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("decoded %d entries, want 3", len(got))
	}
	tv, pv := got[1].Tool, got[2].Presented
	if tv == nil || pv == nil {
		t.Fatalf("the tool card or the presentation did not decode: %#v", got)
	}
	for name, field := range map[string]string{
		"entry text":      got[0].Text,
		"ctxModel":        got[1].CtxModel,
		"label":           tv.Label,
		"verb":            tv.Verb,
		"target":          tv.Target,
		"name":            tv.Name,
		"stat":            tv.Stat,
		"statValue noun":  tv.StatValue.NounForOne,
		"statValue many":  tv.StatValue.NounForMany,
		"task":            tv.Task,
		"summary text":    tv.Summary.Text,
		"summary gutter":  tv.Summary.Gutter,
		"summary noun":    tv.Summary.Stat.NounForOne,
		"detail text":     tv.Details[0].Text,
		"detail gutter":   tv.Details[0].Gutter,
		"region leading":  tv.Regions[0].Leading[0],
		"region removed":  tv.Regions[0].Removed[0],
		"region inserted": tv.Regions[0].Inserted[0],
		"region trailing": tv.Regions[0].Trailing[0],
		"regionFiles":     tv.RegionFiles[0],
		"presented title": pv.Title,
		"presented path":  pv.Path,
		"presented loc":   pv.Location,
		"presented why":   pv.Reason,
	} {
		if containsESC(field) {
			t.Errorf("%s came back with an escape sequence in it: %q", name, field)
		}
	}
	// The stored arguments are the deliberate exception: nothing on the replay path paints them, and
	// the surface that eventually shows them is the surface that must strip them — so the record's
	// own bytes come back exactly as they stand rather than being rewritten under it.
	if want := `{"path":"` + esc + `main.go"}`; string(tv.Args) != want {
		t.Errorf("the stored arguments were rewritten: got %s, want %s", tv.Args, want)
	}
}

// containsESC reports whether s still carries an ESC byte.
func containsESC(s string) bool {
	for i := range len(s) {
		if s[i] == 0x1b {
			return true
		}
	}
	return false
}

// TestCloseInterruptedCallsClosesWhatTheRecordCaughtOpen proves the fact-rewrite a resume owes the
// reader: a call still open in the blob is waiting for a result that is never coming, because the
// engine running it died with the process that wrote the record. It comes back closed and worded as
// interrupted, and the count is what lets the caller add one note for the whole replay.
//
// The tool-less toolCall is the other half: Tool is a POINTER on the wire, so a truncated or
// hand-written blob can perfectly well carry a call with no card, and a pass that dereferenced it
// blindly would panic on a file rather than degrade.
func TestCloseInterruptedCallsClosesWhatTheRecordCaughtOpen(t *testing.T) {
	t.Parallel()
	blob := []byte(`{"version":1,"entries":[` +
		`{"kind":"toolCall","callID":"open","tool":{"label":"Sub-Agent","summary":{}}},` +
		`{"kind":"toolCall","callID":"settled","done":true,"tool":{"label":"Read","summary":{"text":"10 lines"}}},` +
		`{"kind":"toolCall","callID":"c1"},` +
		`{"kind":"assistant","text":"still talking"}]}`)
	entries, err := DecodeTranscript(blob)
	if err != nil {
		t.Fatalf("DecodeTranscript: %v", err)
	}

	if closed := CloseInterruptedCalls(entries); closed != 1 {
		t.Fatalf("CloseInterruptedCalls closed %d calls, want the one open card", closed)
	}
	if !entries[0].Done || entries[0].Tool.Summary.Text != interruptedSummary {
		t.Errorf("the open call did not come back interrupted: done=%v summary=%q",
			entries[0].Done, entries[0].Tool.Summary.Text)
	}
	if entries[1].Tool.Summary.Text != "10 lines" {
		t.Errorf("a settled call was rewritten: %q", entries[1].Tool.Summary.Text)
	}
	if entries[2].Tool != nil || entries[2].Done {
		t.Errorf("the card-less call was touched: %#v", entries[2])
	}
	if entries[3].Done {
		t.Errorf("a non-call entry was closed: %#v", entries[3])
	}
}

// TestDecodeTranscriptKeepsAnUnknownKind pins where the skip rule lives. A kind this build does not
// know is a FUTURE variant within the same version, and the codec hands it back as stored: the
// consumer decides whether it can paint it, because only the consumer knows its own vocabulary.
func TestDecodeTranscriptKeepsAnUnknownKind(t *testing.T) {
	t.Parallel()
	got, err := DecodeTranscript([]byte(`{"version":1,"entries":[{"kind":"hologram","text":"hi"}]}`))
	if err != nil {
		t.Fatalf("DecodeTranscript: %v", err)
	}
	if len(got) != 1 || got[0].Kind != "hologram" {
		t.Fatalf("the unknown kind did not come back as stored: %#v", got)
	}
}

// TestDecodeTranscriptRejectsAMalformedBlob covers the third degrade: neither a version refusal nor
// a clean read, so the caller gets an error it words as a no-replay note rather than a panic.
func TestDecodeTranscriptRejectsAMalformedBlob(t *testing.T) {
	t.Parallel()
	if _, err := DecodeTranscript([]byte(`{"version":1,"entries":`)); err == nil {
		t.Fatal("a truncated blob decoded without error")
	}
}
