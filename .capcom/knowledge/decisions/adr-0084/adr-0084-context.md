---
title: ADR 0084 context
---

The Budget split the working room — the working ceiling
less the reply reserve — into three fixed shares: 15% for the system-prompt part, 25% for the file
context, and whatever remained (48% of the window at the default 20% reserve) for History. The two
standing shares were reserved whether or not that content existed, and `Budget.FileContext` had no
reader at all: nothing in the engine ever consumed the 25% it held back. A session with no
workspace context file and a short prompt therefore gave a quarter of its working room to nobody,
and paid for it in History.

The reducers read History, so the cost landed on them. Pruning fires above a fraction of the
History allocation, and with History at 48% of the window the 60% band put the first prune pass at
roughly 29% of the context window. In live session `20260921T173133Z-718e0f6c` (160k window,
`ctxUsed` 55,885) that produced three prune passes — 18:04, 18:32, 19:01 — stubbing 14 tool results
while the window was barely a third full, and the model re-read the same files three and four
times to recover what had been stubbed. Four protected Turns is a narrow window for a model that
reads files in 400-line chunks: the chunks of one file can fall outside it while the read is still
in progress.

Two further facts shaped the fix. The oversize warning for Context files
compares the standing content against `Budget.SystemPrompt` (ADR 0026 §8) — under a measured
reservation that comparison becomes a tautology, since the reservation *is* the measurement plus
headroom and can never be exceeded. And ADR 0018 §8's structural floor sits below the emergency
fold's transcript budget only while `0.6 × (window − reserve) < window − 4608` — above a ~8.9k
crossover. A History that grows when the standing content is small would push that crossover
upward if nothing bounded it.
