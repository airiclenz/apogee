package main

// Enterable workflow stages end to end (ADR 0090): a Recipe the human launches runs as a Workflow
// whose block paints one row per stage and none of its items' work, and each row is a way INTO
// that work — a stage of several items opens a stage view listing them, an item opens as its run
// view, and esc walks back up one level at a time.
//
// The seams are pinned in internal/tui (the fold, the stage rows, the stage level of the view
// stack). What only a driven run shows is the whole rope: a recipe skill found in the workspace, the
// engine's real phase events reaching the block, the item children's receipts reaching their rows,
// and a pointer driving the ways in and out.
//
// The recipe is two stages on purpose: one item, then three. The first is the single stage — it
// opens its item's run straight away — and the second is the stage that has a view of its own.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The wordings this run asserts on. The trails and the stage-row endings are internal/tui's
// (subagentblock.go's breadcrumb, workflowblock.go's stage row) restated here because cmd/apogee
// cannot import them: they are what apogee promises a human, so a rename over there has to fail
// here. The item briefs and summaries are the fixture's (testdata/stubllm/recipe-stages.yaml).
const (
	// The recipe's launch line, its Workflow's name, and the model's answer once the Workflow is over
	// — the moment the block has painted its last.
	stagesLaunch = "/" + stagesRecipeID
	stagesAnswer = "Every stage ran."
	stagesTitle  = "Staged build"

	// The block's two stage rows, by the glyph that seats each and its name, and the slot a finished
	// stage row ends with: the word, then the ▶ that says the row opens.
	planRow     = "┝ plan"
	buildRow    = "┕ build"
	stageRowEnd = "done ▶"

	// The ways back, one per level: the single stage opens its item's run under a trail that ends on
	// the stage, the stage view under the stage, and an item opened from it under the item — the
	// four crumbs.
	planTrail  = "← main › " + stagesRecipeID + " › plan"
	buildTrail = "← main › " + stagesRecipeID + " › build"
	betaTrail  = buildTrail + " › beta"
	mainCrumb  = "← main"

	// The one item's summary, and the one item's work its run view shows: the finish card.
	outlineSummary = "outline planned"
	finishCard     = "finish ▶"
)

// stagesItems are the second stage's items and the summaries their receipts carry, in the order the
// stage view lists them.
var stagesItems = []struct{ name, summary string }{
	{"alpha", "alpha built"},
	{"beta", "beta built"},
	{"gamma", "gamma built"},
}

// stagesRecipeID is the fixture recipe skill's id, the token that launches it and the Workflow's
// name its block and every trail carry.
const stagesRecipeID = "stages"

// stagesRecipe is the fixture recipe skill: a fanout of one item, then a fanout of three. Each
// brief is unique, so the stub answers each item by its own last message.
const stagesRecipe = "---\nid: " + stagesRecipeID + "\nsummary: build in two stages\n" +
	"recipe:\n" +
	"  - name: plan\n    kind: fanout\n    over:\n      list: [outline]\n    task: \"plan the {item}\"\n" +
	"  - name: build\n    kind: fanout\n    over:\n      list: [alpha, beta, gamma]\n" +
	"    task: \"build part {item}\"\n" +
	"---\nRun the two stages.\n"

// TestE2EWorkflowStages launches a two-stage recipe and walks its stages: the top-level block
// shows stage rows and no item output, the one-item stage opens its item's run, the three-item stage
// opens a stage view of its items, an item opens under the four-crumb trail, and esc, esc is main.
func TestE2EWorkflowStages(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, loadScript(t, "recipe-stages"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIIn(t, drv, stub, stagesWorkspace(t), "")
	waitIdle(drv)
	drv.WaitQuiet(settled)

	submit(drv, stagesLaunch)
	drv.WaitText(stagesAnswer)
	drv.WaitText(stagesTitle)
	waitIdle(drv)
	drv.WaitQuiet(settled)

	// The top level — one row per stage, each finished and each openable, and nothing any item said
	// or was told: every receipt was ok, so no trouble line stands for one either.
	top := drv.Frame()
	for _, row := range []string{planRow, buildRow} {
		if got := strings.TrimRight(rowContaining(t, top, row), " "); !strings.HasSuffix(got, stageRowEnd) {
			t.Errorf("the stage row %q does not end %q: %q", row, stageRowEnd, got)
		}
	}
	assertNoItemWork(t, top, "the top-level frame")
	// Refresh with `go test ./cmd/apogee -run TestE2EWorkflowStages -update`.
	tuitest.Golden(t, "workflow-stages", top, goldenRedactions(sess)...)

	// The single stage opens its item's run directly, under a trail that ends on the stage.
	clickText(t, drv, planRow)
	drv.WaitText(planTrail)
	drv.WaitQuiet(settled)
	single := drv.Frame()
	if crumb := strings.TrimSpace(rowContaining(t, single, planTrail)); strings.Contains(crumb, planTrail+" ›") {
		t.Errorf("the one-item stage's trail goes past the stage: %q", crumb)
	}
	if !holds(single, finishCard) {
		t.Errorf("the one-item stage did not open its item's run:\n%s", single)
	}
	leaveTo(drv, planTrail, planRow)

	// The stage of three opens a stage view: one row per item, each carrying its receipt's summary,
	// and nothing of the other stage.
	clickText(t, drv, buildRow)
	drv.WaitText(buildTrail)
	drv.WaitQuiet(settled)
	stage := drv.Frame()
	for _, item := range stagesItems {
		if row := rowContaining(t, stage, "┕ "+item.name+" "); !strings.Contains(row, item.summary) {
			t.Errorf("the stage view's %s row does not carry its summary %q: %q", item.name, item.summary, row)
		}
	}
	if holds(stage, outlineSummary) {
		t.Errorf("the build stage's view shows the plan stage's item:\n%s", stage)
	}
	tuitest.Golden(t, "workflow-stage-view", stage, goldenRedactions(sess)...)

	// An item opens as its run view under the four-crumb trail, and shows that item's run alone.
	clickText(t, drv, "┕ beta ")
	drv.WaitText(betaTrail)
	drv.WaitQuiet(settled)
	item := drv.Frame()
	if !holds(item, finishCard) {
		t.Errorf("the beta row did not open its run:\n%s", item)
	}
	for _, sibling := range []string{"alpha", "gamma"} {
		if holds(item, sibling) {
			t.Errorf("beta's run view shows %q, which belongs to a sibling:\n%s", sibling, item)
		}
	}

	// esc walks up one level — back to the stage view — and esc again is main, the block's stage
	// rows standing where they stood.
	drv.Press(tuitest.Esc)
	drv.WaitGone(betaTrail)
	drv.WaitText(buildTrail)
	drv.Press(tuitest.Esc)
	drv.WaitGone(mainCrumb)
	drv.WaitText(buildRow)
	drv.WaitQuiet(settled)
	back := drv.Frame()
	if !holds(back, planRow) || !holds(back, stagesAnswer) {
		t.Errorf("esc, esc did not return to the conversation:\n%s", back)
	}
	assertNoItemWork(t, back, "the frame esc, esc returned to")

	stub.AssertConsumed(t)
	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// stagesWorkspace is the seeded scratch workspace with the fixture recipe skill in it, where a
// workspace's own skills are discovered (`<ws>/.apogee/skills/<id>/SKILL.md`).
func stagesWorkspace(t *testing.T) string {
	t.Helper()

	ws := e2eWorkspace(t)
	dir := filepath.Join(ws, ".apogee", "skills", stagesRecipeID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the recipe skill's folder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(stagesRecipe), 0o600); err != nil {
		t.Fatalf("write the recipe skill: %v", err)
	}
	return ws
}

// clickText clicks the first cell of the first row holding text — how a pointer aims at a row it
// can see.
func clickText(t *testing.T, drv *tuitest.Driver, text string) {
	t.Helper()

	x, y, ok := drv.Frame().Find(text)
	if !ok {
		t.Fatalf("no row of the frame holds %q:\n%s", text, drv.Frame())
	}
	click(drv, x, y)
}

// leaveTo presses esc out of the view whose trail is on screen and waits for the conversation's
// row that proves main is back.
func leaveTo(drv *tuitest.Driver, trail, mainRow string) {
	drv.Press(tuitest.Esc)
	drv.WaitGone(trail)
	drv.WaitText(mainRow)
	drv.WaitQuiet(settled)
}

// assertNoItemWork fails when the frame shows anything an item child said or was told — its
// brief, its finish card or its receipt's summary. It is the top level's claim: the block's stage
// rows stand for the items' work, and the work itself lives behind them.
func assertNoItemWork(t *testing.T, f tuitest.Frame, where string) {
	t.Helper()

	shown := []string{"plan the outline", "build part", finishCard, outlineSummary}
	for _, item := range stagesItems {
		shown = append(shown, item.summary)
	}
	for _, text := range shown {
		if holds(f, text) {
			t.Errorf("%s shows the item output %q:\n%s", where, text, f)
		}
	}
}
