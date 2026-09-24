package main

// A server entry's `request-extra:` passthrough (ADR 0085) reaches every request apogee sends to
// that entry, and never a request to another one. The merge itself is the provider Client's and is
// pinned there (internal/provider); what these tests pin is the ROPE — that every Client built for
// an entry is handed that entry's value: the session's own, the one a `/server` move builds, a
// routed delegation's, the out-of-band naming calls' and `apogee probe model`'s battery.
//
// The witness is a top-level `witness` key each entry's passthrough names with its own value, read
// back off the body the scripted upstream logged (stubllm's Request.Body), so a key that rode to
// the wrong server — or stayed behind after a move — is told apart by what it says.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The witness values the two entries' passthroughs carry, and the canonical JSON each one is.
const (
	sessionWitness = "workstation"
	targetWitness  = "grunt"

	sessionExtra = `{"witness":"workstation"}`
	targetExtra  = `{"witness":"grunt"}`
)

// requestExtraWitness is the top-level `witness` key of a logged body, or "" when the body carries
// none. A body that is not a JSON object fails the test: every body apogee sends is one.
func requestExtraWitness(t *testing.T, body []byte) string {
	t.Helper()

	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		t.Fatalf("decode a logged request body: %v\n%s", err, body)
	}
	raw, ok := top["witness"]
	if !ok {
		return ""
	}
	var witness string
	if err := json.Unmarshal(raw, &witness); err != nil {
		t.Fatalf("the witness key is not a string: %s", raw)
	}
	return witness
}

// assertEveryWitness fails for each request the server answered whose body does not carry want as
// its witness ("" ⇒ carries none), and when the server answered nothing at all.
func assertEveryWitness(t *testing.T, stub *stubllm.Server, who, want string) {
	t.Helper()

	requests := stub.Requests()
	if len(requests) == 0 {
		t.Fatalf("the %s server answered no request; the case asserts about none", who)
	}
	for _, req := range requests {
		if got := requestExtraWitness(t, req.Body); got != want {
			t.Errorf("request %d to the %s server (last message %q) carried witness %q; want %q",
				req.N, who, lastMessageOf(req), got, want)
		}
	}
}

// lastMessageOf is the text of the request's last message, for a failure to name which call it was.
func lastMessageOf(req stubllm.Request) string {
	if len(req.Messages) == 0 {
		return ""
	}
	return req.Messages[len(req.Messages)-1].Content
}

// requestExtraCases are the two shapes the second entry takes in every driven case below: one with
// a passthrough of its own, and one that names none — the case that proves a value is REPLACED
// rather than inherited, since a child or a move that kept the session entry's keys would carry
// them here.
var requestExtraCases = []struct {
	name        string
	targetExtra string
	wantTarget  string
}{
	{name: "target names its own", targetExtra: targetExtra, wantTarget: targetWitness},
	{name: "target names none", targetExtra: "", wantTarget: ""},
}

// requestExtraHome writes seatHome's two-server home under `sub-agents-choice: model`, with the
// session entry's passthrough and — when targetJSON is not "" — the target entry's.
func requestExtraHome(t *testing.T, session, target *stubllm.Server, targetJSON string) string {
	t.Helper()

	body := "system-prompt-text: |\n" +
		"  You are apogee, a terminal coding agent.\n" +
		"sub-agents-choice: " + seatChoiceModel + "\n" +
		"sub-agents-server: " + seatTargetServer + "\n" +
		"servers:\n" +
		"  - name: " + seatSessionServer + "\n" +
		"    endpoint: " + session.URL + "\n" +
		"    model: " + session.Model + "\n" +
		"    request-extra: " + sessionExtra + "\n" +
		"  - name: " + seatTargetServer + "\n" +
		"    endpoint: " + target.URL + "\n" +
		"    model: " + target.Model + "\n"
	if targetJSON != "" {
		body += "    request-extra: " + targetJSON + "\n"
	}
	body += "server: " + seatSessionServer + "\n"
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write the two-server home's config: %v", err)
	}
	return home
}

// launchRequestExtraSession starts a driven session on requestExtraHome's session entry.
func launchRequestExtraSession(t *testing.T, targetJSON string) seatRun {
	t.Helper()

	session := stubllm.New(t, loadScript(t, "seat-session"))
	target := stubllm.New(t, loadScript(t, "seat-target"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIOn(t, drv, session, requestExtraHome(t, session, target, targetJSON), "")
	waitIdle(drv)
	drv.WaitQuiet(settled)
	return seatRun{session: session, target: target, sess: sess, drv: drv}
}

// TestE2ERequestExtraRidesTheTurnAndARoutedDelegation: the session entry's passthrough rides every
// request the session server answers — the Turns and the title call alike — and a delegation routed
// to the second entry carries THAT entry's value, never the session's.
func TestE2ERequestExtraRidesTheTurnAndARoutedDelegation(t *testing.T) {
	t.Parallel()

	for _, tc := range requestExtraCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			run := launchRequestExtraSession(t, tc.targetExtra)
			submit(run.drv, seatPlainPrompt)
			run.drv.WaitText(seatPlainReply)

			awaitNotice(t, run.drv, "sub-agents: routing to "+seatTargetServer)
			submit(run.drv, seatSubAgentPrompt)
			run.drv.WaitText(seatWrapUp)
			run.drv.WaitQuiet(settled)

			if got := childRequests(run.target, seatFarTask); got != 1 {
				t.Fatalf("the sub-agents server answered %d of the child's requests; want the one routed there", got)
			}
			assertEveryWitness(t, run.session, "session", sessionWitness)
			assertEveryWitness(t, run.target, "sub-agents", tc.wantTarget)
			run.quit(t)
		})
	}
}

// TestE2ERequestExtraFollowsAServerSwitch: a `/server` move builds a Client for the arrived-at
// entry, carrying that entry's passthrough — and, for an entry that names none, nothing of the
// retired one's.
func TestE2ERequestExtraFollowsAServerSwitch(t *testing.T) {
	t.Parallel()

	for _, tc := range requestExtraCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			run := launchRequestExtraSession(t, tc.targetExtra)
			// The model the target advertises binding is what says the move's first beat landed; a
			// send before it is refused with a note (ADR 0024).
			submit(run.drv, "/server "+seatTargetServer)
			run.drv.WaitText(run.target.Model)
			submit(run.drv, seatPlainPrompt)
			run.drv.WaitFor(func() bool {
				for _, req := range run.target.Requests() {
					if strings.HasPrefix(lastMessageOf(req), seatPlainPrompt) {
						return true
					}
				}
				return false
			}, tuitest.Awaiting("the moved session's Turn on the target server"))
			run.drv.WaitQuiet(settled)

			assertEveryWitness(t, run.target, "arrived-at", tc.wantTarget)
			run.quit(t)
		})
	}
}

// TestTitleGeneratorRequestExtraFollowsTheHoldersBinding: the session-naming call is built from the
// holder's binding at call time, so it carries the bound entry's passthrough — and after a Swap to
// an entry that names none, carries none.
func TestTitleGeneratorRequestExtraFollowsTheHoldersBinding(t *testing.T) {
	t.Parallel()

	first := stubllm.New(t, stubllm.Script{Model: "model-a", Turns: []stubllm.Turn{{Repeat: true, Text: "fix the parser"}}})
	second := stubllm.New(t, stubllm.Script{Model: "model-b", Turns: []stubllm.Turn{{Repeat: true, Text: "rename the rows"}}})

	holder := newUpstreamHolder()
	holder.Bind(first.URL, "", "model-a", "", sessionExtra, nil)
	wiring := newTitleWiring(holder.Binding, noDialect, "/home/dev/apogee")

	if _, err := wiring.generate(context.Background(), []string{"the parser test fails"}); err != nil {
		t.Fatalf("generate on the first entry: %v", err)
	}
	assertEveryWitness(t, first, "first", sessionWitness)

	holder.Swap(second.URL, "", "", "", nil)
	if _, err := wiring.generate(context.Background(), []string{"the rows are unreadable"}); err != nil {
		t.Fatalf("generate after the swap: %v", err)
	}
	assertEveryWitness(t, second, "second", "")
}

// TestDelegationWiringRecordsTheTargetsRequestExtraForNaming: the binding a landed target leaves
// for the routed naming call carries the target's passthrough, and a naming call built from it
// sends it.
func TestDelegationWiringRecordsTheTargetsRequestExtraForNaming(t *testing.T) {
	t.Parallel()

	grunt := stubllm.New(t, stubllm.Script{Model: "cheap-7b", Turns: []stubllm.Turn{{Repeat: true, Text: "grep the keys"}}})
	entries := []config.ServerEntry{{Name: "grunt", Endpoint: grunt.URL, Model: "cheap-7b"}}
	wiring := newDelegationWiring(
		"grunt", staticServerList(entries), &delegationSpy{}, noProfiles, nil, config.NewKeyResolver(""))

	wiring.land(wiring.generation, "grunt", &apogee.DelegationTarget{
		Endpoint: grunt.URL, Model: "cheap-7b", ServerName: "grunt", RequestExtra: targetExtra,
	}, nil)

	binding, _, ok := wiring.routedBinding()
	if !ok {
		t.Fatal("routedBinding reports no target after a landing")
	}
	if binding.RequestExtra != targetExtra {
		t.Errorf("routedBinding.RequestExtra = %q; want the landed target's %q", binding.RequestExtra, targetExtra)
	}

	namer := namerOn(constUpstream(upstreamBinding{Endpoint: "http://127.0.0.1:1"}, provider.EffortDialectNone), wiring.routedBinding)
	if _, err := namer.NameDelegation(context.Background(),
		domain.DelegationNaming{Task: "search every config key", Routed: true}); err != nil {
		t.Fatalf("NameDelegation for a routed child: %v", err)
	}
	assertEveryWitness(t, grunt, "sub-agents", targetWitness)
}

// TestFiringConfigCarriesTheEntrysRequestExtra: an unattended run's Config carries its bound
// entry's passthrough (ADR 0031's Driver parity), its routed target the Sub-agent entry's, and the
// Firing namer sends each server's own value on the naming call it makes there.
func TestFiringConfigCarriesTheEntrysRequestExtra(t *testing.T) {
	primary := stubllm.New(t, stubllm.Script{Model: "entry-model", Turns: []stubllm.Turn{{Repeat: true, Text: "session name"}}})
	grunt := stubllm.New(t, stubllm.Script{Model: "grunt-model", Turns: []stubllm.Turn{{Repeat: true, Text: "grunt name"}}})

	entry := config.ServerEntry{
		Name: "box", Endpoint: primary.URL, Model: "entry-model", RequestExtra: config.RequestExtra(sessionExtra),
	}
	gruntEntry := config.ServerEntry{
		Name: "grunt", Endpoint: grunt.URL, Model: "grunt-model", RequestExtra: config.RequestExtra(targetExtra),
	}
	cfg, routing, _, err := firingConfig(context.Background(), firingInputs{
		opts: config.Options{
			Bypass:          true,
			AutoTitle:       true,
			Servers:         []config.ServerEntry{entry, gruntEntry},
			SubAgentsServer: "grunt",
		},
		entry:    entry,
		roots:    firingRoots(t),
		confiner: fenceableHost,
		mode:     domain.ModePlan,
		recordID: "2026-09-24T10-00-00-firing",
	})
	if err != nil {
		t.Fatalf("firingConfig: %v", err)
	}

	if cfg.RequestExtra != sessionExtra {
		t.Errorf("Config.RequestExtra = %q; want the bound entry's %q", cfg.RequestExtra, sessionExtra)
	}
	if routing.target == nil {
		t.Fatal("no routed target resolved from the reachable grunt entry")
	}
	if routing.target.RequestExtra != targetExtra || routing.target.ServerName != "grunt" {
		t.Errorf("routed target = (name %q, request-extra %q); want the grunt entry's own (%q, %q)",
			routing.target.ServerName, routing.target.RequestExtra, "grunt", targetExtra)
	}

	for _, routed := range []bool{false, true} {
		if _, err := cfg.Namer.NameDelegation(context.Background(),
			domain.DelegationNaming{Task: "grep every config key", Routed: routed}); err != nil {
			t.Fatalf("NameDelegation (routed %v): %v", routed, err)
		}
	}
	assertEveryWitness(t, primary, "run's own", sessionWitness)
	assertEveryWitness(t, grunt, "sub-agents", targetWitness)
}

// TestFiringNamerSpeaksTheEntrysWire: the Firing namer's session binding carries the bound entry's
// wire and passthrough, so an unattended run on a `wire: anthropic` entry names its delegations over
// the Messages API — not the chat-completions path the entry does not speak — with the entry's keys.
func TestFiringNamerSpeaksTheEntrysWire(t *testing.T) {
	primary := stubllm.New(t, stubllm.Script{Model: "claude", Turns: []stubllm.Turn{{Repeat: true, Text: "session name"}}})

	entry := config.ServerEntry{
		Name: "box", Endpoint: primary.URL, Model: "claude", APIKey: "sk-test", Wire: "anthropic",
		RequestExtra: config.RequestExtra(sessionExtra),
	}
	cfg, _, _, err := firingConfig(context.Background(), firingInputs{
		opts:     config.Options{Bypass: true, AutoTitle: true, Servers: []config.ServerEntry{entry}},
		entry:    entry,
		apiKey:   "sk-test",
		roots:    firingRoots(t),
		confiner: fenceableHost,
		mode:     domain.ModePlan,
		recordID: "2026-09-24T11-00-00-firing",
	})
	if err != nil {
		t.Fatalf("firingConfig: %v", err)
	}
	if _, err := cfg.Namer.NameDelegation(context.Background(),
		domain.DelegationNaming{Task: "grep every config key"}); err != nil {
		t.Fatalf("NameDelegation: %v", err)
	}

	requests := primary.Requests()
	if len(requests) != 1 {
		t.Fatalf("the run's server answered %d requests; want the one naming call", len(requests))
	}
	if requests[0].Wire != stubllm.WireAnthropic {
		t.Errorf("the naming call arrived on the %q wire; want the entry's %q", requests[0].Wire, stubllm.WireAnthropic)
	}
	assertEveryWitness(t, primary, "run's own", sessionWitness)
}

// TestProbeModelRequestExtraRidesTheBattery: `apogee probe model` measures the model under the body
// a session on the startup entry sends it, so its battery carries that entry's passthrough.
func TestProbeModelRequestExtraRidesTheBattery(t *testing.T) {
	t.Parallel()

	srv := stubllm.New(t, stubllm.Script{Model: "battery-model", Turns: []stubllm.Turn{{Repeat: true, Text: "ok"}}})
	configHome := t.TempDir()
	body := "servers:\n" +
		"  - name: probe-target\n" +
		"    endpoint: " + srv.URL + "\n" +
		"    request-extra: " + sessionExtra + "\n" +
		"server: probe-target\n"
	if err := os.WriteFile(filepath.Join(configHome, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	_ = runProbeModel(t, configHome, "--model", "battery-model", "--no-save")

	assertEveryWitness(t, srv, "probed", sessionWitness)
}
