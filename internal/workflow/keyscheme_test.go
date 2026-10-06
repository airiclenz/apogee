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

// goldenSource is the fanout item the golden verify and merge items key over: goldenStage's item
// "a" under its scheme-2 and scheme-1 golden fanout keys and output paths, checked by a verify
// whose partial receipt says confirmed — scheme 1's verdict rule reads confirmed, scheme 2's
// unclear.
func goldenSource() ItemResult {
	source := ItemResult{
		Key: "9d18da896fa0435da4a8543766c731eeb2596c7590667558a7cb15e8675744ec", Label: "a",
		Phase: PhaseDone, Output: "/wf/items/9d18da896fa0435da4a8543766c731eeb2596c7590667558a7cb15e8675744ec/output.md",
		Receipt: &Receipt{Status: StatusOK, Summary: "two races", Fields: map[string]any{"findings": "2"}},
		older: []schemeItem{{
			key:    "4d8be8301be721fa5adcefd2e08b4edc5783813b1816d4f1ad79104036e0fc0a",
			output: "/wf/items/4d8be8301be721fa5adcefd2e08b4edc5783813b1816d4f1ad79104036e0fc0a/output.md",
		}},
	}
	source.setVerdicts(ItemResult{
		Phase: PhaseDone, Receipt: &Receipt{Status: StatusPartial, Fields: map[string]any{VerdictField: "confirmed"}},
	})
	return source
}

// goldenChainInput is the key input of the golden verify ("verify item") or merge ("merge item")
// item over goldenSource under keySchemes[scheme]: the suffix its draft renders for that scheme.
func goldenChainInput(t *testing.T, kind string, scheme int) keyInput {
	t.Helper()
	input := keyInput{prompts: goldenPrompts(), workspace: goldenWorkspace()}
	switch kind {
	case "verify item":
		input.stage = Stage{Name: "check", Kind: StageVerify, Prompt: "p.md", Returns: verifyReturns(), Context: []string{"ctx.md"}}
		input.item = Item{Label: "a", Units: []string{"a.go"}}
		input.suffix = verifyDraft(0, input.item, goldenSource()).keySuffix(scheme)
	case "merge item":
		input.stage = Stage{Name: "report", Kind: StageMerge, Prompt: "p.md", Context: []string{"ctx.md"}}
		input.item = Item{Label: "report", Units: []string{"/wf/stages/report/manifest.md"}}
		input.suffix = "\n" + renderManifest([]ItemResult{goldenSource()}, scheme)
	default:
		t.Fatalf("unknown golden chain item %q", kind)
	}
	return input
}

// TestKeySchemeGolden pins every scheme's exact output: a fanout key, a key with a fixed suffix,
// and the keys of a verify item and a merge item over goldenSource, whose suffixes the drafts
// render per scheme. Scheme 1's values were computed with v0.23.4's code, scheme 2's with the code
// at 1e3efe44. A failure here means a shipped formula moved, which orphans every receipt a folder
// stored under it.
func TestKeySchemeGolden(t *testing.T) {
	t.Parallel()
	golden := map[int]struct{ fanout, verify, verifyItem, mergeItem string }{
		2: {
			fanout:     "9d18da896fa0435da4a8543766c731eeb2596c7590667558a7cb15e8675744ec",
			verify:     "8a50d37248d8068807dbc025cc1ee5c192c1b478ef90a4eff6ceb21e80fcec20",
			verifyItem: "97567b386864b5fb5026796e22f86ec9439ed285f4f0316e4839c1197ce51cd2",
			mergeItem:  "c69e2685cbf92524ac5d707b9ab4796ebb8a1821e42576f7b9e448a31cc03816",
		},
		1: {
			fanout:     "4d8be8301be721fa5adcefd2e08b4edc5783813b1816d4f1ad79104036e0fc0a",
			verify:     "841a149204a763a148cc4848cb3c989aed5569c57001bfb1c6c52d84fce393f4",
			verifyItem: "1c25aa3f54398bb87976ac05d0d113b649b074e16ed3d75f409ec82a9fbc6dad",
			mergeItem:  "5b9ceeb50832dd62ba70de1ecaf41c88d4416ae5e381dcdd798040e0a80add58",
		},
	}
	if len(golden) != len(keySchemes) {
		t.Fatalf("golden keys for %d schemes, keySchemes has %d: pin every scheme", len(golden), len(keySchemes))
	}

	for index, scheme := range keySchemes {
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
		chain := []struct{ name, want string }{
			{name: "verify item", want: want.verifyItem},
			{name: "merge item", want: want.mergeItem},
		}
		for _, tc := range chain {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				input := goldenChainInput(t, tc.name, index)

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
	items, _, err := Expand(*stage.Over, runner.Workspace, runner.Split)
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
