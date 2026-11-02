---
title: One oversized tool result overflows the context window and abandons the Turn
status: open
type: bug
priority: 2
created: 2026-11-02T17:31:11Z
---

A single large tool result can push the next request past the model's context window. The server answers `HTTP 400 … context_length_exceeded`, and apogee abandons the Turn. In headless mode that ends the run, and everything the run did in that Turn is lost.

Seen in Seqcom run 20261102T1700Z (apogee 0.30.0, headless, orchestrator `mlx-community/Qwen3.8-27B-4bit` on a local server). The run's 22nd Turn called `web_fetch https://openrouter.ai/api/v1/models`, which returns OpenRouter's whole model list as one JSON document. The next request failed with:

    apogee headless: the run's final turn was abandoned — apogee: upstream HTTP 400: {"error":{"message":"prompt exceeds the context window","type":"invalid_request_error","code":"context_length_exceeded"}}

Why nothing stopped it, traced 2026-11-02 at 28fdf0ac:

- `web_fetch` caps a response at `maxNetworkResponseBytes` = 2 MiB (`internal/tools/network.go:44`). That is far above any window a local model has. The terminal tools get 256 KiB (`subprocess.MaxSubprocessOutputBytes`).
- The `tool-result-cap` Floor guard (`floor.CapToolResults`, `internal/floor/resultcap.go`) trims a result to 40% of the working budget. But it protects everything from the most recent tool-call Turn onward (`mostRecentToolCallTurn`), so the result that has just arrived always goes into the next request whole. That is exactly the case that overflows.
- Nothing handles `context_length_exceeded`: no cap, no compaction, and no retry before the Turn is abandoned.

Related: `fetch-ingest-size-blind` (done) shows the ingested size in the TUI but does not bound it. `mid-exchange-auto-compaction` (parked) is the broader budget-pressure work.

## Acceptance

- A fresh tool result that, appended whole, would leave the request over the working window is trimmed with the existing head/tail elision and marker before the request is sent. Results that fit still reach the model whole.
- A test pins this: a stub model with a small window, a tool result larger than the window, and the next request stays under the window and carries the elision marker.
- A `context_length_exceeded` answer from the server is not the end of the Turn while results remain that can still be trimmed: apogee trims and retries once.
