---
title: git_show renders through the raw-name fallback
status: done
type: bug
priority: 3
plan: 2026-09-24 - 00 - review-findings-follow-through-plan
aliases: [apogee-git-show-no-presenter]
created: 2026-09-24T09:16:51Z
closed: 2026-09-24T12:47:47Z
reason: "Resolved by review-findings-follow-through-plan (30ad3bbc) item 1: git_show has its own tool card and registry coverage is pinned to tools.KnownToolNames"
---

internal/tui/toolregistry.go has no row for git_show (34 tools, 33 rows); the card shows the raw name, labelled args and an empty stat slot. The registry comment claiming full built-in coverage is false. Nothing pins registry coverage against tools.KnownToolNames().
