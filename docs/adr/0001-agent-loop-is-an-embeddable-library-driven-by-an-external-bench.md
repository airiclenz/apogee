---
Status: accepted
---

# The agent loop is an embeddable library, driven by an external bench

## Context

Apogee must be validatable: we need to simulate changes and measure how the agent
behaves against real local models before trusting a Mechanism. The predecessor
`apogee-sim` already owns the instrument for this — RealSandbox, classifier, scorers,
trace archive, and the fork/stepwise counterfactual rig. The open question was *how*
that instrument reaches the agent now that the agent (not a proxy) owns the loop, tool
execution, and conversation state.

## Decision

The eval/simulation harness stays **out of the `apogee` binary** and lives in
**`apogee-sim`** (the bench). The bench reaches the agent by **importing
`github.com/airiclenz/apogee` as a Go module** and driving the agent loop as an
**embeddable library**, in-process — not by shipping the harness inside the binary, and
not over a serialized control protocol.

To make this possible, the loop is designed as an embeddable, steppable component:

- it owns **no process-global state and no implicit global filesystem state** — an agent
  is a value the caller constructs, and *every* state root (Library, sessions, config) is
  injected via `Config`, never assumed at `~/.apogee`;
- **conversation state is a cleanly copyable value** (no live handles, no process globals)
  — a hygiene property Apogee owns; the bench builds forking *on top of* it, but the loop
  itself exposes no fork;
- it exposes **Hook points as Go interfaces**, so the bench can register a temporary
  **experimental hook** to test a behaviour that is not yet a Mechanism, and the loop
  can be **stepped** one Turn at a time;
- session **snapshot/resume** (a real user feature) doubles as the bench's
  snapshot/restore primitive.

The shipped binary links none of the bench's code.

**What Apogee exposes vs. what the bench composes.** Apogee exposes only **snapshot/resume**
(a real user feature) and **clean-library hygiene** (Config-injected state roots, no process
globals, copyable conversation state, an injectable tool registry, Hook-point interfaces).
**Forking is *not* an Apogee feature** — the bench *composes* forking, record/replay, and
counterfactual scoring on its side from those primitives (deep-copy the copyable state, copy
the sandbox directory, drive two branches from byte-identical state). No fork or record/replay
code ships in the binary. (External effects — MCP, network — are non-forkable; the bench
disables them with deterministic stubs for v1 — see
[ADR 0008](0008-stateless-tools-and-non-forkable-external-effects.md).)

## Considered options

- **Harness inside the binary** (a `sim`/`bench` subcommand) — rejected: bloats the
  binary against its single-static-binary promise; the bench is a development-time
  instrument end users never run.
- **Drive the real binary over a serialized stdio protocol** (step/snapshot/intervene as
  JSON) — rejected: highest fidelity and language-agnostic, but neither is needed (both
  repos are Go), and it imposes a permanent versioned control surface on the agent —
  exactly the weight the small-binary goal resists.

## Consequences

- The loop must expose a **public** Go package (it cannot be buried in `internal/`).
- The bench measures the *library build* of the loop, not the literal shipped binary;
  fidelity is protected by a `go.mod` version pin.
- "Embeddable, steppable, no process-global state, copyable conversation state" becomes a
  Phase-0 design constraint on the loop — get it wrong and neither forking nor stepping
  is possible later.
- **Bench isolation by default**: because state roots are Config-injected, the bench
  points the Library/sessions at ephemeral directories, so sim runs never read or write the
  developer's production Library — runs stay reproducible and the production Library is
  never flooded by a narrow sim distribution. A deliberate opt-in "reduced-weight bleed" of
  sim observations into production may be added later *if* it proves worthwhile; it is not
  built by default.
- The public API is **not bench-only**: it is a deliberate **third-party embedding
  surface** — other applications can run an Apogee agent in-process. The TUI, the
  optional `apogee headless` CLI, and the bench are all consumers of this one package.
  This raises the bar on API stability and versioning (semver, a guarded public surface)
  and makes the `internal/` vs public boundary a product decision, not an afterthought.
- **Co-development & versioning.** apogee-sim uses a `go.mod replace` → a local apogee path
  during active development (the bench measures the working tree); a pinned version/commit is
  used only for archived A/B evidence. The public API is **v0.x with no stability promise
  through Phase 3, and `v1.0.0` is cut at the end of Phase 3**, once every consumer (TUI,
  bench, optional `headless`) has exercised it. Events and Hook points stay **additively
  extensible** (a new variant is a minor bump). Seed types the bench needs (e.g.
  `OrderingConstraints`) **move into apogee** and the bench imports them — never the reverse.

  > **Amendment 2026-06-25 (P3.16 — `v1.0.0` cut).** Phase 3 is complete and **`v1.0.0` is
  > tagged**. The "v0.x, no stability promise" clause above is now spent: **semver begins.** The
  > frozen v1 public surface is the root `apogee` package (`Agent`/`New`/`Resume`, `Config` and
  > its host delegates `EventSink`/`Approver`/`Asker`/`ExternalEffects`, the four-rung `Mode`
  > ladder, the `Tool`/`ToolRegistry` extension point and its `ReadOnlyTool`/`ExternalEffectTool`
  > markers, the `Event` variants, and the hook points); tools are an open extension point behind
  > the registry (ADR 0002), not root types. Events and hook points remain **additively
  > extensible** — a new Event variant or hook point is a **minor** bump, not a break. Phase-3
  > public-surface additions reviewed at the freeze (§3 D7 of the Phase-3 detail plan): the
  > `Asker` host delegate (struct-typed for additive growth) and the `ModeAllowEdits` constant.
  > The changelog is tracked from this release in [`CHANGELOG.md`](../../CHANGELOG.md).

## Amendment (2026-08-10) — hook mutation is index-addressed, and `Message.Content` is string-only

**Why now.** "Copyable conversation state" and "Hook points as Go interfaces" are two of this
record's Phase-0 constraints, and the P0.1 hook-mutation survey
(`docs/design/archived/hook-mutation-api.md` §8, decisions 5 and 6) made two calls that decide
whether those constraints actually hold once a Mechanism starts rewriting history. Both shipped in
`internal/domain/hooks.go` and both have been load-bearing since; the survey that decided them is an
archived draft, so the reasons are recorded here, where the property they protect lives.

**1. Mutation is index-addressed, never raw-slice.** A hook never receives the loop's backing
storage. It reads `Message` **value snapshots** — `Conversation.At(i)`, `Range`, or `Messages()`,
which returns a copy — and edits by index against the owning container: `SetMessageContent(i, …)`,
`Insert(i, m)`, `DropRange(start, end)`, `Append`, and `Replace(msgs)` for a wholesale rewrite. The
loop keeps the slice.

The alternative — hand out `[]Message` and take back whatever comes off the other side — fails both
classes of Mechanism at once. The in-place editors (compress, decompose) want to change one message
without asserting anything about the rest, and a returned slice makes every such edit a
whole-history claim; the wholesale rewriters (Compaction, `truncate_history`) want exactly that
claim, and only get it safely if the container knows a rewrite happened. Index addressing gives each
its own verb. It also keeps two properties this ADR promises: the backing storage never escapes, so
"copyable conversation state" stays something the loop guarantees rather than hopes for, and
`Message.extra` — the preserved unknown wire fields (`reasoning_content`, `thinking`, …) read
through `Extra` — round-trips through an edit instead of being dropped by a hook that rebuilt
messages from the fields it happened to know about. That round-trip is what the bench's fork and
snapshot/resume both rest on, and `MarshalJSON` splices the preserved siblings in sorted key order,
so the bytes stay reproducible for a later snapshot diff or hash.

**2. `Message.Content` is a string; unknown structure is preserved in `Extra`.** The OpenAI wire
shape allows an array of content parts; apogee's `Message.Content` is a plain `string`, as
apogee-sim's pipeline already flattened it. A small-model coding agent's content is text — a parts
union would push a type switch into every Mechanism, every renderer and every scorer to buy nothing
any of them use. Structure that does arrive is not lost: unknown siblings land in `extra` and are
re-emitted verbatim, so a provider field apogee has no opinion about survives a history rewrite
untouched. **Revisit when a vision-model target appears** — that is the case this call is scoped
against, and it would be an additive change to the wire projection, not a break in the hook
surface.
