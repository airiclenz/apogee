package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/muesli/cancelreader"

	"github.com/airiclenz/apogee/internal/mcpauth"
	"github.com/airiclenz/apogee/internal/present"
	"github.com/airiclenz/apogee/internal/sanitize"
	"github.com/airiclenz/apogee/internal/security"
)

// loginListenAddress is where the callback listener binds: loopback only, on a port the kernel
// picks. A preregistered client-id relies on RFC 8252 §7.3, which obliges the authorization server
// to accept any port on a loopback redirect URI, so there is no fixed-port setting (owner,
// 2026-10-08).
const loginListenAddress = "127.0.0.1:0"

// loginCallbackPath is the path of the redirect URI the authorization server sends the browser to.
const loginCallbackPath = "/callback"

// callbackReadHeaderTimeout bounds how long the callback listener waits for a request's headers,
// so a local client that opens a connection and says nothing cannot hold it.
const callbackReadHeaderTimeout = 10 * time.Second

// callbackShutdownTimeout bounds how long closing the listener waits for an in-flight answer page
// to reach the browser before it drops whatever connection is still open.
const callbackShutdownTimeout = 2 * time.Second

// The pages the callback answers the browser with, once the authorization response has arrived.
const (
	loginReceivedPage = "apogee: login received. You can close this tab and return to the terminal.\n"
	loginFailedPage   = "apogee: the login did not complete. The terminal says why; you can close this tab.\n"
)

// errLoginRefused is the sentinel a fetch wraps when the authorization server redirected back with
// an `error` instead of a code (RFC 6749 §4.1.2.1): the user declined, or the server refused.
var errLoginRefused = errors.New("the authorization server refused the login")

// loginTerminal is the terminal an MCP OAuth login talks through, every input injected so a test
// can drive it without a browser, a desktop or a real stdin. The zero value of each field falls
// back to this process's own (see withDefaults).
type loginTerminal struct {
	// out receives the authorize URL and the instructions around it.
	out io.Writer
	// openInput opens a cancellable reader over the user's input for one wait; the fetcher
	// cancels it, joins the read and closes it before returning, so the next prompt reads the
	// terminal from where this one left it. Nil means os.Stdin.
	openInput func() (cancelreader.CancelReader, error)
	// goos is the platform whose browser opener to run. Empty means runtime.GOOS.
	goos string
	// getenv is the environment the locality and desktop checks read. Nil means os.Getenv.
	getenv func(string) string
	// look resolves the opener program on PATH. Nil means exec.LookPath.
	look func(string) (string, error)
	// launch starts the resolved opener and does not wait for the browser. Nil means
	// startDetached.
	launch func(name string, args ...string) error
	// workspace is the model-writable root the opener program may not resolve inside
	// (security.ResolveProgram).
	workspace string
}

// loginResult is how one login wait ends: an authorization code and its state, or an error.
type loginResult struct {
	code  string
	state string
	err   error
}

// newLoginFetcher binds the loopback callback listener and returns the redirect URL it answers
// on, the fetch a Login hands the authorize URL to, and the close that frees the port. The bind
// comes first so the redirect URL is known before the client is registered; the caller defers
// close, so a Login that fails before fetching still frees the port. fetch closes the listener
// itself when it returns, and may be called once.
func newLoginFetcher(term loginTerminal) (string, mcpauth.CodeFetcher, func(), error) {
	term = term.withDefaults()
	listener, err := net.Listen("tcp", loginListenAddress)
	if err != nil {
		return "", nil, nil, fmt.Errorf("open the login callback listener: %w", err)
	}

	results := make(chan loginResult, 1)
	state := &loginState{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+loginCallbackPath, callbackHandler(state, results))
	server := &http.Server{Handler: mux, ReadHeaderTimeout: callbackReadHeaderTimeout}
	served := make(chan struct{})
	go func() {
		defer close(served)
		if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
			deliver(results, loginResult{err: fmt.Errorf("the login callback listener failed: %w", err)})
		}
	}()

	var once sync.Once
	closeListener := func() {
		once.Do(func() {
			// Shutdown lets a callback that is still answering the browser finish its page; past
			// the timeout, Close drops what is left. Either error leaves nothing to do: the port
			// is released either way.
			ctx, cancel := context.WithTimeout(context.Background(), callbackShutdownTimeout)
			defer cancel()
			if server.Shutdown(ctx) != nil {
				_ = server.Close()
			}
			<-served
		})
	}
	redirectURL := "http://" + listener.Addr().String() + loginCallbackPath
	fetch := func(ctx context.Context, authorizeURL string) (string, string, error) {
		defer closeListener()
		want, err := authorizeState(authorizeURL)
		if err != nil {
			return "", "", err
		}
		state.expect(want)
		return term.await(ctx, authorizeURL, redirectURL, state, results)
	}
	return redirectURL, fetch, closeListener, nil
}

// withDefaults fills every unset field with this process's own terminal, platform and launcher.
func (term loginTerminal) withDefaults() loginTerminal {
	if term.out == nil {
		term.out = os.Stderr
	}
	if term.openInput == nil {
		term.openInput = func() (cancelreader.CancelReader, error) { return cancelreader.NewReader(os.Stdin) }
	}
	if term.goos == "" {
		term.goos = runtime.GOOS
	}
	if term.getenv == nil {
		term.getenv = os.Getenv
	}
	if term.launch == nil {
		term.launch = startDetached
	}
	return term
}

// await prints the authorize URL and waits for whichever comes first: the browser's callback, a
// pasted redirect URL carrying state's value, or ctx's end. An empty line opens the URL on a local
// desktop and repeats the paste instruction anywhere else; a line that is not this login's redirect
// URL is answered and the wait goes on.
func (term loginTerminal) await(
	ctx context.Context,
	authorizeURL, redirectURL string,
	state *loginState,
	results <-chan loginResult,
) (string, string, error) {
	isLocalDesktop := present.Locality(term.getenv) == present.Local && present.HasDesktop(term.goos, term.getenv)
	term.printPrompt(authorizeURL, redirectURL, isLocalDesktop)
	lines, stopInput := term.readLines()
	defer stopInput()

	for {
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case result := <-results:
			return result.code, result.state, result.err
		case line, isOpen := <-lines:
			if !isOpen {
				lines = nil
				_, _ = fmt.Fprintln(term.out, "Input closed; waiting for the browser to finish the login.")
				continue
			}
			if result, isDone := term.answerLine(line, authorizeURL, redirectURL, state, isLocalDesktop); isDone {
				return result.code, result.state, result.err
			}
		}
	}
}

// printPrompt shows the full authorize URL — always, so a user who cannot or will not have it
// opened for them can copy it — and says what the terminal will accept while it waits.
func (term loginTerminal) printPrompt(authorizeURL, redirectURL string, isLocalDesktop bool) {
	_, _ = fmt.Fprintf(term.out, "Open this URL in a browser to log in:\n\n  %s\n\n", sanitize.StripEscapesToLine(authorizeURL))
	if isLocalDesktop {
		_, _ = fmt.Fprintln(term.out, "Press Enter to open it in your browser, or paste the URL your browser "+
			"lands on after the login.")
		return
	}
	term.printPasteInstruction(redirectURL)
}

// printPasteInstruction tells a remote user what to paste: the browser on their own machine cannot
// reach this machine's loopback listener, so the page it lands on fails to load, but its address
// carries the code.
func (term loginTerminal) printPasteInstruction(redirectURL string) {
	_, _ = fmt.Fprintf(term.out, "After the login, paste the full address your browser lands on here "+
		"(it starts with %s; the page itself may fail to load).\n", redirectURL)
}

// answerLine handles one line of input and reports whether it ended the wait. A pasted redirect
// that carries another login's state — an address left over from an earlier attempt — is answered
// like any other stray line, so it cannot end this login.
func (term loginTerminal) answerLine(
	line, authorizeURL, redirectURL string,
	state *loginState,
	isLocalDesktop bool,
) (loginResult, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		if isLocalDesktop {
			term.openURL(authorizeURL)
		} else {
			term.printPasteInstruction(redirectURL)
		}
		return loginResult{}, false
	}

	pasted, err := url.Parse(line)
	if err == nil {
		if result, isRedirect := redirectResult(pasted.Query()); isRedirect {
			if state.matches(result.state) {
				return result, true
			}
			_, _ = fmt.Fprintln(term.out, "That address belongs to a different login attempt; paste the "+
				"address this login redirected to.")
			return loginResult{}, false
		}
	}
	_, _ = fmt.Fprintf(term.out, "That is not the address the login redirected to; paste the full URL, "+
		"starting with %s.\n", redirectURL)
	return loginResult{}, false
}

// openURL hands the authorize URL to the platform's browser opener. The URL comes from the
// authorization server's metadata, which apogee does not trust, so only an https or loopback http
// address is ever opened, and the opener program is resolved through the exec fence like every
// other program apogee runs. Every failure is a one-line note: the URL is already on screen.
func (term loginTerminal) openURL(authorizeURL string) {
	if !isOpenableURL(authorizeURL) {
		_, _ = fmt.Fprintln(term.out, "Not opening this URL automatically: only https and loopback http "+
			"addresses are opened. Copy it into a browser yourself.")
		return
	}
	argv := urlOpenerArgv(term.goos, authorizeURL)
	program, err := security.ResolveProgram(term.look, argv[0], term.workspace, nil)
	if errors.Is(err, security.ErrExecFromWritablePath) {
		_, _ = fmt.Fprintf(term.out, "Not opening the browser: %v\n", err)
		return
	}
	if err != nil {
		_, _ = fmt.Fprintf(term.out, "No browser opener (%s) on this machine; copy the URL into a browser yourself.\n", argv[0])
		return
	}
	if err := term.launch(program, argv[1:]...); err != nil {
		_, _ = fmt.Fprintf(term.out, "Could not open the browser (%v); copy the URL into a browser yourself.\n", err)
		return
	}
	_, _ = fmt.Fprintln(term.out, "Opened your browser; finish the login there.")
}

// readLines starts reading the user's input one line at a time and returns the lines and the stop
// that cancels the read, joins it and closes the reader. When no input can be opened the wait runs
// on the callback alone, so the returned channel is nil.
func (term loginTerminal) readLines() (<-chan string, func()) {
	reader, err := term.openInput()
	if err != nil {
		_, _ = fmt.Fprintf(term.out, "Pasting is unavailable (%v); waiting for the browser.\n", err)
		return nil, func() {}
	}

	lines := make(chan string)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(lines)
		scanner := bufio.NewScanner(reader)
		scanner.Split(splitTerminalLines())
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-stop:
				return
			}
		}
	}()

	return lines, func() {
		close(stop)
		// A reader that cannot interrupt a blocked read (cancelreader's fallback) is not joined:
		// waiting on it would hold the login until the user typed another line.
		if reader.Cancel() {
			<-done
		}
		// Close releases only the reader's own cancel plumbing, never the terminal itself, so its
		// error leaves nothing to do.
		_ = reader.Close()
	}
}

// splitTerminalLines splits input on "\n", "\r" or "\r\n": a console in raw mode ends a line with
// a lone carriage return, and a pasted Windows line ends with both, which must not read as an
// extra empty line (an empty line opens the browser).
func splitTerminalLines() bufio.SplitFunc {
	isAfterCarriageReturn := false
	return func(data []byte, atEOF bool) (int, []byte, error) {
		// The skipped "\n" is consumed together with the token after it: bufio.Scanner reads more
		// input after an advance that yields no token, so returning it alone would stall a line
		// that is already buffered.
		skip := 0
		if isAfterCarriageReturn && len(data) > 0 && data[0] == '\n' {
			skip = 1
		}
		rest := data[skip:]
		if end := bytes.IndexAny(rest, "\r\n"); end >= 0 {
			isAfterCarriageReturn = rest[end] == '\r'
			return skip + end + 1, rest[:end], nil
		}
		if atEOF && len(rest) > 0 {
			isAfterCarriageReturn = false
			return len(data), rest, nil
		}
		return 0, nil, nil
	}
}

// callbackHandler answers the authorization server's redirect: it tells the browser to return to
// the terminal and then hands the first authorization response carrying the login's state to the
// waiting fetch. Any other request — not an authorization response, or one for another login,
// which any local process or web page could send — is refused and ends nothing.
func callbackHandler(state *loginState, results chan<- loginResult) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, isRedirect := redirectResult(r.URL.Query())
		if !isRedirect {
			http.Error(w, "apogee: this is not an authorization response.", http.StatusBadRequest)
			return
		}
		if !state.matches(result.state) {
			http.Error(w, "apogee: this authorization response is not for the login in progress.",
				http.StatusBadRequest)
			return
		}
		page := loginReceivedPage
		if result.err != nil {
			page = loginFailedPage
		}
		// The page goes out whole before the fetch hears of the response: the fetch closes the
		// listener as soon as it returns, and a page still buffered then would reach the browser
		// as a reset connection instead.
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(page)))
		_, _ = io.WriteString(w, page)
		// A failed flush leaves the page to the server's own end-of-request write, which the
		// graceful shutdown in closeListener still waits for.
		_ = http.NewResponseController(w).Flush()
		deliver(results, result)
	}
}

// redirectResult reads an authorization response out of a redirect's query: an `error` (with its
// description, escape sequences stripped, since the server is untrusted and the text is printed),
// or a code; either with its state, which RFC 6749 §4.1.2 and §4.1.2.1 require on both. Anything
// else is not an authorization response.
func redirectResult(query url.Values) (loginResult, bool) {
	if refusal := query.Get("error"); refusal != "" {
		detail := sanitize.StripEscapesToLine(refusal)
		if description := query.Get("error_description"); description != "" {
			detail += ": " + sanitize.StripEscapesToLine(description)
		}
		return loginResult{state: query.Get("state"), err: fmt.Errorf("%w: %s", errLoginRefused, detail)}, true
	}
	code, state := query.Get("code"), query.Get("state")
	if code == "" || state == "" {
		return loginResult{}, false
	}
	return loginResult{code: code, state: state}, true
}

// loginState is the state of the login a fetch is waiting on, read from the authorize URL once the
// fetch has it. A response is accepted only when it carries that state, so until the fetch starts
// none is.
type loginState struct {
	mu   sync.Mutex
	want string
}

// expect records the state the login in progress sent with its authorization request.
func (s *loginState) expect(want string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.want = want
}

// matches reports whether got is the state of the login in progress.
func (s *loginState) matches(got string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.want != "" && got == s.want
}

// authorizeState returns the state an authorize URL carries: the value an authorization response
// must echo for the fetch to accept it.
func authorizeState(authorizeURL string) (string, error) {
	parsed, err := url.Parse(authorizeURL)
	if err != nil {
		return "", fmt.Errorf("read the authorize URL: %w", err)
	}
	state := parsed.Query().Get("state")
	if state == "" {
		return "", errors.New("the authorize URL carries no state")
	}
	return state, nil
}

// deliver hands result to the waiting fetch unless an earlier one already got there: the first
// authorization response for this login wins.
func deliver(results chan<- loginResult, result loginResult) {
	select {
	case results <- result:
	default:
	}
}

// isOpenableURL reports whether rawURL may be handed to the browser opener: an https address, or an
// http one on a loopback host, written in printable ASCII with no space or quote, so the opener's
// command line carries exactly one URL and nothing an opener could read as more.
func isOpenableURL(rawURL string) bool {
	for i := 0; i < len(rawURL); i++ {
		if c := rawURL[i]; c <= ' ' || c > '~' || c == '"' {
			return false
		}
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return false
	}
	switch parsed.Scheme {
	case "https":
		return true
	case "http":
		host := parsed.Hostname()
		ip := net.ParseIP(host)
		return host == "localhost" || (ip != nil && ip.IsLoopback())
	default:
		return false
	}
}

// urlOpenerArgv is the platform's command line for opening a URL in the default browser. Windows
// uses url.dll's protocol handler rather than `cmd /c start`, because cmd.exe would read the `&`
// every authorize URL carries as its own syntax.
func urlOpenerArgv(goos, rawURL string) []string {
	switch goos {
	case "darwin":
		return []string{"open", rawURL}
	case "windows":
		return []string{"rundll32", "url.dll,FileProtocolHandler", rawURL}
	default:
		return []string{"xdg-open", rawURL}
	}
}

// startDetached starts the resolved opener with its standard streams on the null device, so
// nothing it prints lands in the prompt, and reaps it in the background: an opener hands the URL
// to the browser and exits, and the login does not wait on it.
func startDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		// The opener has already handed the URL over; its exit status changes nothing, and the
		// URL stays on screen for the user to open by hand.
		_ = cmd.Wait()
	}()
	return nil
}
