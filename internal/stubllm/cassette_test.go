package stubllm

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// chatBody is a chat-completions request with the given system text, tool menu and messages,
// each message a raw JSON object.
func chatBody(system string, tools []string, messages ...string) []byte {
	all := append([]string{`{"role":"system","content":` + quote(system) + `}`}, messages...)
	menu := make([]string, 0, len(tools))
	for _, name := range tools {
		menu = append(menu, `{"type":"function","function":{"name":`+quote(name)+`}}`)
	}
	return []byte(`{"model":"m","stream":true,"tools":[` + strings.Join(menu, ",") +
		`],"messages":[` + strings.Join(all, ",") + `]}`)
}

// quote is s as a JSON string literal.
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s) + `"`
}

// assistantCall is an assistant message issuing one read_file call with the given arguments.
func assistantCall(args string) string {
	return `{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function",` +
		`"function":{"name":"read_file","arguments":` + quote(args) + `}}]}`
}

// toolResult is a tool message answering call_1 with content.
func toolResult(content string) string {
	return `{"role":"tool","tool_call_id":"call_1","content":` + quote(content) + `}`
}

func TestCassetteKeyNamesTheConversationNotTheBytes(t *testing.T) {
	t.Parallel()

	parentTools := []string{"read_file", "spawn_agent", "edit_file"}
	childTools := []string{"read_file", "edit_file"}
	user := `{"role":"user","content":"tests are failing — today is 2026-09-24"}`
	base := chatBody("scratch /tmp/a, session 1", parentTools,
		user, assistantCall(`{"path":"a.go"}`), toolResult("took 1.2s"))

	cases := []struct {
		name      string
		other     []byte
		wantEqual bool
	}{
		{
			name: "system text and tool-result content differ",
			other: chatBody("scratch /tmp/b, session 2", parentTools,
				user, assistantCall(`{"path":"a.go"}`), toolResult("took 3.4s")),
			wantEqual: true,
		},
		{
			name: "the tool menu arrives in another order",
			other: chatBody("scratch /tmp/a, session 1", []string{"edit_file", "spawn_agent", "read_file"},
				user, assistantCall(`{"path":"a.go"}`), toolResult("took 1.2s")),
			wantEqual: true,
		},
		{
			name: "the first user message carries another day's date",
			other: chatBody("scratch /tmp/a, session 1", parentTools,
				`{"role":"user","content":"tests are failing — today is 2026-10-01"}`,
				assistantCall(`{"path":"a.go"}`), toolResult("took 1.2s")),
			wantEqual: true,
		},
		{
			name: "an assistant tool call's arguments differ",
			other: chatBody("scratch /tmp/a, session 1", parentTools,
				user, assistantCall(`{"path":"b.go"}`), toolResult("took 1.2s")),
			wantEqual: false,
		},
		{
			name: "a child with its own tool set asks the same first message",
			other: chatBody("scratch /tmp/a, session 1", childTools,
				user, assistantCall(`{"path":"a.go"}`), toolResult("took 1.2s")),
			wantEqual: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := cassetteKey(chatCompletionsPath, tc.other) == cassetteKey(chatCompletionsPath, base)

			if got != tc.wantEqual {
				t.Errorf("keys equal = %t, want %t", got, tc.wantEqual)
			}
		})
	}
}

func TestCassetteKeySeparatesTheWires(t *testing.T) {
	t.Parallel()
	chat := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
	messages := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`)

	chatKey, messagesKey := cassetteKey(chatCompletionsPath, chat), cassetteKey(messagesPath, messages)

	if chatKey == messagesKey {
		t.Errorf("chat and messages keys both %q, want the wire in the key", chatKey)
	}
}

func TestCassetteKeyOfAnUndecodableBodyIsRaw(t *testing.T) {
	t.Parallel()

	key := cassetteKey(chatCompletionsPath, []byte("not json"))

	if !strings.HasPrefix(key, rawKeyPrefix) {
		t.Errorf("key = %q, want the %q prefix", key, rawKeyPrefix)
	}
}

func TestCassetteSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()
	want := &Cassette{
		Exchanges: []Exchange{{
			Key:    "abc",
			Status: 200,
			Header: map[string][]string{"Content-Type": {"text/event-stream"}},
			Chunks: []Chunk{
				{Offset: 12 * time.Millisecond, Bytes: []byte("data: {\"x\":1}\n\n")},
				{Offset: 30 * time.Millisecond, Bytes: []byte{0xe2, 0x88}}, // half a U+2212
			},
			Truncated: true,
		}},
		Probes: []ProbeReply{{Path: propsPath, Status: 404, ContentType: "text/plain", Body: "no"}},
	}
	path := filepath.Join(t.TempDir(), "hero.cassette.json")

	if err := want.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadCassette(path)

	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loaded = %+v, want %+v", got, want)
	}
}

func TestLoadCassetteRefusesAnotherVersion(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "old.json")
	if err := (&Cassette{}).Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	raw := strings.Replace(read(t, path), `"version": 1`, `"version": 99`, 1)
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	_, err := LoadCassette(path)

	if err == nil || !strings.Contains(err.Error(), "version 99") {
		t.Errorf("load error = %v, want a refusal naming version 99", err)
	}
}
