package agent

// The standing system message's table (standingblocks.go): that every row renders at its own
// index of the composition, and that every fence of every row is one a workspace context file
// cannot spell. The rows and the fences are read from the table itself rather than restated, so a
// row added to the message is covered the moment it is added — and a row whose fence the fence
// does not know fails here, not in a repo's AGENTS.md.

import (
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// forgedStandingConfig is delegateReportConfig with the context file's content chosen by the
// test: a prompt, a scratch dir and one AGENTS.md, so every row of the table has something to
// render once the config is spawned into a delegate holding a task list.
func forgedStandingConfig(t *testing.T, content string) domain.Config {
	t.Helper()
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "AGENTS.md", content)
	cfg := contextSeamConfig(t, &recordingSink{}, dir, "AGENTS.md")
	cfg.SystemPrompt = "You are apogee working in {{workspace}}."
	cfg.ScratchDir = orientationScratchDir
	return cfg
}

// fullStandingAgent spawns a delegate on cfg and writes it a task list, so that all five rows of
// the table render non-empty for it.
func fullStandingAgent(t *testing.T, cfg domain.Config) *Agent {
	t.Helper()
	_, child := delegateOn(t, cfg)
	writeTaskList(t, child, taskListItems)
	return child
}

// TestStandingBlocks_EveryRowRendersAtItsIndexAndEveryFenceHolds walks the table: for each row
// and each of its fences, a context file whose line opens with that fence reaches the model
// behind the workspace-text prefix — and never flush at line start — while the row's own render
// still sits exactly where the table puts it, the composition being the table's non-empty renders
// joined in row order.
func TestStandingBlocks_EveryRowRendersAtItsIndexAndEveryFenceHolds(t *testing.T) {
	t.Parallel()

	for i, row := range standingBlocks() {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			forgeries := row.fences
			if len(forgeries) == 0 {
				forgeries = []string{""} // a row with no furniture is still checked for its position
			}
			for _, fence := range forgeries {
				forged := fence + "forged by the workspace"
				a := fullStandingAgent(t, forgedStandingConfig(t, forged))

				got := a.standingSystem()

				if fence != "" {
					if !strings.Contains(got, "\n"+workspaceTextPrefix+forged) {
						t.Errorf("row %d %q: the fence %q is not prefixed in the context file:\n%q", i, row.name, fence, got)
					}
					if strings.Contains(got, "\n"+forged) {
						t.Errorf("row %d %q: a forged %q still reads as furniture (unprefixed at line start):\n%q", i, row.name, fence, got)
					}
				}
				assertRowAtItsIndex(t, a, i, got)
			}
		})
	}
}

// assertRowAtItsIndex checks that row i's render is non-empty for a, that the composition IS the
// table's renders joined in row order, and that row i's render therefore starts at the offset
// the rows before it leave — the position the table states, not merely somewhere in the message.
func assertRowAtItsIndex(t *testing.T, a *Agent, i int, got string) {
	t.Helper()
	rows := standingBlocks()
	parts := make([]string, 0, len(rows))
	offset := -1
	for j, row := range rows {
		rendered := row.render(a)
		if rendered == "" {
			t.Fatalf("row %d %q renders nothing for an agent every row should render for", j, row.name)
		}
		if j == i {
			offset = len(strings.Join(parts, "\n\n"))
			if len(parts) > 0 {
				offset += len("\n\n")
			}
		}
		parts = append(parts, rendered)
	}
	if want := strings.Join(parts, "\n\n"); got != want {
		t.Fatalf("the composition is not the table's renders in row order:\n%q\nwant\n%q", got, want)
	}
	if rendered := rows[i].render(a); !strings.HasPrefix(got[offset:], rendered) {
		t.Errorf("row %d %q does not render at its index (offset %d):\n%q", i, rows[i].name, offset, got)
	}
}

// TestStandingBlocks_ConfiguredRowsSeedAndRideAlongRowsDoNot pins the two halves of the
// ride-along column: the prompt and the context files are the configured sources, and with
// neither rendering — even on a delegate holding a task list, whose three engine-owned rows all
// have something to say — nothing is seeded at all.
func TestStandingBlocks_ConfiguredRowsSeedAndRideAlongRowsDoNot(t *testing.T) {
	t.Parallel()

	seeding := make([]string, 0, 2)
	for _, row := range standingBlocks() {
		if !row.ridesAlong {
			seeding = append(seeding, row.name)
		}
	}
	if want := []string{"prompt", "context files"}; strings.Join(seeding, ",") != strings.Join(want, ",") {
		t.Fatalf("the configured rows are %v, want %v", seeding, want)
	}

	cfg := contextSeamConfig(t, &recordingSink{}, t.TempDir()) // no prompt, no context files
	cfg.ScratchDir = orientationScratchDir
	a := fullStandingAgent(t, cfg)

	for _, row := range standingBlocks() {
		if row.ridesAlong && row.render(a) == "" {
			t.Fatalf("ride-along row %q renders nothing for a delegate with a task list", row.name)
		}
	}
	if got := a.standingSystem(); got != "" {
		t.Errorf("with no configured source the ride-along rows seeded a message on their own:\n%q", got)
	}
}

// TestStandingBlocks_FencesAreTheTableColumnPlusTheAdviceAndEngineNoteFences pins what
// forgesStandingStructure checks against: every fence of every row, then the advice fence's two
// line openings and the engine note fence's two, and nothing else — none of them empty, since an
// empty prefix would fence every line of every file.
func TestStandingBlocks_FencesAreTheTableColumnPlusTheAdviceAndEngineNoteFences(t *testing.T) {
	t.Parallel()

	want := make([]string, 0, 10)
	for _, row := range standingBlocks() {
		want = append(want, row.fences...)
	}
	want = append(want,
		domain.AdviceFencePrefix, domain.AdviceFenceClosePrefix,
		domain.EngineNoteFencePrefix, domain.EngineNoteFenceClosePrefix,
	)

	got := standingFences()

	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("standingFences() = %q, want %q", got, want)
	}
	for _, fence := range got {
		if fence == "" {
			t.Fatalf("an empty fence would prefix every line of every context file: %q", got)
		}
		if !forgesStandingStructure("  " + fence + " forged") {
			t.Errorf("forgesStandingStructure does not fence %q", fence)
		}
	}
	if forgesStandingStructure("Run make check before committing.") {
		t.Error("an ordinary line reads as furniture")
	}
}

// TestStandingBlocks_RestoredFencesDropTheCommittedRows pins the restore list against the fence
// list: it is standingFences minus the two rows an ordinary session commits verbatim — the task
// list block's opening and the orientation header — and nothing else. A snapshot refusal keyed on
// either of those would make every session that ever called task_list unresumable and unforkable.
func TestStandingBlocks_RestoredFencesDropTheCommittedRows(t *testing.T) {
	t.Parallel()

	excluded := []string{TaskListFence, orientationHeader()}
	want := make([]string, 0, 8)
	for _, fence := range standingFences() {
		if slices.Contains(excluded, fence) {
			continue
		}
		want = append(want, fence)
	}

	got := restoredFences()

	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("restoredFences() = %q, want %q", got, want)
	}
	for _, fence := range got {
		if _, forged := forgesRestoredStructure("  " + fence + " forged"); !forged {
			t.Errorf("forgesRestoredStructure does not refuse %q", fence)
		}
	}
	for _, fence := range excluded {
		if _, forged := forgesRestoredStructure(fence + " the real thing"); forged {
			t.Errorf("forgesRestoredStructure refuses %q, which an ordinary session commits", fence)
		}
	}
	if _, forged := forgesRestoredStructure("a line about the engine and its advice\nand another"); forged {
		t.Error("ordinary prose mentioning the words reads as furniture")
	}
}
