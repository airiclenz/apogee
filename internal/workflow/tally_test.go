package workflow

import "testing"

func TestTally_OfResultStatusAndLine(t *testing.T) {
	t.Parallel()

	okReceipt := &Receipt{Status: StatusOK, Summary: "fine"}
	partialReceipt := &Receipt{Status: StatusPartial, Summary: "half"}
	blockedReceipt := &Receipt{Status: StatusBlocked, Summary: "stuck"}

	t.Run("TallyOf", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name   string
			result Result
			want   Tally
		}{
			{
				name: "verify, merge and a skipped fanout are not counted",
				result: Result{Stages: []StageResult{
					{Kind: StageFanout, Phase: PhaseDone, Tally: Tally{OK: 2, Blocked: 1}},
					{Kind: StageVerify, Phase: PhaseDone, Tally: Tally{OK: 3}},
					{Kind: StageMerge, Phase: PhaseDone, Tally: Tally{OK: 1}},
					{Kind: StageFanout, Phase: PhaseSkipped, Tally: Tally{OK: 5}},
				}},
				want: Tally{OK: 2, Blocked: 1},
			},
			{
				name: "unfinished, resumed and verdicts carry through across fanouts",
				result: Result{Phase: PhaseStopped, Stages: []StageResult{
					{Kind: StageFanout, Phase: PhaseStopped, Tally: Tally{OK: 1, Unfinished: 2, Resumed: 1}},
					{Kind: StageFanout, Phase: PhaseDone, Tally: Tally{Partial: 1, Confirmed: 1, Refuted: 1}},
				}},
				want: Tally{OK: 1, Partial: 1, Unfinished: 2, Resumed: 1, Confirmed: 1, Refuted: 1},
			},
			{
				name: "a repeated fanout counts its latest round, which replaced the earlier one",
				result: Result{Stages: []StageResult{
					{Kind: StageFanout, Phase: PhaseDone, Round: 2, Tally: Tally{OK: 3}},
					{Kind: StageRepeat, Phase: PhaseDone, Note: "re-ran a in 2 of at most 3 rounds"},
				}},
				want: Tally{OK: 3},
			},
			{
				name: "a stage's Tally is summed, never its Items recounted",
				result: Result{Stages: []StageResult{
					{Kind: StageFanout, Phase: PhaseDone, Tally: Tally{OK: 1, Partial: 2}},
				}},
				want: Tally{OK: 1, Partial: 2},
			},
		}
		for _, testCase := range cases {
			t.Run(testCase.name, func(t *testing.T) {
				t.Parallel()

				got := TallyOf(testCase.result)

				if got != testCase.want {
					t.Errorf("TallyOf = %+v, want %+v", got, testCase.want)
				}
			})
		}
	})

	t.Run("TallyOfStatus", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name   string
			status RunStatus
			want   Tally
		}{
			{
				name: "fanout items by receipt, unfinished until done with one",
				status: RunStatus{Stages: []StageStatus{{Kind: StageFanout, Phase: PhaseRunning, Items: []ItemStatus{
					{Phase: PhaseDone, Receipt: okReceipt},
					{Phase: PhaseDone, Receipt: partialReceipt},
					{Phase: PhaseDone, Receipt: blockedReceipt},
					{Phase: PhaseRunning},
					{Phase: PhasePending},
					{Phase: PhaseDone},
				}}}},
				want: Tally{OK: 1, Partial: 1, Blocked: 1, Unfinished: 3},
			},
			{
				name: "a verify line and a skipped fanout are not counted",
				status: RunStatus{Stages: []StageStatus{
					{Kind: StageFanout, Phase: PhaseDone, Items: []ItemStatus{{Phase: PhaseDone, Receipt: okReceipt}}},
					{Kind: StageVerify, Phase: PhaseDone, Items: []ItemStatus{{Phase: PhaseDone, Receipt: blockedReceipt}}},
					{Kind: StageScript, Phase: PhaseDone, Items: []ItemStatus{{Phase: PhaseDone, Receipt: okReceipt}}},
					{Kind: StageFanout, Phase: PhaseSkipped, Items: []ItemStatus{{Phase: PhasePending}}},
				}},
				want: Tally{OK: 1},
			},
		}
		for _, testCase := range cases {
			t.Run(testCase.name, func(t *testing.T) {
				t.Parallel()

				got := TallyOfStatus(testCase.status)

				if got != testCase.want {
					t.Errorf("TallyOfStatus = %+v, want %+v", got, testCase.want)
				}
			})
		}
	})

	t.Run("Line", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name  string
			tally Tally
			want  string
		}{
			{
				name:  "neither unfinished nor verdicts",
				tally: Tally{OK: 2, Partial: 1, Blocked: 1, Resumed: 2},
				want:  "items 4 · ok 2 · partial 1 · blocked 1",
			},
			{
				name:  "unfinished only",
				tally: Tally{OK: 1, Unfinished: 2},
				want:  "items 3 · ok 1 · partial 0 · blocked 0 · unfinished 2",
			},
			{
				name:  "verdicts only",
				tally: Tally{OK: 2, Unclear: 1},
				want:  "items 2 · ok 2 · partial 0 · blocked 0 · confirmed 0 · refuted 0 · unclear 1",
			},
			{
				name:  "unfinished before verdicts, resumed never",
				tally: Tally{OK: 1, Blocked: 1, Unfinished: 1, Resumed: 1, Confirmed: 1, Refuted: 1},
				want:  "items 3 · ok 1 · partial 0 · blocked 1 · unfinished 1 · confirmed 1 · refuted 1 · unclear 0",
			},
			{
				name: "empty",
				want: "items 0 · ok 0 · partial 0 · blocked 0",
			},
		}
		for _, testCase := range cases {
			t.Run(testCase.name, func(t *testing.T) {
				t.Parallel()

				got := testCase.tally.Line()

				if got != testCase.want {
					t.Errorf("Line = %q, want %q", got, testCase.want)
				}
				if total := testCase.tally.Total(); total != testCase.tally.OK+testCase.tally.Partial+testCase.tally.Blocked+testCase.tally.Unfinished {
					t.Errorf("Total = %d, want the sum of ok, partial, blocked and unfinished", total)
				}
			})
		}
	})
}

func TestStateOf(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		info Info
		want State
	}{
		{
			name: "queued, the manager marking it background too",
			info: Info{Queued: true, Background: true, Status: RunStatus{Phase: PhasePending}},
			want: State{Kind: StateQueued},
		},
		{
			name: "running in the background",
			info: Info{Background: true, Status: RunStatus{Phase: PhaseRunning}},
			want: State{Kind: StateBackground},
		},
		{
			name: "a recorded phase",
			info: Info{Status: RunStatus{Phase: PhaseDone}},
			want: State{Kind: StateRecorded, Phase: PhaseDone},
		},
		{
			name: "a folder no manager holds, still recorded as running",
			info: Info{Status: RunStatus{Phase: PhaseRunning}},
			want: State{Kind: StateRecorded, Phase: PhaseRunning},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := StateOf(testCase.info)

			if got != testCase.want {
				t.Errorf("StateOf = %+v, want %+v", got, testCase.want)
			}
		})
	}
}

func TestItemText_Pieces(t *testing.T) {
	t.Parallel()

	t.Run("ItemStatusWord", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name    string
			phase   Phase
			receipt *Receipt
			want    string
		}{
			{name: "done with a receipt", phase: PhaseDone, receipt: &Receipt{Status: StatusPartial}, want: "partial"},
			{name: "stopped", phase: PhaseStopped, want: "stopped"},
			{name: "stopped with a receipt from an earlier round", phase: PhaseStopped, receipt: &Receipt{Status: StatusOK}, want: "stopped"},
			{name: "pending", phase: PhasePending, want: "pending"},
		}
		for _, testCase := range cases {
			t.Run(testCase.name, func(t *testing.T) {
				t.Parallel()

				got := ItemStatusWord(testCase.phase, testCase.receipt)

				if got != testCase.want {
					t.Errorf("ItemStatusWord = %q, want %q", got, testCase.want)
				}
			})
		}
	})

	t.Run("FirstLine", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name string
			text string
			want string
		}{
			{name: "a leading newline", text: "\nfirst\nsecond", want: "first"},
			{name: "space padded", text: "   padded summary  \n more", want: "padded summary"},
		}
		for _, testCase := range cases {
			t.Run(testCase.name, func(t *testing.T) {
				t.Parallel()

				got := FirstLine(testCase.text)

				if got != testCase.want {
					t.Errorf("FirstLine(%q) = %q, want %q", testCase.text, got, testCase.want)
				}
			})
		}
	})

	t.Run("FieldValue", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name  string
			value any
			want  string
		}{
			{name: "plain text", value: "plain", want: "plain"},
			{name: "a space", value: "two words", want: `"two words"`},
			{name: "an equals sign", value: "a=b", want: `"a=b"`},
			{name: "a newline", value: "one\ntwo", want: `"one\ntwo"`},
			{name: "a carriage return", value: "one\rtwo", want: `"one\rtwo"`},
			{name: "a no-break space", value: "one\u00a0two", want: `"one\u00a0two"`},
			{name: "empty text", value: "", want: `""`},
			{name: "a list", value: []any{"a", 2.0}, want: "a,2"},
			{name: "a float", value: 3.5, want: "3.5"},
		}
		for _, testCase := range cases {
			t.Run(testCase.name, func(t *testing.T) {
				t.Parallel()

				got := FieldValue(testCase.value)

				if got != testCase.want {
					t.Errorf("FieldValue(%#v) = %s, want %s", testCase.value, got, testCase.want)
				}
			})
		}
	})
}
