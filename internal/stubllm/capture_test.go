package stubllm

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// chunkGap is how long the streaming upstream waits between its chunks — long enough that each
// write reaches the proxy as its own read even under -race.
const chunkGap = 25 * time.Millisecond

// keyEnvVar is the environment variable the auth test hands the recorder.
const keyEnvVar = "STUBLLM_CAPTURE_TEST_KEY"

// simpleChat is a minimal chat-completions request body.
const simpleChat = `{"model":"m","messages":[{"role":"user","content":"hi"}]}`

// capturing is a CassetteRecorder mounted on a loopback listener in front of an upstream.
type capturing struct {
	URL      string
	recorder *CassetteRecorder
}

// captureProxy starts an upstream serving handler and a CassetteRecorder in front of it. The
// upstream is a raw handler, not a Script, because what these tests pin is the recorder's
// fidelity to bytes, timing and headers a Script does not let a test place exactly.
func captureProxy(t *testing.T, keyEnv string, handler http.HandlerFunc) capturing {
	t.Helper()

	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	recorder, err := NewCassetteRecorder(upstream.URL, keyEnv)
	if err != nil {
		t.Fatalf("new cassette recorder: %v", err)
	}
	proxy := httptest.NewServer(recorder)
	t.Cleanup(proxy.Close)
	return capturing{URL: proxy.URL, recorder: recorder}
}

// send makes one request through the proxy, drains the reply and returns its status.
func send(t *testing.T, method, url, body string) int {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatalf("read reply: %v", err)
	}
	return resp.StatusCode
}

// replyBody is an exchange's chunks joined back into the body they were read from.
func replyBody(exchange Exchange) string {
	var out bytes.Buffer
	for _, chunk := range exchange.Chunks {
		out.Write(chunk.Bytes)
	}
	return out.String()
}

func TestCassetteRecorderCapturesChunksWithTheirOffsets(t *testing.T) {
	t.Parallel()
	events := []string{"data: {\"n\":1}\n\n", "data: {\"n\":2}\n\n", "data: [DONE]\n\n"}
	models := `{"object":"list","data":[{"id":"live-model","object":"model"}]}`
	proxy := captureProxy(t, "", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == modelsPath {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, models)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for i, event := range events {
			if i > 0 {
				time.Sleep(chunkGap)
			}
			_, _ = io.WriteString(w, event)
			w.(http.Flusher).Flush()
		}
	})

	send(t, http.MethodGet, proxy.URL+modelsPath, "")
	send(t, http.MethodPost, proxy.URL+chatCompletionsPath, simpleChat)
	cassette := proxy.recorder.Cassette()

	if len(cassette.Exchanges) != 1 {
		t.Fatalf("exchanges = %d, want 1", len(cassette.Exchanges))
	}
	exchange := cassette.Exchanges[0]
	t.Run("one chunk per upstream write, offsets increasing", func(t *testing.T) {
		if len(exchange.Chunks) != len(events) {
			t.Fatalf("chunks = %d, want %d: %+v", len(exchange.Chunks), len(events), exchange.Chunks)
		}
		for i := 1; i < len(exchange.Chunks); i++ {
			if exchange.Chunks[i].Offset <= exchange.Chunks[i-1].Offset {
				t.Errorf("chunk %d offset %s is not after chunk %d's %s",
					i, exchange.Chunks[i].Offset, i-1, exchange.Chunks[i-1].Offset)
			}
		}
	})
	t.Run("the raw bytes, keyed and whole", func(t *testing.T) {
		if got, want := replyBody(exchange), strings.Join(events, ""); got != want {
			t.Errorf("body = %q, want %q", got, want)
		}
		if want := cassetteKey(chatCompletionsPath, []byte(simpleChat)); exchange.Key != want {
			t.Errorf("key = %q, want %q", exchange.Key, want)
		}
		if exchange.Status != http.StatusOK || exchange.Truncated {
			t.Errorf("status %d truncated %t, want a complete 200", exchange.Status, exchange.Truncated)
		}
		if got := exchange.Header.Get("Content-Type"); got != "text/event-stream" {
			t.Errorf("Content-Type = %q, want text/event-stream", got)
		}
	})
	t.Run("the models body verbatim", func(t *testing.T) {
		probe, ok := cassette.Probe(modelsPath)
		if !ok || probe.Status != http.StatusOK || probe.Body != models {
			t.Errorf("models probe = %+v (found %t), want the 200 and %q", probe, ok, models)
		}
	})
}

func TestCassetteRecorderStoresARefusedProbeAsItsStatus(t *testing.T) {
	t.Parallel()
	proxy := captureProxy(t, "", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })

	send(t, http.MethodGet, proxy.URL+propsPath, "")
	probe, ok := proxy.recorder.Cassette().Probe(propsPath)

	if !ok || probe.Status != http.StatusNotFound || !strings.HasPrefix(probe.ContentType, "text/plain") {
		t.Errorf("props probe = %+v (found %t), want the upstream's text/plain 404", probe, ok)
	}
}

// TestCassetteRecorderCarriesTheKeyInTheWiresSpelling is not parallel: it sets the environment.
func TestCassetteRecorderCarriesTheKeyInTheWiresSpelling(t *testing.T) {
	t.Setenv(keyEnvVar, "s3cret")
	var mu sync.Mutex
	seen := map[string]http.Header{}
	proxy := captureProxy(t, keyEnvVar, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.URL.Path] = r.Header.Clone()
		mu.Unlock()
		_, _ = io.WriteString(w, "{}")
	})

	send(t, http.MethodPost, proxy.URL+messagesPath, simpleChat)
	send(t, http.MethodPost, proxy.URL+chatCompletionsPath, simpleChat)

	mu.Lock()
	defer mu.Unlock()
	cases := []struct {
		path, header, want, absent string
	}{
		{path: messagesPath, header: "x-api-key", want: "s3cret", absent: "Authorization"},
		{path: chatCompletionsPath, header: "Authorization", want: "Bearer s3cret", absent: "x-api-key"},
	}
	for _, tc := range cases {
		if got := seen[tc.path].Get(tc.header); got != tc.want {
			t.Errorf("%s: %s = %q, want %q", tc.path, tc.header, got, tc.want)
		}
		if got := seen[tc.path].Get(tc.absent); got != "" {
			t.Errorf("%s: %s = %q, want none", tc.path, tc.absent, got)
		}
	}
}

func TestNewCassetteRecorderRefusesAnUnsetKeyVariable(t *testing.T) {
	t.Parallel()

	_, err := NewCassetteRecorder("http://127.0.0.1:1", "STUBLLM_CAPTURE_TEST_UNSET_KEY")

	if err == nil {
		t.Error("recorder built with an unset key variable, want a refusal")
	}
}

func TestCassetteRecorderRetryRule(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		statuses []int
		want     []string
	}{
		{name: "a failure then a success replaces", statuses: []int{500, 200}, want: []string{"reply 2"}},
		{name: "two successes queue", statuses: []int{200, 200}, want: []string{"reply 1", "reply 2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			served := 0
			proxy := captureProxy(t, "", func(w http.ResponseWriter, _ *http.Request) {
				mu.Lock()
				status := tc.statuses[served]
				served++
				n := served
				mu.Unlock()
				w.WriteHeader(status)
				_, _ = fmt.Fprintf(w, "reply %d", n)
			})

			for range tc.statuses {
				send(t, http.MethodPost, proxy.URL+chatCompletionsPath, simpleChat)
			}
			cassette := proxy.recorder.Cassette()

			got := make([]string, 0, len(cassette.Exchanges))
			for _, exchange := range cassette.Exchanges {
				got = append(got, replyBody(exchange))
			}
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("recorded replies = %q, want %q", got, tc.want)
			}
		})
	}
}
