package main

// The context-fill notice end to end (ADR 0077): a real `apogee headless` run over a stub whose
// Exchange reads three files, asserted on what the STUB received — the tool message the model
// itself read on its next request — because a notice that was booked but never rendered onto the
// wire would look identical from inside the Agent. The unit tests in internal/agent drive the
// rungs to the character; what these prove is the announced surface: the exact fence and line a
// model on a real run is handed, once, on the result that crosses the first rung, and nothing at
// all when the key is absent.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/agent"
	"github.com/airiclenz/apogee/internal/config"
	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/stubllm"
)

const (
	// fillNoticeName is the reaction id the notice's fence, its firing line and its `/settings`
	// row are keyed by — the spelling a user reads in every one of them.
	fillNoticeName = "context-fill-notice"

	// fillNoticePrompt is the prompt the script's first turn answers (testdata/stubllm/fill-notice.yaml).
	fillNoticePrompt = "Read the three notes in this workspace."

	// fillNoticeWindow is the `context-window:` the journeys pin, and the window the line names:
	// 8192 renders as `8.2k` (internal/agent formatTokens).
	fillNoticeWindow = 8192

	// fillFixtureShare is each fixture's size as a share of the compaction line, so three reads
	// land the conversation at ~54% of it before the prompt, the calls and the read headers add
	// their own few hundred characters, and two reads leave it near 36% — the first rung crossed
	// on the third result and on no earlier one, with room on both sides.
	fillFixtureShare = 0.18

	// fillFixtureCount is how many fixtures the script reads, and the number the 50 rung needs.
	fillFixtureCount = 3

	// fillNoticeConfig is the `extraConfig` a notice journey runs with: the pinned window, so the
	// Budget carries an allocation to measure against, the key switched on, and the default system
	// prompt OFF — with no prompt and no workspace context file nothing seeds the standing message
	// (the ride-along rule: standingRenders returns nil), so both standing parts measure zero and
	// seedFillFixtures can size its files from that allocation exactly.
	fillNoticeConfig = "context-window: 8192\nuse-default-prompt: false\n" + fillNoticeName + ": true\n"

	// fillNoticeOffConfig is the same home with the key ABSENT — the shipped default.
	fillNoticeOffConfig = "context-window: 8192\n"
)

// fillNoticeLine is the shape of the one line the notice writes (internal/agent/prompts/
// context-fill-notice.txt) as this journey expects it: a percent in the fifties, because the
// third read crosses the 50 rung and stops short of the 75 one; a token count in thousands, the
// same estimate the percent was computed from; and the pinned window.
var fillNoticeLine = regexp.MustCompile(
	`context: 5\d% of the way to automatic compaction — \d+\.\dk tokens used of a 8\.2k window`)

// fillNoticeFence is the substring every fence of the notice's own opens with — the mark a request
// must NOT carry for the absence claims below to hold — assembled from the exported prefix so a
// change to the fence spelling fails here rather than passing on a stale literal.
var fillNoticeFence = domain.AdviceFencePrefix + fillNoticeName

// TestE2EFillNoticeLandsAsTheAnnouncedLine is the announced-surface journey: with the key on, the
// tool message the model reads after the crossing read ends with the exact shipped trailer — the
// fence domain.RenderAdvice composes for the notice, engine origin, at post-tool-result, on the
// Turn of that read — around the rendered fact line, and no earlier request carries the notice's
// fence at all. The Turn is 2 because the crossing read is the third call of the run, numbered as
// the event stream numbers Turns (testdata/eventlines/run.jsonl).
func TestE2EFillNoticeLandsAsTheAnnouncedLine(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "fill-notice"))
	headlessFillNotice(t, stub, fillNoticeConfig)
	stub.AssertConsumed(t)

	// Requests 1 and 2 close the first two reads: under the rung, so silent.
	for n := 1; n < fillFixtureCount; n++ {
		if got := toolResultHandedOn(t, stub, n); strings.Contains(got, fillNoticeFence) {
			t.Errorf("request %d handed the model a tool result carrying %q; the fill is under the "+
				"first rung there:\n%s", n+1, fillNoticeFence, got)
		}
	}

	got := toolResultHandedOn(t, stub, fillFixtureCount)
	line := fillNoticeLine.FindString(got)
	if line == "" {
		t.Fatalf("the crossing read's result reached the model as\n%q\nwith no line matching %s",
			got, fillNoticeLine)
	}
	want := domain.RenderAdvice(domain.AdviceSpan{
		Reaction: fillNoticeName,
		Origin:   domain.OriginEngine,
		Moment:   domain.MomentPostToolResult,
		Turn:     fillFixtureCount - 1,
	}, line)
	if !strings.HasSuffix(got, want) {
		t.Errorf("the crossing read's result reached the model as\n%q\nwant it to end with the trailer\n%q",
			got, want)
	}
	if n := strings.Count(got, adviceFenceMark); n != 1 {
		t.Errorf("the crossing read's result carries %d advice fences, want exactly the notice's one:\n%s",
			n, got)
	}
}

// TestE2EFillNoticeBooksOneFiring is the same run read off the Event stream: `--format json`
// carries exactly one `reaction_fired` line for the notice, booked under the `notice` action with
// the rung and the actual percent as its detail — the line a Driver attributes the trailer by.
func TestE2EFillNoticeBooksOneFiring(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "fill-notice"))
	stdout := headlessFillNotice(t, stub, fillNoticeConfig, "--format", formatJSON)

	fired := fillNoticeFirings(t, jsonEventLines(t, stdout))
	if len(fired) != 1 {
		t.Fatalf("the stream booked %d firings for %s (%q); want one, for the 50 rung",
			len(fired), fillNoticeName, fired)
	}
	if want := "rung 50 ("; !strings.HasPrefix(fired[0], want) {
		t.Errorf("the firing's detail is %q; want it to start with %q", fired[0], want)
	}
}

// TestE2EFillNoticeIsOffByDefault is the shipped default: the same climb in a home that never
// names the key hands the model every result whole and fenceless. The third read is the positive
// control — it reaches the model, so a silent run is the switch and not a missing read.
func TestE2EFillNoticeIsOffByDefault(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "fill-notice"))
	headlessFillNotice(t, stub, fillNoticeOffConfig)
	stub.AssertConsumed(t)

	if got := toolResultHandedOn(t, stub, fillFixtureCount); !strings.HasPrefix(got, "[File: fill-3.txt,") {
		t.Fatalf("request %d's tool result %q is not the third read, so a silent run proves nothing",
			fillFixtureCount+1, got)
	}
	for n, req := range stub.Requests() {
		for _, msg := range req.Messages {
			if strings.Contains(msg.Content, fillNoticeFence) {
				t.Errorf("request %d carries a %q message with the notice's fence; the key is absent, "+
					"so the notice is off:\n%s", n+1, msg.Role, msg.Content)
			}
		}
	}
}

// fillNoticeFirings is the detail of every reaction_fired line booked for the notice, in stream
// order, failing on one booked under any action but `notice` — the only action the notice speaks
// under. Firings of other entries are left alone: a Floor guard is free to fire on the same run.
func fillNoticeFirings(t *testing.T, lines []map[string]any) []string {
	t.Helper()

	var details []string
	for i, line := range lines {
		if line["event"] != "reaction_fired" {
			continue
		}
		if stringMember(t, i, line, "reaction") != fillNoticeName {
			continue
		}
		if action := stringMember(t, i, line, "action"); action != "notice" {
			t.Fatalf("line %d books %s under the action %q; want notice", i+1, fillNoticeName, action)
		}
		details = append(details, stringMember(t, i, line, "detail"))
	}
	return details
}

// seedFillFixtures writes the three files the script reads into workspace, each sized from the
// pinned window's History allocation at the uncalibrated ratio — fillFixtureShare of the
// compaction line apiece — rather than hand-pinned, so a change to the allocation moves the
// fixtures with the line they are measured against. Each file is lines of one fixed width, which
// read_file renders back verbatim under its one-line header.
//
// The allocation is taken exactly as the binary's own Budget takes it: fillNoticeConfig seeds no
// system prompt and the workspace holds no context file, so both standing parts measure zero
// (Measured{0, 0}, the MEASURED zero — not the unmeasured fallback) and floor at their 2% share;
// and History is then held under the emergency fold's transcript budget, which is what binds at
// this window (agent.HistoryCap — cmd/apogee is package main and cannot name the fold's own
// constants, and a re-pinned literal here would drift from that arithmetic silently).
func seedFillFixtures(t *testing.T, workspace string) {
	t.Helper()

	history := min(
		apogeectx.Allocate(fillNoticeWindow, 0, 0, apogeectx.Measured{SystemPrompt: 0, FileContext: 0}).History,
		agent.HistoryCap(fillNoticeWindow),
	)
	if history <= 0 {
		t.Fatalf("the %d-token window allocated no History; the notice would have no line to measure against",
			fillNoticeWindow)
	}
	size := int(float64(history) * apogeectx.DefaultCharsPerToken * fillFixtureShare)
	const width = 64 // 63 characters and a newline
	line := strings.Repeat("x", width-1) + "\n"
	body := strings.Repeat(line, size/width)
	for i := 1; i <= fillFixtureCount; i++ {
		path := filepath.Join(workspace, "fill-"+string(rune('0'+i))+".txt")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("seed %s: %v", path, err)
		}
	}
}

// headlessFillNotice runs one real `apogee headless` for the notice journeys and hands back what
// the answer stream carried. It is [headlessHooksIn]'s twin rather than a call to it, for one
// reason: that helper makes the workspace and runs in a single call, and this journey has to seed
// the workspace — three files sized to the compaction line — BETWEEN the two. Nothing else differs:
// the runner injected is the production [run.Once], stated rather than defaulted, because the
// notice is the engine's own Reaction and a stubbed runner produces no post-tool-result Moment for
// it to fire at; the home is written the same way, and the ambient environment is neutralised the
// same way.
func headlessFillNotice(t *testing.T, stub *stubllm.Server, extraConfig string, extra ...string) (stdout string) {
	t.Helper()

	// The environment must not move the home or the mode out from under the run.
	assertNoAmbientApogeeConfig(t)
	t.Setenv(config.EnvMode, "")

	home := t.TempDir()
	writeConfigHome(t, home, extraConfig+
		"servers:\n"+
		"  - name: stub\n"+
		"    endpoint: "+stub.URL+"\n"+
		"    model: "+stub.Model+"\n"+
		"server: stub\n")

	workspace := e2eWorkspace(t)
	seedFillFixtures(t, workspace)
	cmd := newHeadlessCommandWith(headlessDeps{runner: run.Once})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetIn(strings.NewReader(""))
	args := []string{"--config", home, "--workspace", workspace}
	args = append(args, extra...)
	cmd.SetArgs(append(args, fillNoticePrompt))
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("headless: %v\n%s", err, errBuf.String())
	}
	return outBuf.String()
}
