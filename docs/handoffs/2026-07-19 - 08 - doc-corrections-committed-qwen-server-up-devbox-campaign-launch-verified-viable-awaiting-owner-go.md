# Handoff — doc corrections committed; qwen server up & pins verified; devbox campaign launch verified viable; next: launch from the devbox once the owner confirms the Mac won't sleep and says go

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
3. **Challenged the host-side-launch assumption (owner: "this dependency is not
   good") and verified a devbox launch is viable.** No technical blocker:
   `make build` succeeds on the devbox at `6634376` (current `main`, which
   *includes* the tool-call preflight and engagement guard — runbook step 1
   satisfied on this machine; fresh binary at `../apogee-sim/apogee-sim`); the
   devbox config's `upstream.url` is already `http://192.168.64.1:1111`, the
   default for `campaign run -endpoint`; the store root `~/.apogee-sim/campaigns`
   is created on demand (`internal/campaign/store.go:174`) — a NEW campaign needs
   no pre-existing store. The runbook's "host-side" (D6) framing rested on facts
   now false (stale devbox binary) or inapplicable to a fresh bundle (resume needs
   the bundle's machine). Launching here is an operational amendment to the
   runbook, not a change to the frozen design (arms/reps/δ/checkpoint untouched);
   record the deviation in the bundle's `CHECKPOINT.md`.

## Owner correction — campaign-store location is OPEN (do not assume the Mac host)

Handoffs 06/07 and the runbook's framing assumed the campaign bundles and store
live on the Mac host. Owner, this session: the latest sims may have run on
**another computer**, so the qwen25 bundles and the `~/.apogee-sim/campaigns`
store may be there instead. Re-verified this session: the **devbox** has no store
(`~/.apogee-sim/campaigns` absent — only config/sessions/traces). Implications:

- With the devbox-launch finding (item 3 above), the store's location now matters
  only for *existing* bundles: runbook step 2 (the qwen25 `not-engaged` stamps)
  must run wherever those bundles actually live — locate them
  (`ls ~/.apogee-sim/campaigns` on the candidate machines). The new qwen3.6
  campaign creates its bundle fresh wherever it launches (planned: the devbox).
- The server itself runs on the Mac host regardless (llama-launcher and the model
  files live there); `192.168.64.1:1111` is the devbox's view of it, correct for
  a devbox launch.

## Next moves

1. **Launch the campaign from the devbox** — blocked on exactly two things from
   the owner: confirmation that **the Mac will not sleep** for the run (8–16 h
   checkpoint slice, day-plus full 140; the server and this VM both live on the
   Mac — caffeinate or power settings, unreachable from here) and an explicit
   **go**. Then runbook step 4 with the fresh `../apogee-sim/apogee-sim` binary:
   create with `-model /Users/airic/LL-Models/Qwen/qwen3.6-27B-Q4_K_S.gguf
   -reps 5`, interrupt once the id prints, drive to 56/140 with `-id <id>
   -reps 2` — detached/background and unsandboxed so it survives the session —
   then apply the checkpoint rule and write `CHECKPOINT.md` (including the
   launch-machine deviation note). Full runbook:
   `../apogee-sim/docs/plans/qwen36-27b-first-aggregate-campaign-plan.md`.
2. **Operator, on whichever machine holds the qwen25 bundles:** runbook step 2
   (`campaign analyze` `not-engaged` stamps) — independent of the launch.
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

- apogee `main` local = `a5ef2d3` + this handoff-update commit, clean; 3 ahead /
  0 behind `origin/main` (`1f70aeb`). apogee-sim = `6634376`, clean, level with
  origin; fresh `apogee-sim` binary built this session at
  `../apogee-sim/apogee-sim` (untracked build artifact). Devbox
  `~/.apogee-sim/campaigns` does not exist yet — it appears on first
  `campaign run`.
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
- Do not launch before the owner confirms host-sleep handling and gives an
  explicit go; do not resume or analyze bundles that live on other machines from
  here. Do not re-grill the pre-registration — the design freeze is untouched by
  the launch-machine amendment.
- New from this session: do not assume which machine holds the qwen25 bundles —
  locate them before step 2 (owner correction above).

## Suggested skills

- **`manage-llm-server`** — observing the server during the run
  (`tail_log`/`server_status`); mutating calls only with owner confirmation while
  a campaign may be in flight.
- **`handoff`** — at session end, superseding this doc.
- **`grill-with-docs`** — only if a campaign outcome (checkpoint kill, gate fail)
  reopens design; the pre-registration itself is settled.
