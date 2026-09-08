---
Status: accepted; decisions 2, 4, 5 and 7 and the veto and lint rejections superseded by ADR 0076
---

# Hooks are observe-only, Driver-side reactions to engine events

> **Superseded in part by [ADR 0076](0076-one-reaction-core-with-an-origin-by-class-policy-matrix.md)
> (2026-09-07).** A Hook is now the colloquial alias for a user-origin **Reaction**, and ADR 0076
> supersedes exactly these: **decision 2** (observe-only — the user row of the policy matrix gains
> `advise` and `gate`, admitted by a fixed-text machinery arm against Bypass), **decision 4**'s "no
> `pre-*` event, ever" (seams are Moments; a user gate at `pre-tool-exec` is an Approver stage),
> **decision 5** (global only — three config layers, a repo layer's execution keys proposed until
> adopted), **decision 7** for the sync classes (the engine waits on `advise` and `gate` under a
> deadline; it still never waits on `observe`), and the rejections of **tool-call policy or veto
> hooks** and **post-edit lint fed back to the model**. Decisions 1 (term, as amended), 3, 6, 8 and
> 9 and the plugin and Driver-side-feed rejections stand.

## Context

A 2026-09-06 scout asked whether apogee should grow event hooks and/or plugins. It found that the
engine already has the observation door: `domain.EventSink` (`internal/domain/events.go`) is a
typed, sealed, one-way stream every Driver reads, and an observer can attach to it with no engine
edit. It also found the real gaps: nothing today runs a user command or posts a webhook when a
run finishes, a Turn ends, a file changes, an Approval waits or an error lands. The grill the same
day settled the shape, and it settled three refusals just as firmly, because each is the door
through which this surface would otherwise drift into a second Mechanism catalogue.

Two moments users care about most were not observable: a Turn's end is a `StepResult` return
value, and an Approval is announced only after its decision. Both become additive
observation-only event variants, so the signal is the engine's and every Driver sees it — nothing
model-visible or safety-relevant may exist in one Driver's surface alone
([ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)).

## Decision

**1. Term: Hook, beside Hook point.** The user-facing thing is a **Hook** — the word every
external reader expects from git and other coding agents. The glossary's **Hook point** and
**Experimental hook** stay what they are (the Mechanism lab seam), and the glossary carries the
distinction: a Hook fires after the fact and may not act on the model; a Mechanism fires inside
the loop and may. The confinement contract's §10 subprocess permit (`RunHookSubprocess`) governs
Mechanism-spawned processes and does **not** govern Hooks.

**2. Observe-only, by construction.** A Hook is an `EventSink` decorator. `Emit` returns nothing,
so a Hook cannot veto, delay or answer. Its stdout, exit code and HTTP response are never fed to
the model, the conversation, or the Session record. The day someone wants a Hook's output in
front of the model, that is a Mechanism and goes through the bench (ADR 0031 invariant 4,
[ADR 0034](0034-the-daemon-is-an-in-repo-subcommand-over-a-declarative-trigger-action-file.md) §8).

**3. Two additive event variants.** `TurnEvent{Status, Faulted, StepCapped}` at every Turn
boundary, and `ApprovalEvent` emitted twice — requested (zero Decision) and decided — with a
phase field so existing consumers keep folding one card. Observation only, like
`SubAgentPhaseEvent`.

**4. Five Hook events in v1, all post-hoc.** `exchange-finished`, `turn-finished` (Depth 0),
`file-changed`, `approval-waiting`, `error` (any depth, depth and call-ID in the payload). No
`pre-*` event, ever: a pre-event with an exit-code veto is user-authored policy at a lab seam.
Everything else (tokens, tool calls, sub-agent phases, session saves, prune, usage) is additive
later.

> Stage 2 of [ADR 0076](0076-one-reaction-core-with-an-origin-by-class-policy-matrix.md) took the
> additive slot: the waiting spelling above is now `approval-requested`, and six notices joined it —
> `approval-decided` plus the five seam-closing `<seam>-finished` notices. Eleven notice **Moments**
> ship; the "no `pre-*` event, ever" rule is unchanged, because a seam-closing notice reports a pass
> that is already over.

**5. Global config only, workspace filter.** `hooks:` is a list in `~/.apogee/config.yaml`
(structured row, count summary, live-reloaded with the file). A repo-local hook file is
**denied**: a Hook runs commands on the user's machine, and a cloned repository must never be
able to install one — the same reason `confine-to-workspace` is global-file-only. An entry
scopes itself with `workspace:`.

**6. Exec posture.** `command:` is an argv list run directly — no shell, no interpolation —
under the `api-key-cmd` posture (exec-fenced, bounded timeout, capped output); a user who wants a
shell writes `[sh, -c, …]`. It runs **outside** confinement: it is the user's config, not a model
action. Payload is a JSON document on stdin plus a small `APOGEE_HOOK_*` env set. `webhook:` is a
POST of the same JSON, no retries (the receiver owns durability), optional `headers:` whose values
may reference an env var so no token sits in the file. The payload is not secret-scrubbed: it goes
to the user's own command or URL, the same trust as the screen — accepted.

> Stage 2 of ADR 0076 renamed the spellings, not the posture: the env set is `APOGEE_REACTION_*`
> (`APOGEE_HOOK_*` above is the historical name), the payload's `hook` field is `reaction`, and
> `command:` and `webhook:` became the two shapes — argv list, or `{url, headers, headers-env}`
> mapping — of one `run:` key. The config migration says so in its note, because a user's own
> scripts are the one half apogee cannot rewrite.

**7. Dispatch.** One bounded, ordered queue and one worker per Hook; different Hooks run in
parallel; a full queue drops the newest event and counts it; shutdown waits a bounded grace. The
engine never waits on a Hook.

**8. Failures never re-enter the stream.** The runner reports through a Driver-supplied reporter
— a de-duplicated ephemeral notice in the TUI, a log line headless and in the daemon — never as an
`ErrorEvent`, so an `error` Hook cannot fire on its own failure.

> Stage 2 of ADR 0076 keeps the rule and re-words the notice, which now reports `reaction <id>: …`.
> It covers all eleven notice **Moments**, the five seam-closing ones included: a failure on any of
> them still never re-enters the Event stream.

**9. One library, every root.** `internal/hooks`, re-exported on the public facade; wired at the
TUI bridge sink and through `run.Spec.Config.Events` for headless and daemon Firings, whose
payload carries the Schedule's id and name. A per-schedule `run: notify:` key in the daemon
envelope is deferred as the additive ADR 0034 §4 slot.

> Stage 2 of ADR 0076 renamed the package to `internal/reactions` and re-typed it over
> `domain.Reaction` and `domain.Generation` instead of an entry type of its own. The one library,
> its install sites and its re-export on the public facade are otherwise unchanged.

## Rejected

- **Plugins** — out-of-process tools are what MCP is (ADR 0031 invariant 3); in-process tools are
  already open to Go embedders ([ADR 0002](0002-tools-are-an-open-extension-point-mechanisms-are-curated.md));
  a dynamic loader is a large trust surface for a gap MCP covers.
- **Tool-call policy or veto hooks** — a user-authored guard at the pre-tool-exec seam is a second
  Mechanism surface, which [ADR 0071](0071-floor-guards-are-engine-behaviour-and-the-nudge-catalogue-retires.md) closed.
- **Post-edit lint fed back to the model** — the retired `autofix`/`syntax` rows; a benched
  Mechanism if it returns, never a Hook.
- **Driver-side feeds instead of engine variants** (a `Boundary(StepResult)` call and an Approver
  wrapper) — each Driver would have to remember both, and the one that forgot would silently lose
  those Hooks.
- **A distinct term ("Notifier")** — cleaner in the glossary, opaque to every external reader.
