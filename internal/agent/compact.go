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
)

// Compact triggers generative Compaction on demand — the engine half of the /compact command.
// It summarizes the conversation and Replaces the folded history with a single summary message
// (internal/context.Compact), keeping the protected prefix verbatim. Valid only at a quiescent
// boundary; calling it mid-Exchange is refused (ErrInputPending) so a half-streamed Turn is
// never orphaned, mirroring ClearContext. The Turn counter is untouched and the Agent stays
// snapshot-safe after it returns. A summary-call failure leaves the conversation unchanged.
func (a *Agent) Compact(ctx context.Context) error {
	if a.inExchange {
		return domain.ErrInputPending
	}
	_, err := apogeectx.Compact(ctx, compactCompleter{a}, &a.conv)
	return err
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
