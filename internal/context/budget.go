package context

import (
	"github.com/airiclenz/apogee/internal/domain"
)

// DefaultCharsPerToken is the chars→token ratio the estimator starts from before any
// server-reported usage has calibrated it — a conservative English-text average that a real
// UsageEvent quickly folds toward the model's actual tokenizer ratio. It replaces the loop's
// former trivial constant so an uncalibrated Budget still reports a usable value.
const DefaultCharsPerToken = 4.0

// minCharsPerToken and maxCharsPerToken bound the calibrated ratio to a sane range so a single
// anomalous usage report (a near-empty prompt, a server that miscounts) cannot drive the
// estimate to an absurd value. Real subword tokenizers land well inside this band — dense code
// near the low end, prose near the high — so the clamp only ever fires on noise.
const (
	minCharsPerToken = 2.0
	maxCharsPerToken = 8.0
)

// calibrationWeight is how much one fresh usage sample moves the running chars→token ratio (an
// exponential moving average). Half-weight tracks a model's real ratio within a couple of Turns
// while damping the per-Turn jitter a raw recompute would suffer from the chat-template overhead
// the character count cannot see.
const calibrationWeight = 0.5

// The working window (Window - ResponseReserve) is split across the prompt's parts by these
// fractions. History gets the lion's share — it grows unboundedly and is what the reducers
// reclaim; the system prompt is small and near-constant; file context sits between. They sum to
// 1.0, and History takes the remainder, so the three parts fill the working window exactly.
const (
	systemPromptFraction = 0.15
	fileContextFraction  = 0.25
)

// defaultReserveFraction is the share of the window held back for the model's reply when the
// caller supplies no explicit reserve (ContextConfig.ResponseReserve == 0). Generous reply
// headroom matters more for a small local model than squeezing the last tokens of prompt in.
const defaultReserveFraction = 0.20

// Allocation is the Budget's split of a model's context window across the parts of one request:
// the ResponseReserve held back for the model's reply, and the working room the prompt's parts —
// SystemPrompt, FileContext, History — draw from (CONTEXT: Budget, "the single authority on how
// much room each part gets"). SystemPrompt + FileContext + History sum to Window -
// ResponseReserve, so every field sums to Window exactly. A zero Allocation (every field 0) means
// the window is unknown — there is no basis to allocate — and a consumer treats it as unbounded,
// matching the generative Compaction path.
type Allocation struct {
	Window          int
	ResponseReserve int
	SystemPrompt    int
	FileContext     int
	History         int
}

// Allocate splits window (the model's discovered context window, n_ctx tokens) into an
// Allocation. reserve is the tokens to hold back for the reply; a non-positive reserve falls back
// to defaultReserveFraction of the window, and a reserve that would leave no working room is
// clamped so at least one token remains to fill. A non-positive window yields the zero Allocation
// (the window is unknown, so there is no basis to allocate). History takes the remainder after the
// system-prompt and file-context shares, so the parts sum to window exactly with no rounding drift.
func Allocate(window, reserve int) Allocation {
	if window <= 0 {
		return Allocation{}
	}
	if reserve <= 0 {
		reserve = int(float64(window) * defaultReserveFraction)
	}
	if reserve >= window {
		reserve = window - 1
	}
	working := window - reserve
	system := int(float64(working) * systemPromptFraction)
	file := int(float64(working) * fileContextFraction)
	return Allocation{
		Window:          window,
		ResponseReserve: reserve,
		SystemPrompt:    system,
		FileContext:     file,
		History:         working - system - file,
	}
}

// TokenEstimator turns a character count into a token estimate through a chars→token ratio it
// CALIBRATES against server-reported usage. It starts at DefaultCharsPerToken and, each time a
// real prompt's character count and the server's reported prompt-token count are known
// (Calibrate), recomputes the ratio toward chars/tokens — bounded to a sane range and smoothed
// across Turns — and records the reported tokens as the honest Used fill. It is per-Agent and not
// serialized: a resumed Agent recalibrates from its first UsageEvent (the Budget view reports the
// default ratio and a zero Used until then).
//
// It is not safe for concurrent use; the loop drives it from the single worker goroutine (the same
// one that streams the reply and reads the Budget view), never across goroutines.
type TokenEstimator struct {
	charsPerToken float64
	used          int
}

// NewTokenEstimator returns an estimator seeded with the default, uncalibrated ratio.
func NewTokenEstimator() *TokenEstimator {
	return &TokenEstimator{charsPerToken: DefaultCharsPerToken}
}

// CharsPerToken reports the current chars→token ratio — the default until the first Calibrate,
// then the value converged from server usage.
func (e *TokenEstimator) CharsPerToken() float64 { return e.charsPerToken }

// Used reports the tokens the most recent server usage said the prompt occupied — the honest
// context fill. It is 0 until the first Calibrate with a positive token count.
func (e *TokenEstimator) Used() int { return e.used }

// EstimateTokens converts a character count to a token estimate through the current ratio,
// delegating the rounding to the single domain implementation (domain.Budget.EstimateTokens —
// ceil, so a part is never estimated to fit when it is one token over). A non-positive ratio
// (never produced here, but defensive) falls back to the default.
func (e *TokenEstimator) EstimateTokens(chars int) int {
	return domain.Budget{CharsPerToken: e.ratio()}.EstimateTokens(chars)
}

// ratio is the calibrated chars→token ratio with the estimator's defensive default-ratio
// fallback applied — the value its token math hands to the shared domain implementation.
func (e *TokenEstimator) ratio() float64 {
	if e.charsPerToken <= 0 {
		return DefaultCharsPerToken
	}
	return e.charsPerToken
}

// Calibrate folds one server usage report into the estimate: it snaps Used to
// reportedPromptTokens (the honest fill) and moves the chars→token ratio toward
// promptChars/reportedPromptTokens, clamped to [minCharsPerToken, maxCharsPerToken] and blended by
// calibrationWeight so the ratio converges toward the model's real tokenizer across Turns while one
// noisy sample cannot swing it. A non-positive token count carries no information (a server that
// omitted usage), so it is ignored; a non-positive char count snaps Used but leaves the ratio
// untouched.
func (e *TokenEstimator) Calibrate(promptChars, reportedPromptTokens int) {
	if reportedPromptTokens <= 0 {
		return
	}
	e.used = reportedPromptTokens
	if promptChars <= 0 {
		return
	}
	sample := clampFloat(float64(promptChars)/float64(reportedPromptTokens), minCharsPerToken, maxCharsPerToken)
	e.charsPerToken = e.charsPerToken*(1-calibrationWeight) + sample*calibrationWeight
}

// PromptChars re-exports domain.PromptChars at its original home: the stable character measure
// of a request's prompt used both as the calibration sample (Calibrate) and as the basis for a
// token estimate (EstimateTokens). The measure itself moved to internal/domain (ADR 0010's
// lowest-layer rule — it reads only domain types); this delegate keeps the existing callers and
// tests unchanged.
func PromptChars(msgs []domain.Message, tools []domain.ToolDef) int {
	return domain.PromptChars(msgs, tools)
}

// HistoryExceedsAllocation reports whether the estimated token size of msgs (the conversation
// history the reducers reclaim) has outgrown historyBudget — the Budget's History allocation. It
// is the signal the automatic Compaction trigger fires on (CONTEXT: Compaction, "the default
// reducer ... when the conversation exceeds a threshold"; Phase-4 item 9): the loop calls it at a
// quiescent boundary and folds the history when it reports true. The measure runs the whole
// conversation through the calibrated estimator (PromptChars omits the tool menu — that is not
// history) so it tracks the model's real tokenizer, and it is deliberately conservative: comparing
// the whole conversation against the History slice trips slightly before the prompt would overflow.
// A non-positive historyBudget (the window is unknown, so Allocate returned a zero Allocation)
// never trips — there is no basis to bound, matching /compact's unbounded transcript render.
// It builds on the single domain compare (domain.Budget.HistoryExceedsAllocation), so it can
// never disagree with a hook reading the same Budget.
func HistoryExceedsAllocation(historyBudget int, e *TokenEstimator, msgs []domain.Message) bool {
	return domain.Budget{CharsPerToken: e.ratio(), History: historyBudget}.HistoryExceedsAllocation(msgs)
}

// clampFloat bounds v to [lo, hi].
func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
