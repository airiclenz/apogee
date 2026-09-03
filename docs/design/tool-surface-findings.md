# Tool-surface findings — the 2026-08-10 poll round and its 2026-08-16 second round

**Provenance:** recorded 2026-08-10 at the close of the tool-surface plan and extended 2026-08-16
by a second poll round; relocated 2026-08-19 out of the issue register, where it lived as the
*Tool-surface findings (4-poll round, 2026-08-10)* entry. This document is the authoritative
record — both poll rounds, the bench arms, the grill topics, the deferred candidates, the
engine-level notes, the denials and the method lessons. The text below is that entry's body
unchanged apart from relative ADR links repointed for this directory; the issue register (`bd`,
`apogee-304`) keeps a pointer entry listing the live gates only, with the seven bench arms as
`apogee-304.1` … `apogee-304.7` in the order (a)…(g).

**Status:** recorded 2026-08-10 at the close of the tool-surface plan (`2026-08-10 - 00`, under
`docs/plans/`, archived on completion). Four polls of the target class — Qwen3.6-35B-A3B ×2,
Gemma-4-26B-A4B, Gemma-4-E4B — were asked what they wanted from apogee's tool surface. The plan
shipped the uncontested improvements and the promoted new tools (`find_files`, `git_status`,
`git_log`, `copy_file`/`move_file`/`delete_file`, `run_tests`) plus the global `tools.disabled`
switch; everything the round raised that did **not** ship is recorded here, so a deferral or a
denial never becomes a silent drop. **None of it is a live gap.**

**Bench experiments required before any tool removal.** Models are unreliable narrators about their
own tooling: the E4B poll preferred patch-only editing — the format small models are measurably
worst at — and a repeat Qwen poll returned a substantially different list, so only REPLICATED
findings count. Nothing leaves the roster on poll evidence alone; each arm below is a bench
experiment, not a decision:

- **(a)** remove `single_find_and_replace` — flagged in all four polls.
- **(b)** patch-only vs find-replace editing — Qwen vs both Gemmas, a falsifiable disagreement.
  *Update 2026-08-16:* deepseek-v4-flash voted retire `edit_existing_file` and keep the
  find-replace family — a second replication for the find-replace side, and the first from above
  the small-model band.
- **(c)** `open_file`/`read_file` merge — **open watch-item, not an experiment arm.** This arm was
  decided by owner call on 2026-08-11 and shipped without the bench experiment the standing rule
  otherwise requires (an owner-ratified exception for this arm alone; the rule still binds (a),
  (b), (d), (e) and (f) — see `CHANGELOG.md` and
  `docs/plans/archived/2026-08-11 - 03 - open-file-read-file-merge-plan.md` for what landed). What
  stays open is what the skipped experiment would have measured: whether sub-35B models find
  `read_file`'s `locate` *parameter* as readily as they found the `open_file` *name*. Method
  lesson 2 below says they may not; `read_file`'s description advertises locate by name to hedge
  it, and a sighting of models no longer locating reopens this arm rather than re-filing it as a
  gap.
- **(d)** measure whether sub-35B models use `view_diff` at all.
  *Update 2026-08-16:* second independent flag (deepseek-v4-flash), which paired it with a
  `write_file` dry-run — see the second-round block below; decide the two together.
- **(e)** `web_fetch` → `http_request` merge — the real question is whether sub-35B models
  distinguish GET from POST; if they don't, the separate named GET tool earns its slot. Both are
  ExternalEffect-classified, so gating doesn't decide it.
- **(f)** do sub-35B models ever discover `edit_existing_file`'s patch mode unprompted? — a
  discovery experiment feeding the explicit-patch-param idea.
- **(g)** a unified `git` action-enum tool vs the named `git_*` family, on the small class —
  recorded 2026-08-25 by the grill that closed the line below. Named tools are the discovery
  surface (method lesson 2) and all five already ride the subproc row, so unifying gains a roster
  slot and loses nothing on gating; which wins is a measurement. The family stays until the arm
  returns; new git verbs are added only on a REPLICATED ask.

**Unified `git` tool vs the `git_*` family: GRILLED 2026-08-25 → arm (g) above.** The
`git_pull`/`git_push` gating question the second round raised is answered by the existing table:
a remote write is a subprocess (the subproc row — and `git push` is already reachable through
`terminal` in Auto, where the network is open, ADR 0012), so a dedicated tool changes no
boundary. A future `git_push` blocks `--force` and protected-branch pushes the way `git_commit`
blocks amend-on-published; no dangerous-action guard rule is added ahead of the tool, and the
candidate itself stays unreplicated.
*(Per-profile tool rosters: GRILLED 2026-08-23 →
[ADR 0057](../adr/0057-the-tool-roster-is-a-third-model-profile-axis-resolved-axis-wise.md);
implementation plan `docs/plans/2026-08-23 - 00 - per-profile-tool-rosters-plan.md`.)*

**Deferred candidates:** env-var parameters on `terminal`/`python_exec` (stable across both Qwen
sessions); `directory_create`/`directory_delete`; `git_stash`; `git_tag`; `file_metadata`; batch
rename/replace operations (×2 — Qwen 2026-08-10 sessions; Qwen3.8-27B 2026-08-16 asked for a
bounded `replace_all` with a replacement cap and shown context); `workspace_summary`.

**Engine-level notes (not tools):** context-window introspection for the model (Mechanism
territory); streaming/progress for long-running tools; structured JSON tool outputs.

**Denied, with reasons** (do not re-file as gaps):

- `database_query` — [ADR 0031](../adr/0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md):
  no first-party connectors, that is MCP's job.
- standalone `apply_patch` — it already exists inside `edit_existing_file`; models missing it is a
  *discovery* problem, tracked as the explicit-patch-param idea in (f) above.
- concurrent `terminal` — parallelism lands at the sub-agent layer
  ([ADR 0039](../adr/0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md)).
- `inspect_environment` — `terminal` covers it.

**Method lessons** (they generalise past this round):

1. Models reliably converge on **problems** but not on **solutions** — removals need measurement.
2. Models discover capabilities by tool **name**, not by parameters — three sightings in one round:
   `list_dir`'s `recursive` missed twice, `edit_existing_file`'s patch mode missed once. Naming and
   descriptions are the discovery surface.

**Second round (2026-08-16, 3 polls — deepseek-v4-flash, Qwen3.8-27B, Qwen3.6-35B-A3B).** Larger
models than the first round. Owner framing, ratified this day, and applied to the identity
statements in `README.md`/`AGENTS.md`/`CONTEXT.md`: **apogee is built for smaller local models —
while working even better with bigger ones.** The ~4B–35B range is dropped from identity docs
(kept in ADRs and past rationales as historical context, and in the bench arms above as a concrete
measurement class); the hard-learned lesson it encoded stays: usefully agentic behaviour starts
roughly where that upper limit sat, so the Mechanisms are help smaller models *need* and bigger
models simply don't switch on. Consequences: the default roster stays tuned for the models that
need the help; feedback from larger models informs the profile/`tools.disabled` tier, not the
default; the per-profile-rosters grill above is promoted — it is the mechanism serving both
classes, and it lets a new tool ship default-off/profile-enabled instead of costing every small
model a tool-list slot.

- **New candidate, replicated within the round (both Qwens): `watch_files` / file-event wait** —
  "block until path X changes or N seconds elapse"; models currently poll with repeated reads.
  Deferred; design belongs with the engine's event model, not a quick tool add.
- **New candidate: `write_file` dry-run/preview** (deepseek) — diff a proposed full-file write
  before writing; preview exists today only for replace-style edits via `view_diff`. Pairs with
  arm (d): a write-preview is what would let `view_diff` fold away — decide them together.
- **Persistent terminal / PTY session** (Qwen3.8's single top pick) — attach to a running dev
  server, drive REPLs/interactive programs via a start/send/poll session pair. NOT covered by the
  first round's `concurrent terminal` denial (that was parallelism,
  [ADR 0039](../adr/0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md));
  this is interactivity, with heavy confinement and lifecycle implications. **GRILLED
  2026-08-25 → the Console family,
  [ADR 0059](../adr/0059-a-console-is-live-host-state-the-model-drives-across-turns.md)**
  (`console_open/send/read/close`, live host state, split along the classification line,
  default-off/profile-enabled); implementation plan pending.
- **Git verbs — deferred candidates, not arms:** `git_add`/staging visibility and `git_blame`
  (Qwen3.8); `git_pull`/`git_push` (Qwen3.6-35B — its gating record is under arm (g) above).
  Each is a single unreplicated ask; one is added only when a second poll asks for it by name.
- **Re-filed and re-denied** (unchanged): env/system-info tool (Qwen3.6-35B) — `terminal` covers
  it. Task/todo persistence (Qwen3.8) — Mechanism territory (guided decomposition), not a tool.
- Method lesson 1 re-confirmed: same roster, one model removes two tools, another removes none.
