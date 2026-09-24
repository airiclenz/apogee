package provider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
)

// Attempt is the measurement of one HTTP attempt a streamed call made against its Upstream — the
// payload of a DeltaAttempt (ADR 0085). Every duration is timed from the attempt's send, the
// moment its POST was issued, so the four read as one timeline: TTFB ≤ TTFT ≤ Last ≤ Duration
// wherever each was reached, and 0 where it was not.
type Attempt struct {
	// Server is the server entry's name, as WithServerIdentity stamped it.
	Server string
	// Endpoint is the entry's endpoint redacted to scheme, host and path — userinfo, query and
	// fragment stripped (redactEndpoint), so no credential a URL carries reaches a record.
	Endpoint string
	// Model is the model id the server answered with (Delta.Model on the terminal Done) and,
	// where it named none or the attempt never got that far, the model id the request asked
	// for — so a failed attempt still files under the model it was made for.
	Model string
	// RequestID is shared by every attempt of one Stream call, so retries group under the call
	// that made them; it is random per call and carries nothing of the request.
	RequestID string
	// Index is the attempt's 0-based position within its call: 0 for the first POST, 1 for the
	// first retry, and so on.
	Index int
	// TTFB is send → the first body byte of any kind, an SSE keep-alive comment included; 0 when
	// no byte was read through the stream (an attempt abandoned for retry, a transport fault).
	TTFB time.Duration
	// TTFT is send → the first model delta of any kind — reasoning, content or a tool call; a
	// keep-alive comment never advances it. 0 when the attempt produced none.
	TTFT time.Duration
	// Last is send → the last model delta, so output tokens ÷ (Last − TTFT) is the generation
	// rate. 0 when the attempt produced none.
	Last time.Duration
	// Duration is send → the attempt's end, however it ended.
	Duration time.Duration
	// OutputTokens is the completion token count the server reported on the terminal Done's
	// usage; 0 when it reported none, which a consumer reads as "not reported", never as an
	// estimate to be made.
	OutputTokens int
	// Outcome is how the attempt ended, one of the closed vocabulary below: AttemptOK, an
	// "http_<code>" status (attemptHTTPOutcome), AttemptOverflow, AttemptInBand,
	// AttemptTransport, AttemptIdle, AttemptStreamFault or AttemptCancelled.
	Outcome string
}

// The closed outcome vocabulary of an Attempt. The one open-looking member, "http_<code>", is
// built by attemptHTTPOutcome from a non-2xx status and names nothing else.
const (
	// AttemptOK is an attempt that ended on the terminal Done.
	AttemptOK = "ok"
	// AttemptOverflow is a context-window rejection, as a status or in-band.
	AttemptOverflow = "overflow"
	// AttemptInBand is an error member the server wrapped in an HTTP 200.
	AttemptInBand = "in_band"
	// AttemptTransport is a POST that never got a response: a dial, TLS or connection fault.
	AttemptTransport = "transport"
	// AttemptIdle is the idle cut (WithStreamIdleTimeout), before the headers or mid-body.
	AttemptIdle = "idle"
	// AttemptStreamFault is any other fault of a 200 body: a read fault, a cap crossed, a
	// stream that decoded nothing.
	AttemptStreamFault = "stream_fault"
	// AttemptCancelled is an attempt the caller's context ended. A consumer excludes it from
	// latency percentiles and the failure rate: nothing about the server caused it.
	AttemptCancelled = "cancelled"
)

// attemptHTTPOutcome is the outcome of an attempt that ended on a non-2xx status.
func attemptHTTPOutcome(status int) string { return fmt.Sprintf("http_%d", status) }

// WithServerIdentity stamps the Client with the server entry it speaks for: its name and its
// endpoint, redacted to scheme, host and path on the way in. It is what arms attempt
// measurement: a stamped Client's Stream yields one DeltaAttempt per HTTP attempt; an unstamped
// one yields none, so its delta sequence stays exactly what it was — the WithWireObserver
// precedent of costing nothing until asked for.
func WithServerIdentity(name, endpoint string) Option {
	return func(c *Client) {
		c.identity = &serverIdentity{name: name, endpoint: redactEndpoint(endpoint)}
	}
}

// serverIdentity is what WithServerIdentity stamped: the entry's name and redacted endpoint.
type serverIdentity struct {
	name     string
	endpoint string
}

// ServerIdentity reports the identity WithServerIdentity stamped — the entry's name and its
// endpoint already redacted — and ok false on a Client built without it.
func (c *Client) ServerIdentity() (name, endpoint string, ok bool) {
	if c.identity == nil {
		return "", "", false
	}
	return c.identity.name, c.identity.endpoint, true
}

// RedactEndpoint is the endpoint as WithServerIdentity stamps it — scheme, host and path — so a
// reader of the attempt records (the per-server stats a picker summarises) can key on exactly the
// value the records carry.
func RedactEndpoint(endpoint string) string { return redactEndpoint(endpoint) }

// redactEndpoint reduces an endpoint to scheme, host and path: userinfo, query and fragment are
// dropped, so a key a URL carries in any of them never reaches a record. An endpoint that does
// not parse redacts to "" rather than to a string that might still hold one.
func redactEndpoint(endpoint string) string {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return ""
	}
	clean := url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}
	return clean.String()
}

// newRequestID returns a fresh random id for one Stream call's attempts.
func newRequestID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// attemptRecorder times the attempts of one Stream call and yields each one's DeltaAttempt. A
// nil recorder — an unstamped Client — is valid and does nothing, so Stream calls it without a
// branch. It lives on the goroutine driving the range: send reports abandoned attempts through
// it, and the parse's deltas pass through wrap, both on that goroutine.
type attemptRecorder struct {
	identity  serverIdentity
	requestID string
	model     string // the model id the request asked for — the fallback for Attempt.Model
	callerCtx context.Context
	headers   *idleTimer // the pre-header idle window, to tell an idle cut from a cancel
	stop      func()     // cancels send once the consumer broke, so no attempt starts after
	yield     func(Delta) bool
	broken    bool // the consumer's yield returned false: nothing may be yielded again

	// The attempt in flight.
	index     int
	start     time.Time
	body      *firstByteBody
	idle      *idleBody
	ttft      time.Duration
	last      time.Duration
	served    string
	outTokens int
}

// newAttemptRecorder arms a recorder for one Stream call, or returns nil when the Client carries
// no identity.
func (c *Client) newAttemptRecorder(ctx context.Context, req Request, yield func(Delta) bool) *attemptRecorder {
	if c.identity == nil {
		return nil
	}
	model := c.activeModel()
	if model == "" {
		model = req.Model
	}
	return &attemptRecorder{
		identity:  *c.identity,
		requestID: newRequestID(),
		model:     model,
		callerCtx: ctx,
		yield:     yield,
	}
}

// begin opens attempt index, starting its clock at the send.
func (r *attemptRecorder) begin(index int) {
	if r == nil {
		return
	}
	r.index, r.start = index, time.Now()
	r.body, r.idle = nil, nil
	r.ttft, r.last, r.served, r.outTokens = 0, 0, "", 0
}

// abandon ends an attempt send gave up on before handing a response back: a transport fault
// (err), a retryable status it drained for a retry (status), or a POST its context ended
// (cancelled — the idle cut or the caller, told apart here).
func (r *attemptRecorder) abandon(status int, err error, cancelled bool) {
	if r == nil || r.broken {
		return
	}
	switch {
	case cancelled:
		r.end(r.cancelOutcome())
	case err != nil:
		r.end(AttemptTransport)
	default:
		r.end(attemptHTTPOutcome(status))
	}
}

// cancelOutcome names why a context ended an attempt: the pre-header idle cut, or the caller.
func (r *attemptRecorder) cancelOutcome() string {
	if r.headers != nil && r.headers.fired.Load() && r.callerCtx.Err() == nil {
		return AttemptIdle
	}
	return AttemptCancelled
}

// end yields the in-flight attempt's DeltaAttempt with outcome and reports whether the consumer
// is still ranging. A consumer that broke stops send too, so no later attempt is made.
func (r *attemptRecorder) end(outcome string) bool {
	if r == nil {
		return true
	}
	if r.broken {
		return false
	}
	a := &Attempt{
		Server:       r.identity.name,
		Endpoint:     r.identity.endpoint,
		Model:        r.served,
		RequestID:    r.requestID,
		Index:        r.index,
		TTFT:         r.ttft,
		Last:         r.last,
		Duration:     time.Since(r.start),
		OutputTokens: r.outTokens,
		Outcome:      outcome,
	}
	if a.Model == "" {
		a.Model = r.model
	}
	if r.body != nil {
		a.TTFB = r.body.ttfb
	}
	if !r.yield(Delta{Kind: DeltaAttempt, Attempt: a}) {
		r.broken = true
		if r.stop != nil {
			r.stop()
		}
		return false
	}
	return true
}

// watch wraps a response body in the first-byte clock, beneath the idle cut and the capture
// tee; a nil recorder hands the body back untouched.
func (r *attemptRecorder) watch(body io.ReadCloser) io.ReadCloser {
	if r == nil {
		return body
	}
	r.body = &firstByteBody{ReadCloser: body, start: r.start}
	return r.body
}

// wrap returns the yield the codec parses into: every model delta advances the TTFT and Last
// clocks, and a terminal delta is preceded by the attempt it ends. A nil recorder returns yield
// itself.
func (r *attemptRecorder) wrap(yield func(Delta) bool) func(Delta) bool {
	if r == nil {
		return yield
	}
	return func(d Delta) bool {
		switch d.Kind {
		case DeltaContent, DeltaThinking, DeltaToolCall:
			r.mark()
		case DeltaDone:
			r.served = d.Model
			if d.Usage != nil {
				r.outTokens = d.Usage.CompletionTokens
			}
			if !r.end(AttemptOK) {
				return false
			}
		case DeltaContextOverflow:
			if !r.end(AttemptOverflow) {
				return false
			}
		case DeltaError:
			if !r.end(r.faultOutcome(d)) {
				return false
			}
		}
		return yield(d)
	}
}

// mark advances the model-delta clocks: TTFT once, Last every time.
func (r *attemptRecorder) mark() {
	now := time.Since(r.start)
	if r.ttft == 0 {
		r.ttft = now
	}
	r.last = now
}

// faultOutcome classifies a terminal DeltaError of a 200 body: the caller's cancel first (a
// cancelled read faults however the body was doing), then the idle cut, then an in-band error
// member, and anything else as a stream fault.
func (r *attemptRecorder) faultOutcome(d Delta) string {
	switch {
	case r.callerCtx.Err() != nil:
		return AttemptCancelled
	case r.idle != nil && r.idle.idle.fired.Load():
		return AttemptIdle
	case strings.HasPrefix(d.Err, inBandErrPrefix):
		return AttemptInBand
	default:
		return AttemptStreamFault
	}
}

// statusOutcome is the outcome of a final non-2xx response: overflow when statusDelta rendered
// it as one, its status otherwise.
func statusOutcome(d Delta, status int) string {
	if d.Kind == DeltaContextOverflow {
		return AttemptOverflow
	}
	return attemptHTTPOutcome(status)
}

// firstByteBody is a response body that records when its first byte arrived — any byte, an SSE
// keep-alive comment included. It is its own reader, independent of idleBody, so TTFB is timed
// whether or not the idle cut is armed.
type firstByteBody struct {
	io.ReadCloser
	start time.Time
	ttfb  time.Duration
}

// Read reads from the body and stamps TTFB on the first read that returned bytes.
func (b *firstByteBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 && b.ttfb == 0 {
		b.ttfb = time.Since(b.start)
	}
	return n, err
}
