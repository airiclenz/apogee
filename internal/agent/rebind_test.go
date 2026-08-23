package agent

// Coverage for the full engine rebind (ADR 0024) and the deferred model binding it enables:
// Agent.Rebind swaps the wire model, the system-prompt template, the context window, the
// catalogued Mechanism set and the model profile (ADR 0044) together at a quiescent boundary,
// refuses mid-Exchange, and leaves every binding intact when the new spec fails a gate. The
// white-box package placement is what lets these inject a fake Responder through newAgent and
// read the resulting Budget and parse-seam collaborators directly.

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// registeredIDs lists every catalogued Mechanism in reg across all five hook points, so a test
// can assert WHICH set a rebuilt registry holds without reaching into its unexported storage.
func registeredIDs(reg *domain.MechanismRegistry) []domain.MechanismID {
	seen := map[domain.MechanismID]bool{}
	var ids []domain.MechanismID
	for _, at := range []domain.HookPoint{
		domain.HookPreRequest,
		domain.HookPostResponse,
		domain.HookPreToolExec,
		domain.HookPostToolResult,
		domain.HookHistoryRewrite,
	} {
		for _, m := range reg.Ordered(at) {
			if !seen[m.Descriptor.ID] {
				seen[m.Descriptor.ID] = true
				ids = append(ids, m.Descriptor.ID)
			}
		}
	}
	return ids
}

func hasRegistered(reg *domain.MechanismRegistry, id domain.MechanismID) bool {
	for _, got := range registeredIDs(reg) {
		if got == id {
			return true
		}
	}
	return false
}

// submitAndRun is runExchange without the *testing.T, so a test can drive an Exchange from
// another goroutine (t.Fatalf is legal only on the test goroutine).
func submitAndRun(a *Agent, text string) error {
	if err := a.Submit(domain.UserInput{Text: text}); err != nil {
		return err
	}
	_, err := a.Run(context.Background())
	return err
}

// TestRebindSwapsRequestBindings: after a Rebind the NEXT request carries the new model id and
// the new system prompt, and the Budget measures against the new window — the three per-model
// bindings a heartbeat-observed model switch has to move together.
func TestRebindSwapsRequestBindings(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.SystemPrompt = "you are bound to the old model"
	cfg.Context.MaxContextTokens = 8192
	responder := &captureAllResponder{scripts: [][]provider.Delta{contentScript("first"), contentScript("second")}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "before the switch")

	if err := a.Rebind(RebindSpec{
		Model:            "new-model",
		SystemPrompt:     "you are bound to the new model",
		MaxContextTokens: 16384,
	}); err != nil {
		t.Fatalf("Rebind: %v", err)
	}
	runExchange(t, a, "after the switch")

	if len(responder.got) != 2 {
		t.Fatalf("responder saw %d requests, want 2", len(responder.got))
	}
	before, after := responder.got[0], responder.got[1]

	if before.Model != "test-model" {
		t.Errorf("pre-rebind request model = %q, want %q", before.Model, "test-model")
	}
	if after.Model != "new-model" {
		t.Errorf("post-rebind request model = %q, want %q", after.Model, "new-model")
	}
	if len(after.Messages) == 0 || after.Messages[0].Role != "system" {
		t.Fatalf("post-rebind request has no leading system message: %+v", after.Messages)
	}
	if got := after.Messages[0].Content; got != "you are bound to the new model" {
		t.Errorf("post-rebind system message = %q, want the re-selected template", got)
	}
	if got := a.budget().ContextLimit; got != 16384 {
		t.Errorf("Budget.ContextLimit = %d, want 16384 (the rebound window)", got)
	}
}

// TestRebindRefusedMidExchange: an Exchange left open by a cancel refuses the rebind with
// ErrInputPending (the ClearContext/RestoreSession class), and every binding stands.
func TestRebindRefusedMidExchange(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.SystemPrompt = "the standing prompt"
	cfg.Context.MaxContextTokens = 8192
	cfg.EnableMechanisms = []domain.MechanismID{"empty_response_recovery"}
	responder := blockingResponder{started: make(chan struct{})}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "slow"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-responder.started
		cancel()
	}()
	if _, err := a.Step(ctx); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if !a.InExchange() {
		t.Fatal("the cancelled Exchange is not open; the refusal below would prove nothing")
	}

	registry := a.registry
	err = a.Rebind(RebindSpec{Model: "new-model", SystemPrompt: "the new prompt", MaxContextTokens: 16384})
	if !errors.Is(err, domain.ErrInputPending) {
		t.Errorf("Rebind mid-Exchange err = %v, want ErrInputPending", err)
	}
	if a.cfg.Model != "test-model" {
		t.Errorf("model = %q after a refused Rebind, want it unchanged", a.cfg.Model)
	}
	if a.cfg.SystemPrompt != "the standing prompt" {
		t.Errorf("system prompt = %q after a refused Rebind, want it unchanged", a.cfg.SystemPrompt)
	}
	if got := a.budget().ContextLimit; got != 8192 {
		t.Errorf("Budget.ContextLimit = %d after a refused Rebind, want 8192", got)
	}
	if a.registry != registry {
		t.Error("the Mechanism registry was rebuilt by a refused Rebind")
	}
}

// TestRebindRebuildsMechanismsForNewModel: a new EnableMechanisms set replaces the registry, and
// a set that fails a gate (a half Requires stack) leaves the OLD registry and cfg fully intact —
// the validate-then-commit guarantee.
func TestRebindRebuildsMechanismsForNewModel(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.EnableMechanisms = []domain.MechanismID{"empty_response_recovery"}

	a, err := newAgent(cfg, echoResponder{reply: "unreached"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seeded := a.registry

	if err := a.Rebind(RebindSpec{
		Model:            "second-model",
		EnableMechanisms: []domain.MechanismID{"validate"},
	}); err != nil {
		t.Fatalf("Rebind: %v", err)
	}
	if a.registry == seeded {
		t.Error("Rebind kept the seeded registry; the new model's set was never built")
	}
	if !hasRegistered(a.registry, "validate") {
		t.Errorf("rebuilt registry = %v, want it to hold the newly enabled validate", registeredIDs(a.registry))
	}
	if hasRegistered(a.registry, "empty_response_recovery") {
		t.Errorf("rebuilt registry = %v, want the previous model's set gone", registeredIDs(a.registry))
	}

	// guided_decomposition Requires tool_result_cap: enabling it alone must fail the requirements
	// gate BEFORE anything is committed.
	rebuilt := a.registry
	err = a.Rebind(RebindSpec{
		Model:            "third-model",
		EnableMechanisms: []domain.MechanismID{"guided_decomposition"},
	})
	if !errors.Is(err, domain.ErrMissingRequirement) {
		t.Fatalf("Rebind with a half stack err = %v, want it to wrap ErrMissingRequirement", err)
	}
	if a.registry != rebuilt {
		t.Error("a failed Rebind swapped the registry")
	}
	if a.cfg.Model != "second-model" {
		t.Errorf("model = %q after a failed Rebind, want %q", a.cfg.Model, "second-model")
	}
}

// TestRebindKeepsConversation: the conversation is session state, not a per-model binding — a
// model switch mid-session must not cost the user their history.
func TestRebindKeepsConversation(t *testing.T) {
	responder := &captureAllResponder{scripts: [][]provider.Delta{contentScript("kept")}}
	a, err := newAgent(baseConfig(&recordingSink{}), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "remember this")
	before := a.conv.Messages()

	if err := a.Rebind(RebindSpec{Model: "new-model"}); err != nil {
		t.Fatalf("Rebind: %v", err)
	}

	after := a.conv.Messages()
	if len(after) != len(before) {
		t.Fatalf("conversation length = %d after Rebind, want %d", len(after), len(before))
	}
	for i := range before {
		if after[i].Role != before[i].Role || after[i].Content != before[i].Content {
			t.Errorf("message %d = %+v after Rebind, want %+v", i, after[i], before[i])
		}
	}
}

// TestRebindBetweenExchangesRaceClean drives two full Exchanges on worker goroutines with a
// Rebind between them, handed off through a channel exactly as the host's terminal fold does.
// It exists to run under -race: the boundary — not a lock on cfg — is what makes the loop's
// un-mutexed cfg reads safe against Rebind's writes (ADR 0024).
func TestRebindBetweenExchangesRaceClean(t *testing.T) {
	a, err := newAgent(baseConfig(&recordingSink{}), echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	first := make(chan error, 1)
	go func() { first <- submitAndRun(a, "one") }()
	if err := <-first; err != nil {
		t.Fatalf("first Exchange: %v", err)
	}

	if err := a.Rebind(RebindSpec{Model: "new-model", SystemPrompt: "fresh", MaxContextTokens: 4096}); err != nil {
		t.Fatalf("Rebind: %v", err)
	}

	second := make(chan error, 1)
	go func() { second <- submitAndRun(a, "two") }()
	if err := <-second; err != nil {
		t.Fatalf("second Exchange: %v", err)
	}

	if a.cfg.Model != "new-model" {
		t.Errorf("model = %q after the rebind, want %q", a.cfg.Model, "new-model")
	}
}

// TestNewAllowsEmptyModelSubmitRefuses: construction no longer requires a model (the async
// startup relaxation), Submit refuses while nothing is bound, and the first Rebind — the late
// seed — makes the Agent submittable.
func TestNewAllowsEmptyModelSubmitRefuses(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.Model = ""

	a, err := newAgent(cfg, echoResponder{reply: "late"})
	if err != nil {
		t.Fatalf("newAgent with an empty Model: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "too early"}); !errors.Is(err, errNoModelBound) {
		t.Errorf("Submit with no model bound err = %v, want errNoModelBound", err)
	}

	if err := a.Rebind(RebindSpec{Model: "late-seeded", MaxContextTokens: 32768}); err != nil {
		t.Fatalf("late-seeding Rebind: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "now it flows"}); err != nil {
		t.Errorf("Submit after the late seed: %v", err)
	}
}

// TestRebindRefusesUnbuildableSpecs covers the two specs Rebind cannot honour: one naming no
// model (Rebind binds, it never unbinds) and one on an Agent whose Mechanism registry the host
// supplied pre-built (it cannot be rebuilt for a new model without dropping the host's hooks).
func TestRebindRefusesUnbuildableSpecs(t *testing.T) {
	t.Run("empty model", func(t *testing.T) {
		a, err := newAgent(baseConfig(&recordingSink{}), echoResponder{reply: "unreached"})
		if err != nil {
			t.Fatalf("newAgent: %v", err)
		}
		if err := a.Rebind(RebindSpec{Model: ""}); !errors.Is(err, errRebindMissingModel) {
			t.Errorf("Rebind with no model err = %v, want errRebindMissingModel", err)
		}
		if a.cfg.Model != "test-model" {
			t.Errorf("model = %q after the refusal, want it unchanged", a.cfg.Model)
		}
	})

	t.Run("host-supplied registry", func(t *testing.T) {
		cfg := baseConfig(&recordingSink{})
		fired := false
		cfg.Mechanisms = domain.NewMechanismRegistry()
		if err := cfg.Mechanisms.AddExperimental(domain.HookPreRequest, firingHook{fired: &fired}); err != nil {
			t.Fatalf("AddExperimental: %v", err)
		}

		a, err := newAgent(cfg, echoResponder{reply: "unreached"})
		if err != nil {
			t.Fatalf("newAgent: %v", err)
		}
		if err := a.Rebind(RebindSpec{Model: "new-model"}); !errors.Is(err, errRebindPrebuiltMechanisms) {
			t.Errorf("Rebind over a pre-built registry err = %v, want errRebindPrebuiltMechanisms", err)
		}
		if a.cfg.Model != "test-model" {
			t.Errorf("model = %q after the refusal, want it unchanged", a.cfg.Model)
		}
	})

	// The profile is the LAST thing a spec can fail on, so it is the case that proves the new
	// binding did not sneak a half-commit in behind the gates that passed.
	t.Run("untranslatable profile", func(t *testing.T) {
		live := domain.ModelProfile{ToolCallFormat: domain.FormatMarkdownFenced}
		cfg := baseConfig(&recordingSink{})
		cfg.Profile = live

		a, err := newAgent(cfg, echoResponder{reply: "unreached"})
		if err != nil {
			t.Fatalf("newAgent: %v", err)
		}
		spec := RebindSpec{
			Model:   "new-model",
			Profile: domain.ModelProfile{Thinking: domain.ThinkingProfile{Style: domain.ThinkingStyle("telepathy")}},
		}

		if err := a.Rebind(spec); err == nil {
			t.Fatal("Rebind accepted a profile processing.ParserFor refuses")
		}
		if a.cfg.Model != "test-model" {
			t.Errorf("model = %q after the refusal, want it unchanged", a.cfg.Model)
		}
		if !reflect.DeepEqual(a.cfg.Profile, live) {
			t.Errorf("cfg.Profile = %+v after the refusal, want the live one %+v", a.cfg.Profile, live)
		}
		if _, found := a.textParser.ParseToolCall(fencedReadFileCall); !found {
			t.Error("the live parser stopped recovering fenced calls after a refused rebind")
		}
	})
}

// TestRebindSwapsTheModelProfile: a model switch carries the new model's dialect with it now
// (ADR 0044, reversing RebindSpec's old exclusion) — the inline thinking channel the departed
// model never spoke is lifted out of the NEXT response's visible content, and cfg.Profile moves
// with the collaborators so the emit half follows.
func TestRebindSwapsTheModelProfile(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	a := newProfileAgent(t, cfg, echoResponder{reply: "<mm:think>weighing it up</mm:think>The answer is 42."})

	before := answerOnce(t, a, "think about it")
	if !strings.Contains(before.Content, "weighing it up") {
		t.Fatalf("the zero profile stripped an inline channel it does not know: %q", before.Content)
	}

	profile := domain.ModelProfile{Thinking: domain.ThinkingProfile{
		Style: domain.ThinkingDelimited,
		Start: "<mm:think>",
		End:   "</mm:think>",
	}}
	if err := a.Rebind(RebindSpec{Model: "minimax-m3", Profile: profile}); err != nil {
		t.Fatalf("Rebind: %v", err)
	}

	after := answerOnce(t, a, "think about it again")
	if strings.Contains(after.Content, "weighing it up") {
		t.Errorf("the rebound model's thinking channel survived in visible content: %q", after.Content)
	}
	if !strings.Contains(after.Content, "The answer is 42.") {
		t.Errorf("the rebound stripper ate the visible answer: %q", after.Content)
	}
	assertReasoning(t, after, "weighing it up")
	if !reflect.DeepEqual(a.cfg.Profile, profile) {
		t.Errorf("cfg.Profile = %+v after the rebind, want the spec's %+v", a.cfg.Profile, profile)
	}
}

// TestRebindToZeroProfileResetsTheParsers: the zero Profile is a meaningful value — the native,
// no-inline-thinking default a model that matches no entry gets — so rebinding to it must UNDO the
// departed model's dialect rather than leave its stripper installed against a model that does not
// speak it.
func TestRebindToZeroProfileResetsTheParsers(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.Profile = domain.ModelProfile{
		ToolCallFormat: domain.FormatMarkdownFenced,
		Thinking: domain.ThinkingProfile{
			Style: domain.ThinkingDelimited,
			Start: "<think>",
			End:   "</think>",
		},
	}
	a := newProfileAgent(t, cfg, echoResponder{reply: "<think>weighing it up</think>The answer is 42."})

	before := answerOnce(t, a, "think about it")
	if strings.Contains(before.Content, "weighing it up") {
		t.Fatalf("the delimited profile did not strip its own channel: %q", before.Content)
	}

	if err := a.Rebind(RebindSpec{Model: "native-model"}); err != nil {
		t.Fatalf("Rebind: %v", err)
	}

	after := answerOnce(t, a, "think about it again")
	if !strings.Contains(after.Content, "<think>weighing it up</think>") {
		t.Errorf("the departed model's stripper is still installed: %q", after.Content)
	}
	if _, found := a.textParser.ParseToolCall(fencedReadFileCall); found {
		t.Error("the departed model's text-format tool-call parser is still installed")
	}
	if !reflect.DeepEqual(a.cfg.Profile, domain.ModelProfile{}) {
		t.Errorf("cfg.Profile = %+v after a zero-profile rebind, want the zero profile", a.cfg.Profile)
	}
}

// TestRebindCarriesTheReplyCeiling: the reply ceiling is a fact about the SERVER rather than about
// the model, and it rides RebindSpec anyway (ADR 0046) — the `max-output-tokens:` pin has no engine
// setter of its own, so a live edit of it reaches the engine only through the re-resolution the
// composition root is already driving.
//
// The three states the field has are the three the pin has, and the middle one is what makes
// carrying it safe at all: a spec SILENT about the ceiling leaves the bound one standing, so a
// rebind driven for a model change — or by any caller that resolved the per-model bindings alone —
// can never un-bound a reply the operator bounded.
func TestRebindCarriesTheReplyCeiling(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.Context.MaxContextTokens = 98304
	cfg.Context.MaxOutputTokens = 2048
	responder := &captureAllResponder{scripts: [][]provider.Delta{contentScript("bounded")}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	// Stated: the ceiling binds, and the very NEXT request states that number on the wire. The
	// assertion is the wire's own max_tokens rather than the field, because the field is only
	// interesting insofar as the server is told about it.
	pinned := 8192
	if err := a.Rebind(RebindSpec{
		Model: "new-model", MaxContextTokens: 98304, MaxOutputTokens: &pinned,
	}); err != nil {
		t.Fatalf("Rebind with a stated ceiling: %v", err)
	}
	if got := a.maxOutputTokens(); got != 8192 {
		t.Errorf("reply cap = %d after the rebind, want the spec's 8192", got)
	}
	runExchange(t, a, "answer within the edited ceiling")
	if len(responder.got) != 1 {
		t.Fatalf("responder saw %d requests, want 1", len(responder.got))
	}
	sent := responder.got[0].Sampling.MaxTokens
	if sent == nil {
		t.Fatalf("the request carried a nil max_tokens — the rebound ceiling reached the engine only halfway")
	}
	if *sent != 8192 {
		t.Errorf("wire max_tokens = %d, want the rebound 8192", *sent)
	}

	// Silent: the ceiling stands. This is the invariant ADR 0046 turns on — the ceiling describes the
	// server, and a spec that re-resolved only the per-model bindings has said nothing about it.
	if err := a.Rebind(RebindSpec{Model: "another-model", MaxContextTokens: 98304}); err != nil {
		t.Fatalf("Rebind with no ceiling stated: %v", err)
	}
	if got := a.maxOutputTokens(); got != 8192 {
		t.Errorf("reply cap = %d after a spec silent about it, want the 8192 still in force — a nil "+
			"field must leave the bound ceiling untouched, never clear it", got)
	}

	// The stated ZERO: the operator DROPPED the pin, which is a statement and not silence. The engine
	// derives the cap from the reply room the Budget already reserves (98,304 × 0.20), never "no cap".
	dropped := 0
	if err := a.Rebind(RebindSpec{
		Model: "third-model", MaxContextTokens: 98304, MaxOutputTokens: &dropped,
	}); err != nil {
		t.Fatalf("Rebind with the pin dropped: %v", err)
	}
	if got := a.maxOutputTokens(); got != 19660 {
		t.Errorf("reply cap = %d after the pin was dropped, want the derived 19660", got)
	}
}

// TestRebindCarriesTheResponseReserveShare: how the bound window is SPLIT for the reply is the
// second bound that describes the SERVER rather than the model, and it rides RebindSpec for the
// reply ceiling's reason — the `response-reserve:` pin has no engine setter of its own, so a live
// edit of it reaches the engine only through the re-resolution the composition root is already
// driving.
//
// Same three states as the ceiling beside it, asserted through the Budget because the share is only
// interesting insofar as it moves the tokens actually held back: stated binds, SILENT leaves the
// share in force standing (so a caller that re-resolved only the per-model bindings can never
// re-divide a window an entry pinned), and a stated ZERO is the operator dropping the pin — the
// split falls to the engine's own built-in fifth, not to the departed entry's number.
func TestRebindCarriesTheResponseReserveShare(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.Context.MaxContextTokens = 100000

	a, err := newAgent(cfg, &modelBindingResponder{reply: "unreached"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	// Stated: the entry's share binds, and the Budget holds back exactly that much of the window.
	stated := 0.35
	if err := a.Rebind(RebindSpec{
		Model: "new-model", MaxContextTokens: 100000, ResponseReserveFraction: &stated,
	}); err != nil {
		t.Fatalf("Rebind with a stated share: %v", err)
	}
	if got := a.budget().ResponseReserve; got != 35000 {
		t.Errorf("reply reserve = %d after the rebind, want the spec's 35%% of 100,000", got)
	}

	// Silent: the share stands. This is the invariant the pointer exists for — the share describes the
	// server, and a spec that re-resolved only the per-model bindings has said nothing about it.
	if err := a.Rebind(RebindSpec{Model: "another-model", MaxContextTokens: 100000}); err != nil {
		t.Fatalf("Rebind with no share stated: %v", err)
	}
	if got := a.budget().ResponseReserve; got != 35000 {
		t.Errorf("reply reserve = %d after a spec silent about it, want the 35,000 still in force — a "+
			"nil field must leave the bound share untouched, never re-divide the window", got)
	}

	// The stated ZERO: the operator DROPPED the pin, which is a statement and not silence. Allocate
	// applies apogee's own 0.20 default rather than keeping the share the previous entry stated.
	dropped := 0.0
	if err := a.Rebind(RebindSpec{
		Model: "third-model", MaxContextTokens: 100000, ResponseReserveFraction: &dropped,
	}); err != nil {
		t.Fatalf("Rebind with the pin dropped: %v", err)
	}
	if got := a.budget().ResponseReserve; got != 20000 {
		t.Errorf("reply reserve = %d after the pin was dropped, want the built-in fifth of 100,000", got)
	}
}
