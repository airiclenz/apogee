---
title: ADR 0103 consequences
---

- Every Driver writes one more file while a held session runs, and removes it at release.
  `--no-save` writes none.
- A held record opened by `--resume` or the browser's ⏎ is watched instead of refused. Rename,
  delete and `--continue` keep the held-record refusal.
- The interactive TUI takes its hold at the first prompt rather than at the first Save.
- Event lines gain the `prompt` and `sub_agent_group` line kinds and the Tool summary mirror,
  additively within `v:2`. Headless consumers see `sub_agent_group` and the mirror too. `prompt`
  and the `title` and `entries` frame members are written to the journal only.
- The startup sweep now walks the sessions directory on every boot, to remove orphan journals.
- With `sessions.share-group` or `sessions.watch-homes` set, a second local user is inside the
  threat model for those files. `SECURITY.md` currently scopes out a hostile local user, and the
  manual and security note are updated by the plan's last item.
