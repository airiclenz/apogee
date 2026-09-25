package stubllm

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// replayChat is a chat-completions request carrying one user message.
func replayChat(prompt string) string {
	return fmt.Sprintf(`{"model":"m","stream":true,"messages":[{"role":"user","content":%q}]}`, prompt)
}

// chatKey is the cassette key replayChat(prompt) is filed under.
func chatKey(prompt string) string {
	return cassetteKey(chatCompletionsPath, []byte(replayChat(prompt)))
}

// textExchange is a 200 exchange under key whose body is the given chunks, each at its offset.
func textExchange(key string, chunks ...Chunk) Exchange {
	return Exchange{
		Key:    key,
		Status: http.StatusOK,
		Header: http.Header{"Content-Type": {"text/event-stream"}},
		Chunks: chunks,
	}
}

// chunkAt is a chunk of text at an offset.
func chunkAt(offset time.Duration, text string) Chunk {
	return Chunk{Offset: offset, Bytes: []byte(text)}
}

// replayServer mounts a Replayer for c at pace on a loopback listener.
func replayServer(t *testing.T, c *Cassette, pace float64) (string, *Replayer) {
	t.Helper()

	replayer := NewReplayer(c, pace)
	server := httptest.NewServer(replayer)
	t.Cleanup(server.Close)
	return server.URL, replayer
}

// fetch makes one request and returns the status and the whole body.
func fetch(t *testing.T, ctx context.Context, method, url, body string) (int, string) {
	t.Helper()

	request, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read reply: %v", err)
	}
	return resp.StatusCode, string(raw)
}

func TestReplayerReproducesACapturedStreamThroughTheProvider(t *testing.T) {
	t.Parallel()
	origin := New(t, Script{Model: "live-model", Turns: []Turn{
		{
			Text:       "one two three four five",
			ChunkRunes: 4,
			TokenDelay: time.Millisecond,
			Usage:      &Usage{Prompt: 120, Completion: 9, Cached: 64},
		},
		{
			ToolCalls: []ToolCall{{Name: "read_file", Arguments: `{"path":"go.mod"}`}},
		},
	}})
	recorder, err := NewCassetteRecorder(origin.URL, "")
	if err != nil {
		t.Fatalf("new cassette recorder: %v", err)
	}
	proxy := httptest.NewServer(recorder)
	t.Cleanup(proxy.Close)

	prompts := []string{"say the numbers", "open go.mod"}
	captured := make([]replayed, 0, len(prompts))
	for _, prompt := range prompts {
		captured = append(captured, observe(t, proxy.URL, origin.Model, prompt))
	}
	replayURL, _ := replayServer(t, recorder.Cassette(), 0)

	for i, prompt := range prompts {
		if got := observe(t, replayURL, origin.Model, prompt); !got.same(captured[i]) {
			t.Errorf("prompt %q: replayed %+v, want the captured %+v", prompt, got, captured[i])
		}
	}
}

func TestReplayerServesAKeysQueueThenRepeatsItsLast(t *testing.T) {
	t.Parallel()
	key := chatKey("again")
	url, _ := replayServer(t, &Cassette{Exchanges: []Exchange{
		textExchange(key, chunkAt(0, "first")),
		textExchange(chatKey("other"), chunkAt(0, "unrelated")),
		textExchange(key, chunkAt(0, "second")),
	}}, 0)

	want := []string{"first", "second", "second"}
	for i, expected := range want {
		status, body := fetch(t, t.Context(), http.MethodPost, url+chatCompletionsPath, replayChat("again"))
		if status != http.StatusOK || body != expected {
			t.Errorf("request %d = %d %q, want 200 %q", i+1, status, body, expected)
		}
	}
}

func TestReplayerReplaysTheRecordedStatusAndHeader(t *testing.T) {
	t.Parallel()
	exchange := textExchange(chatKey("limited"), chunkAt(0, `{"error":"slow down"}`))
	exchange.Status = http.StatusTooManyRequests
	exchange.Header = http.Header{"Content-Type": {"application/json"}, "Retry-After": {"7"}}
	url, _ := replayServer(t, &Cassette{Exchanges: []Exchange{exchange}}, 0)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		url+chatCompletionsPath, strings.NewReader(replayChat("limited")))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want the recorded 429", resp.StatusCode)
	}
	if got := resp.Header.Get("Retry-After"); got != "7" {
		t.Errorf("Retry-After = %q, want the recorded 7", got)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want the recorded application/json", got)
	}
}

func TestReplayerRefusesAnUnknownKey(t *testing.T) {
	t.Parallel()
	url, replayer := replayServer(t, &Cassette{Exchanges: []Exchange{
		textExchange(chatKey("known"), chunkAt(0, "hi")),
	}}, 0)
	var mu sync.Mutex
	var logged []string
	replayer.logf = func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		logged = append(logged, fmt.Sprintf(format, args...))
	}

	// Posted raw: the provider client would retry a 500 before a test could see it.
	status, body := fetch(t, t.Context(), http.MethodPost, url+chatCompletionsPath, replayChat("never captured"))
	key := chatKey("never captured")

	if status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", status)
	}
	for _, want := range []string{key, "never captured"} {
		if !strings.Contains(body, want) {
			t.Errorf("body = %q, want it to name %q", body, want)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(logged) != 1 || !strings.Contains(logged[0], key) || !strings.Contains(logged[0], "never captured") {
		t.Errorf("log = %q, want one line naming the key and the first user message", logged)
	}
}

func TestReplayerAnswersProbesFromTheCassette(t *testing.T) {
	t.Parallel()
	models := `{"object":"list","data":[{"id":"live-model","object":"model"}]}`
	url, _ := replayServer(t, &Cassette{Probes: []ProbeReply{
		{Path: modelsPath, Status: http.StatusOK, ContentType: "application/json", Body: models},
		{Path: propsPath, Status: http.StatusNotFound, ContentType: "text/plain", Body: "not found"},
	}}, 0)

	cases := []struct {
		path   string
		status int
		body   string
	}{
		{modelsPath, http.StatusOK, models},
		{propsPath, http.StatusNotFound, "not found"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			status, body := fetch(t, t.Context(), http.MethodGet, url+tc.path, "")
			if status != tc.status || body != tc.body {
				t.Errorf("GET %s = %d %q, want the stored %d %q", tc.path, status, body, tc.status, tc.body)
			}
		})
	}
	t.Run("a probe the capture never saw is a 404", func(t *testing.T) {
		empty, _ := replayServer(t, &Cassette{}, 0)
		if status, _ := fetch(t, t.Context(), http.MethodGet, empty+modelsPath, ""); status != http.StatusNotFound {
			t.Errorf("status = %d, want 404", status)
		}
	})
}

func TestReplayerPacesChunksByTheirOffsets(t *testing.T) {
	t.Parallel()
	t.Run("pace 0 streams without delay", func(t *testing.T) {
		t.Parallel()
		url, _ := replayServer(t, &Cassette{Exchanges: []Exchange{
			textExchange(chatKey("fast"), chunkAt(0, "a"), chunkAt(time.Hour, "b")),
		}}, 0)
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()

		if _, body := fetch(t, ctx, http.MethodPost, url+chatCompletionsPath, replayChat("fast")); body != "ab" {
			t.Errorf("body = %q, want ab", body)
		}
	})
	t.Run("a non-zero pace scales the offsets", func(t *testing.T) {
		t.Parallel()
		const offset = 800 * time.Millisecond
		const pace = 0.25
		url, _ := replayServer(t, &Cassette{Exchanges: []Exchange{
			textExchange(chatKey("paced"), chunkAt(0, "a"), chunkAt(offset, "b")),
		}}, pace)

		began := time.Now()
		fetch(t, t.Context(), http.MethodPost, url+chatCompletionsPath, replayChat("paced"))
		elapsed := time.Since(began)

		if scaled := time.Duration(float64(offset) * pace); elapsed < scaled {
			t.Errorf("replay took %s, want at least the paced offset %s", elapsed, scaled)
		}
		if elapsed >= offset {
			t.Errorf("replay took %s, want it under the unscaled offset %s", elapsed, offset)
		}
	})
}

func TestReplayerStreamsDifferentKeysConcurrently(t *testing.T) {
	t.Parallel()
	url, _ := replayServer(t, &Cassette{Exchanges: []Exchange{
		textExchange(chatKey("slow"), chunkAt(0, "slow-head\n"), chunkAt(time.Hour, "slow-tail\n")),
		textExchange(chatKey("quick"), chunkAt(0, "quick")),
	}}, 1)

	// The slow key's stream is held open mid-reply: its first chunk is read, its second is an
	// hour away.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		url+chatCompletionsPath, strings.NewReader(replayChat("slow")))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("post slow: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if head, err := bufio.NewReader(resp.Body).ReadString('\n'); err != nil || head != "slow-head\n" {
		t.Fatalf("slow head = %q (%v), want slow-head", head, err)
	}

	quickCtx, quickCancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer quickCancel()
	if status, body := fetch(t, quickCtx, http.MethodPost, url+chatCompletionsPath, replayChat("quick")); status != http.StatusOK || body != "quick" {
		t.Errorf("quick = %d %q while slow streams, want 200 quick", status, body)
	}
}

func TestReplayerDropsTheConnectionOfATruncatedExchange(t *testing.T) {
	t.Parallel()
	exchange := textExchange(chatKey("cut"), chunkAt(0, "data: {\"partial\":true}\n\n"))
	exchange.Truncated = true
	url, _ := replayServer(t, &Cassette{Exchanges: []Exchange{exchange}}, 0)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		url+chatCompletionsPath, strings.NewReader(replayChat("cut")))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if _, err := io.ReadAll(resp.Body); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("read error = %v, want the unexpected EOF of a dropped stream", err)
	}
}
