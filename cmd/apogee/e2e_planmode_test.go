package main

// Plan mode says what it withholds: the orientation block's Plan-only Mode bullet names the tool
// families the Plan menu never offers — the subprocess and network classes — so a model the
// default prompt tells to "verify by running" is told, on the same request, that it cannot, and
// asked to report what it would run instead. The bullet is worded from the live menu, and this
// test is what holds the two against each other: the announced list is read off the wire and each
// family it names is checked against the tool menu the very same request offered.

import (
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// planModePrompt is what the fixture's model is asked, and the phrase planmode.yaml keys its one
// capturing turn on. It asks for exactly what the default prompt's "Verify by running" line tells
// every mode to do, which is the instruction Plan cannot follow.
const planModePrompt = "Please verify the change by running the tests."

// planModeBullet is the Plan-only Mode bullet exactly as it stands on the wire with a session
// scratch dir set — the e2e home always seeds one, so the writers clause rides.
const planModeBullet = "- Mode: plan — reads, plus Apogee's own writers into the session scratch dir only; " +
	"terminal, run_tests, python_exec, web_fetch, web_search, http_request and MCP tools are withheld. " +
	"Report what you would run."

// planWithheldFamilies are the tool names the bullet announces as withheld; each must be absent from
// the Plan menu on the request that carried the announcement.
var planWithheldFamilies = []string{"terminal", "run_tests", "python_exec", "web_fetch", "web_search", "http_request"}

// TestE2EPlanModeAnnouncesWhatItWithholds: a Plan session's system prompt carries the exact Mode
// bullet, and the tool menu on that same request offers none of the six families the bullet names
// while still offering the reads — so the announcement and the menu agree, and the assertion is
// against a populated menu rather than an empty one.
func TestE2EPlanModeAnnouncesWhatItWithholds(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "planmode"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIIn(t, drv, stub, "", announcedStandingPrompt, "--mode", "plan")
	panes := watchApprovalPanes(t, drv)

	submit(drv, planModePrompt)
	drv.WaitText("to verify it.")
	drv.WaitQuiet(settled)

	req, ok := requestCarrying(stub, planModeBullet)
	if !ok {
		t.Fatalf("no request's system prompt carries the Plan bullet %q:\n%s",
			planModeBullet, systemPromptsOnTheWire(stub))
	}
	if !slices.Contains(req.Tools, "read_file") {
		t.Fatalf("the Plan menu on the announcing request offers no read_file; menu = %v", req.Tools)
	}
	for _, family := range planWithheldFamilies {
		if slices.Contains(req.Tools, family) {
			t.Errorf("the bullet announces %q as withheld but the same request's menu offers it: %v",
				family, req.Tools)
		}
	}

	if n := panes(); n != 0 {
		t.Errorf("the run raised %d approval pane(s); Plan withholds, it never asks", n)
	}
	if un := stub.Unmatched(); len(un) > 0 {
		t.Errorf("the run made %d request(s) the script did not anticipate: %v", len(un), un)
	}
	stub.AssertConsumed(t)

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// requestCarrying returns the first request whose system message contains text, so an assertion on
// the tool menu can be made against the very request that carried the announcement.
func requestCarrying(stub *stubllm.Server, text string) (stubllm.Request, bool) {
	for _, req := range stub.Requests() {
		for _, msg := range req.Messages {
			if msg.Role == "system" && strings.Contains(msg.Content, text) {
				return req, true
			}
		}
	}
	return stubllm.Request{}, false
}

// systemPromptsOnTheWire renders every request's system message for a failure report.
func systemPromptsOnTheWire(stub *stubllm.Server) string {
	var out []string
	for _, req := range stub.Requests() {
		for _, msg := range req.Messages {
			if msg.Role == "system" {
				out = append(out, msg.Content)
			}
		}
	}
	return strings.Join(out, "\n---\n")
}
