---
title: "Reaction: the one thing apogee does when the loop passes a Moment"
---

**Reaction**:
One `{id, origin, class, on: [Moments], handler}` — the single thing apogee does when the loop
passes a **Moment**, shipped as `domain.Reaction`. Its **origin** is **engine** (a builtin such as
a Floor guard, or a Go reaction the bench arms in-process through the facade) or
**user**; its **class** is one column of the **Reaction surface** matrix. Its handler is one of
eight kinds behind one sealed interface: six Go func types, each naming exactly one **Moment** —
five per-seam types, one per seam, and `SessionStartedFunc`, the session-start advise slot's, which
`Validate` admits under class advise alone; and two out-of-process kinds a user entry arms —
an `ArgvHandler` (a command) and a `WebhookHandler` (a POST), each serving observe, advise and
gate, the class deciding what the reply is worth — whose `On` list spans Moments and is checked
against the class. `Validate`
refuses an `On` list the handler cannot serve, an origin × class outside the matrix, and a missing
id, origin, class or handler, and is answered ONCE, where the entry is built (the config layer's
mapping for a `reactions:` file, the agent's arming step for `Config.Reactions`) — `Generation.Validate`
and the Reaction Runner take an entry as validated; a duplicate id is refused one level up —
`Generation.Validate` per lane (the same id may appear in both the observe and the sync lane) — and
an id a builtin or an engine notice already holds is refused at the **arming seam**, the one place
for both routes: `armReactions` for `Config.Reactions` and `Agent.SetReactions` for the live sync
lane, which validates the Generation it is handed and installs nothing on a refusal. `TopLevelOnly` opts a reaction
**out** of sub-agent inheritance — the zero value is inherited by every child agent; `Timeout` is
the deadline an out-of-process handler runs under on either lane (observe 30s, advise 10s, gate 5s
by default; an entry's `timeout:` binds every reaction it arms) and is ignored by a per-seam Go one,
which runs without a deadline exactly as a Floor guard does; a `SessionStartedFunc` honours it (else
the advise default), since it runs in the background from the **Session boundary** and the session's
next Exchange opening waits on it for no longer — that join fences its answer ahead of the user's
text and books it as an `advise` firing.
The engine fires them with one `fire(ctx, moment, payload)` per seam — builtins first, then
whatever is armed beside them, each under a recover boundary, an error ending the cascade at once
and a panic reported and stepped over. Each returns one **`Outcome`**:
`{Retry, Inject, Defer, Edited, Gate, Detail}`, whose zero value means "did nothing" and is not
booked. Anything else is a firing, and it reaches every Driver as one `ReactionFiredEvent` keyed by
id, under the action `retry`, else `defer`, else `intercept` when a shape reaction moved the working
value's revision, or `fired` when an armed reaction acted at pre-request, pre-tool-exec or
history-rewrite; two Floor guards book their own labels (`cap`, `salvage`) and the context-fill
notice books `notice`; the sync lane books `advise` for a landed trailer, a gate's own verdict
(`deny`, `ask`, `allow`) and `failed` when a command could not run. The seven Floor-guard booleans stay the
canonical switches for the builtins. An **observe** firing stands outside this cascade: the async
lane's Runner (`internal/reactions`) matches it off the Event the engine already emitted and
delivers it on the reaction's own worker, so it never books a `ReactionFiredEvent` — only advise
and gate firings (and the engine's own) do — and its handler's failure reaches the Driver by the
Runner's report line alone. Whichever lane fires it, an out-of-process handler reads the **one**
payload document, `domain.SeamPayload`: the identity block, the per-Moment members its Moment
carries, and — on the sync lane alone — the call's `arguments` and `result`.
A **user**-origin Reaction is one entry of the global `reactions:` list, or an **Adopted** entry of a
**Project config**'s (`domain.Reaction.Project` set; fenced, scrubbed and run in the Project root,
its pinned project files re-checked before every spawn — ADR 0104) —
`{id, on: [moments], run: <argv | {url, headers, headers-env}>, advise: <argv | {url, …}>, gate: <argv | {url, …}>, workspace?, timeout?, enabled?}` —
resolved into one `domain.Reaction` per action key it spells, sharing its id: `run:` at **observe**
class, fired by the async lane (`internal/reactions`' own runner) on notices; `advise:` at
**advise** class, fired by the agent's sync lane at `post-tool-result` or `file-changed` (the latter
narrowing it to a successful write-tool call), its stdout — or its webhook's reply body —
secret-redacted (`tools.RedactSecrets`), capped and fenced as the trailer — one **Advice span**; and
`gate:` at **gate** class, fired by the sync lane at `pre-tool-exec` only, as the **Gate stage**,
its stdout's or reply body's first line the verdict. Every action key takes the same two shapes —
an argv list or a webhook mapping — through one loader path (`handlerFor`); a bare string or a
mapping key outside `{url, headers, headers-env}` is refused with a sentence naming the key
(`advise: is an argv list or a webhook mapping {url:, headers:, headers-env:}`), as is an `on:` a
key's class cannot take (a seam under `run:`, a notice under `gate:`, any Moment but the two under
`advise:`), and
`enabled: false` **parks** an entry: it stays in the file
and is dropped at resolve, so nothing arms it. The whole live shape swaps as one
**`Generation`** — `{Floor, Bypass, Observe, Sync, ContextFillNotice}` — which a **Driver** hands
to the agent in one `SetReactions` call. `Agent.SetReactions` swaps both halves itself: it
validates the whole Generation (a refused one moves neither half), installs the engine half
(Floor, Bypass, Sync and the **Context-fill notice** switch), and then,
when a runner is configured (`Config.ObserveRunner`) and the observe list moved, hands the runner
its half (Observe) — so nothing downstream reads a half-swapped state. It is the single idiom that
replaced `SetBypass` and `SetFloor`; the runner keeps `Replace` as the observe half's swap
primitive, called only from `Agent.SetReactions`
(ADR 0076 D4, A8). The
key's earlier name was `hooks:`, and a file still carrying it is **folded** into `reactions:` once,
at start-up: backed up first, re-rendered from its parsed entries, and reported in a note naming
every rename it made.
_Avoid_: "Hook" (the colloquial alias — see Hook), "Mechanism" (retired),
"plugin" (no Reaction adds a capability).
