---
title: "apogee's domain glossary: the terms to use in code, docs and messages, one Note per term"
---

Apogee is a terminal **coding agent** built for smaller local models — while working
even better with bigger ones — that owns the full agentic loop — provider, tools,
context, and sessions — and runs seven **Floor guards** inside that loop: engine behaviour
every model runs with, fired as builtin **Reactions** beside which the bench may arm one of its
own for measurement.
The hard constraint, inherited unchanged from the predecessor projects: **nothing Apogee puts in
front of a model may make that model perform worse than the same agent without it.** That floor is
**Bypass mode** (every **advise** and **shape** Reaction of user or bench-armed origin off, and the
engine's own advise builtin — the context-fill notice — with them; the seven Floor guards,
**observe** and **gate** on) —
**not** a naked model, because Budget, Compaction and Pruning are structural and load-bearing (a
truly naked model just overflows its context window). **The bare loop** — the plain-language name
`README.md`, `AGENTS.md` and the ADRs give the constraint's baseline — is this Bypass floor
(ADR 0006): the Floor guards and the
structural reducers are part of it, not held to it, and a Floor guard is admitted to it only if it
cannot regress it (ADR 0071
Decision 1, clause (c)). The constraint is **proved at bench
time** as a ground-truth, distributional non-inferiority gate against Bypass (see
ADR 0009); in production nothing approximates it any
more — the self-regulating lab layer that once did (Adaptive Suppression + the Turn Budget)
retired with the **Mechanism** surface, so a stock install runs the guards with nothing armed
above them.

This glossary is a fresh start, not a migration of `apogee-sim`'s `CONTEXT.md`. The
predecessor project was *middleware* between a coding tool and a model; Apogee **is**
the coding tool now. Terms that described the old middleware structure are retired (see
Retired terms); the language below describes the agent.

The glossary's sections:

- Identity and shape: `glossary-identity`
- Turns and stepping: `glossary-turns`
- Safety and autonomy: `glossary-safety`
- Reactions and Moments: `glossary-reactions`
- Self-regulation: `glossary-self-regulation`
- Context and history: `glossary-context`
- Deliverables and presentation: `glossary-deliverables`
- Probing and model identity: `glossary-probing`
- Validation and the bench: `glossary-validation`
- Retired terms: `glossary-retired`
