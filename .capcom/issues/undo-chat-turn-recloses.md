---
title: "undo: a chat-only exchange re-closes the previous group, clearing redo and absorbing human edits"
status: done
type: bug
priority: 1
plan: 2026-09-23 - 00 - slow-box-follow-ups-and-stale-state-plan
aliases: [apogee-undo-chat-turn-recloses]
created: 2026-09-23T06:17:45Z
closed: 2026-09-23T06:56:02Z
reason: "Item 10: an exchange that opened no group closes none; resolved by plan '2026-09-23 - 00 - slow-box-follow-ups-and-stale-state-plan' (archived at 152dc896)"
---

Found by the regression check of 'docs/plans/2026-09-23 - 00 - slow-box-follow-ups-and-stale-state-plan.md' (2026-09-23). Not a regression: latent since ac9cbcf6 (feat(undo): snapshot-backed Exchange groups, v0.20.8); identical at v0.22.7 and before 9f684b83.
Journal.Close -> closeGroup (internal/undo/snapshot.go) never consults pending. An exchange that makes no write-capable tool call never reaches MarkPre/openGroup, so pending stays true (set by takeWalk or loop.go BeginGroup) and closeGroup acts on groups[len-1] — the PREVIOUS, already-closed group — re-capturing its post image and re-diffing against its pre.
Effects: (1) after /undo, any chat-only exchange with >=2 snapshotted groups finds a non-empty diff, sets j.redo = nil and bumps the generation — /redo is lost. (2) Even without undo, every chat-only exchange re-closes the previous group, so edits the human made between exchanges are absorbed into that group's post image and a later /undo reverts them too.
No test covers it (TestRedo_MaterialisedGroupClearsTheStack_BareBeginGroupDoesNot never calls Close). Candidate fix: Close is a no-op for the group when pending is true (no group opened this exchange) — confirm cancel/re-attempt paths never rely on re-closing.
