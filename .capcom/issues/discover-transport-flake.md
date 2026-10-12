---
title: "internal/provider: TestDiscoverTransportFailureIsLabelled/rate_limited flakes under make check's parallel shards"
status: done
type: bug
priority: 3
aliases: [apogee-3h4]
created: 2026-09-07T17:22:50Z
closed: 2026-09-11T10:28:51Z
reason: "fixed in plan 2026-09-11 - 00 item 4: discovery deadline injectable, test no longer races the 5s constant; cause inferred from the only failure path, not observed"
---

Seen once on 2026-09-07 during a make check run: the rate_limited subtest of TestDiscoverTransportFailureIsLabelled FAILed inside the sharded run, then passed standalone with -count=20 under -race. Unrelated to the Event-lines work (internal/provider untouched). Likely a timing or shared-fixture sensitivity that only appears under the parallel shard load scripts/test-shards.sh creates.
