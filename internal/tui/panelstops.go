package tui

import (
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/sidepanel"
)

// The side panel's stops (ADR 0111 decisions 4–5): the rows of the column a click acts on, each
// with the target that click follows and an identity that outlives the row it sits on. The pointer
// resolves a body row through them ([Model.panelTargetAt]), so a stop's target is the click at
// every row of its span; navigation mode walks the same list.

// panelStopID is what a stop is, independent of the row it is laid out on: a section's header, a
// sub-agent row by its run, a changed-file row by its path, the tasks section's one task card or
// the usage section's one report. It is comparable, so a cursor holding one finds its stop again
// after the column re-lays out.
type panelStopID struct {
	section  domain.SidePanelSection
	isHeader bool
	run      sidepanel.RunKey // a sub-agent row's run
	path     string           // a changed-file row's path
}

// panelStop is one stop of the column: the rows it spans — y the first, which is where it is
// painted and where a cursor on it sits, height how many — the click it follows, and its identity.
type panelStop struct {
	y      int
	height int
	target panelTarget
	id     panelStopID
}

// spans reports whether screen row y is one of the stop's rows.
func (s panelStop) spans(y int) bool {
	return y >= s.y && y < s.y+s.height
}

// panelStops is the column's stops in paint order, laid out over rows rows by the same layout the
// paint reads ([panelLayout] over [Model.panelSections]). Every header is a stop, its target the
// pin glyph's toggle; a body row is one where it has a target, and adjacent rows that share one
// target are one stop (the tasks body, the usage rows). The `+N more` row and a body row with
// nothing to open are none.
func (m Model) panelStops(rows int) []panelStop {
	sections := m.panelSections()
	if len(sections) == 0 {
		return nil
	}
	layout := panelLayout(sections, rows)
	stops := make([]panelStop, 0, len(layout))
	for y, line := range layout {
		if line.section < 0 {
			continue
		}
		section := sections[line.section]
		stop := panelStop{y: y, height: 1}
		if line.body < 0 {
			stop.target = panelTarget{kind: panelTargetPin, section: section.kind, isPinned: section.isPinned}
			stop.id = panelStopID{section: section.kind, isHeader: true}
		} else {
			stop.target, stop.id = m.panelBodyStop(section, line.body)
			if stop.target.kind == panelTargetNone {
				continue
			}
		}
		if n := len(stops); n > 0 && stops[n-1].y+stops[n-1].height == y && stops[n-1].target == stop.target {
			stops[n-1].height++
			continue
		}
		stops = append(stops, stop)
	}
	return stops
}

// panelBodyStop is body row row of section as a stop: the detail view a click on it opens and the
// identity it has, or no target where the transcript holds nothing it could open. It reads the
// source rows the section was painted from, so the row it answers for is the row on the screen.
func (m Model) panelBodyStop(section panelSection, row int) (panelTarget, panelStopID) {
	id := panelStopID{section: section.kind}
	switch section.kind {
	case domain.SidePanelSubAgents:
		if row >= len(section.subAgents) {
			return panelTarget{}, id
		}
		id.run = section.subAgents[row].Run
		at := m.panelRunHeadAt(id.run)
		if _, ok := m.runOpenableAt(at); !ok {
			return panelTarget{}, id
		}
		return panelTarget{kind: panelTargetRun, entry: at}, id
	case domain.SidePanelTasks:
		if at, ok := m.panelTaskCardAt(); ok {
			return panelTarget{kind: panelTargetBlock, entry: at, run: m.viewedRun()}, id
		}
	case domain.SidePanelChangedFiles:
		if row >= len(section.files) {
			return panelTarget{}, id
		}
		file := section.files[row]
		id.path = file.Path
		if at, ok := m.panelWriteAt(file); ok {
			return panelTarget{kind: panelTargetBlock, entry: at, run: m.transcript.entries[at].run()}, id
		}
	case domain.SidePanelUsage:
		return panelTarget{kind: panelTargetUsage}, id
	}
	return panelTarget{}, id
}
