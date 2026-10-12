---
title: "Bypass mode: the flag that skips every user- and bench-armed advise and shape Reaction"
---

**Bypass mode**:
A `Config` flag **orthogonal to Agent mode** that skips every armed **advise**, **shape (view)**
and **shape (work)** Reaction of **user** or **bench-armed** origin and
the engine's one advise builtin, the **Context-fill notice** — exactly what can move the floor —
while leaving the seven Floor guards and the agent's structure intact
(ADR 0076 D9). It says
nothing about the Floor guards, which stay on in **every** arm, so the floor is
*functional* — a baseline that quit at the first stumble would pass the hard constraint trivially —
and nothing about **observe** or **gate** Reactions, which change nothing the floor measures: a
bench run under Bypass keeps the user's notifications and guards. On a stock install with nothing
configured, `--bypass` switches nothing off in effect: the honest floor it names is the engine's own
— guards on, nothing armed above them — and the control arm and the shipped default are literally
the same agent unless a bench arm arms something. Budget, Compaction, and the rest of the loop still
run: Bypass is the honest "model-shaping Reactions off" floor, **not** a naked model. It is also the
bench's **aggregate control arm** — the same code path users can run — against which the
hard-constraint non-inferiority gate is proved. See
ADR 0006, ADR 0071 and ADR 0076.
_Avoid_: "naked model" (Bypass keeps the structural reducers and every Floor guard on), "disabled
mode", "raw mode", "Mechanisms-off" (the term retired; say "advise and shape off").
