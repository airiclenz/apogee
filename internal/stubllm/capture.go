package stubllm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// transferHeaders are the reply header members an Exchange does not keep: each describes one
// particular transfer — its length, its date, a session cookie — rather than the reply a replay
// should reproduce. (Hop-by-hop members never reach the recorder: the reverse proxy strips them
// before its ModifyResponse runs.)
var transferHeaders = []string{"Content-Length", "Date", "Set-Cookie"}

// CassetteRecorder is a recording proxy that captures a live upstream into a [Cassette]: it
// forwards /v1/* (and llama.cpp's /props) to the upstream, and records every completion POST —
// /v1/chat/completions and /v1/messages — as an [Exchange] of raw reply chunks with their arrival
// offsets, and every GET /v1/models and /props as a verbatim [ProbeReply].
//
// # The key
//
// An exchange is filed under a key that names the conversation the request carried, not its
// bytes, so a replayed session finds its replies even though its system prompt (scratch
// directory, session id) and its tool results (timings, paths) differ from the captured run.
// The key is a digest of the wire (`chat` | `messages`), the stream flag, the sorted tool-name
// set — which tells a sub-agent from its parent on the same first message — the first user
// message with its ISO dates normalized to `<date>`, and the ordered assistant turns (content,
// tool-call names and arguments). System text and tool-result content are excluded.
//
// # Queues and retries
//
// A key seen twice queues both replies, in capture order. A retry repeats its key, so a later
// exchange REPLACES the key's last one when that one ended non-2xx or before its EOF, and
// APPENDS otherwise — a retried POST is recorded once, as the reply that finally answered it.
//
// # Auth
//
// The recorder carries the upstream's key so apogee's own config can stay keyless: named by an
// environment variable, it is added to every forwarded request in the spelling the wire reads —
// `x-api-key` on /v1/messages and on any request carrying the anthropic-version header,
// `Authorization: Bearer` otherwise — replacing whatever the client sent.
//
// Capturing is an explicit act performed by a human against a live model, never something a
// `go test` run does.
type CassetteRecorder struct {
	upstream *url.URL
	apiKey   string
	proxy    *httputil.ReverseProxy
	inflight sync.WaitGroup

	mu       sync.Mutex
	cassette Cassette
}

// NewCassetteRecorder returns a CassetteRecorder that proxies to upstream, authenticating with
// the key held in the environment variable keyEnv. An empty keyEnv forwards requests without
// adding a key; a keyEnv that names an unset or empty variable is refused, because a capture that
// runs keyless by accident records a cassette full of 401s.
func NewCassetteRecorder(upstream, keyEnv string) (*CassetteRecorder, error) {
	target, err := url.Parse(upstream)
	if err != nil || target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf(
			"stubllm: %q is not an upstream URL (want e.g. http://127.0.0.1:1111)", upstream)
	}

	recorder := &CassetteRecorder{upstream: target}
	if keyEnv != "" {
		recorder.apiKey = os.Getenv(keyEnv)
		if recorder.apiKey == "" {
			return nil, fmt.Errorf("stubllm: the upstream key variable %s is unset or empty", keyEnv)
		}
	}
	recorder.proxy = &httputil.ReverseProxy{
		Rewrite:        recorder.rewrite,
		ModifyResponse: recorder.capture,
		// Every write is flushed through at once: a buffering proxy would invent the timing the
		// cassette is recording.
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, request *http.Request, err error) {
			// A request that failed before its reply could be captured files nothing; it is
			// settled here so Cassette does not wait on it.
			recorder.settle(takeOf(request))
			http.Error(w, fmt.Sprintf("stubllm: upstream %s: %v", target, err), http.StatusBadGateway)
		},
	}
	return recorder, nil
}

// ServeHTTP proxies one request. Paths outside /v1/ are refused, except llama.cpp's /props.
func (r *CassetteRecorder) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	path := request.URL.Path
	if !strings.HasPrefix(path, "/v1/") && path != propsPath {
		http.NotFound(w, request)
		return
	}
	switch {
	case request.Method == http.MethodPost && (path == chatCompletionsPath || path == messagesPath):
		request = r.beginExchange(request)
	case request.Method == http.MethodGet && (path == modelsPath || path == propsPath):
		request = r.begin(request, &take{probe: path})
	}
	r.proxy.ServeHTTP(w, request)
}

// Cassette returns what has been captured so far, once every request already begun has filed
// its reply (or the settle backstop has passed). The result is the caller's own copy.
func (r *CassetteRecorder) Cassette() *Cassette {
	r.settleInflight()

	r.mu.Lock()
	defer r.mu.Unlock()
	return &Cassette{
		Version:   CassetteVersion,
		Exchanges: slices.Clone(r.cassette.Exchanges),
		Probes:    slices.Clone(r.cassette.Probes),
	}
}

// rewrite points a request at the upstream, carries the key in the wire's spelling, and asks for
// an uncompressed reply so the cassette holds readable bytes.
func (r *CassetteRecorder) rewrite(request *httputil.ProxyRequest) {
	request.SetURL(r.upstream)
	request.Out.Host = r.upstream.Host
	request.Out.Header.Del("Accept-Encoding")
	if r.apiKey == "" {
		return
	}

	request.Out.Header.Del("Authorization")
	request.Out.Header.Del("x-api-key")
	if request.In.URL.Path == messagesPath || request.In.Header.Get(anthropicVersionHeader) != "" {
		request.Out.Header.Set("x-api-key", r.apiKey)
		return
	}
	request.Out.Header.Set("Authorization", "Bearer "+r.apiKey)
}

// beginExchange reads a completion request's body to key it, then hands the request back with a
// replayable body and a take attached. The body is read whole and uncapped: this proxy forwards
// its own operator's traffic, and a truncated request would reach the upstream corrupt.
func (r *CassetteRecorder) beginExchange(request *http.Request) *http.Request {
	body, err := io.ReadAll(request.Body)
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.ContentLength = int64(len(body))
	if err != nil {
		return request
	}
	return r.begin(request, &take{key: cassetteKey(request.URL.Path, body)})
}

// begin stamps a take's start, counts it in flight and attaches it to the request.
func (r *CassetteRecorder) begin(request *http.Request, entry *take) *http.Request {
	entry.start = time.Now()
	r.inflight.Add(1)
	return request.WithContext(context.WithValue(request.Context(), takeKey{}, entry))
}

// capture attaches the timing reader to a reply on its way back to the client. It runs as the
// proxy's ModifyResponse, the one place with both the status and the unread body.
func (r *CassetteRecorder) capture(reply *http.Response) error {
	entry := takeOf(reply.Request)
	if entry == nil {
		return nil
	}

	entry.status = reply.StatusCode
	entry.header = reply.Header.Clone()
	for _, name := range transferHeaders {
		entry.header.Del(name)
	}
	reply.Body = &timedBody{body: reply.Body, entry: entry, done: r.finish}
	return nil
}

// finish files a completed take — a probe as the latest answer to its path, an exchange into its
// key's queue under the retry rule — and settles it.
func (r *CassetteRecorder) finish(entry *take) {
	r.mu.Lock()
	if entry.probe != "" {
		r.fileProbe(entry)
	} else {
		r.fileExchange(entry.exchange())
	}
	r.mu.Unlock()
	r.settle(entry)
}

// fileProbe stores a probe's answer, replacing any earlier answer to the same path. The caller
// holds r.mu.
func (r *CassetteRecorder) fileProbe(entry *take) {
	reply := ProbeReply{
		Path:        entry.probe,
		Status:      entry.status,
		ContentType: entry.header.Get("Content-Type"),
		Body:        string(entry.body()),
	}
	for i, probe := range r.cassette.Probes {
		if probe.Path == reply.Path {
			r.cassette.Probes[i] = reply
			return
		}
	}
	r.cassette.Probes = append(r.cassette.Probes, reply)
}

// fileExchange applies the retry rule: an exchange replaces its key's last one when that one
// failed — non-2xx, or cut before EOF — and is appended to the key's queue otherwise. The caller
// holds r.mu.
func (r *CassetteRecorder) fileExchange(exchange Exchange) {
	exchanges := r.cassette.Exchanges
	for i := len(exchanges) - 1; i >= 0; i-- {
		if exchanges[i].Key != exchange.Key {
			continue
		}
		if exchanges[i].Status/100 != 2 || exchanges[i].Truncated {
			exchanges[i] = exchange
			return
		}
		break
	}
	r.cassette.Exchanges = append(exchanges, exchange)
}

// settle marks a take as no longer in flight, exactly once whatever became of it. nil is a
// no-op: a request the recorder does not file carries no take.
func (r *CassetteRecorder) settle(entry *take) {
	if entry == nil {
		return
	}
	entry.settled.Do(r.inflight.Done)
}

// settleInflight waits for every begun take to settle, up to the settleWait backstop. A client
// can be back in its caller's hands — it stopped at `[DONE]` — while the proxy is still one Read
// short of the EOF that files the reply; without this wait the cassette would be short a reply.
func (r *CassetteRecorder) settleInflight() {
	settled := make(chan struct{})
	go func() {
		r.inflight.Wait()
		close(settled)
	}()

	timer := time.NewTimer(settleWait)
	defer timer.Stop()
	select {
	case <-settled:
	case <-timer.C:
	}
}

// takeKey is the context key the request half uses to hand its take to the reply half.
type takeKey struct{}

// takeOf returns the take a request carries, or nil when it is not one being recorded.
func takeOf(request *http.Request) *take {
	if request == nil {
		return nil
	}
	entry, _ := request.Context().Value(takeKey{}).(*take)
	return entry
}

// take is one request/reply pair in flight: a probe's path or an exchange's key, when the
// request arrived, and the reply as it comes back. Only the reply's own reader goroutine writes
// the chunks, and finish reads them after that reader is done.
type take struct {
	probe string
	key   string
	start time.Time

	settled   sync.Once
	status    int
	header    http.Header
	chunks    []Chunk
	truncated bool
}

// write keeps one read of reply body, copied, with its offset from the request's arrival.
func (t *take) write(p []byte, at time.Time) {
	t.chunks = append(t.chunks, Chunk{Offset: at.Sub(t.start), Bytes: bytes.Clone(p)})
}

// body is the reply body reassembled from its chunks.
func (t *take) body() []byte {
	var out bytes.Buffer
	for _, chunk := range t.chunks {
		out.Write(chunk.Bytes)
	}
	return out.Bytes()
}

// exchange renders the take as the Exchange that replays it.
func (t *take) exchange() Exchange {
	return Exchange{
		Key:       t.key,
		Status:    t.status,
		Header:    t.header,
		Chunks:    t.chunks,
		Truncated: t.truncated,
	}
}

// timedBody is the reply body on its way to the client, handing every read to its take with the
// time it arrived. done fires exactly once: on EOF, on a read error (the take then marked
// truncated) or on a Close before EOF (likewise).
type timedBody struct {
	body  io.ReadCloser
	entry *take
	done  func(*take)
	once  sync.Once
}

func (b *timedBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if n > 0 {
		b.entry.write(p[:n], time.Now())
	}
	if err != nil {
		b.end(!errors.Is(err, io.EOF))
	}
	return n, err
}

func (b *timedBody) Close() error {
	b.end(true)
	return b.body.Close()
}

// end files the take once, marking it truncated when the body ended any way but EOF.
func (b *timedBody) end(truncated bool) {
	b.once.Do(func() {
		b.entry.truncated = truncated
		b.done(b.entry)
	})
}
