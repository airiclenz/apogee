# Handoff 18 — Phase 2 complete (P2.6 e2e + live-model run landed, `-race`-green); next is Phase 3 entry

**Date:** 2026-06-23 · **Branch:** `main` (commit directly — pre-production owner directive) ·
P2.6 code landed at **HEAD `6f763fc`** (`test(cli): P2.6 — hermetic e2e + live-model run close the
Phase-2 deliverable`); this doc + the plan record land in the following `docs(…)` commit.

## Where things stand

**Phase 2 is COMPLETE.** P2.0–P2.6 all landed and `-race`-green. `make check` passes (gofmt · vet ·
build · race tests · ADR-0010 grep empty · 6-target cross-build · `--help` exit 0); `go mod tidy` has
no drift (no new deps in P2.6). The broad-plan §4 Phase-2 deliverable holds end-to-end: **a real
coding conversation with a local model in the terminal — tokens stream, a tool call appears, the
human approves the write, the result renders** — proven hermetically *and* against a live model.

- **Authoritative plan:** [`docs/plans/phase-2-detail-plan.md`](../plans/phase-2-detail-plan.md) —
  now **✅ COMPLETE**; the §4 table marks **P2.0–P2.6 ✅**, and the **P2.6 "✅ Done (HEAD `6f763fc`)"**
  block records exactly what shipped. Do not re-derive Phase 2 — it is built and green.
- **ADRs unchanged:** P2.6 introduced **no new architectural decision** — it is pure test/proof code.
  0011 (TUI concurrency seam), 0007 (quiescent boundary / snapshot-resume), 0001 (no implicit
  `~/.apogee`; the binary injects roots) all held exactly as built.

## What P2.6 built (both in `internal/tui` test code — production `tui` untouched)

- **`internal/tui/e2e_test.go`** — the hermetic, reproducible proof (`-race`). A **stateless**
  scripted OpenAI-compatible `httptest` model decides each reply from the request's own message
  history (fresh task → narrate + `write_file`; history ends in a tool result → final message; a
  later user turn → a plain closing reply), driving a tool Turn, a final Turn, and the continuation
  Turn the way a real model does. A small white-box **`uiHarness`** stands in for `*tea.Program`: it
  drains the Msgs the seam Sends into a **real `Model` through the real `Update`**, auto-approving a
  prompt exactly as a human pressing `a` would (real keypress → `handleApprovalKey` → C3 reply
  rendezvous). The seam Sends from the one worker goroutine, the harness reads on the test goroutine
  ⇒ only one goroutine touches the Model (race-clean, no lock); it launches the worker via
  `startExchange` directly so no spinner-tick timer enters the loop.
  - `TestE2EConversationThroughTUI`: narration → `write_file` → approve → the write lands in a
    `t.TempDir()` workspace → the transcript folds tokens → call → result → final message.
  - `TestE2ESnapshotResumeContinues`: snapshot on a clean quit through the **real saver seam**
    (`session.Store`) → `agent.Resume` from the written file → continue, proving the resumed Exchange
    picks up at the snapshot's `turnIndex` (turn 2, after exchange 1's turns 0+1), not turn 0.
- **`internal/tui/live_test.go`** — `TestE2ELiveModel`, **opt-in** (skipped unless
  `APOGEE_LIVE_ENDPOINT` is set, so `make check` is unaffected). Same harness + real Model against a
  **live** local model — the open **Phase-1 live file-edit eval**, now over the product surface. Run
  it with:
  ```
  APOGEE_LIVE_ENDPOINT=http://192.168.64.1:1111 go test -race -run TestE2ELiveModel -v ./internal/tui/
  ```
  **Runs done this session:** against the loaded `gemma-4-E4B-it-Q8_0` **and** against
  `gpt-oss-20b-MXFP4` (swapped in via the launcher) — both PASS: streamed, called `write_file`, the
  write approved through the real gate, wrote `greeting.txt` (14 bytes), final message rendered,
  `StatusExchangeComplete`, transcript correct (gpt-oss ~0.9s/Turn). gpt-oss-20b is left loaded.

### Two environment notes from the live run (for whoever runs it next)

- The **llama-launcher MCP** is at **`http://192.168.64.1:7331/mcp`** (same host as the inference
  endpoint `:1111`, not `192.168.61.1` — that earlier address was wrong, hence the timeout). It is
  reachable from the dev env and exposes the **full** toolset (`list_profiles`, `load_profile`,
  `server_status`, …), so models **can** be swapped via `manage-llm-server` / the MCP adapter.
  Tool-capable profiles available: **gpt-oss-20b** (MXFP4 / Q6-K), **Qwen3.6-27B**, and the Gemma-4
  family (e4b / 12B / 26B). The live run this session used the already-loaded `gemma-4-E4B-it-Q8_0`
  (a deliberate no-swap); it **is tool-capable** (emits real `tool_calls`) even though `/v1/models`
  advertises only `["completion"]` — don't trust that capability list.
- There is **no TTY** in the dev env, so the literal interactive alt-screen TUI (a human pressing
  `a`) is the **only** unautomated remainder. The hermetic e2e proves the Model handles that real
  keypress; the owner can run `apogee --endpoint http://192.168.64.1:1111` directly to see it live.

## Next: Phase 3 entry (broad plan §4 "Phase 3 — Full subsystems")

Phase 2 was the thin shell; Phase 3 is the depth. The public Go API stays **v0.x, no stability
promise** until the **end of Phase 3, where `v1.0.0` is cut** (every consumer — TUI, bench, optional
headless — has exercised the surface by then). Scope (see broad plan lines ~347–373):

- **The 30-tool suite** behind the public `Tool` interface (git, terminal, web-fetch/search,
  python-exec, **sub-agent**, ask-user, diagnostics, find-replace family). Apply §3a per tool (a dep
  is a decision). **Sub-agent privileges ≤ parent** (ADR 0005) — do **not** port apogee-code's
  gate-less version. This is also where **`Depth > 0` rendering** lands in the TUI (Phase 2 only
  *tolerates* nested events).
- **MCP client** on the official Go SDK (`modelcontextprotocol/go-sdk`, pinned `v1.6.x` — re-confirm
  the pin at entry). `internal/mcp` is still a bare `doc.go` stub.
- **Auto mode + Confinement**: implement the `platform/` `Confiner` backends — macOS seatbelt +
  Linux landlock. Confinement is a **capability set** (Auto needs fs-write **and** network
  confinement; Linux Auto needs landlock ABI v4 / kernel ≥6.7, else Auto → Ask-Before). **Per-tool
  invariant**: a tool runs unsupervised only if confined, so **MCP tools gate through Approval even
  in Auto.** C8 already refuses `--mode auto` gracefully today; Phase 3 makes it real.
- Finish the riskiest **`processing/`** port (all tool-call formats, thinking/harmony channels);
  validate parity against the TS oracle + the bench.
- **Deliverable:** feature-parity with apogee-code's non-UI behaviour, Auto confined on Mac/Linux;
  **cut `v1.0.0`.**

**Recommended first step:** a **Phase-3 detail plan** (the same shape as the phase-1/phase-2 detail
plans) — Phase 3 is large and several pieces (Confiner capability model, sub-agent privilege
inheritance, MCP non-confinable gating) carry real design weight. Use the **`Plan`** agent or
**`/grill-me`** / **`grill-with-docs`** to pin the order and the acceptance gates before coding.

## Heads-up — uncommitted items in the tree that are NOT mine (left for the owner)

- `graphics/apogee-logo-dark.png` — an in-flight logo export (the `.afdesign` + its `~lock~` are
  `.gitignore`-d; the PNG is untracked). The owner's active design work — left untracked deliberately.
- `docs/handoffs/2026-06-23 - bugfix-note - streamed-reply-crash-fixed-on-main.md` — the parallel
  session's note for the streamed-reply crash fix (already on `main` as `1baec7d`). It is **consumed**
  now (the fix it documents shipped, and P2.6 built on the `string`-typed `transcript.pending` it
  established), but it is still **untracked in git and not mine to commit/move** — left in place for
  the owner to file or discard (matching handoff 17's stance).

## Verify gate (plan §7) — all green at HEAD `6f763fc`

```
make check     # gofmt -l . empty · go vet · go build · go test -race · ADR-0010 grep empty ·
               # 6-target cross-build · apogee --help exit 0
go mod tidy    # no drift (P2.6 added no deps)

# the opt-in live confirmation (needs a tool-capable local model up):
APOGEE_LIVE_ENDPOINT=http://192.168.64.1:1111 go test -race -run TestE2ELiveModel -v ./internal/tui/
```

## Suggested skills

- **`Plan`** / **`/grill-me`** / **`grill-with-docs`** — author the **Phase-3 detail plan** before
  coding (Confiner capability model + sub-agent privilege inheritance + MCP gating need the design
  pass).
- **`/code-review`** — Phase 2 is the natural end-of-phase review point (the whole P2.1 seam → P2.2
  model → P2.3 fold → P2.4 approval → P2.5 config/sessions → P2.6 e2e slice). Worth a pass before
  Phase 3 builds on it.
- **`/coding-standards`** (`go`) — mandatory for every Phase-3 Go body (the package idiom keeps
  section dividers + symbol-first doc comments; the plan/idiom wins over the base "no dividers" rule).
- **`manage-llm-server`** / the llama-launcher MCP at **`http://192.168.64.1:7331/mcp`** — to load a
  tool-capable model for the live TUI run (reachable from the dev env, full toolset; profiles include
  gpt-oss-20b, Qwen3.6-27B, Gemma-4 e4b/12B/26B).
- **`/handoff`** at session end; **`archive-handoffs`** — handoff 17 is consumed (archived with this
  handoff active); the untracked bugfix-note is left in place for the owner.
```
