package serverstats

import (
	"slices"
	"time"

	"github.com/airiclenz/apogee/internal/provider"
)

// minSamples is how many counted samples a figure needs before a summary reports it: under it a
// picker row says "no data" and a tok/s cell stays "—" rather than showing a median of one or two.
const minSamples = 5

// Summary is what a server entry's recent attempts at one model add up to. It is computed over
// the newest keepPerKey samples of that model, with cancelled attempts left out of every figure —
// the caller ended them, so they say nothing about the server.
type Summary struct {
	// Model is the model the summary covers.
	Model string
	// Total is how many counted (not cancelled) attempts the summary covers; Failed how many of
	// them did not end "ok".
	Total  int
	Failed int
	// NoData is set when fewer than minSamples attempts were counted: the figures below are then
	// not worth showing.
	NoData bool
	// TTFT is the median send → first model delta over counted attempts that reached one; HasTTFT
	// says whether any did.
	TTFT    time.Duration
	HasTTFT bool
	// TokensPerSec is the median output tokens ÷ (Last − TTFT) over the counted attempts that carry
	// a rate — reported output tokens and a positive generation span. HasTokensPerSec is unset
	// under minSamples carrying attempts; a rate is never estimated.
	TokensPerSec    float64
	HasTokensPerSec bool
}

// Summarize computes model's Summary from samples, which a caller has already narrowed to one
// server entry (Store.Load). Samples of other models are ignored, and of model's own only the
// newest keepPerKey count, so the figures match what a trimmed file holds.
func Summarize(samples []Sample, model string) Summary {
	window := make([]Sample, 0, keepPerKey)
	for _, sample := range samples {
		if sample.Model == model {
			window = append(window, sample)
		}
	}
	if len(window) > keepPerKey {
		window = window[len(window)-keepPerKey:]
	}

	sum := Summary{Model: model}
	var ttfts []time.Duration
	var rates []float64
	for _, sample := range window {
		if sample.Outcome == provider.AttemptCancelled {
			continue
		}
		sum.Total++
		if sample.Outcome != provider.AttemptOK {
			sum.Failed++
		}
		if sample.TTFT > 0 {
			ttfts = append(ttfts, sample.TTFT)
		}
		if rate, ok := tokensPerSec(sample); ok {
			rates = append(rates, rate)
		}
	}
	sum.NoData = sum.Total < minSamples
	if len(ttfts) > 0 {
		sum.TTFT, sum.HasTTFT = median(ttfts), true
	}
	if len(rates) >= minSamples {
		sum.TokensPerSec, sum.HasTokensPerSec = median(rates), true
	}
	return sum
}

// LastModel is the model of the newest sample, "" when there are none: what a summary falls back
// to when the entry's bound model is not known.
func LastModel(samples []Sample) string {
	if len(samples) == 0 {
		return ""
	}
	return samples[len(samples)-1].Model
}

// tokensPerSec is one attempt's generation rate: reported output tokens over the span from its
// first model delta to its last. An attempt that reported no tokens, or whose span is empty,
// carries no rate.
func tokensPerSec(s Sample) (float64, bool) {
	span := s.Last - s.TTFT
	if s.OutputTokens <= 0 || s.TTFT <= 0 || span <= 0 {
		return 0, false
	}
	return float64(s.OutputTokens) / span.Seconds(), true
}

// median is the nearest-rank p50 of values: the lower middle of an even count, so the figure is
// always one an attempt actually measured. values must be non-empty; it is sorted in place.
func median[T time.Duration | float64](values []T) T {
	slices.Sort(values)
	return values[(len(values)-1)/2]
}
