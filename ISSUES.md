A: Activated / Active
P: Planned
X: Executed


- [X] I cannot start writing the next prompt when the model is working. I'd like to be able to type the next prompt already. I am also wondering if there is a way to send off messaged to the model while it is working: scheduled messages - this would be usefull to steer the model and add additional info / remarks. The scheduled messaged would need to be sent when possible even when the model is still working. 
  → **Interjections** — the box stays live while the model works, `⏎` queues, and a queued message is delivered INTO the running exchange at the next tool boundary ("scheduled" = deliver-when-possible; nothing clock-timed). `esc` holds the queue; a natural completion flushes it. See [ADR 0025](docs/adr/0025-interjections-commit-at-the-between-steps-boundary.md), `docs/plans/2026-07-27 - 01 - interjection-and-terminal-cursor-plan.md` items 1–5, and the CHANGELOG `[Unreleased]` entry.

- [X] The cursor in the prompt box is blinking. I don't really want anything blinking. I just want a full static symbol to show where the cursor is. Preferrably we use the terminals defined cursor symbol (line vs block)
  → The prompt now draws the **real terminal cursor**, always steady, shape from the `cursor-shape:` config key (`block` | `underline` | `bar`); the terminal's own configured shape cannot be inherited while a full-screen program runs, so the key is the honest substitute. Plan item 6 + the CHANGELOG `[Unreleased]` entry; no ADR (recorded in the changelog and the config docs by decision).

- [X] I cannot select text in apogee when the model is working. I'd like to be able to select text at any point in time.
  → Transcript selections now survive a repaint by the **keep-if-unchanged** rule (kept while every line they span is unchanged, dropped the moment that text moves), so a drag holds while the model streams; transcript selection works in every state and prompt selection follows editability. Plan items 7–8 + the CHANGELOG `[Unreleased]` entry; the rule is narrated in `internal/tui/mouse.go` and summarised in ADR 0025's consequences.

- [ ] Functionality that exists in apogee-code has not been fully ported to apogee (`@file`, `/clear`, `/continue`, `/skill`, session-management UI now done; `/server`, inspector still pending). Verified + list collected → see **TODO.md → "apogee-code feature parity — user-facing affordances not yet ported"**. Porting still in progress.

- [ ] File references with filenames including spaces are not working properly: @"docs/plans/2026-07-23 - 04 - version-build-number-plan.md" returns this error: loop: @"docs/plans/2026-07-23 could not be resolved and was ignored: statat "docs/plans/2026-07-23: no such file or directory
