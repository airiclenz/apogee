---
title: /sessions pop-up opens seconds late on a slow box
status: done
type: bug
priority: 2
labels: [slow-box]
plan: 2026-11-06 - 00 - sessions-popup-instant-open-plan
created: 2026-11-06T19:30:09Z
closed: 2026-11-06T20:17:41Z
reason: "Resolved by plan 2026-11-06 - 00 - sessions-popup-instant-open-plan (archived at 42087861): /sessions opens at once with a loading row; Store.List reads only the meta header (BenchmarkListLargeRecords)."
---

On a slow box (RPi 4), after typing `/sessions` and pressing enter, the sessions pop-up appears only after a few seconds. Nothing on screen shows that the command was taken.

Cause: the list is read off the Update loop (`listSessions` Cmd), but the pane opens only in `foldSessionList`, once `Store.List` has returned. `Store.List` → `scan` → `loadPath` reads every record file in full and decodes it twice (`decodeAnyRecord`: once into a `map[string]json.RawMessage` probe, once into `Record`), then keeps only `Meta`. On the reporting box that was 84 records, 31 MB, one of them 19 MB. Watched homes repeat the same full read for each home.

## Why

A command that shows no reaction for seconds reads as ignored; the user presses enter again or gives up. A slow machine must make apogee slower, never broken (AGENTS.md, slow-box).

## Acceptance

- The frame after `/sessions` + enter shows the pop-up with a `loading…` row, before the store answers.
- `Store.List` reads only each wrapped record's `meta` header; a benchmark over multi-MB records shows it.
- Empty and error results close the pop-up with today's notes; a reply for a closed or superseded opening changes nothing.
