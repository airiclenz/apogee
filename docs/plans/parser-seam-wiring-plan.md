# Plan — Cross the parser seam: wire the dormant processing/ module into the loop

**Date:** 2026-07-02
**Status:** READY — design settled in a `grill-with-docs` session on 2026-07-02 (all D-items below
are resolved; **no needs-design-call escalation should be needed**). Items are ordered; work them
in sequence, one commit per item.
**Source:** architecture-review candidate **#2** (`docs/architecture-review-20260629-110828.html`),
the natural second move after candidate #1 (Resolution refactor) shipped. Also the deferred
Phase-3 residual **P3.5** (handoff `docs/handoffs/2026-06-25 - 00 - …`): *"the loop adapt-seam
hard-codes the native tool-calling path; the fenced/regex processor factory is built but unwired
(needs a model-profile / `ToolCallingConfig` + `ThinkingConfig` source that doesn't exist in
domain/config yet)."*
**Track:** post-`v1.0.0` **feature-parity** — additive (a new domain type + a `Config` field, an
additive minor per ADR 0001 / decision #18). Not a Phase-4 item. **Native profiles stay
byte-identical** at every step (the D8-style discipline carried from the Resolution plan).
**Docs already updated in the design session:** `CONTEXT.md` gained the **Model profile** and
**Thinking channel** glossary terms. **No ADR** (grill decision D4 — the rationale lives in this
Design record + those two `CONTEXT.md` terms, which cite [ADR 0010](../adr/0010-package-layout-domain-core-and-thin-root-facade.md)).
**Standing requirement:** `/coding-standards` (Go + testing variants) is mandatory for all new
code — invoke `implement-plan` with `coding-standards` forwarded. Apogee is pre-production:
commit direct to `main`, no PRs. `go test ./...`, `go test -race ./...`, `gofmt`, `go vet` green
are the gate on every item.

---

## The problem (grounded in the current code)

`internal/processing/` is the riskiest port (the TS parse layer) and it is **fully built and
oracle-tested** — `NewToolCallParser` (the factory), `MarkdownFencedParser`, `CustomRegexParser`,
`StripThinking`, `StripHarmony`, and the streaming guards `IsThinking` / `IsHarmonyThinking` — but
it has **zero production callers** outside `ParseNativeToolCalls`:

- `internal/agent/loop.go` parses **native tool calls only** (`parseToolCalls` →
  `processing.ParseNativeToolCalls` over the provider's out-of-band `tool_calls`).
- `streamResponse` emits a `TokenEvent` per `delta.Content` chunk **as it arrives**, and
  `respondAndReview` builds `domain.NewResponse(reply.content, reply.thinking, …)` from the **raw**
  accumulated content. Reasoning is only separated when the *Upstream* splits it into
  `DeltaThinking` (the `reasoning_content` field).
- There is **no config source** to select a non-native format or declare inline thinking
  delimiters — `domain.Config` has no such field.

**Consequence (the review's finding):** a small local model that emits a **markdown-fenced /
custom-regex tool call** in its visible content gets it **silently ignored**, and a model that
**inlines `<think>…</think>` or gpt-oss harmony channels** into content **leaks that markup into
visible text** (committed to history and shown to the user). Every shipped profile today is native,
so this is latent, not active — but it caps small-model coverage at native-only.

**The fix is a wire, not a redesign:** give `domain.Config` a **Model profile**, then at the loop's
parse seam strip the thinking/harmony channels and select the tool-call parser from that profile.

## Dependency constraint (the reason D1 was the load-bearing call)

`internal/processing` **imports** `internal/domain` (ADR 0010 — `domain` is the bottom of the
DAG). So `domain.Config` **cannot** hold `processing.ToolCallingConfig` directly (import cycle).
The grill resolved how the profile crosses that boundary (D1–D3 below).

---

## Design record (the grilled decisions — do not re-derive)

- **D1 — The Model profile is declarative `domain` data, translated to the `processing` parsers at
  the boundary.** A new `domain.ModelProfile` on `Config` (host- and embedder-settable). This is
  ADR 0010's rule applied literally — *"`internal/domain` is the ubiquitous language: every type…
  in the public surface; adding a domain term is adding a `domain` type and one root alias"* — and
  it reuses the ADR's **provider-seam precedent verbatim** (*"the wire types stay provider-local…
  the loop translates `domain` ↔ wire shape at that boundary"*). Profile-as-**data** (not behaviour)
  snapshots cleanly and is the natural seed for the deferred **[P1] switchable model-profile** and
  **`apogee probe` auto-profiling** work (`TODO.md`).
  - *Rejected — B: promote `processing`'s config types into `domain`.* `MarkdownFencedConfig` /
    `CustomRegexConfig` carry `withDefaults()` **methods**, which can't move packages — so B forces
    rewriting the riskiest oracle-tested port's internals (and mechanically editing its parity
    tests) for zero behavioural gain. **This is the "why not just move the config up?" a future
    reader will ask — the answer is the `withDefaults()` methods + freezing the oracle port.**
  - *Rejected — C: inject the parser + a content-stripper as `domain` interfaces the host builds*
    (à la `Config.Skills` / `Confiner` / `ExternalEffects`). Coherent, but models the profile as
    *behaviour the host constructs* rather than *data the engine owns* — worse for snapshot/resume
    and for the later `/server`-switch + `apogee probe` work, both of which want profile-as-data.
- **D2 — The translation lives in `processing`, not the agent.** A new entry point
  `processing.ParserFor(p domain.ModelProfile) (ToolCallParser, ContentStripper, error)` maps the
  domain profile onto `processing`'s **existing, frozen** internal `ToolCallingConfig` /
  `ThinkingConfig` and calls the existing `NewToolCallParser`. `internal/agent` calls it once in
  `newAgent` and stays a thin caller — the format→parser knowledge stays in the package that owns
  formats, and the frozen oracle config types **never surface in `internal/agent`** (no mirror of
  them there). A bad profile (unknown format / invalid regex the factory rejects) fails construction
  loudly, surfaced through the normal construction-error path — never a silent native fallback.
- **D3 — `ParserFor` returns a unified `ContentStripper`; the agent never branches on
  harmony-vs-delimited.** `ContentStripper` is a small `processing` interface:
  `Strip(raw) (visible, reasoning string)` + `IsMidChannel(raw) bool` (the streaming guard item 3
  needs). It encapsulates the three thinking styles (`none` → no-op; `delimited` → `StripThinking`;
  `harmony` → `StripHarmony`). The loop calls one method uniformly; a native/none profile gets a
  no-op stripper and a no-op parser, so the content path is **byte-identical**.
  **Harmony's third stream:** `HarmonyStripped` carries *three* channels (Visible, Reasoning,
  Commentary) but `Strip` returns two — the harmony stripper **folds Commentary into the reasoning
  return** (blank-line join, Reasoning first). Both are the model's private channel and the
  `CONTEXT.md` **Thinking channel** term says inline channels are *preserved as reasoning in
  history*; silently dropping the tool-planning text would lose it.
- **D4 — `ModelProfile` shape: two orthogonal axes** (see the `CONTEXT.md` **Model profile** term).
  Confirm the exact exposed knob set in item 1 against the apogee-code oracle **source** —
  `ToolCallingConfig`/`ThinkingConfig` in `src/types.ts` (~lines 172–185) plus
  `src/processing/processor-factory.ts` (the oracle repo lives at
  `~/Repos/Airic/apogee-code/`; do **not** read `media/chat.js`, a minified bundle). The TS
  `ThinkingConfig`'s `displayInChat` / `enableCommand` / `disableCommand` knobs are host-UI
  concerns, not part of the domain profile. Proposed:
  - **tool-call axis** — a `ToolCallFormat` enum mirroring `processing`'s (`""`|`native`|
    `markdown-fenced`|`custom-regex`; `""`⇒native), plus the custom-regex `Pattern` (mandatory for
    that format) and optional regex/fenced override knobs — **empty ⇒ the parser's `withDefaults()`**,
    so a fenced model usually needs only the format and a regex model only a `Pattern`. (Fenced
    marker overrides may be deferred as an additive later field if no shipped model needs them.)
  - **thinking axis** — a `Thinking` sub-struct: a style (`""`/`none` · `delimited` with
    `Start`/`End` tokens · `harmony`). `harmony` needs no tokens; `none` (the default) leaves
    today's Upstream-split `DeltaThinking` path untouched.
  - A **zero `ModelProfile` == native, no inline thinking == today's exact behaviour** (the
    byte-identical anchor).
- **D5 — Tool-call precedence at the seam.** The out-of-band **native** path
  (`ParseNativeToolCalls` over `reply.toolCalls`) **always runs and wins** when it produced calls —
  it is the structured channel and costs nothing (and is how a **harmony** model's calls arrive —
  the Upstream parses harmony server-side; there is **no** harmony tool-call *text* parser, see
  "Explicitly NOT"). The **text** parser runs over the *stripped visible content* and supplies calls
  only when native produced none. Native profile ⇒ text parser is the no-op ⇒ byte-identical.
- **D6 — Strip order + thinking preservation at the seam.** On the accumulated content:
  **(1)** `visible, reasoning := stripper.Strip(content)`; **(2)** run the text tool-call parser
  over `visible` (`ParseToolCall` + `StripToolCall`, so the call's markup leaves the committed
  assistant text). `NewResponse(visible', combinedThinking, mergedCalls, …)` where
  `combinedThinking = join(reply.thinking, reasoning)` — so a model's reasoning is preserved as
  `reasoning_content` in history via `assistantMessage`, exactly as today for the Upstream-split case.
  - **ID for a text-parsed call:** a **deterministic loop-assigned ID derived from the Turn**
    (e.g. `text_call_<turn>` — `ParseToolCall` yields at most one call per reply, so the Turn
    number disambiguates). NOT the oracle's `` tc_${Date.now()} `` timestamp: tests and
    snapshot/resume must stay deterministic.
  - **History representation — a recorded, deliberate divergence from the oracle.** The merged
    call is stored **structurally** on the assistant message (`assistantMessage` sets `ToolCalls`,
    so the next request echoes it upstream as native-shaped `tool_calls` via
    `toProviderToolCalls`). The TS oracle instead commits the stripped assistant text with **no**
    `toolCalls` array (only a tool-role result message — `orchestrator.ts:436`), so a fenced model
    never sees a native-shaped call in its context. We diverge on purpose: structural storage
    keeps **one** dispatch/event/snapshot path for every format — dispatch links the tool-role
    result via `ToolCallID`, and snapshot/resume round-trips `Message.ToolCalls`
    (`domain/hooks.go:45`) — and chat templates tolerate native-shaped history far better than
    the loop tolerates two history shapes. If a shipped fenced model chokes on echoed native
    calls, revisit as a follow-up — do not silently re-derive.
- **D7 — Malformed degrades, never fails (the P1.3 contract, already the package's law).** A
  no-match text parse ⇒ no call (a plain turn); a malformed payload ⇒ no call, never a panic, never
  a Turn failure — matching the existing `parseToolCalls` error handling (surface an `ErrorEvent`,
  treat as final no-tool response).
- **D8 — Public surface + docs.** `domain.ModelProfile` (+ its enum/sub-types) are new **public**
  domain types → re-export from the root facade (`apogee.go`, mirroring
  `type ContextConfig = domain.ContextConfig`). Additive minor (decision #18). Update
  `internal/processing/doc.go` (drop the "0 prod callers" framing once wired) and the processing
  section of `docs/design/technical-design.md`; add a CHANGELOG entry under Unreleased.

Reference pointers (read before implementing): `internal/agent/loop.go`
(`streamResponse` / `respondAndReview` / `parseToolCalls` / `assistantMessage`),
`internal/processing/{factory,parser,thinking,harmony,markdown_fenced,custom_regex}.go`,
`internal/domain/config.go` (the `Config` + `ContextConfig` shape to mirror),
`cmd/apogee/config.go` (`fileConfig`→`layer`→`settings`→`opts`) + `cmd/apogee/wire.go`
(`opts`→`apogee.Config`), `apogee.go` (alias facade) + `example_test.go` (the completeness
guard), `CONTEXT.md` → **Model profile** / **Thinking channel**, and the apogee-code oracle
**source** at `~/Repos/Airic/apogee-code/` — `src/types.ts` (`ToolCallingConfig`/`ThinkingConfig`),
`src/processing/processor-factory.ts`, `src/orchestrator/orchestrator.ts` (the TS parse seam,
~line 430). The oracle repo's location is documented nowhere in this repo — this plan is the
pointer. `media/chat.js` is a minified build artifact; never read it as the oracle.

---

## 1. Add `domain.ModelProfile`, re-export it, and plumb it from config.yaml (no consumer yet) — ✅ DONE (2026-07-02)

**What:** land the config surface end-to-end with **no** loop consumer, so the public-surface /
semver addition is one small reviewable commit and behaviour is provably unchanged.

**Domain (`internal/domain/config.go`):** add `ModelProfile` per D4 (the two orthogonal axes: a
`ToolCallFormat` enum + the custom-regex `Pattern`/optional knobs, and a `Thinking` sub-struct with
style + `Start`/`End`), plus a `Profile ModelProfile` field on `Config`, documented like the other
seams and cross-referencing the `CONTEXT.md` **Model profile** term. A zero value == native, no
inline thinking. Name the enum constants to match `processing`'s so D2's `ParserFor` is a straight
map. **Confirm the exposed knob set against the oracle before finalizing the fields.**

**Root facade (`apogee.go`):** re-export the new public types — `type ModelProfile =
domain.ModelProfile`, the enum type + its consts, and any sub-structs — in the existing alias style
(next to `ContextConfig`). **Also add every new name to the `example_test.go` completeness guard**
(package `apogee_test`): the guard is a hand-maintained surface list that fails the build only when
a *listed* alias is dropped — it does **not** detect a new type that was never listed, so the new
names are protected only once they appear there.

**Host plumbing (`cmd/apogee/config.go` + `wire.go`):** model the profile as a **file-only**
setting (a per-model concern, like `mcp-servers` / `web-search-endpoint` — no flag/env). Add the
profile block to `fileConfig` (yaml), project it through `fileConfig.layer()` → `resolveSettings` →
`settings` → `opts`, and set `Profile:` on the `apogee.Config` built in `runRoot`. Keep the on-disk
schema ↔ value-type mapping independently evolvable, matching `mcpServerConfig.toServerConfig()`.

**Acceptance:** `go test ./... -race` green; a `config_test.go` row proves a config.yaml declaring
`tool-call-format: markdown-fenced` (and a `<think>` thinking block) reaches
`apogee.Config.Profile`; `internal/processing` and `internal/agent/loop.go` are **untouched**
(`git diff --stat`); default (no profile) leaves `Config.Profile` zero. CHANGELOG entry (additive:
Model profile config surface). Commit:
`feat(config): add ModelProfile (tool-call format + thinking channels), plumbed from config.yaml`.

---

## 2. Cross the seam in loop.go via processing.ParserFor (native byte-identical)

**What:** consume `Config.Profile` at the loop's parse seam. One commit: the new
`processing.ParserFor` entry point + the seam wiring + oracle-vector loop tests + the doc updates.

**`internal/processing` (new `ParserFor`, frozen types):** add
`ParserFor(p domain.ModelProfile) (ToolCallParser, ContentStripper, error)` (D2) and the
`ContentStripper` interface + its three implementations (D3): `none` (no-op — `Strip` returns raw
+ "" and `IsMidChannel` false), `delimited` (over `StripThinking`/`IsThinking`), `harmony` (over
`StripHarmony`/`IsHarmonyThinking`). `ParserFor` maps the domain profile onto the **existing**
`ToolCallingConfig`/`ThinkingConfig` and calls the **existing** `NewToolCallParser` — the oracle
config types, parsers, `withDefaults()`, and their parity tests are **not touched**. New code lands
with its own small `ParserFor`/stripper unit tests (separate from the frozen oracle tests).

**`internal/agent` (construction):** in `newAgent`/`resumeAgent`, call `processing.ParserFor(cfg.
Profile)` once and store the `ToolCallParser` + `ContentStripper` on the `Agent`; a `ParserFor`
error fails construction (D2). No mirror of processing's config types appears here.

**`internal/agent` (the seam — `respondAndReview` / a helper over `reply.content`):** apply D5/D6 —
`visible, reasoning := stripper.Strip(content)`; `call, found := textParser.ParseToolCall(visible)`;
if found, `visible = textParser.StripToolCall(visible)` and assign the call its **deterministic
Turn-derived ID (D6)**; merge per D5 (native wins if present, else the text call); build
`NewResponse(visible, join(reply.thinking, reasoning), mergedCalls, reply.finish, req.View())`.
For a native profile the stripper + text parser are no-ops ⇒ **byte-identical**.

**Docs (same commit — D8):** update `internal/processing/doc.go` (now load-bearing, not dormant)
and the processing section of `docs/design/technical-design.md`; CHANGELOG entry (fenced/custom-regex
tool calls + inline thinking/harmony stripping now wired into the loop).

**Tests:** loop-level cases driving the fake responder (see `internal/agent/harness_test.go` for the
injected-fake idiom) with a configured non-native profile, asserting: a markdown-fenced call in
content becomes a dispatched `domain.ToolCall` with its markup stripped from the committed assistant
message; a custom-regex call likewise; an inline `<think>` / harmony channel is removed from the
final `MessageEvent` text and preserved as `reasoning_content`; a native profile run is unchanged.
Reuse the ported oracle vectors from the `processing` `_test.go` files as input/expected fixtures
(parity discipline). Existing tests pass **untouched** (native default).

**Acceptance:** `go test ./... -race` green; native-profile suites unchanged; new non-native cases
pass; `processing`'s oracle types/tests untouched (only the new `ParserFor`/stripper + `internal/
agent` + docs change). Commit:
`feat(loop): parse fenced/regex tool calls and strip thinking/harmony via the model profile`.

---

## 3. (Completion) Suppress in-flight channel tokens while streaming — native no-op

**What:** the batch strip in item 2 fixes committed history + the final `MessageEvent`, but
`streamResponse` still emits a `TokenEvent` for raw `delta.Content` **as it arrives**, so a model
that inlines thinking/harmony **into the content stream** briefly leaks that markup into a live UI
before item 2's post-stream strip. Close it with the same `ContentStripper`: while the accumulated
content satisfies `stripper.IsMidChannel(acc)`, hold emission; emit only the newly-revealed
*visible* delta once a span closes.

**Scope + isolation:** the trickiest half (incremental logic over a token stream) and **deferrable**
— item 2 already makes history and the final render correct. Separate commit, own tests. For a
**native** profile the stripper's `IsMidChannel` is always false ⇒ every content delta emits
immediately, exactly as today (a strict no-op).

**Chunk-boundary edge (recorded — do not "fix"):** a start token **split across deltas** (`<thi`
then `nk>`) briefly leaks the partial prefix live, because `IsMidChannel` only turns true once the
full token has accumulated. The oracle's `isThinking` mirror behaves identically — parity accepts
the leak; do **not** add partial-token suffix-buffering. Item 2's post-stream strip still removes
it from the final `MessageEvent` and history.

**Tests:** feed a chunked harmony/`<think>` stream through the fake responder and assert the emitted
`TokenEvent` sequence carries no channel markup and no analysis text, while the final visible text
matches item 2; assert a native stream emits deltas verbatim and unbuffered. **Chunk the fake stream
so channel tokens arrive whole** (the recorded edge above makes a mid-token split assertion fail by
design).

**Acceptance:** `go test ./... -race` green; native streaming byte-identical (event-for-event);
non-native streams no longer surface channel markup live. Commit:
`feat(loop): hold in-flight thinking/harmony tokens off the live stream (native unchanged)`.

---

## Explicitly NOT in this plan

- **A harmony tool-call *text* parser.** Harmony is a content-stripping concern only; a harmony
  model's tool calls arrive **native** (the Upstream parses harmony server-side) and go through the
  existing `ParseNativeToolCalls` path (D5, and the `CONTEXT.md` **Thinking channel** term). Building
  a parser that extracts a call from an inline commentary + `<|call|>` sequence is out of scope
  (no shipped model needs it).
- **Per-model auto-detection / a profile registry** (probe a model → pick a profile). This plan
  wires the *configured* profile; discovery is the deferred **[P1] switchable model-profile
  abstraction** and Phase-5 `apogee probe` work (`TODO.md`).
- **Runtime `/server` model switching** (swap the profile live). Needs the swappable provider seam
  (`upstream` is immutable after construction) — the separate **[P1]** item.
- **`response_format` / structured-output wire carriage** (a Phase-4 concern noted in
  `toProviderRequest`).
- **Any change to `internal/processing`'s oracle types, parsers, `withDefaults()`, or parity
  tests.** They are frozen (D1/D2); only a new `ParserFor` + `ContentStripper` (new code) and the
  *callers* + config source are added.
