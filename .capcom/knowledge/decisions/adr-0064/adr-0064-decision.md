---
title: ADR 0064 decision
---

**1. The default template is embedded in the binary.** The bytes live in the source tree and are
compiled in with `go:embed`, exactly as the built-in color schemes are
(ADR 0040 §1). Every upgrade ships
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
  **Mechanism** (ADR 0003):
  catalogued, off under Bypass, enabled on bench evidence.
- **task-shaped** instruction wanted for one turn and not the next goes in a **Skill**
  (ADR 0065): invoked, turn-local, never standing.

The rule is a decision procedure, not a taxonomy: a sentence that fits two homes belongs in the
higher one on that list, because the higher home is the one whose delivery is guaranteed.

**6. The default prompt is config-tier, and therefore part of the Bypass floor in both arms.** It is
**not** a Mechanism: it is not catalogued, not gated per model, and not switched off by `--bypass`
(ADR 0006). A bench arm measuring Mechanisms
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
