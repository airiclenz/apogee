package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
)

// probeContextStub is a scripted upstream that answers nothing this command should ever ask: one
// turn, so the script validates, and a request log the tests read back to prove it stayed empty.
func probeContextStub(t *testing.T) *stubllm.Server {
	t.Helper()
	return stubllm.New(t, stubllm.Script{
		Model: "stub-model",
		Turns: []stubllm.Turn{{Text: "never asked"}},
	})
}

// probeContextWorkspace is a workspace with one file, so the composition has a real root to fence
// and name in the orientation block.
func probeContextWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# fixture\n"), 0o600); err != nil {
		t.Fatalf("write the workspace fixture: %v", err)
	}
	return dir
}

// runProbeContext drives `apogee probe context` through the cobra command with the given extra
// flags and returns what it printed on stdout.
func runProbeContext(t *testing.T, home, workspace, endpoint string, extra ...string) string {
	t.Helper()
	assertNoAmbientApogeeConfig(t)

	cmd := probeContextCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(append([]string{
		"--config", home, "--workspace", workspace, "--endpoint", endpoint,
	}, extra...))
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("probe context: %v\nstderr:\n%s", err, errOut.String())
	}
	return out.String()
}

// contextCostRowBytes reads the bytes column of the named row off the printed table, so a test can
// compare two tables numerically rather than by their whole text.
func contextCostRowBytes(t *testing.T, report, row string) int {
	t.Helper()
	pattern := regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(row) + `\s+([\d,]+) B\s+~\d+$`)
	m := pattern.FindStringSubmatch(report)
	if m == nil {
		t.Fatalf("the table has no %q row:\n%s", row, report)
	}
	n, err := strconv.Atoi(strings.ReplaceAll(m[1], ",", ""))
	if err != nil {
		t.Fatalf("the %q row's bytes %q do not parse: %v", row, m[1], err)
	}
	return n
}

// TestProbeContextPrintsTheEstimate drives the command against a stub server and asserts the table
// it prints: the prompt, the tool menu and the total are rows, every token figure is an estimate,
// and the stub saw no request — the composition never dials.
func TestProbeContextPrintsTheEstimate(t *testing.T) {
	t.Parallel()
	stub := probeContextStub(t)
	home := eventLinesHome(t, stub.URL, stub.Model)
	workspace := probeContextWorkspace(t)

	report := runProbeContext(t, home, workspace, stub.URL)

	if !strings.HasPrefix(report, "Context cost — what apogee puts in front of the model at Turn 1 (mode ") {
		t.Errorf("the report does not open on the context-cost header:\n%s", report)
	}
	prompt := contextCostRowBytes(t, report, "prompt")
	menu := contextCostRowBytes(t, report, "tool menu")
	total := contextCostRowBytes(t, report, "total")
	if prompt == 0 || menu == 0 {
		t.Errorf("prompt = %d B, tool menu = %d B; both are sent on every Turn 1 and neither can be empty", prompt, menu)
	}
	if total < prompt+menu {
		t.Errorf("total = %d B is less than prompt + tool menu = %d B", total, prompt+menu)
	}
	if !strings.Contains(report, "estimate, ~4.0 chars/token") {
		t.Errorf("the header does not label the table as an estimate through the default ratio:\n%s", report)
	}
	if got := stub.Requests(); len(got) != 0 {
		t.Errorf("the stub saw %d request(s); the offline estimate must send nothing:\n%v", len(got), got)
	}
}

// TestProbeContextNamesArmedReactions pins the trailing line: a config that arms an advise
// Reaction is told the estimate cannot see its directives, and a config that arms only an observe
// Reaction is not.
func TestProbeContextNamesArmedReactions(t *testing.T) {
	t.Parallel()
	stub := probeContextStub(t)
	workspace := probeContextWorkspace(t)

	t.Run("advise armed", func(t *testing.T) {
		home := eventLinesHome(t, stub.URL, stub.Model)
		appendHomeConfig(t, home,
			"reactions:\n"+
				"  - id: lint\n    on: [post-tool-result]\n    advise: [\"true\"]\n"+
				"  - id: record\n    on: [exchange-finished]\n    run: [\"true\"]\n")

		report := runProbeContext(t, home, workspace, stub.URL)

		want := "1 advise/shape Reaction armed — their directives are measured with --live"
		if !strings.HasSuffix(strings.TrimRight(report, "\n"), want) {
			t.Errorf("the report does not end on the armed line %q:\n%s", want, report)
		}
	})

	t.Run("observe only", func(t *testing.T) {
		home := eventLinesHome(t, stub.URL, stub.Model)
		appendHomeConfig(t, home,
			"reactions:\n  - id: record\n    on: [exchange-finished]\n    run: [\"true\"]\n")

		report := runProbeContext(t, home, workspace, stub.URL)

		if strings.Contains(report, "armed") {
			t.Errorf("an observe-only config must add no armed line:\n%s", report)
		}
	})
}

// TestProbeContextComposesUnderTheStartupMode asserts the mode reaches the composition: the
// header names it, and Plan — which filters the menu — prints a smaller `tool menu` row than Auto.
func TestProbeContextComposesUnderTheStartupMode(t *testing.T) {
	t.Parallel()
	stub := probeContextStub(t)
	home := eventLinesHome(t, stub.URL, stub.Model)
	workspace := probeContextWorkspace(t)

	plan := runProbeContext(t, home, workspace, stub.URL, "--mode", "plan")
	auto := runProbeContext(t, home, workspace, stub.URL, "--mode", "auto")

	if !strings.Contains(plan, "(mode plan;") {
		t.Errorf("the plan table's header does not name its mode:\n%s", plan)
	}
	if !strings.Contains(auto, "(mode auto;") {
		t.Errorf("the auto table's header does not name its mode:\n%s", auto)
	}
	planMenu := contextCostRowBytes(t, plan, "tool menu")
	autoMenu := contextCostRowBytes(t, auto, "tool menu")
	if planMenu >= autoMenu {
		t.Errorf("plan's tool menu = %d B, auto's = %d B; Plan filters the menu and must print the smaller row", planMenu, autoMenu)
	}
}

// TestProbeContextNeitherDialsNorWrites pins the free half's pledge: after the command the temp
// home holds no scratch dir (no mkdir on the way to the estimate) and the stub saw no request (no
// beat taken).
func TestProbeContextNeitherDialsNorWrites(t *testing.T) {
	t.Parallel()
	stub := probeContextStub(t)
	home := eventLinesHome(t, stub.URL, stub.Model)
	workspace := probeContextWorkspace(t)

	runProbeContext(t, home, workspace, stub.URL)

	if _, err := os.Stat(filepath.Join(home, "scratch")); !os.IsNotExist(err) {
		t.Errorf("the home holds a scratch dir after the probe (stat err = %v); the estimate must create none", err)
	}
	if got := stub.Requests(); len(got) != 0 {
		t.Errorf("the stub saw %d request(s); the probe takes no beat:\n%v", len(got), got)
	}
}
