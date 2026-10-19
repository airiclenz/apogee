---
title: ADR 0076 decision
---

**1. One vocabulary, one type, one dispatcher.** Every point the loop passes is a **Moment**, of two
kinds: a **seam** (in-loop, synchronous, the payload is an editable working value — `pre-request`,
`post-response`, `pre-tool-exec`, `post-tool-result`, `history-rewrite`) and a **notice** (post-hoc,
the payload is sealed — `exchange-finished`, `turn-finished`, `file-changed`, `approval-requested`,
`approval-decided`, `error`, additive). Every seam publishes a notice when it closes. A **Reaction**
is `{id, origin, class, on: [moments], handler}`; `origin` is `engine` (a builtin, or a Go reaction
the bench registers in-process through the facade) or `user` (configured); `handler` is a Go func,
an argv list or a webhook. The **lab layer goes**: the hook API, the registry, `Deps`, `register`,
`SwapCatalogue`, `Config.EnableMechanisms`, the `mechanisms:` key and the `/settings` mechanisms row
are deleted, and runtime self-regulation (strikes, Turn Budget) leaves the core — no shipped row
ever needed it, and the bench arms an engine-origin reaction without a catalogue.
`MechanismFiredEvent` and `FloorGuardEvent` fold into one **`ReactionFiredEvent`** keyed by reaction
id. The `internal/floor` policy stays byte-for-byte; the guards' ratified order (salvage first, no
short-circuit — ADR 0071 amendment) stays. This supersedes ADR 0071 decision 4 and its rejected
alternative B.

**2. The policy matrix.** The four exclusive rungs become an executable matrix; exclusivity survives
as *one reaction, one cell*.

| origin ↓ / class → | observe | advise | gate | shape (view) | shape (work) |
|---|---|---|---|---|---|
| engine (builtin or bench-armed) | ✓ | ✓ | ✓ | ✓ | ✓ |
| user | ✓ | ✓ | ✓ | ✓ (reserved) | ✗ |

- **observe** — no return. Today's Hook.
- **advise** — returns text that becomes model-visible context: fenced, capped, fail-open.
- **gate** — returns allow / deny / ask at `pre-tool-exec`, implemented as a stage of the existing
  Approver, never as a seam edit. Deny text stays engine-authored. A user script saying No is the
  same act as a human saying No, so it does not touch the floor.
- **shape (view)** — edits what the model *sees* (`post-tool-result`, `pre-request`,
  `history-rewrite`). The seven Floor guards are the engine-origin builtins of this class.
- **shape (work)** — edits what the model *does*: tool-call arguments at `pre-tool-exec`. **Engine
  only.** Denied to users on evidence: once a later reaction can mutate arguments an earlier gate
  approved, no gate in the system is sound.
- **continue** is **not a class**. Every rival that shipped continuation control wedged and shipped
  a loop breaker late; not building it is the cheapest safety decision available.

The line the old rungs shared survives as the user row: a user reaction may change the model's
*view* and may say No; it may not change the model's *work* or make its *choices*. This supersedes
ADR 0073 decision 2 and its rejections of "tool-call policy or veto hooks" and "post-edit lint fed
back to the model".

**3. Day-one user cells: observe, advise, gate.** The user `shape (view)` cell is **reserved**: it is
in the matrix, no case has yet asked for it, and an edit leaves no span for the provenance ledger
(decision 6) to record — it needs its own design and its own grill before it ships. `mcp:` handlers
are **reserved** likewise: a reaction calling an MCP tool at a seam collides with the Approver (a
tool needing approval would prompt inside a prompt), so the key is held and the posture decided when
a case arrives. Day one is argv and webhook.

**4. Two lanes; the class picks the lane.** The **sync lane** runs on the loop goroutine at a seam,
engine before user, each reaction under a recover boundary and a deadline; outcomes fold to
`(retry, inject, edit)`. The **async lane** is today's Runner: one bounded, ordered queue per
reaction, drop-newest under overload, never waited on. `observe` runs async; `advise`, `gate` and
`shape` run sync. `EventSink.Emit` stays return-less and the tree-wide serialising mutex is
untouched: a reaction that must answer runs *before* the Moment is published, never on the stream.
This supersedes ADR 0073 decision 7 ("the engine never waits on a Hook") for the sync classes only,
and decision 4's "no `pre-*` event, ever": seams are Moments, and a user gate at `pre-tool-exec` is
an Approver stage rather than user-authored policy at a lab seam.

**5. Invariant 4 is satisfied by construction, and one arm admits the advise cell.** Every handler
registers on one core, so any reaction is bench-drivable in-process and the ledger attributes its
effect to a reaction id — that is what ADR 0031 invariant 4 asks for. What a per-user script
*returns* is not a behaviour the bench can own; it carries the standing obligation ADR 0064 §6
already places on a user's configured prompt: content that makes a model worse has moved the floor
down and is a defect. The **admission arm** for the advise cell is therefore the machinery, once: an
argv advise reaction returning a **fixed neutral sentence** at `post-tool-result`, measured against
Bypass under ADR 0009 on the target class. Passing proves the
injection path — slot, fence, cap — is not itself a regression. The user advise cell ships only
after this arm passes; no content arm is required or owned by the bench.

**6. The advise slot, and the provenance ledger.** Advise text never enters the system prompt. For a
tool-shaped Moment (`post-tool-result`, `file-changed`) it lands as a fenced **trailer on the
closing tool result**; for any other Moment it is a fenced user-role message appended at the tail.
Both positions are role-safe under strict chat templates and leave the request prefix untouched, so
a local server's prefix cache survives the Turn; neither sits beside host-authored orientation, which
closes the forgery adjacency `Request.InjectContext`'s system-prompt fold created. Every injected
span records `{reaction, origin, moment, turn}` in a **provenance ledger**: the fence header is
derived from provenance, so nothing out-of-process can forge an engine header; spans are
**ephemeral**, dropped on resume, so a replay never re-reads a stale SHA or timestamp; the bench
attributes effect by reaction id; `/settings` can show what the model saw this Turn.

> *Amendment (2026-09-20, plan `2026-09-19 - 01` items 15–16, bead `apogee-d6t`):* the ledger's
> human-facing surface is **`/advice`, not `/settings`** — the sentence above is superseded on that
> point alone. `/settings` is a pane over the config key registry (its rows are the keys a session
> resolved, ADR 0035), and what the model was handed is not a configuration value. The TUI folds
> every `ReactionFiredEvent` whose action is `advise` (an armed advise entry, Detail = the capped
> fenced text) or `notice` (the context-fill notice's rung, D7 addendum / ADR 0077) onto an advice
> board and shows it as a fourth report pane beside `/thinking`, `/usage` and `/inspect`: the whole
> session, grouped by run and Turn, each firing headed `<reaction> (<origin> origin) @ <moment>` —
> the provenance row, in the pane's words. The verb re-opens on the newest firing and never toggles;
> engine notes (the addendum below) fire no reaction and are absent from it.

> *Amendment (2026-10-19, `adr-0076-amend-1019`):* for the `session-started` notice the "any other
> Moment" slot is no appended message: the span is a fenced block inside the session's next user
> message, ahead of the prompt, ephemeral as above. The user advise cell on it waits for its own
> decision-5-style admission arm.

**7. Deadlines, cap and failure.** `advise` defaults to **10s** and is **fail-open** — no advice is
just no advice. `gate` defaults to **5s** (it runs on every tool call) and on timeout or crash
**escalates to ask**: a broken guard becomes a human question, never a silent allow and never a hard
deny. Advise output is capped at **8 KiB** and truncated with a marker, never spilled to a file. Every
entry may set its own `timeout:`. Failures report through the Driver-supplied reporter as Hook
failures do today (ADR 0073 decision 8 stands), never as an `ErrorEvent`.

**8. Trust posture is class-bound; one entry may carry more than one class.** `observe` runs as ADR
0073 decision 6 has it: the user's config, outside confinement, payload unscrubbed — an outbound
channel at the same trust as the screen. `advise` and `gate` are inbound and run **inside the
workspace exec fence** (the contract's §10 posture), and advise stdout passes the terminal tool's
secret redaction before the fence and the cap. An entry carrying `run:` and `advise:` runs two
handlers, each under its own class's posture; adding a blocking key never silently confines the
notifier beside it.

> **Note (2026-09-12, stage 3 —
> [plan `2026-09-09 - 01`](../../../../docs/plans/archived/2026-09-09%20-%2001%20-%20reaction-core-stage-3-plan.md)).** This
> decision's reading as shipped. *The redactor:* "the terminal tool's secret redaction" is
> `tools.RedactSecrets(text, secretEnv)`, an output-side value redactor that replaces every non-empty
> configured secret value (`Config.SecretEnvVars`, the `api-key-env` names of ADR 0047) with
> `[redacted]`; advise stdout passes it before the cap and the fence. *The permit row:* "inside the
> workspace exec fence" is the contract's §10.4 row for a user-origin sync reaction — it spawns in
> **every mode** under a `SubprocessPermit`, with `Confinement` = the workspace box when
> `confine-to-workspace` is on and the Confiner has caps, nil when it is off; on but no caps ⇒ no
> permit, the handler fails (`workspace confinement is unavailable on this host`) and gate ⇒ ask,
> advise ⇒ nothing. The mode is not a term because the reaction is the user's own configuration, not
> anything the model chose; §10.3's post-response row keeps its mode term. The sync lane's handlers are
> argv only — a sync-lane webhook is bead `apogee-1d8`.
>
> *Amendment (2026-09-19, plan `2026-09-19 - 01` items 12–13, bead `apogee-1d8`):* the "argv only"
> sentence is **superseded**. `advise:` and `gate:` take the same two shapes `run:` does — an argv
> list or a webhook mapping `{url:, headers:, headers-env:}` — and one loader path serves all three
> keys, refusing anything else by the key's own sentence (`advise: is an argv list or a webhook
> mapping {url:, headers:, headers-env:}`). A sync-lane webhook POSTs the identical seam document
> `runSyncArgv` writes to a command's stdin and hands the seam the reply body on the same terms: an
> advise reply is redacted and capped downstream as stdout is, an empty body injects nothing; a gate
> reply's first line is the verdict, parsed as stdout is, and a status code is never a verdict.
> Every failure — non-2xx, transport, deadline, unset `headers-env:` — reads as a command's failure
> does: advise fails open with one reported row, gate escalates to `ask`. The permit row above is the
> argv handler's alone: a webhook spawns nothing, so it takes no permit and the confinement table
> does not apply (contract §10.4). No URL guard is added, on parity with the observe webhook.
>
> **Amended 2026-10-12 (plan `2026-10-12 - 00` item 2).** "No URL guard is added" is
> **superseded**: since commit `2d9e79e6` both the sync-lane webhook (`(*Agent).runSyncWebhook`)
> and the observe webhook post through the url-safety guarded client (`internal/webhook`), which
> honours the session's URL allow and deny hosts and never follows a redirect. Parity with the
> observe webhook holds — both are guarded now.
>
> *Amendment (2026-09-15, plan `2026-09-15 - 00` item 7):* the contract's §10.3 post-response row is
> **retired** — `hookExecutionCtx` is deleted and no cascade installs a `SubprocessPermit` at any
> Moment, in any mode. No shipped Reaction spawns at post-response (the row was a leftover of the
> retired lab mechanisms, ADR 0071), so a post-response handler reads absent and the seam keeps the
> refusal default of §10.2. `syncPermitCtx`'s §10.4 row is now the engine's only permit row.

**9. Bypass switches off the model-shaping classes.** `--bypass` turns off every `advise` and
`shape` reaction of user or bench-armed origin; `observe`, `gate` and the seven Floor guards stay.
That keeps ADR 0006's meaning exactly — what can move the floor is off — without stripping a bench
run of the user's notifications or guards, which change nothing the floor measures. The control arm
and the shipped default remain the same agent.

**10. One config shape, three layers; execution keys need adoption.** `reactions:` is a list;
`run:` is observe, `advise:` returns text, `gate:` returns a decision; builtins appear by id with
`enabled:`. Resolution is `embedded defaults → ~/.apogee/config.yaml → <workspace>/.apogee/config.yaml`,
later layers overriding by key, `/settings` showing each value's layer. The rule that replaces
"global only" is *a clone cannot run a command before the user has seen it*: a repo layer may set
**parameters** (`enabled`, `timeout`, `on:`, `workspace:`, model profile, floor toggles) freely, and
its **execution keys** (`run:` / `advise:` / `gate:`) are **proposed, not live** until the user
adopts them. Adoption pins the entry's hash; an edit re-proposes; the repo config path is on the
tool write deny list, and the pin holds even if that deny is switched off, because `write_file`
could otherwise author a reaction that runs on the next reload. This is doctrine now; the repo
layer **ships in stage 2** (decision 13), and day one stays the global file plus the per-entry
`workspace:` filter. This supersedes ADR 0073 decision 5.

> *Amendment (2026-10-18, `adr-0076-amend-1018`):* ADR 0104 admits the repo layer's execution keys,
> `mcp-servers:` among them. For those, a repo entry never overrides a global one by key, the pin is
> canonical and covers the project files the argv names, and a repo `run:` is fenced.

**11. Migration.** The seven Floor-guard booleans **stay canonical** — they are parameters, and a
Floor guard is by definition one top-level file-only boolean — so nothing migrates. A `hooks:` list
keeps loading through one migration table as an alias for `reactions:`, with a **one-time load
notice** naming the new form. A `mechanisms:` key gets the retired-roll message.

**12. Terms.** **Reaction** and **Moment** enter the glossary. **Mechanism**, **Hook point** and
**Experimental hook** retire (a bench-armed engine-origin reaction is what the last one named).
**Hook** survives only as the colloquial alias the manual's introduction offers ("what other tools
call a hook"). **Floor guard** stays as the name of the seven engine-origin `shape (view)`
builtins.

**13. Staged route.** (1) **Core** — Moment, Reaction, one ladder, `ReactionFiredEvent`; a pure,
behaviour-identical refactor proved by a bench identity arm. (2) **Config** — `reactions:` with the
migration table, three layers, adoption pin. (3) **User cells** — observe (already shipped as
Hooks), gate, and advise once decision 5's arm passes.
