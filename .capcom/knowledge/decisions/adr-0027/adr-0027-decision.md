---
title: ADR 0027 decision
---

**1. ONE `/` namespace, one table.** `commandSpecs` (`command.go`) is the single registry of verbs:
`{name, summary, takesArgs, whileRunning, menuOnly}`. The parser recognises every non-`menuOnly`
name; the dropdown renders every row with its summary. The two-registry drift is gone by
construction, and `commandByName` is the one membership test both sides read.

**2. A skill is invoked by naming its id as a `/token` in the message text — chips are retired.**
`/code-audit please check the parser` invokes the skill, exactly as `@path` names a file. The
token sits at a word boundary, is whitespace-delimited, and **stays in the text** at submit: the
model sees the token *and* the injected skill body. Only a token the catalog confirms is a
reference (`refs.SkillRefs` over `refs.Span`, gated by `Model.knownSkillID`); any other `/word`
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

> **Amended 2026-09-14 by ADR 0025
> (decision 10, amended the same day).** The refusal clause is retired: an idle-only verb accepted
> or entered while a worker works is **queued** — the row is tagged `— runs at idle`, the line joins
> the band above the box as a `queued command: /verb` row, and it runs FIFO at the next idle before
> any held message is sent. The per-command policy itself, `Model.commandRunnable` as the one gate
> both routes consult, and the tagged-not-hidden row all stand; only what accepting a tagged row
> does has changed, and the dropdown accept still cuts only the verb token out of the draft.

`/confine`'s status form is runnable mid-run for a structural reason, not a hopeful one:
`ConfineToWorkspace()` reads the live flag under the Agent's own `RWMutex`, the `SetMode` class
ADR 0011 names. `/skills` is safe
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

ADR 0025 decision 10 reads *"The
`@file` autocomplete works while running (refs are useful in a remark); the `/` and `/skill`
pickers stay idle-only, because offering a command that would be refused misleads."* Decision 6
above replaces the second clause: the menu opens for every region while running, and what would
have misled is answered by **tagging the row** instead of hiding the namespace. Everything else in
ADR 0025 stands, and staging still commits at the between-Steps boundary. *(This section once
also read "commands still never queue, an idle-only verb still earns `commands run at idle — not
queued`"; ADR 0025's 2026-09-14 amendment to decision 10 reversed that — commands queue.)* The
one behavioural repair on that path is decision 2's: a staged interjection now carries its
`SkillIDs` instead of silently dropping them.
