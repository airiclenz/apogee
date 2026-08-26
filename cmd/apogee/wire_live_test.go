package main

import (
	"context"
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/mcp"
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
