package main

// The tool-call salvage Floor guard end to end (ADR 0071, `tool-call-salvage`): a native-profile
// model that writes its call out as JSON in the visible text instead of sending it on the wire.
//
// Nothing below the composition can make this claim. A unit test proves the guard reads the call
// back out of the text; only a driven run against a scripted server proves the salvaged call
// reaches the SERVER on the next request — an assistant `tool_calls` entry with a real tool result
// answering it — which is the whole point of salvaging rather than merely reporting the mistake.

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The prompt testdata/stubllm/salvage.yaml answers with a fenced call, and the workspace file that
// call names — seeded by every driven run (e2eWorkspace).
const (
	salvagePrompt   = "Read a.txt and tell me what is in it."
	salvagedTool    = "read_file"
	salvagedArgPath = "a.txt"
)

// TestE2ESalvagedTextCallReachesTheServerAsAToolCall drives one Exchange whose only tool call was
// written into the reply's text, and reads the run back off both surfaces: the transcript shows the
// narration without the fence and the tool's own block, and the request that followed carries the
// salvaged call and its result on the wire.
func TestE2ESalvagedTextCallReachesTheServerAsAToolCall(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "salvage"))
	drv := tuitest.NewDriver(t, e2eSize)
	launchTUI(t, drv, stub)

	submit(drv, salvagePrompt)
	drv.WaitText("The file holds one line")
	drv.WaitQuiet(settled)

	// The narration survives and the fence does not: the guard hands back the text it cut the call
	// out of, so what the human reads is the sentence the model wrote around its mistake.
	frame := drv.Frame()
	if _, _, ok := frame.Find("I will read that file now."); !ok {
		t.Errorf("the transcript lost the narration around the salvaged call:\n%s", frame.String())
	}
	if _, _, ok := frame.Find("```"); ok {
		t.Errorf("the transcript still shows the fenced block the guard salvaged:\n%s", frame.String())
	}

	// And the wire. The request that followed the dispatch carries the salvaged call as an ordinary
	// assistant `tool_calls` entry, with a tool message answering the very id the guard assigned —
	// which is what makes it a call the server saw rather than a call apogee only talked about.
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
			}
		}
	}
	if !carried {
		t.Fatalf("no request carried a %s tool call; the model wrote one in its text and the guard "+
			"was meant to put it on the wire. Requests: %+v", salvagedTool, stub.Requests())
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
