# Side panel follow-ups — plan

- **Goal:** fix the two defects deferred out of the side-panel run: the sub-agents section's 30 s
  linger expires only on an unrelated repaint, and a symlinked workspace root splits one changed
  file across two rows.
- **Date:** 2026-11-08
- **Status:** unexecuted
- **sized for:** ~200k-context host
- **base:** f7b32609
- **Sources:** `.capcom/issues/panel-linger-idle-repaint.md`; `.capcom/issues/panel-write-key-symlink.md`;
  `.capcom/knowledge/decisions/adr-0110.md` and its children (`capcom know show "ADR 0110" --with-children`);
  `docs/layout/side-panel-layout.md` §"Sections"; `docs/plans/archived/2026-11-08 - 00 - tui-side-panel-plan.md`;
  `docs/design/test-drivers.md` §Waiting.
- **Ratified design calls** (plan author, 2026-11-08; no user-visible change beyond the fixes):
  - **Linger deadline:** the Driver-neutral fold answers when the sub-agents section stops being live;
    the TUI arms one gen-guarded `tea.Tick` at that deadline. No periodic panel tick.
  - **Write key:** both sides of the key (live and resume) resolve through the real path, so a
    resumed row reads as the live write already does (the engine's `WriteTarget` is the real path).
- **Standing requirements:**
  - skills: coding-standards
  - Pi box: tests only as `GOMEMLIMIT=2GiB go test -race -count=1 -run '<TestX>' ./<one package>/`;
    never a package pattern, never two runs at once.
  - Durations in tests go through `internal/testtime`; no test sleeps out the real 30 s linger.
  - The Model stays under `TestModelFitsTheStack`.
- **Out of scope:** symlink handling in the transcript's path shortener (`workspaceRoot.shorten`);
  changing the 30 s linger value; any other side-panel section's live rule.
- **Regression check (2026-11-08, f7b32609):**
  - 1: guard folded — arm from `Model.fold`'s eventMsg arm, route the tick in `watchRouteOf`; deadline timed on the panel's clock.
  - 2: guard folded — driven test runs resume-then-live, Model built through `newModel` with a symlinked `opts.Workspace`.

## 1. Sub-agents section leaves the panel at its linger deadline on an idle frame — ✅ DONE (2026-11-08)

NOTES (2026-11-08): the deadline is answered by a new `State.SubAgentLinger()` (type `sidepanel.Linger{Until, Left}`, Left on the State's own clock) sharing one `lingerAfter` rule with `SubAgents().IsLive`, rather than a new field on the generic `View[R]` every section returns.
NOTES (2026-11-08): internal/tui/fold.go is untouched — per the plan's regression guard the arm leaves from `Model.fold`'s `case eventMsg` (both returns, via `Model.armPanelLinger`), since `foldEventAgainst` returns no Cmd.
NOTES (2026-11-08): the handled tick (`foldPanelLinger`) also re-arms the remainder when it lands before the panel's clock reaches the deadline (wall-clock vs monotonic skew), pinned by TestSidePanelEarlyLingerTickArmsTheRemainder; an Event that moves the deadline to zero (a new delegation queued) also bumps the generation, retiring the outstanding tick.
NOTES (2026-11-08): only the live eventMsg path arms; a watch's own panel (repainted by the follower's poll anyway) and an in-TUI restore's rebuilt panel arm nothing, and the session boundaries leave the arm fields as they were (a stale arm is retired by the next Event or spends itself as a harmless repaint).

**What:** fix for `panel-linger-idle-repaint`: the sub-agents section's linger (`subAgentLinger`) is
judged only at paint time, so an idle frame keeps the section past 30 s until an unrelated repaint.

**Regression guard.** `foldEventAgainst` returns only a Model, so the arm Cmd leaves from `Model.fold`'s `case eventMsg`
(batched into both returns) with a gen-guarded tick case there; `watchRouteOf` routes the tick `watchRouteOwner` (the gen
lives on the parked `sessionView`). The tick's duration is the panel's own time left (`deadline − s.now()`, answered by
`internal/sidepanel` beside the deadline), never `time.Now()`; the TUI test never runs the armed Cmd.

**Goal:** with no other activity, the TUI repaints at the moment the sub-agents section's linger
ends, and that repaint no longer shows the section; a test pins it on an otherwise idle Model.

**Approach (assumed at the header base):**
- `internal/sidepanel`: the sub-agents answer (`State.SubAgents`, `View[SubAgentRow]`) also reports
  the instant its live rule next turns false — `lastEnd + subAgentLinger` while only the linger keeps
  it live, zero while a run is running/queued or once the linger has passed. Extends the existing
  fold; no new module. Driver-neutral: no Bubble Tea import.
- `internal/tui`: after `foldSidePanel` (called from `fold.go`), when that deadline is set and differs
  from the one already armed, arm `tea.Tick(deadline − now, …)` carrying a generation; a tick whose
  generation is stale is dropped. Follow the house gen-guarded one-shot pattern (`spinner.go`,
  `heartbeat.go`, `mouse.go` flash). The handled tick returns the Model so the frame repaints.
- The armed-deadline/generation fields live with the panel state (`sessionView`), not as new
  by-value no-copy types on `Model` (ADR 0011).
- The panel's clock is the one `sidepanel.New(now)` already takes; the TUI test injects a fake clock
  through the Model's existing seam, or adds one, rather than waiting 30 s.

**Files:** internal/sidepanel/sidepanel.go, internal/sidepanel/sidepanel_test.go, internal/tui/sidepanel.go, internal/tui/fold.go, internal/tui/model.go, internal/tui/watch.go, internal/tui/sessionview.go, internal/tui/sidepanel_test.go
**Read first:** internal/sidepanel/sidepanel.go — State.SubAgents, View; internal/tui/model.go — Model.fold (case eventMsg, case flashClearMsg);
  internal/tui/sidepanel.go — Model.foldSidePanel; internal/tui/watch.go — watchRouteOf; internal/tui/sessionview.go — sessionView.panel;
  internal/tui/sidepanel_test.go — panelModel, delegationPhase

**Tests:**
- `internal/sidepanel`: deadline is `lastEnd + 30 s` after the last run ends, zero while one runs or
  queues, zero after the deadline passes; the time left is `deadline − now` on the injected clock (fake clock).
- `internal/tui`: step a delegation's start and end as `eventMsg` through `stepCmd` (not `panelModel`'s
  `m.foldEvent`, which returns no Cmd) on the panel clock fixed at `panelNow`; assert the armed deadline and
  generation on the Model; advance the panel clock past the deadline and deliver a hand-built tick Msg (the
  `flashClearMsg` pattern), never running the armed Cmd — it removes the Sub-agents section from the painted
  column; a tick from a superseded generation changes nothing.
- `TestWatchIsolatesRoutesEveryFoldedMsg` stays green with the new tick case routed in `watchRouteOf`.

**Acceptance:**
- `go build ./internal/sidepanel/ ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/sidepanel/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'SidePanel|Panel' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run TestWatchIsolatesRoutesEveryFoldedMsg ./internal/tui/`

**Closes:** panel-linger-idle-repaint

**Commit:** `fix(tui): repaint the side panel when the sub-agents linger ends`

## 2. One changed-files row for a file written under a symlinked workspace root — ✅ DONE (2026-11-08)

NOTES (2026-11-08): the root's real path is a new `realRoot` field on `workspaceRoot`, set by `newWorkspaceRoot` via `security.EvalRealPath`; `root` and the shortener are unchanged. A hand-built `workspaceRoot{root: …}` literal leaves `realRoot` empty, so `panelWriteKey` returns the absolute real path there (no production code builds one; tests build through `newModel`/`newWorkspaceRoot`).
NOTES (2026-11-08): `panelWriteKey` with no workspace returns the path only cleaned, as before (no symlink resolution); a path outside the workspace now keys as its absolute real path, which a resumed row shows the way the live write already did.

**What:** fix for `panel-write-key-symlink`: `Model.panelWriteKey` takes `filepath.Rel` against the
configured root only. The live `ToolResultEvent.WriteTarget` is the engine's real path
(`writeTarget.Real`, `security.EvalRealPath`), the resumed key is the stored argument's spelling, so
under a symlinked root the same file folds under two keys and shows on two rows.

**Regression guard.** `rebuildSidePanel` replaces `m.panel` whole, so the driven test runs resume-then-live (as
`TestSidePanelResumeRebuildsThroughBothDoors` does): a record with a relative stored arg resumed under `opts.Workspace=<link>`,
then a live write of the same file with `WriteTarget=<real>/…` → one row. Every test builds the root through `newModel`
(or `newWorkspaceRoot(link)`), never a `workspaceRoot{}` literal, which leaves the real root empty.

**Goal:** `panelWriteKey` returns one key for a file whatever spelling reaches it — relative, absolute
through the symlinked root, or absolute through the real root — and the live fold and the resume
rebuild of the same write land on one changed-files row.

**Approach (assumed at the header base):**
- Producers of the key's input: `foldSidePanel` (live `WriteTarget`, already real) and the resume
  rebuild's `panelWriteTarget`/`rebuildSidePanel` (stored argument). Consumers: `sidepanel.State`'s
  changed-files fold and `panelFileRow`, which shortens the key. Both producers go through the one
  `panelWriteKey`; no second keying function.
- `panelWriteKey`: join a relative path onto the root, resolve the result with `security.EvalRealPath`,
  and take `Rel` against the root's real path; a non-local result stays the cleaned absolute real path.
- The root's real path is resolved once per session, not per write: carried on `workspaceRoot` and set
  in `newWorkspaceRoot` (`internal/tui/workspacepath.go`); the existing `root` field and the shortener
  stay as they are.

**Files:** internal/tui/sidepanel.go, internal/tui/workspacepath.go, internal/tui/sidepanel_test.go
**Read first:** internal/tui/sidepanel.go — panelWriteKey, foldSidePanel, replayedWriteKey, rebuildSidePanel;
  internal/tui/workspacepath.go — workspaceRoot, newWorkspaceRoot; internal/security/pathsafety.go — EvalRealPath;
  internal/tui/sidepanel_test.go — TestSidePanelResumeRebuildsThroughBothDoors

**Tests:**
- `panelWriteKey` under a `t.TempDir()` real root reached through a symlink (Model built through `newModel`
  with `opts.Workspace=<link>`, or `m.transcript.ws = newWorkspaceRoot(link)`; never a `workspaceRoot{}` literal):
  `a.go`, `<link>/a.go` and `<real>/a.go` give one key; a path outside both stays absolute; an empty root behaves as before.
- Driven fold: a resume rebuild of a stored `write_file` call spelling the path relative, under a symlinked root,
  then a live `ToolResultEvent` with the real-path `WriteTarget` for the same file, yields one changed-files row
  (two before the fix).

**Acceptance:**
- `go build ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'PanelWriteKey|SidePanel|WorkspaceRoot' ./internal/tui/`

**Closes:** panel-write-key-symlink

**Commit:** `fix(tui): key changed files by real path under a symlinked workspace root`
