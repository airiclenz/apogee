package agent

import (
	"errors"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/workflow"
)

// The expectations below are literal: each word was read, when childEnding was written, off the
// classifiers it replaces — classifyDelegation and delegationSummary (subagent.go), subAgentEnding,
// workflowOutcome and undeliveredWorkflowReason (workflowspawn.go) and undeliveredReason
// (children.go) — and none of those is called here, so retiring them leaves this test standing.

// The model-facing contents an ending carries or is classified from, as the parent reads them.
const (
	childEndingSkippedContent   = "sub-agent not started: the user sent a message while this group was running; delegate again if the task is still needed"
	childEndingCancelledContent = "sub-agent not started: the user cancelled the turn before it started; delegate again if the task is still needed"
	childEndingStoppedContent   = "sub-agent not started: the user stopped it before it started; delegate again if the task is still needed"
	childEndingCeilingContent   = "sub-agent not started: this reply fanned out 7 delegations and the ceiling is 6 (2 rounds × width 3) — the first 6 ran; delegate the rest again once their results are in — for more items, use fan_out"
	childEndingCeilingCause     = "sub-agent not started: this reply fanned out 7 delegations and the ceiling is 6 (2 rounds × width 3) — the first 6 ran; delegate the rest again once their resul…"
	childEndingSteered          = 2
)

// TestChildEnding_ProjectsTodaysWords pins every projection of every reachable ending to the words
// the engine spells it in today. The one difference from today is the ratified never-ran row set
// (owner, 2026-11-21): a child that never ran reads UndeliveredRefused on both paths, where the
// workflow spawner's never-ran child read UndeliveredFaulted.
func TestChildEnding_ProjectsTodaysWords(t *testing.T) {
	t.Parallel()

	complete := domain.StepResult{Status: domain.StatusExchangeComplete}
	tests := []struct {
		name          string
		ending        childEnding
		resultContent string
		report        string
		wantOutcome   delegationOutcome
		wantCause     string
		wantEnding    workflow.Ending
		wantReason    domain.UndeliveredReason
		wantSummary   domain.DelegationOutcome
		wantContent   string
	}{
		{
			name:          "a completed run with a report",
			ending:        runEnding(complete, nil, false).withReport(domain.ToolResult{Content: "Found two call sites."}),
			resultContent: "Found two call sites.",
			report:        "Found two call sites.",
			wantOutcome:   delegationCompleted,
			wantEnding:    workflow.EndCompleted,
			wantReason:    domain.UndeliveredCompleted,
			wantSummary:   domain.DelegationOutcome{SteeredMessages: childEndingSteered},
		},
		{
			name:          "a completed run with no report",
			ending:        runEnding(complete, nil, false).withReport(domain.ToolResult{Content: "[delegate returned no report]"}),
			resultContent: "[delegate returned no report]",
			report:        "[delegate returned no report]",
			wantOutcome:   delegationCompleted,
			wantEnding:    workflow.EndCompleted,
			wantReason:    domain.UndeliveredCompleted,
			wantSummary:   domain.DelegationOutcome{SteeredMessages: childEndingSteered, HasNoReport: true},
		},
		{
			name:          "a completed run whose reply the engine would not hand over",
			ending:        runEnding(complete, nil, false).withReport(domain.ToolResult{IsError: true, Content: "the delegate wrote no output file\nmore"}),
			resultContent: "the delegate wrote no output file\nmore",
			wantOutcome:   delegationFaulted,
			wantCause:     "the delegate wrote no output file",
			wantEnding:    workflow.EndFaulted,
			wantReason:    domain.UndeliveredFaulted,
			wantSummary:   domain.DelegationOutcome{SteeredMessages: childEndingSteered},
		},
		{
			name:          "a run whose result rendering panicked",
			ending:        runEnding(domain.StepResult{}, nil, false).withReport(domain.ToolResult{IsError: true, Content: `tool "sub_agent" panicked`}),
			resultContent: `tool "sub_agent" panicked`,
			wantOutcome:   delegationFaulted,
			wantCause:     `tool "sub_agent" panicked`,
			wantEnding:    workflow.EndFaulted,
			wantReason:    domain.UndeliveredFaulted,
			wantSummary:   domain.DelegationOutcome{SteeredMessages: childEndingSteered},
		},
		{
			name:          "a loop-level Run error",
			ending:        runEnding(domain.StepResult{}, errors.New("boom"), false).withReport(domain.ToolResult{IsError: true, Content: "sub-agent failed: boom"}),
			resultContent: "sub-agent failed: boom",
			wantOutcome:   delegationFaulted,
			wantCause:     "sub-agent failed: boom",
			wantEnding:    workflow.EndFaulted,
			wantReason:    domain.UndeliveredFaulted,
			wantSummary:   domain.DelegationOutcome{SteeredMessages: childEndingSteered},
		},
		{
			name:          "an abandoned Exchange",
			ending:        runEnding(domain.StepResult{Status: domain.StatusExchangeComplete, Faulted: true}, nil, false).withReport(domain.ToolResult{IsError: true, Content: "sub-agent faulted before finishing the delegated task: upstream refused"}),
			resultContent: "sub-agent faulted before finishing the delegated task: upstream refused",
			wantOutcome:   delegationFaulted,
			wantCause:     "sub-agent faulted before finishing the delegated task: upstream refused",
			wantEnding:    workflow.EndFaulted,
			wantReason:    domain.UndeliveredFaulted,
			wantSummary:   domain.DelegationOutcome{SteeredMessages: childEndingSteered},
		},
		{
			name:          "a run the engine stopped at a bound",
			ending:        runEnding(domain.StepResult{Status: domain.StatusExchangeComplete, StepCapped: true}, nil, false).withReport(domain.ToolResult{Content: "[delegate stopped at its token budget (900 tokens); partial result]"}),
			resultContent: "[delegate stopped at its token budget (900 tokens); partial result]",
			wantOutcome:   delegationCapped,
			wantEnding:    workflow.EndCapped,
			wantReason:    domain.UndeliveredCapped,
			wantSummary:   domain.DelegationOutcome{SteeredMessages: childEndingSteered, Bound: domain.DelegationTokenBudget},
		},
		{
			name:          "a run the human stopped",
			ending:        runEnding(domain.StepResult{Status: domain.StatusCancelled}, nil, true).withReport(domain.ToolResult{Content: "[stopped by the user — engine summary follows]"}),
			resultContent: "[stopped by the user — engine summary follows]",
			wantOutcome:   delegationStopped,
			wantEnding:    workflow.EndStopped,
			wantReason:    domain.UndeliveredStopped,
			wantSummary:   domain.DelegationOutcome{SteeredMessages: childEndingSteered, IsStoppedByUser: true},
		},
		{
			name:          "a stopped run whose fold panicked",
			ending:        runEnding(domain.StepResult{Status: domain.StatusCancelled}, nil, true).withReport(domain.ToolResult{IsError: true, Content: `tool "sub_agent" panicked`}),
			resultContent: `tool "sub_agent" panicked`,
			wantOutcome:   delegationStopped,
			wantEnding:    workflow.EndStopped,
			wantReason:    domain.UndeliveredStopped,
			wantSummary:   domain.DelegationOutcome{SteeredMessages: childEndingSteered, IsStoppedByUser: true},
		},
		{
			name:          "a capped run whose rendering or retention panicked",
			ending:        delegateEnding(domain.ToolResult{IsError: true, Content: `tool "sub_agent" panicked`}, dispatchDone, true, false, domain.StepResult{Status: domain.StatusExchangeComplete, StepCapped: true}),
			resultContent: `tool "sub_agent" panicked`,
			wantOutcome:   delegationFaulted,
			wantCause:     `tool "sub_agent" panicked`,
			wantEnding:    workflow.EndFaulted,
			wantReason:    domain.UndeliveredFaulted,
			wantSummary:   domain.DelegationOutcome{SteeredMessages: childEndingSteered},
		},
		{
			name:        "a run cancelled with neither stop nor Turn cancel behind it",
			ending:      runEnding(domain.StepResult{Status: domain.StatusCancelled}, nil, false),
			wantOutcome: delegationCancelled,
			wantEnding:  workflow.EndStopped,
			wantReason:  domain.UndeliveredCancelled,
			wantSummary: domain.DelegationOutcome{SteeredMessages: childEndingSteered},
		},
		{
			name:          "a refused call",
			ending:        refusedEnding(),
			resultContent: "sub_agent requires a non-empty task",
			wantOutcome:   delegationRefused,
			wantCause:     "sub_agent requires a non-empty task",
			wantEnding:    workflow.EndFaulted,
			wantReason:    domain.UndeliveredRefused,
		},
		{
			name:          "a delegation a queued message pre-empted",
			ending:        unstartedSkipped(),
			resultContent: childEndingSkippedContent,
			wantOutcome:   delegationSkipped,
			wantEnding:    workflow.EndFaulted,
			wantReason:    domain.UndeliveredRefused,
			wantContent:   childEndingSkippedContent,
		},
		{
			name:          "a delegation refused past the fan-out ceiling",
			ending:        unstartedPastCeiling(fanOutCeilingResult("call-7", 7, 2, 3, true)),
			resultContent: childEndingCeilingContent,
			wantOutcome:   delegationRefused,
			wantCause:     childEndingCeilingCause,
			wantEnding:    workflow.EndFaulted,
			wantReason:    domain.UndeliveredRefused,
			wantContent:   childEndingCeilingContent,
		},
		{
			name:          "a delegation the Turn's cancel reached before it started",
			ending:        unstartedCancelled(),
			resultContent: childEndingCancelledContent,
			wantOutcome:   delegationCancelled,
			wantEnding:    workflow.EndStopped,
			wantReason:    domain.UndeliveredRefused,
			wantContent:   childEndingCancelledContent,
		},
		{
			name:          "a queued delegation the human stopped",
			ending:        unstartedStopped(),
			resultContent: childEndingStoppedContent,
			wantOutcome:   delegationStopped,
			wantEnding:    workflow.EndStopped,
			wantReason:    domain.UndeliveredRefused,
			wantContent:   childEndingStoppedContent,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			row := delegationRecord{spawnIndex: 3, callID: "call-7", name: "survey", outputPath: "/work/out.md"}
			wantRow := delegationRecord{
				spawnIndex: 3,
				callID:     "call-7",
				name:       "survey",
				outcome:    tt.wantOutcome,
				cause:      tt.wantCause,
				outputPath: "/work/out.md",
			}
			if got := tt.ending.ledgerRecord(row, tt.resultContent); got != wantRow {
				t.Errorf("ledgerRecord = %+v, want %+v", got, wantRow)
			}
			if got := tt.ending.workflowEnding(); got != tt.wantEnding {
				t.Errorf("workflowEnding = %q, want %q", got, tt.wantEnding)
			}
			if got := tt.ending.undelivered(); got != tt.wantReason {
				t.Errorf("undelivered = %q, want %q", got, tt.wantReason)
			}
			if got := tt.ending.summary(childEndingSteered, domain.DelegationTokenBudget, tt.report); got != tt.wantSummary {
				t.Errorf("summary = %+v, want %+v", got, tt.wantSummary)
			}
			if tt.ending.content != tt.wantContent {
				t.Errorf("content = %q, want %q", tt.ending.content, tt.wantContent)
			}
		})
	}
}

// TestRunEnding_OneOrderForBothPaths pins the one run switch over every input a finished child's
// Run can reach, on both paths it serves: the ledger word the sub_agent path books and the
// workflow Ending and undelivered reason the workflow spawner reads. Faulted with StepCapped, and
// Cancelled with either, are left out: finishAtStepCap clears Faulted on the run it caps, and a
// cancelled run is never capped, so the two paths' former orders agree on every reachable input.
func TestRunEnding_OneOrderForBothPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		res         domain.StepResult
		err         error
		stopped     bool
		wantOutcome delegationOutcome
		wantEnding  workflow.Ending
		wantReason  domain.UndeliveredReason
	}{
		{"completed", domain.StepResult{Status: domain.StatusExchangeComplete}, nil, false, delegationCompleted, workflow.EndCompleted, domain.UndeliveredCompleted},
		{"capped", domain.StepResult{Status: domain.StatusExchangeComplete, StepCapped: true}, nil, false, delegationCapped, workflow.EndCapped, domain.UndeliveredCapped},
		{"faulted", domain.StepResult{Status: domain.StatusExchangeComplete, Faulted: true}, nil, false, delegationFaulted, workflow.EndFaulted, domain.UndeliveredFaulted},
		{"cancelled", domain.StepResult{Status: domain.StatusCancelled}, nil, false, delegationCancelled, workflow.EndStopped, domain.UndeliveredCancelled},
		{"Run error", domain.StepResult{}, errors.New("boom"), false, delegationFaulted, workflow.EndFaulted, domain.UndeliveredFaulted},
		{"stopped while cancelled", domain.StepResult{Status: domain.StatusCancelled}, nil, true, delegationStopped, workflow.EndStopped, domain.UndeliveredStopped},
		{"stopped with its fault fold cancelled", domain.StepResult{Status: domain.StatusExchangeComplete, Faulted: true}, nil, true, delegationStopped, workflow.EndStopped, domain.UndeliveredStopped},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ending := runEnding(tt.res, tt.err, tt.stopped)
			if !ending.hasStarted {
				t.Error("runEnding: hasStarted = false, want true")
			}
			if ending.kind != tt.wantOutcome {
				t.Errorf("kind = %q, want %q", ending.kind, tt.wantOutcome)
			}
			if got := ending.workflowEnding(); got != tt.wantEnding {
				t.Errorf("workflowEnding = %q, want %q", got, tt.wantEnding)
			}
			if got := ending.undelivered(); got != tt.wantReason {
				t.Errorf("undelivered = %q, want %q", got, tt.wantReason)
			}
		})
	}
}
