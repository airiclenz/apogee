package probe

import (
	"context"
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// Inputs are the facts the composition root has already resolved and the host report merely
// states: the machine's identity and roots, the Confiner backend this build selected, and the
// EFFECTIVE confine-to-workspace after the host acknowledgement was applied (ADR 0012's
// amendment — the report must show what would actually happen here, not what the config file
// literally says). Everything is injected rather than looked up, so the report is testable on
// any machine with any backend: GatherHost derives, it never discovers a host fact of its own.
type Inputs struct {
	GOOS               string
	GOARCH             string
	Confiner           domain.Confiner
	HostID             string
	Workspace          string
	ConfigHome         string
	Endpoint           string
	ConfineToWorkspace bool
}

// Host is the finished host report: the facts, resolved once, ready to render. It is a value
// so a caller can assert on individual findings (a test, a later machine-readable format)
// without parsing the text Report produces.
type Host struct {
	GOOS               string
	GOARCH             string
	Backend            string
	Caps               domain.ConfinementCaps
	AutoEligible       bool
	ConfineToWorkspace bool
	HostID             string
	Workspace          string
	ConfigHome         string
	Discovery          Discovery
}

// GatherHost runs the host half of `apogee probe`: it reads the backend's capability matrix
// (already probed once at the Confiner's construction — the report does not re-probe the
// kernel) and, when an endpoint is configured, the Upstream's discovery outcome. It runs no
// agent, no tool and no model call, and it writes nothing.
func GatherHost(ctx context.Context, in Inputs) Host {
	caps := domain.ConfinementCaps{}
	if in.Confiner != nil {
		caps = in.Confiner.Capabilities()
	}
	return Host{
		GOOS:               in.GOOS,
		GOARCH:             in.GOARCH,
		Backend:            BackendName(in.Confiner),
		Caps:               caps,
		AutoEligible:       caps.AutoEligible(),
		ConfineToWorkspace: in.ConfineToWorkspace,
		HostID:             in.HostID,
		Workspace:          in.Workspace,
		ConfigHome:         in.ConfigHome,
		Discovery:          Discover(ctx, in.Endpoint),
	}
}

// Report renders the host report for a terminal. It is pure — Gather did the observing — so the
// wording is table-testable without a host, an endpoint, or a captured stdout.
//
// The order is the order the questions get asked: which machine is this, what can Auto do here
// (the question the command exists for — TODO.md's "diagnosable without running an agent"), and
// is the Upstream answering. It closes with the SAME degradation notice the session prints at
// startup when Auto would gate here, so the off-session diagnosis and the in-session one are
// literally the same sentence.
func (h Host) Report() string {
	lines := []string{
		"apogee probe — host report",
		"  (no agent runs, no model is called, nothing is written)",
		"",
		"host",
		field("os/arch", h.GOOS+"/"+h.GOARCH),
		field("host id", orUnknown(h.HostID)),
		field("workspace", orUnknown(h.Workspace)),
		field("config home", orUnknown(h.ConfigHome)),
		"",
		"confinement (ADR 0012)",
		field("backend", CapabilityLine(h.Backend, h.Caps)),
		field("auto", h.autoLine()),
		field("confined", h.confinedLine()),
		"",
		"upstream",
	}
	lines = append(lines, h.upstreamLines()...)

	report := strings.Join(lines, "\n")
	// The one cell where Auto is entered, confinement is asked for, and the backend cannot
	// fence it. Reusing the startup notice verbatim is the point: a user who saw it scroll past
	// and ran `apogee probe` to understand it must find the same words, not a paraphrase.
	if notice := DegradedNotice(h.Backend, h.Caps, domain.ModeAuto, h.ConfineToWorkspace); notice != "" {
		report += "\n\n" + notice
	}
	return report
}

// autoLine states the AutoEligible verdict (ADR 0012: FSWrite alone) in terms of what the user
// will observe — a fenced command or an approval prompt — rather than as a bare boolean.
func (h Host) autoLine() string {
	if h.AutoEligible {
		return "eligible — the backend can fence terminal commands, so auto runs them confined"
	}
	return "NOT eligible — commands cannot be fenced here, so auto gates each one for approval"
}

// confinedLine states the EFFECTIVE confine-to-workspace (ADR 0012 as amended 2026-07-21),
// naming both routes to an unconfined host so the reader can find the setting that did it: the
// blanket `confine-to-workspace: false`, or an `unconfined-hosts:` entry matching the host id
// printed above.
func (h Host) confinedLine() string {
	if h.ConfineToWorkspace {
		return "yes — auto fences what it can and gates the rest (confine-to-workspace: true)"
	}
	return "NO — auto runs every command with your full privileges\n" +
		"                 (confine-to-workspace: false, or an unconfined-hosts entry for this host id)"
}

// upstreamLines render the endpoint block: the URL, whether GET /v1/models answered, what it
// advertised, and whether llama.cpp's GET /props supplied a runtime context window. Each probe
// reports its own outcome, so "the server is down" and "the server is not llama.cpp" cannot be
// confused for one another.
func (h Host) upstreamLines() []string {
	d := h.Discovery
	if !d.Attempted {
		return []string{
			field("endpoint", "(none — set endpoint: in config.yaml, APOGEE_ENDPOINT, or --endpoint)"),
			field("reachable", "not asked — no endpoint is configured"),
		}
	}
	if !d.Reached {
		return []string{
			field("endpoint", d.Endpoint),
			field("reachable", "NO — GET /v1/models did not complete: "+d.Failure),
			field("/props", "not probed (the model probe failed first)"),
		}
	}

	models := fmt.Sprintf("%d advertised · active: %s", len(d.Models), orUnknown(d.ActiveModel))
	if d.ContextWindow > 0 {
		models += fmt.Sprintf(" (context window %d)", d.ContextWindow)
	} else {
		models += " (context window unknown)"
	}

	props := "no runtime window reported — not a llama.cpp server, or /props is absent"
	if d.RuntimeContextWindow > 0 {
		props = fmt.Sprintf("runtime context window %d (llama.cpp)", d.RuntimeContextWindow)
	}

	return []string{
		field("endpoint", d.Endpoint),
		field("reachable", "yes — GET /v1/models answered"),
		field("models", models),
		field("/props", props),
	}
}

// field renders one "  label:        value" line, padded so the values align in a terminal.
func field(label, value string) string {
	return fmt.Sprintf("  %-14s %s", label+":", value)
}

// orUnknown renders an empty fact as "unknown" — never a blank space where a value should be.
func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
