---
Status: accepted
---

# Test drivers are Drivers

## Context

The release checklist for v0.17.1→HEAD
(`docs/test-checklists/2026-08-27 - 00 - since-v0.17.1.md`) shipped with **19 of its 25 items
marked manual** — T-03, T-04, T-06, T-07, T-10–T-16, T-18–T-25. Reading them, almost none of those
claims is actually subjective: "the streamed reply commits complete and in order", "the approval
keys are armed before the pane paints", "a hostile tool name cannot forge a row", "the outcome slot
reports the tool's data, not its prose". Those are assertions about bytes on a screen. They became a
human's job for three structural reasons.

**1 — The TUI had no driver.** Nothing in the repo can start the real composition, type into it and
read back what the terminal shows. The existing tests reach `tui.Model` with a `fakeEngine`, which
proves the reducer and says nothing about the program that runs it — not the Bridge, not the event
sink, not the renderer, not the terminal. Anything that only goes wrong once bytes reach a terminal
was, by construction, unobservable.

**2 — Every upstream in tests was an ad-hoc `httptest` closure.** Each test that needed a streamed
reply hand-rolled its own SSE frames. Nothing was reusable, so per-token timing, `reasoning_content`,
tool calls, `usage.prompt_tokens_details.cached_tokens`, HTTP failures and hangs were each
re-invented per test — or, more often, skipped and handed to the checklist.

**3 — The judgment halves had no observer at all.** "The blast-radius wording reads as one row",
"the pane's wording is honest about what was refused", "the manual matches what the binary does":
no assertion form existed for a claim about wording or shape, so those halves went to a human's
eyes by default and dragged their mechanical siblings with them.

[ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)
already settles the principle this record applies: the embeddable engine must stay **sufficient for
any Driver**, and everything must stay benchable all the way up. A test harness that needs a hook
the engine does not already offer is evidence the engine is *not* sufficient. So a test harness is
not a special case to be granted privileges — it is another **Driver** (CONTEXT.md), and building it
exercises the north star instead of bending it.

The kit's shape was ratified with the owner on 2026-08-27 and is realised item-by-item by
`docs/plans/2026-08-27 - 02 - test-drivers-kit-plan.md`. Two existing invariants bound it:
[ADR 0011](0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md) (the `Model` is copied by
value on every `Update`, so no no-copy type may reach it) and
[ADR 0010](0010-package-layout-domain-core-and-thin-root-facade.md) (`internal/` never imports the
root module path).

## Decision

**1 — Test drivers enter through the composition seam; no test-only hook is added to the engine.**
The entry is `runRoot`'s `launcher` (`cmd/apogee/root.go`) plus a split of `tui.Run` into
"build the program" and "run it", so a test can take the real `tea.Program` with its own input,
output and window size. What a driver drives is therefore the **real** composition — Agent, tools,
session store, Bridge, filewatch, config watch — not a `Model` over a `fakeEngine`. The engine, the
provider and the tools gain nothing test-only: no exported test hook, no `if testing.Testing()`, no
back door. When a claim cannot be observed from the composition seam the answer is to widen the seam
for **every** Driver, or to accept the claim as not observable and record it as such — never to
carve a private path for the tests.

**2 — `internal/stubllm` is the ONE scripted upstream.** It is an OpenAI-compatible server driven by
a YAML `Script`: ordered `turns:`, an optional `when:` matcher (regex over the last message's text,
or `tool_result: <name>`), `repeat: true`, and strict behaviour — an unmatched request is HTTP 500
and is recorded, never silently answered. Turn kinds cover text (per-token delay, chunk size),
reasoning, tool call(s), usage (prompt / completion / cached tokens), raw http (status, body,
redirect) and hang(ms). It ships as a library plus a thin `cmd/stubllm` binary (`serve`, and
`record --upstream URL --out fixture.yaml` which proxies `/v1/*` and writes a Script with timings
and pre-filled matchers). A new test **scripts** stubllm; it does not write another handler.
Recording is an explicit CLI act by a human, never a `go test` flag.

**3 — `github.com/charmbracelet/x/vt` is the frame authority for both drivers.** The in-process
driver and the black-box PTY driver both feed the renderer's bytes through one terminal emulator and
reconstruct one `Frame` type — plain text, cell styles, cursor, cell width. A claim about the screen
is asserted **on cells**: never on a `View()` string (which is not what the terminal shows) and never
on raw byte sequences (which are not what a human sees). The consequence is that the two drivers
answer the same question the same way, and the only difference between them is what produced the
bytes.

**4 — The LLM judge is a first-class oracle for the judgment halves.** `internal/judge` sends named
text artifacts (a frame, stdout, a file, a transcript) plus a rubric — copied verbatim from the
checklist item's own "Pass when / Fails if" — to an OpenAI-compatible endpoint through the repo's own
`internal/provider` client. Strict-JSON verdict (`{verdict: pass|fail, reasons: []}`), temperature 0,
one vote, plus `judge.Pairwise(before, after)` for "no worse than the last release" claims. It is
gated by `APOGEE_JUDGE_ENDPOINT` / `APOGEE_JUDGE_MODEL` (defaulting to `APOGEE_LIVE_ENDPOINT` /
`APOGEE_LIVE_MODEL`) and skips when unset, exactly like the live tests — but where the gate **is**
set the verdict is **binding**: a `fail` fails the Go test and prints the reasons. An advisory judge
would be a judge nobody reads.

**5 — These proxies are the automated definition of the irreducible halves.** Where a claim cannot be
observed directly, the proxy below *is* the test, and passing it is what "that item passes" means:

| Checklist claim | The automated definition |
| --- | --- |
| T-20 — a wide glyph occupies the right number of columns | the emulator's **cell width** is the authority; assert `Frame` cells, not rune counts |
| T-24 — the stream does not flicker | bytes written and full-frame repaints **per streamed token**, read from the `--tui-trace` seam and pinned against a ceiling |
| T-19 — the document is handed to the desktop | a logging fake opener installed through `present.Opener.LookPath`; assert the argv and the wording |
| T-21 — an installed apogee upgrades cleanly | a **post-release** `make release-smoke` (archives, `SHA256SUMS`, `--version`, and `brew upgrade` when `brew` is present) |
| T-21 — the tag job and its action pins are sound | `actionlint` plus a pin grep (`scripts/check-pins.sh`), both run from `make check` |
| T-11 — the landlock residual is honest on an older ABI | a dedicated `ubuntu-22.04` CI job running the probe assertions |
| T-22 — behaviour against a real model | stays env-gated under `make live-eval`; unset means skip, never a silent pass |
| T-23 — the docs work for a newcomer | a judge-driven agent in a clean container with **only** `README.md` + `docs/manual/`, driving apogee against stubllm and reporting every step that did not work as written |

A claim that is neither on this list nor in the design doc's "Not observable" column has a driver,
and is automated rather than written as a human step.

## Consequences

- **"Manual" now needs a reason on the record.** `docs/design/test-drivers.md` carries the
  *Which driver observes which claim* table; a test step may be manual only where that table's
  "Not observable by any driver" column covers its claim class. The `test-checklist` skill gates on
  it, so the default for new work flips from "write a numbered human step" to "write a driver test".
- **`teatest`, `vhs` and `tmux` are settled and not to be re-proposed.** `teatest/v2` is a
  pre-release pinned to bubbletea `v2.0.0-rc.1` while this repo is on `v2.0.8` — adopting it means
  downgrading the TUI or maintaining a fork. `vhs` is an external binary that renders a GIF, so it
  is a prerequisite ([ADR 0042](0042-external-programs-are-optional-enhancements-never-prerequisites.md)
  forbids those) and its output is pixels, which no Go test can assert cells against. `tmux` is an
  external prerequisite too, and it inserts a *second* terminal emulator between the program and the
  assertion, so `capture-pane` polling yields strictly less fidelity (styles, cursor, cell width)
  than reading our own emulator.
- **The kit is maintained like production code.** It lives under `internal/` (plus one `cmd/`), it is
  documented in `docs/design/test-drivers.md`, its own behaviour is tested, and it is covered by
  `make check`. A broken driver is a broken build, not a test-only annoyance to be skipped past.
- **The repo gains a second binary that users never install.** `cmd/stubllm` is a developer tool
  built from source; it is not in `make dist`, not in the Homebrew formula, and not in the manual's
  install path.
- **Gating stays boring.** The in-process and PTY e2e tests run in every plain `go test` — no build
  tags, no `-short` — with the whole e2e set budgeted at ~15 s under `-race`; PTY tests skip on
  Windows or where no pty is available; judge tests skip without the gate and join `make live-eval`.
- **Nothing here is a Mechanism.** The kit observes apogee; it never changes what a model sees.
  stubllm replaces the upstream rather than steering it, the drivers add no prompt text, and the
  judge reads artifacts after the fact. There is nothing for Bypass to switch off and nothing for a
  bench arm to measure, so the Bypass floor is untouched.
