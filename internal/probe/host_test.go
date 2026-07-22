package probe_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/probe"
)

// fakeConfiner is a Confiner whose capability matrix the test dictates — the seam that makes
// the report's confinement verdict provable on ANY machine, capable or not. Its name is what
// the report's backend label derives from ("fakeConfiner" → "fake").
type fakeConfiner struct{ caps domain.ConfinementCaps }

func (f fakeConfiner) Capabilities() domain.ConfinementCaps { return f.caps }

func (fakeConfiner) Confine(context.Context, domain.ConfinementBox, *exec.Cmd) error { return nil }

// upstreamServer serves an OpenAI-compatible /v1/models, and llama.cpp's /props only when
// props is non-empty — the two server SHAPES the report must distinguish: a llama.cpp server
// that reports a runtime context window, and a bare OpenAI-compatible one that has no /props
// at all (404).
func upstreamServer(t *testing.T, models, props string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, models)
		case "/props":
			if props == "" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, props)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

const (
	openAIModels  = `{"data":[{"id":"loaded-model","context_length":32768},{"id":"other-model"}]}`
	llamaCppProps = `{"default_generation_settings":{"n_ctx":8192}}`
)

// The report states the host facts it was given and the confinement verdict it derived, on a
// backend that CAN fence: auto is eligible, and the degradation notice — which exists only for
// the gating case — must not appear.
func TestReportCapableHostAndLlamaCppEndpoint(t *testing.T) {
	t.Parallel()
	srv := upstreamServer(t, openAIModels, llamaCppProps)

	report := probe.GatherHost(context.Background(), probe.Inputs{
		GOOS:               "linux",
		GOARCH:             "arm64",
		Confiner:           fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}},
		HostID:             "test-host-id",
		Workspace:          "/tmp/ws",
		ConfigHome:         "/tmp/home/.apogee",
		Endpoint:           srv.URL,
		ConfineToWorkspace: true,
	}).Report()

	for _, want := range []string{
		"linux/arm64",
		"test-host-id",
		"/tmp/ws",
		"/tmp/home/.apogee",
		"fake (fs-write: available · network: unavailable)",
		"eligible —",
		"2 advertised · active: loaded-model",
		// /props is authoritative for the window and overrides the model's advertised
		// 32768 — the report states the number a session would actually run with.
		"context window 8192",
		"runtime context window 8192 (llama.cpp)",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report does not state %q:\n%s", want, report)
		}
	}
	if strings.Contains(report, "auto mode is gating terminal commands") {
		t.Errorf("a backend that CAN fence got the degradation notice:\n%s", report)
	}
}

// A bare OpenAI-compatible server has no /props, so the runtime window is absent — and the
// report says which probe found nothing, rather than blaming the endpoint as a whole.
func TestReportBareOpenAIEndpoint(t *testing.T) {
	t.Parallel()
	srv := upstreamServer(t, openAIModels, "")

	report := probe.GatherHost(context.Background(), probe.Inputs{
		Confiner:           fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}},
		Endpoint:           srv.URL,
		ConfineToWorkspace: true,
	}).Report()

	if !strings.Contains(report, "yes — GET /v1/models answered") {
		t.Errorf("report does not record the reachable endpoint:\n%s", report)
	}
	if !strings.Contains(report, "context window 32768") {
		t.Errorf("report does not state the advertised context window:\n%s", report)
	}
	if !strings.Contains(report, "no runtime window reported") {
		t.Errorf("report does not record the missing /props runtime window:\n%s", report)
	}
	if strings.Contains(report, "llama.cpp)") {
		t.Errorf("a server without /props was reported as llama.cpp-shaped:\n%s", report)
	}
}

// An unreachable endpoint is a FINDING, not a command failure: the report names the failed
// probe and carries the error, and the /props line says it was never tried rather than
// implying the server is not llama.cpp.
func TestReportUnreachableEndpoint(t *testing.T) {
	t.Parallel()
	srv := upstreamServer(t, openAIModels, "")
	url := srv.URL
	srv.Close() // the port is now closed: the probe's dial is refused

	report := probe.GatherHost(context.Background(), probe.Inputs{
		Confiner:           fakeConfiner{},
		Endpoint:           url,
		ConfineToWorkspace: true,
	}).Report()

	if !strings.Contains(report, "NO — GET /v1/models did not complete") {
		t.Errorf("report does not record the unreachable endpoint:\n%s", report)
	}
	if !strings.Contains(report, "not probed") {
		t.Errorf("report does not say /props was never tried:\n%s", report)
	}
}

// With no endpoint configured there is nothing to ask — the report says so and names the three
// places an endpoint can come from, instead of reporting a failure the user did not cause.
func TestReportNoEndpointConfigured(t *testing.T) {
	t.Parallel()
	report := probe.GatherHost(context.Background(), probe.Inputs{
		Confiner:           fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}},
		ConfineToWorkspace: true,
	}).Report()

	if !strings.Contains(report, "not asked — no endpoint is configured") {
		t.Errorf("report does not state that no endpoint was configured:\n%s", report)
	}
	for _, want := range []string{"config.yaml", "APOGEE_ENDPOINT", "--endpoint"} {
		if !strings.Contains(report, want) {
			t.Errorf("report does not name %q as a place to set the endpoint:\n%s", want, report)
		}
	}
}

// The question the command exists for: on a host whose backend cannot fence, a confined Auto
// gates every command — and the report closes with the SAME notice the session prints at
// startup, so the off-session diagnosis and the in-session one are one sentence.
func TestReportDegradedHostCarriesTheStartupNotice(t *testing.T) {
	t.Parallel()
	host := probe.GatherHost(context.Background(), probe.Inputs{
		Confiner:           fakeConfiner{},
		ConfineToWorkspace: true,
	})
	report := host.Report()

	if host.AutoEligible {
		t.Error("a backend reporting FSWrite=false was gathered as auto-eligible")
	}
	if !strings.Contains(report, "NOT eligible") {
		t.Errorf("report does not state the auto verdict:\n%s", report)
	}
	notice := probe.DegradedNotice(host.Backend, host.Caps, domain.ModeAuto, true)
	if notice == "" || !strings.Contains(report, notice) {
		t.Errorf("report does not carry the startup degradation notice verbatim:\n%s", report)
	}
}

// An unconfined host (an explicit confine-to-workspace: false, or an unconfined-hosts entry
// matching this machine) gets no gating notice — nothing is being gated — and the report states
// the blast radius plainly instead.
func TestReportUnconfinedHostStatesTheBlastRadius(t *testing.T) {
	t.Parallel()
	report := probe.GatherHost(context.Background(), probe.Inputs{
		Confiner:           fakeConfiner{},
		ConfineToWorkspace: false,
	}).Report()

	if !strings.Contains(report, "NO — auto runs every command with your full privileges") {
		t.Errorf("report does not state the unconfined blast radius:\n%s", report)
	}
	if !strings.Contains(report, "unconfined-hosts") {
		t.Errorf("report does not name where an unconfined host is recorded:\n%s", report)
	}
	if strings.Contains(report, "auto mode is gating terminal commands") {
		t.Errorf("an unconfined host got the gating notice:\n%s", report)
	}
}
