---
title: Undo pre-image snapshot git runs inside the workspace box on confined Auto writes (index.lock Permission denied)
status: done
type: bug
priority: 1
plan: 2026-09-17 - 02 - ci-flake-causes-plan
aliases: [apogee-y72]
created: 2026-09-17T10:45:39Z
closed: 2026-09-19T16:00:09Z
reason: "Implemented by docs/plans/archived/2026-09-17 - 02 - ci-flake-causes-plan.md (items ✅ DONE 2026-09-17); CHANGELOG [Unreleased] carries the entries. Closed late: bd had no database on this host at closeout."
---

Every write_file / edit_existing_file Run in Auto + confine-to-workspace on a landlock host paints 'undo: capture the pre-image of <ws>: apogee: snapshot: capture: git write-tree: exit 128: fatal: Unable to create <home>/snapshots/<session>/index.lock: Permission denied'. Root cause: executeRun (internal/agent/dispatch.go ~L908) installs the Confinement handle for confineChildren Runs BEFORE executeTool, so 'floorCtx := ctx' (dispatch.go ~L1270, whose comment claims it is taken before the handle goes on) already carries it and journal.MarkPre(floorCtx) runs the snapshot store's git inside the workspace box, where <home>/snapshots is unwritable. Journal.Close's post image fails the same way. Undo whole-tree images (ADR 0074) have been broken in that cell since 43f07c6a (2026-09-07); Allow-Edits and Ask-Before are unaffected. Reproduced locally (OrbStack kernel, landlock available) via TestE2EAnnouncedWorkspaceThroughASymlink's final frame; on CI the two failed re-exec'd git spawns per write push that test past its 5s wait (5 of the last 11 red runs). Fix: derive floorCtx from a ctx WITHOUT the handle (strip it in executeTool, or take it in executeRun before WithConfinement) so apogee's bookkeeping git never runs inside the model's box while git_stage's child still does. Add a regression test that a confined Auto write leaves the journal with a pre image (or that no 'undo:' ErrorEvent is emitted).

## Acceptance

A confined Auto write on a landlock host emits no 'undo:' ErrorEvent and the journal holds a pre/post image; git_stage's own child stays confined; TestE2EAnnouncedWorkspaceThroughASymlink's frame carries no 'Permission denied'.
