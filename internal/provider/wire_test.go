package provider

import (
	"net/http"
	"testing"
)

func TestWireForDefaultsToOpenAI(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want Wire
	}{
		{name: "empty is the openai default", in: "", want: WireOpenAI},
		{name: "openai spelled out", in: "openai", want: WireOpenAI},
		{name: "anthropic", in: "anthropic", want: WireAnthropic},
		{name: "an unknown spelling folds to openai", in: "grpc", want: WireOpenAI},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := WireFor(tc.in); got != tc.want {
				t.Errorf("WireFor(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestClientWireReportsTheOption(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		opts []Option
		want Wire
	}{
		{name: "no option is openai", opts: nil, want: WireOpenAI},
		{name: "openai asked for", opts: []Option{WithWire(WireOpenAI)}, want: WireOpenAI},
		{name: "a wire without a codec folds to openai", opts: []Option{WithWire(Wire("grpc"))}, want: WireOpenAI},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := NewClient("http://upstream", "m", tc.opts...)

			if got := client.Wire(); got != tc.want {
				t.Errorf("Wire() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The chat path override and the wire selection compose in either order: the codec is built
// once every option has run, so WithChatPath is never lost behind WithWire.
func TestClientChatPathSurvivesWireOption(t *testing.T) {
	t.Parallel()

	client := NewClient("http://upstream", "m", WithChatPath("/custom"), WithWire(WireOpenAI))

	if got := client.codec.path(); got != "/custom" {
		t.Errorf("codec path = %q, want the WithChatPath override", got)
	}
}

// setAuth is the one applier of the codec's key headers, shared by completions and discovery:
// the openai codec spells the key as a bearer token and adds nothing without one.
func TestSetAuthAppliesTheCodecHeaders(t *testing.T) {
	t.Parallel()

	withKey, without := http.Header{}, http.Header{}
	NewClient("http://upstream", "m", WithAPIKey("tok")).setAuth(withKey)
	NewClient("http://upstream", "m").setAuth(without)

	if got := withKey.Get("Authorization"); got != "Bearer tok" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer tok")
	}
	if len(without) != 0 {
		t.Errorf("headers without a key = %v, want none", without)
	}
}
