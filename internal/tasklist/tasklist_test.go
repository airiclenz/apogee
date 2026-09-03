package tasklist

import (
	"strings"
	"testing"
)

// TestReplaceRoundTripsTheWholeList is the tool's whole contract in one assertion: what
// one call carried is what the list holds afterwards, in the order it was given.
func TestReplaceRoundTripsTheWholeList(t *testing.T) {
	t.Parallel()

	list := New()
	given := []Item{
		{Text: "read the plan"},
		{Text: "wire the parser seam", Done: true},
		{Text: "update the manual"},
	}

	if err := list.Replace(given); err != nil {
		t.Fatalf("Replace() = %v, want no error", err)
	}

	assertItems(t, list.Items(), given)
}

// TestReplaceWithAShorterListDropsTheTail pins the whole-list shape against the append
// reading of it: a call carrying two rows leaves two rows, never the old third.
func TestReplaceWithAShorterListDropsTheTail(t *testing.T) {
	t.Parallel()

	list := New()
	mustReplace(t, list, []Item{{Text: "first"}, {Text: "second"}, {Text: "third"}})

	if err := list.Replace([]Item{{Text: "first", Done: true}, {Text: "second"}}); err != nil {
		t.Fatalf("Replace() = %v, want no error", err)
	}

	assertItems(t, list.Items(), []Item{{Text: "first", Done: true}, {Text: "second"}})
}

// TestReplaceDropsEmptyTextAndTrimsWhatIsLeft pins the cleaning: padding an array with
// blanks costs the model rows in its block, so blanks never become rows.
func TestReplaceDropsEmptyTextAndTrimsWhatIsLeft(t *testing.T) {
	t.Parallel()

	list := New()

	err := list.Replace([]Item{
		{Text: "  wire the parser seam  "},
		{Text: "   "},
		{Text: ""},
		{Text: "update the manual", Done: true},
	})

	if err != nil {
		t.Fatalf("Replace() = %v, want no error", err)
	}
	assertItems(t, list.Items(), []Item{
		{Text: "wire the parser seam"},
		{Text: "update the manual", Done: true},
	})
}

// TestReplaceRefusesAnOverCapListAndKeepsThePreviousOne is the refusal contract: the
// call is rejected whole, so the model is left with the list it already had rather than
// a truncated one it would have to reconstruct.
func TestReplaceRefusesAnOverCapListAndKeepsThePreviousOne(t *testing.T) {
	t.Parallel()

	list := New()
	held := []Item{{Text: "read the plan"}}
	mustReplace(t, list, held)

	oversized := make([]Item, MaxItems+1)
	for index := range oversized {
		oversized[index] = Item{Text: "task"}
	}

	err := list.Replace(oversized)

	if err == nil {
		t.Fatal("Replace() of an over-cap list = nil, want an error")
	}
	if !strings.Contains(err.Error(), "40") {
		t.Fatalf("Replace() error %q does not name the cap it broke", err)
	}
	assertItems(t, list.Items(), held)
}

// TestReplaceRefusesAnOverLongTaskAndKeepsThePreviousOne is the same refusal on the
// other cap, and pins that the error names the offending row's own position in the call.
func TestReplaceRefusesAnOverLongTaskAndKeepsThePreviousOne(t *testing.T) {
	t.Parallel()

	list := New()
	held := []Item{{Text: "read the plan"}}
	mustReplace(t, list, held)

	err := list.Replace([]Item{
		{Text: "read the plan"},
		{Text: strings.Repeat("é", MaxTextChars+1)},
	})

	if err == nil {
		t.Fatal("Replace() of an over-long task = nil, want an error")
	}
	if !strings.Contains(err.Error(), "task 2") {
		t.Fatalf("Replace() error %q does not name the offending task", err)
	}
	assertItems(t, list.Items(), held)
}

// TestReplaceWithNothingClearsTheList pins the clear: an empty call is how a restore
// from a snapshot holding no tasks, and a model that finished everything, both land.
func TestReplaceWithNothingClearsTheList(t *testing.T) {
	t.Parallel()

	list := New()
	mustReplace(t, list, []Item{{Text: "read the plan"}})

	if err := list.Replace(nil); err != nil {
		t.Fatalf("Replace(nil) = %v, want no error", err)
	}

	if items := list.Items(); len(items) != 0 {
		t.Fatalf("Items() after Replace(nil) = %v, want empty", items)
	}
}

// TestItemsReturnsACopyTheCallerCannotWriteBackThrough is what makes the snapshot writer
// safe: a caller that sorts or edits what it was handed does not reach into the list.
func TestItemsReturnsACopyTheCallerCannotWriteBackThrough(t *testing.T) {
	t.Parallel()

	list := New()
	mustReplace(t, list, []Item{{Text: "read the plan"}})

	handed := list.Items()
	handed[0] = Item{Text: "something else entirely", Done: true}

	assertItems(t, list.Items(), []Item{{Text: "read the plan"}})
}

// TestRenderIsEmptyForAListNobodyHasWritten pins the ride-along encoding: an untouched
// list contributes nothing, so it can never be the reason a request grows a system
// message.
func TestRenderIsEmptyForAListNobodyHasWritten(t *testing.T) {
	t.Parallel()

	if rendered := New().Render(); rendered != "" {
		t.Fatalf("New().Render() = %q, want \"\"", rendered)
	}

	var zero List
	if rendered := zero.Render(); rendered != "" {
		t.Fatalf("zero List Render() = %q, want \"\"", rendered)
	}
}

// TestRenderWritesTheHeaderCountsAndOneRowPerTask pins the exact block a model reads —
// the wording, the counts and both markers — because every layer above recognises this
// text rather than re-deriving it.
func TestRenderWritesTheHeaderCountsAndOneRowPerTask(t *testing.T) {
	t.Parallel()

	list := New()
	mustReplace(t, list, []Item{
		{Text: "wire the parser seam", Done: true},
		{Text: "grill the fixture"},
		{Text: "update the manual", Done: true},
	})

	rendered := list.Render()

	want := "Task list — yours to maintain; call task_list with the COMPLETE list to update it (1 open, 2 done):\n" +
		"[✔] wire the parser seam\n" +
		"[ ] grill the fixture\n" +
		"[✔] update the manual"
	if rendered != want {
		t.Fatalf("Render() =\n%q\nwant\n%q", rendered, want)
	}
}

// TestRenderOpensWithTheFence pins the fence to the header it is built from: the engine
// forges the block by this string and a wire assertion looks for it, so the two drifting
// apart would leave the block unrecognisable.
func TestRenderOpensWithTheFence(t *testing.T) {
	t.Parallel()

	list := New()
	mustReplace(t, list, []Item{{Text: "read the plan"}})

	if rendered := list.Render(); !strings.HasPrefix(rendered, Fence) {
		t.Fatalf("Render() = %q, want it to open with the fence %q", rendered, Fence)
	}
}

// mustReplace seeds a list, failing the test when the seed itself is refused.
func mustReplace(t *testing.T, list *List, items []Item) {
	t.Helper()

	if err := list.Replace(items); err != nil {
		t.Fatalf("Replace(%v) = %v, want no error", items, err)
	}
}

// assertItems compares what the list holds against what it should, row by row.
func assertItems(t *testing.T, got, want []Item) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("Items() = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("Items()[%d] = %v, want %v", index, got[index], want[index])
		}
	}
}
