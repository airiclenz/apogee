package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/tui"
)

// retiredMechanismNotice is the exact line a `mechanisms:` key naming a retired Mechanism earns,
// for the three Drivers that print it: the TUI's startup (wire_live.go), a headless run and a
// daemon's log. The release is looked up per ID through [mechanisms.RetiredRelease] rather than
// being spelled here, so a row joining the roll in a later release is a one-line change in the
// library and not a hunt through three test files; the wording is spelled out, because these tests
// exist to pin the line a human actually reads. It words the OUTRIGHT retirement — a promoted row
// earns the floor-guard line instead, which these Drivers print through the same seam.
func retiredMechanismNotice(id string) string {
	return fmt.Sprintf("apogee: mechanism %q was retired in %s and is ignored; remove it from mechanisms:",
		id, mechanisms.RetiredRelease(domain.MechanismID(id)))
}

// captureStderr swaps the process os.Stderr for a pipe, runs f, and returns everything f wrote to
// stderr. The caller must NOT be a parallel test: os.Stderr is a process-global, so this is only
// race-free during the sequential test phase (parallel tests are paused until it finishes).
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer func() { _ = r.Close() }()
	os.Stderr = w
	// A t.Fatal or t.Skip inside f is a runtime.Goexit, which unwinds past the restore below
	// exactly as a panic does. This cleanup runs on every exit path: it closes the write end — which
	// ends the reader goroutine — and puts the process stderr back. It is idempotent with the
	// happy-path restore, which stays so the captured string is still returned in order.
	t.Cleanup(func() {
		_ = w.Close()
		os.Stderr = orig
	})
	captured := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		captured <- buf.String()
	}()

	f()

	_ = w.Close()
	os.Stderr = orig
	return <-captured
}

// applySettingSpy is the settingsEngine surface as a witness: it records which seam the dispatcher
// drove and with what, so the mapping from registry path to engine call is asserted without an Agent
// — the narrow-interface reason applySettingFor takes one at all.
type applySettingSpy struct {
	modes        []apogee.Mode
	bypass       []bool
	compaction   []bool
	prune        []bool
	contextFiles []contextFileChoice
	swaps        []*apogee.ToolRegistry
	profiles     []apogee.ModelProfile
	// swapErr is the idle-only refusal a busy engine answers a swap with, so the dispatcher's
	// keep-the-persisted-value path is exercisable without a run in flight.
	swapErr error
	// profileErr is the other idle-only refusal: a dialect the engine will not take, either because a
	// run is in flight or because this build cannot parse it.
	profileErr error
}

func (s *applySettingSpy) SetMode(m apogee.Mode)        { s.modes = append(s.modes, m) }
func (s *applySettingSpy) SetBypass(on bool)            { s.bypass = append(s.bypass, on) }
func (s *applySettingSpy) SetCompactionEnabled(on bool) { s.compaction = append(s.compaction, on) }
func (s *applySettingSpy) SetPruneToolResults(on bool)  { s.prune = append(s.prune, on) }
func (s *applySettingSpy) SetContextFiles(on bool, n []string) {
	s.contextFiles = append(s.contextFiles, contextFileChoice{enable: on, names: n})
}

func (s *applySettingSpy) SwapTools(registry *apogee.ToolRegistry) error {
	if s.swapErr != nil {
		return s.swapErr
	}
	s.swaps = append(s.swaps, registry)
	return nil
}

func (s *applySettingSpy) SetProfile(p apogee.ModelProfile) error {
	if s.profileErr != nil {
		return s.profileErr
	}
	s.profiles = append(s.profiles, p)
	return nil
}

// drove reports how many engine seams the spy was driven through in total — the assertion a key that
// should have touched nothing makes.
func (s *applySettingSpy) drove() int {
	return len(s.modes) + len(s.bypass) + len(s.compaction) + len(s.prune) + len(s.contextFiles) +
		len(s.swaps) + len(s.profiles)
}

// rebindProbe stands in for the composition root's own rebind closure ([tui.ServerHost.Rebind]): it
// records what the dispatcher drove it with, so the rebind-RIDING keys can be told apart from the
// pushed ones without an Agent or a server behind either.
type rebindProbe struct {
	calls []rebindCall
	err   error
}

// rebindCall is one drive: the model the session was bound to, and the observation it was re-driven
// with (0 until a beat has named a window, and the zero dialect until one has named that).
type rebindCall struct {
	model   string
	window  int
	dialect provider.EffortDialect
}

func (p *rebindProbe) rebind(model string, window int, dialect provider.EffortDialect) (tui.RebindResult, error) {
	p.calls = append(p.calls, rebindCall{model: model, window: window, dialect: dialect})
	if p.err != nil {
		return tui.RebindResult{}, p.err
	}
	return tui.RebindResult{Model: model, ContextWindow: window}, nil
}

// writeSettingsFixture writes a config.yaml the way the pane's splice writer leaves one behind.
func writeSettingsFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// fakeMCPSession stands in for a connected set of MCP servers: what it advertises, and whether it has
// been torn down. The reconnect is exercised against the mcpSession seam rather than a real
// *mcp.Client because what lives in the composition root is the ORDER of the act — dial, swap, tear
// down, and each failure's way back to the set that was serving — while the dialling itself is
// internal/mcp's, and its own tests connect to a real fixture server over a real transport.
type fakeMCPSession struct {
	tools  []apogee.Tool
	closed bool
}

func (f *fakeMCPSession) Tools() []apogee.Tool { return f.tools }
func (f *fakeMCPSession) Close() error         { f.closed = true; return nil }

// mcpFixtureTool is one tool a fake server advertises, named so a registry can be asked whose tools
// reached it.
type mcpFixtureTool struct{ name string }

func (t mcpFixtureTool) Name() string            { return t.name }
func (t mcpFixtureTool) Description() string     { return "a tool surfaced from a fake MCP server" }
func (t mcpFixtureTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t mcpFixtureTool) Execute(context.Context, apogee.ToolCall) (apogee.ToolResult, error) {
	return apogee.ToolResult{}, nil
}

// mcpFixture wires the two holders a reconnect drives the way the composition root wires them: the
// registry's build recipe folds whatever the MCP holder is carrying NOW. That coupling is the point —
// it is what makes the swap-then-rebuild order observable in the registry the engine is handed, and
// what a reverted swap has to put back.
type mcpFixture struct {
	set   *liveMCP
	tools *liveTools
	built []string // the endpoints the whole set was rebuilt for, in order
}

func newMCPFixture(start mcpSession, endpoint string, connect func([]mcp.ServerConfig) (mcpSession, error)) *mcpFixture {
	f := &mcpFixture{}
	f.set = newLiveMCP(start, connect)
	f.tools = newLiveTools(apogee.NewToolRegistry(), toolSetSpec{endpoint: endpoint}, func(spec toolSetSpec) *apogee.ToolRegistry {
		f.built = append(f.built, spec.endpoint)
		registry := apogee.NewToolRegistry()
		for _, tool := range f.set.tools() {
			// The fixture names its own tools, so the only registration that can fail is one this
			// test wrote wrong — and then the Lookup assertions below say so in the caller's terms.
			_ = registry.Register(tool)
		}
		return registry
	})
	return f
}

// toolNames is what a set advertises, for an assertion about WHICH connections a holder is on.
func toolNames(tools []apogee.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name())
	}
	return names
}

// mcpServersFixture is the `mcp-servers:` block a reconnect re-reads — the one place the new set
// comes from, since a list of server blocks is a shape no single string spells.
const mcpServersFixture = "mcp-servers:\n" +
	"  - name: docs\n" +
	"    transport: streamable-http\n" +
	"    endpoint: https://mcp.example.com/\n"

// assertFiringScratchDir pins the pair every Driver's Firing composition owes the run it raises:
// a record id minted BEFORE the run, a scratch dir that is that id's own dir under this Driver's
// scratch root, and a dir that actually exists 0700 — a path the confinement box advertises as
// writable has to be there when the first tool call reaches for it, and scratch may hold anything
// the run was working on. Shared by the three Driver composition tests so the invariant is stated
// once rather than three times.
func assertFiringScratchDir(t *testing.T, recordID, dir, root string) {
	t.Helper()

	if recordID == "" {
		t.Fatal("the firing named no record id; want one minted up front so its scratch dir can carry that name")
	}
	if want := filepath.Join(root, recordID); dir != want {
		t.Fatalf("the firing's scratch dir = %q, want %q — the dir and the record must share one name, "+
			"which is what puts the dir on the sessions' own 14-day sweep", dir, want)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("the firing's scratch dir was named but not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("the firing's scratch dir %q is not a directory", dir)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Errorf("the firing's scratch dir is mode %v, want 0700", info.Mode().Perm())
	}
}

// fakeSwitcher stands in for the Agent at the one method a move calls, recording every spec it was
// handed so a test can prove the wire moved — or, more often, that it did NOT.
type fakeSwitcher struct {
	specs []apogee.UpstreamSpec
	err   error
}

func (f *fakeSwitcher) SwitchUpstream(spec apogee.UpstreamSpec) error {
	if f.err != nil {
		return f.err
	}
	f.specs = append(f.specs, spec)
	return nil
}

// fakeStamper stands in for the session host at the metadata half of a move.
type fakeStamper struct{ models []string }

func (f *fakeStamper) SetModel(model string) { f.models = append(f.models, model) }

// validCfg is the minimum Config that constructs (Endpoint/Model/Events). It installs the
// real Bridge sink — the same delegate the binary wires — so the buildAgent tests exercise
// production wiring, not a stand-in. The endpoint is never dialled at construction, so a
// placeholder URL is fine.
func validCfg(t *testing.T) apogee.Config {
	t.Helper()
	return apogee.Config{
		Endpoint:     "http://127.0.0.1:1111",
		Model:        "fake",
		Mode:         apogee.ModeAskBefore,
		Events:       tui.NewBridge().Sink(),
		WorkspaceDir: t.TempDir(),
	}
}
