---
title: apogee probe config — report unknown keys, migrated legacy keys and resolved registry values
status: done
type: feature
priority: 3
aliases: [apogee-lpi]
created: 2026-09-14T15:55:28Z
closed: 2026-09-16T22:35:04Z
reason: "feat(probe): apogee probe config reports notices, pending migration and resolved registry values (4a40b259)"
---

Ratified 2026-09-14 while writing the polish-wave plan (docs/plans/2026-09-14 - 00): the unknown-key startup notice and the template↔registry gate land in that plan; a probe verb that prints unknown keys, migration notes and the resolved value per registry key is its own small feature under the existing probe family (ADR 0021 host-report scope) and lands later.
