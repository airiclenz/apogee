package workflow

import (
	"context"
	"testing"
	"testing/fstest"
)

// keySchemeChangedMessage is what a golden-key failure says, so whoever changed a formula knows the
// fix is a new scheme, not a new golden value.
const keySchemeChangedMessage = "the item-key formula changed: add a new scheme and keep this one"

// goldenStage is the fixed stage every scheme's golden key is taken over: a fanout reading a prompt
// file and a context file, with typed returns and tools. Both task and prompt are set — Validate
// refuses that in a plan, but the key functions take any stage, and every brief field set pins
// the whole encoding.
func goldenStage() Stage {
	return Stage{
		Name: "find", Kind: StageFanout, Task: "audit {item} into {out}", Prompt: "p.md",
		Returns: ReceiptSpec{"findings": "int"}, Tools: []string{"read"}, Context: []string{"ctx.md"},
		Over: &ItemSource{List: []string{"a.go"}},
	}
}

// goldenPrompts and goldenWorkspace are the fixed prompt folder and workspace of goldenStage.
func goldenPrompts() fstest.MapFS {
	return fstest.MapFS{"p.md": {Data: []byte("audit {item} for races")}}
}

func goldenWorkspace() fstest.MapFS {
	return fstest.MapFS{"ctx.md": {Data: []byte("house rules")}}
}

// TestKeySchemeGolden pins every scheme's exact output. Scheme 1's values were computed with
// v0.23.4's code, scheme 2's with the code at 1e3efe44. A failure here means a shipped formula
// moved, which orphans every receipt a folder stored under it.
func TestKeySchemeGolden(t *testing.T) {
	t.Parallel()
	golden := map[int]struct{ fanout, verify string }{
		2: {
			fanout: "9d18da896fa0435da4a8543766c731eeb2596c7590667558a7cb15e8675744ec",
			verify: "8a50d37248d8068807dbc025cc1ee5c192c1b478ef90a4eff6ceb21e80fcec20",
		},
		1: {
			fanout: "4d8be8301be721fa5adcefd2e08b4edc5783813b1816d4f1ad79104036e0fc0a",
			verify: "841a149204a763a148cc4848cb3c989aed5569c57001bfb1c6c52d84fce393f4",
		},
	}
	if len(golden) != len(keySchemes) {
		t.Fatalf("golden keys for %d schemes, keySchemes has %d: pin every scheme", len(golden), len(keySchemes))
	}

	for _, scheme := range keySchemes {
		want, isPinned := golden[scheme.id]
		if !isPinned {
			t.Errorf("scheme %d has no golden key: pin it", scheme.id)
			continue
		}
		cases := []struct{ name, suffix, want string }{
			{name: "fanout", suffix: "", want: want.fanout},
			{name: "verify suffix", suffix: "\nsourcekey\nclaim", want: want.verify},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				input := keyInput{
					stage: goldenStage(), suffix: tc.suffix, item: Item{Label: "a", Units: []string{"a.go"}},
					prompts: goldenPrompts(), workspace: goldenWorkspace(),
				}

				got, err := scheme.key(input)

				if err != nil {
					t.Fatalf("scheme %d: %v", scheme.id, err)
				}
				if got != tc.want {
					t.Errorf("scheme %d key = %s, want %s: %s", scheme.id, got, tc.want, keySchemeChangedMessage)
				}
			})
		}
	}
}

func TestKeySchemesAreNewestFirst(t *testing.T) {
	t.Parallel()

	for index := 1; index < len(keySchemes); index++ {
		if keySchemes[index-1].id <= keySchemes[index].id {
			t.Errorf("keySchemes[%d] is scheme %d after scheme %d: want newest first", index, keySchemes[index].id, keySchemes[index-1].id)
		}
	}
}

func TestKeySchemeCurrentIsTheKeyARunWrites(t *testing.T) {
	t.Parallel()
	spawner := &recordingSpawner{script: func(_ context.Context, spec ItemSpec) (Outcome, error) {
		return Outcome{Ending: EndCompleted, Receipt: okReceipt(spec.Item.Label)}, nil
	}}
	runner := newTestRunner(t, spawner)
	runner.Prompts = goldenPrompts()
	runner.Workspace = goldenWorkspace()
	stage := goldenStage()
	stage.Task = "" // a runnable stage takes its brief from either task or prompt, never both
	items, err := Expand(*stage.Over, runner.Workspace, runner.Split)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	want, err := keySchemes[0].key(keyInput{stage: stage, item: items[0], prompts: runner.Prompts, workspace: runner.Workspace})
	if err != nil {
		t.Fatalf("current scheme: %v", err)
	}

	result := runPlan(t, runner, context.Background(), Plan{Name: "audit", Stages: []Stage{stage}})

	status, err := runner.Store.ReadStatus(result.ID)
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if got := status.Stages[0].Items[0].Key; got != want {
		t.Errorf("status.json item key = %s, want keySchemes[0]'s %s", got, want)
	}
}
