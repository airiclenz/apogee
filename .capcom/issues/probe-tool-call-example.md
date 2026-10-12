---
title: "apogee probe model: suggest a tool-call-example: for a custom-regex model"
status: done
type: feature
priority: 4
plan: 2026-10-08 - 00 - probe-tool-call-format-suggestion-plan
aliases: [apogee-probe-suggests-tool-call-example]
created: 2026-10-08T07:30:06Z
closed: 2026-10-08T08:33:19Z
reason: "Resolved by plan 2026-10-08 - 00 - probe-tool-call-format-suggestion-plan item 2 (archived at 05c73f05): ratified call - the probe never suggests custom-regex or tool-call-example:; it quotes the call shape and docs/manual/probe.md says that is the user's material."
---

Deferred under 'Out of scope' in docs/plans/archived/2026-10-06 - 01 - model-facing-contracts-plan.md:37; surfaced again by the 2026-10-07 refocus (docs/skill-runs/refocus/2026-10-07/planned.md:70).

Today SuggestProfile (internal/probe/modelfingerprint.go:130) only ever suggests tool-call-format native or markdown-fenced, so the printed model-profiles: entry never carries a tool-call-example:. The key (docs/manual/configuration.md:1290) exists for models whose real call shape apogee cannot rebuild from tool-call-pattern:, and today the user has to write it by hand.

Scope to decide: the battery would need to observe a text-format call it can quote verbatim (or the user's configured custom-regex pattern) to propose an example; whatever it prints must pass the same startup check (pattern parses it to a tool name and JSON-object args).

## Log

- 2026-10-08 beads: probe run (docs/skill-runs/probe-model/2026-10-08/results.md): of six Apollo-II models only Qwen3.6-35B-A3B-Q4_K_M wrote a text call, and it was pythonic (probe_echo(text="apogee")), not JSON. A verbatim example of that shape fails this issue's startup check (pattern must yield a tool name + JSON-object args), so scope needs a decision first: convert kwargs to a JSON object, or skip the suggestion for non-JSON shapes. Possible cause on the server side (unverified): that llama.cpp profile may lack --jinja / a tool-call chat template. The probe record stores features only, not the reply text, so the verbatim call is only in the probe's stdout. One sample: the same model passed multi-step-chain, so its call format may vary between replies.
