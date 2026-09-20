package provider

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// DeltaKind tags a streamed Delta. The set mirrors the TS oracle's CompletionDelta union
// so the loop's stream consumer (P1.2) can switch on it directly.
type DeltaKind string

const (
	// DeltaContent carries a chunk of assistant text.
	DeltaContent DeltaKind = "content"
	// DeltaThinking carries a chunk of the reasoning channel (`reasoning_content`, or its
	// `reasoning` alias — see reasoningChannel).
	DeltaThinking DeltaKind = "thinking"
	// DeltaToolCall carries one fully-accumulated tool call. Nothing is emitted mid-stream:
	// every call of the reply is yielded, in wire-index order, immediately before the
	// terminal Done, so a server interleaving parallel calls cannot have them mis-joined.
	DeltaToolCall DeltaKind = "tool_call"
	// DeltaDone is the terminal event: the finish reason and (when the server sent it)
	// token usage. Exactly one Done ends a successful stream.
	DeltaDone DeltaKind = "done"
	// DeltaError is a terminal fault (transport, bad status, oversized tool args, a reply
	// past the text cap, a stream that carried nothing but undecodable chunks). No Done
	// follows it.
	DeltaError DeltaKind = "error"
	// DeltaContextOverflow is the terminal "prompt too long" signal (a 400 the server
	// flagged as a context-window rejection).
	DeltaContextOverflow DeltaKind = "context_overflow"
)

// Delta is one event from a streamed completion. Only the fields relevant to Kind are
// populated; the rest are zero.
type Delta struct {
	Kind         DeltaKind
	Content      string
	Thinking     string
	ToolCall     *ToolCall
	FinishReason string
	Usage        *Usage
	// Model is meaningful on the terminal DeltaDone: the model id the server put on the reply's
	// chunks — what it actually ANSWERED with, which is not always what the request asked for
	// (an alias resolved server-side, an aggregator routing to a backing model). It is read off
	// the first chunk that carries one, since every chunk of a reply repeats it. Empty when the
	// server sends none, which a consumer treats as "unknown", never as "the bound model".
	Model string
	Err   string
	// Retryable is meaningful only on DeltaError: it reports that the fault's class is one
	// the client would have retried had it arrived as an HTTP status (429, 5xx, or an
	// aggregator's "provider_unavailable"), or a body read that ended in a mid-stream EOF, a
	// network timeout or the idle cut (errStreamIdle — an upstream silent for the whole
	// WithStreamIdleTimeout window, before its headers or mid-body), so the caller may
	// re-stream the same request. It is never set on
	// DeltaContextOverflow — a prompt too long stays too long — nor on the text-cap overflow,
	// and the provider itself never acts on it, because retrying mid-stream is the loop's call.
	Retryable bool
	// MalformedChunks is meaningful on the terminal DeltaDone and DeltaError of a parsed
	// stream: how many `data:` payloads failed to decode and were skipped. A fault's Err
	// already names a non-zero count (malformedChunksNote); the field is the same number,
	// machine-readable, and it is what lets a stream capture be bisected to the chunk a
	// server shaped wrong. Zero on every stream that decoded cleanly.
	MalformedChunks int
}

// Stream performs a streaming completion and yields Deltas as they arrive. It is the SSE
// counterpart of Respond: faults and the bad-status path surface as a terminal
// DeltaError / DeltaContextOverflow rather than a Go error, so the consumer drives a
// single range loop (matching the TS AsyncIterable). The HTTP request is issued lazily on
// first iteration; the body and the request context are released when the range ends
// (whether drained or broken early). The stream has two deadlines: the caller's ctx — a
// cancelled or expired ctx ends the body read and surfaces as a terminal DeltaError — and the
// idle timeout (WithStreamIdleTimeout, 10m by default; 0 disables it). An upstream that stays
// SILENT for the whole idle window, whether before its response headers or between two body
// reads, is cut and surfaces as a terminal DeltaError marked Retryable (errStreamIdle); bytes of
// any kind — reasoning deltas, SSE keep-alive comments — reset the clock, so a slow generation
// is never cut, only a stalled one. That default supersedes the 2026-08 record (CHANGELOG.md,
// "The caller's context is documented and pinned as the stream's only deadline") that Stream
// added no inter-chunk idle timeout for the sake of a slow local prefill: that persona now sets
// stream-idle-timeout to 0 or to a longer window. Content plus reasoning is capped at
// maxReplyTextBytes; crossing it ends the stream with a non-retryable terminal DeltaError. A
// body read that fails before the terminator — the connection dropped mid-chunk
// (io.ErrUnexpectedEOF), a network timeout or the idle cut — is a terminal DeltaError marked
// Retryable, so the loop can re-stream it the way it re-streams an in-band 502; a chunk that
// fails to decode is skipped and counted, never dropped silently (Delta.MalformedChunks), and a
// stream that decoded nothing at all but skipped some is a fault naming that count rather than
// an empty Done.
func (c *Client) Stream(ctx context.Context, req Request) iter.Seq[Delta] {
	return func(yield func(Delta) bool) {
		req.Stream = true
		body, carriedEffort, err := c.encode(req)
		if err != nil {
			yield(Delta{Kind: DeltaError, Err: fmt.Sprintf("apogee: marshal request: %v", err)})
			return
		}

		// Streaming is not bounded by a per-attempt timeout — a long generation is not a
		// fault; retries cover only connection/status before the first byte. The idle window
		// bounds the wait for headers instead: the whole of send — attempts and hold-offs —
		// runs under a child ctx the headers timer cancels, and a stall it cut is reported as
		// the idle fault, never as a caller cancel.
		headersCtx, cancelHeaders := context.WithCancel(ctx)
		defer cancelHeaders()
		headers := newIdleTimer(c.streamIdleTimeout, cancelHeaders)
		resp, cancel, err := c.send(headersCtx, body, 0)
		if headers.stopOrFired() && ctx.Err() == nil {
			if err == nil {
				cancel()
				_ = resp.Body.Close()
			}
			yield(Delta{
				Kind:      DeltaError,
				Err:       fmt.Sprintf("apogee: await response: %v", idleFault(c.streamIdleTimeout)),
				Retryable: true,
			})
			return
		}
		if err != nil {
			yield(Delta{Kind: DeltaError, Err: err.Error()})
			return
		}
		defer cancel()
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			yield(c.statusDelta(resp, carriedEffort))
			return
		}

		// The body layer's idle cut sits beneath the capture tee, so both codecs inherit it and
		// the observer's record ends where the codec's parse did: the reader closes the body when
		// the window elapses with no byte read, the pending Read unblocks, and the codec sees
		// errStreamIdle through scanner.Err — the one place both wires render a read fault.
		stream := io.Reader(resp.Body)
		if c.streamIdleTimeout > 0 {
			idle := newIdleBody(resp.Body, c.streamIdleTimeout)
			defer idle.stop()
			stream = idle
		}

		// Wire capture, when armed: tee the body as it is read and hand the observer one record
		// on whichever return ends the parse — [DONE], an in-band error, a read fault, a consumer
		// that broke, or the server closing. The codec never sees the observer: the tee sits in
		// front of it, so the capture is the same whichever wire is parsing. Unarmed, not a byte
		// is kept.
		if c.wireObserver != nil {
			var raw bytes.Buffer
			stream = io.TeeReader(stream, &raw)
			defer func() { c.observeWire(WireResponse, c.streamCapture(raw.Bytes())) }()
		}
		c.codec.parseSSE(stream, carriedEffort, yield)
	}
}

// errStreamIdle is the sentinel behind the idle cut. It carries no `apogee:` prefix because
// every path that renders it supplies its own — the codecs' `apogee: read stream: %v` over
// scanner.Err, the pre-header `apogee: await response: %v` — so the rendered fault reads
// `apogee: read stream: upstream stream idle for 10m0s`, not doubled.
var errStreamIdle = errors.New("upstream stream idle")

// idleFault is the error one idle cut reports: errStreamIdle wrapped with the window that
// elapsed, so errors.Is still matches the sentinel and the text names the bound crossed.
func idleFault(window time.Duration) error {
	return fmt.Errorf("%w for %s", errStreamIdle, window)
}

// idleTimer is one armed idle window: a timer that runs fire when the window elapses, and the
// record that it did — set BEFORE fire runs, so whoever fire unblocks reads it as fired. A
// disarmed idleTimer (idle timeout off) has no timer and never reports fired.
type idleTimer struct {
	timer *time.Timer
	fired atomic.Bool
}

// newIdleTimer arms an idleTimer over window, running fire when it elapses; a window of 0 (idle
// timeout off) returns a disarmed one.
func newIdleTimer(window time.Duration, fire func()) *idleTimer {
	t := &idleTimer{}
	if window > 0 {
		t.timer = time.AfterFunc(window, func() {
			t.fired.Store(true)
			fire()
		})
	}
	return t
}

// reset re-arms the window from now.
func (t *idleTimer) reset(window time.Duration) {
	if t.timer != nil {
		t.timer.Reset(window)
	}
}

// stopOrFired stops the timer and reports whether it had already fired.
func (t *idleTimer) stopOrFired() bool {
	if t.timer == nil {
		return false
	}
	t.timer.Stop()
	return t.fired.Load()
}

// idleBody is a response body under the idle cut: every Read that returns bytes re-arms the
// window; a window that elapses closes the underlying body so a pending Read unblocks, and
// that read's error — or the next read's, on a body the timer closed between reads — is
// replaced by the idle fault. A private type, so the cut lives in one place beneath both
// codecs.
type idleBody struct {
	body   io.ReadCloser
	window time.Duration
	idle   *idleTimer
}

// newIdleBody wraps body under a window > 0, arming the timer at once: the clock starts at the
// headers, the last bytes the upstream is known to have sent.
func newIdleBody(body io.ReadCloser, window time.Duration) *idleBody {
	return &idleBody{
		body:   body,
		window: window,
		idle:   newIdleTimer(window, func() { _ = body.Close() }),
	}
}

// Read reads from the body, re-arming the window on every read that succeeded, and reports the
// idle fault in place of the transport's own error once the window has fired.
func (b *idleBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if err == nil {
		b.idle.reset(b.window)
		return n, nil
	}
	if b.idle.fired.Load() {
		return n, idleFault(b.window)
	}
	return n, err
}

// stop disarms the window once the range ends, so the timer cannot close a body the caller has
// already released.
func (b *idleBody) stop() { b.idle.stopOrFired() }

// streamCapture is what a WireResponse record holds for a stream: on the openai wire the
// `data:` payloads joined by newlines, the shape WireRecord documents and the Inspector reads
// (the SSE framing is not the protocol, the payloads are); on any other wire the body as
// received, because its event-typed framing IS the protocol.
func (c *Client) streamCapture(raw []byte) []byte {
	if c.wire != WireOpenAI {
		return raw
	}
	return joinDataPayloads(raw)
}

// joinDataPayloads extracts the `data:` payloads of an SSE body under exactly the line rule
// the openai parser reads by — the same scanner, the same trim, the same prefix, and the same
// stop at the [DONE] terminator (included: it is the protocol) — so the record holds every
// payload the parser saw and nothing it skipped or never reached.
func joinDataPayloads(raw []byte) []byte {
	scanner := newSSEScanner(bytes.NewReader(raw))
	var payloads []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		payloads = append(payloads, data)
		if data == sseDone {
			break
		}
	}
	return []byte(strings.Join(payloads, "\n"))
}

// newSSEScanner is the line scanner every SSE read goes through: a 64 KiB initial buffer that
// may grow to hold one tool call's whole argument payload plus framing. One constructor, so
// the parser and the capture split lines under the same bound.
func newSSEScanner(body io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxToolCallBytes+64*1024)
	return scanner
}

// statusDelta renders a non-2xx streamed response as a terminal Delta, mirroring statusError
// on the streaming surface — the same maxErrorBodyBytes read cap, the same classify verdict
// over the raw body, the same thinking-effort hint: carriedEffort reports that the failed
// request expressed a thinking effort in some dialect, and a turn is where that failure
// actually lands, since the loop streams. The Delta keeps Retryable false whatever the
// fault's own verdict: send already retried a 429/5xx before one reached here (see
// Delta.Retryable — the class the client WOULD have retried had it arrived as a status is
// the in-band case, inBandErrorDelta's).
func (c *Client) statusDelta(resp *http.Response, carriedEffort bool) Delta {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	text := c.sanitize(string(raw))
	c.observeWire(WireResponse, []byte(text))
	f := classify(resp.StatusCode, "", string(raw), carriedEffort)
	if f.overflow {
		return Delta{Kind: DeltaContextOverflow, Err: "apogee: context window exceeded: " + text}
	}
	message := upstreamStatusText(f.code, text, resp.Header.Get("Location"))
	if f.hinted {
		message += " " + thinkingEffortHint
	}
	return Delta{Kind: DeltaError, Err: message}
}

// providerUnavailable is the aggregator error_type slug for "the upstream I routed to is
// gone" — a transient class even when it arrives with a 4xx or a non-numeric code.
const providerUnavailable = "provider_unavailable"

// inBandErrorDelta renders an in-band error member as a terminal Delta, mirroring
// statusDelta but for a failure the server wrapped in an HTTP 200. The text is the whole raw
// SSE payload (sanitised), so provider-specific metadata — OpenRouter's metadata.raw, say —
// reaches the user verbatim instead of being flattened away; the classify verdict is read
// off the raw message. This is the one renderer that surfaces the fault's retryable verdict:
// an in-band 502 is treated exactly like a 502 status would have been by send, with the
// error_type slug covering the shapes that carry no usable code. A hinted fault appends
// thinkingEffortHint exactly as statusDelta does — an effort failure an aggregator wrapped
// in a 200 needs the same explanation as one that arrived as a status.
func (c *Client) inBandErrorDelta(werr wireError, raw string, carriedEffort bool) Delta {
	f := classify(werr.intCode(), werr.ErrorType, werr.Message, carriedEffort)
	text := fmt.Sprintf("apogee: upstream in-band error %d: %s", f.code, c.sanitize(raw))
	if f.overflow {
		return Delta{Kind: DeltaContextOverflow, Err: text}
	}
	if f.hinted {
		text += " " + thinkingEffortHint
	}
	return Delta{Kind: DeltaError, Err: text, Retryable: f.retryable}
}

// malformedOnlyErrFmt is the fault for a stream that decoded nothing the consumer could
// commit — no text, no tool call — while skipping %d chunks it could not decode: the count is
// the whole diagnosis, and it is what a stream capture is bisected from.
const malformedOnlyErrFmt = "apogee: stream carried no text and no tool calls (%d malformed chunks skipped)"

// malformedChunksNote is the suffix a terminal fault carries when the stream skipped chunks it
// could not decode — empty when it skipped none, so a clean stream's fault text is unchanged.
func malformedChunksNote(count int) string {
	if count == 0 {
		return ""
	}
	return fmt.Sprintf(" (%d malformed chunks skipped)", count)
}

// isTransientReadError reports whether a body-read fault is one the same request can be expected
// to survive — three classes: the connection closed mid-chunk (io.ErrUnexpectedEOF — a server or
// a proxy dropping the stream), a network timeout, or the idle cut (errStreamIdle — an upstream
// silent for the whole idle window, which is how a stalled proxy or a dead generation shows up
// before any 504 does). Every other read fault — a line past the scanner's buffer, a broken
// transport — stays non-retryable. A cancelled or expired ctx surfaces through here too (an
// expired one even reads as a timeout), but the loop checks ctx.Err() before it ever consults
// Retryable, so the verdict is moot for it.
func isTransientReadError(err error) bool {
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, errStreamIdle) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// openToolCalls is the ordered set of tool calls one streamed reply has under accumulation.
// A server addresses a call by its wire index when it sends one, by its id when it does not,
// and by "the call addressed last" when it sends neither — so interleaved parallel calls are
// joined by index rather than by arrival order, and a server repeating one id on every
// fragment continues one call instead of splitting it into many. Nothing is emitted until
// flush runs at the end of the stream. The zero value is ready to use.
type openToolCalls struct {
	entries []*openToolCall
	// last is the call a fragment addressed most recently — the target for a fragment that
	// carries neither an index nor an id. nil until the first call opens.
	last *openToolCall
	// bytes is the sum of accumulated argument bytes across every open call, so a server
	// opening many calls cannot multiply maxToolCallBytes by their number.
	bytes int
}

// openToolCall is one call under accumulation with the wire index that addresses it;
// wireIndex is noIndex for a call the server opened without one.
type openToolCall struct {
	call      ToolCall
	wireIndex int
}

// noIndex marks a call opened by a server that sends no wire index. It sorts after every
// indexed call, so those calls keep their arrival order at the end of the flush.
const noIndex = -1

// fold folds one streamed fragment into the set, reporting whether the accumulated argument
// bytes crossed maxToolCallBytes. A fragment addressing nothing — no index, no id, and no
// call yet open — is dropped silently, as it always has been.
func (o *openToolCalls) fold(frag sseToolCall) bool {
	target := o.address(frag)
	if target == nil {
		return false
	}
	o.last = target
	// An id or name arriving on a later fragment fills in on the call it addresses; one
	// arriving on top of a value already accumulated never overwrites it.
	if target.call.ID == "" {
		target.call.ID = frag.ID
	}
	if target.call.Function.Name == "" {
		target.call.Function.Name = frag.Function.Name
	}
	target.call.Function.Arguments += frag.Function.Arguments
	o.bytes += len(frag.Function.Arguments)
	return o.bytes > maxToolCallBytes
}

// address resolves the call a fragment belongs to, opening one where the fragment may. An
// index-bearing fragment carrying neither an id nor a name never opens a call — that is how
// the provider avoids manufacturing a nameless, id-less call for the loop to report.
func (o *openToolCalls) address(frag sseToolCall) *openToolCall {
	if frag.Index != nil {
		if e := o.atIndex(*frag.Index); e != nil {
			return e
		}
		if frag.ID != "" || frag.Function.Name != "" {
			return o.open(*frag.Index)
		}
		return o.last
	}
	if frag.ID != "" {
		if e := o.withID(frag.ID); e != nil {
			return e
		}
		return o.open(noIndex)
	}
	return o.last
}

// atIndex returns the open call at a wire index, or nil when none is open there.
func (o *openToolCalls) atIndex(index int) *openToolCall {
	for _, e := range o.entries {
		if e.wireIndex != noIndex && e.wireIndex == index {
			return e
		}
	}
	return nil
}

// withID returns the open call carrying an id, or nil when none does.
func (o *openToolCalls) withID(id string) *openToolCall {
	for _, e := range o.entries {
		if e.call.ID == id {
			return e
		}
	}
	return nil
}

// open appends a fresh call at a wire index and returns it; fold fills in its id and name.
func (o *openToolCalls) open(wireIndex int) *openToolCall {
	e := &openToolCall{call: ToolCall{Type: "function"}, wireIndex: wireIndex}
	o.entries = append(o.entries, e)
	return e
}

// flush yields every accumulated call — indexed calls in ascending index order, then any
// call opened without an index in arrival order — and reports false when the consumer broke.
// It runs immediately before the terminal Done on both terminal paths, so the
// DeltaToolCall* -> DeltaDone ordering every consumer sees is preserved.
func (o *openToolCalls) flush(yield func(Delta) bool) bool {
	ordered := make([]*openToolCall, len(o.entries))
	copy(ordered, o.entries)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if (a.wireIndex == noIndex) != (b.wireIndex == noIndex) {
			return b.wireIndex == noIndex
		}
		if a.wireIndex == noIndex {
			return false
		}
		return a.wireIndex < b.wireIndex
	})
	for _, e := range ordered {
		call := e.call
		if !yield(Delta{Kind: DeltaToolCall, ToolCall: &call}) {
			return false
		}
	}
	return true
}

// sseChunk is one decoded SSE data event from a streamed completion. Model is the id the server
// answered with, repeated on every chunk of a reply; absent on servers that omit it.
type sseChunk struct {
	Model   string `json:"model"`
	Choices []struct {
		Delta        sseDelta `json:"delta"`
		FinishReason string   `json:"finish_reason"`
	} `json:"choices"`
	Usage *usageJSON `json:"usage"`
	// Error is the in-band failure member: present only when the server reported an error
	// inside an otherwise-successful stream. Absent on every healthy chunk, so a server that
	// never sends one keeps byte-identical behaviour.
	Error *wireError `json:"error"`
}

// sseDelta is the incremental payload of one streamed choice. It is a named type rather than
// an inline struct so the thinking-channel precedence can hang on it as a method: the embedded
// reasoningChannel carries both wire spellings and the thinking() helper that picks between
// them, identically to the whole reply's chatResponseMessage.
type sseDelta struct {
	reasoningChannel
	Content   string        `json:"content"`
	ToolCalls []sseToolCall `json:"tool_calls"`
}

// sseToolCall is a tool-call fragment within a streamed delta: the first fragment carries
// the id and (usually) the name, later fragments carry argument continuations. Index is the
// wire index of the call the fragment belongs to; it is a POINTER because index 0 is legal
// and an absent index must not read as one.
type sseToolCall struct {
	ID       string `json:"id"`
	Index    *int   `json:"index"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}
