package stubllm

import (
	"context"
	"encoding/json"
	"errors"
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

// awaitLimit bounds how long a request held by a Turn's `await:` waits for [Server.Release]. A
// gate nobody opens is a mistake in the test, and the useful failure is a 500 naming the label —
// not a suite that hangs until the test binary's own timeout and reports nothing about what was
// waiting for what. It is a var only so this package's own test can pin that refusal without
// taking ten seconds over it; nothing outside the package moves it.
var awaitLimit = 10 * time.Second

// completionID is the id every reply carries. It is a constant because nothing reads it — the
// field exists so the payload is shaped like a real one.
const completionID = "chatcmpl-stubllm"

// The two discovery paths apogee probes before its first completion, served here and proxied
// by the recorder. modelsPath is probed on both wires; propsPath is llama.cpp's, off /v1/.
const (
	modelsPath = "/v1/models"
	propsPath  = "/props"
)

// anthropicVersionHeader is the header every Messages-wire request carries and no
// chat-completions request does — the tell by which GET /v1/models chooses its list shape.
const anthropicVersionHeader = "anthropic-version"

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

// WithAPIKey requires every request to carry the key — as `Authorization: Bearer key`, the
// chat-completions spelling, or as `x-api-key: key`, the Messages one; either header opens
// either route. A request carrying neither is answered 401. Unset (the default) accepts any
// request, authenticated or not.
func WithAPIKey(key string) Option {
	return func(s *settings) { s.apiKey = key }
}

// WithLatency stalls every reply by d before its first byte — the time-to-first-token of a
// server that is thinking, which is what a spinner or a cancel path needs to be observable.
func WithLatency(d time.Duration) Option {
	return func(s *settings) { s.latency = d }
}

// Server is a scripted upstream that speaks both of apogee's wires — chat-completions on
// /v1/chat/completions and Anthropic Messages on /v1/messages — from one Script. Build one with
// [New] inside a test or with [Serve] from a binary; both play the same Script through the same
// handler.
type Server struct {
	// URL is the base URL to hand provider.NewClient: no trailing slash, no /v1 suffix.
	URL string
	// Model is the id this upstream advertises, copied from the Script.
	Model string

	set       settings
	discovery Discovery

	mu       sync.Mutex
	count    int
	matcher  *matcher
	requests []Request
	probes   []Probe
	// gates holds one channel per `await:` label, closed by [Server.Release]. Labels are
	// created on first use from either side, so a Release that runs before the request it
	// frees — the ordinary case, since the test is watching apogee and not the wire — opens a
	// gate that is already there when the request arrives.
	gates map[string]chan struct{}

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
	httpServer := httptest.NewServer(server.Handler())
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

	httpServer := &http.Server{Handler: server.Handler(), ReadHeaderTimeout: 10 * time.Second}
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

// InProcess builds a scripted upstream that listens on nothing: a client reaches it through
// [Server.Transport] — the same Handler as [New] and [Serve], minus the socket — so an engine
// test plays a Script without a loopback port per fake and the bytes it decodes are the bytes
// a listening stub would have sent. URL is empty; the transport answers any host. The script
// is validated first, so an unplayable fixture fails here and names the turn.
func InProcess(t testing.TB, s Script, opts ...Option) *Server {
	t.Helper()

	server, err := newServer(s, opts...)
	if err != nil {
		t.Fatalf("%v", err)
	}
	t.Cleanup(server.Close)
	return server
}

// Transport is an [http.RoundTripper] that serves every request from [Server.Handler] in
// process, over a pipe rather than a socket. The reply streams: each Flush the handler makes
// is readable at once, so a `token_delay` or an `await:` gate is observable exactly as it is
// through a listener, and a `cut` turn's kill — which aborts the handler where nothing can be
// hijacked — closes the pipe with io.ErrUnexpectedEOF, the read error a dropped TCP connection
// produces. Hand it to the provider client as `&http.Client{Transport: server.Transport()}`.
func (s *Server) Transport() http.RoundTripper {
	return pipeTransport{handler: s.Handler()}
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

	server := &Server{
		Model:     s.Model,
		set:       settings{requestLog: true},
		discovery: s.Discovery,
		matcher:   m,
		gates:     map[string]chan struct{}{},
	}
	for _, opt := range opts {
		opt(&server.set)
	}
	return server, nil
}

// Release opens the gate label names, so every Turn whose `await:` names it answers from now on.
// It is idempotent and may be called before the held request has even arrived: a test that
// watches apogee — a frame that has painted, an Event that has been folded — is watching the
// thing that has to happen FIRST, and the request it frees is by definition still waiting.
func (s *Server) Release(label string) {
	if label == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	gate := s.gate(label)
	select {
	case <-gate:
	default:
		close(gate)
	}
}

// gate is the channel this label's waiters block on, made on first mention from either side. The
// caller holds the lock.
func (s *Server) gate(label string) chan struct{} {
	if existing, ok := s.gates[label]; ok {
		return existing
	}
	made := make(chan struct{})
	s.gates[label] = made
	return made
}

// Handler is the routing surface: the two discovery probes, the chat-completions route the
// openai wire posts to and the Messages route the anthropic wire posts to, behind the optional
// api-key gate. Both completion routes play the SAME Script — each decodes its own request
// shape into the neutral [Request] and renders the Turn it took in its own reply shape, so a
// fixture is written once whichever wire the test drives. The probes answer from the Script's
// [Discovery] block: GET /v1/models in the list shape of the wire that asked, GET /props with
// the scripted launch facts or a 404 when none are scripted — what a real server that is not
// llama.cpp does for the path the client probes and tolerates. Everything else 404s. [New] and
// [Serve] put it behind a listener; [InProcess] hands it out bare, for a test that reaches it
// through [Server.Transport] or mounts it under a mux of its own.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+modelsPath, s.handleModels)
	mux.HandleFunc("GET "+propsPath, s.handleProps)
	mux.HandleFunc("POST /v1/chat/completions", s.handleChat)
	mux.HandleFunc("POST /v1/messages", s.handleMessages)
	return s.authorized(mux)
}

// authorized enforces WithAPIKey when one is set, in either header spelling.
func (s *Server) authorized(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.set.apiKey != "" && !s.carriesKey(r) {
			http.Error(w, "stubllm: bad or missing api key", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// carriesKey reports whether r presents the configured key as a Bearer token or as x-api-key.
func (s *Server) carriesKey(r *http.Request) bool {
	return r.Header.Get("Authorization") == "Bearer "+s.set.apiKey || r.Header.Get("x-api-key") == s.set.apiKey
}

// handleModels answers the models probe from the Discovery block: a scripted failure (a
// status, or a hang that ends with the request), else the advertised list in the shape of the
// wire that asked — the Messages list when the request carries the anthropic-version header
// every Messages client sends, the OpenAI list otherwise. The probe is logged first, before a
// hang blocks, so a test can see it arrive.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	s.probe(r)
	if s.discovery.Hang {
		<-r.Context().Done()
		return
	}
	if s.discovery.Status != 0 {
		w.WriteHeader(s.discovery.Status)
		_, _ = io.WriteString(w, s.discovery.Body)
		return
	}

	models := s.discovery.models(s.Model)
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get(anthropicVersionHeader) != "" {
		_ = json.NewEncoder(w).Encode(anthropicModels(models))
		return
	}
	_ = json.NewEncoder(w).Encode(openAIModels(models))
}

// handleProps answers llama.cpp's props probe with the scripted launch facts, or 404s when the
// Script scripts none — the answer every server that is not llama.cpp gives.
func (s *Server) handleProps(w http.ResponseWriter, r *http.Request) {
	s.probe(r)
	if s.discovery.Props == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.discovery.Props.reply())
}

// probe logs a discovery GET, unless the log is off.
func (s *Server) probe(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.set.requestLog {
		s.probes = append(s.probes, Probe{Path: r.URL.Path, Header: r.Header.Clone(), At: time.Now()})
	}
}

// handleChat decodes a chat-completions request and serves it.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var request chatRequest
	body, ok := decodeRequest(w, r, &request)
	if !ok {
		return
	}
	s.serve(w, r, Request{
		Wire:     WireOpenAI,
		Model:    request.Model,
		Messages: request.messages(),
		Tools:    request.toolNames(),
		Stream:   request.Stream,
		Sampling: request.sampling(),
		Effort:   request.effort(),
		Body:     body,
	})
}

// handleMessages decodes an Anthropic Messages request and serves it — the same matching,
// gating and Turn as the chat route, rendered in the Messages reply shape.
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	var request anthropicRequest
	body, ok := decodeRequest(w, r, &request)
	if !ok {
		return
	}
	entry := request.logEntry()
	entry.Body = body
	s.serve(w, r, entry)
}

// decodeRequest reads a request body under maxRequestBytes into v and returns the bytes it read
// (the log's [Request.Body]) with whether it could; a failure is already answered 400 when it
// returns false.
func decodeRequest(w http.ResponseWriter, r *http.Request, v any) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes))
	if err != nil {
		http.Error(w, "stubllm: read request: "+err.Error(), http.StatusBadRequest)
		return nil, false
	}
	if err := json.Unmarshal(body, v); err != nil {
		http.Error(w, "stubllm: undecodable request: "+err.Error(), http.StatusBadRequest)
		return nil, false
	}
	return body, true
}

// serve matches the decoded request against the script and plays the Turn it took, on the wire
// the entry names.
func (s *Server) serve(w http.ResponseWriter, r *http.Request, entry Request) {
	turn, err := s.take(entry)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// The gate sits between matching and replying: the request is already logged and its turn
	// already taken, so a test reading the log sees the request arrive at the moment it arrived,
	// and only the ANSWER is held back.
	held, err := s.awaitRelease(r.Context(), turn.Await)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !held {
		return
	}
	if !sleep(r.Context(), s.set.latency) {
		return
	}
	s.reply(w, r, turn, entry)
}

// awaitRelease holds a request until [Server.Release] opens label's gate. It answers true when the
// wait is over and the reply may be written, false when the client gave up first — in which case
// nothing is written at all, the way a cancelled `hang` turn writes nothing — and an error when
// nobody opened the gate inside [awaitLimit].
func (s *Server) awaitRelease(ctx context.Context, label string) (bool, error) {
	if label == "" {
		return true, nil
	}
	s.mu.Lock()
	gate := s.gate(label)
	s.mu.Unlock()

	timer := time.NewTimer(awaitLimit)
	defer timer.Stop()
	select {
	case <-gate:
		return true, nil
	case <-ctx.Done():
		return false, nil
	case <-timer.C:
		return false, fmt.Errorf("stubllm: a turn waited for the gate %q, which nothing released", label)
	}
}

// take logs a request — the wire-neutral entry its route decoded, numbered and stamped here —
// and hands back the Turn that answers it, expanded against the request's own text. The lock
// spans all of it so request numbering, turn consumption and the capture evaluation stay in
// step under concurrent requests. Both failures — no turn at all, and a turn whose captures
// found nothing — log the request with Unmatched set and leave the script where it was, so the
// 500 body is the whole story of what went wrong.
func (s *Server) take(entry Request) (Turn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.count++
	entry.N = s.count
	entry.TurnIndex = -1
	entry.At = time.Now()

	index := s.matcher.next(entry)
	if index < 0 {
		return Turn{}, s.refuse(entry, errors.New("no turn"))
	}
	turn, err := s.matcher.turns[index].expand(entry)
	if err != nil {
		s.matcher.release(index)
		return Turn{}, s.refuse(entry, err)
	}

	entry.TurnIndex = index
	s.record(entry)
	return turn, nil
}

// refuse logs an unanswerable request and renders the 500 body naming it. The caller holds the
// lock.
func (s *Server) refuse(entry Request, cause error) error {
	entry.Unmatched = true
	s.record(entry)
	return fmt.Errorf("stubllm: %w for request %d", cause, entry.N)
}

// record appends an entry to the request log, unless the log is off. The caller holds the lock.
func (s *Server) record(entry Request) {
	if s.set.requestLog {
		s.requests = append(s.requests, entry)
	}
}

// reply plays one Turn onto the wire in the shape the request asked for: streamed or whole, on
// the wire the request arrived by. An http turn and a hang are wire-neutral and come first.
func (s *Server) reply(w http.ResponseWriter, r *http.Request, t Turn, request Request) {
	if t.HTTP != nil {
		writeHTTPReply(w, *t.HTTP)
		return
	}
	if !sleep(r.Context(), t.Hang) {
		return
	}
	switch {
	case request.Wire == WireAnthropic && request.Stream:
		writeMessagesStream(r.Context(), w, t, s.Model)
	case request.Wire == WireAnthropic:
		writeMessagesWhole(w, t, s.Model)
	case request.Stream:
		writeStream(r.Context(), w, t, s.Model)
	default:
		writeWhole(w, t, s.Model)
	}
}

// writeStream plays a Turn as SSE: reasoning first, then content, then the tool-call
// fragments, then the terminal finish_reason, the usage chunk and [DONE] — the order and
// framing a real OpenAI-compatible server uses. A `cut` turn kills the connection where the
// terminator would go; an `error` turn writes its in-band object there instead.
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

	for i, delta := range streamDeltas(t.beforeCut()) {
		if i > 0 && !sleep(ctx, t.TokenDelay) {
			return
		}
		if !send(sseEnvelope{Choices: []sseChoice{{Delta: delta}}}) {
			return
		}
	}
	if t.Cut != nil {
		kill(w)
		return
	}
	if t.Error != nil {
		// The error object is followed by [DONE] on purpose: a client that honours the
		// object returns at it, while one that ignores it and reads on to the [DONE] would
		// commit a silent empty reply — the very failure the member exists to prevent.
		if !send(sseEnvelope{Error: t.Error.wire()}) {
			return
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
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

// beforeCut is the Turn as far as its `cut` lets it stream: the content truncated to the cut
// point and, when the cut lands inside the content, the tool calls that would have followed it
// dropped. A turn without a cut, or one cut at or past the end of its content, is returned
// whole — every delta streams and the kill takes the terminator's place. A cut inside a
// hand-chunked content falls back to the even split for what remains: the boundaries past the
// cut are never reached, and the ones before it are not what such a test is about.
func (t Turn) beforeCut() Turn {
	if t.Cut == nil {
		return t
	}
	runes := []rune(t.content())
	if t.Cut.AfterRunes >= len(runes) {
		return t
	}
	cut := t
	cut.Text = string(runes[:t.Cut.AfterRunes])
	cut.Chunks = nil
	cut.ToolCalls = nil
	return cut
}

// kill drops the TCP connection under w without a terminating chunk, so the client's read
// fails with io.ErrUnexpectedEOF. It hijacks the connection and closes the socket; where the
// writer cannot be hijacked it aborts the handler with http.ErrAbortHandler, which makes
// net/http close the connection the same way. Simply returning from the handler is NOT an
// option: that ends the chunked body cleanly, and a clean EOF is what the provider client
// commits as Done(stop) — the opposite of the fault a `cut` turn scripts.
func kill(w http.ResponseWriter) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		panic(http.ErrAbortHandler)
	}
	conn, _, err := hijacker.Hijack()
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	_ = conn.Close()
}

// wire is the JSON member this error reaches the client as.
func (e InBandError) wire() *wireError {
	return &wireError{Code: e.code(), Message: e.Message}
}

// streamDeltas is the ordered list of deltas a Turn streams before its terminator. An
// empty-reply turn yields none, which is exactly what a model abandoning a reply sends.
func streamDeltas(t Turn) []sseDelta {
	var deltas []sseDelta
	for _, part := range t.reasoningDeltas() {
		deltas = append(deltas, reasoningDelta(t, part))
	}
	for _, part := range t.contentDeltas() {
		deltas = append(deltas, sseDelta{Content: part})
	}
	for i, call := range t.ToolCalls {
		deltas = append(deltas, toolCallDeltas(i, call)...)
	}
	return deltas
}

// reasoningDelta is one chunk of the thinking channel in the spelling this Turn scripts. Exactly
// one of the two fields is ever set, which is what real servers do and what makes an unset
// `reasoning_field` stream the bytes it streamed before the key existed.
func reasoningDelta(t Turn, part string) sseDelta {
	if t.spellsBareReasoning() {
		return sseDelta{Reasoning: part}
	}
	return sseDelta{ReasoningContent: part}
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
// no meaning here: there are no chunks to space out, and a `cut` has no runes to land in, so
// it kills the connection after the 200 header and before any body.
func writeWhole(w http.ResponseWriter, t Turn, model string) {
	w.Header().Set("Content-Type", "application/json")
	if t.Cut != nil {
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		kill(w)
		return
	}
	reply := wholeReply{
		ID:     completionID,
		Object: "chat.completion",
		Model:  model,
		Choices: []wholeChoice{{
			Message:      wholeMessage{Role: "assistant", Content: t.content()},
			FinishReason: t.finishReason(),
		}},
	}
	// The same one-of-two rule the streamed path follows, on the whole reply's message.
	if t.spellsBareReasoning() {
		reply.Choices[0].Message.Reasoning = t.reasoning()
	} else {
		reply.Choices[0].Message.ReasoningContent = t.reasoning()
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
	if t.Error != nil {
		reply.Error = t.Error.wire()
	}
	_ = json.NewEncoder(w).Encode(reply)
}

// writeMessagesStream plays a Turn as the Messages event stream: message_start, then one
// content block per channel — thinking, text, one tool_use per call — each opened, streamed
// as deltas and stopped, then message_delta with the stop reason and the output usage, then
// message_stop — the order and framing the real API uses, with the `event:` line every payload
// repeats its type on. The text deltas honour Turn.Chunks and ChunkRunes as the chat route's
// do; a tool_use input streams as the head-and-tail split the chat route's fragments use. A
// `cut` turn kills the connection where message_delta would go; an `error` turn writes the
// in-band error event there instead — and no message_stop after it, as the real API sends none.
func writeMessagesStream(ctx context.Context, w http.ResponseWriter, t Turn, model string) {
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

	send := func(event anthropicEvent) bool {
		data, err := json.Marshal(event)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	var usage *anthropicUsage
	if t.Usage != nil {
		usage = anthropicUsageOf(*t.Usage)
	}
	start := &anthropicReply{ID: messageID, Type: "message", Role: "assistant", Model: model, Content: []anthropicBlock{}}
	if usage != nil {
		start.Usage = usage.inputSide()
	}
	if !send(anthropicEvent{Type: eventMessageStart, Message: start}) {
		return
	}

	// TokenDelay spaces the deltas, as on the chat route: the pause lands before every delta
	// but the first, and never before a block's start or stop.
	sentDelta := false
	for _, event := range messageEvents(t.beforeCut()) {
		if event.Type == eventBlockDelta {
			if sentDelta && !sleep(ctx, t.TokenDelay) {
				return
			}
			sentDelta = true
		}
		if !send(event) {
			return
		}
	}
	if t.Cut != nil {
		kill(w)
		return
	}
	if t.Error != nil {
		wire := t.Error.anthropicWire()
		send(anthropicEvent{Type: eventError, Error: &wire})
		return
	}
	closing := anthropicEvent{Type: eventMessageDelta, Delta: &anthropicDelta{StopReason: t.stopReason(), StopSequence: jsonNull}}
	if usage != nil {
		closing.Usage = usage.outputSide()
	}
	if !send(closing) {
		return
	}
	send(anthropicEvent{Type: eventMessageStop})
}

// messageEvents is the ordered list of block events a Turn streams between message_start and
// its terminator: the thinking block, the text block, then one tool_use block per call, each
// as start, deltas and stop. An empty-reply turn yields none. Block indexes count up across
// the channels, as the real API numbers them.
func messageEvents(t Turn) []anthropicEvent {
	var events []anthropicEvent
	index := 0
	block := func(open anthropicBlock, deltas []anthropicDelta) {
		at := index
		index++
		events = append(events, anthropicEvent{Type: eventBlockStart, Index: &at, ContentBlock: &open})
		for _, delta := range deltas {
			d := delta
			events = append(events, anthropicEvent{Type: eventBlockDelta, Index: &at, Delta: &d})
		}
		events = append(events, anthropicEvent{Type: eventBlockStop, Index: &at})
	}

	if t.hasReasoning() {
		var deltas []anthropicDelta
		for _, part := range t.reasoningDeltas() {
			deltas = append(deltas, anthropicDelta{Type: deltaThinking, Thinking: part})
		}
		block(anthropicBlock{Type: blockThinking, Thinking: new(string)}, deltas)
	}
	if parts := t.contentDeltas(); len(parts) > 0 {
		var deltas []anthropicDelta
		for _, part := range parts {
			deltas = append(deltas, anthropicDelta{Type: deltaText, Text: part})
		}
		block(anthropicBlock{Type: blockText, Text: new(string)}, deltas)
	}
	for i, call := range t.ToolCalls {
		var deltas []anthropicDelta
		head, tail := splitHalf(call.arguments())
		deltas = append(deltas, anthropicDelta{Type: deltaInputJSON, PartialJSON: head})
		if tail != "" {
			deltas = append(deltas, anthropicDelta{Type: deltaInputJSON, PartialJSON: tail})
		}
		block(anthropicBlock{
			Type:  blockToolUse,
			ID:    call.callID(i),
			Name:  call.Name,
			Input: json.RawMessage(`{}`),
		}, deltas)
	}
	return events
}

// writeMessagesWhole plays a Turn as a single Messages body — the non-streamed path — with the
// same content blocks the stream opens, whole. An `error` turn is the error body in place of
// the message, the way the API frames one; a `cut` kills the connection after the 200 header,
// before any body, as writeWhole does.
func writeMessagesWhole(w http.ResponseWriter, t Turn, model string) {
	w.Header().Set("Content-Type", "application/json")
	if t.Cut != nil {
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		kill(w)
		return
	}
	if t.Error != nil {
		_ = json.NewEncoder(w).Encode(anthropicErrorBody{Type: eventError, Error: t.Error.anthropicWire()})
		return
	}
	stop := t.stopReason()
	reply := anthropicReply{
		ID:         messageID,
		Type:       "message",
		Role:       "assistant",
		Model:      model,
		Content:    []anthropicBlock{},
		StopReason: &stop,
	}
	if t.hasReasoning() {
		thinking := t.reasoning()
		reply.Content = append(reply.Content, anthropicBlock{Type: blockThinking, Thinking: &thinking})
	}
	if text := t.content(); text != "" {
		reply.Content = append(reply.Content, anthropicBlock{Type: blockText, Text: &text})
	}
	for i, call := range t.ToolCalls {
		reply.Content = append(reply.Content, anthropicBlock{
			Type:  blockToolUse,
			ID:    call.callID(i),
			Name:  call.Name,
			Input: json.RawMessage(call.arguments()),
		})
	}
	if t.Usage != nil {
		reply.Usage = anthropicUsageOf(*t.Usage)
	}
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
