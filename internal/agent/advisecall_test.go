package agent

// The one advise call (callAdvise): every advise Moment — the three in-loop seams through
// adviseUser, the two session lanes as the briefing's adviseCaller — reaches its handler through it,
// and it answers as a VALUE. These rows pin that value per handler kind and outcome, and that the
// call itself never emits nor reports: a failure is only returned, and landing it is the caller's.

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

func TestAdviseCall_AnswersAsAValue(t *testing.T) {
	// No t.Parallel: t.Setenv.
	t.Setenv("APOGEE_TEST_ADVISE_SECRET", "hunter2-the-real-one")

	webhook := func(t *testing.T, status int, body string) domain.Reaction {
		t.Helper()

		var hits atomic.Int64
		server := replyWith(t, &hits, status, body)
		return userAdviseWebhook("coach", []domain.Moment{domain.MomentPostToolResult},
			domain.WebhookHandler{URL: server.URL})
	}
	goAdvise := func(h domain.Handler) domain.Reaction {
		return domain.Reaction{
			ID:      "engine-advice",
			Origin:  domain.OriginEngine,
			Class:   domain.ClassAdvise,
			On:      []domain.Moment{domain.MomentSessionStarted},
			Handler: h,
		}
	}
	toolDoc := domain.SeamPayload{Event: domain.MomentPostToolResult, Tool: "list_dir"}
	startDoc := sessionStartedDoc(domain.SessionStartNew)

	tests := []struct {
		name     string
		reaction func(t *testing.T) domain.Reaction
		doc      domain.SeamPayload
		want     domain.Outcome
		wantErr  bool
		seamErr  bool
	}{
		{
			name: "a webhook's answer comes back redacted",
			reaction: func(t *testing.T) domain.Reaction {
				return webhook(t, http.StatusOK, "token=hunter2-the-real-one\n")
			},
			doc:  toolDoc,
			want: domain.Outcome{Inject: "token=[redacted]\n"},
		},
		{
			name: "an empty reply is silence: the zero Outcome and no error",
			reaction: func(t *testing.T) domain.Reaction {
				return webhook(t, http.StatusNoContent, "")
			},
			doc:  toolDoc,
			want: domain.Outcome{},
		},
		{
			name: "a non-2xx reply is returned as an error",
			reaction: func(t *testing.T) domain.Reaction {
				return webhook(t, http.StatusInternalServerError, "boom")
			},
			doc:     toolDoc,
			wantErr: true,
		},
		{
			name: "a SessionStartedFunc keeps its Detail",
			reaction: func(*testing.T) domain.Reaction {
				return goAdvise(domain.SessionStartedFunc(
					func(context.Context, domain.SessionStart) (domain.Outcome, error) {
						return domain.Outcome{Inject: "read AGENTS.md first", Detail: "rung=1"}, nil
					}))
			},
			doc:  startDoc,
			want: domain.Outcome{Inject: "read AGENTS.md first", Detail: "rung=1"},
		},
		{
			name: "a PostToolResultFunc is a mis-wiring: wrongHandler",
			reaction: func(*testing.T) domain.Reaction {
				return goAdvise(domain.PostToolResultFunc(
					func(context.Context, domain.LoopView, domain.ToolCall, *domain.ToolResultEdit) (domain.Outcome, error) {
						return domain.Outcome{Inject: "never asked"}, nil
					}))
			},
			doc:     startDoc,
			wantErr: true,
			seamErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &recordingSink{}
			var reported []string
			a := adviseArgvAgent(t, sink, t.TempDir(), false, &reported)
			a.cfg.SecretEnvVars = []string{"APOGEE_TEST_ADVISE_SECRET"}

			before := len(sink.events)
			got, err := a.callAdvise(context.Background(), 0, tt.doc, tt.reaction(t))

			if (err != nil) != tt.wantErr {
				t.Fatalf("callAdvise error = %v, want error %v", err, tt.wantErr)
			}
			if tt.seamErr && !errors.Is(err, errReactionSeam) {
				t.Errorf("callAdvise error = %v, want errReactionSeam", err)
			}
			if got != tt.want {
				t.Errorf("callAdvise = %+v, want %+v", got, tt.want)
			}
			if len(sink.events) != before {
				t.Errorf("events = %+v, want none from the call: it never emits", sink.events[before:])
			}
			if len(reported) != 0 {
				t.Errorf("reporter said %q, want nothing: the call never reports", reported)
			}
		})
	}
}
