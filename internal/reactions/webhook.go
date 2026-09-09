package reactions

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

// maxResponseDrain bounds how much of a webhook's reply apogee reads before closing the body. The
// body is read for one reason only — an unread connection cannot be reused, so a Reaction firing
// every Turn would open a fresh socket each time — and the content is never looked at, because
// nothing a Reaction answers may reach the model, the conversation or the Session record (ADR 0073
// §1). A server that streams a gigabyte back is therefore drained up to this bound and then hung up
// on.
const maxResponseDrain = 64 << 10

// webhookSender POSTs the payload to an entry's `run: url:` webhook. It carries no state at all:
// the client is built per send because its Timeout is the ENTRY's, and a client with no Transport
// of its own uses http.DefaultTransport — so every entry still shares one connection pool rather
// than opening a fresh socket for each firing.
//
// It is safe for concurrent use, which the Executor contract requires.
type webhookSender struct{}

// Run POSTs the payload as JSON and reports anything that was not a 2xx. There is NO RETRY, by
// decision: a Reaction is a post-hoc notification, a retry would fire the user's endpoint twice for
// one event, and a queue holding failed firings would outlive the run they belong to (ADR 0073 §6).
//
// The headers are resolved BEFORE the request is sent, so a `headers-env:` entry naming a variable
// that is not set fails without the endpoint ever hearing from us — the alternative is a POST that
// arrives unauthenticated and is refused for a reason the user cannot see from here.
func (webhookSender) Run(ctx context.Context, r domain.Reaction, p Payload) error {
	handler, ok := r.Handler.(domain.WebhookHandler)
	if !ok {
		return errors.New("no webhook to POST to")
	}
	body, err := encodePayload(p)
	if err != nil {
		return err
	}
	header, err := webhookHeaders(handler)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, handler.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("could not build the request: %w", err)
	}
	request.Header = header
	request.ContentLength = int64(len(body))

	client := &http.Client{Timeout: r.Timeout}
	response, err := client.Do(request)
	if err != nil {
		return postFailure(r.Timeout, err)
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.CopyN(io.Discard, response.Body, maxResponseDrain)

	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return nil
}

// webhookHeaders builds the request's headers: apogee's own two first, then the entry's literal
// `headers:`, then its `headers-env:` read from the environment at SEND time — so a token rotated
// in the shell that launched apogee is picked up by the next firing, and an ordering that lets the
// user's own entries override apogee's defaults.
//
// A variable that is not set is a failure naming the header and the variable and NEVER the value,
// which is the whole reason `headers-env:` exists: the secret lives in the environment so that it
// is never in the config file, and it must not leak into a failure line either.
func webhookHeaders(handler domain.WebhookHandler) (http.Header, error) {
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

// postFailure words a transport-level failure. It deliberately drops the URL that net/http wraps
// its errors with: the failure line is shown to the user by the Driver, the entry's name is
// already on it, and a webhook URL is exactly the kind of thing that carries a token in its path
// or query.
func postFailure(timeout time.Duration, err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Timeout() {
			return fmt.Errorf("timed out after %s", timeout)
		}
		err = urlErr.Err
	}
	return fmt.Errorf("POST failed: %w", err)
}
