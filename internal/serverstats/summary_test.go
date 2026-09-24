package serverstats

import (
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/provider"
)

// okSample is a completed attempt at model with the given ttft and a generation of tokens over
// span.
func okSample(model string, ttft time.Duration, tokens int, span time.Duration) Sample {
	return Sample{
		Server:       "local",
		Endpoint:     "http://127.0.0.1:8080/v1",
		Model:        model,
		TTFT:         ttft,
		Last:         ttft + span,
		Duration:     ttft + span,
		OutputTokens: tokens,
		Outcome:      provider.AttemptOK,
	}
}

// withOutcome is s ending on outcome instead.
func withOutcome(s Sample, outcome string) Sample {
	s.Outcome = outcome
	return s
}

// repeat is n copies of s.
func repeat(s Sample, n int) []Sample {
	out := make([]Sample, n)
	for i := range out {
		out[i] = s
	}
	return out
}

func TestSummarize(t *testing.T) {
	t.Parallel()
	const model = "qwen"
	sec := time.Second
	// A 100-token generation over one second: 100 tok/s.
	good := okSample(model, sec, 100, sec)

	tests := []struct {
		name    string
		samples []Sample
		want    Summary
	}{
		{
			name:    "no samples is no data",
			samples: nil,
			want:    Summary{Model: model, NoData: true},
		},
		{
			name:    "four samples is still no data",
			samples: repeat(good, 4),
			want:    Summary{Model: model, Total: 4, NoData: true, TTFT: sec, HasTTFT: true},
		},
		{
			name:    "five samples carry every figure",
			samples: repeat(good, 5),
			want:    Summary{Model: model, Total: 5, TTFT: sec, HasTTFT: true, TokensPerSec: 100, HasTokensPerSec: true},
		},
		{
			name: "median is the nearest-rank lower middle",
			samples: []Sample{
				okSample(model, 4*sec, 400, sec),
				okSample(model, 1*sec, 100, sec),
				okSample(model, 3*sec, 300, sec),
				okSample(model, 2*sec, 200, sec),
				okSample(model, 5*sec, 500, sec),
				okSample(model, 6*sec, 600, sec),
			},
			want: Summary{Model: model, Total: 6, TTFT: 3 * sec, HasTTFT: true, TokensPerSec: 300, HasTokensPerSec: true},
		},
		{
			name: "tok/s is output tokens over last minus ttft",
			samples: repeat(Sample{
				Model: model, TTFT: 2 * sec, Last: 6 * sec, Duration: 7 * sec,
				OutputTokens: 200, Outcome: provider.AttemptOK,
			}, 5),
			want: Summary{Model: model, Total: 5, TTFT: 2 * sec, HasTTFT: true, TokensPerSec: 50, HasTokensPerSec: true},
		},
		{
			name:    "tok/s absent under five carrying samples",
			samples: append(repeat(good, 4), repeat(okSample(model, sec, 0, sec), 6)...),
			want:    Summary{Model: model, Total: 10, TTFT: sec, HasTTFT: true},
		},
		{
			name: "failures count, cancelled attempts are excluded everywhere",
			samples: append(append(repeat(good, 5),
				withOutcome(Sample{Model: model, Duration: sec}, "http_503"),
				withOutcome(Sample{Model: model, Duration: sec}, provider.AttemptTransport)),
				repeat(withOutcome(okSample(model, 9*sec, 1, sec), provider.AttemptCancelled), 20)...),
			want: Summary{Model: model, Total: 7, Failed: 2, TTFT: sec, HasTTFT: true, TokensPerSec: 100, HasTokensPerSec: true},
		},
		{
			name:    "cancelled attempts do not lift a key out of no data",
			samples: append(repeat(good, 4), repeat(withOutcome(good, provider.AttemptCancelled), 10)...),
			want:    Summary{Model: model, Total: 4, NoData: true, TTFT: sec, HasTTFT: true},
		},
		{
			name:    "other models are ignored",
			samples: append(repeat(good, 5), repeat(withOutcome(okSample("llama", 9*sec, 1, sec), "idle"), 10)...),
			want:    Summary{Model: model, Total: 5, TTFT: sec, HasTTFT: true, TokensPerSec: 100, HasTokensPerSec: true},
		},
		{
			name:    "only the newest fifty samples count",
			samples: append(repeat(withOutcome(good, "http_500"), 30), repeat(good, keepPerKey)...),
			want:    Summary{Model: model, Total: keepPerKey, TTFT: sec, HasTTFT: true, TokensPerSec: 100, HasTokensPerSec: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Summarize(tt.samples, model); got != tt.want {
				t.Errorf("Summarize() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLastModel(t *testing.T) {
	t.Parallel()
	if got := LastModel(nil); got != "" {
		t.Errorf("LastModel(nil) = %q, want empty", got)
	}
	samples := []Sample{{Model: "a"}, {Model: "b"}, {Model: "c"}}
	if got := LastModel(samples); got != "c" {
		t.Errorf("LastModel() = %q, want %q", got, "c")
	}
}
