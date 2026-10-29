---
title: "Scratch dir: the per-session writable directory outside the workspace"
---

**Scratch dir**:
The per-session writable directory **outside the workspace** —
`~/.apogee/scratch/<session-id>/`, a dotdir sibling of `sessions/` — that the **Confinement**
box carries as an extra writable root (ADR 0056). It exists because a workspace-only fence left
a confined agent nowhere safe for scratch work, so improvisations landed in the workspace (the
2026-08-22 clobber incident): now scratch tests, probes, and temp files have a home the fence
allows, named to the model via the **`{{scratch}}`** prompt placeholder — for a user's own prose —
and by the **Orientation block** that rides on every standing system message in every mode,
**Plan** included: since ADR 0012's second loosen (2026-09-14) the scratch dir is the one place Plan
writes and the one native write Ask-Before does not gate, so the bullet is true on every rung (the
terminal route into it stays refused in Plan and gated in Ask-Before; the one subprocess Plan runs is
a Recipe's script stage, the engine's own call, confined so it writes only its own workflow folder —
ADR 0012 amendment 2026-09-27).
Writable, and readable by every read tool: the announced path is a live read root for
`read_file`, `grep`, `list_dir`, `find_files`, `copy_file`'s source and `present_document`
(the `Scratch` mount of the one `Config.ReadMounts` value, 2026-09-15 — a mount of its own beside
`Roots`, which stays the announced **library** roots, and `Virtual`, the `shipped:` trees), so what
the model drafts there it can read back and show.
A tool set copies `Config.ReadMounts` when it is built (`tools.HostToolsOf`), so every composition
root binds `ReadMounts.Scratch` before it builds its tools — the TUI as a closure over the engine
holder it constructs later; bound after, the set carries no scratch root until a rebuild and the
model cannot read back what it was told to write there (2026-10-14, docs/plans/archived/2026-10-14 -
01 - architecture-review-defects-plan.md).
Created `0700` when the session id is minted, follows the **active** session across rotation,
advertised writable only once it actually exists, and swept by a
best-effort 14-day startup GC. Per-session constant, so prompt use is KV-cache safe. A **Firing**
gets one of its own on every **Driver** — the in-session Schedule, the daemon and
`apogee headless` each mint the record id before the run and create that id's dir, so the dir and
the saved record share one name and the same sweep reclaims it.
_Avoid_: "temp dir" (`/tmp` is exactly what confinement may deny), "cache" (it is disposable
work space, not a cache with an invalidation story).
