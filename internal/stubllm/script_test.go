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
				Reasoning: "thinking about it",
				ToolCalls: []ToolCall{{ID: "tc_1", Name: "list_dir", Arguments: `{"path":"."}`}},
			},
			{
				When:         &Match{LastMessage: "^weather", ToolResult: "list_dir"},
				Repeat:       true,
				Text:         "sunny",
				TokenDelay:   2 * time.Millisecond,
				ChunkRunes:   3,
				Usage:        &Usage{Prompt: 812, Completion: 14, Cached: 640},
				FinishReason: "length",
			},
			{HTTP: &HTTPReply{Status: 503, Body: "busy", Location: "/elsewhere", ContentType: "text/plain"}},
			{Hang: 250 * time.Millisecond},
			{},
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
			want: "a when block sets last_message, tool_result, or both",
		},
		{
			name: "a when regexp that does not compile",
			yaml: "turns:\n  - when: {last_message: \"(unclosed\"}\n    text: hi\n",
			want: "when.last_message is not a regexp",
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

// TestDesignDocExampleScriptParses pins the example in docs/design/test-drivers.md against the
// parser it documents — a documented format that no longer loads is worse than none.
func TestDesignDocExampleScriptParses(t *testing.T) {
	t.Parallel()

	example := designDocExample(t)

	script, err := Parse([]byte(example))
	if err != nil {
		t.Fatalf("the design doc's example script does not parse: %v\n%s", err, example)
	}

	if script.Model == "" || len(script.Turns) < 2 {
		t.Errorf("example script = %+v, want a model and the turns the doc describes", script)
	}
}

// designDocExample returns the first YAML fence in the design doc's `## stubllm` section.
func designDocExample(t *testing.T) string {
	t.Helper()

	raw, err := os.ReadFile(designDoc)
	if err != nil {
		t.Fatalf("read the design doc: %v", err)
	}
	_, after, found := strings.Cut(string(raw), "## stubllm")
	if !found {
		t.Fatalf("%s has no ## stubllm section", designDoc)
	}
	body, _, _ := strings.Cut(after, "\n## tuitest")

	_, fenced, found := strings.Cut(body, "```yaml\n")
	if !found {
		t.Fatalf("the ## stubllm section carries no yaml example")
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
