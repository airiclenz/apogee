package domain

// White-box tests for the Reaction core's vocabulary and its one gate. They are
// package-internal because the Handler seal is unexported: only a test inside domain can
// prove that the five func types are the whole of it.

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// noopPreRequest is a handler that does nothing, used wherever a table row needs a valid
// pre-request handler and the handler's body is beside the point.
var noopPreRequest = PreRequestFunc(func(context.Context, *Request) (Outcome, error) {
	return Outcome{}, nil
})

// TestMomentValuesArePinnedLiterals pins all sixteen Moment spellings. The strings are the ones a
// user writes in configuration and an observer reads off a ReactionFiredEvent, so changing one
// breaks every configuration in the wild — the seams carry the retired hook-point values and the
// notices the reactions.Event values, unchanged.
func TestMomentValuesArePinnedLiterals(t *testing.T) {
	t.Parallel()

	cases := []struct {
		moment Moment
		want   string
	}{
		{MomentPreRequest, "pre-request"},
		{MomentPostResponse, "post-response"},
		{MomentPreToolExec, "pre-tool-exec"},
		{MomentPostToolResult, "post-tool-result"},
		{MomentHistoryRewrite, "history-rewrite"},
		{MomentExchangeFinished, "exchange-finished"},
		{MomentTurnFinished, "turn-finished"},
		{MomentFileChanged, "file-changed"},
		{MomentApprovalRequested, "approval-requested"},
		{MomentApprovalDecided, "approval-decided"},
		{MomentError, "error"},
		{MomentPreRequestFinished, "pre-request-finished"},
		{MomentPostResponseFinished, "post-response-finished"},
		{MomentPreToolExecFinished, "pre-tool-exec-finished"},
		{MomentPostToolResultFinished, "post-tool-result-finished"},
		{MomentHistoryRewriteFinished, "history-rewrite-finished"},
	}

	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			t.Parallel()

			if string(c.moment) != c.want {
				t.Errorf("Moment = %q, want %q", string(c.moment), c.want)
			}
		})
	}
}

// TestSeamsAndNoticesReportTheVocabularyAsACopy checks both listings' contents and order, and
// that a caller mutating a returned slice cannot disturb the vocabulary behind it.
func TestSeamsAndNoticesReportTheVocabularyAsACopy(t *testing.T) {
	t.Parallel()

	wantSeams := []Moment{
		MomentPreRequest,
		MomentPostResponse,
		MomentPreToolExec,
		MomentPostToolResult,
		MomentHistoryRewrite,
	}
	wantNotices := []Moment{
		MomentExchangeFinished,
		MomentTurnFinished,
		MomentFileChanged,
		MomentApprovalRequested,
		MomentApprovalDecided,
		MomentError,
		MomentPreRequestFinished,
		MomentPostResponseFinished,
		MomentPreToolExecFinished,
		MomentPostToolResultFinished,
		MomentHistoryRewriteFinished,
	}

	seams := Seams()
	notices := Notices()

	assertMoments(t, "Seams", seams, wantSeams)
	assertMoments(t, "Notices", notices, wantNotices)

	seams[0] = "clobbered"
	notices[0] = "clobbered"

	assertMoments(t, "Seams after a caller mutated its copy", Seams(), wantSeams)
	assertMoments(t, "Notices after a caller mutated its copy", Notices(), wantNotices)
}

// assertMoments fails t when got does not equal want, element for element.
func assertMoments(t *testing.T, what string, got, want []Moment) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s = %v, want %v", what, got, want)
		}
	}
}

// TestMomentIsSeam separates the in-loop half of the vocabulary from the post-hoc half, per
// const, and refuses a spelling outside it.
func TestMomentIsSeam(t *testing.T) {
	t.Parallel()

	cases := []struct {
		moment Moment
		want   bool
	}{
		{MomentPreRequest, true},
		{MomentPostResponse, true},
		{MomentPreToolExec, true},
		{MomentPostToolResult, true},
		{MomentHistoryRewrite, true},
		{MomentExchangeFinished, false},
		{MomentTurnFinished, false},
		{MomentFileChanged, false},
		{MomentApprovalRequested, false},
		{MomentApprovalDecided, false},
		{MomentError, false},
		{MomentPreRequestFinished, false},
		{MomentPostResponseFinished, false},
		{MomentPreToolExecFinished, false},
		{MomentPostToolResultFinished, false},
		{MomentHistoryRewriteFinished, false},
		{Moment("not-a-moment"), false},
		{Moment(""), false},
	}

	for _, c := range cases {
		t.Run(string(c.moment), func(t *testing.T) {
			t.Parallel()

			if got := c.moment.IsSeam(); got != c.want {
				t.Errorf("Moment(%q).IsSeam() = %v, want %v", string(c.moment), got, c.want)
			}
		})
	}
}

// TestMomentIsNotice separates the post-hoc half of the vocabulary from the in-loop half. It is
// deliberately not the negation of IsSeam: a spelling outside the vocabulary is neither, which is
// the case a caller validating a configured Moment has to see.
func TestMomentIsNotice(t *testing.T) {
	t.Parallel()

	for _, m := range Notices() {
		t.Run(string(m), func(t *testing.T) {
			t.Parallel()

			if !m.IsNotice() {
				t.Errorf("Moment(%q).IsNotice() = false, want true", string(m))
			}
		})
	}

	for _, m := range append(Seams(), "not-a-moment", "") {
		t.Run("not a notice: "+string(m), func(t *testing.T) {
			t.Parallel()

			if m.IsNotice() {
				t.Errorf("Moment(%q).IsNotice() = true, want false", string(m))
			}
		})
	}
}

// TestClosingMapsEverySeamToItsNotice walks the whole vocabulary: each of the five seams answers
// its own `<seam>-finished` notice, that answer is itself a notice and never a seam, and every
// notice — plus a spelling outside the vocabulary — answers the zero Moment.
func TestClosingMapsEverySeamToItsNotice(t *testing.T) {
	t.Parallel()

	wantClosings := map[Moment]Moment{
		MomentPreRequest:     MomentPreRequestFinished,
		MomentPostResponse:   MomentPostResponseFinished,
		MomentPreToolExec:    MomentPreToolExecFinished,
		MomentPostToolResult: MomentPostToolResultFinished,
		MomentHistoryRewrite: MomentHistoryRewriteFinished,
	}

	for _, seam := range Seams() {
		t.Run(string(seam), func(t *testing.T) {
			t.Parallel()

			got := seam.Closing()

			if got != wantClosings[seam] {
				t.Fatalf("Moment(%q).Closing() = %q, want %q", string(seam), string(got), string(wantClosings[seam]))
			}
			if !got.IsNotice() || got.IsSeam() {
				t.Errorf("Moment(%q).Closing() = %q, which is not a notice — a closing reports a pass, it is not one", string(seam), string(got))
			}
		})
	}

	for _, m := range append(Notices(), "not-a-moment", "") {
		t.Run("no closing: "+string(m), func(t *testing.T) {
			t.Parallel()

			if got := m.Closing(); got != "" {
				t.Errorf("Moment(%q).Closing() = %q, want the zero Moment — only a seam closes", string(m), string(got))
			}
		})
	}
}

// TestReactionValidateAppliesTheReactionSurfaceMatrix walks every origin × class cell: the
// engine takes all five classes, the user takes observe, advise and gate, and an origin or class
// outside the vocabulary occupies no cell at all (ADR 0076 D2).
func TestReactionValidateAppliesTheReactionSurfaceMatrix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		origin  Origin
		class   Class
		wantOK  bool
		subtest string
	}{
		{OriginEngine, ClassObserve, true, "engine/observe"},
		{OriginEngine, ClassAdvise, true, "engine/advise"},
		{OriginEngine, ClassGate, true, "engine/gate"},
		{OriginEngine, ClassShapeView, true, "engine/shape-view"},
		{OriginEngine, ClassShapeWork, true, "engine/shape-work"},
		{OriginUser, ClassObserve, true, "user/observe"},
		{OriginUser, ClassAdvise, true, "user/advise"},
		{OriginUser, ClassGate, true, "user/gate"},
		{OriginUser, ClassShapeView, false, "user/shape-view is reserved"},
		{OriginUser, ClassShapeWork, false, "user/shape-work is refused"},
		{Origin("bench"), ClassObserve, false, "an unknown origin has no cell"},
		{OriginEngine, Class("nudge"), false, "an unknown class has no cell"},
	}

	for _, c := range cases {
		t.Run(c.subtest, func(t *testing.T) {
			t.Parallel()

			reaction := Reaction{
				ID:      "probe",
				Origin:  c.origin,
				Class:   c.class,
				On:      []Moment{MomentPreRequest},
				Handler: noopPreRequest,
			}

			err := reaction.Validate()

			switch {
			case c.wantOK && err != nil:
				t.Errorf("Validate() = %v, want nil", err)
			case !c.wantOK && err == nil:
				t.Errorf("Validate() = nil, want an error")
			case !c.wantOK && !errors.Is(err, ErrInvalidReaction):
				t.Errorf("Validate() = %v, want it to wrap ErrInvalidReaction", err)
			}
		})
	}
}

// TestReactionValidateRefusesAMalformedReaction covers the failures that are not about the
// matrix: a reaction nothing can name, one nothing can run, and one whose On list disagrees with
// the seam its handler serves.
func TestReactionValidateRefusesAMalformedReaction(t *testing.T) {
	t.Parallel()

	valid := Reaction{
		ID:      "probe",
		Origin:  OriginEngine,
		Class:   ClassShapeView,
		On:      []Moment{MomentPreRequest},
		Handler: noopPreRequest,
	}

	cases := []struct {
		name   string
		mutate func(*Reaction)
	}{
		{"empty ID", func(r *Reaction) { r.ID = "" }},
		{"empty Origin", func(r *Reaction) { r.Origin = "" }},
		{"empty Class", func(r *Reaction) { r.Class = "" }},
		{"nil Handler", func(r *Reaction) { r.Handler = nil }},
		{"empty On", func(r *Reaction) { r.On = nil }},
		{"duplicated On entry", func(r *Reaction) {
			r.On = []Moment{MomentPreRequest, MomentPreRequest}
		}},
		{"On names a seam the handler does not serve", func(r *Reaction) {
			r.On = []Moment{MomentPostResponse}
		}},
		{"On names a notice Moment", func(r *Reaction) {
			r.On = []Moment{MomentTurnFinished}
		}},
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("the unmutated reaction must be valid, got %v", err)
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			reaction := valid
			c.mutate(&reaction)

			err := reaction.Validate()

			if err == nil {
				t.Fatalf("Validate() = nil, want an error")
			}
			if !errors.Is(err, ErrInvalidReaction) {
				t.Errorf("Validate() = %v, want it to wrap ErrInvalidReaction", err)
			}
		})
	}
}

// TestReactionValidateAcceptsEverySeamHandler proves the seal is complete and that each of the
// five func types declares the seam its On list must name.
func TestReactionValidateAcceptsEverySeamHandler(t *testing.T) {
	t.Parallel()

	cases := []struct {
		moment  Moment
		handler Handler
	}{
		{MomentPreRequest, noopPreRequest},
		{MomentPostResponse, PostResponseFunc(func(context.Context, *Response) (Outcome, error) {
			return Outcome{}, nil
		})},
		{MomentPreToolExec, PreToolExecFunc(func(context.Context, LoopView, *ToolCallEdit) (Outcome, error) {
			return Outcome{}, nil
		})},
		{MomentPostToolResult, PostToolResultFunc(
			func(context.Context, LoopView, ToolCall, *ToolResultEdit) (Outcome, error) {
				return Outcome{}, nil
			},
		)},
		{MomentHistoryRewrite, HistoryRewriteFunc(func(context.Context, *Conversation) (Outcome, error) {
			return Outcome{}, nil
		})},
	}

	for _, c := range cases {
		t.Run(string(c.moment), func(t *testing.T) {
			t.Parallel()

			reaction := Reaction{
				ID:      "probe",
				Origin:  OriginEngine,
				Class:   ClassShapeView,
				On:      []Moment{c.moment},
				Handler: c.handler,
			}

			if err := reaction.Validate(); err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
			if got := c.handler.seam(); got != c.moment {
				t.Errorf("handler seam = %q, want %q", got, c.moment)
			}
		})
	}
}

// TestReactionValidateAppliesThePerClassRulesToAnAsyncHandler walks the rules the argv and
// webhook handlers brought in. They key on the handler KIND, not the class alone: an argv handler
// serves observe on notices, advise at post-tool-result or file-changed and gate at pre-tool-exec,
// while a webhook stays observe-only and a Go handler keeps the per-seam rule for every class —
// which is why the bench's class-observe Go reactions still validate. The refusals are pinned by
// their exact text: they are what a user reads when their `run:` entry names the wrong Moment.
func TestReactionValidateAppliesThePerClassRulesToAnAsyncHandler(t *testing.T) {
	t.Parallel()

	argv := ArgvHandler{Argv: []string{"/usr/bin/notify", "--quiet"}}
	webhook := WebhookHandler{
		URL:        "https://example.test/apogee",
		Headers:    map[string]string{"Content-Type": "application/json"},
		HeadersEnv: map[string]string{"Authorization": "APOGEE_WEBHOOK_TOKEN"},
	}

	cases := []struct {
		name     string
		reaction Reaction
		wantErr  string
	}{
		{
			name: "an argv handler on a notice validates",
			reaction: Reaction{
				ID:      "notify",
				Origin:  OriginUser,
				Class:   ClassObserve,
				On:      []Moment{MomentTurnFinished, MomentPostResponseFinished},
				Handler: argv,
			},
		},
		{
			name: "a webhook handler on a notice validates",
			reaction: Reaction{
				ID:      "notify",
				Origin:  OriginUser,
				Class:   ClassObserve,
				On:      []Moment{MomentFileChanged},
				Handler: webhook,
			},
		},
		{
			name: "a Go handler stays on its seam as class observe",
			reaction: Reaction{
				ID:      "probe",
				Origin:  OriginEngine,
				Class:   ClassObserve,
				On:      []Moment{MomentPreRequest},
				Handler: noopPreRequest,
			},
		},
		{
			name: "an argv handler on a seam is refused",
			reaction: Reaction{
				ID:      "notify",
				Origin:  OriginUser,
				Class:   ClassObserve,
				On:      []Moment{MomentPreRequest},
				Handler: argv,
			},
			wantErr: `apogee: invalid reaction "notify": run: reacts to notices; "pre-request" is a seam`,
		},
		{
			name: "a webhook handler on a seam is refused",
			reaction: Reaction{
				ID:      "notify",
				Origin:  OriginUser,
				Class:   ClassObserve,
				On:      []Moment{MomentTurnFinished, MomentHistoryRewrite},
				Handler: webhook,
			},
			wantErr: `apogee: invalid reaction "notify": run: reacts to notices; "history-rewrite" is a seam`,
		},
		{
			name: "an argv handler on a spelling outside the vocabulary is refused",
			reaction: Reaction{
				ID:      "notify",
				Origin:  OriginUser,
				Class:   ClassObserve,
				On:      []Moment{"turn-done"},
				Handler: argv,
			},
			wantErr: `apogee: invalid reaction "notify": run: reacts to notices; "turn-done" is not one`,
		},
		{
			name: "an argv handler advising at post-tool-result validates",
			reaction: Reaction{
				ID:      "advise",
				Origin:  OriginUser,
				Class:   ClassAdvise,
				On:      []Moment{MomentPostToolResult, MomentFileChanged},
				Handler: argv,
			},
		},
		{
			name: "an argv handler gating at pre-tool-exec validates",
			reaction: Reaction{
				ID:      "gate",
				Origin:  OriginUser,
				Class:   ClassGate,
				On:      []Moment{MomentPreToolExec},
				Handler: argv,
			},
		},
		{
			name: "an argv handler advising on another notice is refused",
			reaction: Reaction{
				ID:      "notify",
				Origin:  OriginUser,
				Class:   ClassAdvise,
				On:      []Moment{MomentTurnFinished},
				Handler: argv,
			},
			wantErr: `apogee: invalid reaction "notify": advise: reacts at post-tool-result or file-changed; "turn-finished" is neither`,
		},
		{
			name: "an argv handler advising on another seam is refused",
			reaction: Reaction{
				ID:      "notify",
				Origin:  OriginUser,
				Class:   ClassAdvise,
				On:      []Moment{MomentPostToolResult, MomentPreRequest},
				Handler: argv,
			},
			wantErr: `apogee: invalid reaction "notify": advise: reacts at post-tool-result or file-changed; "pre-request" is neither`,
		},
		{
			name: "an argv handler gating anywhere else is refused",
			reaction: Reaction{
				ID:      "notify",
				Origin:  OriginUser,
				Class:   ClassGate,
				On:      []Moment{MomentPostToolResult},
				Handler: argv,
			},
			wantErr: `apogee: invalid reaction "notify": gate: reacts at pre-tool-exec; "post-tool-result" is not it`,
		},
		{
			name: "an argv handler gating on a spelling outside the vocabulary is refused",
			reaction: Reaction{
				ID:      "notify",
				Origin:  OriginUser,
				Class:   ClassGate,
				On:      []Moment{"pre-tool"},
				Handler: argv,
			},
			wantErr: `apogee: invalid reaction "notify": gate: reacts at pre-tool-exec; "pre-tool" is not it`,
		},
		{
			name: "a webhook handler outside class observe is refused",
			reaction: Reaction{
				ID:      "notify",
				Origin:  OriginUser,
				Class:   ClassGate,
				On:      []Moment{MomentTurnFinished},
				Handler: webhook,
			},
			wantErr: `apogee: invalid reaction "notify": run: a command or webhook reacts as class "observe", not "gate"`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			err := c.reaction.Validate()

			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want %q", c.wantErr)
			}
			if !errors.Is(err, ErrInvalidReaction) {
				t.Errorf("Validate() = %v, want it to wrap ErrInvalidReaction", err)
			}
			if got := err.Error(); got != c.wantErr {
				t.Errorf("Validate() = %q, want %q", got, c.wantErr)
			}
		})
	}
}

// TestAsyncHandlersNameNoSeam pins the seal's other half: the two async-lane handlers answer the
// ZERO Moment, which is what makes "the handler's seam" a question Validate must not ask of
// them.
func TestAsyncHandlersNameNoSeam(t *testing.T) {
	t.Parallel()

	if got := (ArgvHandler{Argv: []string{"true"}}).seam(); got != "" {
		t.Errorf("ArgvHandler.seam() = %q, want the zero Moment", got)
	}
	if got := (WebhookHandler{URL: "https://example.test"}).seam(); got != "" {
		t.Errorf("WebhookHandler.seam() = %q, want the zero Moment", got)
	}
	if got := noopPreRequest.seam(); got != MomentPreRequest {
		t.Errorf("PreRequestFunc.seam() = %q, want %q", got, MomentPreRequest)
	}
}

// TestGenerationValidateRejectsANonObserveEntry pins the one rule a Generation adds beyond what
// each entry already answers for itself: the observe list is the Runner's lane, so an entry in
// any other class is refused there even though the entry validates on its own.
func TestGenerationValidateRejectsANonObserveEntry(t *testing.T) {
	t.Parallel()

	gen := Generation{
		Floor:  FloorConfig{DisableReadCache: true},
		Bypass: true,
		Observe: []Reaction{{
			ID:      "shaper",
			Origin:  OriginEngine,
			Class:   ClassShapeView,
			On:      []Moment{MomentPreRequest},
			Handler: noopPreRequest,
		}},
	}

	if err := gen.Observe[0].Validate(); err != nil {
		t.Fatalf("the entry must validate on its own, got %v", err)
	}

	err := gen.Validate()

	if err == nil {
		t.Fatalf("Generation.Validate() = nil, want an error")
	}
	if !errors.Is(err, ErrInvalidReaction) {
		t.Errorf("Generation.Validate() = %v, want it to wrap ErrInvalidReaction", err)
	}
	want := `apogee: invalid reaction "shaper": the observe list takes class "observe", not "shape-view"`
	if got := err.Error(); got != want {
		t.Errorf("Generation.Validate() = %q, want %q", got, want)
	}
}

// TestGenerationValidateAcceptsAnObserveListAndRefusesTheRest covers the generation's remaining
// answers: an empty generation is well formed — a Driver with no user entries applies one every
// time — a list of distinct observe entries passes, and both a malformed entry and a repeated ID
// are refused, since the ID is what a firing is reported under.
func TestGenerationValidateAcceptsAnObserveListAndRefusesTheRest(t *testing.T) {
	t.Parallel()

	entry := func(id string) Reaction {
		return Reaction{
			ID:      id,
			Origin:  OriginUser,
			Class:   ClassObserve,
			On:      []Moment{MomentTurnFinished},
			Handler: ArgvHandler{Argv: []string{"/usr/bin/notify"}},
		}
	}

	cases := []struct {
		name    string
		gen     Generation
		wantErr string
	}{
		{name: "the zero generation", gen: Generation{}},
		{
			name: "two distinct observe entries",
			gen:  Generation{Observe: []Reaction{entry("first"), entry("second")}},
		},
		{
			name:    "a repeated ID",
			gen:     Generation{Observe: []Reaction{entry("notify"), entry("notify")}},
			wantErr: `apogee: invalid reaction "notify": listed twice in the observe list`,
		},
		{
			name: "an entry that does not validate",
			gen: Generation{Observe: []Reaction{{
				ID:      "notify",
				Origin:  OriginUser,
				Class:   ClassObserve,
				On:      []Moment{MomentPreToolExec},
				Handler: ArgvHandler{Argv: []string{"/usr/bin/notify"}},
			}}},
			wantErr: `apogee: invalid reaction "notify": run: reacts to notices; "pre-tool-exec" is a seam`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			err := c.gen.Validate()

			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("Generation.Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Generation.Validate() = nil, want %q", c.wantErr)
			}
			if !errors.Is(err, ErrInvalidReaction) {
				t.Errorf("Generation.Validate() = %v, want it to wrap ErrInvalidReaction", err)
			}
			if got := err.Error(); got != c.wantErr {
				t.Errorf("Generation.Validate() = %q, want %q", got, c.wantErr)
			}
		})
	}
}

// TestGenerationValidateGuardsTheSyncLane pins what the sync lane adds beyond the rules each
// entry already answers for itself: the lane is the user's alone, it takes advise or gate and
// nothing else, and it refuses a repeated ID of its own. The last case is the one a configured
// entry actually produces — the SAME id in both lanes, because one entry resolves to up to one
// reaction per class and they all carry the entry's id.
func TestGenerationValidateGuardsTheSyncLane(t *testing.T) {
	t.Parallel()

	argv := ArgvHandler{Argv: []string{"/usr/bin/react"}}
	entry := func(id string, origin Origin, class Class, on Moment) Reaction {
		return Reaction{ID: id, Origin: origin, Class: class, On: []Moment{on}, Handler: argv}
	}
	observe := func(id string) Reaction {
		return entry(id, OriginUser, ClassObserve, MomentTurnFinished)
	}
	advise := func(id string) Reaction {
		return entry(id, OriginUser, ClassAdvise, MomentPostToolResult)
	}
	gate := func(id string) Reaction {
		return entry(id, OriginUser, ClassGate, MomentPreToolExec)
	}

	cases := []struct {
		name    string
		gen     Generation
		wantErr string
	}{
		{
			name: "an advise and a gate entry",
			gen:  Generation{Sync: []Reaction{advise("first"), gate("second")}},
		},
		{
			name: "one id in both lanes",
			gen: Generation{
				Observe: []Reaction{observe("watch")},
				Sync:    []Reaction{advise("watch"), gate("guard")},
			},
		},
		{
			name:    "an observe entry in the sync lane",
			gen:     Generation{Sync: []Reaction{observe("notify")}},
			wantErr: `apogee: invalid reaction "notify": the sync list takes class "advise" or "gate", not "observe"`,
		},
		{
			name:    "an engine-origin entry in the sync lane",
			gen:     Generation{Sync: []Reaction{entry("builtin", OriginEngine, ClassAdvise, MomentPostToolResult)}},
			wantErr: `apogee: invalid reaction "builtin": the sync list takes origin "user", not "engine"`,
		},
		{
			name:    "a repeated ID within the sync lane",
			gen:     Generation{Sync: []Reaction{advise("advice"), advise("advice")}},
			wantErr: `apogee: invalid reaction: the sync list names "advice" twice`,
		},
		{
			name:    "a sync entry that does not validate",
			gen:     Generation{Sync: []Reaction{entry("notify", OriginUser, ClassGate, MomentTurnFinished)}},
			wantErr: `apogee: invalid reaction "notify": gate: reacts at pre-tool-exec; "turn-finished" is not it`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			err := c.gen.Validate()

			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("Generation.Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Generation.Validate() = nil, want %q", c.wantErr)
			}
			if !errors.Is(err, ErrInvalidReaction) {
				t.Errorf("Generation.Validate() = %v, want it to wrap ErrInvalidReaction", err)
			}
			if got := err.Error(); got != c.wantErr {
				t.Errorf("Generation.Validate() = %q, want %q", got, c.wantErr)
			}
		})
	}
}

// TestSplitLanesDividesByClassAndKeepsOrder pins the divider a Driver uses to turn one resolved
// list into a Generation's two lanes: observe on one side, advise and gate on the other, each in
// the order the list carried them, and a class belonging to neither lane dropped.
func TestSplitLanesDividesByClassAndKeepsOrder(t *testing.T) {
	t.Parallel()

	argv := ArgvHandler{Argv: []string{"/usr/bin/react"}}
	entry := func(id string, class Class) Reaction {
		return Reaction{ID: id, Origin: OriginUser, Class: class, Handler: argv}
	}

	list := []Reaction{
		entry("a", ClassObserve),
		entry("b", ClassGate),
		entry("c", ClassAdvise),
		entry("d", ClassObserve),
		entry("e", ClassShapeView),
		entry("f", ClassGate),
	}

	observe, sync := SplitLanes(list)

	ids := func(list []Reaction) []string {
		out := make([]string, 0, len(list))
		for _, r := range list {
			out = append(out, r.ID)
		}
		return out
	}
	if got, want := ids(observe), []string{"a", "d"}; !slices.Equal(got, want) {
		t.Errorf("observe lane = %v, want %v", got, want)
	}
	if got, want := ids(sync), []string{"b", "c", "f"}; !slices.Equal(got, want) {
		t.Errorf("sync lane = %v, want %v", got, want)
	}

	observe, sync = SplitLanes(nil)

	if observe != nil || sync != nil {
		t.Errorf("SplitLanes(nil) = %v, %v, want both nil", observe, sync)
	}
}

// TestSeamPayloadRevisionsForwardToTheWorkingValue pins the two paired payloads: the dispatcher
// brackets a firing on the payload's Revision(), so each pair must report the revision of the
// working value it wraps rather than one of its own.
func TestSeamPayloadRevisionsForwardToTheWorkingValue(t *testing.T) {
	t.Parallel()

	t.Run("ToolResultMoment forwards to the edit", func(t *testing.T) {
		t.Parallel()

		edit := NewToolResultEdit(&ToolResult{CallID: "c1", Content: "raw"})
		payload := ToolResultMoment{Call: ToolCall{ID: "c1", Tool: "read_file"}, Edit: edit}

		before := payload.Revision()
		edit.SetContent("reshaped")

		if before != edit.Revision()-1 {
			t.Fatalf("payload revision %d did not track the edit's %d", before, edit.Revision())
		}
		if got := payload.Revision(); got != edit.Revision() {
			t.Errorf("ToolResultMoment.Revision() = %d, want the edit's %d", got, edit.Revision())
		}
	})

	t.Run("PostResponseMoment forwards to the response", func(t *testing.T) {
		t.Parallel()

		resp := NewResponse("answer", "", nil, FinishStop, nil)
		payload := PostResponseMoment{Resp: resp, Retryable: true}

		before := payload.Revision()
		resp.SetText("reshaped")

		if before != resp.Revision()-1 {
			t.Fatalf("payload revision %d did not track the response's %d", before, resp.Revision())
		}
		if got := payload.Revision(); got != resp.Revision() {
			t.Errorf("PostResponseMoment.Revision() = %d, want the response's %d", got, resp.Revision())
		}
	})
}
