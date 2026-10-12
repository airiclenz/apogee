---
title: config.Price has no MarshalYAML, so a priced entry does not round-trip
status: done
type: bug
priority: 4
plan: 2026-10-03 - 00 - table-stakes-and-image-input-plan
aliases: [apogee-price-marshal-yaml]
created: 2026-10-03T18:24:36Z
closed: 2026-10-03T23:28:51Z
reason: "Resolved by refocus-followups-plan (docs/plans/archived/2026-10-03 - 01 - refocus-followups-plan.md, db1550b0) item 4: Price.MarshalYAML round-trips a priced entry"
---

Deferred from plan 'docs/plans/archived/2026-10-03 - 00 - table-stakes-and-image-input-plan.md' item 2 (2afbbca7). internal/config/config.go:1499: config.Price has no MarshalYAML, so a priced ServerEntry written back gains cached-input: 0 and loses the input-rate default (CachedInputRate falls back to the input rate only when HasCachedInput is false, and the yaml:"-" Has* flags are not written). No production path marshals a priced entry today.
