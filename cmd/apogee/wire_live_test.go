package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/security"
)

// The `url-safety:` guard an MCP connect is made under (audit 2026-08-25 F-40).
//
// Both call sites used to hand the transport a ZERO security.URLGuard, so a configured
// `deny-hosts` entry bound every network tool and no `sse`/`streamable-http` endpoint. The
// proof belongs here rather than in internal/mcp: the transport has always checked whatever
// guard it was given — what was missing was the composition root giving it the operator's.

// deniedMCPServer is the one entry both tests below reconnect to: an HTTP-transported server on a
// host the deny list closes, at a port nothing listens on. The port matters as much as the host —
// it is what makes the two outcomes distinguishable, because a guard that never judged the
// endpoint lets the dial happen and fails with a connection error instead.
var deniedMCPServer = mcp.ServerConfig{
	Name:      "denied",
	Transport: mcp.TransportSSE,
	Endpoint:  "http://localhost:1/mcp",
}

// urlGuardWiring assembles the boot phase over a fresh workspace and hands back the wiring with
// wireSession NOT yet run, so a caller can drive that step itself and read what it answered.
func urlGuardWiring(t *testing.T, opts config.Options) *rootWiring {
	t.Helper()
	opts.Endpoint = "http://127.0.0.1:1111"
	opts.Model = "fake"
	opts.Mode = "ask-before"
	opts.Workspace = t.TempDir()
	opts.ConfigDir = t.TempDir()
	opts.AutoCompact = true

	roots, err := resolveRoots(opts.ConfigDir, opts.Workspace)
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	w := newRootWiring(opts, apogee.ModeAskBefore, roots)
	t.Cleanup(w.close)
	if err := w.resolveConfig(); err != nil {
		t.Fatalf("resolveConfig: %v", err)
	}
	return w
}

// A startup connect is judged by the operator's own host lists: the endpoint is refused before
// anything is dialled, with the url-safety message, and the all-or-nothing connect makes that the
// startup error. A connection error here would mean the guard is still the zero one.
func TestWireSessionConnectsMCPUnderTheConfiguredURLGuard(t *testing.T) {
	t.Parallel()
	w := urlGuardWiring(t, config.Options{
		URLDenyHosts: []string{"localhost"},
		MCPServers:   []mcp.ServerConfig{deniedMCPServer},
	})

	err := w.wireSession(context.Background())

	if err == nil {
		t.Fatal("wireSession: want the url-safety refusal, got none")
	}
	if !strings.Contains(err.Error(), "blocked by url-safety") {
		t.Errorf("wireSession error = %v; want it blocked by url-safety before any dial", err)
	}
}

// A reconnect lands later than startup, when the host lists may have moved through `/settings` —
// so it is judged by the lists the session is on NOW, not by the snapshot the process launched
// with. The empty-list half is what makes the assertion about the guard rather than the network:
// the same entry, the same port, a different outcome.
func TestMCPReconnectUsesTheLiveURLSafetyLists(t *testing.T) {
	t.Parallel()
	w := urlGuardWiring(t, config.Options{})
	if err := w.wireSession(context.Background()); err != nil {
		t.Fatalf("wireSession: %v", err)
	}
	engine := &applySettingSpy{}

	if err := w.toolSet.setDenyHosts([]string{"localhost"}, engine); err != nil {
		t.Fatalf("setDenyHosts: %v", err)
	}
	err := w.mcpSet.reconnect([]mcp.ServerConfig{deniedMCPServer}, w.toolSet, engine)

	if err == nil {
		t.Fatal("reconnect under a live deny list: want the url-safety refusal, got none")
	}
	if !strings.Contains(err.Error(), "blocked by url-safety") {
		t.Errorf("reconnect error = %v; want the live deny list to close the endpoint", err)
	}

	// The same reconnect with the deny list lifted reaches the dial instead — the guard, not the
	// unreachable port, is what refused it above.
	if err := w.toolSet.setDenyHosts(nil, engine); err != nil {
		t.Fatalf("setDenyHosts(nil): %v", err)
	}
	err = w.mcpSet.reconnect([]mcp.ServerConfig{deniedMCPServer}, w.toolSet, engine)

	if err == nil {
		t.Fatal("reconnect with no deny list: want the connection error, got none")
	}
	if strings.Contains(err.Error(), "blocked by url-safety") {
		t.Errorf("reconnect error = %v; want a connection error once no list closes the host", err)
	}
}

// The root the settings editor's exec fence is measured against, asserted where it is SEEDED
// (audit residual, 2026-08-28).
//
// settingsedit.go's own tests hand newExternalEdit a workspace of their own, so every one of them
// passes whatever the composition root seeds here. And the value matters more than an ordinary
// wiring mistake would suggest: security.RefuseExecFromWritablePath drops an EMPTY root from its
// fence set rather than measuring against it, so a seeded "" raises no fence at all — the editor
// ladder would resolve a program the model authored inside the workspace and the pane would
// suspend into it, outside any box. Both halves are asserted: the root IS the session's resolved
// workspace, and it is load-bearing.
func TestWireSessionFencesTheSettingsEditorAgainstTheWorkspace(t *testing.T) {
	t.Parallel()
	w := urlGuardWiring(t, config.Options{})
	if err := w.wireSession(context.Background()); err != nil {
		t.Fatalf("wireSession: %v", err)
	}

	if w.externalEdits.workspace != w.roots.workspace {
		t.Errorf("externalEdit.workspace = %q; want the session's resolved workspace %q — the editor "+
			"fence and the file tools' scope must not disagree about which bytes the model can write",
			w.externalEdits.workspace, w.roots.workspace)
	}
	if !filepath.IsAbs(w.externalEdits.workspace) {
		t.Errorf("externalEdit.workspace = %q is not absolute; a fence measured against a relative root "+
			"compares an absolute resolved program path with something that is not a root at all",
			w.externalEdits.workspace)
	}

	// The behavioural half: an editor that resolves to a program under the wired workspace is
	// refused. Seeding "" at the newExternalEdit call fails exactly here — the assertions above
	// still hold their shape, but the fence set would be empty and the spec would hand back an argv.
	planted := filepath.Join(w.roots.workspace, "bin", "vim")
	if err := os.MkdirAll(filepath.Dir(planted), 0o755); err != nil {
		t.Fatalf("plant the editor: %v", err)
	}
	if err := os.WriteFile(planted, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("plant the editor: %v", err)
	}
	w.externalEdits.look = func(string) (string, error) { return planted, nil }

	launch, err := w.externalEdits.spec("mode")

	if err == nil {
		t.Fatalf("spec over an editor inside the wired workspace = %+v; want the fence's refusal", launch)
	}
	if !errors.Is(err, security.ErrExecFromWritablePath) {
		t.Errorf("refusal %q does not wrap security.ErrExecFromWritablePath; the seeded fence root is not "+
			"the workspace the model writes into", err)
	}
}

// A `url-safety:` edit binds BOTH surfaces the lists reach, and the MCP one is only reachable by
// dialling again: the guard is consumed at connect time and never retained (internal/mcp/
// transport.go), so a connection made under the old lists keeps talking to a host the operator has
// just closed until something else happens to reconnect. This is the re-admission that closes it
// (audit 2026-08-28: "after a `/settings` url-safety edit, network tools and the MCP connection
// disagree about which hosts are allowed") — and because Connect is all-or-nothing, the denied
// server is DROPPED and named rather than costing the session the servers it may still talk to.
func TestApplySettingURLSafetyHostsDropsAnMCPServerTheNewListDenies(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeSettingsFixture(t, path, mcpServersFixture)

	old := &fakeMCPSession{tools: []apogee.Tool{mcpFixtureTool{name: "docs__echo"}}}
	var dialled [][]mcp.ServerConfig
	fixture := newMCPFixture(old, "", func(servers []mcp.ServerConfig) (mcpSession, error) {
		dialled = append(dialled, servers)
		return &fakeMCPSession{}, nil
	})
	spy := &applySettingSpy{}
	apply := applySettingFor(settingsApplier{engine: spy, tools: fixture.tools, mcp: fixture.set, configPath: path})

	note, err := apply("url-safety.deny-hosts", "[mcp.example.com]")

	if err != nil {
		t.Fatalf("apply url-safety.deny-hosts: %v", err)
	}
	if want := toolRosterNote + "; mcp server docs disconnected — its endpoint is denied"; note != want {
		t.Errorf("note = %q, want %q — a server the edit disconnected is news the row owes the human", note, want)
	}
	if len(dialled) != 1 || len(dialled[0]) != 0 {
		t.Fatalf("dialled = %+v, want exactly one reconnect, to no server at all", dialled)
	}
	if !old.closed {
		t.Error("the connection to the denied host is still open; the tool set followed the new list " +
			"and the connection did not, which is the disagreement this closes")
	}
	if names := toolNames(fixture.set.tools()); len(names) != 0 {
		t.Errorf("live MCP tools = %v, want none: the only server was dropped", names)
	}
}

// The guard on that re-admission, and the reason it is a comparison rather than an unconditional
// dial: a reconnect launches a process per stdio server and handshakes every HTTP one, synchronously
// on the Update goroutine. The ordinary edit — a web-tool host, while an MCP server is connected —
// changes no endpoint verdict at all, and paying a full re-dial for it would freeze the frame, reset
// every server's state and break a stdio server holding a lock or a port. So a deny that covers
// nothing leaves the connection exactly where it is.
func TestApplySettingURLSafetyHostsLeavesMCPAloneWhenNoVerdictMoved(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeSettingsFixture(t, path, mcpServersFixture)

	serving := &fakeMCPSession{tools: []apogee.Tool{mcpFixtureTool{name: "docs__echo"}}}
	dials := 0
	fixture := newMCPFixture(serving, "", func([]mcp.ServerConfig) (mcpSession, error) {
		dials++
		return &fakeMCPSession{}, nil
	})
	spy := &applySettingSpy{}
	apply := applySettingFor(settingsApplier{engine: spy, tools: fixture.tools, mcp: fixture.set, configPath: path})

	note, err := apply("url-safety.deny-hosts", "[unrelated.example.com]")

	if err != nil {
		t.Fatalf("apply url-safety.deny-hosts: %v", err)
	}
	if note != toolRosterNote {
		t.Errorf("note = %q, want just %q: no server moved, so there is nothing to report", note, toolRosterNote)
	}
	if dials != 0 {
		t.Errorf("the edit dialled %d time(s); a list that closes no configured endpoint must leave "+
			"the live connections untouched", dials)
	}
	if serving.closed {
		t.Error("the connections that are still admitted were torn down and re-made")
	}
	// Identity, not equivalence: a re-dial that happened to reach the same servers would still have
	// cost every one of them its state. swap is the holder's only reader of the session itself, and
	// putting `serving` back is a no-op exactly when the assertion passes.
	if got := fixture.set.swap(serving); got != mcpSession(serving) {
		t.Errorf("the holder is on %p, want the very session it started on (%p)", got, serving)
	}
}

// The failure posture, and it is the opposite of the `mcp-servers:` row's. The tool rebuild has
// already COMMITTED by the time the re-admission dials, so the primary effect of the edit is in
// force — returning the dial's failure as the row's error would make the pane say `saved — live
// apply failed` about an edit that applied. It goes in the note instead, in liveMCP's own words, so
// the human is still told the two things that matter: what failed, and that the connections they had
// are still theirs.
func TestApplySettingURLSafetyHostsReportsAFailedReconnectInTheNote(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeSettingsFixture(t, path, mcpServersFixture)

	serving := &fakeMCPSession{tools: []apogee.Tool{mcpFixtureTool{name: "docs__echo"}}}
	fixture := newMCPFixture(serving, "", func([]mcp.ServerConfig) (mcpSession, error) {
		return nil, errors.New("dial: connection refused")
	})
	spy := &applySettingSpy{}
	apply := applySettingFor(settingsApplier{engine: spy, tools: fixture.tools, mcp: fixture.set, configPath: path})

	note, err := apply("url-safety.deny-hosts", "[mcp.example.com]")

	if err != nil {
		t.Fatalf("apply url-safety.deny-hosts = %v; the host lists ARE in force, so the row must not "+
			"report the edit as failed over the half that could not follow", err)
	}
	for _, want := range []string{toolRosterNote, "mcp reconnect failed", "connection refused", "previous connections kept"} {
		if !strings.Contains(note, want) {
			t.Errorf("note = %q, want it to contain %q", note, want)
		}
	}
	if serving.closed {
		t.Error("the sessions that are still serving were torn down by a reconnect that never happened")
	}
}

// The startup line a human sees when their `mechanisms:` block still names a Mechanism this
// release retired (ISSUES.md, 2026-08-29: the notice's producer and the helper's wording were
// pinned, the boundary that prints it was not).
//
// This is the ONE resolver path that may write to stderr — startup runs before the alt screen —
// so the assertion is about the process stream rather than a returned string, and the test is
// sequential for that reason: captureStderr swaps a process global.
func TestWireSessionReportsARetiredMechanismOnStderr(t *testing.T) {
	w := urlGuardWiring(t, config.Options{Mechanisms: map[string]bool{"grammar": true}})

	var err error
	stderr := captureStderr(t, func() { err = w.wireSession(context.Background()) })

	if err != nil {
		t.Fatalf("wireSession: %v; a retired mechanism id is tolerated, never a refusal", err)
	}
	want := retiredMechanismNotice("grammar")
	if got := strings.Count(stderr, want); got != 1 {
		t.Errorf("the retired-mechanism notice appeared %d times on stderr; want exactly 1 line\n"+
			"want: %q\nstderr: %q", got, want, stderr)
	}
	if len(w.cfg.EnableMechanisms) != 0 {
		t.Errorf("EnableMechanisms = %v; a retired id is dropped from what the engine arms, which is "+
			"exactly why the line above has to be printed", w.cfg.EnableMechanisms)
	}
}
