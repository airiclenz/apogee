package stubllm

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// chatCompletionsPath is the one proxied path whose traffic becomes a Turn; every other /v1/
// path (the models probe, llama.cpp's /v1/props) is forwarded and forgotten.
const chatCompletionsPath = "/v1/chat/completions"

// doneSentinel is the payload of the event that terminates an SSE reply. It carries no delta,
// so it is skipped rather than decoded.
const doneSentinel = "[DONE]"

// recordedFileMode is the permission a written fixture gets — a checked-in data file.
const recordedFileMode = 0o644

// timingGrain is what a measured delay is rounded to before it reaches a fixture. Raw
// measurements carry nanosecond noise no server reproduces, and `token_delay: 10.37ms` reads
// as a decision where `token_delay: 10.372413ms` reads as a mistake.
const timingGrain = 10 * time.Microsecond

// Recorder is a recording proxy: it forwards /v1/* to a real upstream and writes what it saw
// back out as a [Script], so a fixture comes from a server that genuinely behaves that way
// rather than from a developer's memory of the wire format.
//
// It is an [http.Handler]. Mount it on a listener, point apogee (or any OpenAI-compatible
// client) at that address, drive the run, then [Recorder.Close] to write the fixture. Every
// completed /v1/chat/completions request becomes one Turn, pre-filled with a `when:` matcher
// over the request's last message, so a recorded script answers a replayed run in the same
// places even when the ordering shifts.
//
// Recording is an explicit act performed by a human at a command line (`stubllm record`), never
// something a `go test` run does: a test that silently re-records its own fixture cannot fail.
type Recorder struct {
	upstream *url.URL
	out      string
	proxy    *httputil.ReverseProxy

	mu    sync.Mutex
	model string
	next  int
	turns map[int]Turn
	// inflight is how many begun requests have not yet filed their Turn. A client stops reading
	// the moment it sees the last event it cares about, so it can be back in its caller's hands
	// while this proxy is still a Read away from the EOF that files the reply — see
	// [Recorder.settle].
	inflight int
}

// settleWait is how long [Recorder.Close] gives replies still in flight. It is a backstop, not a
// budget: the wait normally ends on the last reply filing, in well under a millisecond.
const settleWait = 2 * time.Second

// NewRecorder returns a Recorder that proxies to upstream and writes its fixture to out.
// Neither is optional: a recorder with nowhere to send traffic, or nowhere to put the result,
// is a mistake worth catching before a session is driven through it.
func NewRecorder(upstream, out string) (*Recorder, error) {
	target, err := url.Parse(upstream)
	if err != nil || target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf(
			"stubllm: %q is not an upstream URL (want e.g. http://127.0.0.1:1111)", upstream)
	}
	if out == "" {
		return nil, errors.New("stubllm: a recorder needs a path to write the fixture to")
	}

	recorder := &Recorder{upstream: target, out: out, turns: map[int]Turn{}}
	recorder.proxy = &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(target)
			request.Out.Host = target.Host
		},
		ModifyResponse: recorder.capture,
		// A proxy that buffers is a proxy that invents timing. -1 flushes every write
		// through at once, so what the recorder measures is the upstream's own pacing and
		// what the client downstream sees is the stream it would have seen directly.
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, request *http.Request, err error) {
			// A request that failed before its reply could be captured files no Turn, so it is
			// settled here instead — otherwise Close would wait out its whole backstop for a
			// reply that is never coming.
			recorder.settle(captureOf(request))
			http.Error(w, fmt.Sprintf("stubllm: upstream %s: %v", target, err), http.StatusBadGateway)
		},
	}
	return recorder, nil
}

// ServeHTTP proxies one request. Paths outside /v1/ are refused rather than forwarded: the
// recorder stands in for an OpenAI-compatible server and nothing else, and a client reaching
// for another path has the wrong address.
func (r *Recorder) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if !strings.HasPrefix(request.URL.Path, "/v1/") {
		http.NotFound(w, request)
		return
	}
	if request.Method == http.MethodPost && request.URL.Path == chatCompletionsPath {
		request = r.begin(request)
	}
	r.proxy.ServeHTTP(w, request)
}

// Script is what has been recorded so far: the model the requests named and one Turn per
// completed request, in the order the requests arrived.
func (r *Recorder) Script() Script {
	r.mu.Lock()
	defer r.mu.Unlock()

	numbers := slices.Sorted(maps.Keys(r.turns))
	turns := make([]Turn, 0, len(numbers))
	for _, n := range numbers {
		turns = append(turns, r.turns[n])
	}
	return Script{Model: r.model, Turns: turns}
}

// Close writes the recorded Script to the output path, under a header naming where it came
// from, and reports what is wrong with it — if anything.
//
// An unplayable recording is still WRITTEN and then reported: a real server can answer with a
// shape the format refuses (content alongside tool calls, say), and a fixture a human can fix
// by hand is worth more than a deleted one plus an error message.
func (r *Recorder) Close() error {
	r.settleInflight()

	script := r.Script()
	if len(script.Turns) == 0 {
		return fmt.Errorf("stubllm: nothing recorded — no completion request reached %s", r.upstream)
	}

	body, err := Marshal(script)
	if err != nil {
		return err
	}
	header := fmt.Sprintf(
		"# Recorded by `stubllm record` from %s on %s.\n# Replay it with: stubllm serve --script %s\n",
		r.upstream, time.Now().Format(time.DateOnly), filepath.Base(r.out),
	)
	if err := os.WriteFile(r.out, append([]byte(header), body...), recordedFileMode); err != nil {
		return fmt.Errorf("stubllm: write %s: %w", r.out, err)
	}
	if err := script.Validate(); err != nil {
		return fmt.Errorf("stubllm: %s needs a hand edit: %w", r.out, err)
	}
	return nil
}

// begin reads the request body so the recorder knows what was asked, then hands the request
// back with a replayable body and a capture attached for the reply half to fill in. The body
// is read whole and uncapped on purpose: this proxy forwards its own operator's traffic, and a
// truncated request would reach the upstream as a corrupt one.
func (r *Recorder) begin(request *http.Request) *http.Request {
	body, err := io.ReadAll(request.Body)
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.ContentLength = int64(len(body))
	if err != nil {
		return request
	}

	var decoded chatRequest
	if err := json.Unmarshal(body, &decoded); err != nil {
		return request
	}

	entry := &capture{lastMessage: lastText(decoded.messages())}
	r.mu.Lock()
	if r.model == "" {
		r.model = decoded.Model
	}
	entry.n = r.next
	r.next++
	r.inflight++
	r.mu.Unlock()

	return request.WithContext(context.WithValue(request.Context(), captureKey{}, entry))
}

// capture attaches the recording reader to a reply on its way back to the client. It runs as
// the proxy's ModifyResponse, which is the only place with both the status and the body still
// unread.
func (r *Recorder) capture(reply *http.Response) error {
	entry := captureOf(reply.Request)
	if entry == nil {
		return nil
	}

	entry.status = reply.StatusCode
	entry.contentType = reply.Header.Get("Content-Type")
	entry.location = reply.Header.Get("Location")
	reply.Body = &recordingBody{
		body: reply.Body,
		read: entry.write,
		done: func() { r.finish(entry) },
	}
	return nil
}

// finish files a completed reply as its Turn, keyed by the request's arrival number so
// overlapping requests still land in the script in the order the upstream saw them.
func (r *Recorder) finish(entry *capture) {
	turn := entry.turn()

	r.mu.Lock()
	r.turns[entry.n] = turn
	r.mu.Unlock()
	r.settle(entry)
}

// settle marks a request as no longer in flight, exactly once and whatever became of it. nil and
// a capture already settled are both no-ops: the proxy can reach its error handler after a reply
// was captured, and a request outside /v1/chat/completions carries no capture at all.
func (r *Recorder) settle(entry *capture) {
	if entry == nil {
		return
	}
	entry.settled.Do(func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.inflight--
	})
}

// settleInflight waits for every begun request to file its Turn — or to give up on one — so a
// fixture written the instant the last client returned still holds that client's last reply.
//
// This is the recorder's one clock, and it is here because the alternative is a silent hole: a
// streaming client stops at the `[DONE]` event and hands control back to its caller, while the
// proxy is still one Read short of the EOF that files the Turn. Close a millisecond later and the
// fixture is short a turn, with nothing to say so.
func (r *Recorder) settleInflight() {
	deadline := time.Now().Add(settleWait)
	for {
		r.mu.Lock()
		inflight := r.inflight
		r.mu.Unlock()
		if inflight <= 0 || !time.Now().Before(deadline) {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

// captureKey is the context key the request half uses to hand its capture to the reply half.
type captureKey struct{}

// captureOf returns the capture a request carries, or nil when it is not one being recorded.
func captureOf(request *http.Request) *capture {
	if request == nil {
		return nil
	}
	entry, _ := request.Context().Value(captureKey{}).(*capture)
	return entry
}

// capture is one request/reply pair in flight: what was asked, and the reply bytes as they
// arrive with the times they arrived at.
type capture struct {
	n           int
	settled     sync.Once
	lastMessage string
	status      int
	contentType string
	location    string

	pending []byte
	events  []streamedEvent
	body    bytes.Buffer
}

// streamedEvent is one SSE `data:` payload and the moment the proxy read it.
type streamedEvent struct {
	data string
	at   time.Time
}

// recordingBody is the reply body on its way to the client, timestamping every chunk the proxy
// reads through it. Those times are the only place a fixture's token_delay can come from — SSE
// events carry no timing of their own — and done fires exactly once, on EOF or on Close.
type recordingBody struct {
	body io.ReadCloser
	read func(p []byte, at time.Time)
	done func()
	once sync.Once
}

func (b *recordingBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if n > 0 {
		b.read(p[:n], time.Now())
	}
	if err != nil {
		b.once.Do(b.done)
	}
	return n, err
}

func (b *recordingBody) Close() error {
	b.once.Do(b.done)
	return b.body.Close()
}

// write takes one chunk of reply body. A streamed reply is parsed into events as they complete,
// so only the small payloads are kept; every other shape is buffered whole, because a JSON
// completion or an error page is only meaningful entire.
func (c *capture) write(p []byte, at time.Time) {
	if !c.isStream() {
		c.body.Write(p)
		return
	}

	// Carriage returns are dropped so CRLF-framed and LF-framed servers parse identically;
	// a JSON payload carries its own returns escaped, never raw.
	c.pending = append(c.pending, bytes.ReplaceAll(p, []byte("\r"), nil)...)
	for {
		payload, rest, found := cutEvent(c.pending)
		if !found {
			return
		}
		c.pending = rest
		if payload != "" {
			c.events = append(c.events, streamedEvent{data: payload, at: at})
		}
	}
}

// isStream reports whether the reply is a successful SSE stream — the only shape whose chunk
// timing means anything.
func (c *capture) isStream() bool {
	return c.status/100 == 2 && strings.HasPrefix(c.contentType, "text/event-stream")
}

// turn renders the captured pair as the Turn that replays it.
func (c *capture) turn() Turn {
	turn := Turn{}
	if c.lastMessage != "" {
		turn.When = &Match{LastMessage: regexp.QuoteMeta(c.lastMessage)}
	}

	var finish string
	switch {
	case c.status/100 != 2:
		turn.HTTP = &HTTPReply{
			Status:      c.status,
			Body:        c.body.String(),
			Location:    c.location,
			ContentType: c.contentType,
		}
	case c.isStream():
		finish = c.fillFromStream(&turn)
	default:
		finish = c.fillFromWhole(&turn)
	}

	// Only a finish_reason the format would not have derived is worth writing down.
	if finish != "" && finish != turn.finishReason() {
		turn.FinishReason = finish
	}
	return turn
}

// fillFromStream reassembles a streamed reply into the Turn that reproduces it and returns the
// finish reason it ended on. Timing is measured across every delta-carrying event, because a
// server paces reasoning, content and tool-call fragments alike.
func (c *capture) fillFromStream(turn *Turn) string {
	var text, reasoning strings.Builder
	var arrivals []time.Time
	var chunkRunes []int
	var calls callSet
	var finish, reasoningField string

	for _, event := range c.events {
		if event.data == doneSentinel {
			continue
		}
		var envelope sseEnvelope
		if err := json.Unmarshal([]byte(event.data), &envelope); err != nil {
			continue
		}
		if envelope.Usage != nil {
			turn.Usage = usageFrom(envelope.Usage)
		}

		carried := false
		for _, choice := range envelope.Choices {
			if content := choice.Delta.Content; content != "" {
				text.WriteString(content)
				chunkRunes = append(chunkRunes, len([]rune(content)))
				carried = true
			}
			if thought, spelling := capturedReasoning(choice.Delta.ReasoningContent, choice.Delta.Reasoning); thought != "" {
				reasoning.WriteString(thought)
				reasoningField = spelling
				carried = true
			}
			for _, fragment := range choice.Delta.ToolCalls {
				calls.add(fragment)
				carried = true
			}
			if choice.FinishReason != "" {
				finish = choice.FinishReason
			}
		}
		if carried {
			arrivals = append(arrivals, event.at)
		}
	}

	turn.Text, turn.Reasoning, turn.ToolCalls = text.String(), reasoning.String(), calls.done()
	turn.ReasoningField = reasoningField
	turn.TokenDelay = medianGap(arrivals)
	if runes := median(chunkRunes); runes > 0 {
		turn.ChunkRunes = runes
	}
	return finish
}

// fillFromWhole reads a non-streamed completion into the Turn and returns its finish reason.
// TokenDelay stays zero: there were no chunks to space out.
func (c *capture) fillFromWhole(turn *Turn) string {
	var reply wholeReply
	if err := json.Unmarshal(c.body.Bytes(), &reply); err != nil || len(reply.Choices) == 0 {
		return ""
	}

	choice := reply.Choices[0]
	turn.Text = choice.Message.Content
	if thought, spelling := capturedReasoning(choice.Message.ReasoningContent, choice.Message.Reasoning); thought != "" {
		turn.Reasoning, turn.ReasoningField = thought, spelling
	}
	for _, call := range choice.Message.ToolCalls {
		turn.ToolCalls = append(turn.ToolCalls, ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: call.Function.Arguments,
		})
	}
	if reply.Usage != nil {
		turn.Usage = usageFrom(reply.Usage)
	}
	return choice.FinishReason
}

// capturedReasoning is the thinking channel one captured payload carried, paired with the
// `reasoning_field` value that reproduces its spelling — EMPTY for `reasoning_content`, which is
// what an unset key already means, so a recording of a `reasoning_content` server is written
// exactly as it was before this knob existed.
//
// The precedence is the provider's: `reasoning_content` wins wherever it is non-empty, and the
// bare `reasoning` Ollama and OpenRouter send is read only where it is not. It is tested for
// NON-EMPTINESS and never for presence, because LM Studio always sends the key and leaves it
// empty when the model did not reason. This is the recorder's ONE decode site for the channel: a
// third spelling is a line here and nowhere else.
func capturedReasoning(content, bare string) (text, field string) {
	if content != "" {
		return content, ""
	}
	if bare != "" {
		return bare, reasoningFieldBare
	}
	return "", ""
}

// callSet reassembles tool calls from the fragments a stream splits them into, keeping them in
// the order the upstream numbered them.
type callSet struct {
	builders []*callBuilder
}

// callBuilder is one call under construction: its wire index, what is known of it, and the
// argument text accumulated so far.
type callBuilder struct {
	index int
	call  ToolCall
	args  strings.Builder
}

// add folds one streamed fragment into the call it belongs to.
func (s *callSet) add(fragment sseToolCall) {
	builder := s.builder(fragment.Index)
	if fragment.ID != "" {
		builder.call.ID = fragment.ID
	}
	if fragment.Function.Name != "" {
		builder.call.Name = fragment.Function.Name
	}
	builder.args.WriteString(fragment.Function.Arguments)
}

// builder returns the builder for a wire index, starting one the first time that index is seen.
func (s *callSet) builder(index int) *callBuilder {
	for _, builder := range s.builders {
		if builder.index == index {
			return builder
		}
	}
	builder := &callBuilder{index: index}
	s.builders = append(s.builders, builder)
	return builder
}

// done returns the finished calls, in wire order.
func (s *callSet) done() []ToolCall {
	if len(s.builders) == 0 {
		return nil
	}
	slices.SortFunc(s.builders, func(a, b *callBuilder) int { return cmp.Compare(a.index, b.index) })

	calls := make([]ToolCall, 0, len(s.builders))
	for _, builder := range s.builders {
		call := builder.call
		call.Arguments = builder.args.String()
		calls = append(calls, call)
	}
	return calls
}

// cutEvent splits the first complete SSE event off buf and returns its `data:` payload. An
// event without a data field yields an empty payload, which the caller drops.
func cutEvent(buf []byte) (string, []byte, bool) {
	end := bytes.Index(buf, []byte("\n\n"))
	if end < 0 {
		return "", buf, false
	}

	event, rest := string(buf[:end]), buf[end+2:]
	for _, line := range strings.Split(event, "\n") {
		if data, ok := strings.CutPrefix(strings.TrimSpace(line), "data:"); ok {
			return strings.TrimSpace(data), rest, true
		}
	}
	return "", rest, true
}

// usageFrom reads the wire's accounting object into the script's.
func usageFrom(wire *usageWire) *Usage {
	usage := &Usage{Prompt: wire.PromptTokens, Completion: wire.CompletionTokens}
	if wire.PromptTokensDetails != nil {
		usage.Cached = wire.PromptTokensDetails.CachedTokens
	}
	return usage
}

// medianGap is the typical pause between arrivals: the median gap between successive
// timestamps, rounded to timingGrain. The median is what makes it usable — it ignores the long
// wait before the first token and any one stall the network contributed.
func medianGap(at []time.Time) time.Duration {
	if len(at) < 2 {
		return 0
	}

	gaps := make([]time.Duration, 0, len(at)-1)
	for i := 1; i < len(at); i++ {
		gaps = append(gaps, at[i].Sub(at[i-1]))
	}
	return median(gaps).Round(timingGrain)
}

// median is the middle value of xs, or the zero value when xs is empty.
func median[T cmp.Ordered](xs []T) T {
	var zero T
	if len(xs) == 0 {
		return zero
	}
	sorted := slices.Clone(xs)
	slices.Sort(sorted)
	return sorted[len(sorted)/2]
}
