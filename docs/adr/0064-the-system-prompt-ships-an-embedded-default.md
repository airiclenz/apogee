---
Status: accepted
---

# The system prompt ships an embedded default

## Context

[ADR 0023](0023-the-system-prompt-is-a-configured-template-rendered-per-request.md) made the system
prompt a configured template and put the shipped default in **one place**: an uncommented
`system-prompt-text:` in `internal/config/defaults/config.yaml`, seeded into `~/.apogee/config.yaml`
on first run. §8 chose that deliberately over a compiled-in fallback — "deleting the key is how you
turn it off, and an upgrade therefore changes nothing for anyone already running" — and the
rejected-alternatives list repeats the reasoning.

The property that bought is the property that now costs. `seedConfig` never overwrites, so the
default a user received on the day they installed apogee is the default they still have. Every
sentence of standing guidance written since then — every convention learned from a bench run, every
correction of a small model's habitual failure — reaches nobody who installed earlier. The default
is not merely un-upgradable; it is **frozen per install**, so there is no single text whose quality
can be improved at all.

ADR 0023's own [2026-08-25 amendment](0023-the-system-prompt-is-a-configured-template-rendered-per-request.md)
already named this gap in as many words — "a fact added to the shipped template after a user's first
run reaches that user never" — and routed *around* it for one class of content: host **facts** moved
into the engine-composed orientation block, which no upgrade can miss and no edit can delete. That
was the right fix for facts. It left **steering** — who the assistant is, how it works, what it does
before it edits — with no maintained channel at all, and it left the general question unanswered: a
new sentence of guidance now has four plausible homes (orientation block, template, Mechanism,
skill) and no written rule for choosing between them.

What forces the question now is the shipped-skills wave
([ADR 0065](0065-shipped-skills-and-the-load-skill-door.md)), which gives task-shaped guidance a
maintained home in the binary. Standing guidance needs the same, and the two must not compete for
the same sentences.

This ADR records the design the owner ratified on 2026-08-31, implemented by
`docs/plans/2026-08-31 - 00 - base-guidance-and-shipped-skills-plan.md`. It **supersedes ADR 0023
§8's "no compiled-in fallback" rule and the matching rejected alternative**, and nothing else.

## Decision

**1. The default template is embedded in the binary.** The bytes live in the source tree and are
compiled in with `go:embed`, exactly as the built-in color schemes are
([ADR 0040](0040-color-schemes-are-embedded-roles-with-user-shadowing.md) §1). Every upgrade ships
the current text to every user, including one whose `~/.apogee/config.yaml` was seeded a year ago.
This is the rule ADR 0023 §8 rejected; it is superseded here, on evidence §8 did not have — the
seeded file is not a maintainable channel, and the orientation block already proved that
harness-owned text with no per-install freeze is the shape that works.

**2. Resolution is a four-rung ladder, first hit wins.** A matching `system-prompt-models` entry >
the top-level `system-prompt-text` / `system-prompt-file` > the **embedded default**, when
`use-default-prompt` is not `false` > nothing. ADR 0023 §2's whole-entry replacement is unchanged
and now extends down the whole ladder: a rung that hits supplies the **entire** prompt. The embedded
default is never appended to, merged into, or filled in behind an explicit prompt — half a prompt is
still not a prompt.

**3. `use-default-prompt` is a bool, default `true`, and governs only the nothing-configured case.**
It is the off switch ADR 0023 §8 spelled as "delete the key", restated as a key of its own now that
deleting the key means *fall through to the default*. It has no effect whatsoever when text or a
file is configured at either level: an explicit prompt was already replacing everything below it.
Setting it `false` with nothing configured restores the pre-0.9 promptless run — the fourth rung —
which is exactly where ADR 0023 §6's "`""` seeds **nothing**" anchor is now reached from.

**4. Migration is a silent keep.** An existing config carrying `system-prompt-text:` — the key ADR
0023 §8 seeded active — is *explicit configuration* by rung 2 and wins, so an upgrading user's
prompt does not change under them and no migration prompt, warning or rewrite is needed. New seeds
ship the key **commented out**: a fresh install runs on the embedded default, and the file shows the
reader where their own text would go. The user who wants the shipped text as a starting point gets
it from the settings editor (§7), not from a stale copy on disk.

**5. The placement rule: four homes, one question each.** Where a new sentence of guidance belongs
is decided by what *kind* of sentence it is, not by which surface is convenient:

- a **fact about this host** the model cannot work without — a path, a directory, a tool that
  reaches it — goes in the **orientation block**: engine-composed, unmissable, not editable away.
- **standing steering** — who the assistant is, how it works in general, what it does before it
  edits — goes in the **embedded default template**: user-replaceable in full, refreshed by every
  upgrade.
- behaviour that must be **gated per model and measured against the floor** goes in a
  **Mechanism** ([ADR 0003](0003-mechanisms-are-a-constraint-declared-registry-not-a-fixed-pipeline.md)):
  catalogued, off under Bypass, enabled on bench evidence.
- **task-shaped** instruction wanted for one turn and not the next goes in a **Skill**
  (ADR 0065): invoked, turn-local, never standing.

The rule is a decision procedure, not a taxonomy: a sentence that fits two homes belongs in the
higher one on that list, because the higher home is the one whose delivery is guaranteed.

**6. The default prompt is config-tier, and therefore part of the Bypass floor in both arms.** It is
**not** a Mechanism: it is not catalogued, not gated per model, and not switched off by `--bypass`
([ADR 0006](0006-bypass-mode-is-the-mechanisms-off-floor.md)). A bench arm measuring Mechanisms
therefore runs the same default prompt on both sides, which is what keeps the arm honest — the floor
the Mechanisms must not fall below is a floor *with* the base guidance on it. The consequence is a
standing obligation on the text itself: a default prompt that makes any model worse has moved the
floor down and is a defect in the same sense a Mechanism regression is, catchable only by benching
the prompt against `use-default-prompt: false`.

**7. One universal base, edited through the settings editor, never exported.** The embedded default
is **model-agnostic** — per-model steering stays where ADR 0023 §2 put it (`system-prompt-models`)
and per-model *behaviour* stays with Mechanisms; there is no per-model default variant to pick
between and no tier detection. The editing seam is the settings editor, which pre-fills the embedded
text when nothing is configured, so editing the default is "open it, change a line, save" and the
saved result is ordinary rung-2 configuration. There is deliberately **no export command** for the
prompt (unlike `/color-scheme export`): a second copy on disk re-freezes exactly what §1 unfroze,
and the editor already hands the user the bytes.

## Considered options

- **Keep ADR 0023 §8's seeded-file default** — rejected, by the whole Context above: it is frozen at
  first run, so the text cannot be improved for anyone already running.
- **Re-seed or merge the shipped text into `~/.apogee/config.yaml` on upgrade** — rejected.
  `seedConfig`'s never-overwrite is load-bearing (it is what makes the config file safe to hand-edit
  at all), and a merge would have to guess which of the user's lines were theirs.
- **An export command mirroring `/color-scheme export`** — rejected. The scheme case exports a
  file the resolver then reads *instead* of the built-in, which is the freeze this ADR removes.
  Pre-filling the editor gives the same starting point without creating a stale copy.
- **Make the default prompt a Mechanism** so it can be gated per model — rejected by §6. A Mechanism
  is measured against a floor; the base guidance *is* part of the floor, and a floor that switches
  itself off under Bypass is not a floor.
- **Append the embedded default beneath an explicit prompt** (so a user gets both) — rejected with
  ADR 0023 §2's merge alternatives: order and duplicate-persona questions have no obviously right
  answer, and a user who wants both can paste.
- **Per-model embedded defaults** (a short variant for small models, a long one for large) — not
  built here. It is ADR 0021's parked automatic-tiering idea in new clothes, still evidence-free;
  `system-prompt-models` is the manual lever until measurement justifies more.
- **Ship the guidance as an always-loaded skill instead** — rejected by §5: a skill is invoked and
  turn-local by ADR 0027, and an always-loaded one is a standing prompt wearing the wrong word.

## Consequences

- **ADR 0023 §8 is superseded in part and gains a pointer note.** Its "no compiled-in fallback"
  sentence and its "A compiled-in default prompt" rejected alternative no longer hold; the rest of
  §8 — that the shipped template sets exactly one key, and that every other key parses to nothing —
  is unaffected, and the key it sets is now commented out (§4).
- **A stock run now seeds a system message.** ADR 0023 §6's byte-identical native anchor is reached
  only with `use-default-prompt: false` and no configured prompt or context files; the anchor test
  states that condition rather than assuming it. This is the one behaviour change an upgrade brings
  to a user who configured nothing, and `use-default-prompt` is its opt-out.
- **The default text is now apogee's to get right, continuously.** Because it ships with the binary,
  a bad sentence reaches every user on the next upgrade — the same exposure the orientation block
  carries, and the reason §6 makes prompt quality a floor concern rather than a taste question.
- **ADR 0023's marker-phrase suppression residual becomes checkable.** A user prompt containing a
  Mechanism's marker silently suppresses that directive; the *default* prompt is text apogee writes,
  so it can be kept clear of the catalogued markers by inspection instead of hope.
- **The Budget measures the default like any other prompt** (ADR 0023 Consequences): a stock small
  window now spends the default's tokens on every request, which is the cost §5's "steering, not
  facts" boundary exists to keep bounded.
- **CONTEXT.md's *System prompt* entry gains the embedded default and the ladder**, and the
  placement rule of §5 is the answer to "where does this sentence go?" for every later wave.
