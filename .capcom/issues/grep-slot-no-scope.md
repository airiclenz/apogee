---
title: grep transcript slot shows no scope qualifier for a paths-only call
status: done
type: bug
priority: 4
labels: [plan-2026-09-14-02, run-residual]
aliases: [apogee-1kv]
created: 2026-09-15T11:49:41Z
closed: 2026-09-16T21:02:22Z
reason: "fix(tui): the grep and find_files transcript slots name a paths-scoped call's scope"
---

Deferred from plan 2026-09-14 - 02 item 14. `searchScopeArg` (internal/tui/toolregistry.go:1230) and `grepTarget` (internal/tui/toolregistry.go:1241) read `path` only, so a grep call scoped through the new `paths` argument shows no scope qualifier in the transcript slot (the hit count is still right). Wording gap outside item 14's Files; not a regression.
