---
title: apogee probe model suggests markdown-fenced for a model whose text call markdown-fenced cannot parse
status: done
type: bug
priority: 3
plan: 2026-10-08 - 00 - probe-tool-call-format-suggestion-plan
aliases: [apogee-probe-suggests-unparseable-format]
created: 2026-10-08T07:30:17Z
closed: 2026-10-08T08:33:17Z
reason: "Resolved by plan 2026-10-08 - 00 - probe-tool-call-format-suggestion-plan item 1 (archived at 05c73f05): markdown-fenced is suggested only when a taught-format trial reply parses to probe_echo; otherwise native, quoting the model's call."
---

SuggestProfile (internal/probe/modelfingerprint.go:129-137) suggests tool-call-format: markdown-fenced whenever the battery saw no native tool call, without checking that the reply's written call is in a shape markdown-fenced parses.

Seen 2026-10-08 (docs/skill-runs/probe-model/2026-10-08/results.md, model 3): Qwen3.6-35B-A3B-Q4_K_M on llama.cpp replied with the pythonic call probe_echo(text="apogee") (no JSON object). The printed model-profiles: entry suggested markdown-fenced, which would not recover that call, so a user pasting the suggestion gets a profile that still drops every call.

Expected: suggest markdown-fenced only when the observed reply parses under it (the battery already asks the salvage guard, battery.go salvageableCallName); otherwise say no listed format fits and quote the reply's call shape. Related: apogee-probe-suggests-tool-call-example.
