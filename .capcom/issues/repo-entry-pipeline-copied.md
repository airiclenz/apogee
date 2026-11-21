---
title: "Repo-entry pipeline with per-kind descriptors (architecture review 2026-10-27 #9)"
status: dropped
type: task
priority: 3
created: 2026-11-05T19:56:21Z
closed: 2026-11-21T16:14:32Z
reason: "Not worth the churn: review said wait for a third kind, none proposed; security path. Triage 2026-11-21."
---

Found by docs/reviews/archived/architecture-review-2026-10-27.html (#9), re-verified 2026-11-05 at f7ec72b6. internal/config/execlayer.go still holds two hand-written copies of one pipeline for repo-declared entries, one for MCP servers and one for reactions: repoMCPServers / repoReactionConfigs, checkRepoServer / checkRepoReactionID, pinRepoServers / pinRepoReactions, resolveRepoServers / resolveRepoReactions and globalMCPServers / globalReactions, with the pinned field list kept by hand (pinFieldOrder in execpin.go, guarded by a reflection drift test). Every security rule in it is written, and tested, twice. The review proposes one generic pipeline (decode, unique handle, global wins, pin, classify, arm when adopted) with each kind supplying a descriptor (block name, handle field, noun, pinned fields, extra checks), with Allow rules as an implicit third kind; the test surface (LoadLayeredConfig) stays. It is a security path: the review advises doing it when a third kind is coming. No ADR conflict.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.

## Log

- 2026-11-21 Airic Lenz: Triage 2026-11-21 at 7869cca7 (read-only sub-agent check, not re-verified): DOUBTFUL — the review itself said wait for a third kind; none proposed; security path (execlayer.go:167-469)
