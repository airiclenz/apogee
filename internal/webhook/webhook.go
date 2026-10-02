// Package webhook is the one HTTP POST every out-of-process webhook handler sends, shared by the
// two lanes that fire one: the observe lane's Runner (internal/reactions), whose reply is discarded,
// and the agent's sync lane (internal/agent), whose reply is an `advise:` trailer or a `gate:`
// verdict. The request is the same on both — the seam payload as JSON, apogee's own two headers,
// the entry's literal `headers:` and its `headers-env:` read from the environment at SEND time — so
// it is built in one place, and what differs between the lanes (what is done with the reply, and
// under which deadline) stays with the lane.
//
// Every post goes through the url-safety guarded client (security.URLGuard.GuardedClient): the
// endpoint is an operator-named one, so it meets the url-safety host lists before any dial but not
// the resolved-IP floor, its connection is pinned to its own resolved addresses, and a redirect is
// handed back as the reply rather than followed — a `headers-env:` token never reaches a second
// host (ADR 0012, Amendment 2026-07-26).
//
// One direction: the package imports internal/domain for the handler it reads and internal/security
// for the guard it posts through, and nothing else in the tree — never internal/agent, never
// internal/reactions, never internal/tools — so both lanes can depend on it without either
// depending on the other.
package webhook

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

// MaxResponseDrain bounds how much of a webhook's reply apogee reads when the reply's content is
// NOT what it is after — the observe lane, and the body of any non-2xx answer. The body is read for
// one reason only there: the endpoint finishes writing its answer before apogee hangs up, rather
// than meeting a reset mid-reply. Every post's connection is its own and is closed once the reply
// is (Post), so nothing is kept for reuse; a server that streams a gigabyte back is drained up to
// this bound and then hung up on.
const MaxResponseDrain = 64 << 10

// Post sends body as the JSON payload of one POST to the handler's URL through guard and returns
// the reply. The caller owns the reply's Body and must Close it; a reply that is not a 2xx is
// drained up to MaxResponseDrain, closed here and reported as `HTTP <status>`, so a caller never
// reads a body the endpoint refused to answer with. A 3xx is such a reply: the guarded client never
// follows a redirect, so the request — and the `headers-env:` values on it — reaches the configured
// host and no other.
//
// The guard judges the endpoint as an operator-named one, the way an `mcp-servers:` endpoint is
// judged (ADR 0012, Amendment 2026-07-26): the pre-flight applies the url-safety allow/deny host
// lists with the resolved-IP floor off — a webhook on loopback or the LAN is the user's own — and
// the client's dial is pinned to the endpoint's own resolved addresses (DialPinDestination), so a
// rebind to a different private address is still refused. A refused endpoint is never dialled.
//
// There is NO RETRY, by decision: a Reaction is a post-hoc notification, a retry would fire the
// user's endpoint twice for one event, and a queue holding failed firings would outlive the run
// they belong to (ADR 0073 §6).
//
// The headers are resolved BEFORE the request is sent, so a `headers-env:` entry naming a variable
// that is not set fails without the endpoint ever hearing from us — the alternative is a POST that
// arrives unauthenticated and is refused for a reason the user cannot see from here. timeout bounds
// the WHOLE send from here on — the pin's lookup, the request and the reply's body — because the
// sync lane's ctx carries no deadline of its own. The client is built per send, and its connection
// is closed with the reply: nothing is pooled across posts.
func Post(
	ctx context.Context,
	guard security.URLGuard,
	handler domain.WebhookHandler,
	timeout time.Duration,
	body []byte,
) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	header, err := Headers(handler)
	if err != nil {
		cancel()
		return nil, err
	}
	target, client, err := guardedClient(ctx, guard, handler.URL, timeout)
	if err != nil {
		cancel()
		return nil, err
	}
	release := func() {
		cancel()
		client.CloseIdleConnections()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		release()
		return nil, fmt.Errorf("could not build the request: %w", err)
	}
	request.Header = header
	request.ContentLength = int64(len(body))

	response, err := client.Do(request)
	if err != nil {
		release()
		return nil, PostFailure(timeout, err)
	}
	response.Body = &releasingBody{ReadCloser: response.Body, release: release}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		Drain(response)
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return response, nil
}

// guardedClient vets raw against guard and builds the client the post goes through. The pre-flight
// runs on the floorless copy (DisableIPFloor) and the client on guard itself, whose floor is what
// pins the dial to the endpoint's own addresses — the pair vetEndpoint in internal/mcp builds.
//
// Every refusal is worded here and none quotes the URL, PostFailure's rule: a webhook URL is
// exactly the kind of thing that carries a token in its path or query. The host a url-safety list
// closed is named, which is what the user has to look for in their own lists.
func guardedClient(
	ctx context.Context,
	guard security.URLGuard,
	raw string,
	timeout time.Duration,
) (*url.URL, *http.Client, error) {
	target, err := security.NormalizeURL(raw)
	if err != nil {
		return nil, nil, errors.New("the URL does not parse")
	}
	if err := guard.DisableIPFloor().CheckContext(ctx, target.String()); err != nil {
		return nil, nil, fmt.Errorf("refused by url-safety: %w", err)
	}
	client, err := guard.GuardedClient(ctx, target, security.GuardedClientOptions{
		Timeout: timeout,
		Policy:  security.DialPinDestination,
	})
	if err != nil {
		return nil, nil, pinFailure(ctx, timeout, err)
	}
	return target, client, nil
}

// pinFailure words a GuardedClient refusal. The pin resolves the endpoint under ctx, so a deadline
// hit there is the same timeout a stalled endpoint is; an unusable egress proxy is named without
// its value (a proxy URL may carry credentials); any other refusal is the pin's own cause, which
// names the host that could not be resolved and nothing else.
func pinFailure(ctx context.Context, timeout time.Duration, err error) error {
	var pinErr *security.PinError
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("timed out after %s", timeout)
	case errors.Is(err, security.ErrProxyUnusable):
		return errors.New("refused by url-safety: the egress proxy is not a usable URL")
	case errors.As(err, &pinErr):
		return fmt.Errorf("refused by url-safety: %w", pinErr.Err)
	default:
		return fmt.Errorf("refused by url-safety: %w", err)
	}
}

// releasingBody is the reply body Post hands back: closing it closes the body, then ends the
// send's deadline and closes the per-send client's idle connection, so a post leaves no socket
// and no timer behind once its reply is done with.
type releasingBody struct {
	io.ReadCloser
	release func()
}

// Close closes the underlying body and releases the send. It is safe to call more than once.
func (b *releasingBody) Close() error {
	err := b.ReadCloser.Close()
	b.release()
	return err
}

// Drain reads up to MaxResponseDrain of the reply and closes it — what a caller does with a reply
// whose content it never looks at.
func Drain(response *http.Response) {
	defer func() { _ = response.Body.Close() }()
	_, _ = io.CopyN(io.Discard, response.Body, MaxResponseDrain)
}

// Headers builds the request's headers: apogee's own two first, then the entry's literal
// `headers:`, then its `headers-env:` read from the environment at SEND time — so a token rotated
// in the shell that launched apogee is picked up by the next firing, and an ordering that lets the
// user's own entries override apogee's defaults.
//
// A variable that is not set is a failure naming the header and the variable and NEVER the value,
// which is the whole reason `headers-env:` exists: the secret lives in the environment so that it
// is never in the config file, and it must not leak into a failure line either.
func Headers(handler domain.WebhookHandler) (http.Header, error) {
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	header.Set("User-Agent", "apogee")
	for _, name := range sortedKeys(handler.Headers) {
		header.Set(name, handler.Headers[name])
	}
	for _, name := range sortedKeys(handler.HeadersEnv) {
		envName := handler.HeadersEnv[name]
		value, ok := os.LookupEnv(envName)
		if !ok {
			return nil, fmt.Errorf("header %s: the environment variable %s is not set", name, envName)
		}
		header.Set(name, value)
	}
	return header, nil
}

// sortedKeys orders a header map so two runs of the same entry build the request the same way and
// a failure names the same header every time — map order alone would make both arbitrary.
func sortedKeys(m map[string]string) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// PostFailure words a transport-level failure. It deliberately drops the URL that net/http wraps
// its errors with: the failure line is shown to the user by the Driver, the entry's name is
// already on it, and a webhook URL is exactly the kind of thing that carries a token in its path
// or query.
func PostFailure(timeout time.Duration, err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Timeout() {
			return fmt.Errorf("timed out after %s", timeout)
		}
		err = urlErr.Err
	}
	return fmt.Errorf("POST failed: %w", err)
}
