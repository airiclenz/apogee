---
title: Capability tier
---

**Capability tier**:
The ordinal summary of a battery run — what the model can be **asked** to do (native tool calls,
structured output, multi-step chaining). Today it is a **reported signal only**: it carries no
automatism, gates nothing, and changes no request. The *adaptive prompt complexity* idea it exists
for — slimming tool descriptions and system prompts for a lower tier — is a parked follow-on in
the issue register (capcom, `apogee-fsx`), because that is a model-shaping **Reaction** and a
model-shaping Reaction ships on the non-inferiority gate, not on plausibility
(ADR 0009).
_Avoid_: "model tier" / "model size" (it describes observed behaviour, not parameters or
quality), treating it as a config knob (it is an observation).
