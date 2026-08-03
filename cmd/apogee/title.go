package main

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
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

// finishReasonLength is the OpenAI finish reason for a completion the server cut off at the token
// cap. Paired with an empty reply it is the signature of a thinking model that spent the whole
// budget reasoning, which is the one generation outcome this wiring names rather than passes on.
const finishReasonLength = "length"

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

// generate performs the naming call for prompts — the window of the user's requests, oldest first —
// and returns the model's RAW reply; it is the value wired into [tui.Options.GenerateTitle]. How
// wide that window is belongs to the TUI, which knows which of the two forms asked; this side only
// renders whatever it is handed. Cleaning the reply up is deliberately NOT done here:
// title.Sanitize runs TUI-side so the generated title and a manual `/rename <text>` share one
// pipeline (Ratified design 6).
//
// An error is returned as-is and means no title was produced; the caller's posture — silence on the
// automatic path, a quiet note on a bare `/rename` — is the TUI's to choose, because only it knows
// which of the two asked. [title.ErrTruncated] is the one error this side synthesises rather than
// passes on: a reply the server cut off at the token cap with nothing in it is a REPORTABLE cause,
// not the generic "nothing usable came back" a caller would otherwise be left to guess at.
func (w titleWiring) generate(ctx context.Context, prompts []string) (string, error) {
	binding := w.binding()
	// Retries OFF, unlike every other client the binary builds. The Client's default policy re-POSTs
	// a faulted attempt twice, and here each attempt is bounded by requestTimeout — so one naming call
	// could occupy a single-slot server's queue three times over, for a cosmetic result that is dropped
	// on the first failure anyway. "One out-of-band call" is the contract; a title nobody asked twice
	// for is not worth a second slot ahead of the user's next Exchange.
	client := provider.NewClient(binding.Endpoint, binding.Model,
		provider.WithRequestTimeout(w.requestTimeout), provider.WithAPIKey(binding.APIKey),
		provider.WithMaxRetries(0))

	req := title.Prompt(prompts, w.workspaceBase, w.now())
	resp, err := client.Respond(ctx, req)
	if err != nil && req.DisableThinking && rejectedOutright(err) {
		// The "answer without thinking" intent rides as a chat_template_kwargs object, which llama.cpp
		// accepts and a stricter OpenAI-compatible server may reject as an unknown field. Without this
		// one re-send, asking for it would trade "naming fails on big sessions" for "naming fails
		// always" on such a server; dropping the flag falls back on the raised token cap, which is the
		// backstop it was raised to be (internal/title).
		//
		// It does not soften the retries-OFF contract above. That policy is about queue time, and a 4xx
		// comes back before the server generates a token — the second POST costs the user's next
		// Exchange nothing, where a re-POST of a faulted attempt would have cost it a whole generation.
		req.DisableThinking = false
		resp, err = client.Respond(ctx, req)
	}
	if err != nil {
		return "", err
	}
	// The failure this plan exists for: a thinking model on a server whose template ignored the switch
	// burns the entire cap on reasoning and returns finish_reason "length" with no content. Passing
	// that back as ("", nil) makes it indistinguishable from a garbage reply, so it is named instead. A
	// cut-off reply that still carries text is NOT this case — a truncated title is a title.
	if resp.FinishReason == finishReasonLength && strings.TrimSpace(resp.Content) == "" {
		return "", title.ErrTruncated
	}
	return resp.Content, nil
}

// rejectedOutright reports whether err is an Upstream 4xx — the class that means "I will not accept
// this request as written", and so the only one where re-sending it WITHOUT the thinking kwarg can
// change the answer. A transport fault or a 5xx would fail the same way twice, and a context
// overflow is the request being too big for the window, which dropping one field cannot fix; the
// overflow is excluded explicitly even though the Client classifies it as its own sentinel rather
// than a *provider.StatusError today, so this guard holds if that classification ever moves.
func rejectedOutright(err error) bool {
	if errors.Is(err, provider.ErrContextOverflow) {
		return false
	}
	var status *provider.StatusError
	return errors.As(err, &status) &&
		status.Code >= http.StatusBadRequest && status.Code < http.StatusInternalServerError
}
