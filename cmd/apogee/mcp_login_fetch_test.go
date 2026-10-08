package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/muesli/cancelreader"
)

// pipeInput is a cancellable stand-in for the terminal: lines written to it arrive as typed input,
// and Cancel interrupts a blocked read the way cancelreader does on a real terminal.
type pipeInput struct {
	reader   *io.PipeReader
	writer   *io.PipeWriter
	inFlight atomic.Int32
	isClosed atomic.Bool
}

func newPipeInput() *pipeInput {
	reader, writer := io.Pipe()
	return &pipeInput{reader: reader, writer: writer}
}

func (p *pipeInput) Read(buffer []byte) (int, error) {
	p.inFlight.Add(1)
	defer p.inFlight.Add(-1)
	return p.reader.Read(buffer)
}

func (p *pipeInput) Cancel() bool {
	p.reader.CloseWithError(cancelreader.ErrCanceled)
	return true
}

func (p *pipeInput) Close() error {
	p.isClosed.Store(true)
	return nil
}

// typeLines writes each line as the user pressing Enter after it, in order, on its own goroutine.
// A write that fails because the fetch already returned and cancelled the input is expected.
func (p *pipeInput) typeLines(lines ...string) {
	go func() {
		for _, line := range lines {
			if _, err := p.writer.Write([]byte(line + "\n")); err != nil {
				return
			}
		}
	}()
}

// launchRecord captures the opener command lines a fetch would have run.
type launchRecord struct {
	calls [][]string
}

func (r *launchRecord) launch(name string, args ...string) error {
	r.calls = append(r.calls, append([]string{name}, args...))
	return nil
}

// testLoginTerminal is a login terminal on linux with env as its environment, input as its stdin,
// a PATH that has every program under /usr/bin, and launches recorded instead of run.
func testLoginTerminal(t *testing.T, env map[string]string, input *pipeInput, out *bytes.Buffer) (loginTerminal, *launchRecord) {
	t.Helper()
	record := &launchRecord{}
	return loginTerminal{
		out:       out,
		openInput: func() (cancelreader.CancelReader, error) { return input, nil },
		goos:      "linux",
		getenv:    func(name string) string { return env[name] },
		look:      func(name string) (string, error) { return "/usr/bin/" + name, nil },
		launch:    record.launch,
		workspace: t.TempDir(),
	}, record
}

// newTestFetcher binds a fetcher over term and closes it when the test ends.
func newTestFetcher(t *testing.T, term loginTerminal) (string, func(context.Context, string) (string, string, error)) {
	t.Helper()
	redirectURL, fetch, closeListener, err := newLoginFetcher(term)
	if err != nil {
		t.Fatalf("newLoginFetcher: %v", err)
	}
	t.Cleanup(closeListener)
	return redirectURL, fetch
}

// testState is the state testAuthorizeURL sends; an authorization response must echo it.
const testState = "the-state"

// testAuthorizeURL is an authorize URL as a Login builds it, carrying testState.
const testAuthorizeURL = "https://auth.example/authorize?state=" + testState

// getCallback requests the redirect URL with query, as the browser would after the login, and
// returns the status the callback answered with.
func getCallback(t *testing.T, redirectURL string, query url.Values) int {
	t.Helper()
	response, err := http.Get(redirectURL + "?" + query.Encode())
	if err != nil {
		t.Fatalf("GET the callback: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close the callback response: %v", err)
	}
	return response.StatusCode
}

// fetchOutcome is what one fetch returned.
type fetchOutcome struct {
	code  string
	state string
	err   error
}

// startFetch runs fetch for authorizeURL on its own goroutine and returns once it is waiting on
// input, by which point it has recorded the login's state; the outcome arrives on the channel.
func startFetch(
	t *testing.T,
	fetch func(context.Context, string) (string, string, error),
	input *pipeInput,
	authorizeURL string,
) <-chan fetchOutcome {
	t.Helper()
	outcome := make(chan fetchOutcome, 1)
	go func() {
		code, state, err := fetch(context.Background(), authorizeURL)
		outcome <- fetchOutcome{code: code, state: state, err: err}
	}()
	waitFor(t, func() bool { return input.inFlight.Load() == 1 })
	return outcome
}

// awaitOutcome waits for a started fetch to return.
func awaitOutcome(t *testing.T, outcome <-chan fetchOutcome) fetchOutcome {
	t.Helper()
	select {
	case result := <-outcome:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("fetch did not return")
		return fetchOutcome{}
	}
}

// requireStillWaiting fails if a started fetch has already returned.
func requireStillWaiting(t *testing.T, outcome <-chan fetchOutcome) {
	t.Helper()
	select {
	case result := <-outcome:
		t.Fatalf("fetch returned %q, %q, %v; want it still waiting", result.code, result.state, result.err)
	default:
	}
}

// requirePortFree fails unless the redirect URL's port can be bound again.
func requirePortFree(t *testing.T, redirectURL string) {
	t.Helper()
	parsed, err := url.Parse(redirectURL)
	if err != nil {
		t.Fatalf("parse the redirect URL: %v", err)
	}
	listener, err := net.Listen("tcp", parsed.Host)
	if err != nil {
		t.Fatalf("the callback port is still held: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close the probe listener: %v", err)
	}
}

func TestMCPLoginFetch_Callback_DeliversCodeAndState(t *testing.T) {
	t.Parallel()
	input := newPipeInput()
	term, _ := testLoginTerminal(t, nil, input, &bytes.Buffer{})
	redirectURL, fetch := newTestFetcher(t, term)

	if !strings.HasPrefix(redirectURL, "http://127.0.0.1:") || !strings.HasSuffix(redirectURL, "/callback") {
		t.Fatalf("redirect URL = %q, want a loopback /callback", redirectURL)
	}
	outcome := startFetch(t, fetch, input, testAuthorizeURL)
	getCallback(t, redirectURL, url.Values{"code": {"the-code"}, "state": {testState}})
	result := awaitOutcome(t, outcome)

	if result.err != nil || result.code != "the-code" || result.state != testState {
		t.Fatalf("fetch = %q, %q, %v; want the-code, %s, nil", result.code, result.state, result.err, testState)
	}
	requirePortFree(t, redirectURL)
}

func TestMCPLoginFetch_Callback_ResponseForAnotherLoginEndsNothing(t *testing.T) {
	t.Parallel()
	input := newPipeInput()
	term, _ := testLoginTerminal(t, nil, input, &bytes.Buffer{})
	redirectURL, fetch := newTestFetcher(t, term)

	// Sent before the fetch starts: no login is in progress yet, so nothing may be accepted.
	if status := getCallback(t, redirectURL, url.Values{"code": {"early"}, "state": {testState}}); status != http.StatusBadRequest {
		t.Errorf("a callback before the fetch answered %d, want %d", status, http.StatusBadRequest)
	}
	outcome := startFetch(t, fetch, input, testAuthorizeURL)
	forged := []url.Values{
		{"code": {"forged"}, "state": {"another-state"}},
		{"error": {"access_denied"}},
		{"error": {"access_denied"}, "state": {"another-state"}},
	}
	for _, query := range forged {
		if status := getCallback(t, redirectURL, query); status != http.StatusBadRequest {
			t.Errorf("callback %v answered %d, want %d", query, status, http.StatusBadRequest)
		}
	}
	requireStillWaiting(t, outcome)

	getCallback(t, redirectURL, url.Values{"code": {"the-code"}, "state": {testState}})
	result := awaitOutcome(t, outcome)

	if result.err != nil || result.code != "the-code" || result.state != testState {
		t.Fatalf("fetch = %q, %q, %v; want the-code, %s, nil", result.code, result.state, result.err, testState)
	}
}

// answerPageCase is one authorization response the callback answers, and the page it answers with.
type answerPageCase struct {
	name     string
	query    url.Values
	wantPage string
}

func answerPageCases() []answerPageCase {
	return []answerPageCase{
		{
			name:     "login received",
			query:    url.Values{"code": {"the-code"}, "state": {testState}},
			wantPage: loginReceivedPage,
		},
		{
			name:     "login refused",
			query:    url.Values{"error": {"access_denied"}, "state": {testState}},
			wantPage: loginFailedPage,
		},
	}
}

func TestMCPLoginFetch_CallbackHandler_FlushesThePageBeforeDeliveringTheResult(t *testing.T) {
	t.Parallel()
	for _, testCase := range answerPageCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			results := make(chan loginResult, 1)
			state := &loginState{}
			state.expect(testState)
			writer := &deliveryGuardWriter{t: t, results: results, header: http.Header{}}
			request := httptest.NewRequest(http.MethodGet, loginCallbackPath+"?"+testCase.query.Encode(), nil)

			callbackHandler(state, results).ServeHTTP(writer, request)

			if writer.flushed != testCase.wantPage {
				t.Errorf("flushed %q before the result was delivered, want %q", writer.flushed, testCase.wantPage)
			}
			if len(results) != 1 {
				t.Errorf("delivered %d results, want 1", len(results))
			}
		})
	}
}

// deliveryGuardWriter stands in for the browser's connection and fails the test when any of the
// answer page is written or flushed once the result has reached results: the fetch closes the
// listener as soon as it hears of the result, so a page still unsent by then is lost. It has no
// WriteString, so io.WriteString reaches Write too.
type deliveryGuardWriter struct {
	t       *testing.T
	results chan loginResult
	header  http.Header
	body    bytes.Buffer
	flushed string
}

func (writer *deliveryGuardWriter) Header() http.Header {
	return writer.header
}

func (writer *deliveryGuardWriter) WriteHeader(int) {}

func (writer *deliveryGuardWriter) Write(data []byte) (int, error) {
	if len(writer.results) != 0 {
		writer.t.Errorf("wrote %q after the result was delivered", data)
	}
	return writer.body.Write(data)
}

func (writer *deliveryGuardWriter) Flush() {
	if len(writer.results) != 0 {
		writer.t.Error("flushed after the result was delivered")
	}
	writer.flushed = writer.body.String()
}

func TestMCPLoginFetch_Callback_BrowserReceivesTheWholeAnswerPage(t *testing.T) {
	t.Parallel()
	for _, testCase := range answerPageCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if page := callbackPageWhileFetching(t, testCase.query); page != testCase.wantPage {
				t.Fatalf("the browser got %q, want %q", page, testCase.wantPage)
			}
		})
	}
}

// callbackPageWhileFetching starts a fetch, sends the browser's callback while the fetch waits on
// it, and returns the page the browser was answered with once the fetch has returned.
func callbackPageWhileFetching(t *testing.T, query url.Values) string {
	t.Helper()
	input := newPipeInput()
	term, _ := testLoginTerminal(t, nil, input, &bytes.Buffer{})
	redirectURL, fetch := newTestFetcher(t, term)
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		_, _, _ = fetch(context.Background(), testAuthorizeURL)
	}()
	waitFor(t, func() bool { return input.inFlight.Load() == 1 })

	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	response, err := client.Get(redirectURL + "?" + query.Encode())
	if err != nil {
		t.Fatalf("GET the callback: %v", err)
	}
	page, err := io.ReadAll(response.Body)
	if closeErr := response.Body.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatalf("read the answer page: %v", err)
	}
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("fetch did not return after the callback")
	}
	return string(page)
}

func TestMCPLoginFetch_PastedRedirect_DeliversCodeAndState(t *testing.T) {
	t.Parallel()
	input := newPipeInput()
	out := &bytes.Buffer{}
	term, _ := testLoginTerminal(t, nil, input, out)
	redirectURL, fetch := newTestFetcher(t, term)

	input.typeLines(
		"not a redirect",
		redirectURL+"?code=stale-code&state=an-earlier-attempt",
		"  "+redirectURL+"?code=pasted-code&state="+testState+"  ",
	)
	code, state, err := fetch(context.Background(), testAuthorizeURL)

	if err != nil || code != "pasted-code" || state != testState {
		t.Fatalf("fetch = %q, %q, %v; want pasted-code, %s, nil", code, state, err, testState)
	}
	if !strings.Contains(out.String(), "not the address the login redirected to") {
		t.Errorf("a stray line was not answered; output:\n%s", out)
	}
	if !strings.Contains(out.String(), "belongs to a different login attempt") {
		t.Errorf("a pasted redirect for another login was not answered; output:\n%s", out)
	}
}

func TestMCPLoginFetch_ErrorRedirect_SurfacesStrippedServerError(t *testing.T) {
	t.Parallel()
	input := newPipeInput()
	term, _ := testLoginTerminal(t, nil, input, &bytes.Buffer{})
	redirectURL, fetch := newTestFetcher(t, term)

	outcome := startFetch(t, fetch, input, testAuthorizeURL)
	getCallback(t, redirectURL, url.Values{
		"error":             {"access_denied"},
		"error_description": {"the user said \x1b[31mno\x1b[0m"},
		"state":             {testState},
	})
	err := awaitOutcome(t, outcome).err

	if !errors.Is(err, errLoginRefused) {
		t.Fatalf("fetch error = %v, want errLoginRefused", err)
	}
	if message := err.Error(); strings.Contains(message, "\x1b") ||
		!strings.Contains(message, "access_denied") || !strings.Contains(message, "the user said") {
		t.Errorf("fetch error = %q, want the server's error with escape sequences stripped", message)
	}
}

func TestMCPLoginFetch_EnterOpensOnlyOnLocalDesktopAndOnlySafeURLs(t *testing.T) {
	t.Parallel()
	desktop := map[string]string{"DISPLAY": ":0"}
	remote := map[string]string{"DISPLAY": ":0", "SSH_CONNECTION": "10.0.0.2 5555 10.0.0.1 22"}
	cases := []struct {
		name         string
		env          map[string]string
		authorizeURL string
		isOpened     bool
	}{
		{"desktop https", desktop, "https://auth.example/authorize?a=1&b=2&state=s", true},
		{"desktop loopback http", desktop, "http://127.0.0.1:8080/authorize?state=s", true},
		{"remote https", remote, "https://auth.example/authorize?state=s", false},
		{"desktop plain http", desktop, "http://auth.example/authorize?state=s", false},
		{"desktop file", desktop, "file:///etc/passwd?state=s", false},
		{"desktop https with a space", desktop, "https://auth.example/a b?state=s", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := newPipeInput()
			term, record := testLoginTerminal(t, tc.env, input, &bytes.Buffer{})
			redirectURL, fetch := newTestFetcher(t, term)

			input.typeLines("", redirectURL+"?code=c&state=s")
			_, _, err := fetch(context.Background(), tc.authorizeURL)

			if err != nil {
				t.Fatalf("fetch: %v", err)
			}
			var want [][]string
			if tc.isOpened {
				want = [][]string{{"/usr/bin/xdg-open", tc.authorizeURL}}
			}
			if !slices.EqualFunc(record.calls, want, slices.Equal) {
				t.Errorf("opener calls = %q, want %q", record.calls, want)
			}
		})
	}
}

func TestMCPLoginFetch_ContextCancel_ReturnsJoinsInputAndFreesPort(t *testing.T) {
	t.Parallel()
	input := newPipeInput()
	term, _ := testLoginTerminal(t, nil, input, &bytes.Buffer{})
	redirectURL, fetch := newTestFetcher(t, term)
	ctx, cancel := context.WithCancel(context.Background())
	returned := make(chan error, 1)

	go func() {
		_, _, err := fetch(ctx, testAuthorizeURL)
		returned <- err
	}()
	waitFor(t, func() bool { return input.inFlight.Load() == 1 })
	cancel()

	select {
	case err := <-returned:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("fetch error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fetch did not return after its context was cancelled")
	}
	if input.inFlight.Load() != 0 || !input.isClosed.Load() {
		t.Errorf("input read in flight = %d, closed = %v; want the read joined and the input closed",
			input.inFlight.Load(), input.isClosed.Load())
	}
	requirePortFree(t, redirectURL)
}

func TestMCPLoginFetch_CloseWithoutFetch_FreesPort(t *testing.T) {
	t.Parallel()
	term, _ := testLoginTerminal(t, nil, newPipeInput(), &bytes.Buffer{})
	redirectURL, _, closeListener, err := newLoginFetcher(term)
	if err != nil {
		t.Fatalf("newLoginFetcher: %v", err)
	}

	closeListener()

	requirePortFree(t, redirectURL)
}

func TestMCPLoginFetch_SplitTerminalLines_TreatsCRLFAsOneBreak(t *testing.T) {
	t.Parallel()
	input := newPipeInput()
	term, record := testLoginTerminal(t, map[string]string{"DISPLAY": ":0"}, input, &bytes.Buffer{})
	redirectURL, fetch := newTestFetcher(t, term)

	go func() {
		// "\r\n" then "\r": a paste from Windows, then a raw-mode Enter. Neither may open anything,
		// since the first line is not empty and the second carries the code.
		_, _ = input.writer.Write([]byte("junk\r\n" + redirectURL + "?code=c&state=s\r"))
	}()
	code, _, err := fetch(context.Background(), "https://auth.example/authorize?state=s")

	if err != nil || code != "c" || len(record.calls) != 0 {
		t.Fatalf("fetch = %q, %v with opener calls %q; want c, nil and no opener call", code, err, record.calls)
	}
}

func TestMCPLoginFetch_AuthorizeURLWithoutState_FailsAndFreesPort(t *testing.T) {
	t.Parallel()
	term, _ := testLoginTerminal(t, nil, newPipeInput(), &bytes.Buffer{})
	redirectURL, fetch := newTestFetcher(t, term)

	_, _, err := fetch(context.Background(), "https://auth.example/authorize")

	if err == nil || !strings.Contains(err.Error(), "carries no state") {
		t.Fatalf("fetch error = %v, want the authorize URL refused for carrying no state", err)
	}
	requirePortFree(t, redirectURL)
}

func TestMCPLoginFetch_EnterWithoutOpener_SaysToCopyTheURL(t *testing.T) {
	t.Parallel()
	input := newPipeInput()
	out := &bytes.Buffer{}
	term, record := testLoginTerminal(t, map[string]string{"DISPLAY": ":0"}, input, out)
	term.look = func(name string) (string, error) { return "", exec.ErrNotFound }
	redirectURL, fetch := newTestFetcher(t, term)

	input.typeLines("", redirectURL+"?code=c&state="+testState)
	_, _, err := fetch(context.Background(), testAuthorizeURL)

	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(record.calls) != 0 {
		t.Errorf("opener calls = %q, want none", record.calls)
	}
	if !strings.Contains(out.String(), "No browser opener (xdg-open) on this machine; copy the URL into a browser yourself.") {
		t.Errorf("the missing opener was not reported; output:\n%s", out)
	}
}

// waitFor polls condition until it holds, failing the test after a few seconds.
func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition never held")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
