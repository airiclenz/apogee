---
title: "Apogee (the coding agent): the terminal agent that owns the agentic coding loop end to end"
---

**Apogee** (the coding agent):
The terminal-based agent that owns the agentic coding loop end-to-end for a small local
LLM: it builds each request, calls the Upstream, parses the response, dispatches tools,
and fires the **Reactions** armed at each **Moment** it passes — all in one cross-platform Go
binary. It is no longer a layer between a coding tool and a model; it is the coding tool.
_Avoid_: "the proxy", "Apogee Core", "the extension", "middleware" (all describe the
retired predecessor structure).
