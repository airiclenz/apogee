---
title: "One advise call for every advise Moment (architecture review 2026-10-27 #8)"
status: parked
type: task
priority: 3
created: 2026-11-05T19:56:21Z
---

Found by docs/reviews/archived/architecture-review-2026-10-27.html (#8), re-verified 2026-11-05 at f7ec72b6. An advise reaction is still invoked by two copies that disagree: adviseUser in internal/agent/reactions.go (the post-tool-result cascade) reports a handler error inline and returns nil, and drops the handler's Detail, while callSessionAdvice in sessionadvise.go (the session-started and compaction-finished lanes) returns the error to be reported at join and keeps Detail; each switches over the handler kinds (runSyncArgv, runSyncWebhook, runSyncMCP) and redacts on its own, attachSessionAdvice calls bookFiring with dummy out-params, and isAdvice hard-codes the three advise Moments. The review proposes one call that answers "run this advise reaction against this document" as a value (a capped, redacted span or a failure to report, emitting nothing), plus a small landing step that fences it onto the target message and books the Firing, so ADR 0076 D7's fail-open, cap and redaction live in one place and a new advise Moment touches one. No ADR conflict; the review rates it the cheapest standalone warm-up of its set.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
