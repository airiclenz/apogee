---
title: rootWiring.layeredOptions has no production caller
status: open
type: chore
priority: 3
created: 2026-10-28T09:17:03Z
---

After plan `2026-10-27 - 01 - project-pin-admit-and-adoption-snapshot-plan` item 11, `rootWiring.layeredOptions` (`cmd/apogee/projectrules.go:437`) is called only from tests (`cmd/apogee/projectrules_test.go:879`, `cmd/apogee/mcp_cmd_test.go:764`); production reads go through `loadLayeredOptions` and the Adoption snapshot.

## Why

A production method whose only callers are tests reads as a live door it no longer is.

## Acceptance

- layeredOptions is gone from the production files (moved to a test helper or its tests call loadLayeredOptions), and no production caller is lost.
