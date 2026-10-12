---
title: ADR 0035 context
---

`~/.apogee/config.yaml` is a **documented, hand-edited document the user owns**: seeded once from
the embedded template (`internal/config/defaults/config.yaml`) and never overwritten again — an
invariant pinned by a test (`internal/config/defaults_test.go:45`) and restated as a promise in the
README ("an upgrade never touches it") and in
ADR 0023 §8. Two more
records draw the same line from the other side:
ADR 0012's amendment
(2026-07-21) allows exactly **one** config write, `/confine off --save`, and only because it is a
distinct affirmative user act; ADR 0021
§5 refuses to write even a suggested `model-profile`, printing paste-ready YAML instead, because
"a probe produces *evidence*, and turning evidence into a preference is the user's move".

Against that, the config surface has outgrown a text editor: roughly forty leaf keys across three
resolution sources (flag over `APOGEE_*` env over file over built-in default), a schema whose
source metadata lives as parallel string literals inside `resolveSettings` (`internal/config/config.go`),
and a file that — because seeding never re-runs — is missing every key added after the day it was
written. `ISSUES.md` carried the request ("a full screen menu for all available settings when
running the slash command `/settings`. This needs grilling."). A 2026-08-05 grill settled that
surface; this record is its ratification.

The constraints it lands inside: the TUI is a thin renderer over a worker engine
(ADR 0011 — no YAML in the
renderer, no no-copy types on the value Model), one width authority
(ADR 0030), one slash
namespace (ADR 0027), `layout.md`'s row
budget where every pane's rows come out of the transcript and the frame floors are non-negotiable,
and ADR 0031's
door-keeping: nothing safety-relevant may exist only in one Driver's surface.
