---
title: ADR 0019 consequences
---

- **A new host delegate joins `Approver`/`Asker`/`Confiner` on `domain.Config`.** `Presenter` is
  additive public surface (nil ⇒ the tool is unregistered), so the bench and any headless
  embedder are unaffected by construction, and the change is a **minor** bump.
- **A new `internal/present` package** carries the host-side mechanisms (locality/desktop
  detection, the OS opener, the doc server) as injectable-seam code the TUI wires and any embedder
  may reuse. It imports the standard library plus `shlex` — the POSIX splitter the `terminal` tool
  already uses — so the one place a user's command line is parsed (`present.command`) behaves
  identically wherever it appears; it adds **no new dependency**. Under
  ADR 0010 it depends on
  `internal/domain` downward only, never the root facade.
- **The transcript grows a first-class presentation entry**, visually distinct from tool cards —
  a deliverable is not plumbing. Path and URL are rendered as **plain text on their own lines**,
  never wrapped in markup and not split mid-token if avoidable, because terminal linkification is
  the whole mechanism.
- **A file-only `present:` config block** (`auto-open`, `command`, `port`, `host`) tunes rungs 1–3.
  `auto-open: false` disables rung 1 and **never** rung 0. The shipped template documents the
  macOS Local Network gotcha as the first thing to check when a served URL is unreachable.
- **Skills stay user-authored** (ADR 0002):
  nothing here edits a builtin skill. The guidance that a report-producing skill should end with
  `present_document` is documentation, recorded in `ISSUES.md`.
- **CONTEXT.md gains the vocabulary** — *Present / Presentation*, *Presenter*, *presentation
  ladder*, *doc server* — worded to match this ADR.
- **Deferred, kept additive:** a markdown→HTML rendering rung (the doc server stays
  extension-agnostic so it can be added without reshaping the ladder), and port-forward
  integration (Zed `port_forwards`, VS Code auto-forwarding) — the direct-IP URL covers the
  primary topology.
- **The Windows opener ships unexercised** until the merge plan's Phase 5 (Windows shell/path
  backend) provides a real Windows harness — the same posture as `internal/platform`'s Windows
  stub, and stated rather than hidden. (2026-07-22: Phase 5 shipped; the live opener check is
  folded into the owner-run smoke passes.)
