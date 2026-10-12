---
title: ADR 0022 considered options
---

- **Keep saving only on quit.** Rejected: a crash or `kill -9` mid-task loses the whole session,
  and "sessions" that only exist after a clean exit are not sessions. Per-Turn is what makes the
  interrupted-task `/continue` pay off.
- **Persist the engine envelope alone (no transcript).** Rejected: resume would leave the view
  empty over a remembering engine — the forbidden "fresh view lying about the engine" — so the
  scrollback must persist too. Re-deriving the scrollback from the conversation was rejected as a
  second renderer that would inevitably drift from the real one.
- **One shared schema version for the whole record.** Rejected: it couples three layers that
  ship and evolve independently. The engine envelope is already `domain`-owned and opaque to the
  store; forcing the store to bump when the TUI changes its scrollback shape (or vice versa) is
  exactly the coupling ADR 0010's layering forbids.
- **Fork-on-resume (a resumed session writes a new file).** Rejected: with per-Turn saves it is
  silent data sprawl — every resume spawns a duplicate that then diverges. Resume continues the
  session *in place*; `/clear`/`/new` is the explicit "start a fresh one".
- **A cross-instance file lock now.** Deferred: ids are per-instance unique so the only clobber
  is the same session opened twice, a narrow case. Documented as last-write-wins with a recorded
  TODO rather than shipping a lock nobody has yet needed. *Superseded 2026-09-19 (`apogee-3b3`):*
  the lock shipped as the live-session hold — see decision 7's note.
- **LLM-generated titles.** Rejected for v1: the first-user-message heuristic (≤50 chars,
  word-boundary truncate) costs no tokens and is renameable inline. An LLM title is an additive
  follow-on, not a dependency of the feature.

### Addendum (2026-07-31) — LLM titles adopted as a cosmetic out-of-band call

The **LLM-generated titles** rejection immediately above is **reversed**, ratified with the owner
in the 2026-07-31 grill session. Nothing in the Decision changes: the first-user-message heuristic
still stamps the first Save and remains the fallback, and the record's shape, save cadence, and
per-layer versioning are untouched. What is added is a *naming call* that renames a record once,
shortly after it is born.

> **Amended 2026-09-01 by ADR 0068.**
> Everything below is the SESSION title's and stands exactly as ratified. What 0068 adds is a
> sibling one level down — the naming of a DELEGATION the model left unnamed — which keeps every
> property recorded here (cosmetic, fires at no Hook point, never shapes the primary call, silent
> on failure, gated by the same `auto-title` key) except one: it announces its result as a single
> `SubAgentNamedEvent`, so the "emits no `TokenEvent` or `UsageEvent`" claim below is the session
> title's alone. Its seam is in the engine rather than on `tui.Options`, because a delegation is
> the engine's object and every Driver shows one.

- **It is a cosmetic call — a category of its own.** The naming completion is neither a
  **Mechanism** (it fires at no Hook point and never shapes the primary call, so it is exempt from
  Bypass reasoning and from the ADR 0009 non-inferiority gate) nor
  structural like compaction (nothing breaks without it — the heuristic title stands). It is
  **not a Turn**: it emits no `TokenEvent` or `UsageEvent`, never enters the transcript, and
  never moves the context gauge. It lives entirely in the TUI and the composition root, so the
  **bench and embedder path is untouched** — anything that does not construct `tui.Options`
  cannot fire it
  (ADR 0001).
- **It fires at first-prompt submit, in parallel with the main call.** Under the single-slot
  server posture
  (ADR 0024) it
  therefore queues behind Turn 1 and runs between Turns 1 and 2 — the cheapest KV-eviction point
  in the session, because context is at its smallest there. Its request timeout is correspondingly
  generous: waiting out the whole first stream is the expected case, not a fault.
- **It never goes through the Agent.** The Agent is single-goroutine
  (ADR 0011), so the call rides a
  **nil-able func seam on `tui.Options`** backed by its own out-of-band `provider.Client` built
  from the session's *current* server + model binding — the `probe.Chat` pattern. There is no
  separate naming endpoint or naming model: a session names itself with the model it is running. A
  nil seam means automatic naming never fires and the on-demand form reports unavailability —
  never an error.
- **`Rename` is the only writer.** `Save` ignores its title argument after the first call, so a
  generated title lands through the existing `Rename` path; a result that arrives before the first
  Save has minted an id is **stashed and applied at the first save-complete**.
- **A user's title always wins (never-clobber).** Any user-initiated rename (the browser's `^r`,
  or `/rename <text>`) marks the title as touched, and a late-landing *automatic* title is then
  dropped. An explicitly requested regeneration (bare `/rename`) is the exception — the user
  asked for it, so it applies and leaves the mark set. Naming fires once per new Session record,
  including after the `/clear` / `/new` rotation of Decision 4, and never on a resumed session.
- **The two naming forms read different amounts of the session** (added 2026-08-01). The automatic
  call reads the first prompt — the only one that exists when it fires — while an explicitly
  requested regeneration (bare `/rename`) reads a bounded, budget-capped window of the user side
  of the transcript: the opening request plus the most recent, filled inward from the newest. That
  form is instructed to name the **dominant thread, biased recent**, because a session that moved
  on must be findable by what it moved to.
- **The gate is a config key, default on.** `auto-title` (flat, config-file only, nil ⇒ true)
  gates only the *automatic* firing; the seam stays wired so `/rename` regenerates on demand even
  when it is off. The automatic path fails **silently** to the heuristic title — a cosmetic
  maintenance nicety must never nag.
