---
title: ADR 0085 context
---

On 2026-09-21 an apogee session running through OpenRouter was very slow, and apogee could not
say why. It knew token counts (`domain.UsageEvent`) and a TUI-only tokens/s, and nothing else:
no time to first byte, no time to first token, no per-request duration, no per-server fault
rate. Whether the cost was a slow provider, a starved queue or a ten-minute stall cut by
`stream-idle-timeout` (ADR 0082)
was a guess.

The owner asked whether apogee could route dynamically across providers by cost, speed and
reliability — with the hard constraint that apogee must not become an OpenRouter-tuned tool: a
local llama.cpp user, an OpenRouter user and a user behind any other gateway must all benefit
equally. The brainstorm (`docs/handoffs/2026-09-21 - 00 - provider-routing-speed-brainstorm.md`)
settled on four layers — an opaque body passthrough, per-server measurement, explicit fallback
pools, a dynamic scoring router — and the owner's 2026-09-23 grilling session ratified the
design of the first two, which `docs/plans/2026-09-23 - 00 - upstream-passthrough-and-measurement-plan.md`
implements. This ADR records those calls, rejects the fourth layer, and names the third as future
work.

Two facts shaped it. Apogee has no OpenRouter-specific Go code, and keeps none: the only
OpenRouter awareness is documentation and the effort-dialect discovery that reads a
`reasoning.mandatory` flag. And a configured `model` id that the server does not advertise —
an OpenRouter variant slug such as `vendor/model:nitro` — has been used verbatim since
b6e51496 (`provider.HintResolution`: `exact`, `base-slug`, `trusted`), which made "sort by
throughput" a zero-code answer; that change landed with no ADR, and ADR 0024 §6 still says the
binding "follows observed reality" once the hint vanishes from `/v1/models`.
