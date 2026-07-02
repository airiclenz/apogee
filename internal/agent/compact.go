package agent

import (
	"context"
	"errors"
	"strings"

	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// Compaction sampling: a low temperature for a faithful, low-embellishment summary, and a
// generous token cap so a long conversation's summary is bounded but not truncated. They are
// fixed here (not a config surface) — a model-profile knob is a later, additive concern.
const (
	compactTemperature = 0.2
	compactMaxTokens   = 4096

	// compactPromptOverheadTokens reserves headroom, on top of compactMaxTokens (the summary
	// response's reserve), for the summarizer's system prompt, the trailing instruction, the
	// per-message role headers, and the slack in the chars→tokens estimate. The rendered
	// transcript is budgeted to whatever the discovered window has left after both reserves.
	compactPromptOverheadTokens = 512

	// compactMinTranscriptTokens floors the transcript budget so a very small window still
	// sends a useful (if heavily elided) tail rather than collapsing to nothing.
	compactMinTranscriptTokens = 256
)

// Compact triggers generative Compaction on demand — the engine half of the /compact command.
// It summarizes the conversation and Replaces the folded history with a single summary message
// (internal/context.Compact), keeping the protected prefix verbatim. Valid only at a quiescent
// boundary; calling it mid-Exchange is refused (ErrInputPending) so a half-streamed Turn is
// never orphaned, mirroring ClearContext. The Turn counter is untouched and the Agent stays
// snapshot-safe after it returns. A summary-call failure leaves the conversation unchanged.
//
// skipped reports that the conversation was too small to be worth folding (the reducer's
// Result.Skipped — no upstream call, conv untouched), so the caller can say "nothing to
// compact" and leave the context gauge alone rather than falsely claiming a compaction. It is
// always false on error (a fault is not a skip).
func (a *Agent) Compact(ctx context.Context) (skipped bool, err error) {
	if a.inExchange {
		return false, domain.ErrInputPending
	}
	res, err := apogeectx.Compact(ctx, compactCompleter{a}, &a.conv, a.compactTranscriptChars())
	return res.Skipped, err
}

// compactTranscriptChars returns the character budget for the rendered transcript the summary
// call carries, derived from the discovered context window so the call itself cannot overflow at
// exactly the high fill /compact exists to relieve (post-v1 remediation item 6). The window (in
// tokens) minus the response reserve (compactMaxTokens) minus prompt overhead is the transcript's
// token budget, converted to characters via the budget's chars→token estimate. It returns 0
// (unbounded — render the whole conversation) when the window is unknown: neither discovery nor
// config reported one, so there is no safe basis to bound, and the pre-item-6 full render stands.
func (a *Agent) compactTranscriptChars() int {
	window := a.cfg.Context.MaxContextTokens
	if window <= 0 {
		return 0
	}
	transcriptTokens := window - compactMaxTokens - compactPromptOverheadTokens
	if transcriptTokens < compactMinTranscriptTokens {
		transcriptTokens = compactMinTranscriptTokens
	}
	return int(float64(transcriptTokens) * a.budget().CharsPerToken)
}

// compactCompleter adapts the Agent's provider seam to context.Completer: a single, SILENT
// upstream completion. Unlike streamResponse it emits NO TokenEvent/UsageEvent — compaction is
// a maintenance call, not a Turn, so it must not stream into the transcript or move the live
// context gauge (the gauge re-measures on the next real Turn's usage). It reuses the loop's
// request projection (toProviderRequest) and collects the streamed content into one string; a
// cancelled ctx or a terminal stream fault surfaces as an error, so the reducer leaves the
// conversation untouched.
type compactCompleter struct{ a *Agent }

func (c compactCompleter) Complete(ctx context.Context, msgs []domain.Message) (string, error) {
	req := domain.NewRequest(c.a.cfg.Model, msgs, nil, c.a.budget(), c.a.turnIndex)
	temp, maxTok := compactTemperature, compactMaxTokens
	req.SetSampling(domain.SamplingParams{Temperature: &temp, MaxTokens: &maxTok})

	var content strings.Builder
	var failed bool
	var errMsg string
	for delta := range c.a.upstream.Stream(ctx, c.a.toProviderRequest(req)) {
		switch delta.Kind {
		case provider.DeltaContent:
			content.WriteString(delta.Content)
		case provider.DeltaError, provider.DeltaContextOverflow:
			failed = true
			errMsg = delta.Err
		}
	}
	if ctx.Err() != nil {
		return "", ctx.Err() // a cancel masquerades as a stream error; ctx wins (as in respondAndReview)
	}
	if failed {
		return "", errors.New(errMsg)
	}
	return content.String(), nil
}
