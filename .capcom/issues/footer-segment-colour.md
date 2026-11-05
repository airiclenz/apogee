---
title: Colour the footer's segments (meaning-driven, per-segment, or user-set)
status: open
type: feature
priority: 3
plan: 2026-11-05 - 02 - footer-rows-and-colour-plan.md
created: 2026-11-05T08:49:40Z
---

The footer is almost all one muted tone. Only the mode marker carries its mode's colour, `✦ offline` and `unconfined` take the `error` tone, and non-zero git counts lift to `MutedBright` (layout.md, "The git state" and the mode marker). Give the segments colour so the row is easier to scan.

## Ideas to weigh

- **Meaning-driven colour:** a colour that says something. Examples: the context gauge or spend shifts green, then yellow, then orange as it fills (the owner's Claude Code status line does this for context), `↓` behind and `Δ` dirty in a warning tone, the model in an accent.
- **Identity colour:** each segment keeps one steady hue so the eye finds it (host, model, workdir, branch).
- **User-set colour:** a per-segment colour in the footer layout config (`footer-overflow-layout`), defaulting to the scheme's roles.

## Constraints

- Colours come from the colour scheme's roles (`internal/scheme`, `ui.color-scheme`), never raw ANSI, so `dark`, `light` and custom schemes all stay readable. A new role needs a value in every built-in scheme.
- The mode marker's colour must stay the loudest thing on the row: it is the blast-radius fact (layout.md). Segment colour must not compete with it or with the `error` tone.
- A colourless terminal (NO_COLOR, a low colour profile) still reads correctly from text alone.
- Per-segment colour config would live in the same `ui:` footer key as the layout, so decide alongside `footer-overflow-layout`.

## Open questions

- **Which approach.** Suggested (2026-11-05, not yet decided): meaning-driven colour by default, with per-segment overrides in the footer layout key. Identity colour would be the fallback.
- **Which facts get a colour.** Candidates: the context gauge and the spend (fill thresholds), git `Δ` (dirty), `↓` (behind) and `↑` (ahead), the effort word by level, and the model as an accent.
- **Where the thresholds sit.** The fill points where the gauge and spend change colour, and whether they can be configured.
- **New scheme roles or existing ones.** Which roles the segments take from `internal/scheme`, and whether new roles are needed in every built-in scheme.
- **Override format.** How a per-segment colour is spelled in the `ui:` footer key (a role name, or also a raw colour), and how a bad value is refused at load.
- **Staying quieter than the mode marker.** How much colour the row can carry before it competes with the mode marker and the `error` tone.

## Acceptance

- The colour rules are written into layout.md, plus docs/manual/configuration.md if they can be configured.
- Golden tests cover each built-in scheme and a no-colour profile.
