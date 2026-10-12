---
title: Give the five seam-closing notice Moments headless Event line kinds
status: done
type: task
priority: 4
plan: 2026-09-13 - 00 - headless-ci-hygiene-plan
aliases: [apogee-5cf]
created: 2026-09-08T06:10:23Z
closed: 2026-09-14T05:48:25Z
reason: delivered by plan 2026-09-13 - 00 items 2-3, commits 2e43da34 (eventjson seam_closed kind behind Options.Seams) and 90f0bdcb (headless --format json --seams); CHANGELOG entries landed in f7d5f44b
---

ADR 0076 amendment A5 ships pre-request-finished, post-response-finished, pre-tool-exec-finished, post-tool-result-finished and history-rewrite-finished as SINK-ONLY notices: eventjson.Encode returns ok=false for them, so headless --json is unchanged. Reason: pre-request/post-response fire per streamed Turn and the two tool seams per tool call, so five new line kinds would roughly double a typical stream's volume for consumers who never asked. ADR 0075 D10 makes adding them later purely additive (new kinds are additive within v). Open until a headless consumer asks for the seam ladder.
