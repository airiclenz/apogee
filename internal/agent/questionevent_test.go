package agent

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
)

// ----------------------------------------------------------------------------
// The QuestionEvent — a question put to the human, reported before it is waited on
// ----------------------------------------------------------------------------
//
// Three places put a question to the human, and each reports it as a QuestionEvent BEFORE the
// Asker is reached (the question-asked notice fires on it, as approval-requested fires on an
// Approval's requested phase): the ask_user tool, a recipe's prompt for a missing input (which
// also carries a foreground Workflow's `ask` stage), and a background Workflow's `ask` stage once
// it queues. Each is announced exactly once.

// questionEvents returns every QuestionEvent sink saw, in emission order.
func questionEvents(sink *lockedSink) []domain.QuestionEvent {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	var questions []domain.QuestionEvent
	for _, e := range sink.events {
		if question, ok := e.(domain.QuestionEvent); ok {
			questions = append(questions, question)
		}
	}
	return questions
}

// firstTopLevelTurn returns the Turn index of the first Depth-0 TurnEvent sink saw.
func firstTopLevelTurn(t *testing.T, sink *lockedSink) int {
	t.Helper()
	sink.mu.Lock()
	defer sink.mu.Unlock()
	for _, e := range sink.events {
		if turn, ok := e.(domain.TurnEvent); ok && turn.Depth == 0 {
			return turn.Turn
		}
	}
	t.Fatal("the run emitted no top-level TurnEvent")
	return 0
}

// announcedAsker answers every question with answer and records, for each Ask, how many
// QuestionEvents the sink held when it arrived — so a test can pin that the event came first.
type announcedAsker struct {
	sink   *lockedSink
	answer string

	mu        sync.Mutex
	seen      []domain.AskRequest
	seenAfter []int
}

func (a *announcedAsker) Ask(_ context.Context, req domain.AskRequest) (domain.AskAnswer, error) {
	announced := len(questionEvents(a.sink))
	a.mu.Lock()
	defer a.mu.Unlock()
	a.seen = append(a.seen, req)
	a.seenAfter = append(a.seenAfter, announced)
	return domain.AskAnswer{Text: a.answer}, nil
}

// asked returns the questions the Asker saw and the announcement count at each.
func (a *announcedAsker) asked() ([]domain.AskRequest, []int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.seen), slices.Clone(a.seenAfter)
}

// TestQuestionEvent_AskUserIsAnnouncedBeforeTheAskerIsCalled pins the ask_user emit point end to
// end: the engine's per-call announcer emits one QuestionEvent, stamped with the Turn the call ran
// in, carrying the question as the Asker then receives it — and the Asker is reached only after it.
func TestQuestionEvent_AskUserIsAnnouncedBeforeTheAskerIsCalled(t *testing.T) {
	sink := newLockedSink()
	asker := &announcedAsker{sink: sink, answer: "main"}
	cfg := configWithTools(sink, tools.NewAskUser(asker))
	up := scriptedResponder(t,
		toolCallTurn("q1", "ask_user", `{"question":"which branch?","choices":["main","dev"]}`),
		contentTurn("done"),
	)
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "ask me"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	seen, seenAfter := asker.asked()
	if !slices.Equal(seenAfter, []int{1}) {
		t.Fatalf("the Asker was reached with %v questions announced, want once, after the one announcement", seenAfter)
	}
	questions := questionEvents(sink)
	if len(questions) != 1 {
		t.Fatalf("emitted %d QuestionEvents, want 1", len(questions))
	}
	got := questions[0]
	if got.Request.Question != "which branch?" || !slices.Equal(got.Request.Choices, []string{"main", "dev"}) {
		t.Errorf("QuestionEvent request = %+v, want the model's question and choices", got.Request)
	}
	if got.Request.Question != seen[0].Question || !slices.Equal(got.Request.Choices, seen[0].Choices) {
		t.Errorf("announced %+v, the Asker saw %+v; want the same question", got.Request, seen[0])
	}
	if want := firstTopLevelTurn(t, sink); got.Depth != 0 || got.Turn != want {
		t.Errorf("QuestionEvent identity depth %d turn %d, want depth 0 turn %d", got.Depth, got.Turn, want)
	}
}

// TestQuestionEvent_ARecipesMissingInputIsAnnounced pins the recipe emit point on both launch
// paths: a typed `/review` and a Driver's StartRecipe each ask for the unbound required input, and
// each reports the question first, at the Turn the launch's Exchange runs in.
func TestQuestionEvent_ARecipesMissingInputIsAnnounced(t *testing.T) {
	t.Parallel()

	const question = "/review needs scope: the folder to review"
	launches := []struct {
		name   string
		launch func(t *testing.T, a *Agent)
	}{
		{"typed", func(t *testing.T, a *Agent) {
			runInput(t, a, domain.UserInput{Text: "/review", SkillIDs: []string{"review"}})
		}},
		{"StartRecipe", func(t *testing.T, a *Agent) {
			if _, err := a.StartRecipe(context.Background(), RecipeLaunch{SkillID: "review"}); err != nil {
				t.Fatalf("StartRecipe: %v", err)
			}
			if _, err := a.Run(context.Background()); err != nil {
				t.Fatalf("Run: %v", err)
			}
		}},
	}
	for _, tc := range launches {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sink := newLockedSink()
			cfg := recipeConfig(t, sink, reviewRecipe())
			asker := &announcedAsker{sink: sink, answer: "pkg"}
			cfg.Asker = asker
			a, err := newAgent(cfg, reviewUpstream("pkg"))
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}

			tc.launch(t, a)

			seen, seenAfter := asker.asked()
			if len(seen) != 1 || seen[0].Question != question || !slices.Equal(seenAfter, []int{1}) {
				t.Fatalf("the Asker saw %+v with %v announced, want the scope question once, after its announcement", seen, seenAfter)
			}
			questions := questionEvents(sink)
			if len(questions) != 1 || questions[0].Request.Question != question || questions[0].Request.Choices != nil {
				t.Fatalf("QuestionEvents = %+v, want the one scope question", questions)
			}
			if want := firstTopLevelTurn(t, sink); questions[0].Turn != want {
				t.Errorf("QuestionEvent turn = %d, want the launch's Turn %d", questions[0].Turn, want)
			}
		})
	}
}

// TestQuestionEvent_AForegroundAskStageIsAnnouncedOnce pins that a foreground Workflow's `ask`
// stage, which reaches the human through the recipe's own prompt (recipeAsker), is reported by
// exactly one QuestionEvent — its options as the choices, at the Turn the recipe started on, the
// Turn its WorkflowWaiting event carries — and not a second time by the observer that reports the
// stage as waiting.
func TestQuestionEvent_AForegroundAskStageIsAnnouncedOnce(t *testing.T) {
	t.Parallel()

	sink := newLockedSink()
	cfg := recipeConfig(t, sink, askRecipe())
	asker := &announcedAsker{sink: sink, answer: "yes"}
	cfg.Asker = asker
	up := (&workflowResponder{}).
		route("/asking", nil, contentScript("swept")).
		route("sweep alpha", nil, finishScript("f1", "alpha is fine"))
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	runInput(t, a, domain.UserInput{Text: "/asking", SkillIDs: []string{"asking"}})

	seen, seenAfter := asker.asked()
	if len(seen) != 1 || seen[0].Question != "Sweep now?" || !slices.Equal(seenAfter, []int{1}) {
		t.Fatalf("the Asker saw %+v with %v announced, want the stage's question once, after its announcement", seen, seenAfter)
	}
	questions := questionEvents(sink)
	if len(questions) != 1 {
		t.Fatalf("emitted %d QuestionEvents, want exactly 1: %+v", len(questions), questions)
	}
	if got := questions[0].Request; got.Question != "Sweep now?" || !slices.Equal(got.Choices, []string{"yes", "no"}) {
		t.Errorf("QuestionEvent request = %+v, want the stage's question and options", got)
	}
	waiting := waitingPhases(sink)
	if len(waiting) != 1 || questions[0].Turn != waiting[0].Turn || questions[0].Turn != firstTopLevelTurn(t, sink) {
		t.Errorf("QuestionEvent turn = %d, waiting events %+v; want the recipe's launch Turn on both", questions[0].Turn, waiting)
	}
}

// TestQuestionEvent_ABackgroundAskStageIsAnnouncedOnce pins the background emit point: the
// question queued in the session's background prompts is reported once, beside its WorkflowWaiting
// event and stamped with the same launch Turn, and the host Asker is never reached.
func TestQuestionEvent_ABackgroundAskStageIsAnnouncedOnce(t *testing.T) {
	t.Parallel()

	sink := newPromptProbeSink()
	cfg := recipeConfig(t, sink, askRecipe())
	host := &scriptedAsker{answer: "host"}
	cfg.Asker = host
	up := (&workflowResponder{}).route("sweep alpha", nil, finishScript("f1", "alpha is fine"))
	a := newBackgroundParent(t, cfg, up)
	sink.agent.Store(a)
	launchBackground(t, a, "asking")
	awaitClosed(t, sink.waited, "the question's waiting event")

	prompts := a.WorkflowPrompts()
	if len(prompts) != 1 || prompts[0].Question == nil {
		t.Fatalf("WorkflowPrompts = %+v, want the one question", prompts)
	}
	if !a.AnswerWorkflowPrompt(prompts[0].ID, domain.WorkflowPromptAnswer{Text: "yes"}) {
		t.Fatal("AnswerWorkflowPrompt refused the waiting question")
	}
	a.background.waitAll()

	questions := questionEvents(sink.lockedSink)
	if len(questions) != 1 {
		t.Fatalf("emitted %d QuestionEvents, want exactly 1: %+v", len(questions), questions)
	}
	if got := questions[0].Request; got.Question != "Sweep now?" || !slices.Equal(got.Choices, []string{"yes", "no"}) {
		t.Errorf("QuestionEvent request = %+v, want the stage's question and options", got)
	}
	if waiting := sink.waitings(); len(waiting) != 1 || questions[0].Turn != waiting[0].Turn {
		t.Errorf("QuestionEvent turn = %d, waiting events %+v; want the launch Turn the waiting event carries", questions[0].Turn, waiting)
	}
	if len(host.questions) != 0 {
		t.Errorf("the host Asker was asked %+v, want nothing", host.questions)
	}
}

// waitingPhases returns every WorkflowWaiting event sink saw, in order.
func waitingPhases(sink *lockedSink) []domain.WorkflowPhaseEvent {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	var waiting []domain.WorkflowPhaseEvent
	for _, e := range sink.events {
		if phase, ok := e.(domain.WorkflowPhaseEvent); ok && phase.Phase == domain.WorkflowWaiting {
			waiting = append(waiting, phase)
		}
	}
	return waiting
}
