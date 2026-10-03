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

// TestCodecsEncodeImageParts pins the request bodies both dialects write for a user message
// carrying images: openai turns content into an array of a text part and one image_url part per
// image, each a base64 data URL; anthropic writes one base64 image block per image ahead of the
// text block. A message without images keeps the plain shape each dialect always sent.
func TestCodecsEncodeImageParts(t *testing.T) {
	t.Parallel()

	images := []Image{
		{MediaType: "image/png", Data: []byte{0x89, 'P', 'N', 'G'}},
		{MediaType: "image/jpeg", Data: []byte{0xff, 0xd8}},
	}
	withText := Request{Model: "m", Messages: []Message{{Role: "user", Content: "what is this?", Images: images}}}
	imageOnly := Request{Model: "m", Messages: []Message{{Role: "user", Images: images[:1]}}}
	textOnly := Request{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}}
	cases := []struct {
		name  string
		codec wireCodec
		req   Request
		want  string
	}{
		{
			name:  "openai: a text part, then one image_url part per image",
			codec: &openaiCodec{},
			req:   withText,
			want:  `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"what is this?"},{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw=="}},{"type":"image_url","image_url":{"url":"data:image/jpeg;base64,/9g="}}]}],"stream":false}`,
		},
		{
			name:  "openai: an image-only message has no text part",
			codec: &openaiCodec{},
			req:   imageOnly,
			want:  `{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw=="}}]}],"stream":false}`,
		},
		{
			name:  "openai: a message without images keeps string content",
			codec: &openaiCodec{},
			req:   textOnly,
			want:  `{"model":"m","messages":[{"role":"user","content":"hi"}],"stream":false}`,
		},
		{
			name:  "anthropic: one image block per image, then the text block",
			codec: &anthropicCodec{},
			req:   withText,
			want:  `{"model":"m","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBORw=="}},{"type":"image","source":{"type":"base64","media_type":"image/jpeg","data":"/9g="}},{"type":"text","text":"what is this?"}]}],"max_tokens":4096,"stream":false,"thinking":{"type":"disabled"}}`,
		},
		{
			name:  "anthropic: an image-only message is its image block alone",
			codec: &anthropicCodec{},
			req:   imageOnly,
			want:  `{"model":"m","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBORw=="}}]}],"max_tokens":4096,"stream":false,"thinking":{"type":"disabled"}}`,
		},
		{
			name:  "anthropic: a message without images is its text block alone",
			codec: &anthropicCodec{},
			req:   textOnly,
			want:  `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"max_tokens":4096,"stream":false,"thinking":{"type":"disabled"}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body, _, err := tc.codec.encode(tc.req)

			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			if got := string(body); got != tc.want {
				t.Errorf("body mismatch\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}
