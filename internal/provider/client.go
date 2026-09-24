package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultChatPath = "/v1/chat/completions"
	modelsPath      = "/v1/models"
	propsPath       = "/props"
	maxErrorLength  = 500

	// maxToolCallBytes caps the argument bytes one streamed reply accumulates across ALL of
	// its tool calls, not one call each: a server opening call after call cannot multiply the
	// bound by their number.
	maxToolCallBytes = 1 << 20 // 1 MiB

	// maxOpenToolCalls caps how many tool calls one streamed reply may open. The byte cap
	// bounds the arguments, not the calls: a server opening call after call with empty
	// arguments would otherwise grow the open set without limit. Sixty-four is far past any
	// honest reply's parallel-call count; the 65th ends the stream exactly as the byte cap does.
	maxOpenToolCalls = 64

	// maxErrorBodyBytes caps how much of a non-2xx body is read before it is classified. The
	// request timeouts default to 0, so a hostile or broken upstream answering a multi-GB
	// error body would otherwise be buffered whole and exhaust the agent's memory. 64 KiB is
	// far past any genuine error payload; what is read still flows through the same
	// context-overflow sniff and sanitize path, so a truncated body needs no error kind of
	// its own.
	maxErrorBodyBytes = 64 << 10

	// maxReplyTextBytes caps the content plus reasoning bytes one streamed completion may
	// yield. The engine's own reply ceiling is maxOutputTokenCap — 32,768 tokens (ADR 0046) —
	// and a UTF-8 rune is at most 4 bytes, so a server that honours max_tokens fits in ~128
	// KiB. 8 MiB is sixty-fold that: past any honest reply, yet small enough that a server
	// ignoring the cap cannot exhaust the agent.
	maxReplyTextBytes = 8 << 20

	// maxResponseBodyBytes caps a NON-streamed 200 body, which must hold the same reply text
	// as a stream plus the maxToolCallBytes its whole tool-call set may sum to, plus metadata.
	// A body cut at the limit fails the JSON decode and surfaces as the existing decode
	// error — no error kind of its own.
	maxResponseBodyBytes = 16 << 20

	// topLogProbsCount is how many alternatives a Request that asks for LogProbs requests per
	// token position. Five is enough for the candidate set to identify a model's distribution
	// while staying inside every server's top_logprobs cap.
	topLogProbsCount = 5

	defaultMaxRetries     = 2
	defaultRetryBaseDelay = 200 * time.Millisecond

	// defaultStreamIdleTimeout is how long Stream lets an upstream sit silent — before its
	// headers or between two body reads — before cutting it as a retryable fault. Ten minutes
	// clears every remote provider's keep-alive cadence and most local prefills; a local server
	// whose prefill runs longer disables it with WithStreamIdleTimeout(0) or sets a longer one.
	defaultStreamIdleTimeout = 10 * time.Minute

	// retry429BaseDelay is the backoff base for a rate-limited (429) attempt that carried no
	// Retry-After header. A rate limit is a "come back later", not a transport blip, so retrying
	// it at the 200ms transport base only burns the budget before the window reopens.
	retry429BaseDelay = 1 * time.Second

	// maxRetryAfter caps how long a server-supplied Retry-After is honoured. Beyond it the
	// upstream is telling us we are banned for longer than any turn should sit blocked, so the
	// reply is surfaced as an error immediately instead of being waited out.
	maxRetryAfter = 30 * time.Second
)

// ErrContextOverflow is returned (wrapped) when the Upstream rejects a request because
// the prompt exceeds the model's context window — a 400 whose body matches a known
// overflow marker. It is distinct from a generic HTTP error so the context reducers can
// branch on it (TDD §8 #8); P1.1 only surfaces it.
var ErrContextOverflow = errors.New("apogee: context window exceeded")

// StatusError is a non-2xx Upstream reply that is NOT a context overflow (that one stays
// ErrContextOverflow so its existing errors.Is callers keep working). It carries the status
// code and the sanitised body so a caller can branch on the HTTP class with errors.As
// instead of matching the message text — the naming call uses it to tell "this server
// rejected my request outright" (a 4xx, worth one retry without the offending field) from
// a transport or 5xx fault.
type StatusError struct {
	Code     int    // the HTTP status the Upstream answered with
	Body     string // the response body, API key redacted and length-capped
	Location string // the response's Location header, "" when absent (in-band errors carry none)
}

// Error renders the reply through upstreamStatusText: byte-identical to the text this branch
// produced before the type existed whenever the body is non-empty, so logs and any
// message-matching caller are unaffected; an empty body names the status instead.
func (e *StatusError) Error() string {
	return upstreamStatusText(e.Code, e.Body, e.Location)
}

// redirectHint is appended to any 3xx upstream reply. The client never follows a redirect
// (see NewClient), so the bare status would leave the user guessing why a reachable server
// answers nothing — the hint names the fix and, when the server said so, the address.
const redirectHint = " — redirects are not followed; point endpoint: at the URL the server redirects to"

// upstreamStatusText is the ONE renderer for a non-2xx upstream reply, shared by the
// blocking (StatusError) and streaming (statusDelta) surfaces so both show the same text.
// A non-empty body keeps the historical "apogee: upstream HTTP <code>: <body>" form; an empty
// one names the status ("apogee: upstream HTTP 308 Permanent Redirect") rather than ending
// in a bare colon, and an unknown code renders with no trailing space. A 3xx additionally
// carries redirectHint plus the Location header when the server sent one.
func upstreamStatusText(code int, body, location string) string {
	var text string
	switch {
	case body != "":
		text = fmt.Sprintf("apogee: upstream HTTP %d: %s", code, body)
	case http.StatusText(code) != "":
		text = fmt.Sprintf("apogee: upstream HTTP %d %s", code, http.StatusText(code))
	default:
		text = fmt.Sprintf("apogee: upstream HTTP %d", code)
	}
	if code < 300 || code > 399 {
		return text
	}
	text += redirectHint
	if location != "" {
		text += " (Location: " + location + ")"
	}
	return text
}

// Client is the HTTP Responder: it turns a provider.Request into the wire body of the
// protocol WithWire selected, calls the Upstream over net/http, and assembles the reply. The
// protocol-specific half — the body, the headers that carry the key, the reply decode and
// the SSE parser — is a wireCodec the Client selects once at construction (openaiCodec is the
// default and the historical behaviour); everything else — retries, timeouts, redirect
// refusal, fault classification, sanitising, wire capture — is shared by every wire. It adds
// bounded retries (transient transport faults, 429, and 5xx) and an optional per-attempt
// timeout on top of the bare TS oracle, which the embeddable core needs and the VS Code
// extension got from the editor.
//
// Retry policy: a retryable reply carrying a Retry-After header is waited out for exactly
// that long, up to maxRetryAfter — a longer one is surfaced as an error at once rather than
// blocking the turn on a long ban. Without the header the wait is exponential, base·2ⁿ, off
// the Client's configured base for transport faults and 5xx and off the slower
// retry429BaseDelay for a 429. Every wait is cancellable by the caller's context.
//
// One Client is safe for concurrent Respond/Stream calls
// (it holds no per-request state) and for a concurrent SetModel; cancellation is via the
// caller's context.
type Client struct {
	baseURL  string
	chatPath string

	// modelMu guards model — the ONE field that changes after construction. The host rebinds
	// the wire model when the Upstream starts serving a different one (SetModel, driven by
	// Agent.Rebind — ADR 0024) while another goroutine may be mid-Stream or mid-Discover, so
	// both readers go through activeModel under this lock and the "safe for concurrent use"
	// contract above stays literally true. Every other field is written once by NewClient.
	modelMu sync.RWMutex
	model   string

	apiKey            string
	httpClient        *http.Client
	maxRetries        int
	retryBaseDelay    time.Duration
	requestTimeout    time.Duration    // per-attempt bound for Respond; 0 ⇒ caller's ctx governs
	streamIdleTimeout time.Duration    // silence bound for Stream (see WithStreamIdleTimeout); 0 ⇒ off
	discoveryDeadline time.Duration    // bound for one Discover call; 0 ⇒ the DiscoveryTimeout default
	wireObserver      func(WireRecord) // nil ⇒ no wire capture at all (see WithWireObserver)

	// wire names the protocol WithWire selected and codec speaks it. Both are settled once by
	// NewClient — after every Option has run, so WithWire and WithChatPath compose in any order —
	// and never change: a server's protocol is a property of the endpoint, and moving to another
	// endpoint means another Client.
	wire  Wire
	codec wireCodec

	// effortDialect is the thinking-effort dialect this server's entry FORCED, and the zero
	// EffortDialectNone when it forced none — the `auto` that leaves Discover's passive detection
	// to answer (see WithEffortDialect). Unlike model it is written once by NewClient and never
	// again, so it needs no lock: a server's wire shape is a property of the endpoint, and moving
	// to another endpoint means another Client.
	effortDialect EffortDialect

	// requestExtra is the server entry's request-extra merge patch (ADR 0085), decoded once by
	// WithRequestExtra and only read afterwards; nil ⇒ no merge, and the body is the codec's
	// bytes exactly. requestExtraErr is the decode fault of a malformed patch, reported by every
	// encode rather than at construction, which never fails.
	requestExtra    *mergePatch
	requestExtraErr error

	// identity is the server entry WithServerIdentity stamped; nil ⇒ Stream measures no
	// attempts and yields no DeltaAttempt.
	identity *serverIdentity
}

// wireCodec is the protocol-specific half of a Client: one implementation per Wire, selected
// at construction and consulted at exactly five points of a round-trip. The Client owns
// everything around those points — retries, timeouts, the read caps, fault classification, the
// wire observer — so a codec is only ever asked to translate: a seam Request onto bytes, bytes
// off a reply onto the seam types. A codec never touches net/http and never sees the observer.
type wireCodec interface {
	// path is the request path joined onto the Client's base URL.
	path() string
	// headers are the request headers that carry the API key — none when the key is empty.
	headers(apiKey string) map[string]string
	// encode renders a Request (its Model already resolved by the Client) onto the request
	// body and reports whether that body expressed a thinking effort, the gate on
	// thinkingEffortHint.
	encode(req Request) (body []byte, carriesEffort bool, err error)
	// decodeWhole decodes one non-streamed 200 body. A reply that framed a failure in-band
	// comes back as the *wireError with a zero RawResponse; a body that cannot be decoded is
	// the bare error, which the Client wraps.
	decodeWhole(body io.Reader) (RawResponse, *wireError, error)
	// parseSSE reads one streamed 200 body and yields Deltas until it ends, however it ends.
	// carried reports that the request expressed a thinking effort (see encode). toolFragment,
	// when non-nil, runs as each tool-call fragment arrives — calls are held until the stream
	// ends, so it is the only signal of when one was generated; it never changes what is yielded.
	parseSSE(body io.Reader, carried bool, toolFragment func(), yield func(Delta) bool)
}

// Option configures a Client (functional-options pattern — most fields have a sane
// default and only advanced callers override them).
type Option func(*Client)

// WithAPIKey sets the API key sent on every request, in whichever header the selected wire
// carries it (`Authorization: Bearer` on the openai wire).
func WithAPIKey(key string) Option { return func(c *Client) { c.apiKey = key } }

// WithChatPath overrides the openai wire's chat-completions path (default
// "/v1/chat/completions"). It composes with WithWire in either order.
func WithChatPath(path string) Option { return func(c *Client) { c.chatPath = path } }

// WithWire selects the protocol this Client speaks (default WireOpenAI). A Wire the Client
// has no codec for is served as WireOpenAI — the Client stays total, and the config loader is
// where an unknown spelling is refused. Read back through Client.Wire.
func WithWire(w Wire) Option { return func(c *Client) { c.wire = w } }

// WithHTTPClient injects the underlying *http.Client (for custom transports or test
// servers). Its Timeout must stay 0 — a client-level timeout would also abort streams;
// bound a request with WithRequestTimeout or the caller's context instead.
//
// The injected client is the embedder's, redirect policy included: the refusal NewClient
// builds in is on the DEFAULT client, and an embedder that hands in a bare &http.Client{}
// gets net/http's follow-up-to-ten behaviour. Carry CheckRedirect over unless following a
// redirect is what the embedder means to do (see NewClient for why the default refuses).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

// WithMaxRetries sets how many times a retryable attempt is re-tried (default 2 ⇒ up to
// 3 attempts). Zero disables retries.
func WithMaxRetries(n int) Option { return func(c *Client) { c.maxRetries = n } }

// WithRetryBaseDelay sets the base backoff for transport faults and 5xx replies; attempt n
// waits base·2ⁿ (default 200ms). It does not govern a 429, which has its own slower base, nor
// a reply that supplied a Retry-After header — that duration is honoured verbatim.
func WithRetryBaseDelay(d time.Duration) Option { return func(c *Client) { c.retryBaseDelay = d } }

// WithRequestTimeout bounds a single non-streaming Respond attempt (default 0 ⇒ unbounded,
// governed by the caller's context). Streaming is never bounded this way — a long
// generation is not a fault.
func WithRequestTimeout(d time.Duration) Option { return func(c *Client) { c.requestTimeout = d } }

// WithStreamIdleTimeout bounds how long a streaming call may sit SILENT — waiting for the
// response headers, or between two body reads — before Stream cuts it and surfaces a
// retryable DeltaError (default defaultStreamIdleTimeout, 10m; 0 disables, leaving the caller's
// ctx as the stream's only deadline). Bytes of any kind reset the clock — reasoning deltas and
// SSE keep-alive comments count as activity — so a long generation is never cut, only a stalled
// one. Respond is not governed by it: WithRequestTimeout bounds that path. Read back through
// Client.StreamIdleTimeout.
func WithStreamIdleTimeout(d time.Duration) Option {
	return func(c *Client) { c.streamIdleTimeout = d }
}

// WithDiscoveryTimeout bounds one Discover call — both probes together — so a hung server cannot
// stall construction (default DiscoveryTimeout; zero or negative keeps the default). A test that stacks many
// loopback servers under a race-instrumented shard passes a generous bound so the row it is
// asserting cannot be failed by the deadline rather than by the reply.
func WithDiscoveryTimeout(d time.Duration) Option { return func(c *Client) { c.discoveryDeadline = d } }

// WithEffortDialect forces the thinking-effort dialect Discover reports for this server, for the
// providers passive detection cannot see (ADR 0060 decision 3): a forced dialect overrides what the
// two discovery payloads said, and — for the three wire dialects — also declares the dial supported,
// so a server that advertises no tell still offers one. EffortDialectOff forces the opposite verdict:
// unsupported, and nothing on the wire. The zero EffortDialectNone forces nothing, which is the
// default and leaves detection to answer.
//
// It governs DISCOVERY, not the request: what a completion carries is the dialect on the Request,
// and this one reaches it by riding the reported EffortSupport back out to whoever binds the
// session. That keeps one channel from detection to the wire whether the answer was detected or
// configured (ADR 0060).
func WithEffortDialect(d EffortDialect) Option { return func(c *Client) { c.effortDialect = d } }

// WithRequestExtra overlays a server entry's request-extra — a JSON object in canonical form —
// onto every body the Client sends, on either wire, as an RFC 7396 JSON Merge Patch: objects
// merge member by member, null deletes, anything else replaces (ADR 0085). It is opaque: apogee
// reads none of it, so a provider's routing hints pass through without apogee-side code. The
// patch is decoded here, once; "" and "{}" install nothing, and the body stays byte-identical to
// the codec's. A patch that is not a JSON object fails every request that would carry it,
// because construction never fails. The wire observer sees the merged body — it is what was
// posted.
func WithRequestExtra(patch string) Option {
	return func(c *Client) {
		c.requestExtra, c.requestExtraErr = parseMergePatch(patch)
		if c.requestExtraErr != nil {
			c.requestExtra = nil
			c.requestExtraErr = fmt.Errorf("request-extra: %w", c.requestExtraErr)
		}
	}
}

// WithWireObserver installs a callback that is handed one WireRecord per direction per
// Respond/Stream call — the request bytes as posted and, at stream end, the raw SSE
// payloads as received (see WireRecord for the exact shape, including which replies are
// recorded). It is how a Driver shows raw protocol without the Client keeping any of it:
// the Client calls the observer and forgets. Nothing accumulates while no observer is
// installed, which is the default and costs nothing.
//
// The observer runs synchronously on the goroutine driving the call, so a slow one slows
// that call; it must be safe for concurrent use, because one Client serves concurrent
// Respond/Stream callers. Credentials never reach it — records carry bodies only, never
// headers, and error bodies arrive already redacted.
func WithWireObserver(observe func(WireRecord)) Option {
	return func(c *Client) { c.wireObserver = observe }
}

// NewClient builds a Client for the OpenAI-compatible server at baseURL, defaulting the
// model when a Request leaves it empty. Whitespace around baseURL and a trailing slash on
// it are trimmed so path joins are clean — the config loader already stores an endpoint
// canonical, and this is the same guard for an embedder that passes one straight in.
// Construction never fails — a malformed endpoint surfaces as a request
// error, matching the TS oracle (a bad fetch URL throws at call time, not at construction).
//
// The client it builds never follows a redirect, the policy the network tools and the MCP
// transports already carry. The per-Turn POST carries the WHOLE conversation — every message,
// every tool result — so a 307 or 308 from the endpoint would re-aim all of it at an address
// nobody configured. A 3xx therefore comes back as the response itself and reaches the caller
// as a *StatusError naming the code (send does not retry it: isRetryableStatus is 429 and 5xx
// only, and discovery reports it as an upstream-HTTP error the same way). A server that
// redirects must be configured at the URL it redirects to.
//
// Refusing redirects is the whole of the fix: this client deliberately does NOT dial under
// URLGuard.PinnedDialControl the way an MCP HTTP transport does, because NewClient and
// Agent.Rebind promise that construction never fails, and an address pinned once at
// construction would break a running session whose LAN endpoint moves IP — the local-server
// case apogee exists for. With the re-aim vector closed at the redirect, the pin buys
// fragility rather than reach.
func NewClient(baseURL, model string, opts ...Option) *Client {
	c := &Client{
		baseURL:  strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		chatPath: defaultChatPath,
		model:    model,
		httpClient: &http.Client{
			// No client-level Timeout: it would also kill streams. A 3xx is handed back as
			// the response instead of being followed (see above).
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		maxRetries:        defaultMaxRetries,
		retryBaseDelay:    defaultRetryBaseDelay,
		streamIdleTimeout: defaultStreamIdleTimeout,
	}
	for _, opt := range opts {
		opt(c)
	}
	c.wire, c.codec = c.selectCodec()
	return c
}

// selectCodec resolves the Wire the options asked for — every spelling the Client has no
// codec for folds to WireOpenAI — and builds that wire's codec. It runs once, after the
// options, so the codec sees the final chat path.
func (c *Client) selectCodec() (Wire, wireCodec) {
	if c.wire == WireAnthropic {
		return WireAnthropic, &anthropicCodec{client: c}
	}
	return WireOpenAI, &openaiCodec{client: c, chatPath: c.chatPath}
}

// Wire reports the protocol this Client speaks — the WithWire selection after the unknown
// fold, so it is always a Wire the Client has a codec for.
func (c *Client) Wire() Wire { return c.wire }

// StreamIdleTimeout reports the silence bound Stream enforces — the WithStreamIdleTimeout
// selection, or the 10m default; 0 means no idle cut.
func (c *Client) StreamIdleTimeout() time.Duration { return c.streamIdleTimeout }

var (
	_ Responder = (*Client)(nil)
	_ io.Closer = (*Client)(nil)
)

// SetModel rebinds the model id this Client sends on the wire — and hints Discover with —
// for every subsequent request. It exists because the Upstream's loaded model can change
// under a running session (the heartbeat observes the switch; Agent.Rebind applies it, ADR
// 0024), and the configured model wins over the Request's in encode, so rebinding the
// engine's Config alone would leave the old id on the wire.
//
// It is safe to call from another goroutine while requests are in flight: the change lands on
// the next body the Client builds, in the shape of Agent.SetMode. It never touches the
// endpoint — switching servers means a new Client, not a mutated one.
func (c *Client) SetModel(model string) {
	c.modelMu.Lock()
	c.model = model
	c.modelMu.Unlock()
}

// activeModel reads the configured model under the lock. It is the single read seam for the
// two places the field is consumed — the request body and model discovery — so a concurrent
// SetModel is observed race-free by both.
func (c *Client) activeModel() string {
	c.modelMu.RLock()
	defer c.modelMu.RUnlock()
	return c.model
}

// Close releases the connections this Client is holding to the Upstream: the keep-alive sockets
// its HTTP client left idle after earlier requests. It is the teardown half of NewClient, and the
// seam Agent.Close and Agent.SwitchUpstream reach for when they retire a client they own — a
// switched-away or finished session must not pin sockets to a server nobody will speak to again.
//
// It never fails (the nil return is the io.Closer shape, not a promise the caller must check),
// it is safe to call twice, and it does not invalidate the Client: requests in flight are
// untouched — CloseIdleConnections only reaps IDLE sockets — and a later request simply dials
// again. Concurrency follows the type's contract: closing while another goroutine is mid-Respond
// or mid-Stream is allowed and costs that call nothing.
//
// One caveat about reach: a Client built without WithHTTPClient uses net/http's shared
// DefaultTransport, so Close reaps the idle sockets in THAT pool rather than in a pool of its own.
// This is safe by construction (idle only, and every holder re-dials on demand) but it is not
// confined to this Client. Pass WithHTTPClient carrying a Transport of its own where a private
// connection pool matters.
func (c *Client) Close() error {
	c.httpClient.CloseIdleConnections()
	return nil
}

// Respond performs one non-streaming round-trip and assembles the reply. A non-2xx
// status becomes an error (ErrContextOverflow for a 400 overflow, otherwise an
// HTTP-status error with the body sanitised); transient faults are retried per the
// Client's policy before the final error escapes. An HTTP 200 whose body carries an
// in-band error member becomes the same kind of error, never an empty reply. The 200
// body is read through a maxResponseBodyBytes limit, so an unbounded reply fails the
// decode rather than exhausting memory.
func (c *Client) Respond(ctx context.Context, req Request) (RawResponse, error) {
	req.Stream = false
	body, carriedEffort, err := c.encode(req)
	if err != nil {
		return RawResponse{}, fmt.Errorf("apogee: marshal request: %w", err)
	}

	resp, cancel, err := c.send(ctx, body, c.requestTimeout, nil)
	if err != nil {
		return RawResponse{}, err
	}
	defer cancel()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return RawResponse{}, c.statusError(resp, carriedEffort)
	}

	reply, werr, err := c.codec.decodeWhole(io.LimitReader(resp.Body, maxResponseBodyBytes))
	if err != nil {
		return RawResponse{}, fmt.Errorf("apogee: decode response: %w", err)
	}
	if werr != nil {
		// A failure the server framed inside an HTTP 200 — the empty-reply masquerade the codec
		// refuses to map onto a zero RawResponse — is rendered exactly like a status would be.
		return RawResponse{}, c.inBandError(*werr, carriedEffort)
	}
	return reply, nil
}

// encode renders req onto the selected wire. The configured model wins over the Request's
// on every wire — SetModel rebinds it under a running session — so it is resolved here, once,
// before the codec sees the request. The bool reports whether the body expressed a thinking
// effort; it gates thinkingEffortHint on the reply's fault, and it is the codec's verdict even
// when request-extra touched the effort members. The request-extra patch, when one is
// installed, is merged over the codec's bytes here — before send and so before the observer.
func (c *Client) encode(req Request) (body []byte, carriedEffort bool, err error) {
	if model := c.activeModel(); model != "" {
		req.Model = model
	}
	if c.requestExtraErr != nil {
		return nil, false, c.requestExtraErr
	}
	body, carriedEffort, err = c.codec.encode(req)
	if err != nil || c.requestExtra == nil {
		return body, carriedEffort, err
	}
	merged, err := c.requestExtra.apply(body)
	if err != nil {
		return nil, false, fmt.Errorf("request-extra: %w", err)
	}
	return merged, carriedEffort, nil
}

// send issues the POST with bounded retries and returns the live response together with
// a cancel func the caller MUST invoke once the body is read (it releases the
// per-attempt timeout context). The body is the caller's to Close. Retries cover
// transport faults, 429, and 5xx; a caller-cancelled context aborts without retrying.
// A retryable reply's Retry-After header sets the wait when it is at most maxRetryAfter,
// and ends the retries outright when it is longer — the response is handed back so the
// caller surfaces it as the final error instead of the turn hanging on a long ban.
// attemptTimeout > 0 bounds each attempt so a stuck attempt becomes retryable without
// touching the caller's context — but it must outlive the body read, so it rides the
// returned cancel rather than a local defer. rec, when non-nil, is told where each attempt
// begins and handed every attempt send abandons; the attempt it returns is the caller's to end.
func (c *Client) send(ctx context.Context, body []byte, attemptTimeout time.Duration, rec *attemptRecorder) (*http.Response, context.CancelFunc, error) {
	url := c.baseURL + c.codec.path()

	// One request record per call, not per attempt: every retry posts these same bytes, and
	// this is the last point at which they are still exactly what goes on the wire.
	c.observeWire(WireRequest, body)

	var lastErr error
	var wait time.Duration // how long to hold off before the next attempt, set by the failed one
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, wait); err != nil {
				return nil, nil, err
			}
		}

		rec.begin(attempt)
		attemptCtx, cancel := c.attemptContext(ctx, attemptTimeout)
		resp, err := c.do(attemptCtx, url, body)
		if err != nil {
			cancel()
			if ctx.Err() != nil {
				rec.abandon(0, err, true)
				return nil, nil, ctx.Err() // caller cancelled — not a transient fault
			}
			rec.abandon(0, err, false)
			lastErr = err
			wait = c.retryDelay(0, attempt+1)
			continue // transport/timeout fault — retry if budget remains
		}
		if isRetryableStatus(resp.StatusCode) && attempt < c.maxRetries {
			after, ok := parseRetryAfter(resp.Header.Get("Retry-After"))
			if ok && after > maxRetryAfter {
				// The upstream named a wait longer than we are willing to sit blocked for.
				// Give up now and let this response become the surfaced error.
				return resp, cancel, nil
			}
			drain(resp) // free the connection for reuse before retrying
			cancel()
			rec.abandon(resp.StatusCode, nil, false)
			if ok {
				wait = after
			} else {
				wait = c.retryDelay(resp.StatusCode, attempt+1)
			}
			lastErr = fmt.Errorf("apogee: upstream HTTP %d", resp.StatusCode)
			continue
		}
		return resp, cancel, nil
	}
	return nil, nil, fmt.Errorf("apogee: upstream unreachable after %d attempts: %w", c.maxRetries+1, lastErr)
}

// attemptContext derives the per-attempt context: a timeout child when attemptTimeout > 0,
// otherwise a plain cancellable child so the caller always gets a non-nil cancel.
func (c *Client) attemptContext(ctx context.Context, attemptTimeout time.Duration) (context.Context, context.CancelFunc) {
	if attemptTimeout > 0 {
		return context.WithTimeout(ctx, attemptTimeout)
	}
	return context.WithCancel(ctx)
}

// do issues exactly one POST under ctx.
func (c *Client) do(ctx context.Context, url string, body []byte) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("apogee: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	c.setAuth(httpReq.Header)
	return c.httpClient.Do(httpReq)
}

// retryDelay is the exponential backoff to observe before attempt n (1-based) after a failure
// that carried the given status — 0 for a transport fault: base·2ⁿ⁻¹. A 429 backs off from the
// slower retry429BaseDelay; transport faults and 5xx use the Client's configured base.
func (c *Client) retryDelay(status, attempt int) time.Duration {
	base := c.retryBaseDelay
	if status == http.StatusTooManyRequests {
		base = retry429BaseDelay
	}
	return base << (attempt - 1)
}

// sleepCtx waits for d, returning early with ctx.Err() if the context is cancelled first. A
// non-positive d still yields to the scheduler rather than special-casing zero — a Retry-After
// of 0 means "retry now", which the caller's very next request already honours.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// parseRetryAfter reads a Retry-After header in either RFC 9110 form — delta-seconds ("120")
// or an HTTP-date ("Fri, 31 Dec 1999 23:59:59 GMT") — and reports how long to hold off. A
// value already in the past yields zero: the ban has lapsed, so retry at once. An absent or
// malformed value reports false, leaving the caller on its own backoff.
func parseRetryAfter(h string) (time.Duration, bool) {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(h); err == nil {
		if secs <= 0 {
			return 0, true
		}
		return time.Duration(secs) * time.Second, true
	}
	when, err := http.ParseTime(h)
	if err != nil {
		return 0, false
	}
	if d := time.Until(when); d > 0 {
		return d, true
	}
	return 0, true
}

// thinkingEffortHint is appended to a non-2xx error raised by a request that expressed a
// thinking effort — in any dialect. A server that rejects the value says so uselessly: a chat
// template raises inside Jinja and answers HTTP 500 with a traceback, an API answers a 4xx, and
// neither ever names the field it choked on, so the bare status leaves the user guessing
// mid-turn. The hint therefore names the INTENT, not any one dialect's field, plus both doors
// the value can have come through — the profile leaf and the session override (ADR 0050).
const thinkingEffortHint = "(this request asked for a thinking effort — one this model does not accept? check model-profiles thinking.effort or the /effort override)"

// statusError reads a non-2xx body — at most maxErrorBodyBytes of it, so an oversized one
// cannot exhaust memory — classifies it (classify, over the raw bytes) and renders the fault
// through faultError. The body is sanitised (API key redacted, length capped) before it
// reaches the caller. carriedEffort reports that the failed request expressed a thinking
// effort in some dialect.
func (c *Client) statusError(resp *http.Response, carriedEffort bool) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	text := c.sanitize(string(raw))
	c.observeWire(WireResponse, []byte(text))
	f := classify(resp.StatusCode, "", string(raw), carriedEffort)
	return faultError(f, text, resp.Header.Get("Location"))
}

// inBandError renders an error member the server wrapped in an HTTP 200 exactly as
// statusError renders a status, so both framings of the same failure reach callers as the
// same error types: one classify verdict over the raw message, one faultError. A non-numeric
// code yields Code 0 — the sanitised body carries the truth in that case. carriedEffort
// rides along for the same reason it does on statusError: an aggregator that wraps an effort
// failure in a 200 leaves the user just as blind as a raw 500 would.
func (c *Client) inBandError(werr wireError, carriedEffort bool) error {
	f := classify(werr.intCode(), werr.ErrorType, werr.Message, carriedEffort)
	return faultError(f, c.sanitize(werr.render()), "")
}

// faultError is the blocking surface's renderer over a fault: an overflow wraps
// ErrContextOverflow, anything else is a *StatusError carrying the code, the sanitised text
// and the Location header ("" for an in-band error, which carries none). A hinted fault
// appends thinkingEffortHint to the WRAPPING error, never to StatusError.Body, and the
// wrapping keeps errors.As(*StatusError) working for callers that branch on the code.
// The fault's retryable verdict is not read here: send already retried the statuses it
// covers before one became an error.
func faultError(f fault, text, location string) error {
	if f.overflow {
		return fmt.Errorf("%w: %s", ErrContextOverflow, text)
	}
	err := &StatusError{Code: f.code, Body: text, Location: location}
	if !f.hinted {
		return err
	}
	return fmt.Errorf("%w %s", err, thinkingEffortHint)
}

// setAuth adds the headers that carry the API key, in the selected wire's spelling — the one
// applier every request goes through, completions and discovery alike. Nothing is added when
// no key is configured.
func (c *Client) setAuth(h http.Header) {
	for name, value := range c.codec.headers(c.apiKey) {
		h.Set(name, value)
	}
}

// sanitize redacts the API key from server-echoed text and caps its length so an error
// never leaks a secret or floods a log.
func (c *Client) sanitize(text string) string {
	if c.apiKey != "" {
		text = strings.ReplaceAll(text, c.apiKey, "[REDACTED]")
	}
	if len(text) > maxErrorLength {
		text = text[:maxErrorLength] + "...[truncated]"
	}
	return text
}

// observeWire hands one record to the installed wire observer. It is the single capture
// seam — every capture site goes through it, so the "no observer ⇒ nothing happens"
// guarantee lives in exactly one nil check.
func (c *Client) observeWire(direction WireDirection, payload []byte) {
	if c.wireObserver == nil {
		return
	}
	c.wireObserver(WireRecord{Direction: direction, Payload: payload})
}

// isRetryableStatus reports whether an HTTP status warrants a retry: 429 (rate-limited)
// or any 5xx (server-side transient).
func isRetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

// isContextOverflow reports whether a 400 body looks like a context-window rejection,
// matching the markers the TS oracle recognises across server implementations plus the
// Messages API's "prompt is too long" (its invalid_request_error for an oversized prompt).
func isContextOverflow(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{
		"exceed_context_size",
		"exceeds the available context",
		"context length exceeded",
		"maximum context length",
		"too many tokens",
		"prompt is too long",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// drain reads and closes a response body so the underlying connection can be reused.
func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}
