---
Status: accepted
Amends: ADR 0023 (decision 1's key count)
---

# `system-prompt-layers` are an explicit additive channel

## Context

[ADR 0023](0023-the-system-prompt-is-a-configured-template-rendered-per-request.md) §2 made the
system prompt **whole-entry replacement**, and
[ADR 0064](0064-the-system-prompt-ships-an-embedded-default.md) §2 extended that rule down the
whole four-rung ladder: a rung that hits supplies the **entire** prompt, and the embedded default
"is never appended to, merged into, or filled in behind an explicit prompt — half a prompt is
still not a prompt". That rule is right and is not in question here. What it leaves the user
without is a way to say the *other* thing: keep whatever prompt you would have sent, and add
these lines to it.

The gap shows as a **one-flag-two-meanings surprise**. `use-default-prompt` reads, to a user
scanning the config file, as "use apogee's default prompt" — so a user who sets it `true` (or
leaves it at its default) alongside their own `system-prompt-text:` expects both. ADR 0064 §3
gives it exactly one meaning — it governs the nothing-configured case and "has no effect
whatsoever when text or a file is configured at either level" — so the second reading is silently
wrong: the user's five lines *replaced* fifty they never saw. Nothing in the file says so, because
the thing that vanished was never in the file.

The workaround the surface offers is the **paste-into-the-editor** one, and it is the one thing
ADR 0064 set out to end. §7 pre-fills the settings editor with the embedded default so a user can
"open it, change a line, save"; a user who wants the default *plus* their own lines saves the
pre-filled bytes with their lines appended, and the saved result is ordinary rung-2 configuration.
That is a second copy of the default on disk, frozen at the day it was pasted — precisely the
per-install freeze §1 unfroze, re-created by hand and now invisible to every upgrade. The same
applies to a user's own standing text they want under *both* a per-model entry and the global one:
the only way to have one sentence in two prompts is to write it twice.

There is also no additive channel for text that is **not** persona at all. The engine has one —
the Orientation block (ADR 0023's 2026-08-25 amendment) — but it is engine-owned host facts by
construction, and ADR 0064 §5's placement rule sends everything else to the template, a Mechanism
or a Skill. A user's own standing addendum ("this box has no network", "prefer `rg`") fits none of
them, and today can only be spliced into whichever prompt is selected.

This ADR records the design the owner ratified on 2026-09-01, implemented by
`docs/plans/2026-09-01 - 01 - system-prompt-layers-plan.md`. It **supersedes** ADR 0023 §1's
top-level key count and nothing else: the ladder, whole-entry replacement and the embedded
default's fallback-only role all stand exactly as written.

## Decision

**A new top-level `system-prompt-layers:` key holds an ordered list of text fragments that are
appended, in listed order, to whatever prompt the ladder selected. If it is enabled it is sent,
and nothing the user did not enable is sent.**

**1. The key is `system-prompt-layers:`, a list, beside its siblings.** It is top-level and
file-only, following the same convention as the three keys ADR 0023 §1 named — a standing
statement about how you work, so no flag and no environment variable. It sits beside
`system-prompt-text:` and `system-prompt-file:` in the config file and in `/settings`. The **full
top-level key set** is therefore five: `system-prompt-text`, `system-prompt-file`,
`system-prompt-models`, `system-prompt-layers`, `use-default-prompt`. This supersedes ADR 0023
§1's "three top-level keys" count (already amended to four by ADR 0064 §3); the character of every
one of those keys is unchanged.

**2. Layers are NOT a fifth rung — they append AFTER the selected prompt.** Rung selection runs
exactly as ADR 0064 §2 describes and picks at most one whole prompt; the layers are then appended
to whatever it picked. A layer therefore **never competes with** the rung it lands behind, and a
per-model entry that replaces the global prompt does **not** replace the layers: standing text a
user wants under every model is written once and rides every rung. Whole-entry replacement is
intact because it is a rule about the *selected* prompt, and the layers are not one — they are a
second, explicitly enabled channel.

**3. Layers never trigger the embedded default, and never suppress it.** The default's role stays
exactly what ADR 0064 §2–§3 made it: the third rung, fired when **nothing at all** is configured
and `use-default-prompt` is not `false`. Configuring layers alone is not configuring a prompt, so
the question the default answers — "did the user configure a prompt?" — is answered by the
ladder's own inputs and by nothing the layers do. Two boundary readings follow and are binding:

- **Layers alone, `use-default-prompt` untouched**: the ladder selects nothing (no text, no file,
  no matching model entry), so the run sends **the layers alone**. The default does not fire
  behind them. This is the reading that keeps the flag honest: a layer is an explicit act, and
  firing the default underneath it would be apogee sending fifty lines the user did not ask for —
  the very surprise §1 of the Context describes, inverted.
- **A prompt plus layers**: the selected prompt, then the layers. `use-default-prompt` is not
  consulted at all, as ADR 0064 §3 already says of the configured case.

The composition this does **not** offer is "the shipped default, plus my lines". By ADR 0064 §2
the default is a rung, and a rung that fires supplies the whole prompt; layering behind it would
be appending to a fallback, which stops being a fallback the moment anything rides on it. A user
who wants the shipped text as a base takes ADR 0064 §7's editor seam and owns the result as rung-2
configuration — see the rejected alternatives for why that stays the answer.

**4. Composition order is fixed and shallow: selected prompt first, then layers in listed YAML
order, joined by a blank line.** The parts are joined with `"\n\n"` — the separator a reader of
the rendered prompt sees as a paragraph break, and the one the engine already produces between the
standing sources it composes. Order is the file's order, top to bottom, with no sorting, no
priority key and no per-layer placement: the user reads the composition off the config file, in
the order it is written. Nothing is deduplicated and nothing is trimmed — a layer that repeats the
prompt's own sentence sends it twice, which is a defect in the config and reads as one.

**5. A layer states exactly one of `text:` or `file:`.** The shape mirrors ADR 0023 §1's two
spellings, one entry at a time:

```yaml
system-prompt-layers:
  - text: "This box has no network. Prefer rg over grep."
  - file: house-conventions.md
```

A `file:` resolves exactly as `system-prompt-file` does — a leading `~` expands, and a relative
path resolves against the **apogee home**, never the workspace, for ADR 0023 §1's reason: the key
lives in a global file that travels with the home. Stating **both** spellings in one entry, or
**neither**, is a defect in the file itself and is a validation error naming the offending index
(`system-prompt-layers[N]`), the same class of check ADR 0023 §3 puts at every level while config
resolves. Every layer is a **template** in the same closed four-placeholder language (ADR 0023
§4); an unknown placeholder in a layer is a startup error naming the index and the known four,
never raw braces on the wire.

Unlike a non-matching `system-prompt-models` entry, a layer's file **is** read on every run that
resolves the prompt: a layer is unconditional by construction, so ADR 0023 §2's travelling-config
argument does not apply to it — an unreadable layer file is a startup error naming the index and
the path.

## Considered options

- **Make the existing keys additive by default** — so `system-prompt-text` is appended to the
  embedded default unless the user opts out. Rejected on two counts. It stacks **two personas**:
  the shipped default opens by saying who the assistant is, and a user's own prompt usually opens
  the same way, so the model reads two answers to one question — the duplicate-persona objection
  ADR 0023 already used to reject merging per-model entries, and ADR 0064 re-used to reject
  appending the default beneath an explicit prompt. And it **moves the floor**: by ADR 0064 §6 the
  default prompt is config-tier and part of the Bypass floor in both bench arms, so making it ride
  along under every configured prompt changes what every existing config sends, silently, on
  upgrade — the same "changes nothing for anyone already running" property ADR 0023 §8 protected
  and ADR 0064 §4 preserved as a silent keep. An additive channel has to be one the user **turned
  on**.
- **Let layers stack behind the embedded default** (leave every prompt key unset and have the
  default fire as rung 3 with layers appended) — rejected, as §3 states. It is the previous option
  wearing a list: it re-introduces two-persona stacking and re-opens the floor question, with the
  extra hazard that the text being appended to is text the user has never read. A user who wants
  the shipped default as a *base* takes ADR 0064 §7's editor seam and owns the result as rung-2
  configuration — which is an explicit, visible copy, not an invisible composition.
- **Fold layering into `system-prompt-text` as a list-or-string** — accept either a scalar or a
  sequence under the existing key. Rejected. It makes one key mean two things by its YAML *type*,
  which is the failure mode this ADR exists to remove; it collides with ADR 0023 §2's whole-entry
  replacement, since a per-model entry replacing a *list* would have to answer whether it replaces
  every element or the last; and it breaks the settings surface, where `system-prompt-text` is a
  multi-line editor row (ADR 0037) that cannot show a list.
- **Composition in the settings editor only** — no key at all; the user pastes what they want
  into the pre-filled multi-line editor. Rejected: this is the status quo, and the Context says
  why it fails. The paste is a frozen copy of the embedded default, invisible to every later
  upgrade, and it cannot express "this sentence under every model" at all.
- **Per-model layers** (a `layers:` list inside a `system-prompt-models` entry) — not built here.
  The global list already covers the case the surprise names, and a per-model list would
  immediately raise the replace-or-concatenate question §2 avoids by having exactly one list. It
  stays open; the key's shape does not foreclose it.
- **A prepend channel, or per-layer placement** (before/after the selected prompt) — rejected as
  unevidenced surface. Appending is the order that keeps the selected prompt's opening persona
  first, which is the position a small model weights most; a user who needs their text first can
  put it in the prompt.
- **An `export`/`import` command for layers** — rejected with ADR 0064 §7's reasoning: a second
  copy on disk is the freeze. A layer's `file:` spelling already puts a layer in a file the user
  owns.

## Consequences

- **Zero engine change.** Layers are composed while config resolves, in `ResolveSystemPrompt`,
  and the resolved value keeps its shape: one rendered template string on `Config.SystemPrompt`.
  `internal/agent` is untouched — `standingSystem()` receives a longer template and composes it
  into position 0 exactly as before, so ADR 0023 §6's wire order (prompt → orientation → context
  files → mechanism directives → tool block) and §7's sub-agent inheritance hold with no wiring of
  their own.
- **The Budget and the prefix KV cache inherit through the existing channel.** Because the layers
  arrive as part of the same template, `req.State()` measures them like any other prompt bytes
  (ADR 0023 Consequences) and the predictive overflow guard stays honest. Cache stability is
  likewise inherited: the composed string is constant within a day and within a session, so a
  local server's prefix cache survives exactly as far as ADR 0023 §4's `{{datetime}}` boundary
  lets it — layers add tokens, not churn. The cost is the plain one: a small window now spends the
  layers' tokens on every request, which is why the channel is opt-in and per-entry.
- **An empty or absent list is inert.** Every existing config resolves byte-identically, so the
  ADR 0064 §2 ladder's pinned resolutions and ADR 0023 §6's "`""` seeds **nothing**" anchor are
  untouched. This is what makes the change safe to ship into installs that will never set the key.
- **The settings surface gains one row.** `system-prompt-layers` is a structured list key, so it
  takes the external-edit affordance `system-prompt-models` already takes (ADR 0037): the row
  exists so the key is visible and its effective value readable, and editing is a suspend to
  `$EDITOR` at the key's line. There is deliberately no in-pane list editor for it here.
- **The editor's pre-fill seam narrows to what the run actually sends.** ADR 0064 §7 pre-fills
  the multi-line `system-prompt-text` editor with the embedded default when nothing is configured;
  with layers in the file the run is no longer sending the default, so the pre-fill must not offer
  it — the editor opens on what a save would replace. The seam itself is preserved: a resolution
  that really is the embedded default still seeds it.
- **ADR 0023 §1's key count is superseded, and the ADR 0064 ladder text is not.** Readers of 0023
  §1 and 0064 §2 see three and four keys respectively; both are historical counts and are left as
  written, with this record as the current one. ADR 0037's live-apply list and ADR 0035's
  persist-one-key-per-edit list name the system-prompt keys individually; `system-prompt-layers`
  joins `system-prompt-models`'s external-edit class in both and those records are left as
  ratified.
- **CONTEXT.md's *System prompt* entry gains the layers term**, worded to match §2–§4, and
  `docs/manual/configuration.md`'s "The system prompt" section gains the user-facing Layering
  passage. No other document states the key's behaviour; `README.md` stays the front door.
- **A layer is standing text, so ADR 0064 §5's placement rule still applies to its content.** The
  key is a *channel*, not a new home: a host fact still belongs in the Orientation block, gated
  and measured behaviour still belongs in a Mechanism, and task-shaped instruction still belongs
  in a Skill. What the layers add is the ability to keep standing steering in more than one piece.
- **ADR 0023's marker-phrase suppression residual widens slightly.** `Request.AppendToSystem` is
  idempotent by a `strings.Contains` over the first system message, so a *layer* containing a
  catalogued Mechanism's marker phrase suppresses that directive exactly as a prompt containing it
  does. The blast radius, the reasoning and the disposition are unchanged from ADR 0023's
  Consequences — accepted, not fixed, and recorded in `ISSUES.md`.
