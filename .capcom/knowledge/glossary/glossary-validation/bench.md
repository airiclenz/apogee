---
title: "The bench (apogee-sim): the external module that validates apogee by importing it as a library"
---

**The bench** (apogee-sim):
The external Go module that validates Apogee by **importing it as a library** and driving
the agent loop in-process — owning the sandbox, stepping turns, forking counterfactuals,
and scoring outcomes against real local LLMs. It is a development-time instrument; its
code is never linked into the shipped `apogee` binary. apogee-sim keeps its own glossary
(Sim, Baseline, Intervention, Trace archive, Frontier driver, …); those are *bench*
terms, not Apogee terms. See ADR 0001.
_Avoid_: "the harness" inside Apogee's own docs (there is no harness in Apogee), "the
external service" (it's a sibling Go module, not a running service).
