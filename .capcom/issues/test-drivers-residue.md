---
title: "Test drivers — residue: the claims no driver observes"
status: done
type: decision
priority: 4
labels: [standing-rule]
aliases: [apogee-n6z]
created: 2026-09-03T18:41:09Z
closed: 2026-09-03T18:42:21Z
reason: Accepted proxies ratified by ADR 0062 decision 5, recorded so they are not re-opened as coverage gaps. Migrated from ISSUES.md 2026-09-03.
---

**Status:** recorded 2026-08-28 at the close of the test-drivers kit plan
(`docs/plans/2026-08-27 - 02 - test-drivers-kit-plan.md`). **No open work.** These are the
accepted proxies ratified by
[ADR 0062](docs/adr/0062-test-drivers-are-drivers.md) decision 5, written down so a later reader
does not re-open them as coverage gaps.

The live source is the "which driver observes which claim" table in
[`docs/design/test-drivers.md`](docs/design/test-drivers.md). Its **Not observable** column names
two different things: most cells name the instrument that asserts the claim INSTEAD (a session
record, a request log, a unit test), and those are covered. Only the rows below are irreducible —
the claim leaves the machine, and the proxy beside it *is* what "that item passes" means.

- **Font tofu (T-20)** — whether the reader's own font carries the glyph at all. The emulator's
  cell width is the width authority (`TestE2EWidthTicksMultiSelectChoices`,
  `TestE2EWidthSurvivesAColourSchemeSwitch`); what a font does with a codepoint is outside every
  terminal apogee can drive.
- **Felt flicker (T-24)** — proxied by the `--tui-trace` repaint ceiling
  (`TestE2EStreamRepaintCeiling`): bytes written and full-frame repaints per streamed token,
  pinned against a ceiling. Nothing measures perception.
- **What a real desktop application does with the file (T-19)** — the hand-off is asserted at its
  argv through the `openerLookPath` seam (`TestE2EPresentOpensOnlyTheAllowedFormats`), and the
  refusals by that log's ABSENCE of a launch. The application on the other side is not apogee's.
- **`brew upgrade` before the release it upgrades to exists (T-21), and the Homebrew and
  OpenRouter steps of the newcomer walk (T-23)** — both need a PUBLISHED release, and the second
  a real API key. The post-publish half runs by hand as `make release-smoke VERSION=vX.Y.Z`
  (`scripts/release-smoke.sh`); the container walk (`TestNewcomerFollowsTheDocs`) judges
  everything that needs neither.
