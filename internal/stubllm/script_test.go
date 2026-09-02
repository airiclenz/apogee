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
			want: "more than one of text, tool_calls, http and hang",
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
