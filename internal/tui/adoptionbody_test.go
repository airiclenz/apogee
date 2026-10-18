package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The adoption pane's body (adoptionbody.go)
// ----------------------------------------------------------------------------

// docsServer is the repo stdio server of the eighty-column mockup
// (docs/layout/user-questions-layout.md §Adoption pane): proposed for the first time, its `env:`
// loading a project file the program never names, and no `env-allowlist:`.
var docsServer = domain.Proposal{
	Kind:        domain.ProposalMCPServer,
	Handle:      "docs",
	Text:        `{"name":"docs"}`,
	Fingerprint: "docs-pin-1",
	Fields: []domain.ProposalField{
		{Label: "transport", Values: []string{"stdio"}},
		{Label: "command", Values: []string{"node"}},
		{Label: "args", Shape: domain.ProposalList, Values: []string{"tools/docs-server.js", "--root", "./docs"}},
		{Label: "env", Shape: domain.ProposalList, Values: []string{"NODE_OPTIONS=--require ./hook.js"}},
	},
	Files: []string{"hook.js", "tools/docs-server.js"},
}

// docsSearchServer is the fifty-column mockup's server: adopted before, its `env` and its program
// changed since, and a value long enough to wrap.
var docsSearchServer = domain.Proposal{
	Kind:        domain.ProposalMCPServer,
	Handle:      "docs-search",
	Text:        `{"name":"docs-search"}`,
	Fingerprint: "docs-search-pin-2",
	Fields: []domain.ProposalField{
		{Label: "transport", Values: []string{"stdio"}},
		{Label: "command", Values: []string{"node"}},
		{Label: "args", Shape: domain.ProposalList, Values: []string{"tools/docs-server.js", "--root", "./docs"}},
		{Label: "env", Shape: domain.ProposalList,
			Values: []string{"NODE_OPTIONS=--require ./tools/hooks/trace-every-call.js"}},
	},
	Files:   []string{"tools/docs-server.js", "tools/hooks/trace-every-call.js"},
	Changed: []string{"env", "tools/docs-server.js"},
}

// fmtReaction is the second eighty-column mockup's reaction: adopted before, its script edited
// since, a `run:` argv and a webhook `gate:` that sends a variable it names.
var fmtReaction = domain.Proposal{
	Kind:        domain.ProposalReaction,
	Handle:      "fmt",
	Text:        `{"id":"fmt"}`,
	Fingerprint: "fmt-pin-2",
	Fields: []domain.ProposalField{
		{Label: "run", Shape: domain.ProposalList, Values: []string{"sh", "scripts/fmt.sh"}},
		{Label: "gate", Shape: domain.ProposalGroup, Fields: []domain.ProposalField{
			{Label: "url", Values: []string{"https://hooks.example.com/apogee/gate"}},
			{Label: "headers-env", Shape: domain.ProposalMap, Values: []string{"Authorization: HOOK_TOKEN"}},
		}},
	},
	Files:   []string{"scripts/fmt.sh"},
	Changed: []string{"scripts/fmt.sh"},
}

// adoptionFrame is the frame of a session that started with proposals waiting, at width × height.
func adoptionFrame(t *testing.T, width, height int, proposals ...domain.Proposal) (Model, string) {
	t.Helper()
	m := newTestModelEng(t, &fakeEngine{}, adoptionOpts(&adoptionLog{}, proposals...))
	m = step(t, m, tea.WindowSizeMsg{Width: width, Height: height})
	return m, plain(m.View())
}

// paneInteriors is every boxed row of frame with its border and padding taken off and its trailing
// blanks trimmed — the text a pane painted, row by row, in the order it was painted.
func paneInteriors(frame string) []string {
	var interiors []string
	for _, row := range strings.Split(frame, "\n") {
		if !strings.HasPrefix(row, "│ ") || !strings.HasSuffix(row, "│") {
			continue
		}
		interior := strings.TrimSuffix(strings.TrimPrefix(row, "│ "), "│")
		interiors = append(interiors, strings.TrimRight(interior, " "))
	}
	return interiors
}

// assertPaneRuns holds frame to painting want as consecutive pane rows, starting at want's first.
func assertPaneRuns(t *testing.T, frame string, want []string) {
	t.Helper()
	interiors := paneInteriors(frame)
	start := slices.Index(interiors, want[0])
	if start < 0 || start+len(want) > len(interiors) || !slices.Equal(interiors[start:start+len(want)], want) {
		t.Fatalf("the pane does not paint the mockup's rows %q in order:\n%s", want, frame)
	}
}

// A repo server's pane shows what Adoption pins between its title and its rows, verbatim and
// exactly as the mockups draw it: what it runs, then what it carries — an unset allowlist stated as
// the floor it is — then the pinned files; at fifty columns the title is cut, the reason and the
// floor fact wrap two cells in, and the long `env:` value wraps at its space under its own `- `.
func TestAdoptionPaneShowsARepoServerBody(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		width    int
		proposal domain.Proposal
		want     []string
	}{
		{"eighty columns", 80, docsServer, []string{
			"use MCP server entry `docs` in this project?",
			"transport: stdio",
			"command: node",
			"args:",
			"  - tools/docs-server.js",
			"  - --root",
			"  - ./docs",
			"",
			"env:",
			"  - NODE_OPTIONS=--require ./hook.js",
			"env-allowlist: not set — the platform floor only",
			"",
			"pinned files:",
			"  - hook.js",
			"  - tools/docs-server.js",
			"",
			"  adopt    — it runs here until the entry or a pinned file changes",
			"❯ not now  — it stays inert; the offer comes back at the next start-up",
			"  reject   — it stays inert, and is not offered again until it changes",
			"",
			"↑/↓ select · ⏎ choose · esc close",
		}},
		{"fifty columns", 50, docsSearchServer, []string{
			"use MCP server entry `docs-search` in this pr…",
			"`env` and `tools/docs-server.js` changed since",
			"  you adopted it",
			"",
			"transport: stdio",
			"command: node",
			"args:",
			"  - tools/docs-server.js",
			"  - --root",
			"  - ./docs",
			"",
			"env:",
			"  - NODE_OPTIONS=--require",
			"    ./tools/hooks/trace-every-call.js",
			"env-allowlist: not set — the platform floor",
			"  only",
			"",
			"pinned files:",
			"  - tools/docs-server.js",
			"  - tools/hooks/trace-every-call.js",
			"",
			"  adopt    — it runs here until the entry or a",
			"           pinned file changes",
			"❯ not now  — it stays inert; the offer comes",
			"           back at the next start-up",
			"  reject   — it stays inert, and is not",
			"           offered again until it changes",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, frame := adoptionFrame(t, tc.width, 60, tc.proposal)

			assertPaneRuns(t, frame, tc.want)
		})
	}
}

// A reaction adopted before whose pinned script was edited since leads its body with the re-ask
// reason the code builds (changedReason) — painted exactly, as its own row — and then its actions,
// a webhook hanging its own fields one level in and naming, never reading, the variable it sends.
func TestAdoptionPaneShowsTheReaskReason(t *testing.T) {
	t.Parallel()

	_, frame := adoptionFrame(t, 80, 60, fmtReaction)

	reason := changedReason(fmtReaction.Changed)
	if reason != "`scripts/fmt.sh` changed since you adopted it" {
		t.Fatalf("changedReason = %q, want the spec's sentence", reason)
	}
	assertPaneRuns(t, frame, []string{
		"use reaction `fmt` in this project?",
		reason,
		"",
		"run:",
		"  - sh",
		"  - scripts/fmt.sh",
		"",
		"gate:",
		"  url: https://hooks.example.com/apogee/gate",
		"  headers-env:",
		"    Authorization: HOOK_TOKEN",
		"",
		"pinned files:",
		"  - scripts/fmt.sh",
	})
}

// A first proposal carries no reason, and an Allow rule's pane has no body at all: its title is the
// whole of what it pins.
func TestAdoptionPaneBodyIsOnlyAStructuredEntrys(t *testing.T) {
	t.Parallel()

	_, server := adoptionFrame(t, 80, 60, docsServer)
	m, rule := adoptionFrame(t, 80, 60, goTestRule)

	if strings.Contains(server, adoptionChangedTail) {
		t.Errorf("a first proposal names a change:\n%s", server)
	}
	if body := m.adoptionBody(); body != "" {
		t.Errorf("an Allow rule's body = %q, want none", body)
	}
	assertPaneRuns(t, rule, []string{
		"allow `go test` without asking in this project?",
		"",
		"  adopt    — it runs without asking here, until the entry changes",
	})
}

// A value is bounded at eight of its own lines, head, marker and tail, so a padded `args:` cannot
// push the fields after it off the pane; its marker hangs where its elements do.
func TestAdoptionPaneBoundsAPaddedValue(t *testing.T) {
	t.Parallel()
	padded := docsServer
	padded.Fields = slices.Clone(docsServer.Fields)
	args := make([]string, 0, 20)
	for i := range 20 {
		args = append(args, "--pad-"+string(rune('a'+i)))
	}
	padded.Fields[2] = domain.ProposalField{Label: "args", Shape: domain.ProposalList, Values: args}

	_, frame := adoptionFrame(t, 80, 60, padded)

	assertPaneRuns(t, frame, []string{
		"args:",
		"  - --pad-a",
		"  - --pad-b",
		"  - --pad-c",
		"  - --pad-d",
		"  - --pad-e",
		"  - --pad-f",
		"  … (+13 more lines)",
		"  - --pad-t",
		"",
		"env:",
	})
}

// No line the file wrote starts at column zero: a value spelled like one of the pane's labels, its
// line breaks included, hangs under its own label, so it reads as the value it is.
func TestAdoptionPaneNeverStartsTheFilesTextAtColumnZero(t *testing.T) {
	t.Parallel()
	forged := docsServer
	forged.Fields = []domain.ProposalField{
		{Label: "transport", Values: []string{"stdio"}},
		{Label: "command", Values: []string{"node\npinned files:\n- nothing.js"}},
	}

	_, frame := adoptionFrame(t, 80, 60, forged)

	assertPaneRuns(t, frame, []string{
		"command: node",
		"  pinned files:",
		"  - nothing.js",
	})
}

// On a window too short for the whole body the three answers are the floor: every one stays on the
// screen, the body keeps its first line — the re-ask reason — and counts what it cannot seat.
func TestAdoptionPaneKeepsItsAnswersOnAShortTerminal(t *testing.T) {
	t.Parallel()

	_, frame := adoptionFrame(t, 80, 20, docsSearchServer)

	interiors := paneInteriors(frame)
	for _, row := range adoptionRows(docsSearchServer) {
		if !slices.ContainsFunc(interiors, func(line string) bool { return strings.Contains(line, row[1]) }) {
			t.Errorf("the answer %q is off the screen:\n%s", row[0], frame)
		}
	}
	if !strings.Contains(frame, changedReason(docsSearchServer.Changed)) {
		t.Errorf("the re-ask reason is off the screen:\n%s", frame)
	}
	if !strings.Contains(frame, "more lines)") {
		t.Errorf("the body hid lines without counting them:\n%s", frame)
	}
}
