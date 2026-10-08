package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
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

// getCallback requests the redirect URL with query, as the browser would after the login.
func getCallback(t *testing.T, redirectURL string, query url.Values) {
	t.Helper()
	response, err := http.Get(redirectURL + "?" + query.Encode())
	if err != nil {
		t.Fatalf("GET the callback: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close the callback response: %v", err)
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
	term, _ := testLoginTerminal(t, nil, newPipeInput(), &bytes.Buffer{})
	redirectURL, fetch := newTestFetcher(t, term)

	if !strings.HasPrefix(redirectURL, "http://127.0.0.1:") || !strings.HasSuffix(redirectURL, "/callback") {
		t.Fatalf("redirect URL = %q, want a loopback /callback", redirectURL)
	}
	getCallback(t, redirectURL, url.Values{"code": {"the-code"}, "state": {"the-state"}})
	code, state, err := fetch(context.Background(), "https://auth.example/authorize")

	if err != nil || code != "the-code" || state != "the-state" {
		t.Fatalf("fetch = %q, %q, %v; want the-code, the-state, nil", code, state, err)
	}
	requirePortFree(t, redirectURL)
}

func TestMCPLoginFetch_PastedRedirect_DeliversCodeAndState(t *testing.T) {
	t.Parallel()
	input := newPipeInput()
	out := &bytes.Buffer{}
	term, _ := testLoginTerminal(t, nil, input, out)
	redirectURL, fetch := newTestFetcher(t, term)

	input.typeLines("not a redirect", "  "+redirectURL+"?code=pasted-code&state=pasted-state  ")
	code, state, err := fetch(context.Background(), "https://auth.example/authorize")

	if err != nil || code != "pasted-code" || state != "pasted-state" {
		t.Fatalf("fetch = %q, %q, %v; want pasted-code, pasted-state, nil", code, state, err)
	}
	if !strings.Contains(out.String(), "not the address the login redirected to") {
		t.Errorf("a stray line was not answered; output:\n%s", out)
	}
}

func TestMCPLoginFetch_ErrorRedirect_SurfacesStrippedServerError(t *testing.T) {
	t.Parallel()
	term, _ := testLoginTerminal(t, nil, newPipeInput(), &bytes.Buffer{})
	redirectURL, fetch := newTestFetcher(t, term)

	getCallback(t, redirectURL, url.Values{
		"error":             {"access_denied"},
		"error_description": {"the user said \x1b[31mno\x1b[0m"},
	})
	_, _, err := fetch(context.Background(), "https://auth.example/authorize")

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
		{"desktop https", desktop, "https://auth.example/authorize?a=1&b=2", true},
		{"desktop loopback http", desktop, "http://127.0.0.1:8080/authorize", true},
		{"remote https", remote, "https://auth.example/authorize", false},
		{"desktop plain http", desktop, "http://auth.example/authorize", false},
		{"desktop file", desktop, "file:///etc/passwd", false},
		{"desktop https with a space", desktop, "https://auth.example/a b", false},
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
		_, _, err := fetch(ctx, "https://auth.example/authorize")
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
	code, _, err := fetch(context.Background(), "https://auth.example/authorize")

	if err != nil || code != "c" || len(record.calls) != 0 {
		t.Fatalf("fetch = %q, %v with opener calls %q; want c, nil and no opener call", code, err, record.calls)
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
