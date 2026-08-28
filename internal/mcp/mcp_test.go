package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/security"
)

// ----------------------------------------------------------------------------
// Hermetic stdio MCP server fixture (the fork-and-exec trick)
// ----------------------------------------------------------------------------
//
// The tests connect to a REAL MCP server over a REAL stdio transport — no network, no
// external binary, no separate build step. TestMain re-execs THIS test binary as the
// fixture server when runAsServerEnv is set; the stdio tests launch it via ServerConfig's
// Command pointing at os.Executable(). This exercises the whole live path Connect builds
// (buildTransport(stdio) → CommandTransport → ListTools → CallTool → Close) deterministically,
// so the suite is hermetic and runs everywhere `go test` does (the SDK's own stdio-test idiom).

const (
	runAsServerEnv       = "APOGEE_MCP_TEST_SERVER"
	runAsWedgedServerEnv = "APOGEE_MCP_TEST_WEDGED_SERVER"
)

// fixtureToolSchema is the input schema the fixture's echo tool advertises (a single string
// argument), so a surfaced tool carries a real, non-empty schema to assert against.
var fixtureToolSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"text": map[string]any{"type": "string", "description": "text to echo back"},
	},
	"required": []any{"text"},
}

// TestMain runs the fixture MCP server instead of the test suite when re-exec'd with
// runAsServerEnv set (the fork-and-exec trick), otherwise runs the tests normally.
func TestMain(m *testing.M) {
	if os.Getenv(runAsServerEnv) != "" {
		runFixtureServer()
		return
	}
	if os.Getenv(runAsWedgedServerEnv) != "" {
		runWedgedFixtureServer()
		return
	}
	os.Exit(m.Run())
}

// runFixtureServer serves the fixture MCP server over stdio until its peer disconnects. It
// exposes a single deterministic "echo" tool (and one that reports an error) so the tests
// can assert tool discovery, a successful call, and a server-side tool error round-trip.
func runFixtureServer() {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "apogee-test-fixture", Version: "v0.0.1"}, nil)

	server.AddTool(
		&mcpsdk.Tool{Name: "echo", Description: "Echo the text argument back.", InputSchema: fixtureToolSchema},
		func(_ context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			var args struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(req.Params.Arguments, &args)
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "echo: " + args.Text}},
			}, nil
		},
	)
	server.AddTool(
		&mcpsdk.Tool{Name: "boom", Description: "Always returns a tool error.", InputSchema: map[string]any{"type": "object"}},
		func(_ context.Context, _ *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "kaboom"}},
				IsError: true,
			}, nil
		},
	)

	server.AddTool(
		&mcpsdk.Tool{Name: "spawn", Description: "Start a long-lived descendant and report its pid.", InputSchema: map[string]any{"type": "object"}},
		func(_ context.Context, _ *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			// A descendant in the SERVER'S OWN process group — not detached — which is what the
			// group teardown is supposed to reach. Nothing waits on it: the whole point is a
			// process that outlives its parent unless the group is reaped. Only the POSIX
			// descendant test calls this.
			child := exec.Command("sleep", "60")
			if err := child.Start(); err != nil {
				return nil, err
			}
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: strconv.Itoa(child.Process.Pid)}},
			}, nil
		},
	)

	if err := server.Run(context.Background(), &mcpsdk.StdioTransport{}); err != nil {
		os.Exit(1)
	}
}

// runWedgedFixtureServer serves the same stdio fixture, but refuses every polite rung of the SDK's
// shutdown ladder: it ignores SIGTERM, it does not exit when the client closes its stdin (the
// server runs on a goroutine the process outlives), and it holds stdout open until it is killed.
// That is what a badly behaved stdio server looks like from the client's side — the shape the
// bounded drain exists for.
func runWedgedFixtureServer() {
	signal.Ignore(syscall.SIGTERM)
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "apogee-test-wedged-fixture", Version: "v0.0.1"}, nil)
	server.AddTool(
		&mcpsdk.Tool{Name: "echo", Description: "Echo the text argument back.", InputSchema: fixtureToolSchema},
		func(_ context.Context, _ *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "echo"}}}, nil
		},
	)
	go func() { _ = server.Run(context.Background(), &mcpsdk.StdioTransport{}) }()
	// A sleep loop rather than a bare block: once the server goroutine returns on the stdin EOF
	// the runtime's deadlock detector would kill a parked process outright, which is the very
	// wedge this fixture exists to hold.
	for {
		time.Sleep(time.Minute)
	}
}

// stdioServerConfig returns a ServerConfig that launches THIS test binary as the fixture MCP
// server over stdio (the re-exec trick), under the alias "fixture".
func stdioServerConfig(t *testing.T) ServerConfig {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	return ServerConfig{
		Name:      "fixture",
		Transport: TransportStdio,
		Command:   exe,
		Env:       []string{runAsServerEnv + "=1"},
	}
}

// wedgedStdioServerConfig is stdioServerConfig pointed at the WEDGED fixture instead — the same
// re-exec, the environment switch being the only difference.
func wedgedStdioServerConfig(t *testing.T) ServerConfig {
	t.Helper()
	cfg := stdioServerConfig(t)
	cfg.Env = []string{runAsWedgedServerEnv + "=1"}
	return cfg
}

// connectFixture connects a Client to the stdio fixture server and registers cleanup that
// closes it. It fails the test on a connect error (the fixture must always be reachable).
func connectFixture(t *testing.T) *Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)

	c, err := Connect(ctx, []ServerConfig{stdioServerConfig(t)}, security.URLGuard{}, t.TempDir())
	if err != nil {
		t.Fatalf("Connect to stdio fixture: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// ----------------------------------------------------------------------------
// Connect / discovery / call (the live stdio path)
// ----------------------------------------------------------------------------

// TestConnect_SurfacesServerToolsAndCalls proves the acceptance core: a hermetic stdio MCP
// server exposes a tool that appears in the menu (surfaced as a domain.Tool with the server-
// qualified name and the server's schema) and is callable end to end over the real transport.
func TestConnect_SurfacesServerToolsAndCalls(t *testing.T) {
	c := connectFixture(t)

	tools := c.Tools()
	echo := findTool(t, tools, "fixture__echo")

	if got := echo.Description(); !strings.Contains(got, "Echo the text") {
		t.Errorf("echo Description() = %q, want it to carry the server's description", got)
	}
	// The surfaced schema is the server's, normalised to JSON — not the empty fallback.
	if got := string(echo.Schema()); !strings.Contains(got, "text") {
		t.Errorf("echo Schema() = %q, want the server's input schema", got)
	}

	res, err := echo.Execute(context.Background(), domain.ToolCall{
		ID:        "call-1",
		Tool:      "fixture__echo",
		Arguments: json.RawMessage(`{"text":"hello"}`),
	})
	if err != nil {
		t.Fatalf("echo.Execute returned a Go error (reserved for cancellation): %v", err)
	}
	if res.IsError {
		t.Fatalf("echo.Execute IsError=true, result=%q", res.Content)
	}
	if res.Content != "echo: hello" {
		t.Errorf("echo result = %q, want %q", res.Content, "echo: hello")
	}
}

// TestServerTool_IsMCPExternalEffect proves the load-bearing gating property: a REAL surfaced
// MCP tool is an ExternalEffectTool of kind mcp. This is the signal the dispatch disposition
// classifies as classMCP and gates through Approval in Auto under confine-to-workspace=true
// (proven exhaustively in internal/agent/dispatch_test.go); surfacing every server tool with
// this kind is what makes that gate hold for free (§3 D3).
func TestServerTool_IsMCPExternalEffect(t *testing.T) {
	c := connectFixture(t)
	echo := findTool(t, c.Tools(), "fixture__echo")

	ext, ok := echo.(domain.ExternalEffectTool)
	if !ok {
		t.Fatalf("surfaced MCP tool %T does not implement domain.ExternalEffectTool", echo)
	}
	if got := ext.ExternalEffect(); got != domain.EffectMCP {
		t.Errorf("ExternalEffect() = %q, want %q (the kind the Auto disposition gates)", got, domain.EffectMCP)
	}
}

// TestExecute_ServerToolErrorIsErrorResult proves a server-side tool error round-trips as an
// error ToolResult the model sees (IsError) — NOT a Go error, which the loop reserves for ctx
// cancellation (ADR 0007).
func TestExecute_ServerToolErrorIsErrorResult(t *testing.T) {
	c := connectFixture(t)
	boom := findTool(t, c.Tools(), "fixture__boom")

	res, err := boom.Execute(context.Background(), domain.ToolCall{ID: "c", Tool: "fixture__boom"})
	if err != nil {
		t.Fatalf("boom.Execute returned a Go error: %v", err)
	}
	if !res.IsError {
		t.Errorf("boom result IsError=false, want a tool-error result; content=%q", res.Content)
	}
	if !strings.Contains(res.Content, "kaboom") {
		t.Errorf("boom result = %q, want the server's error text", res.Content)
	}
}

// TestExecute_CancelledContextIsGoError proves a cancelled context is the one case Execute
// returns a Go error (ADR 0007), not an error tool-result.
func TestExecute_CancelledContextIsGoError(t *testing.T) {
	c := connectFixture(t)
	echo := findTool(t, c.Tools(), "fixture__echo")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := echo.Execute(ctx, domain.ToolCall{ID: "c", Tool: "fixture__echo"}); err == nil {
		t.Error("Execute with a cancelled context returned nil error, want ctx.Err()")
	}
}

// ----------------------------------------------------------------------------
// Lifecycle: Close tears down cleanly; resume reconnects fresh
// ----------------------------------------------------------------------------

// TestClose_TearsDownSessions proves Close shuts the live stdio session down cleanly (no
// orphaned process) and is safe to call again (idempotent in effect).
func TestClose_TearsDownSessions(t *testing.T) {
	ctx := context.Background()
	c, err := Connect(ctx, []ServerConfig{stdioServerConfig(t)}, security.URLGuard{}, t.TempDir())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("second Close: %v, want nil (idempotent)", err)
	}
}

// TestConnect_RefusesAStdioServerInsideTheWorkspace proves the exec fence reaches the MCP launch
// (F-36): a server program sitting inside the workspace — bytes a confined call is allowed to
// write, and could have written — is refused at connect time rather than executed. What is planted
// is the REAL fixture server, so a missing fence would not merely produce a different error: the
// connection would SUCCEED, which is exactly what this test would then report.
func TestConnect_RefusesAStdioServerInsideTheWorkspace(t *testing.T) {
	workspace := t.TempDir()
	cfg := stdioServerConfig(t)
	cfg.Command = copyExecutable(t, cfg.Command, filepath.Join(workspace, "planted-mcp-server"))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	c, err := Connect(ctx, []ServerConfig{cfg}, security.URLGuard{}, workspace)
	if err == nil {
		_ = c.Close()
		t.Fatal("a stdio server planted inside the workspace connected; want the exec fence to refuse it")
	}
	if !errors.Is(err, security.ErrExecFromWritablePath) {
		t.Errorf("error = %v; want it to wrap ErrExecFromWritablePath", err)
	}
	if !strings.Contains(err.Error(), "planted-mcp-server") {
		t.Errorf("error = %v; want it to name the planted program", err)
	}
	if !strings.Contains(err.Error(), cfg.Name) {
		t.Errorf("error = %v; want it to name the server", err)
	}
}

// TestClose_ReapsTheStdioServersDescendants proves F-42's fix: Close reaps the launched server's
// whole process TREE, not the leader alone. The SDK's spec-shaped shutdown (close stdin, wait,
// SIGTERM, SIGKILL) signals cmd.Process only, so before the process group a server that
// backgrounded anything left it running past the session — an unsupervised process a session
// teardown reported as clean.
func TestClose_ReapsTheStdioServersDescendants(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process groups; the Windows Job Object half of the same seam is verified on the owner's box")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	c, err := Connect(ctx, []ServerConfig{stdioServerConfig(t)}, security.URLGuard{}, t.TempDir())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	res, err := findTool(t, c.Tools(), "fixture__spawn").Execute(ctx, domain.ToolCall{ID: "s", Tool: "fixture__spawn"})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(res.Content))
	if err != nil {
		t.Fatalf("spawn returned %q, want the descendant's pid: %v", res.Content, err)
	}
	t.Cleanup(func() {
		if proc, err := os.FindProcess(pid); err == nil {
			_ = proc.Kill()
		}
	})
	if !pidAlive(pid) {
		t.Fatalf("the descendant (pid %d) was not running before Close", pid)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for pidAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("the descendant (pid %d) outlived Close; want the server's process group reaped", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestClose_BoundsTheDrainOfAWedgedStdioServer proves the drain bound the teardown seam advertises
// (platform.ProcessWaitDelay) now reaches an MCP stdio server: the Cmd is built on a session-scoped
// cancellable context that Close cancels once the SDK's shutdown ladder is spent, so cmd.Cancel and
// cmd.WaitDelay fire instead of sitting inert on a context.Background Cmd nothing ever cancels.
// The fixture refuses the whole polite ladder — stdin close ignored, SIGTERM ignored, stdout held
// open — and Close must still return inside that ladder's own bound plus ProcessWaitDelay (both
// shrunk to milliseconds here), leaving no goroutine parked in cmd.Wait behind it.
func TestClose_BoundsTheDrainOfAWedgedStdioServer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture wedges itself by ignoring a POSIX SIGTERM; the Windows half of the seam is verified on the owner's box")
	}
	// Package vars, so this test must not run in parallel with another that reads them.
	waitDelay, terminate := platform.ProcessWaitDelay, stdioTerminateDuration
	platform.ProcessWaitDelay, stdioTerminateDuration = 200*time.Millisecond, 100*time.Millisecond
	t.Cleanup(func() { platform.ProcessWaitDelay, stdioTerminateDuration = waitDelay, terminate })

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	c, err := Connect(ctx, []ServerConfig{wedgedStdioServerConfig(t)}, security.URLGuard{}, t.TempDir())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if len(c.Tools()) == 0 {
		t.Fatal("the wedged fixture surfaced no tools; the session was never live")
	}

	// Everything the wedge can legitimately cost: the ladder's two waits (stdin close, then
	// SIGTERM) before the SIGKILL, then the cancelled context's own bounded drain — plus slack for
	// a loaded machine. Anything past that is the unbounded wait this wiring removes.
	bound := 2*stdioTerminateDuration + platform.ProcessWaitDelay + 2*time.Second
	closed := make(chan error, 1)
	go func() { closed <- c.Close() }()
	select {
	case err := <-closed:
		// A killed server does not close cleanly; the error is expected, the RETURN is the claim.
		t.Logf("Close returned %v", err)
	case <-time.After(bound):
		t.Fatalf("Close did not return within %v; the wedged server's drain is unbounded", bound)
	}
	assertNoGoroutineIn(t, "os/exec.(*Cmd).Wait", 2*time.Second)
}

// TestBuildStdioTransport_CancelArmsTheCmdsTeardown pins the other half of the same wiring, the
// half the end-to-end test above cannot separate (the SDK's ladder ends in a SIGKILL of its own, so
// it bounds the wedged server either way): the CancelFunc buildStdioTransport returns is what arms
// cmd.Cancel and cmd.WaitDelay. It starts the wedged fixture directly — no SDK, no session, a
// process that ignores SIGTERM and never exits on its own — parks a goroutine in cmd.Wait, and
// cancels. On a Cmd built on a context nothing can cancel (the previous context.Background one)
// that Wait never returns.
func TestBuildStdioTransport_CancelArmsTheCmdsTeardown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture wedges itself by ignoring a POSIX SIGTERM; the Windows half of the seam is verified on the owner's box")
	}
	_, cmd, td, cancel, err := buildTransport(context.Background(), wedgedStdioServerConfig(t), security.URLGuard{}, t.TempDir())
	if err != nil {
		t.Fatalf("buildTransport: %v", err)
	}
	// Started here rather than through the transport: with no stdin the fixture's server sees EOF
	// at once and the process falls through to its sleep loop, which is precisely the server that
	// outlives every polite shutdown.
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the wedged fixture: %v", err)
	}
	td.Contain(cmd)
	t.Cleanup(func() {
		td.Reap(cmd)
		td.Release()
	})

	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	cancel()

	bound := platform.ProcessWaitDelay + 2*time.Second
	select {
	case <-waited:
	case <-time.After(bound):
		t.Fatalf("cmd.Wait did not return within %v of the cancel; the stdio Cmd's context is inert, so cmd.Cancel and cmd.WaitDelay never fire", bound)
	}
}

// assertNoGoroutineIn fails unless every goroutine naming frame has left it before the deadline.
// It polls rather than sampling once: the SDK parks its own goroutine in cmd.Wait for as long as
// the shutdown ladder runs, so the claim is about what SURVIVES Close, not about one instant.
func assertNoGoroutineIn(t *testing.T, frame string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		buf := make([]byte, 1<<20)
		stacks := string(buf[:runtime.Stack(buf, true)])
		if !strings.Contains(stacks, frame) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("a goroutine was still parked in %s %v after Close:\n%s", frame, within, stacks)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// pidAlive reports whether pid still names a live process, the signal-0 probe. A process the reap
// killed lingers as a zombie until its reparented init collects it, which is why the caller polls
// rather than asserting once.
func pidAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// copyExecutable copies src to dst with the executable bit set and returns dst — a real, runnable
// program planted where a test wants one.
func copyExecutable(t *testing.T, src, dst string) string {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
	return dst
}

// TestResume_ReconnectsFresh proves the ADR 0008 contract: resume reconnects FRESH — a new
// Connect after Close re-establishes the connection from scratch (no server-side state is
// restored), discovering the tools again. It models a resumed Session re-building its MCP
// client from the same config.
func TestResume_ReconnectsFresh(t *testing.T) {
	ctx := context.Background()
	configs := []ServerConfig{stdioServerConfig(t)}

	workspace := t.TempDir()

	first, err := Connect(ctx, configs, security.URLGuard{}, workspace)
	if err != nil {
		t.Fatalf("first Connect: %v", err)
	}
	if got := len(first.Tools()); got == 0 {
		t.Fatalf("first connect surfaced no tools")
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close before resume: %v", err)
	}

	// Resume: a brand-new client from the same config, no state carried over.
	second, err := Connect(ctx, configs, security.URLGuard{}, workspace)
	if err != nil {
		t.Fatalf("resume Connect: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	if findTool(t, second.Tools(), "fixture__echo") == nil {
		t.Error("resumed client did not rediscover the echo tool")
	}
}

// ----------------------------------------------------------------------------
// Dormant client + config validation (no process, no network)
// ----------------------------------------------------------------------------

// TestConnect_NoServersIsDormant proves a host with no MCP configured pays nothing: Connect
// returns a Client surfacing no tools whose Close is a no-op.
func TestConnect_NoServersIsDormant(t *testing.T) {
	c, err := Connect(context.Background(), nil, security.URLGuard{}, t.TempDir())
	if err != nil {
		t.Fatalf("Connect(nil): %v", err)
	}
	if got := c.Tools(); got != nil {
		t.Errorf("dormant Tools() = %v, want nil", got)
	}
	if err := c.Close(); err != nil {
		t.Errorf("dormant Close() = %v, want nil", err)
	}
}

// TestConnect_RejectsBadServerNames proves an empty or duplicate server name is rejected
// before any connection — the name must uniquely qualify a surfaced tool's registry key.
func TestConnect_RejectsBadServerNames(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		_, err := Connect(context.Background(),
			[]ServerConfig{{Name: "", Transport: TransportStdio, Command: "x"}}, security.URLGuard{}, t.TempDir())
		if err == nil {
			t.Fatal("Connect with an empty server name returned nil error")
		}
	})
	t.Run("duplicate", func(t *testing.T) {
		cfg := ServerConfig{Name: "dup", Transport: TransportStdio, Command: "x"}
		_, err := Connect(context.Background(), []ServerConfig{cfg, cfg}, security.URLGuard{}, t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("Connect with a duplicate name: err = %v, want a duplicate-name error", err)
		}
	})
}

// ----------------------------------------------------------------------------
// Transport selection / SSRF floor (no connection made)
// ----------------------------------------------------------------------------

// TestBuildTransport_StdioRequiresCommand proves a stdio server with no command is refused at
// build time rather than launching nothing.
func TestBuildTransport_StdioRequiresCommand(t *testing.T) {
	_, _, _, _, err := buildTransport(context.Background(), ServerConfig{Name: "s", Transport: TransportStdio}, security.URLGuard{}, t.TempDir())
	if err == nil {
		t.Fatal("stdio transport with no command built without error")
	}
}

// TestBuildTransport_HTTPEndpointBlockedByURLSafety proves an SSE / streamable-http server whose
// endpoint the host's url-safety policy denies is refused before any connection is made. Until
// 2026-07-26 this test asserted the same of a LOOPBACK endpoint, which the resolved-IP SSRF floor
// refused; a configured endpoint is now exempt from that floor (ADR 0012, Amendment 2026-07-26),
// so the denial asserted here is the scheme/host one — which is unchanged. The exemption's own
// bound (pinning) and the loopback case live in transport_test.go.
func TestBuildTransport_HTTPEndpointBlockedByURLSafety(t *testing.T) {
	guard := security.URLGuard{DenyHosts: []string{"blocked.example"}}.WithResolver(publicResolver)
	for _, transport := range []Transport{TransportSSE, TransportStreamableHTTP} {
		t.Run(string(transport), func(t *testing.T) {
			cfg := ServerConfig{Name: "local", Transport: transport, Endpoint: "https://blocked.example/mcp"}
			_, _, _, _, err := buildTransport(context.Background(), cfg, guard, t.TempDir())
			if err == nil {
				t.Fatalf("%s endpoint to a denied host built without error, want a url-safety block", transport)
			}
			if !strings.Contains(err.Error(), "url-safety") {
				t.Errorf("error = %v, want a url-safety block", err)
			}
		})
	}
}

// TestBuildTransport_UnknownAndMissing proves an unknown transport and a missing HTTP endpoint
// are connect-time errors, never silently defaulted.
func TestBuildTransport_UnknownAndMissing(t *testing.T) {
	if _, _, _, _, err := buildTransport(context.Background(), ServerConfig{Name: "s", Transport: "carrier-pigeon"}, security.URLGuard{}, t.TempDir()); err == nil {
		t.Error("unknown transport built without error")
	}
	if _, _, _, _, err := buildTransport(context.Background(), ServerConfig{Name: "s", Transport: ""}, security.URLGuard{}, t.TempDir()); err == nil {
		t.Error("empty transport built without error")
	}
	if _, _, _, _, err := buildTransport(context.Background(), ServerConfig{Name: "s", Transport: TransportSSE}, security.URLGuard{}, t.TempDir()); err == nil {
		t.Error("SSE transport with no endpoint built without error")
	}
}

// ----------------------------------------------------------------------------
// Rollback on a failing server (the all-or-nothing connect)
// ----------------------------------------------------------------------------

// TestConnect_RollsBackOnLaterFailure proves Connect is all-or-nothing: when a later server
// fails to connect, the session opened for an earlier, healthy server is torn down and the
// error is surfaced — no half-wired MCP set, no orphan. (The first server is the live stdio
// fixture; the second is a stdio server whose command does not exist.)
func TestConnect_RollsBackOnLaterFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	configs := []ServerConfig{
		stdioServerConfig(t), // healthy
		{Name: "broken", Transport: TransportStdio, Command: "this-command-does-not-exist-apogee"},
	}
	c, err := Connect(ctx, configs, security.URLGuard{}, t.TempDir())
	if err == nil {
		_ = c.Close()
		t.Fatal("Connect with a broken second server returned nil error, want the connect failure")
	}
	if c != nil {
		t.Errorf("Connect returned a non-nil Client alongside the error: %+v", c)
	}
}

// ----------------------------------------------------------------------------
// Content rendering (pure, no transport)
// ----------------------------------------------------------------------------

// TestRenderContent covers the result flattening branches in isolation: text concatenation, a
// non-text placeholder, and the empty-result note.
func TestRenderContent(t *testing.T) {
	t.Run("text blocks joined", func(t *testing.T) {
		res := &mcpsdk.CallToolResult{Content: []mcpsdk.Content{
			&mcpsdk.TextContent{Text: "line one"},
			&mcpsdk.TextContent{Text: "line two"},
		}}
		if got := renderContent(res); got != "line one\nline two" {
			t.Errorf("renderContent = %q, want the two lines joined", got)
		}
	})
	t.Run("non-text becomes a placeholder", func(t *testing.T) {
		res := &mcpsdk.CallToolResult{Content: []mcpsdk.Content{
			&mcpsdk.ImageContent{Data: []byte{1, 2, 3}, MIMEType: "image/png"},
		}}
		if got := renderContent(res); !strings.Contains(got, "content omitted") {
			t.Errorf("renderContent = %q, want a non-text placeholder", got)
		}
	})
	t.Run("empty result is an explicit note", func(t *testing.T) {
		if got := renderContent(&mcpsdk.CallToolResult{}); !strings.Contains(got, "no content") {
			t.Errorf("renderContent(empty) = %q, want an explicit no-content note", got)
		}
	})
}

// findTool returns the surfaced tool with the given name, failing the test if it is absent —
// the "appears in the menu" assertion the discovery tests share.
func findTool(t *testing.T, tools []domain.Tool, name string) domain.Tool {
	t.Helper()
	for _, tool := range tools {
		if tool.Name() == name {
			return tool
		}
	}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name())
	}
	t.Fatalf("tool %q not surfaced; have %v", name, names)
	return nil
}
