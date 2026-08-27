# Test drivers

**Date:** 2026-08-27 · **Status:** 🚧 **skeleton** — each section is filled by the plan item named
under its heading · **Owner ADR:**
[ADR 0062](../adr/0062-test-drivers-are-drivers.md) ("test drivers are Drivers") ·
**Realised by:** `docs/plans/2026-08-27 - 02 - test-drivers-kit-plan.md`

> **How to read this file.** It is the practical counterpart to ADR 0062: the ADR says *why* the
> kit exists and what it may not do, this document says *how to use it*. If you are about to write
> a test — or about to write a numbered human step — start at
> [Which driver observes which claim](#which-driver-observes-which-claim).

## Why

Before this kit, a claim about apogee's screen had no assertion form. The TUI had no driver, so
tests reached `tui.Model` with a `fakeEngine` and proved the reducer rather than the program;
every upstream was a hand-rolled `httptest` closure, so streaming, tool calls, usage and failure
modes were re-invented per test; and claims about wording or shape had no observer at all. The
result was a release checklist with 19 of 25 items marked manual — most of them mechanical claims
that only *looked* subjective because nothing could look at them.

The kit closes that gap with three packages and one rule. `internal/stubllm` is the single scripted
upstream, `internal/tuitest` drives the real composition and reconstructs what the terminal shows,
and `internal/judge` renders binding verdicts on the judgment halves. The rule is ADR 0062's: a
test step may be **manual** only where the table below says no driver observes its claim.

## stubllm

*Filled by plan items 2 and 3.* Script format (`turns:`, `when:`, `repeat:`), matching and strict
behaviour, the turn kinds (text, reasoning, tool calls, usage, http, hang), the request log, and
the `cmd/stubllm serve` / `cmd/stubllm record` binary.

## tuitest

*Filled by plan items 4, 5 and 6.* The `Frame` type over `github.com/charmbracelet/x/vt` (text,
cell styles, cursor, cell width), the in-process `Driver` over `runRoot`'s launcher seam, the
black-box `PTYDriver`, and the golden-frame convention under `cmd/apogee/testdata/frames/`
(`go test ./cmd/apogee -update`).

## judge

*Filled by plan item 7.* The `APOGEE_JUDGE_ENDPOINT` / `APOGEE_JUDGE_MODEL` gate, how a rubric is
written beside its test, the named text artifacts a verdict is rendered on, `judge.Pairwise`, and
what a binding `fail` looks like in `go test` output.

## Which driver observes which claim

Seeded here with the proxies ratified for the irreducible halves (ADR 0062, decision 5); **the rest
of the rows are filled by plan item 8**, and plan item 16 re-checks every example-test name against
`go test -list 'TestE2E.*' ./cmd/apogee/`. Names marked *(planned)* do not exist yet.

| Claim class | Driver | Example test | Not observable by any driver |
| --- | --- | --- | --- |
| Wide-rune and glyph alignment (T-20) | `tuitest` — the emulator's cell width is the authority; assert `Frame` cells, never rune counts | `TestE2EWidth…` *(planned, item 12)* | Font tofu — whether the reader's font has the glyph at all |
| Flicker during streaming (T-24) | `--tui-trace` counters: bytes written and full-frame repaints per streamed token, pinned against a ceiling | `TestE2EStreamRepaintCeiling` *(planned, item 9)* | Felt flicker — the repaint ceiling is the accepted proxy |
| Desktop hand-off (T-19) | a logging fake opener installed through `present.Opener.LookPath`; assert argv and wording | `TestE2EPresent…` *(planned, item 14)* | What a real desktop application does with the file |
| Upgrade path of an installed apogee (T-21) | post-release `make release-smoke` — archives, `SHA256SUMS`, `--version`, `brew upgrade` when `brew` is present | `make release-smoke` *(planned, item 15)* | `brew upgrade` before the release it upgrades to exists |
| Tag job and action pins (T-21) | `actionlint` plus `scripts/check-pins.sh`, both run from `make check` | `make check` *(planned, item 15)* | — |
| Landlock residual honesty on an older ABI (T-11) | a dedicated `ubuntu-22.04` CI job running the probe assertions | CI job `landlock-abi-1-2` *(planned, item 15)* | — |
| Behaviour against a real model (T-22) | stays env-gated under `make live-eval`; unset means skip, never a silent pass | `TestE2ELiveModel` | — (needs a live tool-capable endpoint) |
| The docs work for a newcomer (T-23) | judge-driven agent in a clean container with only `README.md` + `docs/manual/`, driving apogee against stubllm | `TestNewcomerFollowsTheDocs` *(planned, item 15)* | The Homebrew and OpenRouter steps — they need a published release and a real API key |

## Writing a new e2e test

*Filled by plan item 8.* The checklist a new `cmd/apogee/e2e_<topic>_test.go` follows: pick the
driver from the table, script stubllm instead of writing a handler, assert on `Frame` cells (or a
golden for a rendering surface), add a judge rubric only for a judgment half, and keep inside the
budget below.

## Gates and budgets

*Filled by plan items 5, 6, 7 and 16.* Which tests run in a plain `go test`, what skips where (PTY
on Windows or without a pty; judge without its gate), which join `make live-eval`, and the measured
wall clock of the whole e2e set under `-race` against its ~15 s ceiling. No build tags, no `-short`.
