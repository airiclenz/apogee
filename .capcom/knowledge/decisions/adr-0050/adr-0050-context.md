---
title: ADR 0050 context
---

On 2026-08-14 a slow turn on Qwen3.8-27B sat in bare thinking for ~20 minutes. Live
`/apply-template` evidence (2026-08-15, no inference needed) showed why: the family's chat
template reads a `reasoning_effort` kwarg with levels low/medium/xhigh — and **defaults to
xhigh when the kwarg is omitted**, so every ordinary apogee turn runs at maximum effort today.
The same template also honours `enable_thinking: false` (pre-closes the block), older Qwen3
hybrids honour only `enable_thinking`, and an unsupported value raises in Jinja — the server
answers HTTP 500.

Apogee already has the wire plumbing: `ChatTemplateKwargs` on the chat request, emitted only
when `Request.DisableThinking` is set, whose sole setter is the title namer. The stated anchor
in those comments binds this work: **a caller that asks for nothing changes nothing on the
wire.**

A grill on 2026-08-15 settled the shape; this record is that ratification.
