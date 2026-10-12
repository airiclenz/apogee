---
title: Headless text output is rendered live from the Event stream
status: done
type: feature
priority: 3
deps: [headless-json-output]
plan: 2026-09-13 - 00 - headless-ci-hygiene-plan
aliases: [apogee-czf]
created: 2026-09-07T12:08:06Z
closed: 2026-09-14T05:48:25Z
reason: delivered by plan 2026-09-13 - 00 item 1, commit c386b6b9 (headless --format text narrates tool calls, results and sub-agent phases live on stderr); CHANGELOG entry landed in f7d5f44b
---

Follow-up from the apogee-tvw grill (2026-09-07), owner-approved at grill Q4.

Today `apogee headless` prints NOTHING until the run completes: the answer is one Fprintln of run.Result.FinalText (cmd/apogee/headless.go:642) and the surrounding block is composed post-hoc from the same run.Result (headless.go:635-677). A ten-minute run looks hung.

Once apogee-tvw lands, `--format text` and `--format json` could become two renderers over one Event stream, giving headless live output — the opencode shape.

Deliberately NOT taken into apogee-tvw: its acceptance criterion is 'the human-readable default output is unchanged', and a live renderer cannot reproduce a post-hoc summary that prints run.Result-only facts (Wrote, UndoNote, ContextFiles, per-sub-agent fills) in summary order without buffering to the end. It is cheaper to build AFTER the Event stream exists.

## Acceptance

apogee headless --format text emits output as the run progresses rather than only at completion; the facts that exist only on run.Result still appear, in their current wording, at the end.

## Log

- 2026-09-07 beads: Blocked on apogee-tvw (the Event stream). See ADR 0075 decision 6 and its Rejected section.
- 2026-09-13 beads: premise superseded by plan 2026-09-13 - 00 item 1 — 'prints NOTHING until the run completes' no longer holds: under --format text, cmd/apogee's narrationSink (the generalised pruneNoticeSink) prints → <tool> <summary> / ← <tool> ok|error: <first line> at Depth 0 and sub-agent <name>: started|finished|cancelled live on stderr as each Event arrives; stdout stays the answer alone at the end and the post-hoc block keeps its wording and order (the acceptance criterion as written). Suppressed under --format json like the prune notice (ADR 0075 D6).
