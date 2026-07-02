# Plan — Cross the prompt seam: tool menu + format instructions for non-native profiles

**Date:** 2026-07-02
**Status:** READY (design grilled 2026-07-02; all D-items resolved, **no needs-design-call
escalation should be needed**) — but **QUEUED behind `parser-seam-wiring-plan.md`** finishing:
both plans touch `internal/agent/loop.go`, and this one consumes `processing.ParserFor` /
`domain.ModelProfile` from that plan. Do not start until the seam-wiring run has committed all
three items.
**Source:** item 2 of `docs/plans/parser-seam-follow-through-plan.md` — the parity gap found
reviewing the parser-seam plan: the parse side without the emit side is half a feature.
**Track:** post-`v1.0.0` **feature-parity** — additive, engine-internal (no new public domain
types expected). **Native/zero profiles stay byte-identical on the wire** at every step — the
parser-seam plan's anchor, extended to the *request* side.
**Standing requirement:** `/coding-standards` (Go + testing variants) mandatory — invoke
`implement-plan` with `coding-standards` forwarded. Pre-production: commit direct to `main`,
no PRs. `go test ./...`, `go test -race ./...`, `gofmt`, `go vet` green gate every item.

---

## The problem (grounded, verified 2026-07-02)

The parser-seam plan wires the **parse** side of a non-native Model profile: a fenced/regex tool
call in visible content is parsed and dispatched. But nothing tells the model to *emit* that
format:

- Apogee has **no system prompt anywhere**: the conversation starts empty
  (`internal/agent/agent.go:203` — `domain.NewConversation(nil)`), `domain.Config` has no prompt
  field, the host injects nothing. The only built-in instruction text is the compaction
  summarizer (`internal/context/compact.go:92`).
- Tools reach the model **only** via the native `tools` wire array
  (`internal/agent/loop.go` `toProviderTools`) — never as text.
- The oracle **suppresses that array for non-native profiles**
  (`~/Repos/Airic/apogee-code/src/orchestrator/orchestrator.ts:273` —
  `tools: format === 'native' ? tools : undefined`) and instead renders the tool menu **as text**
  plus format-specific emission instructions into the system prompt
  (`src/context/context-builder.ts` — `formatToolsBlock` :98, `buildToolCallingInstructions`
  :118, `buildMarkdownFencedInstructions` :132, `buildCustomRegexInstructions` :179,
  `pickExampleToolCall` :233).

**Consequence:** with the parser seam wired but this plan unshipped, a fenced-profile model
receives a native `tools` array its template may not render (or may error on), no text menu, and
no format instructions — so it either never calls tools or emits a format nobody told it. The
instructions are not polish: for a non-native profile **the text menu is the only channel the
model learns its tools from.**

## Design record (grilled 2026-07-02 — do not re-derive)

- **D1 — Engine-owned.** The engine renders the instructions whenever `Config.Profile`'s
  tool-call format is non-native. Decisive fact: the tool menu is **per-request, mode-filtered
  state** (`toolMenu()` — Plan mode filters to read-only) that only the engine sees; a
  host-supplied static block silently goes stale on a mode switch. The instructions also derive
  entirely from two things the engine owns — the profile and the `ToolDef` menu. A fenced profile
  is self-sufficient: configure it and it works. (*Rejected:* host-owned — every embedder
  reimplements text that must exactly match the parser's `withDefaults()`; hybrid override knob —
  additive later if a real embedder needs it.)
- **D2 — Request-scoped wire injection, never history.** `toProviderRequest` prepends the
  rendered block as a synthetic **system** message on the wire when the profile is non-native.
  It never enters domain history or the snapshot — exactly how the native `tools` array already
  works (rebuilt per request, never persisted); the text block is its 1:1 projection. This tracks
  the mode-filtered menu turn-by-turn, needs no history rewrite on a future `/server` profile
  swap, and honours the existing value that transient instruction material never persists as a
  system-prompt edit (`CONTEXT.md` Skills: "a skill never persists as a system-prompt edit").
  (*Rejected:* a persisted `RoleSystem` message — snapshots stale instructions, breaks history
  byte-identity; the `AppendToSystem` hook machinery — mutates history, documented as the
  experimental-hook path.)
- **D3 — Wire composition: one system message.** If the wire projection already carries a system
  message (an embedder can seed one via hooks), **append the block to the first system message**;
  otherwise inject a new sole system message at position 0 — the `AppendToSystem` semantics
  (`internal/domain/hooks.go:367-375`) applied at the wire seam, and the oracle's
  single-system-prompt shape. Multi-system-message handling varies across llama.cpp chat
  templates; one merged message is the safe shape.
- **D4 — A non-native tool-call format suppresses the native `tools` array** in the wire request
  — oracle parity (`orchestrator.ts:273`). Sending both would double-tell the model in two
  formats, and a template without tool support can error on the array. **Keyed on the tool-call
  axis only:** a harmony-*thinking* model with native tool-call format still gets the array (the
  profile axes stay orthogonal; harmony calls arrive native — parser-seam D5).
- **D5 — Rendering lives in `internal/processing`**, next to `ParserFor`:
  `InstructionsFor(p domain.ModelProfile, menu []domain.ToolDef) (string, error)`. Emission-side
  format knowledge belongs in the package that owns formats (parser-seam D2 rule); it reads the
  same profile knobs and `withDefaults()` the parser reads, so what we tell the model and what we
  parse cannot drift. `processing` already imports only `domain`; `ToolDef` is `domain`. A
  native/zero profile returns `""`; an unknown format can't reach it at runtime (construction
  already failed via `ParserFor`). (*Rejected:* rendering in `internal/agent` — plants format
  knowledge in the loop, the mirror-drift D2 forbids; a new `internal/prompts` package — needs
  processing's unexported defaults, one-function package.)
- **D6 — Oracle-parity discipline for the text.** Port the menu + instruction renderers from
  `context-builder.ts` (`formatToolsBlock` minus its budget-truncation call — apogee's context
  budget is a separate mechanism; `buildMarkdownFencedInstructions`;
  `buildCustomRegexInstructions`; `pickExampleToolCall`'s live example), gated by ported
  expected-output vectors like every other oracle port. This text is what the small models were
  validated against in apogee-code, and it must describe exactly what `withDefaults()` parses.
- **Scope guard (grilled):** narrow — only the profile-driven block, only for non-native
  tool-call formats; a zero/native profile adds **zero bytes** to the wire request. The oracle's
  full system-prompt template (`{{tools_block}}` / `{{agent_mode_directive}}` / `{{datetime}}` /
  `{{workspace}}` / persona) is a separate, much larger feature-parity item — parked as a TODO.md
  entry (item 3). **No ADR** (house posture, matching the parser-seam plan: the rationale lives
  in this Design record; nothing here is hard to reverse).

Reference pointers (read before implementing): `internal/agent/loop.go` (`toProviderRequest` /
`toProviderTools` / `toolMenu`), `internal/processing/factory.go` (`ParserFor` — landed by the
parser-seam plan), `internal/domain/config.go` (`ModelProfile`), the oracle at
`~/Repos/Airic/apogee-code/src/context/context-builder.ts` (line numbers above — the **source**;
never read `media/chat.js`, a minified bundle), `internal/agent/harness_test.go`
(`recordingResponder` — captures the wire `provider.Request`, the exact fixture item 2 needs).

---

## 1. `processing.InstructionsFor` — port the menu + instruction renderers (no consumer yet) — ✅ DONE (2026-07-02)

**What:** the new entry point per D5/D6, landed with its parity vectors and **no** loop caller,
so the port is one reviewable commit.

Port from `context-builder.ts`: the text tool menu (name, description, JSON-schema parameters —
`formatToolsBlock` :98, **without** the oracle's `truncateToFit` budget call), the
markdown-fenced instructions (:132 — driven by the same `fenceLanguage`/`nameField`/
`argStartField`/`argEndField` knobs and defaults the parser's `withDefaults()` uses), the
custom-regex instructions (:179), and the live example (`pickExampleToolCall` :233). Behaviour:
native/zero profile → `""`, nil error; fenced/regex → menu + format block; the knob values come
from the **profile** (empty ⇒ the same defaults the parser applies — render and parse can never
disagree).

**Tests:** ported expected-output vectors (the parity gate), plus table rows for: zero profile ⇒
empty; fenced with default knobs; fenced with overridden knobs; regex with `Pattern`; empty menu.

**Acceptance:** `go test ./... -race` green; no diff outside `internal/processing`
(`git diff --stat`); the frozen oracle parser types/tests untouched. Commit:
`feat(processing): port the tool-menu and format-instruction renderers (InstructionsFor)`.

---

## 2. Wire it in `toProviderRequest` (native byte-identical on the wire) — ✅ DONE (2026-07-02)

**What:** consume `InstructionsFor` at the request seam per D2/D3/D4, in one commit with its
loop tests and docs.

In `toProviderRequest` (or a small helper it calls): when the profile's tool-call format is
non-native — **(a)** omit the `Tools` field (D4); **(b)** render
`processing.InstructionsFor(profile, menu)` over the same menu `toolMenu()` produced for this
request; **(c)** append the block to the first system message in the wire projection, else
prepend a synthetic system message (D3). The rendered text is wire-only — it must never appear
in `domain` history, the snapshot, or any event. For a native/zero profile the request is
**byte-identical** (no omission, no injection).

**Tests:** drive the loop with `recordingResponder` (harness idiom) and assert on the captured
`provider.Request`: non-native profile ⇒ `Tools` empty, first wire message is a system message
containing the vector-exact menu + instructions, and the *next* Turn's request reflects a menu
change (e.g. Plan mode filtering) in the re-rendered block; embedder-seeded system message ⇒ the
block is appended to it, not a second system message; native profile ⇒ request deep-equal to
today's. Confirm history/snapshot stay free of the injected text.

**Docs (same commit):** `internal/processing/doc.go` (the emit side now lives here too),
technical-design processing/loop rows, CHANGELOG under Unreleased (fenced/regex models now
receive a text tool menu + emission instructions; native `tools` array suppressed for non-native
profiles).

**Acceptance:** `go test ./... -race` green; native suites untouched; the parser-seam plan's
oracle types/tests untouched. Commit:
`feat(loop): inject the profile's tool menu and format instructions at the wire seam`.

---

## 3. Parked doc updates (apply once the parser-seam run has landed — avoid mid-run conflicts)

- **`CONTEXT.md` — extend the *Model profile* term** with the emission side: the profile drives
  *both* directions at the seams — what the loop parses out of content *and* what the engine
  tells the model (text menu + format instructions, native array suppressed). Glossary language
  only, no implementation detail.
- **`TODO.md` — new parked entry:** general system-prompt / template story (the oracle's
  `{{tools_block}}` / `{{agent_mode_directive}}` / `{{datetime}}` / `{{workspace}}` / persona
  template — `context-builder.ts:38-45`), explicitly out of scope here per the grilled scope
  guard; a host-override knob for the instruction block (D1's rejected hybrid) noted as the
  additive extension point.
- Update `docs/plans/parser-seam-follow-through-plan.md`: item 2 is fulfilled by this plan
  (smoke-test Check B no longer needs the manual-instruction workaround once this ships).

**Acceptance:** docs consistent; `git status` clean. Commit:
`docs: record the prompt-seam decisions (Model profile emission side, parked template story)`.

---

## Explicitly NOT in this plan

- **A general system-prompt Config field / template engine** — the parked TODO entry (item 3).
- **A text tool menu for native profiles** — their template renders the native array; adding
  text would break the byte-identical anchor for zero gain.
- **A host override knob for the rendered block** — D1's rejected hybrid; additive later if a
  real embedder needs it.
- **Prompt-side budget truncation** (the oracle's `truncateToFit`) — apogee's context budget is
  its own mechanism (TDD §8 #8); the block is small and bounded by the menu size.
- **Any change to the frozen oracle parser types/tests or the parser-seam plan's code** beyond
  the two named seams.
