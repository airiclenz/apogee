package stubllm

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/provider"
)

// recordedDelay is the pacing the recorded upstream streams at. It is comfortably above the
// scheduler noise a -race run adds, so the measured median stays inside the tolerance below
// without the test having to sleep for a noticeable time.
const recordedDelay = 10 * time.Millisecond

// TestRecorderReplaysWhatItRecorded is the recorder's whole contract in one pass: a session
// driven through the proxy produces a fixture that, played back by a second Server, answers a
// client with the same content, the same calls, the same accounting and comparable pacing.
// Anything less and a recorded fixture would be a plausible-looking file rather than a replay.
func TestRecorderReplaysWhatItRecorded(t *testing.T) {
	t.Parallel()

	origin := New(t, Script{Model: "rec-model", Turns: []Turn{
		{
			Reasoning:  "The user wants the file list.",
			ToolCalls:  []ToolCall{{Name: "list_dir", Arguments: `{"path":"."}`}},
			TokenDelay: recordedDelay,
		},
		{
			Text:       "one two three four five six seven",
			ChunkRunes: 3,
			TokenDelay: recordedDelay,
			Usage:      &Usage{Prompt: 812, Completion: 14, Cached: 640},
		},
	}})
	path := filepath.Join(t.TempDir(), "fixture.yaml")
	proxy := recorderProxy(t, origin.URL, path)

	prompts := []string{"look around", "summarise it"}
	recorded := make([]replayed, 0, len(prompts))
	for _, prompt := range prompts {
		recorded = append(recorded, observe(t, proxy.URL, origin.Model, prompt))
	}
	if err := proxy.recorder.Close(); err != nil {
		t.Fatalf("close recorder: %v", err)
	}

	script, err := Load(path)
	if err != nil {
		t.Fatalf("the recorded fixture does not load: %v\n%s", err, read(t, path))
	}

	t.Run("the fixture names where it came from", func(t *testing.T) {
		if header := read(t, path); !strings.Contains(header, origin.URL) {
			t.Errorf("fixture = %q, want a header naming the upstream %s", header, origin.URL)
		}
	})

	t.Run("shape", func(t *testing.T) {
		if script.Model != origin.Model || len(script.Turns) != len(prompts) {
			t.Fatalf("script = %+v, want model %q and %d turns", script, origin.Model, len(prompts))
		}
		if got := script.Turns[1]; got.ChunkRunes != 3 {
			t.Errorf("chunk_runes = %d, want the 3 the upstream streamed in", got.ChunkRunes)
		}
		if got := script.Turns[1].Usage; got == nil || *got != (Usage{Prompt: 812, Completion: 14, Cached: 640}) {
			t.Errorf("usage = %+v, want the upstream's accounting including the cached share", got)
		}
	})

	t.Run("pacing within half the original", func(t *testing.T) {
		for i, turn := range script.Turns {
			low, high := recordedDelay/2, recordedDelay*3/2
			if turn.TokenDelay < low || turn.TokenDelay > high {
				t.Errorf("turn %d token_delay = %s, want %s..%s", i, turn.TokenDelay, low, high)
			}
		}
	})

	t.Run("matchers select the requests they were recorded from", func(t *testing.T) {
		for i, turn := range script.Turns {
			if turn.When == nil {
				t.Fatalf("turn %d has no when block", i)
			}
			matcher := regexp.MustCompile(turn.When.LastMessage)
			if !matcher.MatchString(prompts[i]) {
				t.Errorf("turn %d matcher %q does not match %q", i, turn.When.LastMessage, prompts[i])
			}
		}
	})

	t.Run("replay", func(t *testing.T) {
		replay := New(t, script)
		for i, prompt := range prompts {
			if got := observe(t, replay.URL, replay.Model, prompt); !got.same(recorded[i]) {
				t.Errorf("replay of %q = %+v, want %+v", prompt, got, recorded[i])
			}
		}
		replay.AssertConsumed(t)
	})
}

// TestRecorderRecordsAFailureAsAnHTTPTurn pins that the shapes a fixture is most wanted for —
// the ones a developer cannot make a real server produce on demand — survive recording.
func TestRecorderRecordsAFailureAsAnHTTPTurn(t *testing.T) {
	t.Parallel()

	origin := New(t, Script{Model: "rec-model", Turns: []Turn{{
		HTTP: &HTTPReply{Status: http.StatusPermanentRedirect, Location: "http://elsewhere/v1", Body: "moved"},
	}}})
	path := filepath.Join(t.TempDir(), "fixture.yaml")
	proxy := recorderProxy(t, origin.URL, path)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	if status := postTo(t, client, proxy.URL, origin.Model, "hi", false); status != http.StatusPermanentRedirect {
		t.Fatalf("status through the proxy = %d, want 308", status)
	}
	if err := proxy.recorder.Close(); err != nil {
		t.Fatalf("close recorder: %v", err)
	}

	script, err := Load(path)
	if err != nil {
		t.Fatalf("the recorded fixture does not load: %v", err)
	}
	got := script.Turns[0].HTTP
	if got == nil || got.Status != http.StatusPermanentRedirect || got.Location != "http://elsewhere/v1" {
		t.Fatalf("http turn = %+v, want the 308 and its Location", got)
	}
	if got.Body != "moved" {
		t.Errorf("http turn body = %q, want %q", got.Body, "moved")
	}
}

// TestRecorderRecordsANonStreamedReplyAsText pins the other reply shape: a whole JSON
// completion becomes a text turn with no pacing, because there were no chunks to space out.
func TestRecorderRecordsANonStreamedReplyAsText(t *testing.T) {
	t.Parallel()

	origin := New(t, Script{Model: "rec-model", Turns: []Turn{{Text: "whole reply"}}})
	path := filepath.Join(t.TempDir(), "fixture.yaml")
	proxy := recorderProxy(t, origin.URL, path)

	postTo(t, http.DefaultClient, proxy.URL, origin.Model, "hi", false)
	if err := proxy.recorder.Close(); err != nil {
		t.Fatalf("close recorder: %v", err)
	}

	script, err := Load(path)
	if err != nil {
		t.Fatalf("the recorded fixture does not load: %v", err)
	}
	if got := script.Turns[0]; got.Text != "whole reply" || got.TokenDelay != 0 {
		t.Errorf("turn = %+v, want the whole text and no token_delay", got)
	}
}

// TestRecorderRefusesAnUnusableConfiguration pins the two mistakes worth catching before a
// session is driven through a recorder rather than after it.
func TestRecorderRefusesAnUnusableConfiguration(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, upstream, out string }{
		{name: "no scheme", upstream: "127.0.0.1:1111", out: "fixture.yaml"},
		{name: "no output path", upstream: "http://127.0.0.1:1111", out: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := NewRecorder(tc.upstream, tc.out); err == nil {
				t.Fatal("NewRecorder accepted an unusable configuration")
			}
		})
	}
}

// TestRecorderReportsAnEmptyRecording pins that closing a recorder nothing was driven through
// says so, instead of writing an empty fixture that fails much later as "no turn for request 1".
func TestRecorderReportsAnEmptyRecording(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "fixture.yaml")
	recorder, err := NewRecorder("http://127.0.0.1:1", path)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}

	if err := recorder.Close(); err == nil {
		t.Fatal("closing an empty recording succeeded, want an error naming the upstream")
	}
	if _, err := os.Stat(path); err == nil {
		t.Errorf("%s was written for an empty recording", path)
	}
}

// recording is a Recorder mounted on a loopback listener: the address a client is pointed at,
// and the recorder behind it whose Close writes the fixture.
type recording struct {
	URL      string
	recorder *Recorder
}

// recorderProxy starts a recording proxy in front of upstream, writing to out on Close.
func recorderProxy(t *testing.T, upstream, out string) recording {
	t.Helper()

	recorder, err := NewRecorder(upstream, out)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}
	server := httptest.NewServer(recorder)
	t.Cleanup(server.Close)
	return recording{URL: server.URL, recorder: recorder}
}

// replayed is what a client saw off one streamed reply — the level a recording has to
// reproduce. Comparing two of these is what "the same reply" means to the code under test.
type replayed struct {
	text     string
	thinking string
	calls    []provider.ToolCall
	usage    provider.Usage
	finish   string
}

// same reports whether two observed replies are the same reply.
func (r replayed) same(other replayed) bool {
	if r.text != other.text || r.thinking != other.thinking {
		return false
	}
	if r.finish != other.finish || r.usage != other.usage {
		return false
	}
	if len(r.calls) != len(other.calls) {
		return false
	}
	for i := range r.calls {
		if r.calls[i] != other.calls[i] {
			return false
		}
	}
	return true
}

// observe drives one streamed prompt at a base URL with the real provider client and reduces
// the stream to what the caller saw.
func observe(t *testing.T, baseURL, model, prompt string) replayed {
	t.Helper()

	var seen replayed
	client := provider.NewClient(baseURL, model)
	for delta := range client.Stream(t.Context(), provider.Request{
		Messages: []provider.Message{{Role: "user", Content: prompt}},
	}) {
		switch delta.Kind {
		case provider.DeltaError:
			t.Fatalf("stream error: %s", delta.Err)
		case provider.DeltaContent:
			seen.text += delta.Content
		case provider.DeltaThinking:
			seen.thinking += delta.Thinking
		case provider.DeltaToolCall:
			seen.calls = append(seen.calls, *delta.ToolCall)
		case provider.DeltaDone:
			seen.finish = delta.FinishReason
			if delta.Usage != nil {
				seen.usage = *delta.Usage
			}
		}
	}
	return seen
}

// postTo sends one completion request to a base URL and returns the status, for the paths the
// provider client would follow or reject before a test could see them.
func postTo(t *testing.T, client *http.Client, baseURL, model, prompt string, stream bool) int {
	t.Helper()

	body := fmt.Sprintf(`{"model":%q,"stream":%t,"messages":[{"role":"user","content":%q}]}`,
		model, stream, prompt)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		baseURL+"/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(request)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := readAll(resp); err != nil {
		t.Fatalf("read reply: %v", err)
	}
	return resp.StatusCode
}

// read returns a file's contents for a failure message.
func read(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}
