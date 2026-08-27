package stubllm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// maxRequestBytes caps what one request body may be. A driver test's conversation is a few
// hundred kilobytes at worst; the cap exists so a runaway loop fails loudly here rather than
// growing the test process until the machine notices.
const maxRequestBytes = 8 << 20

// completionID is the id every reply carries. It is a constant because nothing reads it — the
// field exists so the payload is shaped like a real one.
const completionID = "chatcmpl-stubllm"

// Option configures a Server.
type Option func(*settings)

// settings are the knobs an Option turns.
type settings struct {
	requestLog bool
	apiKey     string
	latency    time.Duration
}

// WithRequestLog turns the request log on or off. It is ON by default; switching it off is for
// a long soak where the log would grow without being read.
func WithRequestLog(enabled bool) Option {
	return func(s *settings) { s.requestLog = enabled }
}

// WithAPIKey requires every request to carry `Authorization: Bearer key`; a request without it
// is answered 401. Unset (the default) accepts any request, authenticated or not.
func WithAPIKey(key string) Option {
	return func(s *settings) { s.apiKey = key }
}

// WithLatency stalls every reply by d before its first byte — the time-to-first-token of a
// server that is thinking, which is what a spinner or a cancel path needs to be observable.
func WithLatency(d time.Duration) Option {
	return func(s *settings) { s.latency = d }
}

// Server is a scripted OpenAI-compatible upstream. Build one with [New] inside a test or with
// [Serve] from a binary; both play the same Script through the same handler.
type Server struct {
	// URL is the base URL to hand provider.NewClient: no trailing slash, no /v1 suffix.
	URL string
	// Model is the id this upstream advertises, copied from the Script.
	Model string

	set settings

	mu       sync.Mutex
	count    int
	matcher  *matcher
	requests []Request

	closeOnce sync.Once
	closer    func()
}

// New starts a scripted upstream on a loopback port for the duration of the test. The script
// is validated first, so an unplayable fixture fails here and names the turn.
func New(t testing.TB, s Script, opts ...Option) *Server {
	t.Helper()

	server, err := newServer(s, opts...)
	if err != nil {
		t.Fatalf("%v", err)
	}
	httpServer := httptest.NewServer(server.handler())
	server.URL = httpServer.URL
	server.closer = httpServer.Close
	t.Cleanup(server.Close)
	return server
}

// Serve starts a scripted upstream on addr and returns once it is listening; "127.0.0.1:0"
// picks a free port, readable afterwards from the Server's URL. It stops when ctx ends or
// [Server.Close] is called, whichever comes first.
func Serve(ctx context.Context, addr string, s Script, opts ...Option) (*Server, error) {
	server, err := newServer(s, opts...)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("stubllm: listen on %s: %w", addr, err)
	}

	httpServer := &http.Server{Handler: server.handler(), ReadHeaderTimeout: 10 * time.Second}
	server.URL = "http://" + listener.Addr().String()

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		_ = httpServer.Serve(listener)
	}()

	closing := make(chan struct{})
	server.closer = func() {
		close(closing)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		<-stopped
	}
	go func() {
		select {
		case <-ctx.Done():
			server.Close()
		case <-closing:
		}
	}()
	return server, nil
}

// Close stops the server. It is idempotent, and [New] already registers it on t.Cleanup.
func (s *Server) Close() {
	s.closeOnce.Do(func() {
		if s.closer != nil {
			s.closer()
		}
	})
}

// newServer validates a Script and assembles the state both entry points share.
func newServer(s Script, opts ...Option) (*Server, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	m, err := newMatcher(s)
	if err != nil {
		return nil, err
	}

	server := &Server{Model: s.Model, set: settings{requestLog: true}, matcher: m}
	for _, opt := range opts {
		opt(&server.set)
	}
	return server, nil
}

// handler is the routing surface: the two endpoints the provider client uses, behind the
// optional api-key gate. Everything else 404s, which is what a real server does for the
// llama.cpp-only paths (/props) the client probes and tolerates.
func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", s.handleModels)
	mux.HandleFunc("POST /v1/chat/completions", s.handleChat)
	return s.authorized(mux)
}

// authorized enforces WithAPIKey when one is set.
func (s *Server) authorized(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.set.apiKey != "" && r.Header.Get("Authorization") != "Bearer "+s.set.apiKey {
			http.Error(w, "stubllm: bad or missing api key", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleModels answers the discovery probe with the one model the Script names.
func (s *Server) handleModels(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(modelsReply{
		Object: "list",
		Data:   []modelEntry{{ID: s.Model, Object: "model"}},
	})
}

// handleChat matches the request against the script and plays the Turn it took.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes))
	if err != nil {
		http.Error(w, "stubllm: read request: "+err.Error(), http.StatusBadRequest)
		return
	}
	var request chatRequest
	if err := json.Unmarshal(body, &request); err != nil {
		http.Error(w, "stubllm: undecodable request: "+err.Error(), http.StatusBadRequest)
		return
	}

	entry, turn, ok := s.take(request)
	if !ok {
		http.Error(w, fmt.Sprintf("stubllm: no turn for request %d", entry.N), http.StatusInternalServerError)
		return
	}
	if !sleep(r.Context(), s.set.latency) {
		return
	}
	s.reply(w, r, turn, request.Stream)
}

// take logs a request and hands back the Turn that answers it. The lock spans both so request
// numbering and turn consumption stay in step under concurrent requests.
func (s *Server) take(request chatRequest) (Request, Turn, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.count++
	entry := Request{
		N:         s.count,
		Model:     request.Model,
		Messages:  request.messages(),
		Tools:     request.toolNames(),
		Stream:    request.Stream,
		TurnIndex: -1,
		At:        time.Now(),
	}

	index := s.matcher.next(entry)
	if index < 0 {
		entry.Unmatched = true
	} else {
		entry.TurnIndex = index
	}
	if s.set.requestLog {
		s.requests = append(s.requests, entry)
	}
	if index < 0 {
		return entry, Turn{}, false
	}
	return entry, s.matcher.turns[index], true
}

// reply plays one Turn onto the wire in the shape the request asked for.
func (s *Server) reply(w http.ResponseWriter, r *http.Request, t Turn, stream bool) {
	if t.HTTP != nil {
		writeHTTPReply(w, *t.HTTP)
		return
	}
	if !sleep(r.Context(), t.Hang) {
		return
	}
	if stream {
		writeStream(r.Context(), w, t, s.Model)
		return
	}
	writeWhole(w, t, s.Model)
}

// writeStream plays a Turn as SSE: reasoning first, then content, then the tool-call
// fragments, then the terminal finish_reason, the usage chunk and [DONE] — the order and
// framing a real OpenAI-compatible server uses.
func writeStream(ctx context.Context, w http.ResponseWriter, t Turn, model string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stubllm: response writer cannot flush", http.StatusInternalServerError)
		return
	}

	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	send := func(envelope sseEnvelope) bool {
		envelope.ID, envelope.Object, envelope.Model = completionID, "chat.completion.chunk", model
		data, err := json.Marshal(envelope)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	for i, delta := range streamDeltas(t) {
		if i > 0 && !sleep(ctx, t.TokenDelay) {
			return
		}
		if !send(sseEnvelope{Choices: []sseChoice{{Delta: delta}}}) {
			return
		}
	}
	if !send(sseEnvelope{Choices: []sseChoice{{FinishReason: t.finishReason()}}}) {
		return
	}
	if t.Usage != nil && !send(sseEnvelope{Usage: usageOf(*t.Usage)}) {
		return
	}
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// streamDeltas is the ordered list of deltas a Turn streams before its terminator. An
// empty-reply turn yields none, which is exactly what a model abandoning a reply sends.
func streamDeltas(t Turn) []sseDelta {
	var deltas []sseDelta
	for _, part := range splitRunes(t.Reasoning, t.chunkRunes()) {
		deltas = append(deltas, sseDelta{ReasoningContent: part})
	}
	for _, part := range splitRunes(t.Text, t.chunkRunes()) {
		deltas = append(deltas, sseDelta{Content: part})
	}
	for i, call := range t.ToolCalls {
		deltas = append(deltas, toolCallDeltas(i, call)...)
	}
	return deltas
}

// toolCallDeltas splits one call into the two fragments real servers send: an id-bearing head
// carrying the name and the first half of the arguments, then an id-less tail carrying the
// rest. Splitting matters — a client that only handles whole calls passes against a one-shot
// stub and fails against a real server.
func toolCallDeltas(index int, c ToolCall) []sseDelta {
	head, tail := splitHalf(c.arguments())
	deltas := []sseDelta{{ToolCalls: []sseToolCall{{
		Index:    index,
		ID:       c.callID(index),
		Type:     "function",
		Function: sseFunction{Name: c.Name, Arguments: head},
	}}}}
	if tail == "" {
		return deltas
	}
	return append(deltas, sseDelta{ToolCalls: []sseToolCall{{
		Index:    index,
		Function: sseFunction{Arguments: tail},
	}}})
}

// writeWhole plays a Turn as a single JSON completion — the non-streamed path. TokenDelay has
// no meaning here: there are no chunks to space out.
func writeWhole(w http.ResponseWriter, t Turn, model string) {
	reply := wholeReply{
		ID:     completionID,
		Object: "chat.completion",
		Model:  model,
		Choices: []wholeChoice{{
			Message: wholeMessage{
				Role:             "assistant",
				Content:          t.Text,
				ReasoningContent: t.Reasoning,
			},
			FinishReason: t.finishReason(),
		}},
	}
	for i, call := range t.ToolCalls {
		reply.Choices[0].Message.ToolCalls = append(reply.Choices[0].Message.ToolCalls, wireToolCall{
			ID:       call.callID(i),
			Type:     "function",
			Function: sseFunction{Name: call.Name, Arguments: call.arguments()},
		})
	}
	if t.Usage != nil {
		reply.Usage = usageOf(*t.Usage)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(reply)
}

// writeHTTPReply writes a raw HTTP answer — status, headers, body — and nothing SSE-shaped.
func writeHTTPReply(w http.ResponseWriter, reply HTTPReply) {
	if reply.Location != "" {
		w.Header().Set("Location", reply.Location)
	}
	contentType := reply.ContentType
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(reply.Status)
	_, _ = io.WriteString(w, reply.Body)
}

// usageOf renders the scripted accounting onto the wire. The cached breakdown is present ONLY
// above zero: an absent member means "this server does not report caching" while a zero one
// means "nothing was cached", and both shapes must be scriptable.
func usageOf(u Usage) *usageWire {
	wire := &usageWire{
		PromptTokens:     u.Prompt,
		CompletionTokens: u.Completion,
		TotalTokens:      u.Prompt + u.Completion,
	}
	if u.Cached > 0 {
		wire.PromptTokensDetails = &promptTokensDetails{CachedTokens: u.Cached}
	}
	return wire
}

// sleep waits for d and reports whether it completed. A cancelled request context ends the
// wait early and reports false, so the caller stops writing to a connection that is gone.
func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// splitRunes cuts text into chunks of at most n runes, in order. An empty text yields no
// chunks, which is what makes the empty-reply turn stream nothing but its terminator.
func splitRunes(text string, n int) []string {
	if text == "" {
		return nil
	}
	runes := []rune(text)
	out := make([]string, 0, (len(runes)+n-1)/n)
	for i := 0; i < len(runes); i += n {
		out = append(out, string(runes[i:min(i+n, len(runes))]))
	}
	return out
}

// splitHalf cuts text in two at its rune midpoint; a text of fewer than two runes stays whole.
func splitHalf(text string) (string, string) {
	runes := []rune(text)
	if len(runes) < 2 {
		return text, ""
	}
	half := len(runes) / 2
	return string(runes[:half]), string(runes[half:])
}
