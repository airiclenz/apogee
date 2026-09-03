---
Status: accepted
---

# The system prompt is a configured template, rendered per request

## Context

Apogee had **no built-in system prompt**. `buildRequest` projected `a.conv.Messages()` onto the
wire unchanged, so a conversation began at the user's first message, and the only text that ever
reached the system channel was engine-owned: the non-native profile's tool menu and
format-emission instructions, folded in at the wire seam
([ADR 0010](0010-package-layout-domain-core-and-thin-root-facade.md)'s `processing` boundary), and
the catalogued nudge Mechanisms' directives, appended through `Request.AppendToSystem`
([ADR 0003](0003-mechanisms-are-a-constraint-declared-registry-not-a-fixed-pipeline.md)).

That emptiness was a **scope guard, not an oversight**. The prompt-seam plan
(`docs/plans/archived/prompt-seam-wiring-plan.md`) shipped only the narrow profile-driven block and
deliberately parked the general template story in `TODO.md`, behind one hard anchor: a zero/native
profile must add **zero bytes** to the request, pinned by
`TestPromptSeam_NativeProfileByteIdentical`. The parked note also carried a design obligation — the
host-override knob for the rendered instruction block (the seam plan's *rejected hybrid*) must be
designed *together with* the general template so the two compose rather than fight.

What forces it now is the user-facing gap the guard was always going to leave. A user cannot tell
apogee anything standing — who it is, what this project's conventions are, that it is talking to a
person who wants terse answers — without retyping it into the first message of every session. The
apogee-code oracle assembled exactly this: a persona preamble plus `{{workspace}}` /
`{{datetime}}` / an agent-mode directive *around* the tools block. And the audience makes the frame
worth more, not less: a ~4B–35B local model reasons better with a short standing statement of who
it is, where it is, and what it is allowed to do — while every system token it spends competes with
the task, which is why the shipped default is five lines and not fifty.

This ADR records the design the owner ratified in the 2026-07-26 grill, implemented by
`docs/plans/2026-07-26 - 02 - configurable-system-prompt-plan.md`. It **supersedes nothing** and
closes the parked story.

## Decision

**1. The configuration surface is file-only: ~~three top-level keys~~.** *(Superseded 2026-08-31 by
[ADR 0064](0064-the-system-prompt-ships-an-embedded-default.md) §2 and this record's own 2026-08-31
amendment below: **four**. A fourth top-level key, `use-default-prompt:`, joins the three named
here — a bool, default `true`, gating the embedded-default rung of that record's resolution ladder;
an explicit `use-default-prompt: false` is the "send no prompt at all" that deleting these keys
used to spell.)* `system-prompt-text:` (the
template inline), `system-prompt-file:` (a file holding it), and `system-prompt-models:` (a map of
model name → an entry reusing the *same two key spellings*, not a bare `text:`/`file:`). There is
no flag and no environment variable, following the newer-key convention (`present:`, `mechanisms:`,
`model-profile:`): a system prompt is a standing statement about how you work, not a per-invocation
switch. `system-prompt-file` expands a leading `~` and resolves a relative path against the
**apogee home** — the directory `config.yaml` itself lives in — and never against the workspace: the
key lives in a global file that travels with the home, so a workspace-relative base would break one
config across every project it is used in.

**2. A matching per-model entry REPLACES the global prompt; a non-matching entry is inert.**
Matching is exact string equality against the **resolved** model name — the label discovery or
`--model` settled on, the same one the Validated sets key on
([ADR 0016](0016-curation-is-per-model-validated-sets-keyed-by-fingerprint.md)) — so selection can
only happen in the composition root *after* model resolution, and `resolveSystemPrompt` is called
from `runRoot` for that reason. Replacement is **whole-entry**: an entry setting only
`system-prompt-file` does not inherit the global `system-prompt-text`, because a prompt is one
document and half of one is not a prompt. An entry naming a model this run is not is never selected
and its file is **never read** — the `unconfined-hosts` posture
([ADR 0012](0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md)'s 2026-07-21
amendment): it describes a configuration this machine is not in, so one config can carry a prompt
per model across every machine it travels to.

**3. Validation splits on what is machine-independent.** Setting **both** spellings at one level, or
a `system-prompt-models` entry setting **neither**, is a defect in the file itself — a contradiction
and an indentation slip respectively — so both are checked at **every** level while config resolves,
where the message can name the exact key. Whether a file **reads** and whether a template's
placeholders are **known** are properties of the **selected** source only, checked in
`resolveSystemPrompt`: refusing to start over a non-matching entry's file, which may exist only on
another machine, would make the travelling config of §2 unusable everywhere else. Defense in depth:
`newAgent` re-validates `Config.SystemPrompt` (the `processing.ParserFor` construction-gate
precedent), so a Go embedder's typo fails construction loudly instead of shipping raw braces to the
model; the host-side check fires first for config users and names the config key.

**4. The template language is exactly four placeholders, strictly spelled, and closed.**
`{{workspace}}` (the absolute workspace path), `{{datetime}}`, `{{mode}}` (the autonomy mode label),
`{{scratch}}` (the session scratch dir, joined with
[ADR 0056](0056-terminal-fail-fast-and-session-scratch.md) §3).
Nothing else, and the set is **closed by decision**, not pending expansion — every placeholder is a
promise the engine must keep rendering. Three properties are load-bearing:

- **`{{datetime}}` renders the DATE only** (`2006-01-02`). A per-request timestamp would change the
  first system message every turn and throw away the prefix KV cache a local llama.cpp server
  relies on; a date is stable within a day, which is the resolution the model actually needs.
- **Spelling is strict**: `{{ workspace }}`, `{{WORKSPACE}}` and `{{foo}}` are all *unknown
  placeholders* and all are a **startup error listing the known four** — never silent literals
  shipped to the model, because raw braces in a system prompt are exactly the kind of defect a
  small model copies rather than questions. Stray or unclosed braces are not placeholders and pass
  through verbatim.
- **There is no `{{tools_block}}`.** The tool menu stays engine-owned and auto-appended (§6), so the
  user's template can never place it wrongly, duplicate it, or omit it for a model that needs it.

The language lives in a **new `internal/prompt` package**, stdlib-only — it imports not even
`internal/domain` (`Inputs.Mode` is a plain string) — so the host (`cmd/apogee`, validating at
startup) and the engine (`internal/agent`, rendering per request) share one implementation with no
import cycle and one error wording.

**5. `Config.SystemPrompt` carries the TEMPLATE; the Agent supplies the render inputs.** The field
is the validated template, never a rendered string, because three of the four inputs are **live**:
the autonomy mode changes on a Shift+Tab, the date changes at midnight in a long-running session,
and the session scratch dir moves at a session boundary. All four inputs already sit on the Agent —
`cfg.WorkspaceDir`, the lock-guarded `Mode()`, the lock-guarded `ScratchDir()`, and a new injectable
`now func() time.Time` (the `sessionHost.now` shape) — so rendering is a private method, not a
public render-provider seam with exactly one implementation.

**6. Seeding is at `buildRequest`, position 0 — one system message, in one fixed order.** The
rendered prompt is prepended to the message projection before `domain.NewRequest` is built, which
buys every required property at once:

- `AppendToSystem` finds the prompt at index 0 and appends **after** it; the wire seam's
  `injectSystemInstructions` appends the tool block after **that**. The wire therefore carries
  **one** system message reading **prompt → mechanism directives → tool block** — the merge shape
  `TestPromptSeam_AppendsToSeededSystemMessage` already pinned, now the default path.
- The projection is **per-request**, so the prompt is re-rendered on every request (`armRequest`,
  and `refold` after an emergency fold —
  [ADR 0018](0018-context-overflow-recovers-structurally-the-emergency-fold-and-one-retry.md)) and
  **never enters history or the snapshot**. Nothing about resume, forking or session records changes
  ([ADR 0022](0022-sessions-persist-per-turn-as-dual-representation-records.md)): `cfg` is not
  serialized, so a session resumed under a changed prompt gets the new one.
- `""` seeds **nothing** — no empty system message, no whitespace — so the native no-prompt anchor
  stays byte-identical and every pre-existing test is untouched.
- `loopView()` stays **unseeded**: it is "the conversation so far" for tool-stage hooks, and the
  profile tool block is likewise absent from it. Seeding is a request-projection concern, not a view
  one.

**7. Sub-agents inherit the prompt; the internal request paths keep their own.** `newChildAgent`
copies `cfg` wholesale, so a child renders the same template against **its own** mode and workspace
with no carve-out — which is right: a sub-agent is the same assistant on a smaller task
([ADR 0005](0005-sub-agent-privileges-are-bounded-by-the-parent.md)). The compaction summariser
(`internal/context`) and the probe battery (`internal/probe`) build their **own** requests and never
call `buildRequest`, so the user's prompt cannot reach them **by construction** — and must not: a
summariser told "you are a coding agent, use tools" writes a worse summary
([ADR 0021](0021-probe-is-two-halves-the-host-report-is-free-the-model-battery-is-an-explicit-act.md)
draws the same line for the battery).

**8. The shipped template goes one key ACTIVE, and that ends its "changes nothing" invariant.**
`internal/config/defaults/config.yaml` carries an uncommented `system-prompt-text:` — a ~5-line persona
and context frame — so a **fresh install** gets a system prompt, while an **existing** seeded config
(which `seedConfig` never overwrites) has no such key and keeps today's promptless behaviour byte
for byte. There is deliberately **no compiled-in fallback**: the default lives in the file the user
can read and edit, deleting the key is how you turn it off, and an upgrade therefore changes nothing
for anyone already running. The template's old invariant — every line commented, so it parses to an
empty `layer{}` — is amended rather than abandoned: the test now asserts that the system prompt is
the **only** key it sets and that every other key still parses to nothing.

## Considered options

- **Seeding through a construction-time PreRequest hook** — rejected. Hooks dispatch
  catalogued-first, so a Mechanism's `AppendToSystem` would create the system message *before* a
  seeding hook ran, putting mechanism directives **ahead** of the prompt; and the prompt is
  structural configuration, not a Mechanism — it must apply under `--bypass`
  ([ADR 0006](0006-bypass-mode-is-the-mechanisms-off-floor.md)), which Mechanism plumbing does not
  guarantee.
- **Seeding at the wire seam (`toProviderRequest`)** — rejected. `AppendToSystem` would then create
  its *own* system message in the domain Request, and the wire seam would need a second merge pass to
  order the two; worse, pre-request hooks and the predictive overflow guard (`requestExceedsWindow`,
  through `req.State()`) would not see the prompt at all, so the Budget estimate would understate
  every request by the prompt's size.
- **The template language in `internal/domain`** — rejected. Domain is declarative data and
  loop-facing working values; scanning, substitution and a date format are a *processing* concern
  (ADR 0010). The `ModelProfile` precedent is exact: domain keeps the profile as **data** and
  delegates parsing outward.
- **The template language in `internal/processing`** — rejected. That package is the model profile's
  parse/emit seam; its vocabulary is tool-call formats and thinking channels. The system-prompt
  template is a host/config concern with different consumers and a different lifetime. One concern
  per package.
- **A rendered string on `Config` instead of a template** — rejected: the mode and the date are live,
  so a rendered string would be stale from the first mode switch, and every embedder would have to
  re-render it themselves on a schedule they cannot know.
- **A compiled-in default prompt** (a fallback used whenever the key is absent) — rejected: it would
  change behaviour for **every existing install** on upgrade, and hide the text where it cannot be
  read or edited. The shipped-file default of §8 gives new users the prompt and old users their
  current behaviour, with one visible source of truth.
- **A `--system-prompt` flag / `APOGEE_SYSTEM_PROMPT` env var** — rejected with the other newer keys:
  the precedence ladder exists for per-invocation switches, and a prompt is not one. A file path in
  the config is already the way to swap prompts.
- **Per-model entries that MERGE with the global prompt** (append, or fill in the missing spelling) —
  rejected: merge order and duplicate-persona questions have no obviously right answer, and
  whole-entry replacement is the rule a user can predict from the config file alone.
- **Automatic per-capability-tier prompt shortening** (ADR 0021's parked idea) — not built here. The
  `system-prompt-models:` map is the **manual** per-model lever, which is evidence-free and
  predictable; automatic tiering stays parked until measurement justifies it.

## Consequences

- **`apogee.Config` gains one additive field, `SystemPrompt`** — no exported name changes (ADR 0010),
  so this is a **minor** bump. An embedder that sets nothing is byte-identical to before, and one
  that sets a bad template fails `New`/`Resume` loudly rather than at request time.
- **The Budget now measures the prompt.** `req.State()` carries the seeded message, so the
  predictive overflow guard and the estimator's calibration account for it — honest accounting of
  what the request actually costs, at the price of slightly less room for history on a small window.
  Post-response scanners reading `req.View()` see a leading system message whenever a prompt is
  configured; that is the shape a seeding pre-request hook already produced.
- **A user prompt containing a Mechanism's marker phrase silently suppresses that directive.**
  `Request.AppendToSystem(marker, text)` is idempotent by `strings.Contains(first system message,
  marker)`, and the markers are natural-language phrases — `decompose`'s `"Focus on one action"`,
  `cot`'s `"have not read any files yet"`, `library`'s `"[Apogee context notes"`. Until now the
  first system message was engine-written, so that check only ever saw the Mechanism's own earlier
  injection; now the user's prompt **is** that message, and a prompt that happens to contain the
  phrase reads as "already injected". This is a **suppression, not a corruption**: the request stays
  well-formed, and the user's own sentence on that subject is what the model reads instead. The
  blast radius is bounded to the catalogued nudge Mechanisms, which are default-off and enabled per
  model on bench evidence ([ADR 0015](0015-catalogued-mechanisms-are-enabled-by-id-through-config.md)).
  It is **accepted, not fixed, here**: a non-textual idempotency channel changes the hook API's
  contract for every Mechanism, and the alternative — opaque sentinel strings — would put text in
  front of the model whose only reader is apogee. Recorded as a residual in `ISSUES.md`; revisit if a
  real prompt trips it.
- **[ADR 0018](0018-context-overflow-recovers-structurally-the-emergency-fold-and-one-retry.md) §1's
  aside is now this ADR's to own.** That decision noted "the shipped config template stays
  behaviour-neutral — nothing here is a new default to enable", which was true of *that* change and
  is still true of it; §8 above is what ends the template-wide neutrality it leaned on. ADR 0018 is
  left as written, with a dated note pointing here.
- **The host-override knob for the rendered instruction block stays additive-later, and composes.**
  This is the obligation the parked TODO note attached to the general template: because §6 merges
  prompt, directives and tool block into **one** system message, a future host-supplied block
  replaces the rendered one *inside the same message* without reshaping anything decided here.
- **CONTEXT.md gains one term — *System prompt*** — worded to match this ADR, and distinguished from
  the summariser's and the battery's dedicated prompts.
- **`{{datetime}}` is a per-day cache boundary.** A session running across midnight re-renders the
  first system message and the server's prefix cache misses once. That is the accepted cost of a
  correct date, and the reason the placeholder is not a timestamp.

## Note (2026-07-28) — the zero-byte anchor now reads "no prompt AND no context files"

Decision §6 says `""` seeds nothing, "so the native no-prompt anchor stays byte-identical". That
still holds for the prompt: an empty template seeds no system message, and
`TestPromptSeam_NativeProfileByteIdentical` is untouched.

What changed is the *condition* the anchor tests.
[ADR 0026](0026-workspace-context-files-are-session-scoped-prompt-data.md) added a **second,
independent** source of standing system content — the workspace context files
(`Config.ContextFiles`, default `AGENTS.md`) — which seeds the same position-0 message on its own,
so the request is byte-identical only with **no prompt and no context files**. The seed is composed
by `standingSystem()` (prompt → context files) and everything else in §6 is unchanged: one merged
system message, per-request projection, never in history or the snapshot. The Decision text above is
left exactly as written; this note is the pointer, so the anchor's condition is not read as narrower
than it is.

## Addendum (2026-08-01) — the restore seam ENFORCES "no system message in committed history"

Decision §6 makes the prompt request-scoped, so apogee never *writes* a `RoleSystem` message into
history or a snapshot — pinned by `TestPromptSeam_ConfiguredPromptNeverEntersHistoryOrSnapshot`.
That covers everything apogee produces. It did not cover what apogee **consumes**: a legacy record
from before this ADR, or a hand-edited snapshot, whose stored conversation already *begins* with a
system message. `restoreState` installed such a conversation wholesale, `buildRequest` then
prepended the freshly rendered standing content unconditionally, and the wire carried **two** system
messages — with the profile's tool block folded into the first (the stale stored one), because
`injectSystemInstructions` merges into the first system message only.

`restoreState` (`internal/agent/state.go`) now drops the restored conversation's **leading run** of
`RoleSystem` messages before installing it. The invariant §6 maintained is therefore *enforced* at
the one seam where outside bytes become history, and every later reader — the request projection,
the next snapshot — is clean by construction.

- **A no-op on the happy path.** A well-formed apogee snapshot has never contained a system message,
  so the normalization drops nothing and the restore path is byte-identical to before.
- **Only the LEADING run.** That is the position the seeded message and the tool-block fold collide
  at. A hook-authored mid-history system message is left alone — `Conversation.PrefixEnd`
  deliberately keeps tolerating leading system messages *in a request*, which is a different
  question from what may be committed.
- **The Exchange boundary moves with the drop.** Removing messages shifts the rest of the history
  down, so the restored `exchangeStart` is shifted by the dropped count (clamped at 0); otherwise
  `AbortExchange` on a normalized legacy snapshot would roll back past that Exchange's own start.
- **Discarding beats preserving.** The dropped message is a stale rendering of standing content that
  §5 says must be re-rendered from live inputs anyway; keeping it would either duplicate the
  standing content on the wire or require the seam to guess which of the two is authoritative.

## Amendment (2026-08-25) — §6's composition gains a THIRD, engine-composed part

§6's decision stands: one system message at position 0, seeded per request, in one fixed order.
Only the composition list grows. Alongside the rendered template and the workspace context files'
blocks ([ADR 0026](0026-workspace-context-files-are-session-scoped-prompt-data.md)) the engine now
appends its own **orientation block** (`internal/agent/orientation.go`,
`prompts/orientation.txt`), so the wire order reads **prompt → context files → orientation →
mechanism directives → tool block**.

- **Why the engine owns it.** The block states host facts — the workspace path, the session
  **scratch dir** and the `/tmp` caveat, the read-only library roots and the tools that reach them
  — that a model cannot work without and that no persona template should have to carry. §8's
  no-compiled-in-fallback rule means a seeded `~/.apogee/config.yaml` is never refreshed, so a
  fact added to the shipped template after a user's first run reaches that user never. Harness text
  the engine composes has no such gap, and no edit to `system-prompt-text` can delete it.
- **Ride-along, not a fourth source.** The block is appended only when the standing system content
  is already non-empty — a rendered template and/or context-file blocks. With neither configured
  `standingSystem()` still returns `""` and nothing is seeded, so §6's "`""` seeds **nothing**"
  anchor and the Bypass floor beneath it stay byte-identical. The block never opens a system
  message of its own.
- **Live inputs, per-session constants.** Like §5's render inputs the facts are read fresh per
  request — `cfg.WorkspaceDir`, the lock-guarded `ScratchDir()`, the live `cfg.ExtraReadRoots` func
  — so a session boundary that moves the scratch dir lands on the next request. All three are
  constant *within* a session, so the block is prefix-KV-cache stable exactly as `{{scratch}}` is.
  A fact the session does not have (no scratch dir yet, no mounted roots) is **omitted**, never
  rendered as an empty path.
- **§7 unchanged.** `newChildAgent` copies `cfg` wholesale and the child renders its own
  `standingSystem`, so a sub-agent gets the block with no carve-out and no wiring of its own.
- **The shipped template drops what the block now carries.** `defaults/config.yaml` no longer
  spends two lines on the scratch dir and `/tmp`; the `{{scratch}}` placeholder stays supported for
  a user's own prose. The template is persona, the block is orientation.

## Addendum (2026-08-26) — the orientation block moves AHEAD of the context files

The composition stands; only its position changes. The wire order is now **prompt → orientation →
context files → mechanism directives → tool block**. With the blocks first, a hostile `AGENTS.md`
could open with a forged orientation naming its own paths and the engine's real block, arriving
after, would read as a correction of the forgery rather than the reverse (security audit F-19).
Riding directly after the prompt means no workspace text ever precedes the host facts. Ride-along
is untouched: the empty check is still taken on the two configured sources *before* the block is
composed in, so "no prompt AND no context files" still seeds nothing. The fence around the blocks
is [ADR 0026](0026-workspace-context-files-are-session-scoped-prompt-data.md)'s own 2026-08-26 addendum.

## Amendment (2026-08-31) — §8's "no compiled-in fallback" is SUPERSEDED by ADR 0064

[ADR 0064](0064-the-system-prompt-ships-an-embedded-default.md) ships the default template
**embedded in the binary**, so two pieces of this record no longer hold and are explicitly
superseded:

- **§8's rule** that "there is deliberately **no compiled-in fallback**: the default lives in the
  file the user can read and edit, deleting the key is how you turn it off, and an upgrade
  therefore changes nothing for anyone already running". The rest of §8 stands — the shipped
  template still sets exactly one system-prompt key and every other key still parses to nothing —
  except that the key it seeds is now **commented out**, so a fresh install runs on the embedded
  default rather than on a copy of it.
- **The rejected alternative** "*A compiled-in default prompt* (a fallback used whenever the key is
  absent)". Its two objections are answered rather than dismissed: the behaviour change on upgrade
  is opted out of with `use-default-prompt: false`, and the text is read and edited through the
  settings editor, which pre-fills it. The evidence §8 did not have is that a seeded file is frozen
  per install, so the default could not be improved for anyone already running — the same gap this
  ADR's own 2026-08-25 amendment named when it moved host **facts** into the orientation block.

Resolution is now a four-rung ladder — a matching `system-prompt-models` entry > top-level
`system-prompt-text`/`system-prompt-file` > the embedded default when `use-default-prompt` is not
`false` > nothing — with §2's whole-entry replacement unchanged all the way down. §6's "`""` seeds
**nothing**" anchor is untouched as a rule but is now **reached only** through
`use-default-prompt: false` or an explicitly empty configured template: a stock run seeds a system
message. Everything else above — the placeholder language, per-request rendering, position-0
seeding, sub-agent inheritance, the restore-seam normalization — is unchanged.

## Addendum (2026-09-02) — a FOURTH, child-only part: the delegate report block

§6's decision stands and so does its order; only the composition list grows again, and this time
the new part is conditional on **who** the agent is rather than on what is configured. On a
**delegated** agent — `depth > 0`, routed and unrouted alike — the engine composes in its own
**delegate report block** (`internal/agent/delegatereport.go`), so a child's wire order reads
**prompt → orientation → delegate block → context files → mechanism directives → tool block**. A
top-level agent's message is byte-identical to what it was, because the block is composed only when
the depth gate opens.

- **Why the engine owns it.** The block states the one fact a delegate cannot learn from any
  configured source: the agent that delegated the task sees nothing of the child's conversation and
  receives only its FINAL reply, so anything not written there is lost. It then asks for that reply
  in the shape a parent can act on — what was found, what changed, what is unfinished, cited by
  `path:line` rather than pasted. A `system-prompt-text` is written by a user who may never delegate,
  and a workspace `AGENTS.md` is written by a repository that cannot know it is being read by a
  child; neither can be relied on to carry it, and no edit to either should be able to delete it.
  It is engine-owned with no config key and no **Mechanism** gate — `wrapUpDirectiveFormat`'s
  precedent (`internal/agent/subagent.go`) — so it is on under **Bypass** too.
- **Position, on the same F-19 reasoning.** It sits after the orientation block and **ahead of the
  context files**, for the reason the 2026-08-26 addendum moved the orientation there: every
  engine-owned part precedes the repo-controlled blocks, so no workspace text can arrive after the
  host's own statement and read as a correction of it. The other half of that guard is the fence —
  a context-file line spelling the block's opening sentence is prefixed `[workspace text] ` exactly
  as a forged orientation header is ([ADR 0026](0026-workspace-context-files-are-session-scoped-prompt-data.md)'s
  2026-08-26 addendum).
- **Ride-along, not a fifth source.** The empty check on the two configured sources is still taken
  **before** the block is asked for, so `standingSystem()` still returns `""` for no prompt AND no
  context files and §6's seeds-nothing anchor stays byte-identical. Per the 2026-08-31 amendment
  that posture is now reached only through `use-default-prompt: false` or an explicitly empty
  configured template, so in practice a stock delegation always carries the block — but the rule is
  unchanged and still binding.
- **It states the new order rather than rewriting the old records.** This ADR's 2026-08-26 addendum
  and [ADR 0026](0026-workspace-context-files-are-session-scoped-prompt-data.md)'s own order
  sentence are dated records of what was decided then; they are left as written, and this addendum
  is where the current order is read off.
- **It does not contradict the wrap-up directive.** A capped child's request carries both: the
  wrap-up directive says *this* turn is the last one it gets, the report block says what the final
  reply is **for**. Neither claims the other's reply is the last.

## Addendum (2026-09-03) — a FIFTH part, and the first engine block that is NOT a session constant

§6's decision stands and so does its position-0 seeding; the composition list grows once more, and
this time the new part breaks the one property every earlier engine-composed part shared. The
engine now composes in the **task list block** (`internal/agent/tasklistblock.go`) — the model's own
checklist, held on the engine and written only through the `task_list` tool
([ADR 0072](0072-the-task-list-is-model-owned-session-state.md)) — so the standing content is
composed of **five** parts and the wire order reads **prompt → orientation → delegate report → task
list → context files → mechanism directives → tool block**.

- **Position, on the 2026-08-26 F-19 reasoning read in both directions.** The block rides ahead of
  the workspace context files, so every engine-owned part still precedes the repo-controlled blocks
  and no workspace text can arrive after the host's own statements and read as a correction of
  them. It goes **last** of the engine's four for the complementary reason: its content is
  MODEL-authored, and text the model wrote must not sit where the host's facts sit. The other half
  of the guard is the fence — a context-file line spelling `tasklist.Fence` is prefixed
  `[workspace text] ` exactly as a forged orientation header is
  ([ADR 0026](0026-workspace-context-files-are-session-scoped-prompt-data.md)'s 2026-08-26
  addendum).
- **Ride-along, not a sixth source.** The empty check on the two configured sources is still taken
  **before** the block is asked for, and an empty list renders `""` besides, so `standingSystem()`
  still returns `""` for no prompt AND no context files and §6's seeds-nothing anchor stays
  byte-identical.
- **THE VOLATILITY EXCEPTION — what this addendum amends.** The 2026-08-25 amendment's "Live
  inputs, per-session constants" bullet holds that an engine-composed block reads live inputs which
  are nevertheless constant *within* a session, so the block is prefix-KV-cache stable exactly as
  `{{scratch}}` is. The task list is the first engine-owned part for which that is false: every
  `task_list` call changes the standing content and invalidates the server's prefix cache from this
  block onward. That bullet is amended to admit an engine block whose volatility is **under the
  MODEL's control** — the property that makes the cost payable rather than imposed, since a session
  whose model never calls the tool pays nothing and one that does is spending a re-encode it asked
  for. The rule for every other engine block is unchanged: per-session constants, or state why not.
  (The standing content as a whole was never wholly constant — the rendered template varies with its
  live `{{mode}}`, so Shift+Tab already moved the prefix; what is new is an *engine-composed* part
  that does.)
- **A delegation gets its own empty list.** §7's wholesale `cfg` copy carries nothing here: the
  child is constructed with a fresh list, so its block starts `""` and fills only from its own calls
  (ADR 0072 decision 7).
- **It states the new order rather than rewriting the old records.** The 2026-08-26 and 2026-09-02
  addenda are dated records of what was decided then and are left as written; this addendum is
  where the current order is read off.
