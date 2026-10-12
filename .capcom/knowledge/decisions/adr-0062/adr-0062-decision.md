---
title: ADR 0062 decision
---

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
