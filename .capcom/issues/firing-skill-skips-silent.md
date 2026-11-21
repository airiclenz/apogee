---
title: A headless or daemon Firing never reports skills that failed to load
status: open
type: task
priority: 3
labels: [driver-parity]
plan: 2026-11-21 - 01 - driver-notice-parity-plan.md
created: 2026-11-21T12:44:48Z
---

Found by docs/reviews/archived/architecture-review-2026-08-30.html (A5); re-verified 2026-11-21 at 43ece2f2.

The TUI's `/skills` report lists every SKILL.md the loader skipped or shadowed (`failedSkillLines` / `shadowedSkillLines`, internal/tui/skills.go:422, :448), fed by `Catalog.Skipped()` (internal/skills/catalog.go). An unattended run builds its own provider in the Firing composer (cmd/apogee/wire_firing.go:257-270), and nothing in cmd/apogee/headless.go, daemon.go, daemonfire.go, internal/run, internal/schedule or internal/daemon reads `Skipped()`. A `/skill` token in a Firing prompt naming a skill that failed to parse stays plain text with no notice; internal/run/doc.go:17-21 states this ("skipped WITHOUT notice", since a Firing has no event sink for the ErrorEvent). The operator cannot tell a broken skill from a missing one, breaking the loader's "soft must not mean silent" rule.

Smaller: the TUI still works out each skill's source from its path (`skillSource`, `underSkillRoot`, internal/tui/skills.go:234-273), although the loader knows each skill's root.

Direction: move the skip-report wording into internal/skills beside `Catalog.Skipped`; headless prints it to stderr once before submit; the daemon logs it once per process, latched like its other start-up notices. Optionally the loader records each Skill's source.

Constraints: ADR 0031 (Driver parity), ADR 0043 (no sub-packages under internal/tui, so the wording goes to a sibling package), ADR 0075 if headless puts it on its versioned event stream.

## Acceptance

- A headless run in a project with a malformed SKILL.md names that skill on stderr.
- The daemon logs it once per process.
- Both are pinned by stubllm-driven tests.
