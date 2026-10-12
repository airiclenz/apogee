---
title: "The loop (the agent loop): apogee's core build, call, parse and dispatch control flow"
---

**The loop** (the agent loop):
Apogee's core control flow: build request → call Upstream → parse response → dispatch
tools → repeat, emitting typed events at each step. The loop owns tool execution and
conversation state — which is precisely what lets a behaviour that once had to live outside the
loop (a tool-result correction, say) be a first-class seam Reaction. Lives in `internal/agent/loop.go`.
_Avoid_: "the pipeline" (that was the proxy-era Transform chain — a narrower thing).
