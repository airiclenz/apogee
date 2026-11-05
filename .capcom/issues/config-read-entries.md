---
title: "Config is read through one source (architecture review 2026-10-14 #3)"
status: parked
type: task
priority: 3
created: 2026-11-05T19:56:22Z
---

Found by docs/reviews/archived/architecture-review-2026-10-14.html (#3), re-verified 2026-11-05 at f7ec72b6. Config still has several read entries — ResolveOptions, ApplyConfig, LoadFileConfig (internal/config/config.go) and LoadLayeredConfig (internal/config/layer.go) — and the Project root is still resolved in three places: resolveProjectRoot (layer.go), resolveRoots (cmd/apogee/wire.go) and probeconfig.go, each calling projectroot.Resolve. The review proposes one config source in internal/config, resolved once from (apogee home, workspace, takes-no-project), that owns every read and every project-file location, so Drivers, /settings, project rules and probe only ask it and a caller can no longer pick the wrong entry. Its three symptoms were fixed by items 4 (no migration on a live re-read), 5 (re-read pinned to the session's Project root) and 16 (probe config honours APOGEE_CONFIG) of docs/plans/archived/2026-10-14 - 01 - architecture-review-defects-plan.md; it fits ADR 0096 D1/D6 and leaves ADR 0083 §4/§5 alone.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
