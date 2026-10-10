package agent

// Construction-surface tests: what the host puts on domain.Config has to survive the translation
// into the tool assembly's own configuration, since a field that silently stops there is a feature
// the operator configured and never got.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tools"
)

// TestNewAgentBuildsATopLevelAgent pins the other side of the split construction path: newAgent
// builds the session's ROOT, which owns every shared handle afresh and carries none of a delegate's
// facts — depth 0, no run identity, no delegate bound, no mode view above it, no mid-Exchange fold.
// It reads its effort dialect from its own Config, the seed a delegate never takes from cfg.
func TestNewAgentBuildsATopLevelAgent(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(&recordingSink{})
	cfg.EffortDialect = domain.EffortDialectKwargs
	cfg.Delegation.MaxSteps = 5
	a, err := newAgent(cfg, echoResponder(t, "ok"))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	if a.depth != 0 || a.callID != "" || a.task != "" || a.consoleOwner != "" {
		t.Errorf("top-level identity = (%d, %q, %q, %q), want all zero", a.depth, a.callID, a.task, a.consoleOwner)
	}
	if a.stepCap != 0 || a.tokenCap != 0 || a.timeCap != 0 {
		t.Errorf("top-level bounds = (%d, %d, %v), want all zero: the delegate caps never bind the main loop", a.stepCap, a.tokenCap, a.timeCap)
	}
	if a.liveMode != nil || a.midExchangeCompaction || a.seatFallback {
		t.Error("a top-level Agent carries no parent mode view, no mid-Exchange fold and no seat note")
	}
	if a.guards.Breaker == nil || a.journal == nil || a.consoles == nil || a.tasks == nil || a.now == nil {
		t.Error("a top-level Agent owns fresh guards, journal, Console registry, task list and clock")
	}
	if a.delegation == nil || a.delegation.snapshot() != nil {
		t.Error("a top-level Agent holds an empty Delegation-target latch of its own")
	}
	if a.effortDialect != toProviderDialect(domain.EffortDialectKwargs) {
		t.Errorf("effortDialect = %q, want the Config's %q", a.effortDialect, domain.EffortDialectKwargs)
	}
}

// ---------------------------------------------------------------------------
// The dangerous-action ruleset (Config.DangerousRules → the top-level guard)
// ---------------------------------------------------------------------------

// TestDangerousGuardOfAZeroConfigIsTheShippedFloor pins the nil half of Config.DangerousRules: an
// embedder that names no rules gets the shipped ruleset, rule for rule, and the circuit breaker
// beside it — the floor is the default, never something to opt into.
func TestDangerousGuardOfAZeroConfigIsTheShippedFloor(t *testing.T) {
	t.Parallel()

	guards := guardsFor(domain.Config{})

	if guards.Breaker == nil {
		t.Error("a zero Config built no circuit breaker; the breaker is not configured by the ruleset")
	}
	if got, want := ruleIDs(guards.Dangerous.Rules()), ruleIDs(security.DefaultDangerousRules()); !sameIDs(got, want) {
		t.Errorf("zero Config guard rules = %v, want the shipped %v", got, want)
	}
}

// TestDangerousGuardOfAnEmptyRulesetHoldsNoRules pins the other half: a non-nil EMPTY ruleset is
// exactly what it says — the shape a global `remove:` of every shipped id merges to — and must not
// read as nil and bring the shipped set back.
func TestDangerousGuardOfAnEmptyRulesetHoldsNoRules(t *testing.T) {
	t.Parallel()

	guards := guardsFor(domain.Config{DangerousRules: []domain.DangerousRule{}})

	if rules := guards.Dangerous.Rules(); len(rules) != 0 {
		t.Errorf("an empty ruleset built %d rules (%v); want none", len(rules), ruleIDs(rules))
	}
	if guards.Breaker == nil {
		t.Error("an empty ruleset dropped the circuit breaker")
	}
}

// TestDangerousGuardForceGatesACallMatchingAnAddedAskRule drives a call through the loop: a rule the
// configuration added at the ask tier forces the Approver even in Auto, where the call would
// otherwise have run confined without a word.
func TestDangerousGuardForceGatesACallMatchingAnAddedAskRule(t *testing.T) {
	t.Parallel()
	sink := &recordingSink{}
	sub := &subprocTool{name: "terminal"}
	cfg := autoConfig(sink, &fakeConfiner{caps: capsBoth()}, true, sub)
	cfg.DangerousRules = append(security.DomainRules(security.DefaultDangerousRules()), domain.DangerousRule{
		ID: "no-prod-deploy", Pattern: `\bkubectl\s+.*--context[= ]prod\b`,
		Tier: domain.DangerousTierAsk, Reason: "deploy to the production cluster",
	})
	approver := &fakeApprover{decision: domain.ApprovalDeny}
	cfg.Approver = approver

	driveToolCall(t, cfg, sink, "c1", "terminal", `{"command":"kubectl apply -f app.yaml --context prod"}`)

	if req := requestOnApproval(t, sink.events); req.Reason != forceApprovalReason {
		t.Errorf("approval reason = %q, want the forced look %q", req.Reason, forceApprovalReason)
	}
	if approver.calls != 1 || sub.ranCount() != 0 {
		t.Errorf("Approver calls = %d, tool runs = %d; want one forced look and no run after the no",
			approver.calls, sub.ranCount())
	}
}

// ruleIDs is the ids of rules, in order.
func ruleIDs(rules []security.Rule) []string {
	ids := make([]string, len(rules))
	for i, r := range rules {
		ids[i] = r.ID
	}
	return ids
}

// sameIDs reports whether two id lists hold the same ids, order aside — the guard sorts its rules
// by tier, so the order it hands back is not the order they were written in.
func sameIDs(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// ---------------------------------------------------------------------------
// The Inspector's arming seam (Config.Inspector → provider wire observer → WireEvent)
// ---------------------------------------------------------------------------

// wireUpstream records the body posted to a stubllm upstream, so a test can hold the WireEvent's
// payload against the bytes that actually went out — a byte-for-byte comparison the stub's own
// request log (a decoded Request, not bytes) cannot make. The mutex is load-bearing for the
// reason authRecorder's is: the handler runs on the server's goroutine while the test reads
// afterwards.
type wireUpstream struct {
	mu   sync.Mutex
	body string
}

// record is the body-recording middleware in front of the stub's handler.
func (u *wireUpstream) record(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posted, _ := io.ReadAll(r.Body)
		u.mu.Lock()
		u.body = string(posted)
		u.mu.Unlock()
		r.Body = io.NopCloser(strings.NewReader(u.body))
		next.ServeHTTP(w, r)
	})
}

// posted returns the recorded request body under the recorder's lock.
func (u *wireUpstream) posted(t *testing.T) string {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.body
}

// seenScript is the one-word reply the Inspector tests stream: short enough to arrive in one
// delta, so the response record's payload lines are known.
func seenScript() stubllm.Script {
	return stubllm.Script{Turns: []stubllm.Turn{{Text: "seen", Repeat: true}}}
}

// newWireUpstream starts the recording Upstream — the recorder in front of seenScript — and
// returns it with its URL.
func newWireUpstream(t *testing.T) (*wireUpstream, string) {
	t.Helper()
	up := &wireUpstream{}
	stub := stubllm.InProcess(t, seenScript())
	srv := httptest.NewServer(up.record(stub.Handler()))
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

	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Endpoint = stubllm.New(t, seenScript()).URL // cfg.Inspector stays false

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

	second := stubllm.New(t, seenScript())
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Endpoint = stubllm.New(t, seenScript()).URL
	cfg.Inspector = true

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.SwitchUpstream(UpstreamSpec{Endpoint: second.URL}); err != nil {
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
// THREADED, which is the only thing the default roster decides. What a real catalog answers is
// internal/skills' question.
type stubSkillLookup struct{}

func (stubSkillLookup) LookupSkill(string) domain.SkillLookupResult {
	return domain.SkillLookupResult{}
}

// TestHostToolsThreadsTheSkillLookupOntoTheDefaultRoster pins the Config → tools.HostToolsOf →
// registry thread for load_skill (ADR 0065 §6). The tool is registered by CONSTRUCTION from this one field,
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

// TestDefaultRosterCarriesTheEmbeddersBackgroundOptIn pins Config.OffersBackground (ADR 0089 D1)
// on the engine's own roster: with fan_out and workflow lifted, the workflow tool is offered and
// fan_out publishes `background` exactly when the embedder set the field — unset is the roster
// before the field existed — and both re-compositions of that roster, a profile edit and a model
// switch, keep what construction chose.
func TestDefaultRosterCarriesTheEmbeddersBackgroundOptIn(t *testing.T) {
	t.Parallel()

	for _, optIn := range []bool{false, true} {
		t.Run(map[bool]string{false: "unset", true: "set"}[optIn], func(t *testing.T) {
			t.Parallel()

			cfg := baseConfig(&recordingSink{})
			cfg.EnabledTools = []string{tools.FanOutToolName, tools.WorkflowToolName}
			cfg.OffersBackground = optIn
			a := rosterAgent(t, cfg)
			assertBackgroundPair(t, a, "at construction", optIn)

			if err := a.SetProfile(domain.ModelProfile{}); err != nil {
				t.Fatalf("SetProfile: %v", err)
			}
			assertBackgroundPair(t, a, "after SetProfile", optIn)

			if err := a.Rebind(RebindSpec{Model: "another-model"}); err != nil {
				t.Fatalf("Rebind: %v", err)
			}
			assertBackgroundPair(t, a, "after Rebind", optIn)
		})
	}
}

// assertBackgroundPair checks the pair ADR 0089 D1/D4 ties together on a's live tool set: the
// workflow tool is offered, and the lifted fan_out publishes `background`, exactly when want.
func assertBackgroundPair(t *testing.T, a *Agent, when string, want bool) {
	t.Helper()

	if got := offers(a, tools.WorkflowToolName); got != want {
		t.Errorf("%s: workflow offered = %v, want %v", when, got, want)
	}
	tool, ok := a.tools.Lookup(tools.FanOutToolName)
	if !ok {
		t.Fatalf("%s: the roster dropped the lifted fan_out", when)
	}
	fanOut, _ := tool.(*tools.FanOut)
	if fanOut == nil {
		t.Fatalf("%s: fan_out is a %T, not *tools.FanOut", when, tool)
	}
	if got := fanOut.OffersBackground(); got != want {
		t.Errorf("%s: fan_out publishes background = %v, want %v", when, got, want)
	}
}
