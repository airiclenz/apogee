---
title: "ADR 0018 amendment of 2026-11-02 (second): one request room, the bound weighs the request sent, and trailers count inside it"
---

Decided by the owner on 2026-11-02, from the architecture review of that day (card #3, the request
room) and the issues `fresh-result-pins-blunt` and `tool-result-trailers-overflow`; shipped by plan
`docs/plans/2026-11-02 - 05 - request-room-and-delegate-bounds-plan.md` (items 2–4 and 8).
`adr-0018-amend-1102` stands: the bound is still a fresh result's share of the room left, clamped
to [1/8 of the History allocation, the History allocation], with the History allocation alone when
no window is known.

**1. One request-room module owns the measure, the share and the clamp.** The arithmetic
decisions 7 and 8 and `adr-0018-amend-1102` rest on lives in one unexported value,
`requestRoom` (`internal/agent/requestroom.go`): the request's measure, the room left after it, a
fresh result's share of that room, the predictive guard's window check and the elision both
structural bounds render through. The seams read it and keep no copy of it. A room is built once
per tool-result commit from one Budget view and one render of the standing table, and handed down
as a value for that commit only (ADR 0084 D1's append-commit note), so the clamp, the fill notice
that predicts it and the event estimates read the same numbers. Out of the module, on purpose:
the fold transcript's `elideNewestMessage`, the delegate `ElideMiddle` cap and the
`tool-result-cap` Floor guard (`CapToolResults`). Each keeps its own seam, and the guard keeps its
apogee-sim parity (ADR 0071 Decision 1, clause (c)).

**2. The bound weighs what `buildRequest` sends.** The estimate of the next request without the
result is no longer the standing content, the conversation and the tool menu alone. It is built
from `buildRequest`'s own two halves (`requestContents`), so it carries what that request will:
the delegations ledger note, a pending wrap-up directive and every deferred injection a
post-tool-result Reaction left for the next request. A note the bound did not weigh can no longer
land the request past the room it measured.

**3. A result's trailers count inside its bound.** The advice spans the post-tool-result cascade
fills and the delegate's budget notices (step, token and time) are collected before the clamp and
their weight is taken off the result's share (`requestRoom.trailedShare`): the body is clamped
shorter by that weight, so the committed message, trailers included, fits the share. They used to
be appended after the clamp uncounted, which landed the next request at the room plus the
trailers and folded it before the model read the result. The trailers are still rendered after
the clamp, so the elision never cuts one. The share loses their weight only above the result
floor: a result is never clamped below 1/8 of the History allocation, where the predictive guard
folds instead. The wrap-up directive is not a trailer: it is decided after the Turn and stamped on
the request tail at send time, and point 2 weighs it while it is pending.

**4. @file and skill references keep their History share.** A reference block is still bounded by
its share of the whole History allocation, split across the references its message submitted
(`refBound`, capped at `fileRefMaxTokens`), not by a share of the room left. The asymmetry with a
tool result is kept on purpose: references are resolved once per user message, outside any
tool-result commit, and the predictive guard (decision 7) folds before that request is sent when
they crowd it. Bringing them under the room left would add a second commit path to the module
with no failure behind it.
