package domain

// White-box tests for the Reaction core's vocabulary and its one gate. They are
// package-internal because the Handler seal is unexported: only a test inside domain can
// prove that the five func types are the whole of it.

import (
	"context"
	"errors"
	"testing"
)

// noopPreRequest is a handler that does nothing, used wherever a table row needs a valid
// pre-request handler and the handler's body is beside the point.
var noopPreRequest = PreRequestFunc(func(context.Context, *Request) (Outcome, error) {
	return Outcome{}, nil
})

// TestMomentValuesArePinnedLiterals pins all ten Moment spellings. The strings are the ones a
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
		{MomentApprovalWaiting, "approval-waiting"},
		{MomentError, "error"},
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
		MomentApprovalWaiting,
		MomentError,
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
		{MomentApprovalWaiting, false},
		{MomentError, false},
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
