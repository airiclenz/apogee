package stubllm

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// designDoc is the file whose `## stubllm` section documents this package. Its example script
// is loaded below so the documentation cannot drift away from the format it describes.
const designDoc = "../../docs/design/test-drivers.md"

// TestScriptRoundTripsThroughYAML pins that the Go form and the on-disk form are one format:
// a fixture recorded by cmd/stubllm and a Script written in a test have to be interchangeable,
// which they only are if every field survives both directions.
func TestScriptRoundTripsThroughYAML(t *testing.T) {
	t.Parallel()

	want := Script{
		Model: "stub-model",
		Turns: []Turn{
			{
				Reasoning:      "thinking about it",
				ReasoningField: "reasoning",
				ToolCalls:      []ToolCall{{ID: "tc_1", Name: "list_dir", Arguments: `{"path":"."}`}},
			},
			{
				When:         &Match{LastMessage: "^weather", ToolResult: "list_dir", System: "^You are apogee"},
				Repeat:       true,
				Await:        "the forecast is in",
				Text:         "sunny",
				TokenDelay:   2 * time.Millisecond,
				ChunkRunes:   3,
				Usage:        &Usage{Prompt: 812, Completion: 14, Cached: 640},
				FinishReason: "length",
			},
			{HTTP: &HTTPReply{Status: 503, Body: "busy", Location: "/elsewhere", ContentType: "text/plain"}},
			{Hang: 250 * time.Millisecond},
			{},
			{
				ReasoningChunks: []string{"Weighing ", "the greeting."},
				Chunks:          []string{"Let me check. ", "<think>", "hidden", "</think>", "Hello!"},
				ToolCalls:       []ToolCall{{Name: "list_dir"}},
			},
			{
				Captures:  []Capture{{Name: "scratch", From: "system", Pattern: `scratch directory: (/\S+)`}},
				ToolCalls: []ToolCall{{Name: "terminal", Arguments: `{"command":"ls {{scratch}}"}`}},
			},
		},
	}

	data, err := Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatalf("parse %s: %v", data, err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip = %+v, want %+v (yaml was %s)", got, want, data)
	}
}

// TestParseRejectsAnUnplayableScript pins validation. Every case here is a fixture that would
// otherwise fail deep inside a driver test, where the real cause is a line of YAML.
func TestParseRejectsAnUnplayableScript(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "text and http together",
			yaml: "turns:\n  - text: hello\n    http: {status: 500}\n",
			want: "more than one of a completion (text, chunks and/or tool_calls), http and hang",
		},
		{
			name: "chunks beside text",
			yaml: "turns:\n  - text: hello\n    chunks: [hel, lo]\n",
			want: "sets both text and chunks",
		},
		{
			name: "chunks beside chunk_runes",
			yaml: "turns:\n  - chunks: [hel, lo]\n    chunk_runes: 2\n",
			want: "sets both chunks and chunk_runes",
		},
		{
			name: "reasoning_chunks beside reasoning",
			yaml: "turns:\n  - reasoning: hmm\n    reasoning_chunks: [hm, m]\n",
			want: "sets both reasoning and reasoning_chunks",
		},
		{
			name: "an empty chunk",
			yaml: "turns:\n  - chunks: [hel, \"\", lo]\n",
			want: "chunks[1] is empty",
		},
		{
			name: "http without a status",
			yaml: "turns:\n  - http: {body: nope}\n",
			want: "an http turn needs a status",
		},
		{
			name: "usage on an http turn",
			yaml: "turns:\n  - http: {status: 500}\n    usage: {prompt: 1, completion: 1}\n",
			want: "an http turn carries no reasoning or usage",
		},
		{
			name: "an empty when block",
			yaml: "turns:\n  - when: {}\n    text: hi\n",
			want: "a when block sets last_message, tool_result, system, or any combination",
		},
		{
			name: "a when regexp that does not compile",
			yaml: "turns:\n  - when: {last_message: \"(unclosed\"}\n    text: hi\n",
			want: "when.last_message is not a regexp",
		},
		{
			name: "a when.system regexp that does not compile",
			yaml: "turns:\n  - when: {system: \"(unclosed\"}\n    text: hi\n",
			want: "when.system is not a regexp",
		},
		{
			name: "a nameless tool call",
			yaml: "turns:\n  - tool_calls:\n      - arguments: '{}'\n",
			want: "tool call 0 needs a name",
		},
		{
			name: "no turns at all",
			yaml: "model: stub-model\n",
			want: "a script needs at least one turn",
		},
		{
			name: "an unknown key",
			yaml: "turns:\n  - chunk_rune: 3\n",
			want: "field chunk_rune not found",
		},
		// The `reasoning_field` cases. Every message names the key, because the mistake it
		// guards is a fixture that meant to change the wire spelling and silently did not.
		{
			name: "an unknown reasoning_field spelling",
			yaml: "turns:\n  - reasoning: thinking\n    reasoning_field: reasoning_text\n    text: hi\n",
			want: `reasoning_field is "reasoning_text" — a turn spells the thinking channel ` +
				"reasoning_content (the default) or reasoning",
		},
		{
			name: "reasoning_field on a turn with no reasoning",
			yaml: "turns:\n  - reasoning_field: reasoning\n    text: hi\n",
			want: "reasoning_field spells a turn's reasoning, and this turn has none",
		},
		{
			name: "reasoning_field on an http turn",
			yaml: "turns:\n  - http: {status: 503}\n    reasoning_field: reasoning\n",
			want: "an http turn carries no reasoning, so it carries no reasoning_field",
		},
		{
			name: "reasoning_field on a hang turn",
			yaml: "turns:\n  - hang: 10ms\n    reasoning_field: reasoning\n",
			want: "a hang turn carries no reasoning, so it carries no reasoning_field",
		},
		{
			name: "a capture pattern with no group",
			yaml: "turns:\n  - captures: [{name: p, from: system, pattern: 'scratch'}]\n    text: hi\n",
			want: "pattern has 0 capture groups, want exactly one",
		},
		{
			name: "a capture pattern with two groups",
			yaml: "turns:\n  - captures: [{name: p, from: system, pattern: '(a)(b)'}]\n    text: hi\n",
			want: "pattern has 2 capture groups, want exactly one",
		},
		{
			name: "a capture pattern that does not compile",
			yaml: "turns:\n  - captures: [{name: p, from: system, pattern: '(unclosed'}]\n    text: hi\n",
			want: "pattern is not a regexp",
		},
		{
			name: "a capture reading an unknown source",
			yaml: "turns:\n  - captures: [{name: p, from: prompt, pattern: '(.)'}]\n    text: hi\n",
			want: `from is "prompt", want system or last_message`,
		},
		{
			name: "a nameless capture",
			yaml: "turns:\n  - captures: [{name: '', from: system, pattern: '(.)'}]\n    text: hi\n",
			want: "capture 0: needs a name",
		},
		{
			name: "two captures with one name",
			yaml: "turns:\n  - captures: [{name: p, from: system, pattern: '(a)'}, " +
				"{name: p, from: system, pattern: '(b)'}]\n    text: hi\n",
			want: `capture 1: duplicate name "p"`,
		},
		{
			name: "a placeholder naming no capture",
			yaml: "turns:\n  - captures: [{name: here, from: system, pattern: '(.)'}]\n" +
				"    tool_calls:\n      - {name: terminal, arguments: 'ls {{there}}'}\n",
			want: "turn 0: {{there}} names no capture on this turn",
		},
		{
			name: "captures on an http turn",
			yaml: "turns:\n  - captures: [{name: p, from: system, pattern: '(.)'}]\n    http: {status: 500}\n",
			want: "an http turn carries no captures",
		},
		{
			name: "captures on a hang turn",
			yaml: "turns:\n  - captures: [{name: p, from: system, pattern: '(.)'}]\n    hang: 10ms\n",
			want: "a hang turn carries no captures",
		},
		// The terminator cases: `cut` and `error` are refused with each other and with the two
		// kinds that never start a stream — and the one-kind rule's wording is untouched, because
		// a terminator is not a kind.
		{
			name: "cut and http together",
			yaml: "turns:\n  - cut: {after_runes: 3}\n    http: {status: 500}\n",
			want: "a cut ends a stream, and an http turn never starts one",
		},
		{
			name: "cut and hang together",
			yaml: "turns:\n  - cut: {after_runes: 3}\n    hang: 10ms\n",
			want: "a cut ends a stream, and a hang turn never starts one",
		},
		{
			name: "cut and error together",
			yaml: "turns:\n  - text: hi\n    cut: {after_runes: 1}\n    error: {message: gone}\n",
			want: "sets both cut and error — a stream ends one way",
		},
		{
			name: "error and http together",
			yaml: "turns:\n  - error: {code: 502, message: gone}\n    http: {status: 500}\n",
			want: "a error ends a stream, and an http turn never starts one",
		},
		{
			name: "usage on a cut turn",
			yaml: "turns:\n  - text: hi\n    cut: {after_runes: 1}\n    usage: {prompt: 1, completion: 1}\n",
			want: "a cut turn never reaches the terminator, so it carries no usage or finish_reason",
		},
		{
			name: "a negative cut",
			yaml: "turns:\n  - text: hi\n    cut: {after_runes: -1}\n",
			want: "cut.after_runes cannot be negative",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse([]byte(tc.yaml))

			if err == nil {
				t.Fatalf("parse accepted %q, want it refused", tc.yaml)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestTerminatorRidesAnOrdinaryTurn pins the other side of the terminator refusals: `cut` and
// `error` are legal on a text, tool-call or empty turn, and the parsed Turn carries them with
// the `error` code defaulting to 502.
func TestTerminatorRidesAnOrdinaryTurn(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		yaml string
	}{
		{name: "cut on a text turn", yaml: "turns:\n  - text: hello\n    cut: {after_runes: 3}\n"},
		{name: "cut on a tool-call turn", yaml: "turns:\n  - tool_calls: [{name: list_dir}]\n    cut: {after_runes: 0}\n"},
		{name: "cut on an empty turn", yaml: "turns:\n  - cut: {after_runes: 0}\n"},
		{name: "error on a text turn", yaml: "turns:\n  - text: hello\n    error: {code: 429, message: slow down}\n"},
		{name: "error on an empty turn", yaml: "turns:\n  - error: {message: gone}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := Parse([]byte(tc.yaml)); err != nil {
				t.Fatalf("parse refused %q: %v", tc.yaml, err)
			}
		})
	}

	t.Run("error code defaults to 502", func(t *testing.T) {
		t.Parallel()

		script, err := Parse([]byte("turns:\n  - error: {message: gone}\n"))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if got := script.Turns[0].Error.code(); got != 502 {
			t.Errorf("code = %d, want 502", got)
		}
	})
}

// TestNarratingToolCallTurnIsOneCompletion pins the shape a narrating model sends: text and
// tool calls on ONE turn are one completion, not two kinds, and the turn ends on tool_calls
// exactly as a call without narration does.
func TestNarratingToolCallTurnIsOneCompletion(t *testing.T) {
	t.Parallel()

	script, err := Parse([]byte("turns:\n  - text: let me look\n    tool_calls: [{name: list_dir}]\n"))
	if err != nil {
		t.Fatalf("parse refused a narrating tool-call turn: %v", err)
	}

	turn := script.Turns[0]
	if turn.kindCount() != 1 {
		t.Errorf("kindCount = %d, want 1 — text and tool calls are one completion", turn.kindCount())
	}
	if got := turn.finishReason(); got != "tool_calls" {
		t.Errorf("finish reason = %q, want tool_calls", got)
	}
}

// TestChunksPlaceTheStreamBoundaries pins the hand-placed split: a chunks list streams one delta
// per element, in order, and reads back as their concatenation wherever the whole content is
// wanted — the non-streamed reply, the cut point, the kind rule.
func TestChunksPlaceTheStreamBoundaries(t *testing.T) {
	t.Parallel()

	script, err := Parse([]byte("turns:\n  - chunks: [\"Let me check. \", \"<think>\", \"hidden\", \"</think>\", \"Hello!\"]\n    reasoning_chunks: [\"Weighing \", \"the greeting.\"]\n"))
	if err != nil {
		t.Fatalf("parse refused a chunks turn: %v", err)
	}

	turn := script.Turns[0]
	if !turn.isCompletion() {
		t.Error("a chunks turn is not a completion — chunks IS the text")
	}
	wantContent := []string{"Let me check. ", "<think>", "hidden", "</think>", "Hello!"}
	if got := turn.contentDeltas(); !reflect.DeepEqual(got, wantContent) {
		t.Errorf("content deltas = %q, want the hand-placed %q", got, wantContent)
	}
	if got := turn.content(); got != "Let me check. <think>hidden</think>Hello!" {
		t.Errorf("content = %q, want the chunks joined", got)
	}
	wantReasoning := []string{"Weighing ", "the greeting."}
	if got := turn.reasoningDeltas(); !reflect.DeepEqual(got, wantReasoning) {
		t.Errorf("reasoning deltas = %q, want the hand-placed %q", got, wantReasoning)
	}
	if got := turn.reasoning(); got != "Weighing the greeting." {
		t.Errorf("reasoning = %q, want the reasoning chunks joined", got)
	}
}

// TestEmptyReplyTurnIsLegal pins the one turn that looks like a mistake and is not: a turn with
// no text, no calls, no http and no hang is how an abandoned reply is scripted.
func TestEmptyReplyTurnIsLegal(t *testing.T) {
	t.Parallel()

	script, err := Parse([]byte("turns:\n  - {}\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got := script.Turns[0].finishReason(); got != "stop" {
		t.Errorf("finish reason = %q, want stop", got)
	}
}

// TestSystemOnlyWhenBlockParses pins the other side of the when.system compile: a when block that
// sets only system, with a regexp that compiles, is a complete and legal match.
func TestSystemOnlyWhenBlockParses(t *testing.T) {
	t.Parallel()

	script, err := Parse([]byte("turns:\n  - when: {system: \"you are .*\"}\n    text: hi\n"))
	if err != nil {
		t.Fatalf("parse refused a system-only when block: %v", err)
	}

	if got := script.Turns[0].When.System; got != "you are .*" {
		t.Errorf("when.system = %q, want %q", got, "you are .*")
	}
}

// TestDesignDocExampleScriptParses pins the example in docs/design/test-drivers.md against the
// parser it documents — a documented format that no longer loads is worse than none.
func TestDesignDocExampleScriptParses(t *testing.T) {
	t.Parallel()

	example := designDocExample(t, "## stubllm")

	script, err := Parse([]byte(example))
	if err != nil {
		t.Fatalf("the design doc's example script does not parse: %v\n%s", err, example)
	}

	if script.Model == "" || len(script.Turns) < 2 {
		t.Errorf("example script = %+v, want a model and the turns the doc describes", script)
	}
}

// TestDesignDocCapturesExampleParses pins the `### Captures` example the same way. A capture is
// the one part of the format a fixture author copies verbatim out of the doc, so an example
// that no longer parses would be read as the format itself.
func TestDesignDocCapturesExampleParses(t *testing.T) {
	t.Parallel()

	example := designDocExample(t, "### Captures")

	script, err := Parse([]byte(example))
	if err != nil {
		t.Fatalf("the design doc's captures example does not parse: %v\n%s", err, example)
	}

	if len(script.Turns) != 1 || len(script.Turns[0].Captures) != 1 {
		t.Fatalf("captures example = %+v, want the one turn with the one capture the doc shows", script)
	}
	if got := script.Turns[0].Captures[0]; got.Name != "scratch" || got.From != captureFromSystem {
		t.Errorf("capture = %+v, want the scratch capture read from the system prompt", got)
	}
}

// designDocExample returns the first YAML fence under heading in the design doc, bounded by the
// next top-level section so a fence further down the file cannot stand in for a missing one.
func designDocExample(t *testing.T, heading string) string {
	t.Helper()

	raw, err := os.ReadFile(designDoc)
	if err != nil {
		t.Fatalf("read the design doc: %v", err)
	}
	_, after, found := strings.Cut(string(raw), heading)
	if !found {
		t.Fatalf("%s has no %s section", designDoc, heading)
	}
	body, _, _ := strings.Cut(after, "\n## ")

	_, fenced, found := strings.Cut(body, "```yaml\n")
	if !found {
		t.Fatalf("the %s section carries no yaml example", heading)
	}
	example, _, _ := strings.Cut(fenced, "```")
	return example
}

// TestLoadReadsAFixtureFromDisk pins the path fixtures actually arrive by, and that a broken
// one names the file it came from.
func TestLoadReadsAFixtureFromDisk(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	good := filepath.Join(dir, "good.yaml")
	if err := os.WriteFile(good, []byte("model: stub-model\nturns:\n  - text: hi\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("turns:\n  - text: hi\n    http: {status: 500}\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	script, err := Load(good)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if script.Model != "stub-model" || len(script.Turns) != 1 {
		t.Errorf("script = %+v, want the one scripted turn", script)
	}

	_, err = Load(bad)
	if err == nil || !strings.Contains(err.Error(), "bad.yaml") {
		t.Errorf("error = %v, want it to name bad.yaml", err)
	}
}
