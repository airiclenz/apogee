---
title: "Second wire dialect: Anthropic Messages behind a per-server dialect: key"
status: done
type: feature
priority: 2
aliases: [apogee-6fp]
created: 2026-09-07T10:48:29Z
closed: 2026-09-16T19:09:12Z
reason: "test(cmd/apogee): a full tool-use loop completes over the anthropic wire"
---

Gap 4 of the 2026-09-06 contender gap assessment, ACCEPTED by the owner 2026-09-07 as wave 2 (after gap 3).

Today the wire is OpenAI chat-completions only (internal/provider/wire.go). Rivals: pi speaks 4 wire formats; Crush and goose ship typed local providers.

Fit: add the Anthropic Messages dialect (Ollama has spoken it since Jan 2026) selected by a per-server 'dialect:' config key. Engine-side and wire-silent, so it does not touch the model-facing surface and needs no bench gate. Effort M.

## Acceptance

A server configured with dialect: anthropic completes a full tool-use loop against a Messages-speaking endpoint; the default dialect stays OpenAI chat-completions; the engine remains wire-silent (ADR 0031).

## Log

- 2026-09-15 beads: triage of docs/reviews/architecture-review-2026-09-14.html candidate 8 + in-passing defect: 'effort dialect stays inside the Client' was DENIED — ADR 0060 D9 ratifies the rebind-spec → Agent field → provider.Request channel, and detection happens in the Monitor's Client (heartbeat.go), never the engine's completion Client, so 'latch on Discover' cannot reach it. The routed-child fallback (subagent.go: a target naming no dialect inherits the parent's) is the ratified 2026-08-31 - 02 call; the per-server dialect: key this bead adds is the sanctioned fix. Optional small win noted: Client.CanAskForNoReasoning() would retire the dialect inspection in compact.go and cmd/apogee/title.go.
