package probe_test

import (
	"context"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/probe"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/stubllm"
)

// The report's probe authenticates exactly as a session would: a keyed server rejects an
// unauthenticated GET /v1/models, so a probe without the key would diagnose a 401 the binary
// itself would never hit. BOTH discovery requests carry it — /props is keyed too, and the
// stub's key gate answers an unkeyed probe 401 before it reaches the log.
func TestDiscoverSendsAPIKey(t *testing.T) {
	t.Parallel()
	srv := stubllm.New(t,
		stubllm.Script{Discovery: stubllm.Discovery{Models: openAIModels, Props: llamaCppProps}},
		stubllm.WithAPIKey("tok"))

	d := probe.Discover(context.Background(), srv.URL, "tok", provider.WireOpenAI)

	if !d.Reached {
		t.Fatalf("Reached = false against a keyed server (Failure = %q)", d.Failure)
	}
	probes := srv.Probes()
	if len(probes) < 2 {
		t.Fatalf("the probe made %d request(s), want both /v1/models and /props", len(probes))
	}
	for i, p := range probes {
		if got := p.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("request %d (%s) carried Authorization %q, want %q", i, p.Path, got, "Bearer tok")
		}
	}
}

// The keyless local server stays exactly as it was: an empty key sends no Authorization header
// at all, not an empty one a strict server could reject.
func TestDiscoverWithoutAPIKeySendsNoAuthHeader(t *testing.T) {
	t.Parallel()
	srv := stubllm.New(t, stubllm.Script{Discovery: stubllm.Discovery{Models: openAIModels, Props: llamaCppProps}})

	if d := probe.Discover(context.Background(), srv.URL, "", provider.WireOpenAI); !d.Reached {
		t.Fatalf("Reached = false (Failure = %q)", d.Failure)
	}
	probes := srv.Probes()
	if len(probes) == 0 {
		t.Fatal("the probe made no request at all")
	}
	for i, p := range probes {
		if _, ok := p.Header["Authorization"]; ok {
			t.Errorf("request %d (%s) carried an Authorization header on an empty api key", i, p.Path)
		}
	}
}

// `apogee probe` dials with the entry's wire, exactly as a session's Monitor does: an anthropic
// entry's discovery carries the Messages API's headers — `x-api-key`, `anthropic-version`, no
// bearer token — and asks for no /props, which nothing on that wire serves. The report then names
// the header the key travelled in and says the /props probe was not made, rather than reporting
// an outcome for a probe that never ran.
func TestDiscoverOnTheAnthropicWireCarriesItsHeadersAndSkipsProps(t *testing.T) {
	t.Parallel()
	// /props is scripted too, so a probe that asked for it would be caught in the log.
	srv := stubllm.New(t,
		stubllm.Script{Discovery: stubllm.Discovery{
			Models: []stubllm.DiscoveredModel{{ID: "claude-x", DisplayName: "Claude X"}},
			Props:  llamaCppProps,
		}},
		stubllm.WithAPIKey("tok"))

	host := probe.GatherHost(context.Background(), probe.Inputs{
		Endpoint: srv.URL, APIKey: "tok", Wire: provider.WireAnthropic,
	})

	d := host.Discovery
	if !d.Reached || d.ActiveModel != "claude-x" || d.Wire != provider.WireAnthropic {
		t.Fatalf("Discovery = %+v, want reached, claude-x active, on the anthropic wire", d)
	}
	probes := srv.Probes()
	if len(probes) != 1 || probes[0].Path != "/v1/models" {
		t.Fatalf("the probe asked %+v, want exactly one GET /v1/models and no /props", probes)
	}
	header := probes[0].Header
	if header.Get("x-api-key") != "tok" || header.Get("anthropic-version") == "" || header.Get("Authorization") != "" {
		t.Errorf("x-api-key=%q anthropic-version=%q Authorization=%q; want the anthropic headers and no bearer",
			header.Get("x-api-key"), header.Get("anthropic-version"), header.Get("Authorization"))
	}
	report := host.Report()
	for _, want := range []string{
		"api key:       configured (sent as x-api-key)",
		"/props:        not probed (the anthropic wire has no /props",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report lacks %q:\n%s", want, report)
		}
	}
	if strings.Contains(report, "bearer token") {
		t.Errorf("report claims a bearer token on the anthropic wire:\n%s", report)
	}
}
