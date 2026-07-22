package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// An ordinary loop request carries NO logprobs fields at all: the pair is opt-in, so adding it
// for `apogee probe model` left every existing caller's bytes on the wire untouched.
func TestBuildBody_OmitsLogProbsUnlessAsked(t *testing.T) {
	t.Parallel()
	body := captureBody(t, Request{Messages: []Message{{Role: "user", Content: "hi"}}})

	for _, key := range []string{"logprobs", "top_logprobs"} {
		if _, present := body[key]; present {
			t.Errorf("an unasked-for request carried %q; the field must be omitted entirely", key)
		}
	}
}

// A request that asks for the candidate distribution sends both fields, because a server needs
// top_logprobs to report alternatives rather than only the token it drew.
func TestBuildBody_LogProbsRequestsCandidates(t *testing.T) {
	t.Parallel()
	body := captureBody(t, Request{Messages: []Message{{Role: "user", Content: "hi"}}, LogProbs: true})

	if body["logprobs"] != true {
		t.Errorf("logprobs = %v; want true", body["logprobs"])
	}
	if got := body["top_logprobs"]; got != float64(topLogProbsCount) {
		t.Errorf("top_logprobs = %v; want %d", got, topLogProbsCount)
	}
}

// The candidate tokens for the FIRST generated position are what reach the caller — the shape
// of the distribution, not the probabilities, which drift with temperature and server build.
func TestRespond_ParsesTopCandidates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "alternatives reported",
			body: `{"choices":[{"message":{"content":" Paris"},"logprobs":{"content":[{"token":" Paris",` +
				`"top_logprobs":[{"token":" Paris"},{"token":" the"}]},{"token":"!"}]},"finish_reason":"stop"}]}`,
			want: []string{" Paris", " the"},
		},
		{
			name: "logprobs without alternatives falls back to the chosen token",
			body: `{"choices":[{"message":{"content":"x"},"logprobs":{"content":[{"token":"x"}]},"finish_reason":"stop"}]}`,
			want: []string{"x"},
		},
		{
			name: "a server that exposes nothing yields nothing",
			body: `{"choices":[{"message":{"content":"x"},"finish_reason":"stop"}]}`,
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			resp, err := NewClient(srv.URL, "m").Respond(context.Background(), Request{LogProbs: true})
			if err != nil {
				t.Fatalf("Respond: %v", err)
			}
			if !reflect.DeepEqual(resp.TopCandidates, tc.want) {
				t.Errorf("TopCandidates = %#v; want %#v", resp.TopCandidates, tc.want)
			}
		})
	}
}
