package main

import (
	"context"
	"path/filepath"
	"time"

	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/title"
)

// titleRequestTimeout bounds ONE naming call. It is deliberately enormous for a request that asks
// for eight words: the call is fired at the first prompt's submit, in parallel with the Exchange
// that prompt starts, and a single-slot server answers it only after that whole first response has
// streamed (ADR 0024). The wait is therefore the FEATURE — it is what makes the naming call land at
// the cheapest KV-eviction point there is, between Turns 1 and 2 with the context at its smallest —
// and a timeout sized for the generation alone would cancel a call that is merely queued.
const titleRequestTimeout = 5 * time.Minute

// titleWiring is the composition root's half of [tui.Options.GenerateTitle]: it turns "name this
// session" into one out-of-band completion against the Upstream the session is bound to at the
// moment of the call.
//
// The split is the one every seam here takes. The TUI owns WHEN a session is named and whether the
// answer is applied — that is Session-record state it holds and the binary does not — while this
// side owns everything the CALL is made of, because the endpoint, the model, and the key are wiring
// the binary resolves and a `/server` switch moves. Which is also why the client is constructed per
// call rather than held: a session that switched servers or rebound its model between two namings
// must name through the server it is on now, and reading [upstreamHolder.Binding] at call time is
// the whole of that.
//
// It emits no events, touches no engine state, and is never the Agent's business (the Agent is
// single-goroutine — ADR 0011 — and this call runs on a Bubble Tea Cmd goroutine).
type titleWiring struct {
	// binding reads the CURRENT Upstream binding; wired to upstreamHolder.Binding.
	binding func() upstreamBinding
	// workspaceBase is the workspace directory's basename — context for the model, never title
	// text (Ratified design 6): the session browser already renders the workspace beside the title.
	workspaceBase string
	// now stamps the date the naming call carries as context. A field so a test can pin it.
	now func() time.Time
	// requestTimeout bounds one call; titleRequestTimeout in production, short in tests.
	requestTimeout time.Duration
}

// newTitleWiring builds the wiring for a session rooted at workspace, reading its live Upstream
// binding through binding.
func newTitleWiring(binding func() upstreamBinding, workspace string) titleWiring {
	return titleWiring{
		binding:        binding,
		workspaceBase:  filepath.Base(workspace),
		now:            time.Now,
		requestTimeout: titleRequestTimeout,
	}
}

// generate performs the naming call for firstUserText and returns the model's RAW reply — the value
// wired into [tui.Options.GenerateTitle]. Cleaning the reply up is deliberately NOT done here:
// title.Sanitize runs TUI-side so the generated title and a manual `/rename <text>` share one
// pipeline (Ratified design 6).
//
// An error is returned as-is and means no title was produced; the caller's posture — silence on the
// automatic path, a quiet note on a bare `/rename` — is the TUI's to choose, because only it knows
// which of the two asked.
func (w titleWiring) generate(ctx context.Context, firstUserText string) (string, error) {
	binding := w.binding()
	// Retries OFF, unlike every other client the binary builds. The Client's default policy re-POSTs
	// a faulted attempt twice, and here each attempt is bounded by requestTimeout — so one naming call
	// could occupy a single-slot server's queue three times over, for a cosmetic result that is dropped
	// on the first failure anyway. "One out-of-band call" is the contract; a title nobody asked twice
	// for is not worth a second slot ahead of the user's next Exchange.
	client := provider.NewClient(binding.Endpoint, binding.Model,
		provider.WithRequestTimeout(w.requestTimeout), provider.WithAPIKey(binding.APIKey),
		provider.WithMaxRetries(0))

	resp, err := client.Respond(ctx, title.Prompt(firstUserText, w.workspaceBase, w.now()))
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}
