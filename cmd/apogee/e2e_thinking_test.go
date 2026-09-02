package main

// The thinking channel's whole journey, driven over BOTH wire spellings.
//
// apogee decoded only `reasoning_content` until this plan, so against Ollama or OpenRouter — both
// of which spell the same channel `reasoning` — the model's thinking streamed into a field nothing
// read and /thinking said `no thinking recorded yet`. The unit tests in internal/provider pin the
// decode; what nobody could see below them is the journey: wire → provider → the thinking board →
// the pane's rows → the cells on screen. That is a claim about a frame, which internal/tuitest can
// hold still and internal/stubllm can reproduce.
//
// Both subtests run the SAME conversation against fixtures that differ by one key
// (testdata/stubllm/thinking.yaml and thinking-reasoning-field.yaml), which is what makes the pair
// a control and an experiment rather than two tests: the `reasoning` case fails without the
// provider's alias, and the `reasoning_content` case must pass on both sides of it — that half is
// what pins that no server already working regressed.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// thinkingPaneMarker is the /thinking pane's own furniture — the hint row it always paints,
// whatever the script said — so the test waits on the PANE rather than on its content.
const thinkingPaneMarker = "↑/↓ scroll · esc close"

// The conversation both fixtures script, named once. thinkingThought is the distinctive string the
// pane has to show: it appears nowhere in the reply, so a frame carrying it can only have come
// through the thinking channel.
const (
	thinkingPrompt  = "Which option is better?"
	thinkingThought = "Weighing the two options before answering."
	thinkingReply   = "The second option."
)

// thinkingEmpty is what the pane says when the board holds nothing (internal/tui/thinkingpane.go).
// It is asserted ABSENT as well as the thought asserted present, because a pane that painted the
// empty row and the thought at once would be a bug the positive check alone would pass.
const thinkingEmpty = "no thinking recorded yet"

// TestE2EThinkingPaneShowsEitherWireSpelling drives a session, opens /thinking, and reads the
// model's reasoning off the frame — once per wire spelling of the channel.
func TestE2EThinkingPaneShowsEitherWireSpelling(t *testing.T) {
	for _, tc := range []struct{ name, fixture string }{
		{name: "reasoning_content, as llama.cpp and vLLM send it", fixture: "thinking"},
		{name: "reasoning, as Ollama and OpenRouter send it", fixture: "thinking-reasoning-field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := stubllm.New(t, loadScript(t, tc.fixture))
			drv := tuitest.NewDriver(t, e2eSize)
			launchTUI(t, drv, stub)

			submit(drv, thinkingPrompt)
			drv.WaitText(thinkingReply)

			submit(drv, "/thinking")
			drv.WaitText(thinkingPaneMarker)
			drv.WaitQuiet(settled)

			frame := drv.Frame()
			if _, _, ok := frame.Find(thinkingThought); !ok {
				t.Errorf("the /thinking pane does not show %q, which the server sent on the "+
					"thinking channel:\n%s", thinkingThought, frame)
			}
			if _, _, ok := frame.Find(thinkingEmpty); ok {
				t.Errorf("the /thinking pane says %q, and the server sent thinking:\n%s",
					thinkingEmpty, frame)
			}
			closePane(drv, thinkingPaneMarker)
		})
	}
}

// TestE2EThinkingFixturesDifferOnlyInTheSpelling pins the pairing the test above rests on. If the
// two fixtures were to drift apart in anything but `reasoning_field`, the subtests would still
// pass while no longer comparing the two spellings of one conversation — so the drift is caught
// here, once and readably, rather than nowhere.
func TestE2EThinkingFixturesDifferOnlyInTheSpelling(t *testing.T) {
	content := thinkingFixtureBody(t, "thinking")
	bare := thinkingFixtureBody(t, "thinking-reasoning-field")

	if want := strings.Replace(content, "      reasoning: "+thinkingThought+"\n",
		"      reasoning: "+thinkingThought+"\n      reasoning_field: reasoning\n", 1); bare != want {
		t.Errorf("the two thinking fixtures differ by more than reasoning_field:\ngot:\n%s\nwant:\n%s",
			bare, want)
	}
}

// thinkingFixtureBody is a fixture's turns with its comment lines dropped: the two files explain
// themselves differently on purpose, and it is the SCRIPT that has to match.
func thinkingFixtureBody(t *testing.T, name string) string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", "stubllm", name+".yaml"))
	if err != nil {
		t.Fatalf("read the %s fixture: %v", name, err)
	}
	var kept []string
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}
