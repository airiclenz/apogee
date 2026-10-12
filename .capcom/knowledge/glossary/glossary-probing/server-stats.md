---
title: Server stats
---

**Server stats**:
The **cross-session** record of Upstream attempts per server,
kept in `~/.apogee/server-stats.jsonl` — one appended line per attempt, keyed by server name plus
the endpoint redacted to scheme + host + path, each line naming the model it served; no prompt
text, body or key name is ever stored, and a file past four times its cap is trimmed at startup to
the last 50 samples per (name, endpoint, model). Its **summary** — ttft, tok/s (reported output
tokens ÷ (`last` − `ttft`), never estimated) and the failure count, cancelled attempts excluded —
is filtered to the model bound on that entry and shown on the `/server` and `/sub-agents-server`
picker rows (`· no data` under five samples); `/inspect` lists the attempts themselves. The root
`server-stats: off` stops the writes and the summary; the events still fire
(ADR 0085).
_Avoid_: "cost" or "context cost" (that is ADR 0079's
byte-denominated report of what apogee injects — server stats are wall-clock timings and
outcomes, and neither is money), "benchmark" (it observes ordinary traffic, it scores nothing),
"health" (the Heartbeat observes reachability; this observes
speed and failures of real requests).
