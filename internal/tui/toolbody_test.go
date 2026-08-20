package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/scheme"
)

// The five frames a tool body is drawn in now share ONE painter (bodyFrame.paint), so the thing
// worth pinning is that each of them still frames a body exactly as its own hand-written loop did:
// the branch list's ┝/┕ open and clipped, the ungrouped call's blank indent, and the two │ gutters.
//
// Each case states its want as the primitive calls that frame used to make, line by line, rather
// than as literal rows: the wrap primitives underneath are deliberately untouched by the merge, so
// a want written in terms of them fails exactly when the FRAME drifts — which is what the merge put
// at risk — and not when a wrap rule legitimately changes beneath every frame at once.
func TestEveryToolBodyFrameKeepsItsOwnFraming(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	const width = 44
	const indent = 4 // branchMarker's own measured width, which is what the ungrouped body hangs at
	body := []detailLine{
		{Text: "the first line, long enough that it has to soft-wrap somewhere below the marker"},
		{Kind: detailDiffAdded, Text: "+ an added line"},
		{Kind: detailDiffRemoved, Text: "- a removed line"},
		{Text: "the last line"},
	}
	last := func(i int) bool { return i == len(body)-1 }

	cases := []struct {
		name string
		got  func() []string
		want func() []string
	}{
		{
			name: "renderDetails — the expanded branch list leads every line with its own ┝/┕",
			got:  func() []string { return renderDetails(th, body, width) },
			want: func() []string {
				var out []string
				for i, d := range body {
					out = append(out, hangingWrap(th, detailStyle(th, d.Kind, true),
						branchMarker(last(i)), d.Text, width)...)
				}
				return out
			},
		},
		{
			name: "clipDetails — the collapsed branch list keeps those markers under the row budget",
			got: func() []string {
				rows, _ := clipDetails(th, body, width)
				return rows
			},
			want: func() []string {
				var out []string
				for i, d := range body {
					rows, _ := clipWrap(th, detailStyle(th, d.Kind, false), branchMarker(last(i)), d.Text,
						width, collapsedBranchRows)
					out = append(out, rows...)
				}
				return out
			},
		},
		{
			name: "renderSubDetails — an ungrouped call's body hangs at the branch marker's indent",
			got: func() []string {
				return renderSubDetails(th, toolView{Details: newToolBody(body)}, indent, width)
			},
			want: func() []string {
				var out []string
				for _, d := range body {
					out = append(out, hangingWrap(th, detailStyle(th, d.Kind, true),
						strings.Repeat(" ", indent), d.Text, width)...)
				}
				return out
			},
		},
		{
			name: "renderExpandedMember — an open super-group member's body runs under its own gutter",
			got: func() []string {
				rows := renderExpandedMember(th, toolView{Target: "main.go", Details: newToolBody(body)},
					superMemberMarker(true), superMemberGutter, width, toolRowCells(th, width))
				return rows[1 : len(rows)-1] // between the ▼ leader row and the see-less row
			},
			want: func() []string {
				var out []string
				for _, d := range body {
					out = append(out, gutteredWrap(th, detailStyle(th, d.Kind, true), superMemberGutter,
						superMemberGutter, d.Text, toolRowCells(th, width))...)
				}
				return out
			},
		},
		{
			name: "renderSubAgentMemberRows — a spanned delegation's report runs under the member gutter",
			got: func() []string {
				// No task text, so the prompt rows a spanned member closes with are empty
				// (subAgentPromptRows) and what is left under the ▼ leader row is the report body.
				rows, _ := renderSubAgentMemberRows(th, toolView{Target: "explore", Details: newToolBody(body)},
					memberGutter, width, width, true, true)
				return rows[1:]
			},
			want: func() []string {
				var out []string
				for _, d := range body {
					out = append(out, gutteredWrap(th, detailStyle(th, d.Kind, true), memberGutter,
						memberGutter, d.Text, width)...)
				}
				return out
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, want := tc.got(), tc.want()
			if !reflect.DeepEqual(got, want) {
				t.Errorf("the frame painted\n%q\nwant the framing it has always had\n%q", got, want)
			}
		})
	}
}

// The frames differ from one another, which is the whole reason the painter takes a spec rather than
// a flag. A merge that collapsed two of them into one would still satisfy the per-frame pins above
// (each want would collapse with it), so the distinctions are asserted directly: the collapsed
// branch list is not the open one, the blank indent is not a ┝/┕ branch, and a │ gutter is neither.
func TestTheToolBodyFramesStayDistinct(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	const width = 44
	line := []detailLine{{Text: strings.Repeat("word ", 20)}}

	paints := map[string][]string{}
	for name, frame := range map[string]bodyFrame{
		"expanded branch":   expandedBranchFrame(),
		"collapsed branch":  collapsedBranchFrame(),
		"sub-detail indent": subDetailFrame(4),
		"member gutter":     openMemberFrame(memberGutter),
	} {
		rows, _ := frame.paint(th, line, width)
		if len(rows) == 0 {
			t.Fatalf("the %s frame painted no row at all", name)
		}
		paints[name] = rows
	}

	if reflect.DeepEqual(paints["expanded branch"], paints["collapsed branch"]) {
		t.Error("the collapsed branch frame paints the open one's rows; want the row budget and the dim tone")
	}
	if len(paints["collapsed branch"]) != collapsedBranchRows {
		t.Errorf("the collapsed branch frame spent %d rows on one line; want its budget of %d",
			len(paints["collapsed branch"]), collapsedBranchRows)
	}
	if reflect.DeepEqual(paints["expanded branch"], paints["sub-detail indent"]) {
		t.Error("the sub-detail frame paints a ┝/┕ branch; want the marker's width in blanks")
	}
	if reflect.DeepEqual(paints["sub-detail indent"], paints["member gutter"]) {
		t.Error("the member frame hangs in blanks; want its │ gutter on every continuation row")
	}
}
