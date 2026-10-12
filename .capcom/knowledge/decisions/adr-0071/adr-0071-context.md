---
title: ADR 0071 context
---

The [mechanism catalogue](../../../../docs/design/archived/mechanism-catalogue.md) holds twenty rows behind one gate:
rule **D1** — a Mechanism ships **off** until an A/B bench run proves it a win, under the
per-Mechanism non-inferiority test of ADR 0009. That gate was
written for *tuning*: behaviour that changes what a model is shown or asked **before** the model
has failed at anything, and that can therefore help one model and hurt the next. For tuning it is
the right gate, and it stays.

Two months of catalogue and four port waves later, the gate has produced almost no evidence.
One row carries a real measurement — `cached_content_intercept`, help rate **0.73** (11/4/1),
`repeated_tool_call` 0.91→0.15 per run on gpt-oss-20b, inert-but-correct on gemma
([`catalogue.md:191`](../../../../docs/design/archived/mechanism-catalogue.md)). One curated set exists (the gemma
Validated entry). Every other row's verdict column still reads `pending`. The parked-item brief
that opened this wave states the position plainly: freeze the catalogue, promote the structural
few, retire the nudges — "`read_repeat`/`cached_content_intercept` (the one row with real
evidence: help rate 0.73 on gpt-oss-20b)"
(`docs/handoffs/2026-09-02 - 00 - harness-over-mechanisms-parked-items.md`, parked item 1).

The reason the gate produced no evidence is that most rows were never on the distribution it
measures. ADR 0070 already made that argument for
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

The catalogue's own retirement rule blocks the obvious exit. ADR 0016's
2026-08-29 amendment says a row "only ever joins `mechanisms.RetiredIDs()` when it was **inert by
construction** at the moment it was retired" — the `grammar` precedent, where the wire never
carried the constraint so retiring it changed nothing anyone had measured. Retiring a row that
*could* fire needs a different licence.
