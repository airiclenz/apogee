# Plan — The configurable system prompt

**Date:** 2026-07-26
**Status:** READY (grilled with the owner 2026-07-26 — see *Owner decisions*; the mechanical
design below is grounded against the working tree).
**Source:** owner request 2026-07-26, closing the parked "General system-prompt / template
story" (`TODO.md:219-243`, parked 2026-07-02 by the prompt-seam grill's scope guard).
**Track:** post-`v0.8.5`. This plan lands one `### Added` CHANGELOG entry under
`## [Unreleased]` and rides whatever minor cut ships next (`Config` gains one **additive**
field ⇒ a **minor** bump; the plan itself bumps nothing).
**Public API:** `apogee.Config` (= `domain.Config`, aliased) gains ONE additive field,
`SystemPrompt string`. Everything else lands in a new `internal/prompt` package,
`internal/agent`, and `cmd/apogee`. No exported name changes (ADR 0010).
**Standing requirement:** `/coding-standards` (Go + testing variants) mandatory — invoke
`implement-plan` with `coding-standards` forwarded. Pre-production: commit direct to `main`,
no PRs (owner directive).

Per-item green gate:

```
gofmt -l .                                              # empty
make check                                              # vet + lint + go test -race -count=1 ./...
GOOS=windows go build ./... && GOOS=darwin go build ./...
```

**Dependencies.** Strictly linear: 1 → 2 → 3 → 4 → 5. Item 1 is the foundation (the template
language); 2 puts it on the wire behind a Config field no config file sets yet; 3 plumbs the
config keys and the per-model resolution; 4 flips the shipped default active (the deliberate
invariant break); 5 is the documentation close-out. `/implement-plan` may stop after any
completed item and the tree is coherent (after 2 the feature exists for Go embedders only;
after 3 it exists for config users with no default; after 4 fresh installs get the prompt).

**Deviations leave a trail.** Any authorized deviation from an item's text must land as a dated
`NOTES:` line under that item's heading in this file, per the sub-agent templates.

**Authoritative sources**, in precedence order, for every item:

1. This document.
2. `internal/agent/promptseam_test.go:104-129` — `TestPromptSeam_NativeProfileByteIdentical`,
   the native byte-identical anchor. It must pass UNTOUCHED through every item.
3. `internal/agent/wire.go:62-82` — `injectSystemInstructions`: the tool-instruction block
   appends to the FIRST system message when one exists; `internal/domain/hooks.go` —
   `AppendToSystem` / `appendOrCreateSystem`: mechanism directives do the same. These two seams
   are what make "one merged system message" fall out of seeding position 0.
4. `cmd/apogee/config.go` — the `present:` block (`presentConfig` → `toPresentSettings` →
   `layer` → `settings` → `applyConfig`/`validate`) is the precedent the new keys copy exactly.
5. `docs/plans/archived/prompt-seam-wiring-plan.md` — the scope guard this plan closes out, and
   the origin of the native anchor.

---

## Owner decisions (grill, 2026-07-26)

- **Active default**: the shipped starter template (`cmd/apogee/defaults/config.yaml`) carries
  an ACTIVE (uncommented) default prompt — fresh installs get it; existing seeded configs (key
  absent) keep today's zero-byte, prompt-less behaviour. No compiled-in fallback.
- **Keys**: top-level, file-only — `system-prompt-text:` (inline), `system-prompt-file:`
  (path), `system-prompt-models:` (map resolved-model-name → entry reusing the same two key
  spellings). The inner text key is named `system-prompt-text`, NOT bare `text` (owner).
- **Templating**: exactly three placeholders — `{{workspace}}`, `{{datetime}}`, `{{mode}}` —
  strict spelling, unknown placeholder = startup error listing the known three.
- **`{{datetime}}` is date-only** (`2026-07-26`), re-rendered per request — KV-cache-stable
  within a day on local llama.cpp servers (a per-request timestamp would bust the prefix cache
  every turn).
- **Per-model override**: a matching `system-prompt-models:` entry REPLACES the global prompt
  entirely; a non-matching key is inert (the `unconfined-hosts` posture). Matching is exact
  string equality against the RESOLVED model name (the label Validated sets key on).
- **Sub-agents inherit** the prompt (falls out of `newChildAgent`'s wholesale cfg copy).
- **Default text**: minimal persona + context (~5 lines, below, verbatim) — small local models
  are the fresh-install audience and every system token competes with the task.

---

## The ground (verified 2026-07-26 against the working tree)

**Apogee has no built-in system prompt.** A request is built in
`internal/agent/loop.go:554` (`buildRequest`): `domain.NewRequest(a.cfg.Model,
a.conv.Messages(), a.toolMenu(), a.budget(), turn, a.tracker.fireCounts)` — the conversation
starts at the first user message. The only text ever injected into the system channel today is
(a) the non-native profile's tool menu + emission instructions, folded in at the WIRE seam
(`internal/agent/wire.go:35-38` → `injectSystemInstructions`), and (b) mechanism directives via
`Request.AppendToSystem` (`internal/domain/hooks.go`), which appends to the first system
message or creates one at position 0 (`appendOrCreateSystem`). `promptseam_test.go:259`
(`seedingHook`) already demonstrates a PreRequest hook seeding a system message, and
`TestPromptSeam_AppendsToSeededSystemMessage` (`:134`) pins that the tool block then MERGES
into it rather than adding a second system message.

**Request projections never enter history.** The `domain.Request` is rebuilt from
`a.conv.Messages()` per Turn (`armRequest`, `loop.go:206`) and re-derived after an overflow
fold (`refold` → `armRequest`), so anything seeded into it exists per-request only —
`TestPromptSeam_InjectedTextNeverEntersHistoryOrSnapshot` (`promptseam_test.go:222`) is the
pattern that pins this. The snapshot serializes only conversation + loop counters; `cfg` is
never serialized.

**The internal prompts are separate request paths.** The compaction summariser builds its OWN
message list around `summaryInstruction` (`internal/context/compact.go:92`) inside
`compactCompleter.Complete` — it constructs its own `domain.NewRequest` with its own messages
and nil tools, never calling `buildRequest`. The probe battery builds raw `provider.Request`s
around `batterySystemPrompt` (`internal/probe/battery.go`), never touching the Agent at all.
Seeding in `buildRequest` therefore cannot leak into either — but item 2 pins the compaction
half with a test anyway.

**Sub-agents copy the parent's cfg wholesale.** `newChildAgent`
(`internal/agent/subagent.go:109`) does `childCfg := a.cfg` and then overrides only
Mode / ConfineToWorkspace / Tools / Mechanisms / EnableMechanisms — a `Config.SystemPrompt`
field inherits with **no carve-out needed** (verified; item 2 pins it).

**Model resolution precedes `runRoot`.** `root.go` (RunE): `applyConfig` resolves the file
layer, then `resolveModel` fills `opts.model` from discovery when unset — so by the time
`runRoot` assembles `apogee.Config` (`cmd/apogee/wire.go:164-196`), `opts.model` IS the
resolved model name the Validated-set surface also keys on. Per-model prompt selection belongs
in `runRoot`, after that point.

**The template invariant this plan deliberately breaks.** `cmd/apogee/defaults/config.yaml:1-11`
promises "Every line below is commented out, so out of the box this file changes nothing", and
`TestEmbeddedDefaultConfigIsNeutral` (`cmd/apogee/defaults_test.go:57`) enforces it
(`reflect.DeepEqual(l, layer{})`). Item 4 amends both — owner-approved. Existing seeded configs
keep today's behaviour: `seedConfig` never overwrites (`TestSeedConfigDoesNotOverwrite`,
`defaults_test.go:34`).

---

## Decisions taken (mechanical — grounded, with rationale)

**(a) The template code lives in a new `internal/prompt` package.** Not `internal/domain`:
domain is declarative data + loop-facing working values; a template language (scanning,
substitution, a date format) is a processing concern, and domain must not grow one (the
`ModelProfile` precedent — domain keeps the profile as DATA and delegates parsing to
`internal/processing`). Not `internal/processing` either: that package is the model profile's
parse/emit seam (its vocabulary is tool-call formats and thinking channels), while the
system-prompt template is a host/config concern with different consumers (`cmd/apogee`
validates at startup, `internal/agent` renders per request) — one concern per package.
`internal/prompt` imports stdlib only (not even domain: `Inputs.Mode` is a plain string), so
both consumers use it with no cycle.

**(b) The seeding point is `buildRequest` (`loop.go:554`).** The rendered prompt is prepended
as a `RoleSystem` message to the projection slice BEFORE `domain.NewRequest` is built.
Rationale, against the alternatives:
- *Not a construction-time PreRequest hook*: hooks dispatch catalogued-first, so a catalogued
  Mechanism's `AppendToSystem` would create the system message before a seeding hook ran,
  putting mechanism directives AHEAD of the prompt; and the prompt is structural config, not a
  Mechanism — it must apply under `--bypass` too, which Mechanism plumbing would not guarantee.
- *Not the wire seam (`toProviderRequest`)*: mechanism `AppendToSystem` would then CREATE its
  own system message in the domain Request and the wire seam would need a second merge pass to
  get the order right; pre-request hooks and the predictive overflow guard
  (`requestExceedsWindow`, via `req.State()`) would not see the prompt, so the Budget estimate
  would lie by the prompt's size.
- `buildRequest` gives every required property for free: `AppendToSystem` finds the prompt at
  index 0 and appends after it, the wire seam's `injectSystemInstructions` appends the tool
  block after THAT — the required order **prompt → mechanism directives → tool block** in ONE
  system message; the projection is per-request so nothing enters history or the snapshot;
  `armRequest`/`refold` re-call it so the render is fresh per request (and per overflow-fold
  retry). `loopView()` (the tool-stage hook view) stays UNSEEDED — it is documented as "the
  conversation so far", and the profile tool block is likewise absent from it today.

**(c) `domain.Config` carries the TEMPLATE, the Agent supplies the render inputs.**
`Config.SystemPrompt string` is the validated template, never a rendered string — rendering per
request needs live inputs, and all three already live on the Agent: workspace =
`cfg.WorkspaceDir`, mode = `a.Mode()` (the live, lock-guarded accessor — a Shift+Tab mode
switch lands on the next request, and a sub-agent renders its own inherited mode), date = a new
`now func() time.Time` field on `Agent` seeded to `time.Now` in `newAgent` (the
injectable-field-for-tests shape copies `sessionHost.now` in `cmd/apogee/wire.go`). No
render-provider interface on Config: it would be a public seam with exactly one implementation.

**Decided details** (each restated in its owning item):
- `{{datetime}}` renders the **local date only**, `Format("2006-01-02")`. `{{mode}}` =
  `string(a.Mode())` (`plan` / `ask-before` / `allow-edits` / `auto`). `{{workspace}}` =
  `Config.WorkspaceDir`.
- Placeholder spelling is **strict**: anything matching `\{\{[^{}]*\}\}` that is not exactly
  one of the three known spellings — including `{{ workspace }}` with spaces — is an unknown
  placeholder, error listing the known three. Unclosed/stray braces pass through verbatim.
- `system-prompt-file:` — `~`/`~/` expands via `os.UserHomeDir`; a relative path resolves
  against the **apogee home** (the directory `config.yaml` itself lives in), NOT the workspace:
  the key lives in a global file that travels with the home, and a workspace base would break
  one global config across projects.
- Validation split: **both-text-and-file at any level** is a structural, machine-independent
  schema error → validated for ALL levels at `applyConfig` time. **File readability and
  placeholder validity** are validated only for the SELECTED source (global, or the matching
  model entry) — a non-matching `system-prompt-models:` entry is inert like a non-matching
  `unconfined-hosts` entry, whose file may only exist on another machine.
- A `system-prompt-models:` entry that sets NEITHER key is a startup error naming the model key
  (a silently-empty entry is far more likely a YAML indentation mistake than intent).
- Defense in depth: `newAgent` also validates `cfg.SystemPrompt` via `prompt.Validate`
  (mirroring the `processing.ParserFor` construction gate), so an embedder setting a bad
  template fails construction loudly; the cmd-side validation fires first for config users,
  with the config key named in the message.

---

## 1. `internal/prompt` — the three-placeholder template language — ✅ DONE (2026-07-27)

**What.** New package `internal/prompt` (files `prompt.go`, `prompt_test.go`). Stdlib-only
imports. The whole language: three placeholders, strict validation, per-request substitution.

```go
// Package prompt owns the system-prompt template language (ADR 0023): exactly three
// placeholders — {{workspace}}, {{datetime}}, {{mode}} — validated once at startup /
// construction and rendered fresh per request by the loop.
package prompt

const (
    PlaceholderWorkspace = "{{workspace}}"
    PlaceholderDatetime  = "{{datetime}}"
    PlaceholderMode      = "{{mode}}"
)

// KnownList is the known placeholders as one comma-joined string, for error messages —
// "{{workspace}}, {{datetime}}, {{mode}}". One source so cmd/apogee and the engine
// error identically.
func KnownList() string

// Inputs are the per-request render inputs. Mode is a plain string (the autonomy mode
// label) so this package imports no domain type.
type Inputs struct {
    Workspace string    // Config.WorkspaceDir
    Mode      string    // string(agent.Mode()) — live, changes on a mode switch
    Now       time.Time // rendered DATE-ONLY (2006-01-02): KV-cache-stable within a day
}

// Validate rejects a template carrying an unknown {{...}} placeholder, listing the known
// three. Spelling is strict ({{ workspace }} is unknown). "" validates: no prompt.
func Validate(template string) error

// Render substitutes the three placeholders (every occurrence). Assumes Validate passed;
// an unknown placeholder — unreachable after validation — passes through verbatim (the
// InstructionsFor defensive-degrade posture).
func Render(template string, in Inputs) string
```

Implementation notes: a package-level `placeholderPattern` regexp matching `\{\{[^{}]*\}\}` drives
Validate (FindAllString, reject any token not in the known set, name the first offender AND the
known list in one error); Render is three `strings.ReplaceAll` calls plus
`in.Now.Format("2006-01-02")`. Keep the error text exactly:
`prompt: unknown placeholder %q; known placeholders: %s` (KnownList) — item 3 wraps it with the
config key name.

**Tests** (`internal/prompt/prompt_test.go`, table style like `cmd/apogee/config_test.go`):
- `TestRenderSubstitutesAllPlaceholders` — a template using all three (one repeated) renders
  workspace, the date-only stamp, and the mode label; repeated occurrences all substitute.
- `TestRenderDateIsDateOnly` — a fixed `time.Date(2026, 7, 26, 23, 59, …)` renders
  `2026-07-26` with no time-of-day.
- `TestValidateAcceptsKnownAndEmpty` — each known placeholder alone, all three together, a
  placeholder-free template, and `""` all validate; literal braces / an unclosed `{{` validate
  (not placeholders).
- `TestValidateRejectsUnknownListingKnown` — `{{foo}}`, `{{ workspace }}`, `{{WORKSPACE}}`
  each error; the message contains the offending token and all three known spellings.

**Acceptance.** The green gate. `go test ./internal/prompt/` green. No import of
`internal/domain` (check `go list -deps`).

**commit.** `feat(prompt): a three-placeholder template language for the system prompt`

---

## 2. Engine — `Config.SystemPrompt` seeds the first system message of every request

**What.** The additive Config field, the `buildRequest` seeding, construction-time validation,
and the `now` seam. Files: `internal/domain/config.go`, `internal/agent/agent.go`,
`internal/agent/construct.go`, `internal/agent/loop.go`.

- `internal/domain/config.go` — on `Config`, after `Profile`:

```go
// SystemPrompt is the system-prompt TEMPLATE (ADR 0023) — internal/prompt's three
// placeholders ({{workspace}}, {{datetime}}, {{mode}}) — rendered FRESH per request by
// the loop and seeded as the first system message of the wire request. It is a template,
// not a rendered string, because two inputs are live (the date, the autonomy mode). It is
// request-scoped only: never committed to history, never in the snapshot. "" (the
// default) seeds nothing — the byte-identical no-prompt anchor. The host folds a
// configured template in from config.yaml (file-only); an embedder sets it directly. An
// unknown placeholder fails construction (New/Resume), like a bad Profile.
SystemPrompt string
```

  (No `apogee.go` change: `Config` is aliased, the field is public automatically.)

- `internal/agent/agent.go` — on `Agent` (beside the other loop fields):
  `now func() time.Time // request-render clock; time.Now in production, fixed in tests (the sessionHost.now shape)`.

- `internal/agent/construct.go` — in `newAgent`: seed `now: time.Now` in the struct literal,
  and beside the `ParserFor` construction gate:

```go
// A bad system-prompt template fails construction loudly (like a bad profile above),
// so an embedder typo never silently ships an un-rendered placeholder to the model.
if err := prompt.Validate(cfg.SystemPrompt); err != nil {
    return nil, err
}
```

- `internal/agent/loop.go` — `buildRequest` (L554) grows the seed, plus one new method:

```go
func (a *Agent) buildRequest(turn int) (*domain.Request, []string) {
    msgs := a.conv.Messages()
    // The configured system prompt (ADR 0023) is seeded at position 0 of the REQUEST
    // projection — never the conversation — so it is re-rendered per request (armRequest,
    // and refold after an overflow fold), stays out of history and the snapshot, and both
    // AppendToSystem (mechanism directives) and the wire seam's tool-instruction block
    // fold into THIS one message (prompt → directives → tool block). "" seeds nothing:
    // the no-prompt native anchor stays byte-identical.
    if sys := a.systemPrompt(); sys != "" {
        msgs = append([]domain.Message{{Role: domain.RoleSystem, Content: sys}}, msgs...)
    }
    req := domain.NewRequest(a.cfg.Model, msgs, a.toolMenu(), a.budget(), turn, a.tracker.fireCounts)
    …unchanged…
}

// systemPrompt renders this request's system prompt from the configured template, or ""
// when none is configured. The inputs are live where the placeholders demand it: the mode
// through the lock-guarded Mode() (a Shift+Tab lands on the next request; a sub-agent
// renders its own inherited mode), the date from a.now (date-only — stable within a day,
// so the KV cache holds), the workspace from Config.
func (a *Agent) systemPrompt() string {
    if a.cfg.SystemPrompt == "" {
        return ""
    }
    return prompt.Render(a.cfg.SystemPrompt, prompt.Inputs{
        Workspace: a.cfg.WorkspaceDir,
        Mode:      string(a.Mode()),
        Now:       a.now(),
    })
}
```

  `loopView()` is deliberately NOT seeded — it is "the conversation so far" for tool-stage
  hooks, and the profile tool block is likewise absent from it today. Note in its doc comment
  that the seeded prompt is a request-projection concern (`buildRequest`), not a view one.

**Deliberate consequences to note in code comments, not fight:** the Budget's predictive guard
and calibration now measure the prompt too (`req.State()` includes it) — honest accounting;
post-response scanners' `req.View()` sees a leading system message when a prompt is configured
— the shape `seedingHook` already exercised. `newChildAgent` needs **no change**:
`childCfg := a.cfg` carries `SystemPrompt`. The compaction summariser builds its own request
and never calls `buildRequest`; the probe battery never touches the Agent — both stay
prompt-free by construction.

**Tests.** In `internal/agent/promptseam_test.go` (same harness: `menuConfig`,
`newProfileAgent`, `recordingResponder`; set `a.now = func() time.Time { return fixed }` after
construction where the date is asserted):
- `TestPromptSeam_ConfiguredPromptNativeSingleSystemMessage` — `cfg.SystemPrompt` set (template
  using all three placeholders), zero profile: the wire request has EXACTLY one system message,
  at position 0, content == `prompt.Render(...)` with the fixed date,
  `string(domain.ModeAskBefore)`, and the workspace; `got.Tools` still carries the native
  `read_file` spec (array intact — the prompt does not trip the non-native suppression).
- `TestPromptSeam_ConfiguredPromptMergesDirectivesAndToolBlock` — prompt + markdown-fenced
  profile + a `seedingHook`-style experimental PreRequest hook calling
  `req.AppendToSystem("[directive]", …)`: exactly ONE wire system message whose content is
  `rendered + "\n\n" + directive + "\n\n" + block` (block via `processing.InstructionsFor`) —
  prompt first, mechanism directives second, tool block last.
- `TestPromptSeam_ConfiguredPromptRendersFreshPerRequest` — two Turns: between them swap
  `a.now` to the next day AND `a.SetMode(domain.ModePlan)`; Turn 2's system message carries the
  new date and `plan` while Turn 1's carried the old pair (pins "re-rendered per request",
  both live inputs).
- `TestPromptSeam_ConfiguredPromptNeverEntersHistoryOrSnapshot` — a distinctive marker in the
  template; after a Step, no committed message is `RoleSystem` and none contains the marker;
  the encoded snapshot does not contain it.
- `TestNewAgentRejectsUnknownPromptPlaceholder` — `cfg.SystemPrompt = "hi {{foo}}"` fails
  `newAgent` with an error naming `{{foo}}` and listing the known three.
- `TestPromptSeam_NativeProfileByteIdentical` (`:106`) passes **untouched** — the anchor.

In `internal/agent/subagent_test.go`:
- `TestSubAgentInheritsSystemPrompt` — parent constructed with `cfg.SystemPrompt` set;
  `a.newChildAgent()` succeeds and `child.cfg.SystemPrompt == a.cfg.SystemPrompt`.

In `internal/agent/compact_test.go`:
- `TestCompactSummaryRequestOmitsSystemPrompt` — Agent with `cfg.SystemPrompt` set (marker
  text) and a `recordingResponder`; after `a.Compact(ctx)` the recorded summary request's first
  message content starts with the `summaryInstruction` text and NO message contains the marker.

**Acceptance.** The green gate. Every existing test in `internal/agent` passes untouched — in
particular `promptseam_test.go:106` and the overflow/predictive-guard suites (the prompt is
absent whenever `SystemPrompt == ""`, which is every existing test).

**commit.** `feat(agent): Config.SystemPrompt seeds the first system message of every request`

---

## 3. Config plumbing — the three file-only keys and post-resolution selection

**What.** The `present:`-precedent plumbing end to end, plus the per-model selection helper the
composition root calls AFTER model resolution. All in `cmd/apogee` (files: `config.go`,
`root.go`, `wire.go`, `defaults/config.yaml`).

| Step | File | Change |
|---|---|---|
| on-disk template | `cmd/apogee/defaults/config.yaml` | a new FULLY-COMMENTED block after the `mode:` block (~L38), in the house prose style: the three keys, that they are config-file-only (no flag/env), the three placeholders (and that an unknown one is a startup error), that `system-prompt-file` expands `~` and resolves a relative path against the apogee home, that a `system-prompt-models:` entry REPLACES the global prompt entirely for that model and a non-matching entry is inert, and that text+file at one level is an error. Commented in THIS item — item 4 activates the text key |
| parse | `config.go` `fileConfig` (after `Present`) | `SystemPromptText string` (yaml `system-prompt-text`), `SystemPromptFile string` (yaml `system-prompt-file`), `SystemPromptModels map[string]systemPromptEntryConfig` (yaml `system-prompt-models`); new type `systemPromptEntryConfig` whose two fields reuse the SAME yaml key spellings (owner decision: the inner text key is `system-prompt-text`, not `text`) |
| resolved shape | `config.go` (beside `presentSettings`) | `type promptSource struct { text, file string }`; `type systemPromptSettings struct { global promptSource; models map[string]promptSource }`; `toSystemPromptSettings()` on the fileConfig fields (the `toPresentSettings` shape) |
| layer | `config.go` `layer` | `systemPrompt *systemPromptSettings` — file-only, comment per the `present` field's; projected in `fc.layer()` when ANY of the three keys is non-empty |
| resolve | `config.go` `resolveSettings` (beside the `file.present` branch) | `systemPrompt systemPromptSettings` on `settings`; `if file.systemPrompt != nil { s.systemPrompt = *file.systemPrompt }` (zero default = no prompt) |
| validate | `config.go` (`systemPromptSettings.validate()`; called in `applyConfig` beside `s.present.validate()`) | the STRUCTURAL check for every level: global text+file both set → error naming both keys; each `system-prompt-models` entry with both set → error naming the model key; an entry with NEITHER set → error naming the model key ("sets neither system-prompt-text nor system-prompt-file"). Machine-independent schema defects only — file readability and placeholders are the selected-source resolver's job |
| opts | `root.go` `options` (after `present`) | `systemPrompt systemPromptSettings` with the house comment; write-back in `applyConfig`: `opts.systemPrompt = s.systemPrompt` |
| selection | `config.go`, new section beside "Model discovery" | `func resolveSystemPrompt(sp systemPromptSettings, model, home string, readFile func(string) ([]byte, error)) (string, error)` — (1) `src := sp.global`; if `m, ok := sp.models[model]; ok { src = m }` (exact match on the RESOLVED name; a matching entry REPLACES the global entirely, even matching-with-only-file); (2) if `src.file != ""`: expand `~`/`~/` via `os.UserHomeDir`, resolve a relative remainder against `home` (`filepath.Join`), `readFile` — any error is a startup error naming the key and path; else the template is `src.text`; (3) `prompt.Validate(template)`, wrapping the error with the source key (`system-prompt-text`, `system-prompt-file %q`, or `system-prompt-models[%q]`); (4) return the template. A helper `expandUserPath(p string) (string, error)` beside it |
| composition root | `wire.go` `runRoot` (immediately before the `apogee.Config` literal, L164-196) | `sysPrompt, err := resolveSystemPrompt(opts.systemPrompt, opts.model, roots.config, os.ReadFile); if err != nil { return err }` — POST-model-resolution by construction (`opts.model` was resolved in root.go before `runRoot`); then `SystemPrompt: sysPrompt,` in the literal (beside `Profile:`, with a one-line comment pointing at ADR 0023) |

**Tests.**
- `cmd/apogee/config_test.go`:
  - `TestApplyConfigSystemPrompt` (the `TestApplyConfigMechanisms` shape) — a file layer with
    `system-prompt-text` reaches `opts.systemPrompt.global.text`; a file with all three keys
    populates all fields; an absent block leaves the zero value.
  - `TestSystemPromptSettingsValidate` — table: text only OK; file only OK; both → error
    naming both keys; a models entry both-set → error naming the model key; a models entry
    neither-set → error; a well-formed models entry beside a global text → OK.
  - `TestResolveSystemPrompt` — table: global text selected; matching model entry (text)
    REPLACES a global text entirely; matching entry with only `file` set replaces a global
    TEXT (whole-entry replacement); non-matching entry inert (global wins; the entry's
    nonexistent file is never read); no prompt anywhere → `""`; file read via injected
    `readFile` (absolute path); relative path resolves against `home`; `~/`-prefixed path
    resolves against `t.Setenv("HOME", …)` (not parallel); unreadable selected file → error
    naming the key and path; unknown placeholder in the selected template → error naming the
    source key and listing the three known placeholders.
- `cmd/apogee/wire_test.go`:
  - `TestRunRootSystemPromptResolutionFails` (the `TestRunRootThreadsContextWindow` harness) —
    `opts.systemPrompt` with an unreadable global file → `runRoot` returns the resolution error
    before launch (proves the call site is wired); a second case with `{{bogus}}` in text → the
    placeholder error surfaces.
- `cmd/apogee/defaults_test.go`: `TestEmbeddedDefaultConfigIsNeutral` passes UNCHANGED in this
  item (the block is fully commented until item 4).

**Acceptance.** The green gate. A manual run with `system-prompt-text: "hi {{workspace}}"` in
`~/.apogee/config.yaml` sends the rendered line as the first system message (observe via the
server log); removing the key restores the promptless request.

**commit.** `feat(config): file-only system-prompt keys with a per-model override`

---

## 4. Ship the default prompt — the template's one active setting

**What.** Activate the owner-approved default in the embedded starter template and amend the
invariant that forbade it. Files: `cmd/apogee/defaults/config.yaml`,
`cmd/apogee/defaults_test.go`.

- In the block item 3 added, make the text key ACTIVE with EXACTLY this owner-approved text
  (verbatim — do not re-word, re-wrap, or "improve" it):

```yaml
system-prompt-text: |
  You are apogee, a coding agent working in the workspace at {{workspace}}.
  Today's date is {{datetime}}. You are operating in {{mode}} mode.

  Work step by step: read before you change, keep edits small and focused,
  and verify your work. Be direct and concise; when a task is done,
  summarise what changed.
```

  The surrounding prose states: this is the ONE active setting in the template; delete it or
  comment it out to run with no system prompt (the pre-0.9 behaviour); `system-prompt-file:`
  and `system-prompt-models:` stay commented examples beside it.
- Amend the header prose (`defaults/config.yaml:9-11`): "Every line below is commented out…"
  becomes wording to the effect of "Every line below is commented out — with ONE exception,
  `system-prompt-text:`, the default system prompt — so out of the box this file changes
  nothing else. Uncomment a line and edit it to make it your default; delete or comment out
  the system prompt to send none." Keep the precedence lines 6-8 untouched.
- `defaults_test.go`: REPLACE `TestEmbeddedDefaultConfigIsNeutral` (L57) with
  `TestEmbeddedDefaultConfigSetsOnlyTheSystemPrompt`: parse the embedded template; assert
  `l.systemPrompt != nil`, `l.systemPrompt.global.text` contains all three placeholder
  spellings and the literal "You are apogee", `global.file == ""`, `models == nil`; assert
  `prompt.Validate(l.systemPrompt.global.text) == nil` and
  `l.systemPrompt.validate() == nil`; then `l.systemPrompt = nil` and
  `reflect.DeepEqual(l, layer{})` — every OTHER key still parses to nothing (the surviving
  half of the old invariant, now stated precisely). The seeding tests
  (`TestSeedConfigCreatesWhenAbsent`, `TestSeedConfigDoesNotOverwrite`) stand untouched.

**Tests.** The amended `TestEmbeddedDefaultConfigSetsOnlyTheSystemPrompt` above (this item's
whole test surface).

**Acceptance.** The green gate. `rm -rf` a scratch `--config` home, run once: the seeded
`config.yaml` carries the active prompt and the session's first request opens with the
rendered default (three placeholders substituted). An EXISTING config without the key:
request unchanged, zero bytes added.

**commit.** `feat(config): ship a default system prompt in the starter template`

---

## 5. Documentation close-out — ADR 0023, CHANGELOG, README, CONTEXT, TODO

**What.** The cross-cutting docs this feature owes, under one owning item:

- **ADR** — new
  `docs/adr/0023-the-system-prompt-is-a-configured-template-rendered-per-request.md`, house
  style (the ADR 0019 shape). Context: the deliberate "no built-in system prompt" stance
  (prompt-seam plan scope guard, the parked TODO story) and why it ends now. Decision:
  file-only config surface (three top-level keys; per-model map keyed on the RESOLVED model
  name, whole-entry replacement, inert non-match); exactly three placeholders, strict
  spelling, date-only datetime; template-not-rendered-string on `Config.SystemPrompt` with
  live per-request render inputs; seeding at `buildRequest` position 0 so mechanism directives
  and the profile tool block fold into ONE system message (prompt → directives → tool block);
  request-scoped only (never history, never snapshot); sub-agents inherit via the wholesale
  cfg copy; the compaction summariser and probe battery keep their dedicated prompts; the
  native no-prompt anchor holds byte-identical; the shipped default deliberately ends the
  template's "parses to an empty layer" invariant. Considered options: the rejected seeding
  points from this plan's decision (b), and `internal/processing`/domain from (a).
  Consequences: additive minor; the host-override knob for the rendered instruction block
  (D1's rejected hybrid) remains additive-later and composes with this (both fold into the
  same first system message).
- **CHANGELOG** — one `### Added` bullet under `## [Unreleased]`, house voice: configurable
  system prompt, the three keys, the three placeholders, per-model override, the shipped
  default (and that deleting the key restores promptless behaviour), and the Go-API note:
  `apogee.Config.SystemPrompt` is additive ⇒ minor bump.
- **README** — in `## Configuration`: add the three keys to the file-only enumeration and a
  short paragraph + yaml example: the placeholders, the per-model replacement, the shipped
  default, how to turn it off.
- **CONTEXT.md** — one new term: **System prompt** — the configured template, rendered per
  request, first system message on the wire, never in history or the snapshot; distinct from
  the summariser's and battery's dedicated prompts.
- **TODO.md** — rewrite `## General system-prompt / template story` (L219-243) to the CLOSED
  shape of the neighbouring closed entries: status line `CLOSED 2026-07-26 (this plan,
  ADR 0023)`, what shipped, and one residual note: the host-override knob for the rendered
  instruction block stays deliberately unbuilt (additive later; it composes — same merged
  system message).

**Tests.** None — documentation only.

**Acceptance.** The green gate. `ls docs/adr/` shows 0023 present and consecutive, links
resolve, `grep -n "parked" TODO.md` no longer matches the system-prompt story, and the
CHANGELOG entry sits under `[Unreleased]`.

**commit.** `docs(prompt): ADR 0023 and the system-prompt close-out (changelog, README, CONTEXT, TODO)`

---

## Explicitly NOT in this plan

- **The host-override knob for the rendered tool-instruction block** (D1's rejected hybrid,
  `TODO.md:235-238`) — stays additive-later; this plan's seeding composes with it (same merged
  first system message), which is exactly what the parked note required. Do not build it here.
- **Per-capability-tier prompt shortening** (ADR 0021's parked Mechanism-shaped idea) — the
  `system-prompt-models:` map is the manual per-model lever; automatic tiering stays parked.
- **A flag or env var** (`--system-prompt` / `APOGEE_SYSTEM_PROMPT`) — the keys are file-only,
  per the newer-key convention (`present:`, `mechanisms:`, `model-profile:`).
- **A `{{tools_block}}` placeholder or any new placeholder** beyond the three — the tool block
  stays engine-owned and auto-appended; the placeholder set is closed by owner decision.
- **A project-level config file** — none exists (`cmd/apogee/config.go` notes this
  deliberately); the prompt is global-config-only like everything else.
- **Prompting the internal request paths** — the compaction summariser and the probe battery
  keep their own dedicated prompts; the configured prompt must never reach them.

## Critical files

- `internal/prompt/prompt.go` (NEW) — the template language (validate + render)
- `internal/agent/loop.go` — `buildRequest` (L554), the seeding point; `systemPrompt()` (new)
- `internal/domain/config.go` — the `Config.SystemPrompt` carrier field
- `internal/agent/construct.go` — the construction-time `prompt.Validate` gate; `now` seeding
- `cmd/apogee/config.go` — fileConfig/layer/settings/validate plumbing + `resolveSystemPrompt`
- `cmd/apogee/wire.go` — `runRoot`'s post-model-resolution wiring site (L164-196)
- `cmd/apogee/defaults/config.yaml` + `cmd/apogee/defaults_test.go` — the starter template that
  goes one-key-active (item 4's deliberate invariant amendment)
- `internal/agent/promptseam_test.go` — the native anchor (L106) and the harness the new tests
  extend

---

## Verification (whole plan)

1. **Per item**, the green gate at the top of this document.
2. **Targeted:**
   `go test ./internal/prompt/ ./internal/agent/ -run 'Prompt|SystemPrompt|Compact|SubAgent'`
   and `go test ./cmd/apogee/ -run 'SystemPrompt|Defaults|Embedded|ApplyConfig|RunRoot'`.
3. **The anchor, explicitly:**
   `go test ./internal/agent/ -run TestPromptSeam_NativeProfileByteIdentical -count=1` — with
   no prompt configured and a zero/native profile the wire request carries no system message
   and the native tools array, byte-identical. This test must never have been edited by any
   item.
4. **Live, against a real server** (`go run ./cmd/apogee` on a fresh `--config` home):
   - first request opens with the rendered default (server log): workspace path, today's
     date, `ask-before`;
   - Shift+Tab to plan, next message: the prompt now says `plan` (mode is live);
   - a `system-prompt-models:` entry keying the server's ACTIVE (discovered, not configured)
     model name replaces the global prompt — proving post-resolution selection;
   - `system-prompt-file: ~/prompts/x.md` works; point it at a missing file → startup error
     naming key and path; add `{{bogus}}` → startup error listing the three placeholders; set
     text AND file → startup error naming both keys;
   - delegate a task (`sub_agent`): the child's request carries the same prompt (server log
     at depth 1); run `/compact`: the summary request's system message is the summariser's
     own, no user prompt in it;
   - comment the key out: the request opens with the user message again, zero bytes added.
5. **The invariant hand-off:** on a machine with an EXISTING `~/.apogee/config.yaml`, upgrade
   and run — behaviour unchanged (no prompt); `rm` the config, run — the seeded template
   carries the active default and the prompt appears.
