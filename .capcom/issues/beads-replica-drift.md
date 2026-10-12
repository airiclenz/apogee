---
title: Beads replicas drift apart because nothing pulls the Dolt remote
status: done
type: task
priority: 2
aliases: [apogee-beads-replica-drift]
created: 2026-10-07T08:37:07Z
closed: 2026-10-07T08:37:09Z
reason: "Sync automation in place: bd-sync-soft from post-merge, pre-push and SessionStart; dolt.auto-push on; verified hooks sync and stay exit 0 on a halted sync."
---

bd has no auto-pull and no hook pulls or pushes Dolt (dolt.auto-push was unset), so a replica that only git-pulls receives another machine's issues.jsonl while its database stays behind; the next write collides with the remote. Seen 2026-10-07: bd dolt pull refused on 254 conflicted rows, 7 remote-only issues. Fix: .beads/hooks/bd-sync-soft runs bd sync after git pull (post-merge), before git push (pre-push) and at Claude Code SessionStart; dolt.auto-push true in .beads/config.yaml; hooks pinned LF; rule in AGENTS.md.
