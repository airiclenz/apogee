package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/muesli/cancelreader"
	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee/internal/adoption"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/mcpauth"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/testtime"
)

const (
	fakeAuthCode    = "the-code"
	fakeAccessToken = "access-token"
)

// fakeOAuthMCP is an OAuth-protected MCP server and its authorization server on two loopback
// origins. The MCP endpoint answers a request without the issued bearer with a 401 pointing at its
// protected-resource metadata; the authorization server registers any client and exchanges the
// one code it accepts. sequence, shared between fakes, stamps each fake's first exchange and first
// authorized MCP request, so a test can tell the order logins and connects happened in.
type fakeOAuthMCP struct {
	resource *httptest.Server
	authSrv  *httptest.Server
	sequence *atomic.Int64
	// rejectRefresh makes the token endpoint refuse every refresh grant with invalid_grant.
	rejectRefresh bool

	exchanges     atomic.Int64
	exchangedAt   atomic.Int64
	firstServedAt atomic.Int64
}

// newFakeOAuthMCP starts both servers, closed when the test ends.
func newFakeOAuthMCP(t *testing.T, sequence *atomic.Int64) *fakeOAuthMCP {
	t.Helper()
	f := &fakeOAuthMCP{sequence: sequence}
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "oauth", Version: "v0.0.1"}, nil)
	server.AddTool(&mcpsdk.Tool{Name: "ping", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "pong"}}}, nil
		})
	mcpHandler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, nil)
	f.resource = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.serveResource(w, r, mcpHandler)
	}))
	f.authSrv = httptest.NewServer(http.HandlerFunc(f.serveAuth))
	t.Cleanup(func() {
		f.resource.CloseClientConnections()
		f.resource.Close()
		f.authSrv.Close()
	})
	return f
}

// endpoint is the MCP endpoint's URL.
func (f *fakeOAuthMCP) endpoint() string { return f.resource.URL + "/mcp" }

// serveResource is the MCP server: its protected-resource metadata, and the endpoint behind the
// bearer check.
func (f *fakeOAuthMCP) serveResource(w http.ResponseWriter, r *http.Request, next http.Handler) {
	metadataPath := "/.well-known/oauth-protected-resource/mcp"
	if r.URL.Path == metadataPath {
		writeFakeJSON(w, http.StatusOK, oauthex.ProtectedResourceMetadata{
			Resource:             f.endpoint(),
			AuthorizationServers: []string{f.authSrv.URL},
		})
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+fakeAccessToken {
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata=%q`, f.resource.URL+metadataPath))
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	f.firstServedAt.CompareAndSwap(0, f.sequence.Add(1))
	next.ServeHTTP(w, r)
}

// serveAuth is the authorization server: RFC 8414 metadata, registration and the token endpoint.
func (f *fakeOAuthMCP) serveAuth(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/oauth-authorization-server":
		writeFakeJSON(w, http.StatusOK, oauthex.AuthServerMeta{
			Issuer:                            f.authSrv.URL,
			AuthorizationEndpoint:             f.authSrv.URL + "/authorize",
			TokenEndpoint:                     f.authSrv.URL + "/token",
			RegistrationEndpoint:              f.authSrv.URL + "/register",
			CodeChallengeMethodsSupported:     []string{"S256"},
			TokenEndpointAuthMethodsSupported: []string{"none"},
		})
	case "/register":
		var requested oauthex.ClientRegistrationMetadata
		if err := json.NewDecoder(r.Body).Decode(&requested); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeFakeJSON(w, http.StatusCreated, map[string]any{
			"client_id":                  "dcr-client",
			"redirect_uris":              requested.RedirectURIs,
			"token_endpoint_auth_method": "none",
		})
	case "/token":
		f.serveToken(w, r)
	default:
		http.NotFound(w, r)
	}
}

// serveToken exchanges the accepted code for the bearer the MCP endpoint accepts.
func (f *fakeOAuthMCP) serveToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if r.PostForm.Get("grant_type") == "refresh_token" && f.rejectRefresh {
		writeFakeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}
	if r.PostForm.Get("code") != fakeAuthCode {
		writeFakeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}
	f.exchanges.Add(1)
	f.exchangedAt.CompareAndSwap(0, f.sequence.Add(1))
	writeFakeJSON(w, http.StatusOK, map[string]any{
		"access_token":  fakeAccessToken,
		"refresh_token": "refresh-token",
		"token_type":    "Bearer",
		"expires_in":    3600,
	})
}

// writeFakeJSON answers status with v as an application/json body.
func writeFakeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// serverConfig is an `auth: oauth` streamable-http entry for the fake under name.
func (f *fakeOAuthMCP) serverConfig(name string) mcp.ServerConfig {
	return mcp.ServerConfig{Name: name, Transport: mcp.TransportStreamableHTTP, Endpoint: f.endpoint(), Auth: mcp.AuthOAuth}
}

// scriptedInput is the terminal across several reads: each open returns a fresh cancellable
// input that types the next script's lines, and an open past the scripts types nothing.
type scriptedInput struct {
	mu      sync.Mutex
	scripts [][]string
	opens   int
}

// open is the loginTerminal's openInput.
func (s *scriptedInput) open() (cancelreader.CancelReader, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	input := newPipeInput()
	if s.opens < len(s.scripts) {
		input.typeLines(s.scripts[s.opens]...)
	}
	s.opens++
	return input, nil
}

// openCount is how many times the terminal was opened for reading.
func (s *scriptedInput) openCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.opens
}

// browser stands in for the opener a local desktop runs on Enter: it follows the authorize URL as a
// user who approved the login would, sending the browser back to the redirect URI with the code.
func browser() func(name string, args ...string) error {
	return func(_ string, args ...string) error {
		authorizeURL, err := url.Parse(args[len(args)-1])
		if err != nil {
			return err
		}
		query := authorizeURL.Query()
		callback := query.Get("redirect_uri") + "?" + url.Values{
			"code":  {fakeAuthCode},
			"state": {query.Get("state")},
		}.Encode()
		// The answer page is not awaited: the fetch closes the listener as soon as the code is
		// delivered, which may cut the page off; the login is complete either way.
		go func() {
			if response, err := http.Get(callback); err == nil {
				_ = response.Body.Close()
			}
		}()
		return nil
	}
}

// loopbackAuthClient reaches the loopback test servers; the real authorization-server client
// refuses loopback by design.
func loopbackAuthClient(security.URLGuard, func(*http.Request) (*url.URL, error)) *http.Client {
	return &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// testMCPLoginDeps is a login on a local linux desktop whose terminal reads input and writes out,
// whose opener is the scripted browser, and whose stdin reports isTerminal.
func testMCPLoginDeps(t *testing.T, input *scriptedInput, out *bytes.Buffer, isTerminal bool) mcpLoginDeps {
	t.Helper()
	return mcpLoginDeps{
		terminal: loginTerminal{
			out:       &syncWriter{buffer: out},
			openInput: input.open,
			goos:      "linux",
			getenv:    func(name string) string { return map[string]string{"DISPLAY": ":0"}[name] },
			look:      func(name string) (string, error) { return "/usr/bin/" + name, nil },
			launch:    browser(),
		},
		isTerminal:    func() bool { return isTerminal },
		newAuthClient: loopbackAuthClient,
	}
}

// syncWriter serialises writes into a buffer the test reads after the run.
type syncWriter struct {
	mu     sync.Mutex
	buffer *bytes.Buffer
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.Write(p)
}

// writeMCPConfig writes a config.yaml under home whose `mcp-servers:` block is servers, beside the
// one LLM server entry every config needs.
func writeMCPConfig(t *testing.T, home string, servers ...mcp.ServerConfig) {
	t.Helper()
	var body strings.Builder
	body.WriteString("servers:\n  - name: box\n    endpoint: http://127.0.0.1:1111\n    model: fake\nserver: box\n")
	body.WriteString("mcp-servers:\n")
	for _, server := range servers {
		fmt.Fprintf(&body, "  - name: %s\n    transport: %s\n    endpoint: %s\n", server.Name, server.Transport, server.Endpoint)
		if server.Auth != "" {
			fmt.Fprintf(&body, "    auth: %s\n", server.Auth)
		}
	}
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(body.String()), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
}

// runMCPCommand runs `apogee mcp <args>` over deps and returns its stdout and error.
func runMCPCommand(t *testing.T, deps mcpLoginDeps, args ...string) (string, error) {
	t.Helper()
	cmd := newMCPCommandWith(deps)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	ctx, cancel := context.WithTimeout(context.Background(), testtime.Budget(30*time.Second))
	defer cancel()
	err := cmd.ExecuteContext(ctx)
	return stdout.String(), err
}

// requireStored fails unless home's token store holds a record for server at endpoint.
func requireStored(t *testing.T, home, server, endpoint string, want bool) {
	t.Helper()
	record, isStored, err := mcpauth.NewStore(home).Load(server, endpoint)
	if err != nil {
		t.Fatalf("Load(%q): %v", server, err)
	}
	if isStored != want {
		t.Fatalf("a record for %q is stored = %v, want %v", server, isStored, want)
	}
	if want && record.AccessToken != fakeAccessToken {
		t.Errorf("stored access token = %q, want %q", record.AccessToken, fakeAccessToken)
	}
}

// `apogee mcp login <name>` logs in through the terminal fetcher and saves the record under the
// --config home; `apogee mcp logout <name>` deletes it and says so, and says when there was none.
func TestMCPCmdLoginSavesTheRecordAndLogoutDeletesIt(t *testing.T) {
	t.Parallel()
	fake := newFakeOAuthMCP(t, &atomic.Int64{})
	home := t.TempDir()
	writeMCPConfig(t, home, fake.serverConfig("remote"))
	input := &scriptedInput{scripts: [][]string{{""}}}
	deps := testMCPLoginDeps(t, input, &bytes.Buffer{}, true)

	stdout, err := runMCPCommand(t, deps, "login", "remote", "--config", home)

	if err != nil {
		t.Fatalf("mcp login: %v", err)
	}
	if !strings.Contains(stdout, `Logged in to MCP server "remote"`) {
		t.Errorf("mcp login stdout = %q; want the login confirmed", stdout)
	}
	requireStored(t, home, "remote", fake.endpoint(), true)

	stdout, err = runMCPCommand(t, deps, "logout", "remote", "--config", home)
	if err != nil {
		t.Fatalf("mcp logout: %v", err)
	}
	if !strings.Contains(stdout, "saved token was deleted") {
		t.Errorf("mcp logout stdout = %q; want the deletion reported", stdout)
	}
	requireStored(t, home, "remote", fake.endpoint(), false)

	stdout, err = runMCPCommand(t, deps, "logout", "remote", "--config", home)
	if err != nil {
		t.Fatalf("second mcp logout: %v", err)
	}
	if !strings.Contains(stdout, "had no saved token") {
		t.Errorf("second mcp logout stdout = %q; want it to say there was nothing to delete", stdout)
	}
}

// Both verbs refuse a name config.yaml does not configure and a server without `auth: oauth`,
// before anything is read from the terminal or sent over the network.
func TestMCPCmdRefusesAnUnknownOrNonOAuthServer(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	writeMCPConfig(t, home, mcp.ServerConfig{
		Name: "plain", Transport: mcp.TransportStreamableHTTP, Endpoint: "https://mcp.example.com/mcp",
	})
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "login of an unknown name", args: []string{"login", "missing"}, want: `no MCP server named "missing"`},
		{name: "logout of an unknown name", args: []string{"logout", "missing"}, want: `no MCP server named "missing"`},
		{name: "login of a server without oauth", args: []string{"login", "plain"}, want: "is not configured with auth: oauth"},
		{name: "logout of a server without oauth", args: []string{"logout", "plain"}, want: "is not configured with auth: oauth"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := &scriptedInput{}
			deps := testMCPLoginDeps(t, input, &bytes.Buffer{}, true)

			_, err := runMCPCommand(t, deps, append(tt.args, "--config", home)...)

			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("mcp %v error = %v; want it to contain %q", tt.args, err, tt.want)
			}
			if input.openCount() != 0 {
				t.Errorf("a refused verb read the terminal %d times; want none", input.openCount())
			}
		})
	}
}

// With no --config flag, login reads config.yaml from APOGEE_CONFIG's home and saves the record
// there — the precedence every other command honours. Not parallel: it sets the environment.
func TestMCPCmdLoginHonoursTheAPOGEE_CONFIGHome(t *testing.T) {
	fake := newFakeOAuthMCP(t, &atomic.Int64{})
	home := t.TempDir()
	writeMCPConfig(t, home, fake.serverConfig("remote"))
	t.Setenv(config.EnvConfig, home)
	input := &scriptedInput{scripts: [][]string{{""}}}

	_, err := runMCPCommand(t, testMCPLoginDeps(t, input, &bytes.Buffer{}, true), "login", "remote")

	if err != nil {
		t.Fatalf("mcp login: %v", err)
	}
	requireStored(t, home, "remote", fake.endpoint(), true)
}

// The registration seam carries `mcp` with both verbs.
func TestSubcommandsRegistersMCP(t *testing.T) {
	t.Parallel()
	root := newRootCommand((&recordingLauncher{}).launch, subcommands()...)

	var mcpCommand *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "mcp" {
			mcpCommand = c
		}
	}
	if mcpCommand == nil {
		t.Fatal("the shipped subcommand set does not register `mcp`")
	}
	children := map[string]bool{}
	for _, c := range mcpCommand.Commands() {
		children[c.Name()] = true
	}
	for _, want := range []string{"list", "login", "logout"} {
		if !children[want] {
			t.Errorf("`mcp` does not register the %q child; has %v", want, children)
		}
	}
}

// ----------------------------------------------------------------------------
// Repo servers: `apogee mcp list`, and login refusing an unadopted one (ADR 0104 §12)
// ----------------------------------------------------------------------------

// projectMCPServersYAML is a Project config proposing three repo servers, one per transport.
const projectMCPServersYAML = "mcp-servers:\n" +
	"  - name: tracker\n    transport: streamable-http\n    endpoint: https://tracker.example.com/mcp\n    auth: oauth\n" +
	"  - name: docs\n    transport: sse\n    endpoint: https://docs.example.com/sse\n" +
	"  - name: lint\n    transport: stdio\n    command: lint-server\n"

// rejectProposalsNamed records as rejected each entry the layered config of the apogee home and
// projectRoot proposes whose handle is among handles — what `apogee project adopt` writes for an
// r — and fails the test when one of handles is not proposed.
func rejectProposalsNamed(t *testing.T, home, projectRoot string, handles ...string) {
	t.Helper()
	store, err := adoption.New(config.WorkspacesDir(home), projectRoot)
	if err != nil {
		t.Fatalf("open the adoption record: %v", err)
	}
	proposed := layeredProposals(t, home, projectRoot).Proposed
	for _, handle := range handles {
		isFound := false
		for _, p := range proposed {
			if p.Handle != handle {
				continue
			}
			isFound = true
			if err := store.Reject(adoption.Entry{Kind: string(p.Kind), Handle: p.Handle, Text: p.Text}); err != nil {
				t.Fatalf("Reject %s: %v", handle, err)
			}
		}
		if !isFound {
			t.Fatalf("the Project config proposes no entry %q: %+v", handle, proposed)
		}
	}
}

// listRow is the fields of the `apogee mcp list` line naming server, or nil when no line does.
func listRow(out, server string) []string {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == server {
			return fields
		}
	}
	return nil
}

// Every configured server is listed with its transport and source, and a repo server with where
// the user's answers left it: adopted, proposed or rejected.
func TestMCPListShowsSourceAndState(t *testing.T) {
	t.Parallel()
	home, workspace := t.TempDir(), t.TempDir()
	writeMCPConfig(t, home, mcp.ServerConfig{
		Name: "plain", Transport: mcp.TransportStreamableHTTP, Endpoint: "https://mcp.example.com/mcp",
	})
	writeProjectConfig(t, workspace, projectMCPServersYAML)
	adoptProposalsNamed(t, home, workspace, "tracker")
	rejectProposalsNamed(t, home, workspace, "lint")

	out, err := runMCPCommand(t, mcpLoginDeps{}, "list", "--config", home, "--workspace", workspace)

	if err != nil {
		t.Fatalf("mcp list: %v\n%s", err, out)
	}
	want := map[string][]string{
		"plain":   {"plain", "streamable-http", "global", "-"},
		"tracker": {"tracker", "streamable-http", "project", "adopted"},
		"docs":    {"docs", "sse", "project", "proposed"},
		"lint":    {"lint", "stdio", "project", "rejected"},
	}
	for name, fields := range want {
		if got := listRow(out, name); strings.Join(got, " ") != strings.Join(fields, " ") {
			t.Errorf("the %s row = %q; want %q\n%s", name, got, fields, out)
		}
	}
	if got := listRow(out, "NAME"); strings.Join(got, " ") != "NAME TRANSPORT SOURCE STATE" {
		t.Errorf("the header = %q\n%s", got, out)
	}
}

// A config that names no LLM server yet still lists its MCP servers: the list connects to nothing
// and needs no startup server, as `apogee project adopt` needs none.
func TestMCPListNeedsNoStartupServer(t *testing.T) {
	t.Parallel()
	home, workspace := projectAdoptRoots(t)
	writeProjectConfig(t, workspace, projectMCPServersYAML)

	out, err := runMCPCommand(t, mcpLoginDeps{}, "list", "--config", home, "--workspace", workspace)

	if err != nil {
		t.Fatalf("mcp list with no startup server: %v\n%s", err, out)
	}
	if got := listRow(out, "docs"); strings.Join(got, " ") != "docs sse project proposed" {
		t.Errorf("the docs row = %q; want it listed as proposed\n%s", got, out)
	}
}

// With no server anywhere the list says so rather than printing a bare header.
func TestMCPListSaysWhenNoServerIsConfigured(t *testing.T) {
	t.Parallel()
	home, workspace := projectAdoptRoots(t)

	out, err := runMCPCommand(t, mcpLoginDeps{}, "list", "--config", home, "--workspace", workspace)

	if err != nil {
		t.Fatalf("mcp list: %v", err)
	}
	if out != "No MCP servers are configured.\n" {
		t.Errorf("mcp list printed %q; want the no-servers line", out)
	}
}

// A login to a repo server still proposed is refused before anything is read or dialled, naming
// the server and `apogee project adopt`.
func TestMCPLoginRefusesAProposedRepoServer(t *testing.T) {
	t.Parallel()
	home, workspace := t.TempDir(), t.TempDir()
	writeMCPConfig(t, home, mcp.ServerConfig{
		Name: "plain", Transport: mcp.TransportStreamableHTTP, Endpoint: "https://mcp.example.com/mcp",
	})
	writeProjectConfig(t, workspace, projectMCPServersYAML)
	input := &scriptedInput{}

	_, err := runMCPCommand(t, testMCPLoginDeps(t, input, &bytes.Buffer{}, true),
		"login", "tracker", "--config", home, "--workspace", workspace)

	want := `apogee mcp: MCP server "tracker" is proposed by the project config ` +
		config.ProjectFilePath(workspace) + " and not adopted on this machine, so it has no login — " +
		"run `apogee project adopt` to adopt it first"
	if err == nil || err.Error() != want {
		t.Fatalf("mcp login of a proposed repo server: error = %v; want %q", err, want)
	}
	if input.openCount() != 0 {
		t.Errorf("a refused login read the terminal %d times; want none", input.openCount())
	}
	requireStored(t, home, "tracker", "https://tracker.example.com/mcp", false)
}

// A login to a rejected repo server is refused naming /settings — `apogee project adopt` no longer
// asks about a rejected entry — and never that command.
func TestMCPLoginRefusesARejectedRepoServerNamingSettings(t *testing.T) {
	t.Parallel()
	home, workspace := t.TempDir(), t.TempDir()
	writeMCPConfig(t, home, mcp.ServerConfig{
		Name: "plain", Transport: mcp.TransportStreamableHTTP, Endpoint: "https://mcp.example.com/mcp",
	})
	writeProjectConfig(t, workspace, projectMCPServersYAML)
	rejectProposalsNamed(t, home, workspace, "tracker")
	input := &scriptedInput{}

	_, err := runMCPCommand(t, testMCPLoginDeps(t, input, &bytes.Buffer{}, true),
		"login", "tracker", "--config", home, "--workspace", workspace)

	want := `apogee mcp: MCP server "tracker" from the project config ` + config.ProjectFilePath(workspace) +
		" was rejected on this machine, so it has no login — adopt it from its `mcp-servers` row in /settings first"
	if err == nil || err.Error() != want {
		t.Fatalf("mcp login of a rejected repo server: error = %v; want %q", err, want)
	}
	if strings.Contains(err.Error(), "apogee project adopt") {
		t.Errorf("the rejected server's refusal names `apogee project adopt`: %v", err)
	}
	if input.openCount() != 0 {
		t.Errorf("a refused login read the terminal %d times; want none", input.openCount())
	}
}

// startupWiring is the boot over servers with the login deps, wireSession not yet run.
func startupWiring(t *testing.T, deps mcpLoginDeps, servers ...mcp.ServerConfig) *rootWiring {
	t.Helper()
	return urlGuardWiringWith(t, config.Options{MCPServers: servers}, rootDeps{mcpLogin: deps})
}

// A startup that finds an `auth: oauth` server with no saved token aborts naming the command that
// logs in, without reading the terminal, when stdin is not a terminal or the offer is declined.
func TestStartupMCPLoginAbortsWhenItCannotOrMayNotAsk(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		isTerminal bool
		scripts    [][]string
		wantOpens  int
	}{
		{name: "stdin is not a terminal", isTerminal: false, wantOpens: 0},
		{name: "the offer is declined", isTerminal: true, scripts: [][]string{{"n"}}, wantOpens: 1},
		{name: "the offer is left at its default", isTerminal: true, scripts: [][]string{{""}}, wantOpens: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeOAuthMCP(t, &atomic.Int64{})
			input := &scriptedInput{scripts: tt.scripts}
			w := startupWiring(t, testMCPLoginDeps(t, input, &bytes.Buffer{}, tt.isTerminal), fake.serverConfig("remote"))

			err := w.wireSession(context.Background())

			if err == nil || !strings.Contains(err.Error(), "apogee mcp login remote") {
				t.Fatalf("wireSession error = %v; want the abort naming `apogee mcp login remote`", err)
			}
			if !errors.Is(err, mcpauth.ErrLoginRequired) {
				t.Errorf("wireSession error = %v; want it to wrap mcpauth.ErrLoginRequired", err)
			}
			if input.openCount() != tt.wantOpens {
				t.Errorf("the terminal was opened %d times; want %d", input.openCount(), tt.wantOpens)
			}
			if fake.exchanges.Load() != 0 {
				t.Error("a login ran although none was agreed to")
			}
		})
	}
}

// A yes at the startup offer runs the login, saves the record under the session's home, and the
// connect that follows succeeds with the server's tools.
func TestStartupMCPLoginYesLogsInThenConnects(t *testing.T) {
	t.Parallel()
	fake := newFakeOAuthMCP(t, &atomic.Int64{})
	input := &scriptedInput{scripts: [][]string{{"y"}, {""}}}
	var out bytes.Buffer
	w := startupWiring(t, testMCPLoginDeps(t, input, &out, true), fake.serverConfig("remote"))

	err := w.wireSession(context.Background())

	if err != nil {
		t.Fatalf("wireSession: %v\nterminal:\n%s", err, out.String())
	}
	requireStored(t, w.roots.config, "remote", fake.endpoint(), true)
	if !hasMCPTool(w, "ping") {
		t.Error("the connected session does not carry the server's tools")
	}
	if !strings.Contains(out.String(), `MCP server "remote" needs an OAuth login. Log in now? [y/N]`) {
		t.Errorf("terminal = %q; want the offer asked on it", out.String())
	}
}

// Two `auth: oauth` servers with no saved token are both offered, and both logins finish, before
// the connect sends either of them a request.
func TestStartupMCPLoginOffersEveryServerBeforeTheConnect(t *testing.T) {
	t.Parallel()
	sequence := &atomic.Int64{}
	first := newFakeOAuthMCP(t, sequence)
	second := newFakeOAuthMCP(t, sequence)
	input := &scriptedInput{scripts: [][]string{{"y"}, {""}, {"yes"}, {""}}}
	var out bytes.Buffer
	w := startupWiring(t, testMCPLoginDeps(t, input, &out, true), first.serverConfig("first"), second.serverConfig("second"))

	err := w.wireSession(context.Background())

	if err != nil {
		t.Fatalf("wireSession: %v\nterminal:\n%s", err, out.String())
	}
	lastLogin := max(first.exchangedAt.Load(), second.exchangedAt.Load())
	firstConnect := min(first.firstServedAt.Load(), second.firstServedAt.Load())
	if first.exchangedAt.Load() == 0 || second.exchangedAt.Load() == 0 {
		t.Fatal("a server was not logged in to")
	}
	if firstConnect == 0 || firstConnect < lastLogin {
		t.Errorf("the connect reached a server (step %d) before both logins finished (step %d)", firstConnect, lastLogin)
	}
}

// A saved token whose refresh the authorization server rejects fails the connect for want of a
// login; the startup offers that server one and connects again.
func TestStartupMCPLoginRetriesAConnectWhoseRefreshWasRejected(t *testing.T) {
	t.Parallel()
	fake := newFakeOAuthMCP(t, &atomic.Int64{})
	fake.rejectRefresh = true
	input := &scriptedInput{scripts: [][]string{{"y"}, {""}}}
	var out bytes.Buffer
	w := startupWiring(t, testMCPLoginDeps(t, input, &out, true), fake.serverConfig("remote"))
	if err := mcpauth.NewStore(w.roots.config).Save("remote", fake.endpoint(), mcpauth.Record{
		AccessToken:   "expired-token",
		RefreshToken:  "spent-refresh-token",
		TokenType:     "Bearer",
		Expiry:        time.Now().Add(-time.Minute),
		TokenEndpoint: fake.authSrv.URL + "/token",
		Resource:      fake.endpoint(),
		Client:        mcpauth.Client{ID: "dcr-client", TokenEndpointAuthMethod: "none"},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	err := w.wireSession(context.Background())

	if err != nil {
		t.Fatalf("wireSession: %v\nterminal:\n%s", err, out.String())
	}
	if fake.exchanges.Load() != 1 {
		t.Errorf("logins = %d; want exactly one, after the rejected refresh", fake.exchanges.Load())
	}
	if !hasMCPTool(w, "ping") {
		t.Error("the connect after the login does not carry the server's tools")
	}
}

// A mid-session reconnect to a server with no saved token never prompts: it fails on the settings
// row with the sentence naming `apogee mcp login <name>`, and the terminal is never read.
func TestStartupMCPLoginReconnectNeverPrompts(t *testing.T) {
	t.Parallel()
	fake := newFakeOAuthMCP(t, &atomic.Int64{})
	input := &scriptedInput{scripts: [][]string{{"y"}, {""}}}
	w := startupWiring(t, testMCPLoginDeps(t, input, &bytes.Buffer{}, true))
	if err := w.wireSession(context.Background()); err != nil {
		t.Fatalf("wireSession: %v", err)
	}

	_, err := w.mcpSet.reconnect([]mcp.ServerConfig{fake.serverConfig("remote")}, w.toolSet, &applySettingSpy{})

	if err == nil {
		t.Fatal("reconnect with no saved token: want the login-required failure, got none")
	}
	if !strings.HasPrefix(err.Error(), "reconnect failed: ") || !strings.Contains(err.Error(), "apogee mcp login remote") {
		t.Errorf("reconnect error = %q; want the row's sentence naming `apogee mcp login remote`", err)
	}
	if input.openCount() != 0 {
		t.Errorf("the reconnect read the terminal %d times; want never", input.openCount())
	}
}

// repoWikiServerYAML is a Project config proposing one stdio server whose program is a script inside
// the repository, so its adoption pins that script's content.
const repoWikiServerYAML = "mcp-servers:\n  - name: wiki\n    transport: stdio\n    command: ./bin/wiki.sh\n"

// writeRepoWikiScript writes the repo server's program under root with body. The pin check refuses
// a moved script before anything is launched, so the script never runs.
func writeRepoWikiScript(t *testing.T, root, body string) {
	t.Helper()
	path := filepath.Join(root, "bin", "wiki.sh")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create bin/: %v", err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write bin/wiki.sh: %v", err)
	}
}

// adoptedRepoWikiServer proposes the repo wiki server in w's Project root, adopts it, and returns it
// as the layered config then holds it in force — Project, with its pin over the script as it is now.
func adoptedRepoWikiServer(t *testing.T, w *rootWiring) mcp.ServerConfig {
	t.Helper()
	writeRepoWikiScript(t, w.roots.project, "exec true")
	writeProjectConfig(t, w.roots.project, repoWikiServerYAML)
	adoptEveryProposal(t, w.configPath(), w.roots.project)
	o, err := w.layeredOptions()
	if err != nil {
		t.Fatalf("load the layered config: %v", err)
	}
	for _, server := range o.MCPServers {
		if server.Name == "wiki" && server.Project && server.Pin != nil {
			return server
		}
	}
	t.Fatalf("mcp-servers = %+v, want the adopted repo server wiki with its pin", o.MCPServers)
	return mcp.ServerConfig{}
}

// newPingMCPServer starts an MCP server over streamable HTTP advertising one `ping` tool, closed
// when the test ends, and returns its config under name: the global server a pin skip must spare.
func newPingMCPServer(t *testing.T, name string) mcp.ServerConfig {
	t.Helper()
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: name, Version: "v0.0.1"}, nil)
	server.AddTool(&mcpsdk.Tool{Name: "ping", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "pong"}}}, nil
		})
	srv := httptest.NewServer(mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, nil))
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	return mcp.ServerConfig{Name: name, Transport: mcp.TransportStreamableHTTP, Endpoint: srv.URL + "/mcp"}
}

// awaitPinWake reports whether w's config wait returns true within the test deadline — the re-read
// cue a pin skip gives, since no watched config file or adoption record moves in these tests.
func awaitPinWake(t *testing.T, w *rootWiring) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), configWatchTestDeadline)
	defer cancel()
	return configHost{w: w}.AwaitConfigChange(ctx)
}

// A repo server whose pinned script is edited after the options are resolved and before the startup
// dial is skipped, not fatal: the launch goes on with the global server connected, a startup notice
// names the server and the edited file, and the config wait wakes so the entry is asked about again.
func TestStartupSkipsARepoServerWhosePinMoved(t *testing.T) {
	t.Parallel()
	global := newPingMCPServer(t, "remote")
	w := startupWiring(t, testMCPLoginDeps(t, &scriptedInput{}, &bytes.Buffer{}, false), global)
	w.opts.MCPServers = append(w.opts.MCPServers, adoptedRepoWikiServer(t, w))
	writeRepoWikiScript(t, w.roots.project, "echo edited")

	err := w.wireSession(context.Background())

	if err != nil {
		t.Fatalf("wireSession: %v, want the launch to go on past the skipped repo server", err)
	}
	if !hasMCPTool(w, "ping") {
		t.Error("the global server's tools are missing; a pin skip must spare the rest of the set")
	}
	want := "apogee: mcp server wiki skipped — stdio server \"wiki\": refusing to launch — bin/wiki.sh changed since you adopted it"
	if len(w.startupNotices) != 1 || !strings.HasPrefix(w.startupNotices[0], want) {
		t.Errorf("startup notices = %q, want one opening %q", w.startupNotices, want)
	}
	if !awaitPinWake(t, w) {
		t.Error("AwaitConfigChange did not return true — the skip must wake the re-read with no watcher moved")
	}
}

// A live reconnect onto a set holding a repo server whose pinned script was edited after its options
// were resolved lands the rest of the set: the reconnect's note names the server and the edited file,
// the global server is connected, and the config wait wakes so the entry is asked about again.
func TestReconnectSkipsARepoServerWhosePinMoved(t *testing.T) {
	t.Parallel()
	global := newPingMCPServer(t, "remote")
	w := startupWiring(t, testMCPLoginDeps(t, &scriptedInput{}, &bytes.Buffer{}, false))
	// Adopted before the session starts its watchers, so neither the Project config nor the
	// adoption record is a change they report: only the skip can wake the wait below.
	servers := []mcp.ServerConfig{global, adoptedRepoWikiServer(t, w)}
	if err := w.wireSession(context.Background()); err != nil {
		t.Fatalf("wireSession: %v", err)
	}
	writeRepoWikiScript(t, w.roots.project, "echo edited")

	note, err := w.mcpSet.reconnect(servers, w.toolSet, &applySettingSpy{})

	if err != nil {
		t.Fatalf("reconnect: %v, want the rest of the set to land past the skipped repo server", err)
	}
	want := "mcp server wiki skipped — stdio server \"wiki\": refusing to launch — bin/wiki.sh changed since you adopted it"
	if !strings.HasPrefix(note, want) {
		t.Errorf("reconnect note = %q, want it opening %q", note, want)
	}
	if !hasMCPTool(w, "ping") {
		t.Error("the global server's tools are missing; a pin skip must spare the rest of the set")
	}
	if !awaitPinWake(t, w) {
		t.Error("AwaitConfigChange did not return true — the skip must wake the re-read with no watcher moved")
	}
}

// hasMCPTool reports whether the session's live MCP tools include one whose name contains name.
func hasMCPTool(w *rootWiring, name string) bool {
	for _, tool := range w.mcpSet.tools() {
		if strings.Contains(tool.Name(), name) {
			return true
		}
	}
	return false
}
