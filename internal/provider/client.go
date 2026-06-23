package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultChatPath  = "/v1/chat/completions"
	modelsPath       = "/v1/models"
	maxErrorLength   = 500
	maxToolCallBytes = 1 << 20 // 1 MiB — cap on accumulated streamed tool-call arguments

	defaultMaxRetries     = 2
	defaultRetryBaseDelay = 200 * time.Millisecond
)

// ErrContextOverflow is returned (wrapped) when the Upstream rejects a request because
// the prompt exceeds the model's context window — a 400 whose body matches a known
// overflow marker. It is distinct from a generic HTTP error so the context reducers can
// branch on it (TDD §8 #8); P1.1 only surfaces it.
var ErrContextOverflow = errors.New("apogee: context window exceeded")

// Client is the OpenAI-compatible chat-completions Responder: it turns a provider.Request
// into the wire JSON, calls the Upstream over net/http, and assembles the reply. It adds
// bounded retries (transient transport faults, 429, and 5xx) and an optional per-attempt
// timeout on top of the bare TS oracle, which the embeddable core needs and the VS Code
// extension got from the editor. One Client is safe for concurrent Respond/Stream calls
// (it holds no per-request state); cancellation is via the caller's context.
type Client struct {
	baseURL        string
	chatPath       string
	model          string
	apiKey         string
	httpClient     *http.Client
	maxRetries     int
	retryBaseDelay time.Duration
	requestTimeout time.Duration // per-attempt bound for Respond; 0 ⇒ caller's ctx governs
}

// Option configures a Client (functional-options pattern — most fields have a sane
// default and only advanced callers override them).
type Option func(*Client)

// WithAPIKey sets the bearer token sent as Authorization on every request.
func WithAPIKey(key string) Option { return func(c *Client) { c.apiKey = key } }

// WithChatPath overrides the chat-completions path (default "/v1/chat/completions").
func WithChatPath(path string) Option { return func(c *Client) { c.chatPath = path } }

// WithHTTPClient injects the underlying *http.Client (for custom transports or test
// servers). Its Timeout must stay 0 — a client-level timeout would also abort streams;
// bound a request with WithRequestTimeout or the caller's context instead.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

// WithMaxRetries sets how many times a retryable attempt is re-tried (default 2 ⇒ up to
// 3 attempts). Zero disables retries.
func WithMaxRetries(n int) Option { return func(c *Client) { c.maxRetries = n } }

// WithRetryBaseDelay sets the base backoff; attempt n waits base·2ⁿ (default 200ms).
func WithRetryBaseDelay(d time.Duration) Option { return func(c *Client) { c.retryBaseDelay = d } }

// WithRequestTimeout bounds a single non-streaming Respond attempt (default 0 ⇒ unbounded,
// governed by the caller's context). Streaming is never bounded this way — a long
// generation is not a fault.
func WithRequestTimeout(d time.Duration) Option { return func(c *Client) { c.requestTimeout = d } }

// NewClient builds a Client for the OpenAI-compatible server at baseURL, defaulting the
// model when a Request leaves it empty. A trailing slash on baseURL is trimmed so path
// joins are clean. Construction never fails — a malformed endpoint surfaces as a request
// error, matching the TS oracle (a bad fetch URL throws at call time, not at construction).
func NewClient(baseURL, model string, opts ...Option) *Client {
	c := &Client{
		baseURL:        strings.TrimRight(baseURL, "/"),
		chatPath:       defaultChatPath,
		model:          model,
		httpClient:     &http.Client{}, // no client-level Timeout: it would also kill streams
		maxRetries:     defaultMaxRetries,
		retryBaseDelay: defaultRetryBaseDelay,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

var _ Responder = (*Client)(nil)

// Respond performs one non-streaming round-trip and assembles the reply. A non-2xx
// status becomes an error (ErrContextOverflow for a 400 overflow, otherwise an
// HTTP-status error with the body sanitised); transient faults are retried per the
// Client's policy before the final error escapes.
func (c *Client) Respond(ctx context.Context, req Request) (RawResponse, error) {
	req.Stream = false
	body, err := json.Marshal(c.buildBody(req))
	if err != nil {
		return RawResponse{}, fmt.Errorf("apogee: marshal request: %w", err)
	}

	resp, cancel, err := c.send(ctx, body, c.requestTimeout)
	if err != nil {
		return RawResponse{}, err
	}
	defer cancel()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return RawResponse{}, c.statusError(resp)
	}

	var decoded chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return RawResponse{}, fmt.Errorf("apogee: decode response: %w", err)
	}
	return decoded.toRawResponse(), nil
}

// send issues the POST with bounded retries and returns the live response together with
// a cancel func the caller MUST invoke once the body is read (it releases the
// per-attempt timeout context). The body is the caller's to Close. Retries cover
// transport faults, 429, and 5xx; a caller-cancelled context aborts without retrying.
// attemptTimeout > 0 bounds each attempt so a stuck attempt becomes retryable without
// touching the caller's context — but it must outlive the body read, so it rides the
// returned cancel rather than a local defer.
func (c *Client) send(ctx context.Context, body []byte, attemptTimeout time.Duration) (*http.Response, context.CancelFunc, error) {
	url := c.baseURL + c.chatPath

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			if err := c.backoff(ctx, attempt); err != nil {
				return nil, nil, err
			}
		}

		attemptCtx, cancel := c.attemptContext(ctx, attemptTimeout)
		resp, err := c.do(attemptCtx, url, body)
		if err != nil {
			cancel()
			if ctx.Err() != nil {
				return nil, nil, ctx.Err() // caller cancelled — not a transient fault
			}
			lastErr = err
			continue // transport/timeout fault — retry if budget remains
		}
		if isRetryableStatus(resp.StatusCode) && attempt < c.maxRetries {
			drain(resp) // free the connection for reuse before retrying
			cancel()
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

// backoff sleeps base·2ⁿ before attempt n, returning early if the context is cancelled.
func (c *Client) backoff(ctx context.Context, attempt int) error {
	delay := c.retryBaseDelay << (attempt - 1)
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// statusError reads a non-2xx body and classifies it: a 400 overflow → ErrContextOverflow,
// anything else → an HTTP-status error. The body is sanitised (API key redacted, length
// capped) before it reaches the caller.
func (c *Client) statusError(resp *http.Response) error {
	raw, _ := io.ReadAll(resp.Body)
	text := c.sanitize(string(raw))
	if resp.StatusCode == http.StatusBadRequest && isContextOverflow(string(raw)) {
		return fmt.Errorf("%w: %s", ErrContextOverflow, text)
	}
	return fmt.Errorf("apogee: upstream HTTP %d: %s", resp.StatusCode, text)
}

// buildBody projects a Request onto the OpenAI chat-completions JSON body, faithfully to
// the TS oracle: the configured model wins over the request's, sampling knobs are
// included only when set, stream_options.include_usage rides every streamed request, and
// tools (when present) switch message formatting into native-tool mode.
func (c *Client) buildBody(req Request) chatRequest {
	hasTools := len(req.Tools) > 0

	body := chatRequest{Stream: req.Stream}
	body.Messages = make([]chatMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		body.Messages = append(body.Messages, formatMessage(m, hasTools))
	}

	if req.Stream {
		body.StreamOptions = &streamOptions{IncludeUsage: true}
	}

	model := c.model
	if model == "" {
		model = req.Model
	}
	body.Model = model

	s := req.Sampling
	body.Temperature = s.Temperature
	body.TopP = s.TopP
	body.TopK = s.TopK
	body.RepeatPenalty = s.RepeatPenalty
	body.MaxTokens = s.MaxTokens

	if hasTools {
		body.Tools = make([]chatTool, 0, len(req.Tools))
		for _, t := range req.Tools {
			body.Tools = append(body.Tools, chatTool{
				Type: "function",
				Function: chatToolFunction{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.Parameters,
				},
			})
		}
	}
	return body
}

// setAuth adds the bearer header when an API key is configured.
func (c *Client) setAuth(h http.Header) {
	if c.apiKey != "" {
		h.Set("Authorization", "Bearer "+c.apiKey)
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

// formatMessage renders one seam Message onto the wire schema. Without native tools a
// tool-result degrades to a user message (the model never sees a bare "tool" role it was
// not told to produce); with native tools the tool linkage is preserved. content is null
// when an assistant message carries only tool calls (OpenAI's convention).
func formatMessage(m Message, hasTools bool) chatMessage {
	if !hasTools && m.Role == "tool" {
		content := m.Content
		return chatMessage{Role: "user", Content: &content}
	}

	out := chatMessage{Role: m.Role}
	if len(m.ToolCalls) > 0 && m.Content == "" {
		out.Content = nil // null: tool-call-only assistant turn
	} else {
		content := m.Content
		out.Content = &content
	}
	if hasTools {
		out.ToolCallID = m.ToolCallID
		out.ToolCalls = m.ToolCalls
	}
	return out
}

// isRetryableStatus reports whether an HTTP status warrants a retry: 429 (rate-limited)
// or any 5xx (server-side transient).
func isRetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

// isContextOverflow reports whether a 400 body looks like a context-window rejection,
// matching the markers the TS oracle recognises across server implementations.
func isContextOverflow(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{
		"exceed_context_size",
		"exceeds the available context",
		"context length exceeded",
		"maximum context length",
		"too many tokens",
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
