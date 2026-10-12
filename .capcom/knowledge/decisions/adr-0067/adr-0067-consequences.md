---
title: ADR 0067 consequences
---

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
