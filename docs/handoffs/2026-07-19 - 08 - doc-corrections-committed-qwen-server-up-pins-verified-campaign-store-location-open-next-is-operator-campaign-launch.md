# Handoff — doc corrections committed; qwen server up & pins verified (runbook step 3 done); campaign-store location now an open question; next: operator campaign launch

**Date:** 2026-07-19 (supersedes `archived/2026-07-19 - 07 - refocus-truth-check-green-doc-corrections-pending-next-is-host-side-campaign-launch.md` — its pending doc corrections landed as `e244ef8` this session; its next-move list carries forward with runbook step 3 now done and one owner correction below). Work from this directory; apogee-sim is the sibling repo at `../apogee-sim`.

## What this session did

1. **Applied handoff 07's three doc corrections** (owner said "please advise", agent
   recommended all three and applied) — plus a fourth instance the sweep surfaced
   (`implementation-plan-apogee-merge.md:197` also said "~30-tool"). Committed as
   `e244ef8` (`docs: correct stale phase-4/tool-count/skill-deferral comments`);
   see the commit for the exact wording.
2. **Executed runbook step 3 from the devbox** (owner authorized driving the server
   via manage-llm-server): found **no LLM server running at all** (all three
   backends idle, port 1111 refusing), loaded profile `Qwen3.6-27B-Q4_K_S.gguf`
   via the llama-launcher MCP (v1.4.5), and verified every pre-registration pin:
   - Served model id (the `--model` value for `campaign run`):
     `/Users/airic/LL-Models/Qwen/qwen3.6-27B-Q4_K_S.gguf`
   - llama.cpp build `b10068-571d0d540` — exactly the pinned b10068
   - `total_slots: 1`; Q4_K-Small, 27.3B params, n_ctx 32768
   - Server PID 80050 on `0.0.0.0:1111`; model resident and idle awaiting launch.

## Owner correction — campaign-store location is OPEN (do not assume the Mac host)

Handoffs 06/07 and the runbook's framing assumed the campaign bundles and store
live on the Mac host. Owner, this session: the latest sims may have run on
**another computer**, so the qwen25 bundles and the `~/.apogee-sim/campaigns`
store may be there instead. Re-verified this session: the **devbox** has no store
(`~/.apogee-sim/campaigns` absent — only config/sessions/traces). Implications:

- **First operator action: locate the store** (`ls ~/.apogee-sim/campaigns` on the
  candidate machines). Runbook steps 1 (rebuild), 2 (qwen25 housekeeping), and
  4–7 (campaign drive) run on whichever machine holds it — the
  preflight/engagement-guard rebuild prerequisite binds *that* machine's binary.
- The inference endpoint must be re-derived from that machine's viewpoint:
  `192.168.64.1:1111` is the devbox's view of the Mac host; from the Mac itself
  it is `localhost:1111`; from a third machine it is the Mac's LAN address.
  The server itself runs on the Mac host regardless (that's where llama-launcher
  and the model files live).

## Next moves

1. **Operator, on the store machine:** runbook steps 1, 2, 4–7
   (`../apogee-sim/docs/plans/qwen36-27b-first-aggregate-campaign-plan.md`).
   Step 3 is done, but re-check the endpoint address from the store machine's
   viewpoint before `campaign run`.
2. **Devbox, during the run:** observe only, via llama-launcher MCP
   `tail_log`/`server_status`; ≥10 min llama-log silence = campaign not
   progressing.
3. **After the campaign (devbox):** L9 ledger entry in
   `../apogee-sim/docs/design/mechanism-catalogue.md` whatever the outcome; on an
   NI pass the Validated-set writes per the plan doc's disposition table.
4. **Owner's-call housekeeping:** push `main` (origin caught up to `1f70aeb`
   between sessions — 07's "11 ahead" is stale; only this session's two commits
   are unpushed); cut a release for the `[Unreleased]` CHANGELOG block.
5. **Carried deferred follow-ups (04–07, none urgent):** TUI in-transcript banner
   for the validated-set notice; behavioral-probe (medium-confidence) resolver;
   user-run validation tooling writing `~/.apogee/validated/`.

## Operational state at handoff

- apogee `main` local = `e244ef8` + this handoff commit, clean; 2 ahead / 0 behind
  `origin/main` (`1f70aeb`). apogee-sim = `6634376`, clean, level with origin
  (checked this session).
- **Server UP (started this session):** qwen3.6-27B-Q4_K_S resident on llama.cpp
  `b10068`, single slot, idle. llama-launcher MCP at
  `http://192.168.64.1:7331/mcp` (v1.4.5).
- Devbox network note: sandboxed Bash cannot reach the host (instant
  connection-refused); use unsandboxed curl. The MCP streamable-HTTP handshake
  (initialize → capture `Mcp-Session-Id` → `notifications/initialized` →
  `tools/call`) works from the devbox; this session's session-id is dead, start a
  fresh handshake.

## Explicitly NOT next (carried forward)

- All of 06/07's list: no default flips / mechanism deletions from current
  evidence, no new campaign designs, no revisiting the item-4 fallback (deleting
  `exchangeStart`) without a design session.
- Do not launch or resume the campaign from the devbox (no store here), and do
  not re-grill the pre-registration — it is settled and frozen.
- New from this session: do not assume which machine holds the campaign store —
  locate it first (owner correction above).

## Suggested skills

- **`manage-llm-server`** — observing the server during the run
  (`tail_log`/`server_status`); mutating calls only with owner confirmation while
  a campaign may be in flight.
- **`handoff`** — at session end, superseding this doc.
- **`grill-with-docs`** — only if a campaign outcome (checkpoint kill, gate fail)
  reopens design; the pre-registration itself is settled.
