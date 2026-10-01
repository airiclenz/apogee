package main

// The dial facts build the Client and the Monitor (dial.go). Each test pins what one of the two
// carries, against a scripted upstream that answers only the key the binding names — so a build
// that dropped the key fails the call itself rather than an assertion about it.

import (
	"context"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/stubllm"
)

// dialKey is the key every stub in this file requires.
const dialKey = "dial-s3cret"

// The Client carries the key, the wire and the request-extra passthrough: the request reaches a
// server that requires the key, arrives on the Messages wire the binding names, and its body
// carries the passthrough's witness. The forced dialect the binding also names is the Monitor's
// and changes nothing here.
func TestDial_ClientCarriesKeyWireAndRequestExtra(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, stubllm.Script{Model: "dial-model", Turns: []stubllm.Turn{{Repeat: true, Text: "ok"}}},
		stubllm.WithAPIKey(dialKey))
	binding := upstreamBinding{
		Endpoint: stub.URL, Model: "dial-model", APIKey: dialKey, Wire: string(provider.WireAnthropic),
		RequestExtra: `{"witness":"dial"}`,
	}

	_, err := binding.Client(provider.WithMaxRetries(0)).Respond(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: "user", Content: "say ok"}},
	})
	if err != nil {
		t.Fatalf("Respond on the binding's Client: %v", err)
	}

	requests := stub.Requests()
	if len(requests) != 1 {
		t.Fatalf("the server answered %d requests; want 1", len(requests))
	}
	if got := requests[0].Wire; got != stubllm.WireAnthropic {
		t.Errorf("the request arrived on wire %q; want %q", got, stubllm.WireAnthropic)
	}
	if got := requestExtraWitness(t, requests[0].Body); got != "dial" {
		t.Errorf("the request carried witness %q; want %q", got, "dial")
	}
}

// The Monitor carries the key, the wire and the forced dialect: a beat reaches a server that
// requires the key and advertises no effort tell, and reports the dial the binding forced; the
// same server observed by a binding that forces nothing reports none, so the verdict is the
// binding's. A Monitor on the Messages wire probes under that wire's headers.
func TestDial_MonitorCarriesTheForcedDialect(t *testing.T) {
	t.Parallel()

	stub := upstreamServerWithKey(t)
	forced := upstreamBinding{
		Endpoint: stub.URL, APIKey: dialKey, EffortDialect: string(provider.EffortDialectReasoning),
	}

	beat := forced.Monitor().Beat(context.Background())
	if !beat.Reachable {
		t.Fatalf("the forced binding's beat was unreachable: %s", beat.Failure)
	}
	if got := beat.EffortSupport; !got.Supported || got.Dialect != provider.EffortDialectReasoning {
		t.Errorf("the forced binding's beat saw effort %+v; want supported on %q",
			got, provider.EffortDialectReasoning)
	}

	unforced := forced
	unforced.EffortDialect = ""
	if got := unforced.Monitor().Beat(context.Background()).EffortSupport; got.Supported {
		t.Errorf("a binding that forces nothing saw effort %+v; the stub advertises none", got)
	}

	anthropic := upstreamServerWithKey(t)
	onMessages := upstreamBinding{Endpoint: anthropic.URL, APIKey: dialKey, Wire: string(provider.WireAnthropic)}
	if beat := onMessages.Monitor().Beat(context.Background()); !beat.Reachable {
		t.Fatalf("the Messages-wire beat was unreachable: %s", beat.Failure)
	}
	probes := anthropic.Probes()
	if len(probes) == 0 {
		t.Fatal("the Messages-wire beat sent no probe")
	}
	for _, probe := range probes {
		if probe.Header.Get("anthropic-version") == "" {
			t.Errorf("probe of %s carried no anthropic-version header; the Monitor dropped the wire", probe.Path)
		}
	}
}

// upstreamServerWithKey is upstreamServer's one-model listing behind dialKey, with no /props and so
// no effort tell of its own.
func upstreamServerWithKey(t *testing.T) *stubllm.Server {
	t.Helper()
	return stubllm.New(t, stubllm.Script{Discovery: stubllm.Discovery{
		Models: []stubllm.DiscoveredModel{{ID: "dial-model", ContextLength: 4096}},
	}}, stubllm.WithAPIKey(dialKey))
}

// Each source's dial facts: an entry's carry its forced dialect in the config's spelling and its
// own model pin; a Delegation target's and an engine Config's carry none, because the dialect each
// holds is the RANKED one, which rides beside the binding rather than in it.
func TestDial_BindingsOfEachSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  upstreamBinding
		want upstreamBinding
	}{
		{
			name: "entry",
			got: bindingOfEntry(config.ServerEntry{
				Endpoint: "http://box:8080", Model: "pinned", Wire: "anthropic",
				RequestExtra: `{"witness":"entry"}`, EffortDialect: "kwargs",
			}, dialKey),
			want: upstreamBinding{
				Endpoint: "http://box:8080", Model: "pinned", APIKey: dialKey, Wire: "anthropic",
				RequestExtra: `{"witness":"entry"}`, EffortDialect: "kwargs",
			},
		},
		{
			name: "delegation target",
			got: bindingOfTarget(&apogee.DelegationTarget{
				Endpoint: "http://grunt:8080", Model: "cheap-7b", APIKey: dialKey, Wire: "openai",
				RequestExtra: `{"witness":"grunt"}`, EffortDialect: provider.EffortDialectReasoning,
			}),
			want: upstreamBinding{
				Endpoint: "http://grunt:8080", Model: "cheap-7b", APIKey: dialKey, Wire: "openai",
				RequestExtra: `{"witness":"grunt"}`,
			},
		},
		{
			name: "engine config",
			got: bindingOfConfig(apogee.Config{
				Endpoint: "http://box:8080", Model: "bound", APIKey: dialKey, Wire: "openai",
				RequestExtra: `{"witness":"config"}`, EffortDialect: domain.EffortDialectKwargs,
			}),
			want: upstreamBinding{
				Endpoint: "http://box:8080", Model: "bound", APIKey: dialKey, Wire: "openai",
				RequestExtra: `{"witness":"config"}`,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.got != tc.want {
				t.Errorf("binding = %+v; want %+v", tc.got, tc.want)
			}
		})
	}
}
