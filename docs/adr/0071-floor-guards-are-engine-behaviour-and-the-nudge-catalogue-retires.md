---
Status: accepted
Supersedes: ADR 0009 for structural behaviour; ADR 0070 Option C
Amends: ADR 0006, ADR 0014, ADR 0015 D1, ADR 0016 (2026-08-29 amendment)
---

# Floor guards are engine behaviour; the nudge catalogue retires

## Context

The [mechanism catalogue](../design/mechanism-catalogue.md) holds twenty rows behind one gate:
rule **D1** — a Mechanism ships **off** until an A/B bench run proves it a win, under the
per-Mechanism non-inferiority test of [ADR 0009](0009-the-ab-decision-rule.md). That gate was
written for *tuning*: behaviour that changes what a model is shown or asked **before** the model
has failed at anything, and that can therefore help one model and hurt the next. For tuning it is
the right gate, and it stays.

Two months of catalogue and four port waves later, the gate has produced almost no evidence.
One row carries a real measurement — `cached_content_intercept`, help rate **0.73** (11/4/1),
`repeated_tool_call` 0.91→0.15 per run on gpt-oss-20b, inert-but-correct on gemma
([`catalogue.md:191`](../design/mechanism-catalogue.md)). One curated set exists (the gemma
Validated entry). Every other row's verdict column still reads `pending`. The parked-item brief
that opened this wave states the position plainly: freeze the catalogue, promote the structural
few, retire the nudges — "`read_repeat`/`cached_content_intercept` (the one row with real
evidence: help rate 0.73 on gpt-oss-20b)"
(`docs/handoffs/2026-09-02 - 00 - harness-over-mechanisms-parked-items.md`, parked item 1).

The reason the gate produced no evidence is that most rows were never on the distribution it
measures. [ADR 0070](0070-off-ramp-mechanisms-ship-on-by-default.md) already made that argument for
two rows and carved them out of D1: an **off-ramp** fires only *after* a Turn has already failed,
so there is no arm in which withholding it is the safer choice, and D1's question — "does this
tuning help or hurt across a Turn distribution?" — has no meaning for it. Its Option C ("stop
calling them Mechanisms; make them structure") was rejected then because the change bought only a
wording difference.

That rejection no longer holds, because the population it applied to has grown. Six rows share the
off-ramp shape in substance if not in Capability: they change only what the model sees after its
own failure, or shape the request without steering it. They are the rows every model benefits from,
the rows the bench cannot meaningfully arm against, and — because they ship off under D1 — the rows
a stock install does not run. Meanwhile the fourteen tuning rows sit unproven, unshipped and
unbenched, holding a hook API, a registry, a `/settings` list, a Validated-set surface and a
twenty-row catalogue in place for behaviour nobody runs.

The catalogue's own retirement rule blocks the obvious exit. [ADR 0016](0016-curation-is-per-model-validated-sets-keyed-by-fingerprint.md)'s
2026-08-29 amendment says a row "only ever joins `mechanisms.RetiredIDs()` when it was **inert by
construction** at the moment it was retired" — the `grammar` precedent, where the wire never
carried the constraint so retiring it changed nothing anyone had measured. Retiring a row that
*could* fire needs a different licence.

## Decision 1 — The Floor-guard test, and the six that pass it

**A behaviour is a Floor guard when it (a) changes only what the model sees after its own failure,
or shapes the request without steering it, (b) needs no per-model proof, and (c) cannot regress
Bypass.** A Floor guard is not a Mechanism. It is plain engine behaviour, on by default, living in
the `internal/floor` policy package with thin call sites in `internal/agent` — no catalogue row, no
`MechanismID`, no descriptor, no Capability, no strikes.

Six catalogued rows pass the test and are promoted, keeping their decision logic verbatim:

| Was | Becomes | Why it passes |
|---|---|---|
| `tool_use_enforcer` | **tool-use enforcer** | Off-ramp (ADR 0070): fires only after a reply that narrated an action instead of calling the tool. |
| `empty_response_recovery` | **empty-response recovery** | Off-ramp (ADR 0070): fires only after a reply with no text and no tool calls. |
| `validate` | **tool-call repair** | Fires only on a tool call the engine has already rejected as malformed — post-failure by construction. |
| `tool_loop_interceptor` | **tool-loop breaker** | Fires only on an identical repeat Turn — a failure the model has already committed twice. |
| `tool_result_cap` | **tool-result cap** | Shapes the request (a 40%-budget per-result cap, most-recent Turn protected); says nothing to the model about what to do. |
| `cached_content_intercept` | **read cache** | Intercepts a redundant successful re-read; the one row with measured evidence (`catalogue.md:191`). |

**Floor guards carry no strikes-3 suppression and no Turn-Budget throttle.** The per-Turn
`maxPostResponseRetries` bound is their only limiter. This supersedes the `SuppressStrikesThree`
posture the promoted rows carried as Mechanisms — the two repair rows, tool-call repair and the
tool-loop breaker, most pointedly — under which three judged-harmful Turns withdrew the guard for
the rest of the Session (`internal/agent/selfreg.go:239-250`). Self-regulation is a *tuning*
instrument: it exists to withdraw help that a model may not want. A Floor guard is not help of that
kind — withdrawing the exit from a failed Turn leaves the model with no exit at all, which is the
argument [ADR 0006](0006-bypass-mode-is-the-mechanisms-off-floor.md) and catalogue rule C1 already
made for the two off-ramps' `SuppressExempt` status. The other four inherit it rather than the
strikes counter.

## Decision 2 — The ADR 0009 A/B gate binds lab rows only

**[ADR 0009](0009-the-ab-decision-rule.md)'s per-Mechanism non-inferiority gate, and catalogue rule
D1 with it, bind catalogued Mechanisms — the lab rows. They do not bind Floor guards.** This is the
one thing this record supersedes in ADR 0009, and it supersedes nothing else: the statistical
engine (measured δ, task-blocked units, one-sided non-inferiority, the burden on the Mechanism)
stands unchanged for everything still in the catalogue.

The aggregate constraint is untouched and is what actually protects the floor: the hard invariant
is proved as "full default-ON set vs Bypass, never worse" (ADR 0006), and Bypass now runs *with*
every Floor guard, so a guard that regressed the floor would show up in the control arm itself.
Clause (c) of the Floor-guard test is that constraint restated as an admission criterion.

## Decision 3 — A row may retire on a ratified verdict; the gemma Validated set retires with it

**A catalogued row may be retired on a ratified owner verdict, not only when it is inert by
construction.** This supersedes [ADR 0016](0016-curation-is-per-model-validated-sets-keyed-by-fingerprint.md)'s
2026-08-29 "inert by construction" precondition for joining `RetiredIDs()`. Fourteen rows retire on
that licence — `stall_nudge`, `list_nudge`, `tool_use_directive`, `decompose`,
`guided_decomposition`, `filehint`, `read_loop`, `read_repeat`, `truncate_history`, `library`,
`toolfilter`, `error_enrichment`, `syntax`, `autofix` — and their source, tests and assets are
deleted, on the `grammar` precedent.

What the amendment *bought*, though, was a graceful-degradation rule: a user's Validated entry
naming a retired id sheds that id and still applies, rather than being skipped whole. That rule
survives verbatim and is now load-bearing, because a retired row here is no longer guaranteed
inert. It rests on a narrower claim than before: what a user's `~/.apogee/validated/*.json`
recorded was correct for the build that recorded it, and a curation change of ours must not
silently cost them their model's set.

**The shipped gemma Validated entry retires.** Its evidence is a leave-one-out campaign over a
fifteen-member stack, of which nine members no longer exist; a measured set is its members, so the
evidence is void without them and re-writing the entry over the survivors would publish a stack no
campaign ever measured. The shipped roster ends empty. The `validated` package, the user-directory
surface, `probe model` and the rebind path all stay — a user's own entry under a retired key still
resolves and still applies, shedding what retired.

## Decision 4 — The machinery stays as the lab surface; the catalogue is frozen

**The hook API, the registry, `Config.EnableMechanisms`, the `mechanisms:` config key, the
`/settings` mechanisms row and `--bypass` all stay**, as [ADR 0001](0001-agent-loop-is-an-embeddable-library-driven-by-an-external-bench.md)'s
externally-driven lab surface: a bench arm hands `New` its own registry and its own experimental
hooks, and that is how a future intervention gets measured before anyone argues about shipping it.
Removing the layer would cost the bench its only instrument to buy a smaller `internal/` tree.

**The shipped catalogue is frozen.** No further ports from the sim, no new rows. The remaining port
plan in `catalogue.md`'s Table A is closed as a plan; the table survives as a record. The shipped
catalogue and the shipped Validated roster both end **empty** — that is the intended end state, not
a transitional one, and the invariants that used to assert "twenty rows" now assert emptiness.

**Bypass switches off lab rows only.** With the catalogue empty, `--bypass` on a stock install is a
no-op in effect, and the honest floor it names is now the engine's own — Floor guards on, nothing
armed above them. ADR 0006's definition is otherwise unchanged, and its same-code-path guarantee
gets stronger: the control arm and the default posture are now literally the same agent unless a
bench arm arms something.

## Decision 5 — Six config keys and a zero-value-on `FloorConfig`

**Each Floor guard gets exactly one top-level, file-only boolean config key, defaulting to `true`:**
`tool-use-enforcer`, `empty-response-recovery`, `tool-call-repair`, `tool-loop-breaker`,
`tool-result-cap`, `read-cache`. File-only: no flag, no `/settings` toggle. An operator debugging
their own upstream can still get a genuinely silent floor — ADR 0070 rejected a lock for that
reason and this keeps the escape hatch — but turning one off is a deliberate edit, not a posture
anyone reaches by accident.

**`domain.FloorConfig` holds six `Disable…` booleans, so its zero value keeps every guard on.** A
library embedder handing `New` a bare `Config` gets the whole floor, which is exactly what ADR
0070's empty-`EnableMechanisms` engine floor was for — the same guarantee, now structural rather
than reconstructed from a Capability column, and the [ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)
benchable-all-the-way-up door held open the same way.

**An old `mechanisms:` entry naming a promoted ID earns a notice naming its new key; there is no
silent mapping.** `tool_use_enforcer: false` in a config written against v0.20.0 does not quietly
become `tool-use-enforcer: false` — the key moved from a per-Mechanism map to a top-level
structural setting, and a user who wants the guard off should say so in the new place.
[ADR 0015](0015-catalogued-mechanisms-are-enabled-by-id-through-config.md)'s "`EnableMechanisms` is
the one enable path" is amended accordingly: it remains the one enable path for **catalogued
Mechanisms**, and Floor guards are simply not enabled through it — they are on, and the six keys
disable them. [ADR 0014](0014-guided-decomposition-steers-the-primary-call-and-serializes-delegation.md)
is amended by removal: `guided_decomposition` retires under Decision 3, so the record stands as
history and its `Requires`/batch-pacing contract binds nothing shipped.

## Decision 6 — Rejected alternatives

**A — Keep the six as Mechanisms under a new `structural` Capability, exempt from D1.** Rejected:
it is ADR 0070's carve-out done a second time and it keeps every cost the carve-out was working
around — a descriptor, an ID, a strikes posture that has to be exempted row by row, a `/settings`
row that invites a user to switch off a guarantee, and a catalogue that reads as twenty candidates
when six are settled behaviour and fourteen are gone. The wording change ADR 0070 declined to buy
is now the smaller half of the change: the substance is the strikes posture, the config shape and
the frozen catalogue.

**B — Delete the Mechanism layer entirely.** Rejected: the hook API and registry are the bench's
only way to arm an intervention without patching the engine, and [ADR 0001](0001-agent-loop-is-an-embeddable-library-driven-by-an-external-bench.md)
makes external drivability a standing commitment. Freezing the shipped catalogue costs nothing the
lab needs; deleting the surface would cost it everything.

**C — Promote the six as engine code with no config keys at all.** Rejected: a guard that injects
text into a model's context with no way to stop it is exactly the footgun ADR 0070's Option D
rejected. Six booleans are cheap, and they keep the floor an opinion rather than a lock.

## Consequences

- A stock install runs six Floor guards and zero Mechanisms. Where the old default armed two
  off-ramps and nothing else, the shipped posture now includes tool-call repair, the tool-loop
  breaker, the tool-result cap and the read cache — behaviour that previously required a
  hand-written `mechanisms:` block or a Validated set.
- **The catalogue and the shipped Validated roster end empty.** Every invariant, test and doc
  sentence that counted rows ("21 mechanisms") is rewritten to assert emptiness; `/settings` shows
  an empty mechanisms list on a stock install.
- Fourteen rows' source files, tests and assets are deleted. `internal/mechanisms/retired.go`
  carries each retired ID with the release that retired it (`v0.20.0`) and, for a promoted ID, the
  config key that succeeds it — so an old config naming any of the twenty gets a specific message
  rather than an unknown-ID failure.
- `~/.apogee/library` on disk is never touched, and `internal/library`'s fingerprint and
  probe-record halves stay: they serve Validated sets and `probe model`, not the retired `library`
  Mechanism.
- Guard firings surface as `domain.FloorGuardEvent`s keyed by config key, reaching every Driver;
  `MechanismFiredEvent` stays for lab hooks. A Driver that only knew about Mechanism firings would
  otherwise have gone quiet the moment the catalogue emptied.
- The catalogue document is archived with a per-row verdict — promoted, retired, or deferred —
  so the record of what was measured, and what was decided without measurement, survives the
  deletions.
- A future intervention still has a route in: arm it as an experimental hook, measure it under ADR
  0009, and bring a record. What it may **not** do is join a frozen shipped catalogue on the
  strength of a port.
