# Handoff 00 (2026-06-24) — finalize Phase 2: documentation pass + fix the `TestE2ELiveModel` cache trap

**Branch:** `main` (commit directly — pre-production owner directive) · **HEAD `26bb0ca`** ·
**Scope of the next session (owner-set):** *finalize Phase 2 — documentation, and fix the cache issue
with `TestE2ELiveModel`.* Two focused finalization items; **no new feature work.** Once these land,
the Phase-3 entry pointer ([handoff 18](./2026-06-23%20-%2018%20-%20phase-2-complete-next-phase-3-entry.md))
is the next thing.

## Where things stand (don't re-derive — read these)

Phase 2 is **code-complete and green**. The full picture is already captured; this handoff does not
repeat it:

- **What shipped + acceptance:** [`docs/plans/phase-2-detail-plan.md`](../plans/phase-2-detail-plan.md)
  — status ✅ COMPLETE; the **P2.6 "Done (HEAD `6f763fc`)" block** is authoritative for the e2e + live run.
- **Phase-2 → Phase-3 narrative:** [handoff 18](./2026-06-23%20-%2018%20-%20phase-2-complete-next-phase-3-entry.md).
- **The P2.6 commits:** `6f763fc` (the e2e + live tests), `90832a9` (plan/handoff records),
  `ec2d0f7` (launcher-address correction), `26bb0ca` (gpt-oss-20b live re-confirm).
- **The tests themselves:** `internal/tui/e2e_test.go` (hermetic, `-race`) and `internal/tui/live_test.go`
  (`TestE2ELiveModel`, opt-in via `APOGEE_LIVE_ENDPOINT`).
- **Verify gate** is green at HEAD `26bb0ca` (`make check`; `go mod tidy` no drift).

## Task 1 — finalize the Phase-2 documentation

"Finalize" is the owner's word; confirm scope if unsure, but the concrete candidates are:

- **Consistency sweep of `phase-2-detail-plan.md`.** It was edited incrementally as P2.6 landed (status
  header, §4 table, the P2.6 Done block, two launcher-address corrections). Read it start-to-finish once
  and make sure the §0–§7 narrative reads as a *finished* phase (e.g. §3's intro still speaks in present
  tense about the seam being "the hard part before panes are drawn" — fine as a design record, but check
  nothing reads as still-open). The §5 "open design calls" should all show their resolution.
- **Top-level status.** Check whether anything outside the detail plan tracks phase status and needs the
  "Phase 2 ✅" flip: the broad plan [`implementation-plan-apogee-merge.md`](../plans/implementation-plan-apogee-merge.md)
  (§4 Phase-2 row), a root `README`/`CLAUDE.md` if present, and the ADR index. Grep for `Phase 2` /
  `🚧` / `IN PROGRESS` across `docs/` and the repo root to find stragglers.
- **The untracked owner items** still sit in the tree (intentionally — *not* the agent's to commit):
  `graphics/apogee-logo-dark.png` and `docs/handoffs/2026-06-23 - bugfix-note - streamed-reply-crash-fixed-on-main.md`.
  The bugfix-note's fix shipped long ago (`1baec7d`); the owner may want to file or delete it now — **ask
  before touching either.**
- Consider whether a one-paragraph **Phase-2 retrospective** (what shipped, the seam/ADR-0011 shape, the
  two live models confirmed) belongs anywhere, or whether the plan's Done blocks already suffice (they
  likely do — don't add redundant docs).

## Task 2 — fix the `TestE2ELiveModel` cache trap

**Symptom (observed this session):** after swapping the loaded model via the launcher (gemma → gpt-oss-20b)
and re-running `APOGEE_LIVE_ENDPOINT=… go test -run TestE2ELiveModel …`, Go returned a **cached PASS from
the previous (gemma) run** — same transcript, same `4.185s` timing, marked `(cached)`. Adding `-count=1`
forced a real run and it passed against gpt-oss-20b (~0.9s/Turn).

**Root cause:** `go test` caches a passing result keyed on the test binary, the cacheable command-line
flags, and the env vars/files the test reads (Go instruments `os.Getenv` via the testlog). `TestE2ELiveModel`
reads `APOGEE_LIVE_ENDPOINT` (tracked) but the **live server's currently-loaded model is not a
Go-visible input** — so re-running with the same endpoint but a different loaded model is a cache *hit*.
The test result no longer reflects reality.

**Recommended fix (idiomatic; pick the combination that fits):**

1. **Make the loaded model a tracked input.** Have the test read `APOGEE_LIVE_MODEL` and, when the runner
   sets it to the loaded model name, a model swap changes the env var → the cache busts naturally. Keep the
   discovery fallback for convenience, but a prominent doc comment should tell runners: *to compare models,
   set `APOGEE_LIVE_MODEL` (or pass `-count=1`) — the bare endpoint caches across swaps.* (There is **no**
   clean in-test API to self-disable caching; `-count=1` is the canonical disable.)
2. **Fix the canonical run command everywhere** to include `-count=1`: the doc comment in `live_test.go`,
   the run line in the P2.6 plan block, and handoff 18's verify-gate snippet all currently show the
   cache-prone form. A `make live-eval` target (default `APOGEE_LIVE_ENDPOINT=http://192.168.64.1:1111`,
   always `-count=1`) would make the right thing the easy thing — match the existing `Makefile` style.
3. Add a short comment in `live_test.go` explaining the trap so the next person doesn't rediscover it.

Keep `make check` unaffected — the test still **skips** when `APOGEE_LIVE_ENDPOINT` is unset (a skip is
cached too, which is fine). Don't make the hermetic `TestE2E*` tests uncacheable.

## Environment / live-eval notes (so a re-run works)

- **Local LLM endpoints** (memory `local-llm-endpoints`): inference `http://192.168.64.1:1111` (OpenAI-
  compatible) + **llama-launcher MCP `http://192.168.64.1:7331/mcp`** (full toolset; *not* 192.168.61.1).
  Drive the launcher via the [`manage-llm-server`] skill or raw JSON-RPC over streamable-http (POST,
  `Accept: application/json, text/event-stream`, capture `Mcp-Session-Id`).
- **`gpt-oss-20b-MXFP4` is currently the loaded model** (swapped in this session; the prior
  `gemma-4-e4b-IT-Q8-0-cpp` had ~8.5h uptime). Switching it back is a mutating op — inspect + confirm with
  the owner first. Both models passed `TestE2ELiveModel`.
- **No TTY in the dev env**, so the literal interactive alt-screen TUI (a human pressing `a`) stays the
  owner's to run: `apogee --endpoint http://192.168.64.1:1111`.

## Suggested skills

- **`/coding-standards`** (`go`) — for the `live_test.go` edit + any `Makefile` target (the package idiom
  keeps section dividers + symbol-first doc comments; the plan/idiom wins over the base "no dividers" rule).
- **`/verify`** or **`run`** — after the cache fix, run `TestE2ELiveModel` with `-count=1` against the
  loaded model to confirm a real (non-cached) PASS; and `make check` for the hermetic suite.
- **`manage-llm-server`** — only if the owner wants the loaded model changed for the verification.
- **`archive-handoffs`** at session end — this handoff (today's `00`) supersedes nothing yet; **handoff 18
  stays active** as the Phase-3 pointer. Archive *this* one once its two tasks land, leaving 18 as next.
- **`/handoff`** at session end.
