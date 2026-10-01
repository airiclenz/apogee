package tui

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"
)

// frameGeometry is what layout() owns about a frame: the viewport widget's height and scroll offset,
// the input box's height, the frame key the transcript lines were painted under, and how many lines
// that paint produced. Two View() calls are deliberately not compared: in stateRunning the status
// clock reads time.Now(), so two renders differ across a second boundary with nothing laid out.
type frameGeometry struct {
	viewportHeight int
	yOffset        int
	inputHeight    int
	painted        frameKey
	lines          int
}

// geometryOf reads the layout-owned state off m.
func geometryOf(m Model) frameGeometry {
	return frameGeometry{
		viewportHeight: m.viewport.Height(),
		yOffset:        m.viewport.YOffset(),
		inputHeight:    m.input.Height(),
		painted:        m.painted,
		lines:          len(m.lines),
	}
}

// assertSettled fails the test unless m, a model Update returned, already stands where an explicit
// layout() would put it: the tail already settled it (doc.go, "an arm mutates; Update's tail lays
// out and repaints"). The copy is laid out after m's own geometry is read, so nothing the extra
// layout touches can leak into what is compared.
func assertSettled(t *testing.T, m Model) {
	t.Helper()
	got := geometryOf(m)
	relaid := m
	relaid.layout()
	if want := geometryOf(relaid); got != want {
		t.Errorf("frame left unsettled by Update's tail:\n got %+v\nwant %+v (after an explicit layout)", got, want)
	}
}

// layoutCallFile is the one file whose layout()/refreshViewport() calls the arm scan leaves alone:
// model.go owns Update, its tail and the frame drivers, and doc.go's arm invariant names the calls
// it keeps there.
const layoutCallFile = "model.go"

// geometryMark opens the same-line comment that licenses a layout()/refreshViewport() call outside
// model.go: the arm reads geometry after the call, and the comment says what.
const geometryMark = "// geometry:"

// TestArmsLeaveLayoutToTail pins doc.go's "an arm mutates; Update's tail lays out and repaints"
// structurally: every non-test file in this package other than model.go is scanned, and a
// layout() or refreshViewport() call on any receiver (m, next, bound, …) without a same-line
// "// geometry:" comment fails here with its file:line. A call the tail already covers is dropped;
// one an arm needs because it reads the fresh frame afterwards keeps its call and says so.
func TestArmsLeaveLayoutToTail(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	scanned := 0
	for _, path := range packageGoFiles(t, false) {
		if path == layoutCallFile {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		scanned++
		for _, finding := range unannotatedLayoutCalls(fset, file) {
			t.Error(finding)
		}
	}
	if scanned == 0 {
		t.Fatal("no non-test files besides " + layoutCallFile + " were scanned — an empty scan proves nothing")
	}
}

// ...and the scanner proven on a fixture rather than assumed: unannotated calls on any receiver are
// reported at their line, an annotated one is not, nor is a comment that is not a geometry: mark, one
// on the line above, a method that merely shares the prefix, or a method value that is never called.
func TestArmsLeaveLayoutToTailBites(t *testing.T) {
	t.Parallel()

	const fixture = `package tui

func (m Model) arm() Model {
	m.layout()
	m.refreshViewport()
	m.layout() // geometry: the offset below reads the fresh frame
	m.refreshViewport() // geometry: AtBottom below reads the repaint
	m.layout() // belt: the tail covers this
	// geometry: a mark on the line above licenses nothing
	m.layout()
	m.refreshViewportAnchored()
	_ = m.layout
	return m
}

func foldBeat(next Model, bound *Model) {
	next.layout()
	bound.layout()
	next.layout() // geometry: the beat reads the placed pane's rows
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", fixture, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	got := unannotatedLayoutCalls(fset, file)

	want := []string{
		"fixture.go:4: m.layout() outside model.go without a same-line \"// geometry:\" comment — leave layout to Update's tail, or say what the arm reads after it (doc.go, \"an arm mutates\")",
		"fixture.go:5: m.refreshViewport() outside model.go without a same-line \"// geometry:\" comment — leave layout to Update's tail, or say what the arm reads after it (doc.go, \"an arm mutates\")",
		"fixture.go:8: m.layout() outside model.go without a same-line \"// geometry:\" comment — leave layout to Update's tail, or say what the arm reads after it (doc.go, \"an arm mutates\")",
		"fixture.go:10: m.layout() outside model.go without a same-line \"// geometry:\" comment — leave layout to Update's tail, or say what the arm reads after it (doc.go, \"an arm mutates\")",
		"fixture.go:17: next.layout() outside model.go without a same-line \"// geometry:\" comment — leave layout to Update's tail, or say what the arm reads after it (doc.go, \"an arm mutates\")",
		"fixture.go:18: bound.layout() outside model.go without a same-line \"// geometry:\" comment — leave layout to Update's tail, or say what the arm reads after it (doc.go, \"an arm mutates\")",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("scanner findings differ\n got:\n  %s\nwant:\n  %s",
			strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// unannotatedLayoutCalls reports, in source order, every argument-less layout() or
// refreshViewport() call in file whose line carries no comment opening with geometryMark. The file
// must be parsed with parser.ParseComments, or every call reads as unannotated.
func unannotatedLayoutCalls(fset *token.FileSet, file *ast.File) []string {
	marked := map[int]bool{}
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if strings.HasPrefix(comment.Text, geometryMark) {
				marked[fset.Position(comment.Slash).Line] = true
			}
		}
	}

	var findings []string
	ast.Inspect(file, func(node ast.Node) bool {
		call, isCall := node.(*ast.CallExpr)
		if !isCall || len(call.Args) != 0 {
			return true
		}
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !isSel || (sel.Sel.Name != "layout" && sel.Sel.Name != "refreshViewport") {
			return true
		}
		pos := fset.Position(sel.Sel.Pos())
		if marked[pos.Line] {
			return true
		}
		findings = append(findings, fmt.Sprintf(
			"%s:%d: %s.%s() outside %s without a same-line %q comment — leave layout to Update's tail, or say what the arm reads after it (doc.go, \"an arm mutates\")",
			pos.Filename, pos.Line, types.ExprString(sel.X), sel.Sel.Name, layoutCallFile, geometryMark))
		return true
	})
	return findings
}
