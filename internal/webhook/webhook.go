// Package webhook is the one HTTP POST every out-of-process webhook handler sends, shared by the
// two lanes that fire one: the observe lane's Runner (internal/reactions), whose reply is discarded,
// and the agent's sync lane (internal/agent), whose reply is an `advise:` trailer or a `gate:`
// verdict. The request is the same on both — the seam payload as JSON, apogee's own two headers,
// the entry's literal `headers:` and its `headers-env:` read from the environment at SEND time — so
// it is built in one place, and what differs between the lanes (what is done with the reply, and
// under which deadline) stays with the lane.
//
// One direction: the package imports internal/domain for the handler it reads and nothing else in
// the tree — never internal/agent, never internal/reactions, never internal/tools — so both lanes
// can depend on it without either depending on the other.
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
)

// MaxResponseDrain bounds how much of a webhook's reply apogee reads when the reply's content is
// NOT what it is after — the observe lane, and the body of any non-2xx answer. The body is read for
// one reason only there: an unread connection cannot be reused, so a reaction firing every Turn
// would open a fresh socket each time. A server that streams a gigabyte back is therefore drained
// up to this bound and then hung up on.
const MaxResponseDrain = 64 << 10

// Post sends body as the JSON payload of one POST to the handler's URL and returns the reply. The
// caller owns the reply's Body and must Close it; a reply that is not a 2xx is drained up to
// MaxResponseDrain, closed here and reported as `HTTP <status>`, so a caller never reads a body
// the endpoint refused to answer with.
//
// There is NO RETRY, by decision: a Reaction is a post-hoc notification, a retry would fire the
// user's endpoint twice for one event, and a queue holding failed firings would outlive the run
// they belong to (ADR 0073 §6).
//
// The headers are resolved BEFORE the request is sent, so a `headers-env:` entry naming a variable
// that is not set fails without the endpoint ever hearing from us — the alternative is a POST that
// arrives unauthenticated and is refused for a reason the user cannot see from here. The client is
// built per send because timeout is the ENTRY's, and a client with no Transport of its own uses
// http.DefaultTransport — so every entry still shares one connection pool.
func Post(
	ctx context.Context,
	handler domain.WebhookHandler,
	timeout time.Duration,
	body []byte,
) (*http.Response, error) {
	header, err := Headers(handler)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, handler.URL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("could not build the request: %w", err)
	}
	request.Header = header
	request.ContentLength = int64(len(body))

	client := &http.Client{Timeout: timeout}
	response, err := client.Do(request)
	if err != nil {
		return nil, PostFailure(timeout, err)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		Drain(response)
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return response, nil
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
