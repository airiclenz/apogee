package agent

// Construction-surface tests: what the host puts on domain.Config has to survive the translation
// into the tool assembly's own configuration, since a field that silently stops there is a feature
// the operator configured and never got.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/security"
)

// hostTools is one of TWO hand-assemblies of tools.HostTools — cmd/apogee's MCP-aware
// registryWithMCP (hostToolsFor) is the other, and the two are field-identical bar
// SubAgentSeatChoice. Nothing structural holds them that way, and a tool-NAMES equivalence between
// the two registries cannot: a name depends only on the roster rungs and the three nil-gated
// delegates, so a dropped URLGuard, SecretEnvVars scrub or read-root mount leaves every tool name
// identical while the user's policy quietly stops applying on one of the two paths.
//
// So the pin is field-by-field: with every Config field this composer reads set to something
// non-zero, every field of the struct it returns must come back non-zero — SubAgentSeatChoice
// excepted, the one field the engine may leave zero because apogee.Config carries nothing for it
// (`sub-agents-choice:` shapes the sub_agent schema, and the engine reads no config — ADR 0031).
// TestHostToolsForFillsEveryHostField (cmd/apogee) is the same pin on the host's side.
func TestHostToolsFillsEveryHostField(t *testing.T) {
	t.Parallel()

	host := reflect.ValueOf(hostTools(domain.Config{
		URLAllowHosts:     []string{"allowed.example"},
		URLDenyHosts:      []string{"denied.example"},
		WebSearchEndpoint: "https://search.example/v1",
		Asker:             stubAsker{},
		Presenter:         stubPresenter{},
		SkillLookup:       stubSkillLookup{},
		DisabledTools:     []string{"run_terminal_cmd"},
		EnabledTools:      []string{"web_search"},
		Profile:           domain.ModelProfile{Tools: domain.ToolRosterDelta{Enabled: []string{"console_open"}}},
		SecretEnvVars:     []string{"SOME_PROVIDER_KEY"},
		ExtraReadRoots:    func() []string { return []string{t.TempDir()} },
		VirtualReadRoots:  func() map[string]fs.FS { return nil },
	}))
	for i := range host.NumField() {
		name := host.Type().Field(i).Name
		if name == "SubAgentSeatChoice" {
			continue
		}
		if host.Field(i).IsZero() {
			t.Errorf("hostTools left tools.HostTools.%s zero for a Config that sets every field it "+
				"reads — a host policy that stops here is one the operator configured and never got",
				name)
		}
	}
}

// TestHostToolsCarriesSecretEnvVars covers the credential half of that translation: the variables a
// host resolved out of its configured `api-key-env:` entries (two servers naming distinct ones) have
// to reach HostTools, because that is the only route by which the execution tools learn to drop them
// from a subprocess environment.
func TestHostToolsCarriesSecretEnvVars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  domain.Config
		want []string
	}{
		{
			name: "two configured key variables both reach the tools",
			cfg:  domain.Config{SecretEnvVars: []string{"FIRST_KEY", "SECOND_KEY"}},
			want: []string{"FIRST_KEY", "SECOND_KEY"},
		},
		{
			name: "a config naming none leaves the scrub at apogee's own",
			cfg:  domain.Config{},
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := hostTools(tc.cfg).SecretEnvVars; !slices.Equal(got, tc.want) {
				t.Errorf("hostTools().SecretEnvVars = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDeriveDepsCarriesSecretEnvVars pins the SECOND route the same names have to travel: a
// Mechanism that spawns (autofix's formatter) scrubs the child environment from Deps, not from
// HostTools, so a config whose names reach the tools but stop before Deps leaves a hook's child
// inheriting the operator's key while a terminal command does not — the asymmetry this route
// closes. Derived for every run, so the empty DepNeeds arm is the one that matters most.
func TestDeriveDepsCarriesSecretEnvVars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  domain.Config
		want []string
	}{
		{
			name: "two configured key variables both reach a spawning Mechanism",
			cfg:  domain.Config{SecretEnvVars: []string{"FIRST_KEY", "SECOND_KEY"}},
			want: []string{"FIRST_KEY", "SECOND_KEY"},
		},
		{
			name: "a config naming none leaves the scrub at apogee's own",
			cfg:  domain.Config{},
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := deriveDeps(tc.cfg, mechanisms.DepNeeds{}).SecretEnvVars

			if !slices.Equal(got, tc.want) {
				t.Errorf("deriveDeps().SecretEnvVars = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDeriveDepsClearsTheExecFenceNetworkAllow pins the one deliberate divergence between the box
// a Config declares and the box a spawning Mechanism measures an executable against. deriveDeps
// builds the FULL ConfinementBox and then clears NetworkAllow, because that field names hosts a
// confined subprocess may REACH and says nothing about the paths a program may be resolved FROM.
// Nothing else notices if that one line goes: the fence still fences and the rest of the package
// stays green, while the exec fence quietly starts carrying a network policy it never vets. So the
// assertion is two-sided — the two path fields have to ARRIVE (a box zeroed wholesale would pass a
// bare emptiness check) and the host list has to be gone.
func TestDeriveDepsClearsTheExecFenceNetworkAllow(t *testing.T) {
	t.Parallel()

	// A config whose ConfinementBox() fills all three fields, so the cleared one is cleared and
	// not merely never set.
	cfg := domain.Config{
		WorkspaceDir:         "/work",
		ConfineWritablePaths: []string{"/work/out"},
		ConfineNetworkAllow:  []string{"example.test"},
	}

	box := deriveDeps(cfg, mechanisms.DepNeeds{}).WritableBox

	if len(box.NetworkAllow) != 0 {
		t.Errorf("deriveDeps().WritableBox.NetworkAllow = %q, want empty", box.NetworkAllow)
	}
	if box.WorkspaceRoot != cfg.WorkspaceDir {
		t.Errorf("deriveDeps().WritableBox.WorkspaceRoot = %q, want %q", box.WorkspaceRoot, cfg.WorkspaceDir)
	}
	if !slices.Equal(box.WritablePaths, cfg.ConfineWritablePaths) {
		t.Errorf("deriveDeps().WritableBox.WritablePaths = %q, want %q", box.WritablePaths, cfg.ConfineWritablePaths)
	}
}

// TestHostToolsBuildsTheURLGuardFromTheConfiguredHosts covers the network half of that
// translation. Until this key existed hostTools handed the tools a zero URLGuard, so the whole
// question was whether the SSRF floor was on; now the operator's own `url-safety:` hosts ride
// Config, and they reach the network tools through this one field or not at all. The deny is
// spelled the way a human writes one into config.yaml (mixed case, a trailing root dot) because
// the entry has to be normalised on the way in — an un-normalised list assembles a guard that
// looks configured and matches nothing.
func TestHostToolsBuildsTheURLGuardFromTheConfiguredHosts(t *testing.T) {
	t.Parallel()

	// Every name resolves to a public address, so the string-level allow/deny decisions under
	// test are reached without touching DNS.
	publicResolver := func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}

	t.Run("a configured deny reaches the guard", func(t *testing.T) {
		t.Parallel()

		guard := hostTools(domain.Config{URLDenyHosts: []string{"Blocked.EXAMPLE."}}).
			URLGuard.WithResolver(publicResolver)

		if err := guard.Check("https://blocked.example/x"); !errors.Is(err, security.ErrURLBlocked) {
			t.Errorf("the configured deny never reached the tools' guard: %v", err)
		}
		if err := guard.Check("https://elsewhere.example/x"); err != nil {
			t.Errorf("the deny entry blocked a host it does not name: %v", err)
		}
	})

	t.Run("a configured allow list reaches the guard", func(t *testing.T) {
		t.Parallel()

		guard := hostTools(domain.Config{URLAllowHosts: []string{"docs.example.com"}}).
			URLGuard.WithResolver(publicResolver)

		if err := guard.Check("https://docs.example.com/x"); err != nil {
			t.Errorf("the allowed host was refused: %v", err)
		}
		if err := guard.Check("https://elsewhere.example/x"); !errors.Is(err, security.ErrURLBlocked) {
			t.Errorf("a host outside the configured allow list was permitted: %v", err)
		}
	})

	t.Run("a config naming no hosts leaves the reach as it was", func(t *testing.T) {
		t.Parallel()

		guard := hostTools(domain.Config{}).URLGuard
		if guard.AllowHosts != nil || guard.DenyHosts != nil {
			t.Errorf("an unconfigured Config produced host lists: allow=%q deny=%q", guard.AllowHosts, guard.DenyHosts)
		}
	})
}

// ---------------------------------------------------------------------------
// The Inspector's arming seam (Config.Inspector → provider wire observer → WireEvent)
// ---------------------------------------------------------------------------

// wireUpstream is a hermetic Upstream that answers with a two-chunk streamed completion and
// records the body it was posted, so a test can hold the WireEvent's payload against the bytes
// that actually went out. The mutex is load-bearing for the reason authRecorder's is: the handler
// runs on the server's goroutine while the test reads afterwards.
type wireUpstream struct {
	mu   sync.Mutex
	body string
}

func (u *wireUpstream) serve(w http.ResponseWriter, r *http.Request) {
	posted, _ := io.ReadAll(r.Body)
	u.mu.Lock()
	u.body = string(posted)
	u.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"seen\"},\"finish_reason\":null}]}\n\n")
	_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

// posted returns the recorded request body under the recorder's lock.
func (u *wireUpstream) posted(t *testing.T) string {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.body
}

// newWireUpstream starts the recording Upstream and returns it with its URL.
func newWireUpstream(t *testing.T) (*wireUpstream, string) {
	t.Helper()
	up := &wireUpstream{}
	srv := httptest.NewServer(http.HandlerFunc(up.serve))
	t.Cleanup(srv.Close)
	return up, srv.URL
}

// wireEvents picks the WireEvents out of a recorded stream, in emission order.
func wireEvents(events []domain.Event) []domain.WireEvent {
	var out []domain.WireEvent
	for _, e := range events {
		if we, ok := e.(domain.WireEvent); ok {
			out = append(out, we)
		}
	}
	return out
}

// TestInspectorArmsWireEventsThroughTheSink is the arming seam end to end: a Config with the
// Inspector on reports one request record and one response record per model call, through the
// SAME EventSink every other Event travels on — the property that keeps the Inspector benchable
// in-process rather than needing a surface of its own (ADR 0031). It runs against a real provider
// client and a real (hermetic) HTTP Upstream because the capture lives inside that client: a fake
// Responder would prove the observer was installed on nothing.
func TestInspectorArmsWireEventsThroughTheSink(t *testing.T) {
	t.Parallel()

	up, url := newWireUpstream(t)
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Endpoint = url
	cfg.APIKey = "super-secret-token"
	cfg.Inspector = true

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	stepOnce(t, a, "hi")

	records := wireEvents(sink.events)
	if len(records) != 2 {
		t.Fatalf("got %d WireEvents for one model call; want exactly one per direction", len(records))
	}
	req, resp := records[0], records[1]
	if req.Direction != domain.WireDirectionRequest {
		t.Errorf("first record direction = %q; want %q", req.Direction, domain.WireDirectionRequest)
	}
	if resp.Direction != domain.WireDirectionResponse {
		t.Errorf("second record direction = %q; want %q", resp.Direction, domain.WireDirectionResponse)
	}

	// The request record is the body that was actually posted, still parseable as the JSON it is.
	if req.Payload != up.posted(t) {
		t.Errorf("request payload = %q; want the posted body %q", req.Payload, up.posted(t))
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(req.Payload), &body); err != nil {
		t.Errorf("request payload does not round-trip as JSON: %v", err)
	}
	if body["model"] != "test-model" {
		t.Errorf("request payload names model %v; want the bound model", body["model"])
	}
	// The credential is a header and headers are never captured, so it cannot be in either record.
	for _, rec := range records {
		if strings.Contains(rec.Payload, cfg.APIKey) || strings.Contains(rec.Payload, "Bearer") {
			t.Errorf("%s record carries authorization material: %q", rec.Direction, rec.Payload)
		}
	}
	// The response record is the stream's own payload lines, in arrival order.
	for _, want := range []string{"\"content\":\"seen\"", "\"finish_reason\":\"stop\"", "[DONE]"} {
		if !strings.Contains(resp.Payload, want) {
			t.Errorf("response payload %q is missing %q", resp.Payload, want)
		}
	}

	// Both records carry the stamp every Event of this Agent carries: the top-level agent's depth
	// and empty run identity, on the Turn the call was made for.
	msg, ok := firstMessageEvent(t, sink.events)
	if !ok {
		t.Fatal("the Turn produced no MessageEvent to take the turn index from")
	}
	for _, rec := range records {
		if rec.Depth != 0 || rec.CallID != "" {
			t.Errorf("%s record stamped depth=%d callID=%q; want the top-level agent's 0/\"\"",
				rec.Direction, rec.Depth, rec.CallID)
		}
		if rec.Turn != msg.Turn {
			t.Errorf("%s record stamped turn %d; want the Turn that made the call (%d)",
				rec.Direction, rec.Turn, msg.Turn)
		}
	}
}

// TestInspectorOffEmitsNoWireEvents is the off-state the ratified call turns on: a Config that
// leaves the key alone installs no observer, so the session emits no WireEvents at all — the
// capture is absent rather than merely unread.
func TestInspectorOffEmitsNoWireEvents(t *testing.T) {
	t.Parallel()

	_, url := newWireUpstream(t)
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Endpoint = url // cfg.Inspector stays false

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	stepOnce(t, a, "hi")

	if got := wireEvents(sink.events); len(got) != 0 {
		t.Errorf("a disarmed Inspector emitted %d WireEvents; want none", len(got))
	}
}

// TestInspectorSurvivesASwitchUpstream pins the re-arm: an observer belongs to the provider client
// it was built with, and /server rebuilds that client — so without re-arming, a session that
// started with the Inspector on would go silently blind the moment the human switched servers.
func TestInspectorSurvivesASwitchUpstream(t *testing.T) {
	t.Parallel()

	_, first := newWireUpstream(t)
	_, second := newWireUpstream(t)
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Endpoint = first
	cfg.Inspector = true

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.SwitchUpstream(UpstreamSpec{Endpoint: second}); err != nil {
		t.Fatalf("SwitchUpstream: %v", err)
	}
	if err := a.Rebind(RebindSpec{Model: "test-model"}); err != nil {
		t.Fatalf("Rebind: %v", err)
	}
	stepOnce(t, a, "hi")

	if got := wireEvents(sink.events); len(got) != 2 {
		t.Errorf("got %d WireEvents after a server switch; want the capture still armed (2)", len(got))
	}
}

// TestConfinedCallBoxCarriesTheScratchDir covers the scratch half of the construction translation
// (workspace-clobber hardening, 2026-08-22): the ScratchDir the host puts on Config must reach the
// box a confined tool call actually runs inside — the real dispatch path, observed through the
// fake Confiner — and must then FOLLOW the active session: after SetScratchDir moves it at a
// session boundary, the box built for the next call carries the new session's dir and not the old.
func TestConfinedCallBoxCarriesTheScratchDir(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	sub := &subprocTool{name: "terminal"}
	conf := &fakeConfiner{caps: capsBoth()}
	cfg := autoConfig(sink, conf, true, sub)
	cfg.ScratchDir = "/home/u/.apogee/scratch/sess-1"

	a := driveToolCall(t, cfg, sink, "c1", "terminal", `{}`)

	if conf.confineCount() != 1 {
		t.Fatalf("Confine called %d times, want 1", conf.confineCount())
	}
	if box := conf.lastConfinedBox(); !slices.Contains(box.WritablePaths, cfg.ScratchDir) {
		t.Errorf("confined box WritablePaths = %v, want to contain the scratch dir %q",
			box.WritablePaths, cfg.ScratchDir)
	}

	// The session boundary: the host moves the scratch dir; the very next box follows it.
	a.SetScratchDir("/home/u/.apogee/scratch/sess-2")
	box := a.confinementBox()
	if !slices.Contains(box.WritablePaths, "/home/u/.apogee/scratch/sess-2") {
		t.Errorf("after SetScratchDir, box WritablePaths = %v, want the new session's dir", box.WritablePaths)
	}
	if slices.Contains(box.WritablePaths, cfg.ScratchDir) {
		t.Errorf("after SetScratchDir, box WritablePaths = %v still carries the OLD session's dir", box.WritablePaths)
	}
}

// stubSkillLookup is a host skill catalog that answers nothing — enough to prove the seam is
// THREADED, which is the only thing hostTools decides. What a real catalog answers is
// internal/skills' question.
type stubSkillLookup struct{}

func (stubSkillLookup) LookupSkill(string) domain.SkillLookupResult {
	return domain.SkillLookupResult{}
}

// TestHostToolsThreadsTheSkillLookupOntoTheDefaultRoster pins the Config → hostTools → registry
// thread for load_skill (ADR 0065 §6). The tool is registered by CONSTRUCTION from this one field,
// so a Config that carries a catalog and a roster that does not offer the door is the whole failure
// mode — and the engine's own assembly is the path a Driver takes whenever it injects no
// Config.Tools of its own.
func TestHostToolsThreadsTheSkillLookupOntoTheDefaultRoster(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	withCatalog := defaultRoster(domain.Config{WorkspaceDir: workspace, SkillLookup: stubSkillLookup{}})
	if _, ok := withCatalog.Lookup("load_skill"); !ok {
		t.Error("Config.SkillLookup never reached the tools: load_skill is not on the default roster")
	}

	// And the graceful half: no catalog, no door — the model is never offered a tool nothing can
	// answer for, exactly as a nil Asker omits ask_user.
	without := defaultRoster(domain.Config{WorkspaceDir: workspace})
	if _, ok := without.Lookup("load_skill"); ok {
		t.Error("load_skill was registered with no SkillLookup configured")
	}

	// The ordinary roster lever still closes it — it is a tool, not a Mechanism (ADR 0065 §6).
	disabled := defaultRoster(domain.Config{
		WorkspaceDir: workspace, SkillLookup: stubSkillLookup{},
		DisabledTools: []string{"load_skill"},
	})
	if _, ok := disabled.Lookup("load_skill"); ok {
		t.Error("`tools.disabled: [load_skill]` did not reach the assembly")
	}
}

// TestBuildEnabledMechanismsFloorsAFreshRegistry: an EnableMechanisms list the engine was handed
// EMPTY builds the off-ramp floor rather than nothing (ADR 0070), while a list that names something
// builds exactly what it names — the Drivers union the floor in before New sees it, so a resolved
// `{"tool_use_enforcer": false}` arrives here as a one-element list and must stay one element.
//
// The claim is read off the registry the construction produced, never off fired events: what the
// list BUILDS is the whole question, and an off-ramp only triggers on a malformed reply.
func TestBuildEnabledMechanismsFloorsAFreshRegistry(t *testing.T) {
	floor := mechanisms.OffRampFloor(nil)
	if len(floor) < 2 {
		t.Fatalf("OffRampFloor(nil) = %v; want the two catalogued off-ramps", floor)
	}

	for _, tt := range []struct {
		name string
		ids  []domain.MechanismID
		want []domain.MechanismID
	}{
		{"nil list", nil, floor},
		{"empty list", []domain.MechanismID{}, floor},
		{"one-element list", []domain.MechanismID{floor[1]}, []domain.MechanismID{floor[1]}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig(&recordingSink{})
			cfg.EnableMechanisms = tt.ids

			a, err := newAgent(cfg, echoResponder{reply: "hi"})
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			if got := armedIDs(a, domain.HookPostResponse); !slices.Equal(got, tt.want) {
				t.Errorf("armed post-response Mechanisms = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBuildEnabledMechanismsNeverFloorsAHandedInRegistry is the bite test for the floor's one
// exception. A sub-agent spawn hands the child its parent's registry and CLEARS EnableMechanisms
// (subagent.go), because those rows are already built into what it inherited. If the engine floored
// a handed-in registry too, it would re-add the parent's own off-ramps and MechanismRegistry.Add
// would refuse them as already registered — failing every delegation from a parent that was itself
// built with nil lists, which after this change is every default parent there is.
func TestBuildEnabledMechanismsNeverFloorsAHandedInRegistry(t *testing.T) {
	parentCfg := baseConfig(&recordingSink{})
	parent, err := newAgent(parentCfg, echoResponder{reply: "hi"})
	if err != nil {
		t.Fatalf("newAgent (parent): %v", err)
	}

	childCfg := baseConfig(&recordingSink{})
	childCfg.Mechanisms = parent.registry.ForSubAgent()
	childCfg.EnableMechanisms = nil

	child, err := newAgent(childCfg, echoResponder{reply: "hi"})
	if err != nil {
		t.Fatalf("newAgent (child off an inherited registry): %v — a handed-in registry must never be floored", err)
	}
	if got, want := armedIDs(child, domain.HookPostResponse), mechanisms.OffRampFloor(nil); !slices.Equal(got, want) {
		t.Errorf("child armed %v, want the inherited floor %v exactly once", got, want)
	}
}
