package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestEveryFramePaneHasASpec pins the pane table against the framePane constants: every pane below
// paneKinds has a row with a distinct name, a valid slot and its funcs filled — a pane declared
// without a row would index a zero row, a nil func rather than a build error, and the walks over
// the table would panic on the first frame or the first click. The key claim is the one func a row
// may leave nil, and only the prompt's: its keys are handleKey's state switches, not a rung of
// keyClaimOrder. modal is true for exactly the four panes that own the keyboard through such a rung
// plus the prompt, which owns it by state. The input slot holds exactly one pane, the dropdown,
// which is what lets stackInputSlot name it rather than filter for it.
func TestEveryFramePaneHasASpec(t *testing.T) {
	t.Parallel()

	modal := map[framePane]bool{panePrompt: true, paneBrowser: true, paneSettings: true, panePicker: true, paneWorkflows: true}
	names := map[string]framePane{}
	var inputPanes []framePane
	for p := framePane(0); p < paneKinds; p++ {
		row := paneSpecs[p]
		if row.name == "" {
			t.Errorf("pane %d has no name", p)
		} else if other, dup := names[row.name]; dup {
			t.Errorf("pane %d shares its name %q with pane %d", p, row.name, other)
		}
		names[row.name] = p
		if row.open == nil {
			t.Errorf("pane %d (%s) has no open predicate", p, row.name)
		}
		if row.render == nil {
			t.Errorf("pane %d (%s) has no renderer", p, row.name)
		}
		if row.height == nil {
			t.Errorf("pane %d (%s) has no height query: the transcript clamp would panic on it", p, row.name)
		}
		if row.click == nil {
			t.Errorf("pane %d (%s) has no click: the click chain would panic on it", p, row.name)
		}
		if row.wheel == nil {
			t.Errorf("pane %d (%s) has no wheel: the wheel chain would panic on it", p, row.name)
		}
		if (row.key == nil) != (p == panePrompt) {
			t.Errorf("pane %d (%s) key claim nil = %v; only the prompt's keys live outside keyClaimOrder", p, row.name, row.key == nil)
		}
		if row.modal != modal[p] {
			t.Errorf("pane %d (%s) modal = %v, want %v", p, row.name, row.modal, modal[p])
		}
		switch row.slot {
		case slotTranscript:
		case slotInput:
			inputPanes = append(inputPanes, p)
		default:
			t.Errorf("pane %d (%s) is in slot %d, which the frame has no walk for", p, row.name, row.slot)
		}
	}

	if len(inputPanes) != 1 || inputPanes[0] != paneDropdown {
		t.Errorf("the input slot holds %v, want the dropdown alone", inputPanes)
	}
}

// TestTheFourReportsPaintInTheFramePaneOrder is the paint the goldens do not cover: every report up at
// once, on a window tall enough to seat them all. Each is on the composed frame, each opens on the
// row the one above it closes on — the framePane order, walked once (stackTranscriptSlot) — and the
// frame the human sees carries their titles in that same order.
func TestTheFourReportsPaintInTheFramePaneOrder(t *testing.T) {
	t.Parallel()

	m := bothPanesModel(t, 4)
	m.thinkingPane = reportPane{open: true}
	m.advicePane = reportPane{open: true}
	m = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 60})

	reports := []reportKind{usageReport, inspectReport, thinkingReport, adviceReport}
	titles := []string{usageTitle, inspectorTitle, thinkingTitle, adviceTitle}
	below := -1
	for i, r := range reports {
		if !m.openPanes().has(r.pane()) {
			t.Fatalf("%s is open but not in the frame's pane set", titles[i])
		}
		y0, h, ok := m.reportPaneRect(r)
		if !ok {
			t.Fatalf("%s is not on the frame", titles[i])
		}
		if below >= 0 && y0 != below {
			t.Errorf("%s starts at row %d, want %d — directly under the pane above it", titles[i], y0, below)
		}
		below = y0 + h
	}

	painted := strip(m.View().Content)
	at := -1
	for _, title := range titles {
		i := strings.Index(painted, title)
		if i < 0 {
			t.Fatalf("the frame does not paint %q:\n%s", title, painted)
		}
		if i < at {
			t.Errorf("%q is painted above the report before it:\n%s", title, painted)
		}
		at = i
	}
}

// paneFixture is one frame with a given pane up, for the tests that walk every row of the pane table.
type paneFixture struct {
	name  string
	pane  framePane
	build func(*testing.T) Model
}

// paneFixtures is one fixture per row of the pane table (paneSpecs) — the /settings pane in each of
// its three paints (the key list, the enum's sub-list, the multi-line field), the prompt in both of
// its (the approval, the ask with and without choices), and a list and a report with nothing in them — so a walk over them
// covers every renderer a row can reach and the painter's empty-offering path.
func paneFixtures() []paneFixture {
	return []paneFixture{
		{"approval prompt", panePrompt, approvalPaneModel},
		{"ask prompt", panePrompt, func(t *testing.T) Model {
			t.Helper()
			return askPaneModel(t, domain.AskRequest{
				Question: "Which one should the refactor keep, given everything the reading turned up so far?",
				Choices: []string{"alpha", "beta, the one with the longer explanation that wraps onto a second line in the pane",
					"gamma", "delta", "epsilon", "zeta"},
			})
		}},
		{"ask prompt without choices", panePrompt, func(t *testing.T) Model {
			t.Helper()
			return askPaneModel(t, domain.AskRequest{Question: "What should the new module be called?"})
		}},
		{"sessions browser", paneBrowser, func(t *testing.T) Model {
			t.Helper()
			return browserPaneModel(t, 30)
		}},
		{"empty sessions browser", paneBrowser, func(t *testing.T) Model {
			t.Helper()
			return browserPaneModel(t, 0)
		}},
		{"picker", panePicker, func(t *testing.T) Model {
			t.Helper()
			return pickerPaneModel(t, pickerCycle)
		}},
		{"workflows list", paneWorkflows, func(t *testing.T) Model {
			t.Helper()
			return workflowsPaneModel(t, workflowsAtList)
		}},
		{"workflow detail", paneWorkflows, func(t *testing.T) Model {
			t.Helper()
			return workflowsPaneModel(t, workflowsAtDetail)
		}},
		{"workflow item", paneWorkflows, func(t *testing.T) Model {
			t.Helper()
			return workflowsPaneModel(t, workflowsAtItem)
		}},
		{"settings key list", paneSettings, func(t *testing.T) Model {
			t.Helper()
			return settingsFrameModel(t, 80, 30, 12)
		}},
		{"settings enum sub-list", paneSettings, func(t *testing.T) Model {
			t.Helper()
			m, _ := settingsEditModel(t, []SettingRow{settingsEnumRow()}, &settingsWriteLog{})
			if m = step(t, m, keyEnter()); m.settings.kind != settingsEnumList {
				t.Fatalf("pane = %+v, want the value sub-list open", m.settings)
			}
			return m
		}},
		{"settings text field", paneSettings, func(t *testing.T) Model {
			t.Helper()
			return settingsTextEditModel(t, strings.Repeat("a prompt line long enough to wrap in the field once or twice over\n", 12))
		}},
		{"usage report", paneUsage, func(t *testing.T) Model {
			t.Helper()
			return bothPanesModel(t, 30)
		}},
		{"inspector pane", paneInspector, func(t *testing.T) Model {
			t.Helper()
			return bothPanesModel(t, 30)
		}},
		{"thinking pane", paneThinking, func(t *testing.T) Model {
			t.Helper()
			return thinkingPaneModel(t, 40)
		}},
		{"advice pane", paneAdvice, func(t *testing.T) Model {
			t.Helper()
			return advicePaneModel(t, 40)
		}},
		{"empty advice pane", paneAdvice, func(t *testing.T) Model {
			t.Helper()
			return advicePaneModel(t, 0)
		}},
		{"autocomplete dropdown", paneDropdown, func(t *testing.T) Model {
			t.Helper()
			return dropdownPaneModel(t, testOpts, "/")
		}},
	}
}

// overlayHeightQueryGroup is one top-level slice of the height-query sweep. The sweep over every
// pane fixture ran as ONE top-level test, the longest in the package, and the shard packer
// (scripts/test-shards.sh) cannot split a test below the top level — so it set the suite's critical
// path. Each group is its own top-level test now, over the one helper, and
// TestOverlayHeightQueryGroupsClaimEveryFixtureOnce keeps their union the whole of paneFixtures.
type overlayHeightQueryGroup int

const (
	overlayHeightPrompts overlayHeightQueryGroup = iota
	overlayHeightBrowserAndPicker
	overlayHeightWorkflows
	overlayHeightSettings
	overlayHeightReports
	overlayHeightDropdown
	overlayHeightGroupKinds
)

// overlayHeightQueryGroupPanes is each group's rows of the pane table: a fixture is swept by the
// group holding its pane.
var overlayHeightQueryGroupPanes = [overlayHeightGroupKinds][]framePane{
	overlayHeightPrompts:          {panePrompt},
	overlayHeightBrowserAndPicker: {paneBrowser, panePicker},
	overlayHeightWorkflows:        {paneWorkflows},
	overlayHeightSettings:         {paneSettings},
	overlayHeightReports:          {paneUsage, paneInspector, paneThinking, paneAdvice},
	overlayHeightDropdown:         {paneDropdown},
}

// TestOverlayHeightQueryGroupsClaimEveryFixtureOnce keeps the split sweep whole: every paneFixtures
// entry lands in exactly one group, so the union of the group tests is the sweep over every fixture,
// and no group is left sweeping nothing.
func TestOverlayHeightQueryGroupsClaimEveryFixtureOnce(t *testing.T) {
	t.Parallel()
	swept := [overlayHeightGroupKinds]int{}
	for _, fx := range paneFixtures() {
		var claims []overlayHeightQueryGroup
		for g, panes := range overlayHeightQueryGroupPanes {
			if slices.Contains(panes, fx.pane) {
				claims = append(claims, overlayHeightQueryGroup(g))
			}
		}
		if len(claims) != 1 {
			t.Errorf("fixture %q (the %s) lands in %d groups %v, want exactly one",
				fx.name, paneSpecs[fx.pane].name, len(claims), claims)
			continue
		}
		swept[claims[0]]++
	}
	for g, n := range swept {
		if n == 0 {
			t.Errorf("group %d sweeps no fixture", g)
		}
	}
}

// The height-query sweep, one top-level test per group of the pane table (see sweepOverlayHeightQuery).

func TestOverlayHeightQueryMatchesItsRenderPrompts(t *testing.T) {
	t.Parallel()
	sweepOverlayHeightQueryGroup(t, overlayHeightPrompts)
}

func TestOverlayHeightQueryMatchesItsRenderBrowserAndPicker(t *testing.T) {
	t.Parallel()
	sweepOverlayHeightQueryGroup(t, overlayHeightBrowserAndPicker)
}

func TestOverlayHeightQueryMatchesItsRenderWorkflows(t *testing.T) {
	t.Parallel()
	sweepOverlayHeightQueryGroup(t, overlayHeightWorkflows)
}

func TestOverlayHeightQueryMatchesItsRenderSettings(t *testing.T) {
	t.Parallel()
	sweepOverlayHeightQueryGroup(t, overlayHeightSettings)
}

func TestOverlayHeightQueryMatchesItsRenderReports(t *testing.T) {
	t.Parallel()
	sweepOverlayHeightQueryGroup(t, overlayHeightReports)
}

func TestOverlayHeightQueryMatchesItsRenderDropdown(t *testing.T) {
	t.Parallel()
	sweepOverlayHeightQueryGroup(t, overlayHeightDropdown)
}

// sweepOverlayHeightQueryGroup sweeps every paneFixtures entry whose pane the group holds, each as a
// parallel subtest.
func sweepOverlayHeightQueryGroup(t *testing.T, group overlayHeightQueryGroup) {
	t.Helper()
	for _, fx := range paneFixtures() {
		if !slices.Contains(overlayHeightQueryGroupPanes[group], fx.pane) {
			continue
		}
		t.Run(fx.name, func(t *testing.T) {
			t.Parallel()
			sweepOverlayHeightQuery(t, fx)
		})
	}
}

// sweepOverlayHeightQuery pins the pane table's height-equals-render contract (paneSpec.height)
// with fx's pane up: on every window the sweep crosses — a width with no room for a box, narrow and
// wide ones, heights from too short to seat any pane up to roomy, the overflow bar on and off — the
// height query of every row answers exactly the rows the rendered block takes, and 0 where the
// render is "". The frame's transcript clamp, the viewport widget's scroll clamp and the click map
// all read the query while View draws the render, so this agreement is what keeps them the same
// rows.
func sweepOverlayHeightQuery(t *testing.T, fx paneFixture) {
	t.Helper()
	base := fx.build(t)
	if !paneSpecs[fx.pane].open(base) {
		t.Fatalf("the fixture does not have the %s open", paneSpecs[fx.pane].name)
	}
	seen := false
	for _, bar := range []bool{false, true} {
		for _, width := range []int{2, 4, 24, 80, 140} {
			for height := 4; height <= 44; height += 2 {
				m := base
				m.opts.UI.ShowScrollbar = bar
				m = step(t, m, tea.WindowSizeMsg{Width: width, Height: height})
				for p := framePane(0); p < paneKinds; p++ {
					got, want := paneSpecs[p].height(m), blockRows(paneSpecs[p].render(m))
					if got != want {
						t.Errorf("bar %v, %dx%d: %s height query = %d, its render takes %d rows",
							bar, width, height, paneSpecs[p].name, got, want)
					}
				}
				seen = seen || paneSpecs[fx.pane].height(m) > 0
			}
		}
	}
	if !seen {
		t.Errorf("no window of the sweep drew the %s: the sweep pinned only its zero", paneSpecs[fx.pane].name)
	}
}
