package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"text/template"

	"github.com/airiclenz/apogee/internal/domain"
)

// starterReactionsExample returns the starter config's commented `reactions:` example as the
// config a user gets by uncommenting it: the one column-0 `# reactions:` line (CommentedExampleLine)
// and the run of comment lines under it, each with exactly one leading "# " (or a bare "#") taken
// off. That is the edit the template's comments tell a user to make, so the result is the text the
// shipped example asks them to trust.
func starterReactionsExample(t *testing.T, starter []byte) string {
	t.Helper()
	lines := strings.Split(string(starter), "\n")
	at, err := CommentedExampleLine(lines, "reactions")
	if err != nil {
		t.Fatalf("CommentedExampleLine: %v", err)
	}
	if at == 0 {
		t.Fatal("the starter config comments no `# reactions:` example at column 0")
	}
	var block []string
	for i := at - 1; i < len(lines) && strings.HasPrefix(lines[i], "#"); i++ {
		line := strings.TrimPrefix(lines[i], "#")
		block = append(block, strings.TrimPrefix(line, " "))
	}
	return strings.Join(block, "\n") + "\n"
}

// loadStarterReactions loads the uncommented example through the real config loader — the parser
// every Driver reads `reactions:` with — and returns the Reactions it resolves to. HOME is moved so
// the example's `~/…` workspace filter resolves under a directory the test owns.
func loadStarterReactions(t *testing.T, starter []byte) []domain.Reaction {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	path := writeReactionsConfig(t, starterReactionsExample(t, starter))
	opts, err := LoadFileConfig(path, os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("the uncommented starter `reactions:` example does not load: %v\n%s",
			err, starterReactionsExample(t, starter))
	}
	return opts.Reactions
}

// Every entry the starter config ships under its commented `reactions:` example is valid config:
// uncommented, the whole block loads through the real parser, so a user who copies it never meets
// a startup refusal. It pins the two ntfy entries in particular — the question-asked Moment under
// `run:`, the webhook mapping and its templating headers — since those are the example the manual's
// Push notifications section tells a user to uncomment.
func TestStarterConfigReactionsExampleLoads(t *testing.T) {
	list := loadStarterReactions(t, defaultConfigYAML)

	wantIDs := []string{"notify", "ci-bell", "no-force-push", "unformatted", "prime", "loaded-model",
		"ntfy-waiting", "ntfy-done"}
	seen := map[string]bool{}
	for _, reaction := range list {
		seen[reaction.ID] = true
	}
	for _, id := range wantIDs {
		if !seen[id] {
			t.Errorf("the uncommented example arms no %q entry; armed: %v", id, reactionIDs(list))
		}
	}

	waiting := starterWebhook(t, list, "ntfy-waiting")
	if got, want := momentsOf(list, "ntfy-waiting"), "approval-requested question-asked"; got != want {
		t.Errorf("ntfy-waiting fires on %q, want %q", got, want)
	}
	if got := momentsOf(list, "ntfy-done"); got != "exchange-finished" {
		t.Errorf("ntfy-done fires on %q, want %q", got, "exchange-finished")
	}
	done := starterWebhook(t, list, "ntfy-done")
	for id, hook := range map[string]domain.WebhookHandler{"ntfy-waiting": waiting, "ntfy-done": done} {
		if !strings.HasPrefix(hook.URL, "https://ntfy.sh/") {
			t.Errorf("%s posts to %q, want an https://ntfy.sh/ topic url", id, hook.URL)
		}
		if hook.Headers["X-Template"] != "yes" {
			t.Errorf("%s X-Template = %q, want %q — without it ntfy shows the raw JSON",
				id, hook.Headers["X-Template"], "yes")
		}
		if len(hook.HeadersEnv) != 0 {
			t.Errorf("%s arms headers-env %v; want it commented out — an unset variable fails the "+
				"request, and a public topic needs no token", id, hook.HeadersEnv)
		}
	}
	if waiting.Headers["X-Priority"] != "high" {
		t.Errorf("ntfy-waiting X-Priority = %q, want %q", waiting.Headers["X-Priority"], "high")
	}
}

// The ntfy templates the starter config ships render a sensible push from the very payload apogee
// POSTs. The X-Title, X-Message and X-Priority values are taken from the loaded example and run
// through Go's text/template — the engine ntfy's message templating is built on — against a real
// domain.SeamPayload marshalled to JSON and decoded into a map, as ntfy decodes the body it
// receives. missingkey=default is chosen on purpose: it is text/template's own behaviour, under
// which a misspelt field prints "<no value>" rather than failing, and the `omitempty` fields the
// templates branch on (faulted, step_capped, denied, schedule) are legitimately absent on most
// payloads, so missingkey=error would refuse a correct template. Each case therefore pins the exact
// text, which also catches a misspelt field inside an `if` that would otherwise read as false.
func TestStarterConfigNtfyTemplatesRender(t *testing.T) {
	list := loadStarterReactions(t, defaultConfigYAML)
	for _, tc := range ntfyTemplateCases() {
		t.Run(tc.name, func(t *testing.T) {
			hook := starterWebhook(t, list, tc.reaction)
			for header, want := range tc.want {
				got, err := renderNtfyTemplate(hook.Headers[header], tc.payload)
				if err != nil {
					t.Fatalf("%s %s does not render: %v", tc.reaction, header, err)
				}
				if strings.Contains(got, "<no value>") {
					t.Errorf("%s %s rendered %q — it names a field the payload does not carry",
						tc.reaction, header, got)
				}
				if got != want {
					t.Errorf("%s %s = %q, want %q", tc.reaction, header, got, want)
				}
			}
		})
	}
}

// ntfyTemplateCase is one payload the shipped ntfy templates must render, and what each header
// renders to.
type ntfyTemplateCase struct {
	name     string
	reaction string
	payload  domain.SeamPayload
	want     map[string]string
}

// ntfyTemplateCases are the firings the two shipped entries see: a run that finished, faulted or hit
// the step cap, with and without refused approvals and a schedule, and the two "you are needed"
// notices — the question text and an approval's reason riding the payload without reaching the push.
func ntfyTemplateCases() []ntfyTemplateCase {
	const workspace = "/work/repo"
	denied := func(n int) *int { return &n }
	finished := func(p domain.SeamPayload) domain.SeamPayload {
		p.Event, p.Reaction, p.Workspace = domain.MomentExchangeFinished, "ntfy-done", workspace
		p.Time, p.Status = "2026-11-24T09:41:00Z", "exchange-complete"
		return p
	}
	nightly := &domain.ScheduleRef{ID: "s-1", Name: "nightly-audit"}
	return []ntfyTemplateCase{
		{
			name: "finished", reaction: "ntfy-done",
			payload: finished(domain.SeamPayload{Denied: denied(0)}),
			want: map[string]string{
				"X-Title":   "apogee",
				"X-Message": "run finished in /work/repo",
			},
		},
		{
			name: "faulted with denials on a schedule", reaction: "ntfy-done",
			payload: finished(domain.SeamPayload{Faulted: true, Denied: denied(3), Schedule: nightly}),
			want: map[string]string{
				"X-Title":   "apogee: nightly-audit",
				"X-Message": "run failed in /work/repo; denied approvals: 3",
			},
		},
		{
			name: "step capped", reaction: "ntfy-done",
			payload: finished(domain.SeamPayload{StepCapped: true, Denied: denied(1)}),
			want: map[string]string{
				"X-Title":   "apogee",
				"X-Message": "run hit the step cap in /work/repo; denied approvals: 1",
			},
		},
		{
			name: "unnamed schedule", reaction: "ntfy-done",
			payload: finished(domain.SeamPayload{Denied: denied(0), Schedule: &domain.ScheduleRef{ID: "s-2"}}),
			want: map[string]string{
				"X-Title":   "apogee",
				"X-Message": "run finished in /work/repo",
			},
		},
		{
			name: "approval requested", reaction: "ntfy-waiting",
			payload: domain.SeamPayload{
				Event: domain.MomentApprovalRequested, Reaction: "ntfy-waiting", Workspace: workspace,
				Tool: "terminal", Reason: "runs a command outside the allow list",
			},
			want: map[string]string{
				"X-Title":    "apogee needs you",
				"X-Message":  "an approval is waiting in /work/repo",
				"X-Priority": "high",
			},
		},
		{
			name: "question asked", reaction: "ntfy-waiting",
			payload: domain.SeamPayload{
				Event: domain.MomentQuestionAsked, Reaction: "ntfy-waiting", Workspace: workspace,
				Question: "Which branch should I rebase onto?", Choices: []string{"main", "dev"},
			},
			want: map[string]string{
				"X-Title":    "apogee needs you",
				"X-Message":  "a question is waiting in /work/repo",
				"X-Priority": "high",
			},
		},
	}
}

// renderNtfyTemplate renders one header value against payload as ntfy would: the payload is
// marshalled to the JSON apogee POSTs and decoded back into a generic map before the template runs.
func renderNtfyTemplate(text string, payload domain.SeamPayload) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return "", err
	}
	tmpl, err := template.New("ntfy").Option("missingkey=default").Parse(text)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	if err := tmpl.Execute(&out, data); err != nil {
		return "", err
	}
	return out.String(), nil
}

// The two tests above fail when the shipped example is broken. Each mutation is applied to the
// starter config's text the way a careless edit would be, and the checks the tests run are run on
// the result: a misspelt Moment fails the load, and a misspelt payload field fails the render.
func TestStarterConfigExampleChecksCatchABrokenBlock(t *testing.T) {
	t.Run("misspelt moment", func(t *testing.T) {
		broken := strings.Replace(string(defaultConfigYAML),
			"on: [approval-requested, question-asked]", "on: [approval-requested, question-askd]", 1)
		if broken == string(defaultConfigYAML) {
			t.Fatal("the mutation found nothing to change; the shipped ntfy-waiting `on:` list moved")
		}
		t.Setenv("HOME", t.TempDir())
		path := writeReactionsConfig(t, starterReactionsExample(t, []byte(broken)))
		if _, err := LoadFileConfig(path, os.ReadFile, noNotify); err == nil {
			t.Error("a misspelt Moment in the example loaded; the load check would not catch it")
		}
	})
	t.Run("misspelt payload field", func(t *testing.T) {
		broken := strings.Replace(string(defaultConfigYAML),
			"in {{.workspace}}{{if .denied}}", "in {{.workspce}}{{if .denied}}", 1)
		if broken == string(defaultConfigYAML) {
			t.Fatal("the mutation found nothing to change; the shipped ntfy-done X-Message moved")
		}
		list := loadStarterReactions(t, []byte(broken))
		hook := starterWebhook(t, list, "ntfy-done")
		got, err := renderNtfyTemplate(hook.Headers["X-Message"], ntfyTemplateCases()[0].payload)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if !strings.Contains(got, "<no value>") {
			t.Errorf("a misspelt field rendered %q with no <no value>; the render check would not "+
				"catch it", got)
		}
	})
	t.Run("misspelt field inside an if", func(t *testing.T) {
		broken := strings.Replace(string(defaultConfigYAML), "{{if .faulted}}", "{{if .faultd}}", 1)
		if broken == string(defaultConfigYAML) {
			t.Fatal("the mutation found nothing to change; the shipped ntfy-done X-Message moved")
		}
		list := loadStarterReactions(t, []byte(broken))
		hook := starterWebhook(t, list, "ntfy-done")
		got, err := renderNtfyTemplate(hook.Headers["X-Message"], ntfyTemplateCases()[1].payload)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if want := ntfyTemplateCases()[1].want["X-Message"]; got == want {
			t.Errorf("a misspelt field inside an if still rendered %q; the exact-text check would "+
				"not catch it", got)
		}
	})
}

// starterWebhook returns the webhook handler the observe Reaction id resolves to.
func starterWebhook(t *testing.T, list []domain.Reaction, id string) domain.WebhookHandler {
	t.Helper()
	for _, reaction := range list {
		if reaction.ID != id || reaction.Class != domain.ClassObserve {
			continue
		}
		hook, ok := reaction.Handler.(domain.WebhookHandler)
		if !ok {
			t.Fatalf("%s handler = %T; want a domain.WebhookHandler", id, reaction.Handler)
		}
		return hook
	}
	t.Fatalf("the uncommented example arms no observe entry %q; armed: %v", id, reactionIDs(list))
	return domain.WebhookHandler{}
}

// momentsOf spells the `on:` list of the observe Reaction id, space-separated.
func momentsOf(list []domain.Reaction, id string) string {
	for _, reaction := range list {
		if reaction.ID == id && reaction.Class == domain.ClassObserve {
			moments := make([]string, 0, len(reaction.On))
			for _, moment := range reaction.On {
				moments = append(moments, string(moment))
			}
			return strings.Join(moments, " ")
		}
	}
	return ""
}
