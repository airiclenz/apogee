---
Status: accepted
Amends: ADR 0025 (decision 10)
---

# One `/` namespace: skills are inline tokens, commands execute at accept

> **Amendment 2026-08-05 — the alternate `/skill` entry point is removed.** Decision 3 below kept
> the two-step `/skill <partial>` picker (and with it the `menuOnly` spec flag) as an alternate
> entry point, chiefly as the route to a skill whose id a command verb shadows. The owner ruled it
> redundant (`ISSUES.md`: *"`/skill` is not needed anymore - remove it"*) and it was deleted with
> plan `docs/plans/2026-08-04 - 06 - remove-skill-command-plan.md`. **The core decision is
> unchanged** — one `/` namespace, skills as inline tokens in the message text, commands executing
> at accept. What changes is that the merged menu and the directly typed `/<skill-id>` token are now
> the only ways in, and the shadowed-skill route named in decision 3 and in the rejected
> alternatives is the `/id` token typed mid-message, which `submitParse` still resolves. The body
> below is left as it was written, describing the design at the time it was taken.

## Context

The prompt box had grown two slash worlds that did not know about each other, and the owner hit
the seam from three directions in one issue (`ISSUES.md`): *"slash commands like `/skills` do not
work when typing a message to be scheduled and they also don't work if I want to use a slash
command after I already typed something in the prompt editor. I also would like to see skills and
files in-line in the prompt text. Skills should also directly be callable with `/` (without the
need to call the list with `/skills`)."*

Every symptom traced to a different piece of the same design:

- **The `/` menu was whole-value and idle-only.** `computeAutocomplete` opened the command region
  only when the entire input was one `/partial` with no whitespace anywhere, and only at
  `stateIdle`. A draft already in the box therefore had no command namespace at all, and neither
  did the message being composed while the model worked — which is the state the human spends the
  most time in.
- **Skills were STATE beside the message, not text in it.** `/skill <partial>` popped a chip onto
  `pendingSkills`, a parallel attachment list rendered as a violet strip above the box, carried to
  submit, popped by Backspace, dropped by `reset`, carried by `/continue` and dropped by
  `/compact`. Nothing about the message the model would read said a skill was attached, a skill
  could not be named directly, and `stageInterjection` silently discarded the attachments
  (`interject.go` parsed with `parsed, _ :=` and built a `UserInput` carrying no `SkillIDs`) — so a
  skill staged while the model worked was quietly lost.
- **`/skills` did not exist**, and a `/word` naming nothing was sent to the model verbatim. The
  literal reproduction in the issue is a human typing `/skills` and watching it arrive as prose.
- **`runCommand` reset the editor unconditionally**, so invoking a command could only ever mean
  destroying the draft.
- Two registries described the same verbs — `knownCommands` (parser) and `commandMenu` (dropdown,
  with summaries and the menu-only `skill`) — and drift between them was structural.

Mid-string completion had been **deliberately deferred** in `TODO.md` on the grounds that the
trailing-token rule was "cursor-position-free, robust". That rationale is what this ADR reverses.

The design below is the 2026-07-28 grill session's rulings, implemented item-by-item by
`docs/plans/2026-07-28 - 03 - slash-skills-inline-plan.md`. It supersedes nothing and amends
[ADR 0025](0025-interjections-commit-at-the-between-steps-boundary.md) decision 10 in one clause.

## Decision

**1. ONE `/` namespace, one table.** `commandSpecs` (`command.go`) is the single registry of verbs:
`{name, summary, takesArgs, whileRunning, menuOnly}`. The parser recognises every non-`menuOnly`
name; the dropdown renders every row with its summary. The two-registry drift is gone by
construction, and `commandByName` is the one membership test both sides read.

**2. A skill is invoked by naming its id as a `/token` in the message text — chips are retired.**
`/code-audit please check the parser` invokes the skill, exactly as `@path` names a file. The
token sits at a word boundary, is whitespace-delimited, and **stays in the text** at submit: the
model sees the token *and* the injected skill body. Only a token the catalog confirms is a
reference (`extractSkillRefs` over `refSpan`, gated by `Model.knownSkillID`); any other `/word`
inside a message is prose, so `/usr/bin` and a typo travel untouched. The extracted ids ride out
as `domain.UserInput.SkillIDs` — the field and the agent-side resolution are unchanged.

Keeping the token in the text is an owner override of the strip-it default, and it is what makes
the accent (decision 7) and the transcript honest: what the human sees in the box is what the
model reads. With it go `pendingSkills`, the chip strip above the box, the Backspace chip-pop,
`/continue`'s chip-carry and `/compact`'s chip-drop. The transcript's **sent-block** chip row
survives, now fed display names resolved from the parsed ids.

**3. The merged menu, and commands shadow skills.** A `/` token offers ONE dropdown: commands
first (prefix-matched, with summaries), then skills (substring-matched on id and display name,
marked with `glyphSkill`). A skill whose id equals a command verb is omitted from the merged rows
— **the collision is resolved menu-side, so the parse layer never has to know skills exist**. The
two-step `/skill <partial>` picker survives as the alternate entry point that reaches a shadowed
skill; accepting a pick now splices an inline `/id ` token rather than popping a chip. A new
`/skills` verb prints the catalog (reloaded first) as a transcript note, purely for browsing.

**4. Accepting a command row RUNS it, and the draft survives.** `acceptAutocomplete` cuts the
`/verb` out of the value, keeps everything else, re-seats the caret, closes the overlay and calls
`runCommand`. `runCommand`'s unconditional `input.Reset()` moved OUT to its call sites, and its
contract is now *it never touches the editor* — the two callers disagree on purpose: a whole-input
invocation arrives from `submit`, which empties the box (the line was nothing but the command); an
accept arrives with the box already prepared. The two verbs that need what follows them —
`/confine` (`takesArgs`) and the menu-only `/skill` — splice and wait instead of firing.

**The whole-input form keeps ownership of arguments.** `⏎` on a finished token falls through to
submit exactly when that token is the whole trimmed input (where `matchCommand` parses
`/confine off --save` as it always has) and accepts everywhere else. So there is exactly one way
to pass arguments and exactly one way to invoke a command mid-draft, and neither can be reached by
accident.

**5. Completion is caret-aware, for all three regions.** The completion region is the token AT the
caret — `caretToken` scans to the whitespace on either side — for `/` (`caretSlashToken`), `@`
(`caretFileToken`, quoted grammar included) and `/skill <partial>` (`skillArgToken`) alike.
`autocompleteState` carries `tokenStart`/`tokenEnd`; accept splices over exactly that range and
re-seats the caret after the splice. This **un-defers** `TODO.md`'s mid-string completion item and
reverses its rationale: the trailing-token rule was not robustness, it was the reason a draft in
the box had no namespace. Going back to fix a misspelled skill id mid-message now offers the same
menu the end of the buffer does.

**6. The while-running policy is per COMMAND, not per state.** Every region opens while the model
works — hiding the menu was the first symptom in the issue. What varies is the verb:
`commandSpec.whileRunning` marks the verbs that only REPORT (`/version`, `/skills`, and `/confine`
in its status **form**), and `parsedInput.safeWhileRunning` reads the flag *and* the parsed
`/confine` action, because one verb is safe under one form and idle-only under the other.
`Model.commandRunnable` is the single gate both `⏎` and the dropdown accept consult. An idle-only
row is rendered **tagged** `— idle only` rather than hidden — the human learns the verb exists and
why it is refused — and accepting it prints `commandsAtIdleNote` with the draft untouched. Skill
and file tokens are message content and simply ride the interjection.

`/confine`'s status form is runnable mid-run for a structural reason, not a hopeful one:
`ConfineToWorkspace()` reads the live flag under the Agent's own `RWMutex`, the `SetMode` class
[ADR 0011](0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md) names. `/skills` is safe
for the same kind of reason: `skills.Provider` swaps whole catalogs behind an `atomic.Pointer`.

**7. A token is accented exactly when it RESOLVES.** A post-render pass over the prompt block
(`inputaccent.go`, composed before the drag-selection so a selection wins over any accent it
covers) gives a `/token` the skill accent only when the catalog confirms it and an `@path` the
file accent only when the workspace listing already holds it. Everything else stays plain, so the
styling doubles as live validation: a typo'd skill visibly fails to light up while it is typed,
instead of failing at submit. Both grammars are located by the ONE scanner the extractors read, so
what lights up is by construction what would be acted on. The render path never walks the disk —
a cache miss renders plain and self-heals on the next walk.

**8. A sole `/word` that names nothing is REFUSED, never sent.** `parseInput` classifies
`kindUnknownSlash`: the entire trimmed input is one whitespace-free `/word` matching no verb, no
menu verb and no skill id. Both `⏎` paths answer it with a note naming the token
(`unknown command or skill: /code-adit — nothing sent`) and leave the text in the box for a
one-character fix — the `blockedUpstream` refusal posture. A bare `/skill` earns the picker's
usage line instead, being a menu verb rather than a typo. **Any** input with more than that one
token is an ordinary message, so the guard can never eat prose.

**9. Edge defaults.** An input that is only a skill token is a valid submit ("just run the skill" —
the text *is* the token). Command verbs shadow colliding skill ids. `/confine`'s arguments are
whole-input only. A multi-line message whose first line is `/clear` stays a message (the
pre-existing rule: the verb delimiter is space or tab, never a newline).

### What this amends in ADR 0025

[ADR 0025](0025-interjections-commit-at-the-between-steps-boundary.md) decision 10 reads *"The
`@file` autocomplete works while running (refs are useful in a remark); the `/` and `/skill`
pickers stay idle-only, because offering a command that would be refused misleads."* Decision 6
above replaces the second clause: the menu opens for every region while running, and what would
have misled is answered by **tagging the row** instead of hiding the namespace. Everything else in
ADR 0025 stands — commands still never queue, an idle-only verb still earns
`commands run at idle — not queued`, and staging still commits at the between-Steps boundary. The
one behavioural repair on that path is decision 2's: a staged interjection now carries its
`SkillIDs` instead of silently dropping them.

## Considered options

- **Strip the skill token out of the text at submit** (the apogee-code oracle's posture, where the
  webview posts `{text, skillIds}` with the token removed). Rejected by the owner: the token is the
  invocation, and a message whose visible text no longer says which skill it invoked cannot be
  accented, cannot be re-read honestly in the scrollback, and makes the box and the model disagree.
- **Keep the chips and add direct `/id` invocation beside them.** Rejected: two ways to attach the
  same thing, each with its own lifecycle (pop, drop, carry), and the parse layer would have to
  reconcile them at submit. The chips were the *only* place skill state lived outside the text, and
  deleting them deletes five special cases with it.
- **A separate skill sigil (`!id`, `#id`, `:id`).** Rejected: the issue asks for `/` specifically,
  it is what every comparable tool uses, and the collision it creates with command verbs is
  resolvable menu-side in one rule (decision 3) rather than by teaching users a second sigil.
- **Resolve command/skill collisions in the parser** (e.g. by prefix or by asking). Rejected: it
  would force the pure, Model-free parse layer to know the catalog exists for something other than
  a membership test, and there is already a route to a shadowed skill — the picker.
- **Let the parser recognise `/skill`.** Rejected, as before: keeping it `menuOnly` is exactly what
  keeps an unknown `/skill foo` line an ordinary message rather than a swallowed one.
- **Execute the command on Enter only, never on dropdown accept.** Rejected: it leaves the
  "I already typed something" symptom unfixed for every verb, since the whole-input rule cannot see
  a draft.
- **Submit the message when Enter is pressed on a fully-typed command mid-draft.** Rejected: the
  same keystroke would send a message in one context and run a command in another, decided by
  whether the token happened to be alone. Enter accepts what the menu is showing; the whole-input
  form is the one documented exception, and it exists because arguments need it.
- **Keep the trailing-token completion rule** (`TODO.md`'s deferral). Rejected — see decision 5.
  The "robustness" it bought was the defect the issue reported.
- **Accent every `/word` and `@path`, resolved or not.** Rejected: the accent's whole value is that
  it is a *verdict*. A uniformly lit typo teaches nothing.
- **Walk the workspace from the render path so an `@path` always resolves.** Rejected outright — a
  filesystem walk per frame, per keystroke. The accent answers from the listing the cache already
  has; a miss renders plain and the next walk heals it.
- **Send an unknown sole `/word` to the model as prose** (today's behaviour). Rejected: it is the
  exact confusion that spawned the issue. Refusing costs the human one keystroke; sending costs a
  wasted turn and a bewildering answer.
- **Refuse any unknown `/word` anywhere in a message.** Rejected: paths, dates and regexes contain
  slashes. The guard is deliberately scoped to the sole-token case, where the human can only have
  meant an invocation.
- **A second dropdown for skills beside the command menu.** Rejected: two overlays competing for
  the same slot above the box, and the human would have to know which one their token belongs to
  before typing it.

## Consequences

- **No public Go surface changes.** Everything lives in `internal/tui`;
  `domain.UserInput.SkillIDs`, `Config.Skills` and the agent-side resolution are reused unchanged.
  The `Engine` interface, `apogee.Config` and every exported name are untouched — this is a TUI
  behaviour release, not an API one.
- **A staged interjection now carries its skills.** The silent drop in `stageInterjection` is
  fixed, `joinedInterjections` unions `SkillIDs` first-seen exactly as it unions file refs, and the
  agent already resolves them at delivery ([ADR 0025](0025-interjections-commit-at-the-between-steps-boundary.md)'s
  resolve-at-delivery rule). A skill invoked in a remark reaches the model.
- **`runCommand` may be reached while a worker runs**, for reporting verbs only. ADR 0011's
  engine-call taxonomy is unchanged: the three verbs that can arrive mid-run are boundary-FREE by
  inspection (two synchronous notes and one `SetMode`-class read), and everything else is refused
  before it gets there.
- **The value-copied `Model` gains only plain values** — a slice header, ints, bools. ADR 0011's
  no-copy invariant and `TestModelNoBuilderByValue` are untouched.
- **The caret walk had to become correct on wrapped lines.** Splicing mid-draft made the editor's
  logical row/column seat load-bearing, which exposed a pre-existing defect: bubbles' `wrap`
  appends a phantom trailing sub-line that `CursorDown` can never enter, so the old walk could
  stall — and `reseatInput`'s guard-free version could spin. `promptEditor.seatCaret` (a
  `CursorEnd`-then-`CursorDown` step, Height-aware) is now the one walk both express, and a
  deadline-guarded test fails instead of wedging `go test` if it ever spins again. The mouse
  path's `reseatCaret` (a click's VISUAL row) is unchanged and still lands imprecisely below a
  phantom-wrapped line — pre-existing, deliberately out of scope, and parked in `TODO.md`.
- **CONTEXT.md's *Skill* entry gains the token grammar** and is cross-referenced against *File
  reference* in both directions; the *Interjection* entry records the per-command policy in place
  of the blanket "commands run at idle".
- **`layout.md` gains a prompt-box section** (the merged dropdown, the idle-only tag, the inline
  accents) and the chip strip leaves the spec with the code.
- **Two palette entries are now load-bearing for meaning, not decoration**: `colSkill` (violet,
  inherited from the retired chip) and the new `colFileRef` (blue). A future theme change must keep
  them distinguishable from plain prompt text, or the live validation stops validating.

## Addendum (2026-08-04) — the sent-block chip row is retired too, in favour of the inline accent

Decision 2 above kept one chip surface alive: *"The transcript's **sent-block** chip row survives,
now fed display names resolved from the parsed ids."* That clause no longer holds. The owner read
the result in `ISSUES.md` — a `❯ /refocus on everything` block with a second row saying `✦ Refocus`
under it — and ruled the tag row redundant: the skill should be "in-line with the text and simply
color marked", with no separate tag. The rest of ADR 0027 stands unchanged, and the retirement is
in fact decision 2's own logic carried to its end — keeping the token in the text is what made a
second surface repeating it unnecessary.

**The sent block paints its `/token` in `colSkill` and carries no chip row.** A submitted `❯`
prompt and a delivered `⧖` interjection are the same shape here, as they are for the collapse. The
accent lands on the rows the block shows, so it composes with the three-row cap rather than arguing
with it, and the transcript's drag-selection is still shaded afterwards and still wins.

**The accent is fed by spans PERSISTED on the entry, not by a catalog lookup at paint time.** The
chip row's job was to be *the record of what the model was actually given*; the spans inherit that
job. `skillRefSpans` — already the one scanner decision 7 names — hands its byte offsets to the
transcript entry at send time, the codec round-trips them, and `renderUserBlock` stays a free
function that never sees a `SkillCatalog`. Two consequences fall out of that choice: a replayed
session keeps its accents after the skill is renamed or deleted, and a skill invoked twice paints
at **both** occurrences, because the spans drive the paint rather than the de-duped id list.

The spans are a display concern and stayed TUI-local — parse result → transcript entry → codec.
`domain.UserInput` was not widened, per the wire-silent engine boundary of
[ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md); `SkillIDs` still
carries everything the agent resolves.

With the row go `renderUserChipRow`, `renderSkillChip`, `theme.skillChip`, the entry's display-name
field and `skillDisplayNames` — the display names had no other consumer, so the transcript now
stores ids and offsets only. `glyphSkill` survives for the `/` menu rows of decision 3, which are
untouched. Entries persisted before the spans existed decode with none and paint plain; a legacy
`skills` field on a decoded record is ignored. Pre-production, so no migration.

Executed by `docs/plans/2026-08-04 - 02 - inline-skill-token-accent-plan.md`; specced in
`layout.md` ("Collapsed and expanded blocks", "The prompt box's mini-language").
