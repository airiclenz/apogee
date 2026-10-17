package eventjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// delegatedBase is the identity of an Event a sub-agent emitted, so every envelope member is
// exercised; topLevelBase is a Depth-0 Event's.
var (
	delegatedBase = domain.EventBase{Turn: 3, Depth: 1, CallID: "call-parent", RunID: "run-7"}
	topLevelBase  = domain.EventBase{Turn: 2}
)

// roundTripEvents is one fixture per Event kind — several where a kind has shapes worth telling
// apart — with every member set, so a member the decoder forgets reads back as its zero and fails.
func roundTripEvents() []domain.Event {
	return []domain.Event{
		domain.TokenEvent{EventBase: delegatedBase, Text: "hel"},
		domain.ReasoningEvent{EventBase: topLevelBase, Text: "weighing"},
		domain.StreamResetEvent{EventBase: topLevelBase},
		domain.MessageEvent{EventBase: topLevelBase, Text: "done"},
		domain.ToolCallEvent{
			EventBase:    topLevelBase,
			Call:         domain.ToolCall{ID: "call-1", Tool: "read_file", Arguments: json.RawMessage(`{"path": "a.go", "limit": 5}`)},
			ResolvedPath: "/work/a.go",
		},
		domain.ToolCallEvent{
			EventBase: topLevelBase,
			Call: domain.ToolCall{ID: "call-2", Tool: "edit_file", Arguments: json.RawMessage(`{}`),
				Malformed: &domain.MalformedArguments{
					Err: fmt.Errorf("parse: %w", errors.New("unexpected end of JSON input")),
					Raw: `{"path":`,
				}},
		},
		domain.ToolCallEvent{
			EventBase:  topLevelBase,
			Call:       domain.ToolCall{ID: "call-3", Tool: "sub_agent", Arguments: json.RawMessage(`{"task":"look"}`)},
			SpawnRunID: "run-7",
		},
		domain.ToolResultEvent{
			EventBase:   topLevelBase,
			Result:      domain.ToolResult{CallID: "call-4", Content: "wrote a.go", IsError: true},
			Tool:        "write_file",
			WriteTarget: "/work/a.go",
			SpawnRunID:  "run-9",
		},
		domain.SubAgentPhaseEvent{
			EventBase: delegatedBase, Phase: domain.SubAgentStarted, StepCap: 30, CapRequested: 50,
		},
		domain.SubAgentPhaseEvent{
			EventBase: delegatedBase,
			Phase:     domain.SubAgentFinished,
			Result: domain.ToolResult{CallID: "call-3", Content: "report",
				Summary: domain.DelegationOutcome{Bound: domain.DelegationStepCap, SteeredMessages: 2}},
		},
		domain.SubAgentGroupEvent{EventBase: topLevelBase, Size: 3, Width: 2},
		domain.SubAgentNamedEvent{EventBase: delegatedBase, Name: "scout"},
		domain.ChildInterjectionEvent{
			EventBase: delegatedBase,
			Input: domain.UserInput{
				Text:         "also check b.go",
				FileRefs:     []string{"b.go"},
				Images:       []domain.Image{{Name: "shot.png", MediaType: "image/png", Data: []byte{1, 2, 3}}},
				SkillIDs:     []string{"review"},
				Recipe:       &domain.RecipeLaunch{SkillID: "triage", Text: "go", Background: true},
				RecipeInputs: map[string]string{"target": "b.go"},
			},
			Reason: domain.UndeliveredCapped,
		},
		domain.ApprovalEvent{
			EventBase: topLevelBase,
			Phase:     domain.ApprovalRequested,
			Request: domain.ApprovalRequest{
				Tool: "terminal", Arguments: json.RawMessage(`{"command": "go test"}`), Reason: "terminal",
				Remedy: "allow it", SubAgentTask: "t", SubAgentName: "n", CacheKey: "k", MCPServerGrant: true,
				MCPServerAlias: "srv", ResolvedPath: "/work", Scope: "workspace",
			},
		},
		domain.ApprovalEvent{
			EventBase: topLevelBase,
			Phase:     domain.ApprovalDecided,
			Request:   domain.ApprovalRequest{Tool: "terminal", Arguments: json.RawMessage(`{"command":"go vet"}`)},
			Decision:  domain.ApprovalAllowedByRule,
			Rules:     []domain.AllowRule{{Kind: domain.AllowRuleTerminal, Text: "go vet", Layer: domain.AllowRuleProject}},
		},
		domain.TurnEvent{EventBase: delegatedBase, Status: domain.StatusTurnComplete, Faulted: true, StepCapped: true},
		domain.ReactionFiredEvent{
			EventBase: topLevelBase, Reaction: "tool-call-repair", Origin: domain.OriginEngine,
			Moment: domain.MomentPreToolExec, Action: "retry", Detail: "fixed quotes",
		},
		domain.SeamClosedEvent{EventBase: topLevelBase, Seam: domain.MomentPreToolExec, Value: "live value"},
		domain.SeamClosedEvent{EventBase: topLevelBase, Seam: domain.MomentPostResponse, Fired: []string{"a", "b"}},
		domain.ErrorEvent{EventBase: topLevelBase, Source: "loop", Err: "boom"},
		domain.PruneEvent{EventBase: topLevelBase, Results: 4, Tokens: 900},
		domain.RefClippedEvent{EventBase: topLevelBase, Ref: "big.go", Tokens: 8000, Absolute: true},
		domain.UsageEvent{
			EventBase: delegatedBase, PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120,
			CachedPromptTokens: 40, Model: "m", ContextWindow: 32000, CostMicros: 1234, Priced: true,
			Maintenance: true, DelegateFold: true, ServedModel: "m-served",
			Cumulative: domain.Usage{
				Calls: 3, PromptTokens: 300, CachedPromptTokens: 90, CompletionTokens: 60, TotalTokens: 360,
				CostMicros: 1_501_234, PricedCalls: 2, UnpricedCalls: 1,
			},
		},
		domain.AuditEvent{
			EventBase: delegatedBase, Tool: "terminal", CallID: "call-audited", Decision: "allow",
			Reason: "rule", IsError: true,
		},
		domain.UpstreamAttemptEvent{
			EventBase: topLevelBase, Server: "local", Endpoint: "http://h/v1", Model: "m", RequestID: "req-1",
			Index: 1, TTFB: 120 * time.Millisecond, TTFT: 300 * time.Millisecond, Last: 2 * time.Second,
			Duration: 2100 * time.Millisecond, OutputTokens: 77, Outcome: "ok",
		},
		domain.UpstreamAttemptEvent{
			EventBase: topLevelBase, Server: "local", TTFB: 1500 * time.Microsecond, Duration: 999 * time.Microsecond,
			Outcome: "cancelled",
		},
		domain.WorkflowPhaseEvent{
			EventBase: topLevelBase, Phase: domain.WorkflowStarted, Workflow: "wf", Name: "build", Call: "workflow-1",
			Background: true, Origin: "recipe", Stages: []string{"plan", "do"}, StageKinds: []string{"single", "each"},
			Items: 4, Rounds: 2, Run: "run-wf", Attempt: 1, Resume: "resume-1",
		},
		domain.WorkflowPhaseEvent{
			EventBase: topLevelBase, Phase: domain.WorkflowItemFinished, Workflow: "wf", Stage: "do", Item: "i-1",
			ItemName: "first", Index: 1, Resumed: true, Detail: "d", Round: 1, Outcome: domain.WorkflowStageDone,
			Receipt: domain.WorkflowReceipt{Status: "ok", Summary: "s", Fields: map[string]string{"k": "v"}},
		},
		domain.WorkflowPhaseEvent{
			EventBase: topLevelBase, Phase: domain.WorkflowFinished, Workflow: "wf",
			Tally: &domain.WorkflowTally{OK: 1, Partial: 2, Blocked: 3, Unfinished: 4},
		},
		domain.MalformedChunksEvent{EventBase: topLevelBase, Count: 2},
	}
}

// roundTripSummaries is one Tool summary per variant — the empty and nil shapes of the variants
// that hold a slice among them — and the nil summary.
func roundTripSummaries() []domain.ToolSummary {
	return []domain.ToolSummary{
		nil,
		domain.ReadSpan{Start: 10, End: 20, Total: 90, Locate: "Encode", LocatedOn: []int{12, 77}},
		domain.ReadSpan{Start: 1, End: 5, Total: 5, Locate: "absent", LocatedOn: []int{}},
		domain.ListedEntries{Total: 48, Skipped: 20},
		domain.MatchedLines{Total: 6},
		domain.DiffStat{Added: 7, Removed: 3},
		domain.ChangedFiles{Staged: 2, Unstaged: 1, Untracked: 4},
		domain.EditRegions{Regions: []domain.EditRegion{{
			BeforeStart: 4, AfterStart: 5, Leading: []string{"a"}, Removed: []string{"b"},
			Inserted: []string{"c", "d"}, Trailing: nil,
		}}},
		domain.EditRegions{Regions: []domain.EditRegion{}},
		domain.EditRegions{},
		domain.SearchHits{Count: 9},
		domain.DelegationOutcome{IsStoppedByUser: true, HasNoReport: true},
		domain.DelegationOutcome{Bound: domain.DelegationStepCap},
		domain.DelegationOutcome{Bound: domain.DelegationTokenBudget},
		domain.DelegationOutcome{Bound: domain.DelegationTimeLimit, SteeredMessages: 1},
	}
}

// roundTripStarted and roundTripFinished are the two frames, every member set.
var (
	roundTripStarted = RunStarted{
		Session: "sess-9", Workspace: "/work", Model: "m", Server: "local", Mode: "auto",
		Bypass: true, Confined: true, Version: "0.9.1",
	}
	roundTripError    = "step cap"
	roundTripFinished = RunFinished{
		ExitCode: 3, Turns: 4, Denied: 1, Faulted: true, Fault: "f", Error: &roundTripError, Title: "t",
		FinalText: "answer", Wrote: []string{"a.go"},
		ContextFiles: ContextFiles{Files: []ContextFileNote{{Name: "AGENTS.md", Bytes: 10}}, StandingTokens: 5, SystemShare: 2},
		ContextCost:  ContextCost{Rows: []ContextCostRow{{Name: "tools", Bytes: 7, Tokens: 2}}, Bytes: 7, Tokens: 2, Calibrated: true},
		UndoNote:     "u", Saved: true,
		Usage: Usage{Calls: 2, PromptTokens: 9, CompletionTokens: 8, TotalTokens: 17, CachedPromptTokens: 1,
			Cost: "0.5", Currency: "EUR", UnpricedCalls: 1},
		SubAgents:               []SubAgentUsage{{Used: 1, Limit: 2, Task: "t", Name: "n", Model: "m", Calls: 1, Cost: "0", UnpricedCalls: 1}},
		Turn1PromptTokens:       11,
		Turn1CachedPromptTokens: 3,
	}
)

// TestDecodeRoundTripsEveryKind writes every Event kind, every Tool summary variant and both frames
// through a Writer and decodes each line back: the envelope reads back as stamped, and the Event
// reads back as the original under Decode's documented lossy points (lossyView).
func TestDecodeRoundTripsEveryKind(t *testing.T) {
	t.Parallel()

	events := roundTripEvents()
	for _, summary := range roundTripSummaries() {
		events = append(events, domain.ToolResultEvent{
			EventBase: topLevelBase,
			Result:    domain.ToolResult{CallID: "call-s", Content: "prose", Summary: summary},
			Tool:      "some_tool",
		})
	}
	var out bytes.Buffer
	w := New(&out, Options{Session: "sess-9", Now: fixedClock, Seams: true, Currency: "EUR"})
	w.RunStarted(roundTripStarted)
	for _, event := range events {
		w.Emit(event)
	}
	w.RunFinished(roundTripFinished)

	decoded := decodeAll(t, &out)
	if len(decoded) != len(events)+2 {
		t.Fatalf("decoded %d lines, want %d", len(decoded), len(events)+2)
	}
	for i, line := range decoded {
		assertEnvelope(t, line, i+1)
	}
	assertFrames(t, decoded[0], decoded[len(decoded)-1])
	seen := map[string]bool{kindRunStarted: true, kindRunFinished: true}
	for i, event := range events {
		line := decoded[i+1]
		kind, base, _, _ := Encode(event)
		seen[line.Kind] = true
		if line.Kind != kind || line.Turn != base.Turn || line.Depth != base.Depth ||
			line.CallID != base.CallID || line.RunID != base.RunID {
			t.Errorf("line %d envelope = %+v, want kind %q and identity %+v", line.Seq, line, kind, base)
		}
		if got, want := lossyView(line.Event), lossyView(event); !reflect.DeepEqual(got, want) {
			t.Errorf("line %d (%s) round trip:\n got %#v\nwant %#v", line.Seq, kind, got, want)
		}
	}
	for _, kind := range Kinds() {
		if !seen[kind] {
			t.Errorf("the round trip covers no %q line", kind)
		}
	}
}

// decodeAll decodes every line out wrote, failing on the first that does not decode.
func decodeAll(t *testing.T, out *bytes.Buffer) []Line {
	t.Helper()

	var decoded []Line
	for _, raw := range lines(t, out) {
		line, err := Decode([]byte(raw))
		if err != nil {
			t.Fatalf("Decode(%s): %v", raw, err)
		}
		decoded = append(decoded, line)
	}
	return decoded
}

// assertEnvelope checks the members every line of the round trip shares.
func assertEnvelope(t *testing.T, line Line, wantSeq int) {
	t.Helper()

	if line.V != lineVersion || line.Seq != wantSeq || line.Session != "sess-9" || !line.Time.Equal(fixedClock()) {
		t.Errorf("line %d envelope = v %d, seq %d, session %q, time %v; want v %d, seq %d, sess-9, %v",
			wantSeq, line.V, line.Seq, line.Session, line.Time, lineVersion, wantSeq, fixedClock())
	}
}

// assertFrames checks the two frames read back whole, with a null identity and no Event.
func assertFrames(t *testing.T, first, last Line) {
	t.Helper()

	if first.Kind != kindRunStarted || first.Started == nil || *first.Started != roundTripStarted {
		t.Errorf("first line = %+v (started %+v), want the run_started frame %+v", first, first.Started, roundTripStarted)
	}
	if last.Kind != kindRunFinished || last.Finished == nil || !reflect.DeepEqual(*last.Finished, roundTripFinished) {
		t.Errorf("last line = %+v (finished %+v), want the run_finished frame %+v", last, last.Finished, roundTripFinished)
	}
	for _, frame := range []Line{first, last} {
		if frame.Event != nil || frame.Turn != 0 || frame.Depth != 0 || frame.CallID != "" || frame.RunID != "" {
			t.Errorf("%s frame carries an Event or an identity: %+v", frame.Kind, frame)
		}
	}
}

// lossyView maps an Event onto what its line can carry, so an original and its decoded copy compare
// equal exactly where Decode documents a loss: argument JSON compacted, a null argument nil, a
// parse error by its text, durations in whole milliseconds, nil and empty slices and maps equal,
// and the members no line carries (a seam's live Value, an image's bytes) cleared.
func lossyView(event domain.Event) domain.Event {
	switch e := event.(type) {
	case domain.ToolCallEvent:
		e.Call.Arguments = compacted(e.Call.Arguments)
		if e.Call.Malformed != nil && e.Call.Malformed.Err != nil {
			e.Call.Malformed = &domain.MalformedArguments{Err: errors.New(e.Call.Malformed.Err.Error()), Raw: e.Call.Malformed.Raw}
		}
		return e
	case domain.ApprovalEvent:
		e.Request.Arguments = compacted(e.Request.Arguments)
		if len(e.Rules) == 0 {
			e.Rules = nil
		}
		return e
	case domain.SeamClosedEvent:
		e.Value = nil
		if len(e.Fired) == 0 {
			e.Fired = nil
		}
		return e
	case domain.UpstreamAttemptEvent:
		e.TTFB, e.TTFT = e.TTFB.Truncate(time.Millisecond), e.TTFT.Truncate(time.Millisecond)
		e.Last, e.Duration = e.Last.Truncate(time.Millisecond), e.Duration.Truncate(time.Millisecond)
		return e
	case domain.WorkflowPhaseEvent:
		if len(e.Receipt.Fields) == 0 {
			e.Receipt.Fields = nil
		}
		return e
	case domain.ChildInterjectionEvent:
		e.Input.Images = imagesWithoutBytes(e.Input.Images)
		return e
	}
	return event
}

// compacted is raw after json.Compact, nil for no bytes.
func compacted(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var out bytes.Buffer
	if err := json.Compact(&out, raw); err != nil {
		return raw
	}
	return out.Bytes()
}

// imagesWithoutBytes copies images without their Data, nil for none.
func imagesWithoutBytes(images []domain.Image) []domain.Image {
	if len(images) == 0 {
		return nil
	}
	out := make([]domain.Image, 0, len(images))
	for _, img := range images {
		out = append(out, domain.Image{Name: img.Name, MediaType: img.MediaType})
	}
	return out
}

// TestDecodeCoversEveryToolSummaryVariant pins the summary decoder to the sealed sum: every type
// internal/domain marks as a ToolSummary — read off its source — has a decoder, and every decoder
// has a fixture in the round trip.
func TestDecodeCoversEveryToolSummaryVariant(t *testing.T) {
	t.Parallel()

	declared := declaredToolSummaryVariants(t)
	if len(declared) != len(summaryDecoders) {
		t.Errorf("internal/domain declares %d ToolSummary variants %v; Decode has %d summary decoders",
			len(declared), declared, len(summaryDecoders))
	}
	covered := map[string]bool{}
	for _, summary := range roundTripSummaries() {
		mirror := toolSummaryOf(summary)
		if mirror == nil {
			continue
		}
		encoded, err := json.Marshal(mirror)
		if err != nil {
			t.Fatalf("marshal %T: %v", summary, err)
		}
		var tag struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(encoded, &tag); err != nil {
			t.Fatalf("unmarshal %s: %v", encoded, err)
		}
		covered[tag.Kind] = true
	}
	for kind := range summaryDecoders {
		if !covered[kind] {
			t.Errorf("the round trip has no %q summary fixture", kind)
		}
	}
}

// declaredToolSummaryVariants reads internal/domain's source for every type with an isToolSummary
// method — the variants of the sealed ToolSummary sum.
func declaredToolSummaryVariants(t *testing.T) []string {
	t.Helper()

	files, err := filepath.Glob(filepath.Join("..", "domain", "*.go"))
	if err != nil {
		t.Fatalf("glob internal/domain: %v", err)
	}
	var names []string
	fileSet := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, decl := range file.Decls {
			method, isFunc := decl.(*ast.FuncDecl)
			if !isFunc || method.Recv == nil || method.Name.Name != "isToolSummary" {
				continue
			}
			if receiver, isIdent := method.Recv.List[0].Type.(*ast.Ident); isIdent {
				names = append(names, receiver.Name)
			}
		}
	}
	if len(names) == 0 {
		t.Fatal("found no ToolSummary variant in internal/domain; the source scan has stopped working")
	}
	return names
}

// TestDecodeNullDataKeepsTheIdentity covers the writer's fallback for arguments json.Marshal
// refused: the line's data is null, and Decode returns the kind's Event with the envelope's
// identity and every other member zero rather than failing the line.
func TestDecodeNullDataKeepsTheIdentity(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	w := New(&out, Options{Session: "sess-3", Now: fixedClock})
	w.Emit(domain.ToolCallEvent{
		EventBase: delegatedBase,
		Call:      domain.ToolCall{ID: "call-1", Tool: "read_file", Arguments: json.RawMessage(`{"truncated":`)},
	})

	decoded := decodeAll(t, &out)

	if !strings.HasSuffix(strings.TrimSpace(out.String()), `"data":null}`) {
		t.Fatalf("the fixture no longer writes null data: %s", out.String())
	}
	want := domain.ToolCallEvent{EventBase: delegatedBase}
	if len(decoded) != 1 || !reflect.DeepEqual(decoded[0].Event, want) {
		t.Errorf("decoded %+v, want one line carrying %#v", decoded, want)
	}
}

// TestDecodeSkipsAnUnknownKind pins ADR 0075 decision 10 for kinds: a kind a newer build added
// returns its envelope and no payload, and no error, so a reader moves on to the next line.
func TestDecodeSkipsAnUnknownKind(t *testing.T) {
	t.Parallel()

	line := `{"event":"future_kind","v":2,"seq":7,"time":"2026-09-07T12:00:00Z","session":"s",` +
		`"turn":1,"depth":0,"call_id":null,"run_id":null,"data":{"anything":[1,2]}}`

	got, err := Decode([]byte(line))

	if err != nil {
		t.Fatalf("Decode returned %v, want no error", err)
	}
	if got.Kind != "future_kind" || got.Seq != 7 || got.Session != "s" || got.Turn != 1 {
		t.Errorf("envelope = %+v, want kind future_kind, seq 7, session s, turn 1", got)
	}
	if got.Event != nil || got.Started != nil || got.Finished != nil || got.Prompt != nil {
		t.Errorf("an unknown kind decoded a payload: %+v", got)
	}
}

// TestDecodeIgnoresAnUnknownField pins ADR 0075 decision 10 for members: a member a newer build
// added, in the envelope or in data, is ignored and the known members still decode.
func TestDecodeIgnoresAnUnknownField(t *testing.T) {
	t.Parallel()

	line := `{"event":"token","v":2,"seq":1,"time":"2026-09-07T12:00:00Z","session":null,"turn":4,` +
		`"depth":0,"call_id":null,"run_id":null,"future":true,"data":{"text":"hi","future":{"x":1}}}`

	got, err := Decode([]byte(line))

	if err != nil {
		t.Fatalf("Decode returned %v, want no error", err)
	}
	want := domain.TokenEvent{EventBase: domain.EventBase{Turn: 4}, Text: "hi"}
	if !reflect.DeepEqual(got.Event, want) {
		t.Errorf("Event = %#v, want %#v", got.Event, want)
	}
}

// TestDecodeRejectsAMalformedLine covers each way a line can fail to be an Event line at all.
func TestDecodeRejectsAMalformedLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
	}{
		{name: "not JSON", line: `{"event":"token",`},
		{name: "not an object", line: `[1,2]`},
		{name: "no event kind", line: `{"v":2,"seq":1,"time":"2026-09-07T12:00:00Z","data":{}}`},
		{name: "unparsable time", line: `{"event":"token","v":2,"seq":1,"time":"noon","data":{"text":"x"}}`},
		{name: "member of the wrong type", line: `{"event":"token","v":2,"seq":1,"time":"2026-09-07T12:00:00Z","data":{"text":5}}`},
		{name: "cost that is not a decimal", line: `{"event":"usage","v":2,"seq":1,"time":"2026-09-07T12:00:00Z","data":{"cost":1e3}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got, err := Decode([]byte(tt.line)); err == nil {
				t.Errorf("Decode(%s) = %+v, want an error", tt.line, got)
			}
		})
	}
}

// TestDecodeRejectsANewerVersion pins the one version error: a `v` above lineVersion is a line whose
// shared members may mean something else, so Decode refuses it with ErrUnsupportedVersion.
func TestDecodeRejectsANewerVersion(t *testing.T) {
	t.Parallel()

	line := `{"event":"token","v":3,"seq":1,"time":"2026-09-07T12:00:00Z","session":null,"turn":1,` +
		`"depth":0,"call_id":null,"run_id":null,"data":{"text":"x"}}`

	_, err := Decode([]byte(line))

	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("Decode(v:3) error = %v, want ErrUnsupportedVersion", err)
	}
}

// TestDecodePromptLine pins the journal's prompt line (ADR 0103 decision 11): it decodes to its own
// Line variant, never to an Event, for a top-level prompt and an Interjection alike.
func TestDecodePromptLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		want Prompt
	}{
		{name: "top-level prompt", data: `{"text":"fix the build","interjection":false}`, want: Prompt{Text: "fix the build"}},
		{name: "interjection", data: `{"text":"use go 1.26","interjection":true}`, want: Prompt{Text: "use go 1.26", Interjection: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			line := `{"event":"prompt","v":2,"seq":2,"time":"2026-09-07T12:00:00Z","session":"s","turn":null,` +
				`"depth":null,"call_id":null,"run_id":null,"data":` + tt.data + `}`

			got, err := Decode([]byte(line))

			if err != nil {
				t.Fatalf("Decode returned %v", err)
			}
			if got.Kind != kindPrompt || got.Prompt == nil || *got.Prompt != tt.want || got.Event != nil {
				t.Errorf("Decode = %+v (prompt %+v), want a prompt line carrying %+v", got, got.Prompt, tt.want)
			}
		})
	}
}

// TestCostMicrosOfInvertsCostAmount pins the usage line's cost read: every amount CostAmount spells
// reads back to the exact millionths it was cut from.
func TestCostMicrosOfInvertsCostAmount(t *testing.T) {
	t.Parallel()

	for _, micros := range []int64{0, 1, 1234, 500_000, 1_000_000, 1_501_234, -2_500_000} {
		got, err := costMicrosOf(CostAmount(micros))
		if err != nil || got != micros {
			t.Errorf("costMicrosOf(%s) = %d, %v; want %d", CostAmount(micros), got, err, micros)
		}
	}
	if _, err := costMicrosOf("0.0000001"); err == nil {
		t.Errorf("costMicrosOf accepted seven decimal places, want an error")
	}
}
