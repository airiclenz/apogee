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

	"github.com/airiclenz/apogee/internal/probe"
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

// probeContextLiveStub is the upstream a live probe measures against: every request is answered
// with the one-word reply and a fixed usage, so the report's measured column is scriptable and a
// second request — the Bypass reading — is answered rather than refused.
func probeContextLiveStub(t *testing.T, usage stubllm.Usage) *stubllm.Server {
	t.Helper()
	return stubllm.New(t, stubllm.Script{
		Model: "stub-model",
		Turns: []stubllm.Turn{{Repeat: true, Text: "OK", Usage: &usage}},
	})
}

// contextCostMeasuredCell reads the cell under the named measured column off the total row: the
// column line names the columns and the total row puts the count under its label, right-aligned,
// so the cell is the text on the total row that ends where the label ends.
func contextCostMeasuredCell(t *testing.T, report, label string) string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(report, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("the live report has %d lines, too few for a column line and a total row:\n%s", len(lines), report)
	}
	columns := lines[1]
	end := strings.Index(columns, label)
	if end < 0 {
		t.Fatalf("the column line does not name %q:\n%s", label, report)
	}
	end += len(label)
	var total string
	for _, line := range lines[2:] {
		if strings.HasPrefix(line, "  total") {
			total = line
		}
	}
	if total == "" {
		t.Fatalf("the live report has no total row:\n%s", report)
	}
	if len(total) < end {
		t.Fatalf("the total row %q ends before the %q column:\n%s", total, label, report)
	}
	cell := total[:end]
	return strings.TrimSpace(cell[strings.LastIndex(cell, "   ")+3:])
}

// TestProbeContextLiveMeasuresTurn1 drives --live against a stub whose reply carries a fixed usage
// and asserts the measured shape: the total row carries the server's own count under `measured`,
// the header labels the ratio as calibrated, the stub saw exactly one request, and that request's
// user message is the fixed one-word prompt with the reply ceiling on it.
func TestProbeContextLiveMeasuresTurn1(t *testing.T) {
	t.Parallel()
	stub := probeContextLiveStub(t, stubllm.Usage{Prompt: 777, Completion: 1})
	home := eventLinesHome(t, stub.URL, stub.Model)
	workspace := probeContextWorkspace(t)

	report := runProbeContext(t, home, workspace, stub.URL, "--live")

	if got := contextCostMeasuredCell(t, report, probe.ContextCostColumnMeasured); got != "777" {
		t.Errorf("the measured cell = %q, want the stub's 777:\n%s", got, report)
	}
	if !strings.Contains(report, "chars/token, calibrated)") {
		t.Errorf("the header does not label the ratio as calibrated:\n%s", report)
	}
	if strings.Contains(report, "estimate,") {
		t.Errorf("a live report must not label itself an estimate:\n%s", report)
	}
	requests := stub.Requests()
	if len(requests) != 1 {
		t.Fatalf("the stub saw %d request(s); --live sends exactly one:\n%v", len(requests), requests)
	}
	if got := stub.LastMessage(1); got != probeContextLivePrompt {
		t.Errorf("the request's user message = %q, want the fixed prompt %q", got, probeContextLivePrompt)
	}
	if cap := requests[0].Sampling.MaxTokens; cap == nil || *cap != probeContextLiveReplyCap {
		t.Errorf("the request's max_tokens = %v, want the reply ceiling %d", cap, probeContextLiveReplyCap)
	}
}

// TestProbeContextLiveCarriesTheCachedShare pins the bracketed cached share on the measured cell
// when the server reports one.
func TestProbeContextLiveCarriesTheCachedShare(t *testing.T) {
	t.Parallel()
	stub := probeContextLiveStub(t, stubllm.Usage{Prompt: 777, Completion: 1, Cached: 512})
	home := eventLinesHome(t, stub.URL, stub.Model)
	workspace := probeContextWorkspace(t)

	report := runProbeContext(t, home, workspace, stub.URL, "--live")

	if got := contextCostMeasuredCell(t, report, probe.ContextCostColumnMeasured); got != "777 (512 cached)" {
		t.Errorf("the measured cell = %q, want the count with its cached share:\n%s", got, report)
	}
}

// TestProbeContextLiveSendsTwiceWhenReactionsArmed asserts the Reactions delta: a config that arms
// an advise Reaction makes --live send twice — as configured, then under Bypass on a fresh Agent —
// and print both measured columns with the delta line, while an observe-only config sends once.
// The two requests carry the same fixed prompt: a user's advise Reaction fires at post-tool-result
// or file-changed, neither of which precedes Turn 1, so the wire cannot show the lane armed — the
// count and the columns are the claim.
func TestProbeContextLiveSendsTwiceWhenReactionsArmed(t *testing.T) {
	t.Parallel()
	workspace := probeContextWorkspace(t)

	t.Run("advise armed", func(t *testing.T) {
		t.Parallel()
		stub := probeContextLiveStub(t, stubllm.Usage{Prompt: 800, Completion: 1})
		home := eventLinesHome(t, stub.URL, stub.Model)
		appendHomeConfig(t, home,
			"reactions:\n"+
				"  - id: lint\n    on: [post-tool-result]\n    advise: [\"true\"]\n")

		report := runProbeContext(t, home, workspace, stub.URL, "--live")

		if got := contextCostMeasuredCell(t, report, probe.ContextCostColumnAsConfigured); got != "800" {
			t.Errorf("the as-configured cell = %q, want 800:\n%s", got, report)
		}
		if got := contextCostMeasuredCell(t, report, probe.ContextCostColumnBypass); got != "800" {
			t.Errorf("the bypass cell = %q, want 800:\n%s", got, report)
		}
		if !strings.HasSuffix(strings.TrimRight(report, "\n"), "Reactions add 0 tokens at Turn 1") {
			t.Errorf("the report does not end on the delta line:\n%s", report)
		}
		if strings.Contains(report, "armed") {
			t.Errorf("a live report must not print the estimate's armed line:\n%s", report)
		}
		requests := stub.Requests()
		if len(requests) != 2 {
			t.Fatalf("the stub saw %d request(s); an armed config sends twice:\n%v", len(requests), requests)
		}
		for n := 1; n <= 2; n++ {
			if got := stub.LastMessage(n); got != probeContextLivePrompt {
				t.Errorf("request %d's user message = %q, want the fixed prompt", n, got)
			}
		}
	})

	t.Run("observe only", func(t *testing.T) {
		t.Parallel()
		stub := probeContextLiveStub(t, stubllm.Usage{Prompt: 800, Completion: 1})
		home := eventLinesHome(t, stub.URL, stub.Model)
		appendHomeConfig(t, home,
			"reactions:\n  - id: record\n    on: [exchange-finished]\n    run: [\"true\"]\n")

		report := runProbeContext(t, home, workspace, stub.URL, "--live")

		if got := contextCostMeasuredCell(t, report, probe.ContextCostColumnMeasured); got != "800" {
			t.Errorf("the measured cell = %q, want 800:\n%s", got, report)
		}
		if got := stub.Requests(); len(got) != 1 {
			t.Errorf("the stub saw %d request(s); an observe-only config sends once:\n%v", len(got), got)
		}
	})
}

// TestProbeContextLiveNeverRunsATool pins the mechanism that keeps a tool off the floor: a reply
// that asks for a write, with usage attached, ends the Turn on the usage — the ctx is cancelled
// before dispatch — so the stub sees exactly one request (no tool result ever comes back) and the
// file the call named never appears, in Auto, where nothing else would have stopped it.
func TestProbeContextLiveNeverRunsATool(t *testing.T) {
	t.Parallel()
	workspace := probeContextWorkspace(t)
	target := filepath.Join(workspace, "never.txt")
	usage := stubllm.Usage{Prompt: 640, Completion: 20}
	stub := stubllm.New(t, stubllm.Script{
		Model: "stub-model",
		Turns: []stubllm.Turn{{
			Repeat: true,
			ToolCalls: []stubllm.ToolCall{{
				Name:      "write_file",
				Arguments: `{"path":"` + target + `","content":"the probe ran a tool\n"}`,
			}},
			Usage: &usage,
		}},
	})
	home := eventLinesHome(t, stub.URL, stub.Model)

	report := runProbeContext(t, home, workspace, stub.URL, "--live", "--mode", "auto")

	if got := contextCostMeasuredCell(t, report, probe.ContextCostColumnMeasured); got != "640" {
		t.Errorf("the measured cell = %q, want 640:\n%s", got, report)
	}
	if got := stub.Requests(); len(got) != 1 {
		t.Errorf("the stub saw %d request(s); the tool call must never come back as a result:\n%v", len(got), got)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("the tool the reply asked for ran: %s exists (stat err = %v)", target, err)
	}
}

// TestProbeContextLiveNeverWrites pins the live half's pledge: after a live probe the temp home
// holds no session record, no scratch dir and no probe fingerprint.
func TestProbeContextLiveNeverWrites(t *testing.T) {
	t.Parallel()
	stub := probeContextLiveStub(t, stubllm.Usage{Prompt: 777, Completion: 1})
	home := eventLinesHome(t, stub.URL, stub.Model)
	workspace := probeContextWorkspace(t)

	runProbeContext(t, home, workspace, stub.URL, "--live")

	for _, dir := range []string{filepath.Join(home, "sessions"), filepath.Join(home, "scratch"), probe.ProbeDir(home)} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("%s exists after a live probe (stat err = %v); --live writes nothing", dir, err)
		}
	}
	if got := stub.Requests(); len(got) != 1 {
		t.Errorf("the stub saw %d request(s); want the one measurement:\n%v", len(got), got)
	}
}

// TestProbeContextLiveRefusesAServerThatReportsNoUsage asserts the honest failure: a reply that
// carries no usage leaves nothing to measure, and the command says so rather than printing an
// estimate dressed as a measurement.
func TestProbeContextLiveRefusesAServerThatReportsNoUsage(t *testing.T) {
	t.Parallel()
	assertNoAmbientApogeeConfig(t)
	stub := stubllm.New(t, stubllm.Script{
		Model: "stub-model",
		Turns: []stubllm.Turn{{Repeat: true, Text: "OK"}},
	})
	home := eventLinesHome(t, stub.URL, stub.Model)
	workspace := probeContextWorkspace(t)

	cmd := probeContextCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--config", home, "--workspace", workspace, "--endpoint", stub.URL, "--live"})
	err := cmd.ExecuteContext(context.Background())

	if err == nil || !strings.Contains(err.Error(), "reported no usage") {
		t.Fatalf("err = %v, want the no-usage refusal; stdout:\n%s", err, out.String())
	}
	if out.Len() != 0 {
		t.Errorf("a refused live probe printed a report:\n%s", out.String())
	}
}
