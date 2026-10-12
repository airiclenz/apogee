---
title: Doc drift found by the 2026-09-23 refocus, unverified against code
status: done
type: task
priority: 3
aliases: [apogee-doc-drift-sweep]
created: 2026-09-24T18:17:16Z
closed: 2026-09-30T16:37:35Z
reason: "Swept 2026-09-30 (uncommitted at close): refocus fixes 1-8 applied (fix 8 as SECURITY.md/configuration.md wording: run: reactions inherit env, advise:/gate: are scrubbed), 19 overtaken ADRs re-statused with dated notes, tool/start-only/battery counts, links, layout sketches, configuration.md ui: split, review cards #8-#10,#14,#16 resolved, #17 partly. Left for owner: CHANGELOG preamble ('SemVer from v1.0.0 onward', retired term 'hook points'); layout.md Diff Preview block (+2 -2 ASCII hyphen, unnumbered body)."
---

From handoff 2026-09-23 - 00 (refocus-follow-through). Verify each against the code before editing; a mismatch can be a code bug.
- docs/layout/tool-layout.md and the 2026-09-20 architecture review count 33 tools and omit git_show; internal/tools/registry.go has 34.
- docs/manual/ gives inconsistent lists of start-only settings.
- Several ADRs still read 'accepted' though later ADRs retired them.
- CONTEXT.md still uses the retired terms 'Mechanism' and 'Hook point'.
- The confinement contract says '8-row battery'; there are now 13 rows.
- docs/design/mcp-client.md names the wrong file for the MCP connection code; building.md has dated text; one doc links an archived plan.
- Architecture-review cards #8-#10 and #14-#17 look open only because CHANGELOG never mentions them; check against the code.
- CONTEXT.md Event lines entry still says 'Versioned v:1'; the tree is at v:2.
