package stubllm

import (
	"reflect"
	"testing"

	"github.com/airiclenz/apogee/internal/provider"
)

// TestRequestsWithImagesDecodeOnBothWires pins that a request carrying image parts — the
// multimodal content array on the chat wire, image blocks on the Messages wire — still decodes
// and matches a Turn, and logs each message's text: an image adds no text, and an image-only
// user message is still logged as a user message.
func TestRequestsWithImagesDecodeOnBothWires(t *testing.T) {
	t.Parallel()

	image := provider.Image{MediaType: "image/png", Data: []byte{0x89, 'P', 'N', 'G'}}
	messages := []provider.Message{
		{Role: "user", Content: "what is this?", Images: []provider.Image{image}},
		{Role: "assistant", Content: "a logo"},
		{Role: "user", Images: []provider.Image{image, image}},
	}
	want := []Message{
		{Role: "user", Content: "what is this?"},
		{Role: "assistant", Content: "a logo"},
		{Role: "user"},
	}
	for _, wire := range []provider.Wire{provider.WireOpenAI, provider.WireAnthropic} {
		t.Run(string(wire), func(t *testing.T) {
			t.Parallel()
			server := New(t, Script{Model: "stub-model", Turns: []Turn{{Text: "ok"}}})
			client := provider.NewClient(server.URL, server.Model, provider.WithWire(wire))

			reply, err := client.Respond(t.Context(), provider.Request{Messages: messages})

			if err != nil {
				t.Fatalf("respond: %v", err)
			}
			if reply.Content != "ok" {
				t.Errorf("reply content = %q, want the scripted %q", reply.Content, "ok")
			}
			requests := server.Requests()
			if len(requests) != 1 {
				t.Fatalf("requests = %d, want 1", len(requests))
			}
			if got := requests[0].Messages; !reflect.DeepEqual(got, want) {
				t.Errorf("logged messages = %+v, want %+v", got, want)
			}
			server.AssertConsumed(t)
		})
	}
}
