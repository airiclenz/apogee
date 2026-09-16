package provider

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestClassify pins the three rules the four renderers used to keep by prose: a 400 with an
// overflow marker is an overflow (never retryable, never hinted, whatever the request
// carried); 429, 5xx and the aggregator's provider_unavailable slug are retryable; an effort
// on the wire hints every non-overflow fault. The marker is read off the text as given —
// unsanitised, case-insensitively — and only a 400 can be an overflow.
func TestClassify(t *testing.T) {
	t.Parallel()

	const overflowBody = `{"error":{"message":"the request exceeds the available context size"}}`

	tests := []struct {
		name    string
		status  int
		errType string
		text    string
		effort  bool
		want    fault
	}{
		{
			name:   "400 with an overflow marker",
			status: http.StatusBadRequest,
			text:   overflowBody,
			want:   fault{overflow: true, code: http.StatusBadRequest},
		},
		{
			name:   "an overflow is never hinted, even with an effort on the wire",
			status: http.StatusBadRequest,
			text:   overflowBody,
			effort: true,
			want:   fault{overflow: true, code: http.StatusBadRequest},
		},
		{
			name:   "the marker is matched case-insensitively",
			status: http.StatusBadRequest,
			text:   "Context Length Exceeded",
			want:   fault{overflow: true, code: http.StatusBadRequest},
		},
		{
			name:   "400 without a marker is a plain fault",
			status: http.StatusBadRequest,
			text:   "jinja2.exceptions.TemplateError",
			want:   fault{code: http.StatusBadRequest},
		},
		{
			name:   "400 without a marker is hinted when the request carried an effort",
			status: http.StatusBadRequest,
			text:   "jinja2.exceptions.TemplateError",
			effort: true,
			want:   fault{code: http.StatusBadRequest, hinted: true},
		},
		{
			name:   "a marker on any status but 400 is not an overflow",
			status: http.StatusInternalServerError,
			text:   overflowBody,
			want:   fault{code: http.StatusInternalServerError, retryable: true},
		},
		{
			name:   "500 is retryable",
			status: http.StatusInternalServerError,
			text:   "boom",
			want:   fault{code: http.StatusInternalServerError, retryable: true},
		},
		{
			name:   "502 is retryable and hinted with an effort on the wire",
			status: http.StatusBadGateway,
			text:   "boom",
			effort: true,
			want:   fault{code: http.StatusBadGateway, retryable: true, hinted: true},
		},
		{
			name:   "429 is retryable",
			status: http.StatusTooManyRequests,
			text:   "slow down",
			want:   fault{code: http.StatusTooManyRequests, retryable: true},
		},
		{
			name:   "a 4xx other than 429 is not retryable",
			status: http.StatusUnauthorized,
			text:   "who are you",
			want:   fault{code: http.StatusUnauthorized},
		},
		{
			name:    "provider_unavailable is retryable without a usable code",
			status:  0,
			errType: providerUnavailable,
			text:    "upstream gone",
			want:    fault{code: 0, retryable: true},
		},
		{
			name:    "provider_unavailable is retryable even under a 4xx",
			status:  http.StatusNotFound,
			errType: providerUnavailable,
			text:    "upstream gone",
			want:    fault{code: http.StatusNotFound, retryable: true},
		},
		{
			name:    "another slug without a usable code is not retryable",
			status:  0,
			errType: "rate_limit_exceeded",
			text:    "slow down",
			want:    fault{code: 0},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := classify(tc.status, tc.errType, tc.text, tc.effort)

			if got != tc.want {
				t.Errorf("classify(%d, %q, %q, %t) = %+v, want %+v", tc.status, tc.errType, tc.text, tc.effort, got, tc.want)
			}
		})
	}
}

// TestStatusDelta_RetryableStatusRendersNotRetryable pins the one place a renderer overrides
// the classifier: a 429 or 5xx that arrives as an HTTP status is a class the fault calls
// retryable, but send already retried it before it reached statusDelta, so the terminal
// Delta keeps Retryable false (Delta.Retryable: the class the client WOULD have retried is
// the in-band case).
func TestStatusDelta_RetryableStatusRendersNotRetryable(t *testing.T) {
	t.Parallel()

	client := NewClient("http://unused.invalid", "m")

	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()

			if !classify(status, "", "boom", false).retryable {
				t.Fatalf("classify(%d) is not retryable; the override under test is moot", status)
			}
			resp := &http.Response{
				StatusCode: status,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("boom")),
			}

			delta := client.statusDelta(resp, false)

			if delta.Kind != DeltaError {
				t.Fatalf("delta = %+v, want a DeltaError", delta)
			}
			if delta.Retryable {
				t.Errorf("status %d rendered Retryable; send already retried it", status)
			}
			if want := upstreamStatusText(status, "boom", ""); delta.Err != want {
				t.Errorf("Err = %q, want %q", delta.Err, want)
			}
		})
	}
}
