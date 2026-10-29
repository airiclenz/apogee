package config

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// writeReactionsConfig writes one config file holding the given body and returns its path.
func writeReactionsConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// A `reactions:` block resolves onto the user-origin Reactions a root arms: origin and class
// stamped, the `on:` list read as Moments, each action key turned into the handler that runs it —
// `run:` as a sequence into an argv command and as a mapping into a webhook, `gate:` as the argv of
// the command whose verdict the Approver reads, `advise:` as the argv of the command whose stdout
// the model reads — an absent `timeout:` defaulted per CLASS (30s observe, 10s advise, 5s gate,
// since the loop waits on the sync classes and a person on a gate) and a spelled one parsed,
// `workspace:` reduced to the comparable spelling with its leading `~` expanded, and a parked entry
// dropped rather than armed. An entry spelling `run:` beside `advise:` resolves to an observe and an
// advise Reaction of one id — the lint case — and the whole list splits into the two lanes the
// Drivers fire. The home directory is moved for the case rather than read, so the `~` rule is
// asserted against a path this test owns.
func TestLoadFileConfigResolvesTheReactionsBlock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := writeReactionsConfig(t, `
reactions:
  - id: notify
    on: [exchange-finished, error]
    run: ["notify-send", "apogee finished"]
    workspace: ~/work
  - id: bell
    on: [approval-requested]
    run:
      url: https://hooks.example.com/apogee
      headers:
        X-Source: apogee
      headers-env:
        Authorization: MY_WEBHOOK_TOKEN
    timeout: 250ms
  - id: parked
    on: [turn-finished]
    run: ["true"]
    enabled: false
  - id: warden
    on: [pre-tool-exec]
    gate: ["./scripts/guard.sh", "--strict"]
  - id: quick-warden
    on: [pre-tool-exec]
    gate: ["./scripts/guard.sh"]
    timeout: 2s
  - id: lint
    on: [file-changed]
    run: ["./scripts/log-change.sh"]
    advise: ["./scripts/lint.sh", "--quiet"]
`)

	opts, err := LoadFileConfig(path, os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("LoadFileConfig: %v", err)
	}

	if len(opts.Reactions) != 6 {
		t.Fatalf("resolved %d reactions; want the 6 the 5 armed entries arm (the parked one is dropped, "+
			"the lint entry arms two): %+v", len(opts.Reactions), opts.Reactions)
	}

	notify := opts.Reactions[0]
	if notify.ID != "notify" {
		t.Errorf("first id = %q, want %q", notify.ID, "notify")
	}
	if notify.Origin != domain.OriginUser || notify.Class != domain.ClassObserve {
		t.Errorf("first entry is %s/%s, want %s/%s",
			notify.Origin, notify.Class, domain.OriginUser, domain.ClassObserve)
	}
	wantOn := []domain.Moment{domain.MomentExchangeFinished, domain.MomentError}
	if len(notify.On) != len(wantOn) {
		t.Fatalf("first entry fires on %v, want %v", notify.On, wantOn)
	}
	for i := range wantOn {
		if notify.On[i] != wantOn[i] {
			t.Fatalf("first entry fires on %v, want %v", notify.On, wantOn)
		}
	}
	argv, ok := notify.Handler.(domain.ArgvHandler)
	if !ok {
		t.Fatalf("first handler = %T; want a domain.ArgvHandler for a `run:` sequence", notify.Handler)
	}
	if len(argv.Argv) != 2 || argv.Argv[0] != "notify-send" || argv.Argv[1] != "apogee finished" {
		t.Errorf("first argv = %v, want the two elements the sequence spells", argv.Argv)
	}
	if notify.Timeout != defaultReactionTimeout {
		t.Errorf("first timeout = %v, want the %v default an entry spelling none takes",
			notify.Timeout, defaultReactionTimeout)
	}
	if notify.Workspace == "" || !strings.HasSuffix(notify.Workspace, "work") {
		t.Errorf("first workspace = %q, want the expanded ~/work under %q", notify.Workspace, home)
	}

	bell := opts.Reactions[1]
	webhook, ok := bell.Handler.(domain.WebhookHandler)
	if !ok {
		t.Fatalf("second handler = %T; want a domain.WebhookHandler for a `run:` mapping", bell.Handler)
	}
	if webhook.URL != "https://hooks.example.com/apogee" {
		t.Errorf("webhook url = %q, want the one the mapping spells", webhook.URL)
	}
	if webhook.Headers["X-Source"] != "apogee" {
		t.Errorf("webhook headers = %v, want the literal X-Source the mapping spells", webhook.Headers)
	}
	if webhook.HeadersEnv["Authorization"] != "MY_WEBHOOK_TOKEN" {
		t.Errorf("webhook headers-env = %v, want the variable NAME the mapping spells", webhook.HeadersEnv)
	}
	if bell.Timeout != 250*time.Millisecond {
		t.Errorf("second timeout = %v, want the 250ms the entry spells", bell.Timeout)
	}
	if bell.Workspace != "" {
		t.Errorf("second workspace = %q, want the unset filter for an entry naming none", bell.Workspace)
	}

	warden := opts.Reactions[2]
	if warden.ID != "warden" || warden.Class != domain.ClassGate {
		t.Errorf("third entry is %q/%s, want the gate reaction %q arms", warden.ID, warden.Class, "warden")
	}
	gateArgv, ok := warden.Handler.(domain.ArgvHandler)
	if !ok {
		t.Fatalf("third handler = %T; want a domain.ArgvHandler for a `gate:` sequence", warden.Handler)
	}
	if len(gateArgv.Argv) != 2 || gateArgv.Argv[0] != "./scripts/guard.sh" || gateArgv.Argv[1] != "--strict" {
		t.Errorf("third argv = %v, want the two elements the sequence spells", gateArgv.Argv)
	}
	if warden.Timeout != domain.DefaultGateTimeout {
		t.Errorf("third timeout = %v, want the %v a gate spelling none takes — shorter than the %v an "+
			"observe entry takes, because a person waits on a verdict",
			warden.Timeout, domain.DefaultGateTimeout, defaultReactionTimeout)
	}

	if quick := opts.Reactions[3]; quick.Timeout != 2*time.Second {
		t.Errorf("fourth timeout = %v, want the 2s the entry spells over the class default", quick.Timeout)
	}

	lintObserve, lintAdvise := opts.Reactions[4], opts.Reactions[5]
	if lintObserve.ID != "lint" || lintObserve.Class != domain.ClassObserve {
		t.Errorf("fifth entry is %q/%s, want the observe reaction %q's `run:` arms",
			lintObserve.ID, lintObserve.Class, "lint")
	}
	if lintAdvise.ID != "lint" || lintAdvise.Class != domain.ClassAdvise {
		t.Errorf("sixth entry is %q/%s, want the advise reaction %q's `advise:` arms",
			lintAdvise.ID, lintAdvise.Class, "lint")
	}
	adviseArgv, ok := lintAdvise.Handler.(domain.ArgvHandler)
	if !ok {
		t.Fatalf("sixth handler = %T; want a domain.ArgvHandler for an `advise:` sequence", lintAdvise.Handler)
	}
	if len(adviseArgv.Argv) != 2 || adviseArgv.Argv[0] != "./scripts/lint.sh" || adviseArgv.Argv[1] != "--quiet" {
		t.Errorf("sixth argv = %v, want the two elements the sequence spells", adviseArgv.Argv)
	}
	if len(lintAdvise.On) != 1 || lintAdvise.On[0] != domain.MomentFileChanged {
		t.Errorf("sixth entry fires on %v, want the one file-changed Moment the entry spells", lintAdvise.On)
	}
	if lintObserve.Timeout != defaultReactionTimeout || lintAdvise.Timeout != domain.DefaultAdviseTimeout {
		t.Errorf("lint timeouts = %v/%v, want the %v observe and %v advise defaults — one entry, two "+
			"class defaults", lintObserve.Timeout, lintAdvise.Timeout,
			defaultReactionTimeout, domain.DefaultAdviseTimeout)
	}

	observe, sync := domain.SplitLanes(opts.Reactions)
	if got := laneIDs(observe); got != "notify bell lint" {
		t.Errorf("observe lane = %q, want %q — every `run:` in file order", got, "notify bell lint")
	}
	if got := laneIDs(sync); got != "warden quick-warden lint" {
		t.Errorf("sync lane = %q, want %q — every `advise:` and `gate:` in file order",
			got, "warden quick-warden lint")
	}
}

// laneIDs spells one lane's ids in order, space-separated, so a lane can be compared in one line.
func laneIDs(lane []domain.Reaction) string {
	ids := make([]string, 0, len(lane))
	for _, reaction := range lane {
		ids = append(ids, reaction.ID)
	}
	return strings.Join(ids, " ")
}

// `advise:` and `gate:` take the same webhook mapping `run:` does (ADR 0076 D2, issue apogee-1d8):
// a mapping under either key resolves to a [domain.WebhookHandler] carrying the url and both header
// mappings, stamped with the key's class and the key's own default timeout when the entry spells
// none. The two arm on the sync lane exactly as their argv twins do.
func TestLoadFileConfigResolvesSyncWebhookMappings(t *testing.T) {
	t.Parallel()

	path := writeReactionsConfig(t, `
reactions:
  - id: coach
    on: [post-tool-result]
    advise:
      url: https://hooks.example.com/advise
      headers:
        X-Source: apogee
      headers-env:
        Authorization: COACH_TOKEN
  - id: warden
    on: [pre-tool-exec]
    gate:
      url: https://hooks.example.com/gate
    timeout: 2s
`)

	opts, err := LoadFileConfig(path, os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("LoadFileConfig: %v", err)
	}

	if len(opts.Reactions) != 2 {
		t.Fatalf("resolved %d reactions; want the 2 the two entries arm: %+v", len(opts.Reactions), opts.Reactions)
	}
	coach, warden := opts.Reactions[0], opts.Reactions[1]
	if coach.ID != "coach" || coach.Class != domain.ClassAdvise {
		t.Errorf("first entry is %q/%s, want the advise reaction %q's `advise:` mapping arms",
			coach.ID, coach.Class, "coach")
	}
	adviseHook, ok := coach.Handler.(domain.WebhookHandler)
	if !ok {
		t.Fatalf("first handler = %T; want a domain.WebhookHandler for an `advise:` mapping", coach.Handler)
	}
	if adviseHook.URL != "https://hooks.example.com/advise" {
		t.Errorf("advise url = %q, want the one the mapping spells", adviseHook.URL)
	}
	if adviseHook.Headers["X-Source"] != "apogee" || adviseHook.HeadersEnv["Authorization"] != "COACH_TOKEN" {
		t.Errorf("advise headers = %v / headers-env = %v, want the literal header and the variable NAME the "+
			"mapping spells", adviseHook.Headers, adviseHook.HeadersEnv)
	}
	if coach.Timeout != domain.DefaultAdviseTimeout {
		t.Errorf("advise timeout = %v, want the %v an advise entry spelling none takes",
			coach.Timeout, domain.DefaultAdviseTimeout)
	}

	if warden.ID != "warden" || warden.Class != domain.ClassGate {
		t.Errorf("second entry is %q/%s, want the gate reaction %q's `gate:` mapping arms",
			warden.ID, warden.Class, "warden")
	}
	gateHook, ok := warden.Handler.(domain.WebhookHandler)
	if !ok {
		t.Fatalf("second handler = %T; want a domain.WebhookHandler for a `gate:` mapping", warden.Handler)
	}
	if gateHook.URL != "https://hooks.example.com/gate" {
		t.Errorf("gate url = %q, want the one the mapping spells", gateHook.URL)
	}
	if warden.Timeout != 2*time.Second {
		t.Errorf("gate timeout = %v, want the 2s the entry spells over the class default", warden.Timeout)
	}

	observe, sync := domain.SplitLanes(opts.Reactions)
	if len(observe) != 0 || laneIDs(sync) != "coach warden" {
		t.Errorf("lanes = observe %q / sync %q, want both webhooks on the sync lane and none on observe",
			laneIDs(observe), laneIDs(sync))
	}
}

// mcpShapeSentence is the tail of the shape refusal every action key earns, the three shapes it may
// take listed together.
const mcpShapeSentence = "is an argv list, a webhook mapping {url:, headers:, headers-env:} or an mcp mapping " +
	"{mcp: {server:, tool:, args:, payload-arg:}}"

// A `run:`, `advise:` or `gate:` mapping whose only key is `mcp:` loads as an MCPHandler carrying
// the server alias, the server's own tool name, the static args and the payload argument exactly as
// the entry spells them (adr-0076-amend-1029), under the class its key names.
func TestLoadFileConfigResolvesMCPMappings(t *testing.T) {
	t.Parallel()

	path := writeReactionsConfig(t, `
reactions:
  - id: tally
    on: [turn-finished]
    run:
      mcp:
        server: notes
        tool: append
        payload-arg: event
  - id: launcher
    on: [session-started]
    advise:
      mcp:
        server: llama
        tool: status
        args:
          verbose: true
          profile: coder
  - id: warden
    on: [pre-tool-exec]
    gate:
      mcp:
        server: policy
        tool: decide
`)

	opts, err := LoadFileConfig(path, os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("LoadFileConfig: %v", err)
	}

	want := []struct {
		id      string
		class   domain.Class
		handler domain.MCPHandler
	}{
		{id: "tally", class: domain.ClassObserve,
			handler: domain.MCPHandler{Server: "notes", Tool: "append", PayloadArg: "event"}},
		{id: "launcher", class: domain.ClassAdvise, handler: domain.MCPHandler{Server: "llama", Tool: "status",
			Args: map[string]any{"verbose": true, "profile": "coder"}}},
		{id: "warden", class: domain.ClassGate, handler: domain.MCPHandler{Server: "policy", Tool: "decide"}},
	}
	if len(opts.Reactions) != len(want) {
		t.Fatalf("resolved %d reactions; want the %d the three entries arm: %+v",
			len(opts.Reactions), len(want), opts.Reactions)
	}
	for i, w := range want {
		got := opts.Reactions[i]
		if got.ID != w.id || got.Class != w.class {
			t.Errorf("entry %d is %q/%s; want %q/%s", i, got.ID, got.Class, w.id, w.class)
		}
		handler, ok := got.Handler.(domain.MCPHandler)
		if !ok {
			t.Errorf("%s handler = %T; want a domain.MCPHandler for an `mcp:` mapping", w.id, got.Handler)
			continue
		}
		if !reflect.DeepEqual(handler, w.handler) {
			t.Errorf("%s handler = %+v; want %+v", w.id, handler, w.handler)
		}
	}
	if names := ReactionEnvNames(opts); len(names) != 0 {
		t.Errorf("ReactionEnvNames = %v; want none — an mcp entry reads no environment variable", names)
	}
}

// `advise:` on `session-started` is admitted (ADR 0076 amendment 1021): an argv or webhook entry
// on that Moment loads as one advise Reaction on the sync lane, and an entry spelling `run:` beside
// `advise:` on `[session-started]` loads — session-started is a notice `run:` reacts to and a
// Moment `advise:` reacts at — as one observe and one advise Reaction of the same id, one per lane.
func TestLoadFileConfigAdmitsSessionStartedAdvice(t *testing.T) {
	t.Parallel()

	path := writeReactionsConfig(t, `
reactions:
  - id: primer
    on: [session-started]
    run: ["./scripts/log-start.sh"]
    advise: ["capcom", "prime"]
  - id: briefing
    on: [session-started]
    advise:
      url: https://hooks.example.com/brief
`)

	opts, err := LoadFileConfig(path, os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("LoadFileConfig: %v", err)
	}
	if len(opts.Reactions) != 3 {
		t.Fatalf("resolved %d reactions; want the 3 the two entries arm (primer arms two): %+v",
			len(opts.Reactions), opts.Reactions)
	}

	primerObserve, primerAdvise, briefing := opts.Reactions[0], opts.Reactions[1], opts.Reactions[2]
	if primerObserve.ID != "primer" || primerObserve.Class != domain.ClassObserve {
		t.Errorf("first entry is %q/%s, want the observe reaction %q's `run:` arms",
			primerObserve.ID, primerObserve.Class, "primer")
	}
	if primerAdvise.ID != "primer" || primerAdvise.Class != domain.ClassAdvise {
		t.Errorf("second entry is %q/%s, want the advise reaction %q's `advise:` arms",
			primerAdvise.ID, primerAdvise.Class, "primer")
	}
	adviseArgv, ok := primerAdvise.Handler.(domain.ArgvHandler)
	if !ok {
		t.Fatalf("second handler = %T; want a domain.ArgvHandler for an `advise:` sequence",
			primerAdvise.Handler)
	}
	if !slices.Equal(adviseArgv.Argv, []string{"capcom", "prime"}) {
		t.Errorf("second argv = %v, want the two elements the sequence spells", adviseArgv.Argv)
	}
	for _, reaction := range opts.Reactions {
		if !slices.Equal(reaction.On, []domain.Moment{domain.MomentSessionStarted}) {
			t.Errorf("%q/%s fires on %v, want the one session-started Moment its entry spells",
				reaction.ID, reaction.Class, reaction.On)
		}
	}

	if briefing.ID != "briefing" || briefing.Class != domain.ClassAdvise {
		t.Errorf("third entry is %q/%s, want the advise reaction %q's `advise:` mapping arms",
			briefing.ID, briefing.Class, "briefing")
	}
	webhook, ok := briefing.Handler.(domain.WebhookHandler)
	if !ok {
		t.Fatalf("third handler = %T; want a domain.WebhookHandler for an `advise:` mapping",
			briefing.Handler)
	}
	if webhook.URL != "https://hooks.example.com/brief" {
		t.Errorf("webhook url = %q, want the one the mapping spells", webhook.URL)
	}

	observe, sync := domain.SplitLanes(opts.Reactions)
	if got := laneIDs(observe); got != "primer" {
		t.Errorf("observe lane = %q, want %q — the one `run:`", got, "primer")
	}
	if got := laneIDs(sync); got != "primer briefing" {
		t.Errorf("sync lane = %q, want %q — every `advise:` in file order", got, "primer briefing")
	}
}

// `compaction-finished` is a notice `run:` reacts to and a Moment `advise:` reacts at (ADR 0076,
// adr-0076-amend-1021b), so the entry that briefs a session at its start and again after every fold
// — `run:` beside `advise:` on `[session-started, compaction-finished]` — loads as one observe and
// one advise Reaction of the same id, each firing on both Moments.
func TestLoadFileConfigAdmitsCompactionFinishedAdvice(t *testing.T) {
	t.Parallel()

	path := writeReactionsConfig(t, `
reactions:
  - id: primer
    on: [session-started, compaction-finished]
    run: ["./scripts/log-brief.sh"]
    advise: ["capcom", "prime"]
`)

	opts, err := LoadFileConfig(path, os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("LoadFileConfig: %v", err)
	}
	if len(opts.Reactions) != 2 {
		t.Fatalf("resolved %d reactions; want the 2 the entry arms: %+v",
			len(opts.Reactions), opts.Reactions)
	}

	wantOn := []domain.Moment{domain.MomentSessionStarted, domain.MomentCompactionFinished}
	for i, wantClass := range []domain.Class{domain.ClassObserve, domain.ClassAdvise} {
		reaction := opts.Reactions[i]
		if reaction.ID != "primer" || reaction.Class != wantClass {
			t.Errorf("entry %d is %q/%s, want %q/%s", i, reaction.ID, reaction.Class, "primer", wantClass)
		}
		if !slices.Equal(reaction.On, wantOn) {
			t.Errorf("%q/%s fires on %v, want %v", reaction.ID, reaction.Class, reaction.On, wantOn)
		}
	}
}

// An empty or absent block resolves to no Reactions and no error: the lane is dormant by default,
// exactly as `mcp-servers:` is. A block whose every entry is parked resolves the same way, so a
// file that keeps its definitions without arming them is indistinguishable from one that has none.
func TestLoadFileConfigWithoutArmedReactionsResolvesNone(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		body string
	}{
		{name: "no block at all", body: "mode: auto\n"},
		{name: "every entry parked", body: "reactions:\n  - id: parked\n    on: [error]\n    run: [\"true\"]\n    enabled: false\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts, err := LoadFileConfig(writeReactionsConfig(t, tt.body), os.ReadFile, noNotify)
			if err != nil {
				t.Fatalf("LoadFileConfig: %v", err)
			}
			if opts.Reactions != nil {
				t.Errorf("resolved reactions = %+v; want none", opts.Reactions)
			}
		})
	}
}

// Every refusal the `reactions:` block can earn, pinned by its sentence. A user meets these at
// startup with nothing else to go on, so the wording is the contract: each one names the entry it
// is about and says what to write instead.
func TestLoadFileConfigRefusesMalformedReactions(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "run: is a bare string",
			body: "reactions:\n  - id: notify\n    on: [error]\n    run: notify-send\n",
			want: `reaction "notify": run: is an argv list, a webhook mapping {url:, headers:, headers-env:} or an mcp mapping {mcp: {server:, tool:, args:, payload-arg:}}`,
		},
		{
			name: "run: is absent",
			body: "reactions:\n  - id: notify\n    on: [error]\n",
			want: `reaction "notify": run: is an argv list, a webhook mapping {url:, headers:, headers-env:} or an mcp mapping {mcp: {server:, tool:, args:, payload-arg:}}`,
		},
		{
			name: "the webhook mapping carries a key it does not have",
			body: "reactions:\n  - id: notify\n    on: [error]\n    run:\n      url: https://example.com/\n      header-env:\n        A: B\n",
			want: `reaction "notify": run: is an argv list, a webhook mapping {url:, headers:, headers-env:} or an mcp mapping {mcp: {server:, tool:, args:, payload-arg:}}`,
		},
		{
			name: "advise: is a bare string",
			body: "reactions:\n  - id: coach\n    on: [post-tool-result]\n    advise: say-something\n",
			want: `reaction "coach": advise: is an argv list, a webhook mapping {url:, headers:, headers-env:} or an mcp mapping {mcp: {server:, tool:, args:, payload-arg:}}`,
		},
		{
			name: "advise: is a webhook mapping carrying a key it does not have",
			body: "reactions:\n  - id: coach\n    on: [post-tool-result]\n    advise:\n      url: https://example.com/\n      header-env:\n        A: B\n",
			want: `reaction "coach": advise: is an argv list, a webhook mapping {url:, headers:, headers-env:} or an mcp mapping {mcp: {server:, tool:, args:, payload-arg:}}`,
		},
		{
			name: "advise: is an empty argv list",
			body: "reactions:\n  - id: coach\n    on: [post-tool-result]\n    advise: []\n",
			want: `invalid reaction "coach": the command's first element is the program to run and must not be blank`,
		},
		{
			name: "advise: reacts at a Moment it cannot take",
			body: "reactions:\n  - id: coach\n    on: [turn-finished]\n    advise: [\"say-something\"]\n",
			want: `invalid reaction "coach": advise: reacts at post-tool-result, file-changed, session-started or compaction-finished; "turn-finished" is none of them`,
		},
		{
			name: "gate: reacts at session-started",
			body: "reactions:\n  - id: warden\n    on: [session-started]\n    gate: [\"decide\"]\n",
			want: `invalid reaction "warden": gate: reacts at pre-tool-exec; "session-started" is not it`,
		},
		{
			name: "gate: reacts at compaction-finished",
			body: "reactions:\n  - id: warden\n    on: [compaction-finished]\n    gate: [\"decide\"]\n",
			want: `invalid reaction "warden": gate: reacts at pre-tool-exec; "compaction-finished" is not it`,
		},
		{
			name: "an mcp mapping carries a key it does not have",
			body: "reactions:\n  - id: coach\n    on: [session-started]\n    advise:\n      mcp:\n        server: llama\n        tool: status\n        payload_arg: event\n",
			want: `reaction "coach": advise: ` + mcpShapeSentence,
		},
		{
			name: "an mcp mapping spells url: beside mcp:",
			body: "reactions:\n  - id: notify\n    on: [error]\n    run:\n      url: https://example.com/\n      mcp:\n        server: llama\n        tool: status\n",
			want: `reaction "notify": run: ` + mcpShapeSentence,
		},
		{
			name: "mcp: is not a mapping",
			body: "reactions:\n  - id: warden\n    on: [pre-tool-exec]\n    gate:\n      mcp: llama\n",
			want: `reaction "warden": gate: ` + mcpShapeSentence,
		},
		{
			name: "an mcp mapping's args: is not a mapping",
			body: "reactions:\n  - id: coach\n    on: [session-started]\n    advise:\n      mcp:\n        server: llama\n        tool: status\n        args: [verbose]\n",
			want: `reaction "coach": advise: ` + mcpShapeSentence,
		},
		{
			name: "an mcp mapping's tool: is not text",
			body: "reactions:\n  - id: notify\n    on: [error]\n    run:\n      mcp:\n        server: llama\n        tool: [status]\n",
			want: `reaction "notify": run: ` + mcpShapeSentence,
		},
		{
			name: "gate: is a bare string",
			body: "reactions:\n  - id: warden\n    on: [pre-tool-exec]\n    gate: decide\n",
			want: `reaction "warden": gate: is an argv list, a webhook mapping {url:, headers:, headers-env:} or an mcp mapping {mcp: {server:, tool:, args:, payload-arg:}}`,
		},
		{
			name: "gate: is a webhook mapping whose url is not http",
			body: "reactions:\n  - id: warden\n    on: [pre-tool-exec]\n    gate:\n      url: ftp://example.com/\n",
			want: `invalid reaction "warden": gate: url: "ftp://example.com/" must be an absolute http:// or https:// URL`,
		},
		{
			name: "gate: reacts at a Moment it cannot take",
			body: "reactions:\n  - id: warden\n    on: [post-tool-result]\n    gate: [\"decide\"]\n",
			want: `invalid reaction "warden": gate: reacts at pre-tool-exec; "post-tool-result" is not it`,
		},
		{
			name: "the id is a Floor guard's key",
			body: "reactions:\n  - id: tool-use-enforcer\n    on: [error]\n    run: [\"true\"]\n",
			want: `reaction "tool-use-enforcer": that is the Floor guard tool-use-enforcer: — set the top-level key, not a reactions: entry`,
		},
		{
			name: "the id is the context-fill notice's key",
			body: "reactions:\n  - id: context-fill-notice\n    on: [post-tool-result]\n    advise: [\"true\"]\n",
			want: `reaction "context-fill-notice": that is the built-in engine reaction context-fill-notice: — set the top-level key, not a reactions: entry`,
		},
		{
			name: "on: names a seam",
			body: "reactions:\n  - id: shaper\n    on: [pre-request]\n    run: [\"true\"]\n",
			want: `invalid reaction "shaper": run: reacts to notices; "pre-request" is a seam`,
		},
		{
			name: "on: names no moment at all",
			body: "reactions:\n  - id: notify\n    on: [turn-done]\n    run: [\"true\"]\n",
			want: `reaction "notify": unknown reaction event "turn-done"`,
		},
		{
			name: "the entry has no id",
			body: "reactions:\n  - on: [error]\n    run: [\"true\"]\n",
			want: "reactions: an entry has no id: every reaction needs an `id:` to be reported by",
		},
		{
			name: "timeout: is not a duration",
			body: "reactions:\n  - id: notify\n    on: [error]\n    run: [\"true\"]\n    timeout: soon\n",
			want: "reaction \"notify\": timeout: \"soon\" is not a duration — write it as `30s` or `2m`",
		},
		{
			name: "an id is spelled twice",
			body: "reactions:\n  - id: notify\n    on: [error]\n    run: [\"true\"]\n  - id: notify\n    on: [turn-finished]\n    run: [\"true\"]\n",
			want: `reaction "notify": a second entry has this name`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := LoadFileConfig(writeReactionsConfig(t, tt.body), os.ReadFile, noNotify)
			if err == nil {
				t.Fatalf("LoadFileConfig accepted the block; want the refusal %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("refusal = %q; want it to carry %q", err, tt.want)
			}
		})
	}
}

// The ids an entry may not take are the seven Floor-guard config keys, and the list is derived from
// domain.FloorGuards, not from the registry — so this is what keeps it honest: every name in it is
// a real registry key, and a guard key renamed out from under it fails here rather than silently
// letting a `reactions:` entry take the name.
func TestFloorGuardKeysAreRegistryKeys(t *testing.T) {
	t.Parallel()

	if len(floorGuardKeys) != 7 {
		t.Errorf("floorGuardKeys names %d keys, want the seven Floor guards", len(floorGuardKeys))
	}
	for _, key := range floorGuardKeys {
		if _, ok := LookupKey(key); !ok {
			t.Errorf("floorGuardKeys names %q, which is no registry key", key)
		}
	}
	// The notice's key is refused the same way but is NOT a Floor guard, so it is a literal of its
	// own — a registry key still, and kept out of the seven.
	if _, ok := LookupKey(contextFillNoticeKey); !ok {
		t.Errorf("contextFillNoticeKey %q is no registry key", contextFillNoticeKey)
	}
	if slices.Contains(floorGuardKeys, contextFillNoticeKey) {
		t.Errorf("floorGuardKeys names %q; the notice is not a Floor guard", contextFillNoticeKey)
	}
	// The step-budget notice has no key: it is structural, and its retired spelling is the
	// unknown-key walk's exemption (unknownkeys.go), never a registry row.
	if _, ok := LookupKey(stepBudgetNoticeKey); ok {
		t.Errorf("stepBudgetNoticeKey %q is a registry key; the switch is retired", stepBudgetNoticeKey)
	}
}

// spelledCounts is how the prose docs write a small count — "seven always-on guards" — indexed by
// the number it spells.
var spelledCounts = []string{
	"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten",
	"eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen",
	"nineteen", "twenty",
}

// readmeFloorGuardCount matches the README's one sentence that counts the Floor guards: "a *floor*:
// seven always-on guards". A rewording that drops it fails the test below rather than skipping it.
var readmeFloorGuardCount = regexp.MustCompile(`\*floor\*:\s+(\w+) always-on guards`)

// The README counts the Floor guards in prose, and floorGuardKeys is the list that count is of: a
// guard added or retired fails here until the README's number says so.
func TestReadmeStatesTheFloorGuardCount(t *testing.T) {
	t.Parallel()

	const readmePath = "../../README.md"

	readme, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("reading %s: %v", readmePath, err)
	}
	match := readmeFloorGuardCount.FindSubmatch(readme)
	if match == nil {
		t.Fatalf("%s no longer counts the Floor guards as %q", readmePath, readmeFloorGuardCount)
	}
	stated := slices.Index(spelledCounts, string(match[1]))
	if stated < 0 {
		t.Fatalf("%s counts the Floor guards as %q, which is no spelled number", readmePath, match[1])
	}
	if got := len(floorGuardKeys); stated != got {
		t.Errorf("%s states %s Floor guards; floorGuardKeys names %d", readmePath, match[1], got)
	}
}

// manualFloorGuardCount matches a manual page's spelled count of the Floor guards wherever it sits —
// "the seven Floor guards", "one of the seven Floor-guard keys", "the seven top-level Floor-guard
// keys". A word before "Floor guard" that spells no number ("the Floor guards") is not a count.
var manualFloorGuardCount = regexp.MustCompile(`(?i)\b(\w+)\s+(?:top-level\s+)?floor[- ]guard`)

// The manual pages count the Floor guards in prose as the README does, several times each, and
// every count must be the one floorGuardKeys holds. A page that stops counting fails rather than
// passing silently, so a page dropped from the list is a decision, not an accident.
func TestManualStatesTheFloorGuardCount(t *testing.T) {
	t.Parallel()

	pages := []string{
		"../../docs/manual/configuration.md",
		"../../docs/manual/daemon.md",
		"../../docs/manual/reactions.md",
	}
	for _, page := range pages {
		doc, err := os.ReadFile(page)
		if err != nil {
			t.Fatalf("reading %s: %v", page, err)
		}
		counted := 0
		for _, match := range manualFloorGuardCount.FindAllSubmatch(doc, -1) {
			stated := slices.Index(spelledCounts, strings.ToLower(string(match[1])))
			if stated < 0 {
				continue
			}
			counted++
			if got := len(floorGuardKeys); stated != got {
				t.Errorf("%s states %q; floorGuardKeys names %d", page, match[0], got)
			}
		}
		if counted == 0 {
			t.Errorf("%s no longer counts the Floor guards as %q", page, manualFloorGuardCount)
		}
	}
}

// The names a root keeps the `terminal` tool away from: every variable a webhook header is read
// from — never an argv's or an MCP call's own text — each once, in a stable order — the values come off a map, so an unsorted answer would
// reorder between runs and make every caller's own output unstable.
func TestReactionEnvNamesDeduplicatesAndSorts(t *testing.T) {
	t.Parallel()

	opts := Options{Reactions: []domain.Reaction{
		{ID: "bell", Handler: domain.WebhookHandler{
			HeadersEnv: map[string]string{"Authorization": "TOKEN_B", "X-Trace": "TOKEN_A"},
		}},
		{ID: "page", Handler: domain.WebhookHandler{
			HeadersEnv: map[string]string{"Authorization": "TOKEN_B"},
		}},
		{ID: "quiet", Handler: domain.WebhookHandler{
			HeadersEnv: map[string]string{"X-Blank": "  "},
		}},
		{ID: "run", Handler: domain.ArgvHandler{Argv: []string{"true"}}},
		{ID: "status", Handler: domain.MCPHandler{
			Server: "llama", Tool: "status", Args: map[string]any{"token": "TOKEN_C"},
		}},
	}}

	got := ReactionEnvNames(opts)

	want := []string{"TOKEN_A", "TOKEN_B"}
	if len(got) != len(want) {
		t.Fatalf("ReactionEnvNames = %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ReactionEnvNames = %v; want %v", got, want)
		}
	}
}
