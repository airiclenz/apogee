package main

// The tool-call salvage Floor guard end to end (ADR 0071, `tool-call-salvage`): a native-profile
// model that writes its call out as JSON in the visible text instead of sending it on the wire.
//
// Nothing below the composition can make this claim. A unit test proves the guard reads the call
// back out of the text; only a driven run against a scripted server proves the salvaged call
// reaches the SERVER on the next request — an assistant `tool_calls` entry carrying the text the
// call was cut out of, with a real tool result answering it — which is the whole point of
// salvaging rather than merely reporting the mistake.

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The tool testdata/stubllm/salvage.yaml writes into its replies, the workspace file that call
// names — seeded by every driven run (e2eWorkspace) — and the wrap-up the model sends once the
// salvaged call has come back, which is how a driven case knows the round trip finished.
const (
	salvagedTool    = "read_file"
	salvagedArgPath = "a.txt"
	salvageWrapUp   = "The file holds one line"
)

// salvageCase is one CONTAINER the guard reads a written call out of, driven end to end. The three
// are the three containers the guard knows (floor.SalvageToolCall): a markdown fenced block, a
// <tool_call> tag pair, and the whole trimmed reply. They are cases rather than three near-copy
// tests because the journey is identical and only the shape the model wrote differs.
type salvageCase struct {
	name string
	// prompt is what the run types; salvage.yaml answers each one in a different container.
	prompt string
	// narration is the text the model wrote AROUND the call. The guard hands it back as the reply,
	// so it is what the transcript shows and what the next request carries. Empty for a reply that
	// was the call and nothing else, which leaves no reply behind at all.
	narration string
	// container is the marker of the container the call was written in. It must NOT survive into
	// the assistant message the salvaged call rides on: the guard hands back the text without the
	// block it took.
	container string
}

var salvageCases = []salvageCase{
	{
		name:      "fenced block",
		prompt:    "Read a.txt and tell me what is in it.",
		narration: "I will read that file now.",
		container: "```",
	},
	{
		name:      "tool_call tags",
		prompt:    "Show a.txt using the tag shape.",
		narration: "Reading it with the tag shape.",
		container: "<tool_call>",
	},
	{
		name:      "whole reply",
		prompt:    "Fetch a.txt with nothing else in the reply.",
		container: `"arguments"`,
	},
}

// TestE2ESalvagedTextCallReachesTheServerAsAToolCall drives one Exchange per container whose only
// tool call was written into the reply's text, and reads each run back off both surfaces: the
// transcript shows the narration the model wrote around its mistake, and the request that followed
// carries the salvaged call, the text it was cut out of, and the call's result on the wire.
func TestE2ESalvagedTextCallReachesTheServerAsAToolCall(t *testing.T) {
	for _, tc := range salvageCases {
		t.Run(tc.name, func(t *testing.T) {
			stub := stubllm.New(t, loadScript(t, "salvage"))
			drv := tuitest.NewDriver(t, e2eSize)
			launchTUI(t, drv, stub)

			submit(drv, tc.prompt)
			drv.WaitText(salvageWrapUp)
			drv.WaitQuiet(settled)

			assertTranscriptKeptTheNarration(t, drv.Frame(), tc)
			assertSalvagedCallReachedTheWire(t, stub, tc)
		})
	}
}

// assertTranscriptKeptTheNarration checks that the sentence the model wrote around its mistake is
// still on screen: salvaging a call out of a reply must not cost the human the reply.
//
// Only the narration. What the guard CUT is asserted on the wire instead, in
// [assertSalvagedCallReachedTheWire]: the reply is painted as it streams and the guard fires on the
// finished response, so the container's own bytes are on screen before there is anything to strip
// and no frame can carry that claim.
func assertTranscriptKeptTheNarration(t *testing.T, frame tuitest.Frame, tc salvageCase) {
	t.Helper()

	if tc.narration == "" {
		return
	}
	if _, _, ok := frame.Find(tc.narration); !ok {
		t.Errorf("the transcript lost the narration around the salvaged call:\n%s", frame.String())
	}
}

// assertSalvagedCallReachedTheWire checks the half no frame can show. The request that followed the
// dispatch carries the salvaged call as an ordinary assistant `tool_calls` entry, with a tool
// message answering the very id the guard assigned — which is what makes it a call the server saw
// rather than a call apogee only talked about — and that entry's content is the narration with the
// container cut out of it, which is the other half of what the guard hands back.
func assertSalvagedCallReachedTheWire(t *testing.T, stub *stubllm.Server, tc salvageCase) {
	t.Helper()

	var carried bool
	for _, req := range stub.Requests() {
		for _, msg := range req.Messages {
			for _, call := range msg.ToolCalls {
				if call.Name != salvagedTool {
					continue
				}
				carried = true
				if !strings.Contains(call.Arguments, salvagedArgPath) {
					t.Errorf("the salvaged call reached the wire with arguments %q, want the path the model wrote",
						call.Arguments)
				}
				if !answeredOnTheWire(req.Messages, call.ID) {
					t.Errorf("no tool result answers the salvaged call %q on the request that carried it", call.ID)
				}
				assertSalvagedTextLostTheContainer(t, msg.Content, tc)
			}
		}
	}
	if !carried {
		t.Fatalf("no request carried a %s tool call; the model wrote one in its text and the guard "+
			"was meant to put it on the wire. Requests: %+v", salvagedTool, stub.Requests())
	}
}

// assertSalvagedTextLostTheContainer checks the text the salvaged call rides on: the narration the
// model wrote, without the container the call was written in — and nothing at all where the reply
// WAS the call, since cutting that leaves no reply behind.
func assertSalvagedTextLostTheContainer(t *testing.T, content string, tc salvageCase) {
	t.Helper()

	if strings.Contains(content, tc.container) {
		t.Errorf("the assistant message carrying the salvaged call still holds the %s it was written in: %q",
			tc.container, content)
	}
	if tc.narration == "" {
		if strings.TrimSpace(content) != "" {
			t.Errorf("the reply was the call and nothing else, so it should carry no text; got %q", content)
		}
		return
	}
	if !strings.Contains(content, tc.narration) {
		t.Errorf("the assistant message carrying the salvaged call lost the narration around it: %q", content)
	}
}

// answeredOnTheWire reports whether the messages carry a tool result keyed to callID — the half of
// a dispatched call the server needs in order to match the result to the call it asked for.
func answeredOnTheWire(msgs []stubllm.Message, callID string) bool {
	for _, m := range msgs {
		if m.ToolCallID == callID {
			return true
		}
	}
	return false
}
