package agent

// The compaction lane (ADR 0076 D6, 2026-10-21 amendments): a user `advise:` entry on
// compaction-finished starts as a top-level fold begins and is joined after its Replace, its answer
// fenced ahead of the next user-role message's text — the next Exchange's opening for a fold between
// Exchanges, the overflow bridge for a fold mid-Exchange — and never in the summary or the system
// prompt. When it lands it retires the same reaction's fences from the protected prefix's first user
// message; a failure leaves them. Bypass, delegates and a fold that did not run start or attach
// nothing, and the lane's runs are kept apart from the session-start lane's.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/testtime"
)

// compactionFinished is the one Moment the re-brief entries in this file subscribe to.
var compactionFinished = []domain.Moment{domain.MomentCompactionFinished}

// rebriefText is what the re-brief command prints ahead of the fold's spelling.
const rebriefText = "re-read the plan"

// rebriefEntry is a user advise entry on on whose command prints rebriefText and the fold that ran
// it, read from the document's environment.
func rebriefEntry(id string, on []domain.Moment) domain.Reaction {
	return userAdvise(id, on, "/bin/sh", "-c", `printf '%s for %s\n' "`+rebriefText+`" "$APOGEE_REACTION_FOLD"`)
}

// compactFenceFor is the header line the compaction lane fences a user entry id's advice under.
func compactFenceFor(id string) string {
	return domain.AdviceFencePrefix + id + " (user origin) at compaction-finished"
}

// compactAdviseAgent builds a top-level Agent, through newAgent, over cfg with reactions armed and a
// history far past the 8k window's allocation, so the next Step's estimate fold runs.
func compactAdviseAgent(
	t *testing.T,
	cfg domain.Config,
	responder *scriptedUpstream,
	reactions ...domain.Reaction,
) *Agent {
	t.Helper()
	cfg.Reactions = reactions
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	seedLargeConv(a)
	return a
}

// staleBriefs fences an earlier briefing of the re-brief entry and one of another entry onto the
// protected prefix's first user message, as a session-start join would have.
func staleBriefs(t *testing.T, a *Agent) {
	t.Helper()
	for _, brief := range []struct{ id, text string }{{"rebrief", "OLD BRIEF"}, {"other", "OTHER BRIEF"}} {
		span := domain.AdviceSpan{Reaction: brief.id, Origin: domain.OriginUser, Moment: domain.MomentSessionStarted}
		if !a.conv.AdviseUserMessage(0, span, brief.text) {
			t.Fatalf("seeding %s's brief on the prefix was refused", brief.id)
		}
	}
}

// firstWireUser is the content of req's first user message — the protected prefix's.
func firstWireUser(t *testing.T, req stubllm.Request) string {
	t.Helper()
	for _, m := range req.Messages {
		if m.Role == "user" {
			return m.Content
		}
	}
	t.Fatalf("request carries no user message")
	return ""
}

// TestCompactAdvise_AFoldBetweenExchangesFencesTheOpening — the estimate fold at an opening starts
// the entry, and the opening that follows the Replace carries its answer fenced ahead of the
// prompt, booked as one advise firing; the summary and the system prompt carry none.
func TestCompactAdvise_AFoldBetweenExchangesFencesTheOpening(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	sink := &recordingSink{}
	up := compactSpyResponder(t, "FOLDED-SUMMARY")
	a := compactAdviseAgent(t, autoCompactConfig(sink), up, rebriefEntry("rebrief", compactionFinished))

	runExchange(t, a, "the fresh question")

	if up.summaryCalls() != 1 {
		t.Fatalf("summarizer calls = %d, want the one estimate fold", up.summaryCalls())
	}
	main := up.mains()[0]
	opening := lastUserContent(t, main)
	if !strings.HasPrefix(opening, compactFenceFor("rebrief")) || !strings.Contains(opening, rebriefText+" for auto\n") ||
		!strings.HasSuffix(opening, "\n\nthe fresh question") {
		t.Errorf("opening = %q, want the fenced re-brief ahead of the prompt", opening)
	}
	if got := wireFenceCount(main, compactFenceFor("rebrief")); got != 1 {
		t.Errorf("request carries %d re-brief fences, want 1 (the opening's alone)", got)
	}
	if got := wireFenceCount(up.lastSummary(), compactFenceFor("rebrief")); got != 0 {
		t.Errorf("summary call carries %d re-brief fences, want none", got)
	}
	fired := sessionAdviseFirings(sink, "rebrief")
	if len(fired) != 1 || fired[0].Action != actionAdvise || fired[0].Moment != domain.MomentCompactionFinished {
		t.Errorf("booked %+v, want one advise firing at compaction-finished", fired)
	}
}

// TestCompactAdvise_AnEmergencyFoldFencesTheBridge — a fold mid-Exchange joins the entry onto the
// overflow bridge before the request is rebuilt: the retried request ends on the bridge carrying
// the re-brief ahead of its fixed text, which stands intact after it.
func TestCompactAdvise_AnEmergencyFoldFencesTheBridge(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	cfg := autoCompactConfig(&recordingSink{})
	cfg.Reactions = []domain.Reaction{rebriefEntry("rebrief", compactionFinished)}
	up := recoveryResponder(t, "recovered reply", "EMERGENCY-SUMMARY", true)
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	seedToolCallConv(a) // small, so no estimate fold runs: the overflow is the only fold

	if err := a.Submit(domain.UserInput{Text: "keep going"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	mains := up.mains()
	if len(mains) != 2 {
		t.Fatalf("main requests = %d, want the overflowed one and the retry", len(mains))
	}
	retry := mains[1]
	bridge := retry.Messages[len(retry.Messages)-1]
	if bridge.Role != string(domain.RoleUser) || !strings.HasPrefix(bridge.Content, compactFenceFor("rebrief")) ||
		!strings.Contains(bridge.Content, rebriefText+" for emergency\n") ||
		!strings.HasSuffix(bridge.Content, "\n\n"+overflowBridge) {
		t.Errorf("retry's last message = %+v, want the bridge with the re-brief fenced ahead of its fixed text", bridge)
	}
	if got := wireFenceCount(retry, compactFenceFor("rebrief")); got != 1 {
		t.Errorf("retry carries %d re-brief fences, want 1 (the bridge's alone)", got)
	}
	assertRequestTemplateLegal(t, retry)
}

// TestCompactAdvise_ALandedReBriefRetiresTheStaleSpan — the re-brief strips its own reaction's
// earlier fence from the protected prefix's first user message and leaves another reaction's.
func TestCompactAdvise_ALandedReBriefRetiresTheStaleSpan(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	up := compactSpyResponder(t, "FOLDED-SUMMARY")
	a := compactAdviseAgent(t, autoCompactConfig(&recordingSink{}), up, rebriefEntry("rebrief", compactionFinished))
	staleBriefs(t, a)

	runExchange(t, a, "the fresh question")

	prefix := firstWireUser(t, up.mains()[0])
	if strings.Contains(prefix, "OLD BRIEF") || !strings.Contains(prefix, "OTHER BRIEF") ||
		!strings.HasSuffix(prefix, "the OVERARCHING-GOAL to keep in the prefix") {
		t.Errorf("prefix = %q, want the stale re-brief stripped and the other reaction's brief kept", prefix)
	}
	if got := a.conv.At(0).Advice; len(got) != 1 || got[0].Reaction != "other" || !got[0].Leading {
		t.Errorf("prefix ledger = %+v, want the other reaction's leading row alone", got)
	}
}

// TestCompactAdvise_AFailedRunAttachesNothingAndKeepsTheStaleSpan — a command that fails or outlives
// its deadline adds nothing to the opening and strips nothing from the prefix.
func TestCompactAdvise_AFailedRunAttachesNothingAndKeepsTheStaleSpan(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	for _, c := range []struct {
		name    string
		script  string
		timeout time.Duration
	}{
		{name: "a non-zero exit", script: "exit 3"},
		{name: "a command that outlives its deadline", script: "sleep 5", timeout: testtime.Fixed(200 * time.Millisecond)},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			entry := userAdvise("rebrief", compactionFinished, "/bin/sh", "-c", c.script)
			entry.Timeout = c.timeout
			up := compactSpyResponder(t, "FOLDED-SUMMARY")
			a := compactAdviseAgent(t, autoCompactConfig(&recordingSink{}), up, entry)
			staleBriefs(t, a)

			runExchange(t, a, "the fresh question")

			main := up.mains()[0]
			if got := lastUserContent(t, main); got != "the fresh question" {
				t.Errorf("opening = %q, want the bare prompt", got)
			}
			if prefix := firstWireUser(t, main); !strings.Contains(prefix, "OLD BRIEF") || !strings.Contains(prefix, "OTHER BRIEF") {
				t.Errorf("prefix = %q, want both earlier briefs kept", prefix)
			}
		})
	}
}

// TestCompactAdvise_BypassRunsNothing — Bypass switches advise off (D9): the fold never starts the
// command and the opening goes out bare.
func TestCompactAdvise_BypassRunsNothing(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	marker := filepath.Join(t.TempDir(), "the-command-ran")
	cfg := autoCompactConfig(&recordingSink{})
	cfg.Bypass = true
	up := compactSpyResponder(t, "FOLDED-SUMMARY")
	a := compactAdviseAgent(t, cfg, up,
		userAdvise("rebrief", compactionFinished, "/bin/sh", "-c", `: > "`+marker+`"; printf 'advice\n'`))

	runExchange(t, a, "the fresh question")

	if up.summaryCalls() != 1 {
		t.Fatalf("summarizer calls = %d, want the fold to run under Bypass", up.summaryCalls())
	}
	if got := lastUserContent(t, up.mains()[0]); got != "the fresh question" {
		t.Errorf("opening = %q, want the bare prompt", got)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("the command ran under Bypass")
	}
}

// TestCompactAdvise_ADelegateFoldStartsNoRun — a delegate's fold re-briefs no user entry, so it
// leaves the compaction lane empty.
func TestCompactAdvise_ADelegateFoldStartsNoRun(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	a := compactAdviseAgent(t, autoCompactConfig(&recordingSink{}), compactSpyResponder(t, "FOLDED-SUMMARY"),
		rebriefEntry("rebrief", compactionFinished))
	a.depth = 1 // the nesting depth isDelegate reads: this Agent is a child one level down
	a.turns.restore(turnSnapshot{inExchange: true, exchangeStart: 1})

	if r := a.foldFor(context.Background(), 1, foldOverflow); r.end != foldEndFolded {
		t.Fatalf("result = %+v, want a fold that ran", r)
	}
	if got := a.briefing.compaction.waiting(); len(got) != 0 {
		t.Errorf("a delegate's fold left %d runs, want none", len(got))
	}
}

// TestCompactAdvise_AFoldThatDidNotRunDiscardsItsRuns — a fold that faulted, was cancelled or found
// nothing to fold hands no run to the lane; a skip leaves an earlier fold's runs standing, so the
// next opening still carries that fold's re-brief, once.
func TestCompactAdvise_AFoldThatDidNotRunDiscardsItsRuns(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	t.Run("faulted", func(t *testing.T) {
		t.Parallel()
		a := compactAdviseAgent(t, autoCompactConfig(&recordingSink{}), overflowResponder(t),
			rebriefEntry("rebrief", compactionFinished))
		if r := a.foldFor(context.Background(), 1, foldOnDemand); r.end != foldEndFaulted {
			t.Fatalf("result = %+v, want a faulted fold", r)
		}
		if got := a.briefing.compaction.waiting(); len(got) != 0 {
			t.Errorf("a faulted fold left %d runs, want none", len(got))
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		t.Parallel()
		a := compactAdviseAgent(t, autoCompactConfig(&recordingSink{}), compactSpyResponder(t, "UNREACHED"),
			rebriefEntry("rebrief", compactionFinished))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if r := a.foldFor(ctx, 1, foldOnDemand); r.end != foldEndCancelled {
			t.Fatalf("result = %+v, want a cancelled fold", r)
		}
		if got := a.briefing.compaction.waiting(); len(got) != 0 {
			t.Errorf("a cancelled fold left %d runs, want none", len(got))
		}
	})
	t.Run("skipped after a fold that ran", func(t *testing.T) {
		t.Parallel()
		up := compactSpyResponder(t, "FOLDED-SUMMARY")
		a := compactAdviseAgent(t, autoCompactConfig(&recordingSink{}), up, rebriefEntry("rebrief", compactionFinished))
		if skipped, err := a.Compact(context.Background()); err != nil || skipped {
			t.Fatalf("first Compact = %v, %v; want a fold that ran", skipped, err)
		}
		if skipped, err := a.Compact(context.Background()); err != nil || !skipped {
			t.Fatalf("second Compact = %v, %v; want a skip", skipped, err)
		}

		runExchange(t, a, "the fresh question")

		main := up.mains()[0]
		if got := wireFenceCount(main, compactFenceFor("rebrief")); got != 1 ||
			!strings.Contains(lastUserContent(t, main), rebriefText+" for manual\n") {
			t.Errorf("opening = %q, want the ran fold's re-brief once", lastUserContent(t, main))
		}
	})
}

// TestCompactAdvise_AResumeThatFoldsAtItsFirstOpeningKeepsTheSessionStartRuns — a resumed session's
// first opening folds the restored history; the fold replaces only compaction runs, so that opening
// carries both the session-start brief and the re-brief.
func TestCompactAdvise_AResumeThatFoldsAtItsFirstOpeningKeepsTheSessionStartRuns(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	up := compactSpyResponder(t, "FOLDED-SUMMARY")
	a := sessionAdviseAgent(t, autoCompactConfig(&recordingSink{}), up,
		userAdvise("brief", sessionStarted, "/bin/sh", "-c", `printf 'session brief\n'`),
		rebriefEntry("rebrief", compactionFinished))
	seedLargeConv(a)
	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := a.RestoreSession(snap); err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}

	runExchange(t, a, "after the resume")

	if up.summaryCalls() != 1 {
		t.Fatalf("summarizer calls = %d, want the first opening's fold", up.summaryCalls())
	}
	opening := lastUserContent(t, up.mains()[0])
	if !strings.HasPrefix(opening, userFenceFor("brief")) || !strings.Contains(opening, compactFenceFor("rebrief")) ||
		!strings.HasSuffix(opening, "\n\nafter the resume") {
		t.Errorf("opening = %q, want the session brief and the re-brief ahead of the prompt", opening)
	}
}

// TestCompactAdvise_AClearAfterAFoldAttachesTheFreshSpanUnstripped — a Session boundary discards the
// fold's runs: the cleared session's first prompt carries the entry's session-start brief alone.
func TestCompactAdvise_AClearAfterAFoldAttachesTheFreshSpanUnstripped(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	both := []domain.Moment{domain.MomentSessionStarted, domain.MomentCompactionFinished}
	up := compactSpyResponder(t, "FOLDED-SUMMARY")
	a := sessionAdviseAgent(t, autoCompactConfig(&recordingSink{}), up, rebriefEntry("rebrief", both))
	seedLargeConv(a)
	if skipped, err := a.Compact(context.Background()); err != nil || skipped {
		t.Fatalf("Compact = %v, %v; want a fold that ran", skipped, err)
	}
	if err := a.ClearContext(); err != nil {
		t.Fatalf("ClearContext: %v", err)
	}

	runExchange(t, a, "after the clear")

	main := up.mains()[0]
	opening := lastUserContent(t, main)
	if !strings.HasPrefix(opening, userFenceFor("rebrief")) || !strings.HasSuffix(opening, "\n\nafter the clear") {
		t.Errorf("opening = %q, want the session-start brief ahead of the prompt", opening)
	}
	if got := wireFenceCount(main, compactFenceFor("rebrief")); got != 0 {
		t.Errorf("request carries %d re-brief fences, want none: the clear discarded the fold's runs", got)
	}
}

// TestCompactAdvise_OneEntryOnBothMomentsLandsOneFence — an entry on session-started and
// compaction-finished joined at one opening lands one fence: the re-brief retires the brief the same
// join just fenced.
func TestCompactAdvise_OneEntryOnBothMomentsLandsOneFence(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	both := []domain.Moment{domain.MomentSessionStarted, domain.MomentCompactionFinished}
	up := compactSpyResponder(t, "FOLDED-SUMMARY")
	a := sessionAdviseAgent(t, autoCompactConfig(&recordingSink{}), up, rebriefEntry("rebrief", both))
	seedLargeConv(a)

	runExchange(t, a, "the fresh question")

	main := up.mains()[0]
	if got := wireFenceCount(main, domain.AdviceFencePrefix+"rebrief "); got != 1 {
		t.Errorf("request carries %d of the entry's fences, want 1", got)
	}
	if got := lastUserContent(t, main); !strings.HasPrefix(got, compactFenceFor("rebrief")) {
		t.Errorf("opening = %q, want the re-brief's fence", got)
	}
}
