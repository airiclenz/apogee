---
title: A tool call's undeclared arguments are silently dropped
status: done
type: task
priority: 3
labels: [tool-surface]
plan: 2026-11-22 - 00 - undeclared-args-note-plan
created: 2026-11-21T12:44:48Z
closed: 2026-11-22T18:24:34Z
reason: "Resolved by plan 2026-11-22 - 00 - undeclared-args-note-plan (archived at 3d4011eb): built-in tool results now name ignored argument keys and the declared ones; declared-only calls unchanged; covered by TestNoteIgnoredArguments."
---

Found by docs/reviews/archived/session-mining-2026-09-14.md (§2); re-verified 2026-11-21 at 43ece2f2.

Every native tool decodes its arguments through `decodeArgs` (internal/tools/tools.go:88-93), a plain `json.Unmarshal`, so a key the schema does not declare is dropped without a word. The tool runs with its default, the result reads as success, and the model never learns its argument did nothing. The repair Floor guard (internal/floor/repair.go:11-17) checks unknown tool names, malformed or empty JSON and missing required parameters, not undeclared ones; internal/tools/grep.go:57-61 already names the behaviour ("decodeArgs drops unknown fields"). Session 6e088546 [1460] passed an invented `line:` to `read_file` 31 times and every call came back as an unranged read. Plan 2026-09-14 - 02 item 6 fixed the other `read_file` traps but did not cover or exclude this one.

Direction: after a successful decode, compare the raw keys with the tool schema's `properties` and append one result line naming the ignored keys and the declared ones (e.g. `note: ignored unknown argument "line" — read_file takes path, start_line, end_line, max_lines, locate`). Do not refuse or retry: under ADR 0071 Decision 1(c) a Floor guard must not regress the bare loop, and a refusal would cost a Turn on harmless extras some models emit. Leave MCP tools alone (their servers validate their own schemas). Record the change in docs/design/tool-surface-findings.md. This is model-facing text, so decide during design whether it needs bench evidence first.

## Acceptance

- A native tool call carrying an undeclared key still runs, and its result names the ignored key(s) and the declared ones.
- A call with only declared keys returns a byte-identical result to today.
- A table test in internal/tools covers both cases.
