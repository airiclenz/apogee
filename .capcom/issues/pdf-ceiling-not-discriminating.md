---
title: The PDF absurd-xref allocation ceiling does not discriminate
status: done
type: task
priority: 3
plan: 2026-09-23 - 00 - slow-box-follow-ups-and-stale-state-plan
aliases: [apogee-pdf-ceiling-not-discriminating]
created: 2026-09-23T04:59:16Z
closed: 2026-09-23T06:55:58Z
reason: "Item 4: xref-stream allocation case bites; resolved by plan '2026-09-23 - 00 - slow-box-follow-ups-and-stale-state-plan' (archived at 152dc896)"
---

Item 10 of 'docs/plans/2026-09-22 - 01 - slow-box-deadlines-plan.md' replaced a wall-clock reading in internal/doctext/pdf_test.go with an allocation ceiling (8 MiB) so the absurd-xref case measures the allocation the refusal prevents instead of timing it. The ceiling does not bite: with refuseAbsurdObjectCount short-circuited, ExtractPDF allocates only ~56 KiB on the /Size 4000000000 fixture, so the case's whole bite still comes from its refusal-message assertion. Either size the ceiling to what an unrefused parse would actually allocate, or use a fixture that allocates.
