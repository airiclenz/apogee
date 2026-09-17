package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The bytes apogee itself injects at Turn 1, pinned per piece (ADR 0079 §6). The fixture is the
// shipped default — the embedded prompt template, the default roster, one workspace context file —
// so a change to what a stock session puts in front of the model lands as a read diff under
// `-update`, never silently. This is the second supersession of the goldens-for-rendering-surfaces
// rule at the top of internal/tuitest/golden.go (ADR 0062 call 13, superseded once by ADR 0075
// §14): the warrant is exactly the injected bytes for this one fixture, and nothing else.

// contextCostGoldenPath is where the pinned rows live, relative to the package directory `go test`
// runs in. Each line is `name<TAB>bytes`, one per ContextCost row, in wire order.
const contextCostGoldenPath = "testdata/contextcost.golden"

// contextCostFixtureWorkspace is the fixed workspace the golden Agent runs in: one AGENTS.md, so
// the `context files` row renders, and nothing else.
const contextCostFixtureWorkspace = "testdata/contextcost-ws"

// contextCostUpdateHint is how the golden is refreshed on purpose — named in every failure so the
// reader knows the move is expected to be deliberate.
const contextCostUpdateHint = "update the golden deliberately with `go test ./internal/agent -run TestContextCostGolden -update`"

// Normalisation tokens: the fixture's absolute workspace and scratch paths are machine-specific,
// so the rendered text carries these literals in their place before its bytes are counted.
const (
	contextCostWorkspaceToken = "<ws>"
	contextCostScratchToken   = "<scratch>"
)

// contextCostGoldenAgent builds the fixture Agent over workspace: baseConfig with the embedded
// default prompt, the default roster (resolveTools composes it because Config.Tools is nil and the
// workspace is set), the one context file, a native profile, depth 0 and Plan mode off — the
// shipped default mode, Ask-Before. The scratch dir is set BEFORE any render so the orientation's
// scratch bullet is in the picture and its path is a non-empty substitution target.
func contextCostGoldenAgent(t *testing.T, workspace string) *Agent {
	t.Helper()
	abs, err := filepath.Abs(workspace)
	if err != nil {
		t.Fatalf("workspace path: %v", err)
	}
	cfg := baseConfig(&recordingSink{})
	cfg.WorkspaceDir = abs
	cfg.ContextFiles = []string{"AGENTS.md"}
	cfg.SystemPrompt = config.DefaultSystemPrompt()
	cfg.Mode = domain.ModeAskBefore
	a := newProfileAgent(t, cfg, echoResponder(t, "ok"))
	a.SetScratchDir(t.TempDir())
	return a
}

// contextCostGoldenText renders the golden's text for a: ContextCost's rows in order, each as
// `name<TAB>bytes`, where a standing row's bytes are counted over its rendered text with the
// workspace and scratch paths normalised — the tool-surface row carries no path and is taken as
// reported. The two paths must be non-empty: an empty substitution target would insert its token
// between every byte.
func contextCostGoldenText(t *testing.T, a *Agent) string {
	t.Helper()
	workspace, scratch := a.cfg.WorkspaceDir, a.ScratchDir()
	if workspace == "" || scratch == "" {
		t.Fatalf("fixture Agent has workspace %q and scratch %q; both must be set before rendering", workspace, scratch)
	}
	normalise := func(text string) string {
		text = strings.ReplaceAll(text, workspace, contextCostWorkspaceToken)
		return strings.ReplaceAll(text, scratch, contextCostScratchToken)
	}
	rendered := make(map[string]string)
	for _, row := range a.standingRenders() {
		rendered[row.name] = row.rendered
	}

	var out strings.Builder
	for _, row := range a.ContextCost().Rows {
		bytes := row.Bytes
		if text, ok := rendered[row.Name]; ok {
			bytes = len(normalise(text))
		}
		fmt.Fprintf(&out, "%s\t%d\n", row.Name, bytes)
	}
	return strings.TrimSuffix(out.String(), "\n")
}

// contextCostRows parses golden text back into its rows, in order.
func contextCostRows(t *testing.T, text string) []domain.ContextCostRow {
	t.Helper()
	var rows []domain.ContextCostRow
	for _, line := range strings.Split(text, "\n") {
		name, bytes, ok := strings.Cut(line, "\t")
		if !ok {
			t.Fatalf("golden line %q is not name<TAB>bytes", line)
		}
		n, err := strconv.Atoi(bytes)
		if err != nil {
			t.Fatalf("golden line %q: %v", line, err)
		}
		rows = append(rows, domain.ContextCostRow{Name: name, Bytes: n})
	}
	return rows
}

// contextCostMoved names every row that differs between want and got — bytes that moved, a row
// that appeared, a row that vanished — so a failure says WHICH piece grew before the diff does.
func contextCostMoved(want, got []domain.ContextCostRow) []string {
	index := func(rows []domain.ContextCostRow) map[string]int {
		m := make(map[string]int, len(rows))
		for _, row := range rows {
			m[row.Name] = row.Bytes
		}
		return m
	}
	wantBytes, gotBytes := index(want), index(got)
	var moved []string
	for _, row := range want {
		switch bytes, ok := gotBytes[row.Name]; {
		case !ok:
			moved = append(moved, fmt.Sprintf("%q vanished (was %d bytes)", row.Name, row.Bytes))
		case bytes != row.Bytes:
			moved = append(moved, fmt.Sprintf("%q moved %d → %d bytes (%+d)", row.Name, row.Bytes, bytes, bytes-row.Bytes))
		}
	}
	for _, row := range got {
		if _, ok := wantBytes[row.Name]; !ok {
			moved = append(moved, fmt.Sprintf("%q appeared (%d bytes)", row.Name, row.Bytes))
		}
	}
	return moved
}

// TestContextCostGolden pins the bytes each Turn-1 piece injects for the fixed fixture — the
// prompt, the orientation, the context file and the tool menu — against testdata/contextcost.golden.
// `-update` (tuitest's package-wide flag) rewrites it; a failure names the row that moved.
func TestContextCostGolden(t *testing.T) {
	t.Parallel()

	a := contextCostGoldenAgent(t, contextCostFixtureWorkspace)

	got := contextCostGoldenText(t, a)

	tuitest.GoldenText(t, contextCostGoldenPath, got)
	if !t.Failed() {
		return
	}
	raw, err := os.ReadFile(contextCostGoldenPath)
	if err != nil {
		t.Fatalf("golden %s: %v", contextCostGoldenPath, err)
	}
	want := contextCostRows(t, strings.TrimSuffix(string(raw), "\n"))
	t.Errorf("the bytes apogee injects at Turn 1 changed: %s — if that growth is intended, %s",
		strings.Join(contextCostMoved(want, contextCostRows(t, got)), "; "), contextCostUpdateHint)
}

// TestContextCostGoldenCatchesOneByte is the tripwire's negative check: the same fixture copied
// to a temp workspace with ONE byte added to its AGENTS.md renders text that no longer matches
// the fixture's own, and the row named as moved is the context file's, by exactly that byte —
// which also proves the path normalisation holds when the workspace lives somewhere else
// entirely. The baseline is rendered in-process rather than read from the golden file, which
// TestContextCostGolden is equal to by construction and may be rewriting concurrently under
// `-update`.
func TestContextCostGoldenCatchesOneByte(t *testing.T) {
	t.Parallel()

	baseline := contextCostGoldenText(t, contextCostGoldenAgent(t, contextCostFixtureWorkspace))
	fixture, err := os.ReadFile(filepath.Join(contextCostFixtureWorkspace, "AGENTS.md"))
	if err != nil {
		t.Fatalf("fixture AGENTS.md: %v", err)
	}
	workspace := t.TempDir()
	// The byte goes ahead of the trailing newline: the context block renders the file's content
	// trimmed, so one appended after it would count as two.
	tampered := strings.TrimSuffix(string(fixture), "\n") + "x\n"
	writeWorkspaceFile(t, workspace, "AGENTS.md", tampered)
	a := contextCostGoldenAgent(t, workspace)

	got := contextCostGoldenText(t, a)

	if got == baseline {
		t.Fatalf("one added byte left the golden text unchanged:\n%s", got)
	}
	moved := contextCostMoved(contextCostRows(t, baseline), contextCostRows(t, got))
	if want := `"context files" moved`; len(moved) != 1 || !strings.HasPrefix(moved[0], want) || !strings.HasSuffix(moved[0], "(+1)") {
		t.Errorf("moved rows = %q, want exactly the context files row, one byte up", moved)
	}
}
