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

// systemPromptFraction and fileContextFraction are the FALLBACK shares of the working window
// (Window - ResponseReserve) a standing part reserves when the caller measured it not at all
// (Measured{-1, -1}) — the fixed split the Budget used before it could measure. A measured part
// reserves what it measures instead (see Measured), so these two only ever bind a caller that
// takes no measurement. systemPromptFraction doubles as the oversize notice's fixed advisory
// ceiling (Allocation.StandingAdvisory), which stays 15% of the working room whatever the
// standing content measures.
const (
	systemPromptFraction = 0.15
	fileContextFraction  = 0.25
)

// standingHeadroomPercent is a MEASURED reservation as a percentage of the measurement itself —
// 10% of headroom above it. The measurement is one render of standing content at one moment; the
// render the next request carries can be a little larger (a task list gains a row, the date rolls,
// a delegation seat opens) and the estimate itself rides a calibrated ratio. The headroom protects
// the standing content from being clipped by its own reservation between two measurements.
//
// It is a percentage, not a float factor, because a reservation is a token count: 12000 × 1.10 in
// float64 lands a hair ABOVE 13200, and rounding that up would reserve a phantom token.
const standingHeadroomPercent = 110

// standingFloorPercent is the smallest share of the working room, in percent, a measured standing
// part reserves whatever it measures. It protects a part that measures zero — or near zero — from
// reserving nothing at all, so a session that seeds its first context file mid-run still has room
// held for it rather than borrowing it from History at the worst moment.
const standingFloorPercent = 2

// historyFloorFraction is the share of the working room History keeps whatever the standing parts
// measure. It protects the reducers' primary reclaim target: standing content larger than this
// leaves would starve the transcript, and that is the oversize notice's job to REPORT (ADR 0026,
// measured against Allocation.StandingAdvisory), never a reason to hand History less than half the
// room. Above the floor the measured reservations are honoured in full; at it they are scaled down
// together so the parts still sum to the working room exactly.
const historyFloorFraction = 0.50

// Measured carries the token size of the standing parts of one request as the CALLER measured
// them: the system-prompt part and the file-context part. A NEGATIVE field means the caller took
// no measurement of that part (spelled Measured{-1, -1}), and the part falls back to its fixed
// fraction. The zero value is MEASURED-ZERO — a caller that rendered nothing — not unmeasured, so
// a measured part that renders nothing still floors at standingFloorFraction.
//
// Measurement arrives in TOKENS: Allocate converts nothing, so the one chars→token implementation
// (domain.Budget.EstimateTokens, which TokenEstimator delegates to) stays the only place the ratio
// is applied.
type Measured struct {
	SystemPrompt int
	FileContext  int
}

// defaultReserveFraction is the share of the window held back for the model's reply when the
// caller supplies neither an explicit reserve (ContextConfig.ResponseReserve == 0) nor a
// configured share (ContextConfig.ResponseReserveFraction outside the accepted (0, 1) range).
// Generous reply headroom matters more for a small local model than squeezing the last tokens of
// prompt in.
const defaultReserveFraction = 0.20

// Allocation is the Budget's split of a model's context window across the parts of one request:
// the ResponseReserve held back for the model's reply, and the working room the prompt's parts —
// SystemPrompt, FileContext, History — draw from (CONTEXT: Budget, "the single authority on how
// much room each part gets"). SystemPrompt + FileContext + History sum to Window -
// ResponseReserve, so those fields sum to Window exactly; StandingAdvisory is an advisory ceiling
// read alongside them, not a fourth share of the room. A zero Allocation (every field 0) means
// the window is unknown — there is no basis to allocate. A consumer then either stays inert (every
// window-gated Reaction, which must never steer on a guess) or substitutes its own conservative
// ceiling (the engine's structural bounds — internal/agent, ADR 0018); what it must NOT read it as
// is "unbounded", which is what wedged an unbudgeted session (audit 2026-08-01).
type Allocation struct {
	Window          int
	ResponseReserve int
	SystemPrompt    int
	FileContext     int
	History         int

	// StandingAdvisory is the fixed 15%-of-working-room share the oversize notice measures the
	// whole standing content against (ADR 0026: oversize is advisory). It is deliberately NOT the
	// reserved room: a measured reservation grows with the content it measures, so reading it as
	// the ceiling would mean the notice could never fire. It stays comparable with the rest of the
	// Allocation, so the zero Allocation still reads as "window unknown".
	StandingAdvisory int
}

// Allocate splits window (the model's discovered context window, n_ctx tokens) into an
// Allocation. The reply reserve follows one precedence: reserve, the tokens to hold back, wins
// whenever it is positive; else fraction, a share of the window in (0, 1), is applied; else
// defaultReserveFraction. A fraction outside (0, 1) is treated as UNSET rather than rejected — the
// config layer validates the key's range, and this is the defensive floor beneath it, so a bad
// fraction costs the caller the built-in default and never a panic. A reserve that would leave no
// working room is clamped so at least one token remains to fill. A non-positive window yields the
// zero Allocation (the window is unknown, so there is no basis to allocate).
//
// measured carries what the caller measured the two standing parts to be, in tokens. A measured
// part reserves its measurement plus standingHeadroomPercent, floored at standingFloorPercent of
// the working room; an unmeasured part (a negative field, Measured{-1, -1}) keeps its fixed
// fraction. History takes the remainder, floored at historyFloorFraction of the working room: when
// the two reservations would push it below that they are scaled down together, proportionally, so
// the parts still sum to window exactly with no rounding drift.
//
// The split is pure — no I/O, no caller state (ADR 0010) — so the same inputs always produce the
// same Allocation and every reader of the Budget sees one number per part.
func Allocate(window, reserve int, fraction float64, measured Measured) Allocation {
	if window <= 0 {
		return Allocation{}
	}
	if reserve <= 0 {
		if !(fraction > 0 && fraction < 1) {
			fraction = defaultReserveFraction
		}
		reserve = int(float64(window) * fraction)
	}
	if reserve >= window {
		reserve = window - 1
	}
	working := window - reserve
	system := standingReservation(measured.SystemPrompt, working, systemPromptFraction)
	file := standingReservation(measured.FileContext, working, fileContextFraction)
	system, file = scaleToHistoryFloor(system, file, working)
	return Allocation{
		Window:           window,
		ResponseReserve:  reserve,
		SystemPrompt:     system,
		FileContext:      file,
		History:          working - system - file,
		StandingAdvisory: int(float64(working) * systemPromptFraction),
	}
}

// standingReservation sizes one standing part's reservation out of working. A negative
// measurement is the "unmeasured" signal and falls back to fallbackFraction of the working room —
// the fixed share, truncated exactly as it always was. A measurement of zero or more reserves it
// with standingHeadroomPercent, rounded UP so a part is never reserved one token short of what it
// measured, and never less than standingFloorPercent of the working room. Both percentages are
// applied in int64 so the proportions stay exact on a 32-bit build.
func standingReservation(measured, working int, fallbackFraction float64) int {
	if measured < 0 {
		return int(float64(working) * fallbackFraction)
	}
	reserved := ceilPercent(measured, standingHeadroomPercent)
	if floor := ceilPercent(working, standingFloorPercent); reserved < floor {
		reserved = floor
	}
	return reserved
}

// ceilPercent reports percent% of tokens, rounded up.
func ceilPercent(tokens, percent int) int {
	return int((int64(tokens)*int64(percent) + 99) / 100)
}

// scaleToHistoryFloor honours the two standing reservations only down to History's floor: while
// they leave historyFloorFraction of working for History they pass through untouched, and beyond
// it they are scaled down TOGETHER, in proportion to what each asked for, so neither part is
// starved to spare the other. The pair is returned summing to exactly the room above the floor, so
// the caller's remainder lands on the floor without rounding drift.
func scaleToHistoryFloor(system, file, working int) (int, int) {
	room := working - int(float64(working)*historyFloorFraction)
	total := system + file
	if total <= room {
		return system, file
	}
	// int64 keeps the proportion exact on a 32-bit build, where the product of two token counts
	// would otherwise overflow.
	scaledSystem := int(int64(system) * int64(room) / int64(total))
	return scaledSystem, room - scaledSystem
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

// Calibrated reports whether a server usage report has been folded in — the same "Used > 0"
// reading the loop's predictive guard takes when it widens its margin on an uncalibrated
// estimate. False until the first Calibrate with a positive token count, so a consumer can label
// a number computed through the default ratio as the estimate it is.
func (e *TokenEstimator) Calibrated() bool { return e.used > 0 }

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
