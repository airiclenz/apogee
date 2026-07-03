# Plan — Parser-seam follow-through: verify live, close the prompt-side gap, release, pick the next track

**Date:** 2026-07-02
**Status:** QUEUED — blocked on `parser-seam-wiring-plan.md` (all three items landed, gates green).
**NOT an `implement-plan` target:** items 1, 3 and 4 are user-in-the-loop (live model, tagging,
a track decision) and item 2 starts as a `grill-with-docs` design session, not code. Work this
plan interactively, top to bottom.
**Source:** the 2026-07-02 review session of `parser-seam-wiring-plan.md` — the follow-through
steps that plan deliberately leaves out, plus one **parity gap discovered during the review**
(item 2). Roadmap inputs: `docs/architecture-review-20260629-110828.html` (candidate #3),
`TODO.md` (**[P1] Server / model switching**, line ~73).
**Track:** post-`v1.0.0` feature parity, continuing the architecture-review trajectory
(#1 Resolution — shipped 8760eea; #2 parser seam — the prerequisite plan).

---

## 1. Live smoke test against a real model (manual — user in the loop)

**Why:** the seam-wiring plan's tests all drive fake responders (`harness_test.go` idiom). The
actual payoff — a small model emitting inline `<think>` or fenced calls — deserves one end-to-end
run before tagging. This is also the first real-world data point for the plan's **recorded D6
divergence** (text-parsed calls echoed upstream native-shaped in history — watch for the model
mishandling its own echoed call).

**Setup:** `llama-launcher` currently has one profile, **`gemma-4-e4b-it-qat`** (the repo's
`<think>`-style reference model — `internal/processing/doc.go`). Start it (`manage-llm-server`
skill), then add a `profile:` block to `~/.apogee/config.yaml` (the config path — `root.go:71`;
exact yaml keys are whatever seam-wiring item 1 shipped).

**Check A — thinking axis** (`delimited`, `<think>`/`</think>`): prompt something that makes the
model reason; confirm (1) no `<think>` markup in the committed assistant text or final
`MessageEvent`, (2) the reasoning survives as `reasoning_content` in the session file,
(3) live `TokenEvent`s hold mid-channel text (seam-wiring item 3), and a native run (profile
removed) streams unchanged.

**Check B — tool-call axis** (`tool-call-format: markdown-fenced`): **the prompt-seam plan
(shipped, item 2 above) now renders the fenced tool menu + emission instructions into the
request automatically — the manual-instruction workaround is no longer needed.** Configure the
fenced profile and ask the model to call a tool; confirm the fenced block parses, dispatches, the
markup is stripped from the committed text, and the follow-up turn (result in context, call
echoed native-shaped) doesn't derail the model — the D6 watch. (To exercise the parse path in
isolation you can still paste a passing vector from `internal/processing/markdown_fenced_test.go`
into the user message.)

**Harmony:** no gpt-oss profile exists in llama-launcher today. Either add one and repeat Check A
with `harmony`, or record it as untested-live (the ported oracle vectors still gate it) and move
on — do not block the release on it.

**Acceptance:** findings recorded in this file under a `### Smoke-test findings` heading (pass /
fail / surprises, esp. the D6 watch). Anything broken goes back through a fix commit before item 3.

### Smoke-test findings

**Date:** 2026-07-03 · **Model:** `gemma-4-e4b-it-qat` (llama-server, `127.0.0.1:1111`) ·
**Verdict:** PASS on all literal checks; nothing broken; no fix commit needed before item 3.

**How it was run.** Item 1 is framed as a manual TUI session, but the TUI can't be driven
head­lessly by an agent, so the run was codified as an opt-in live harness in the repo's existing
`TestE2ELiveModel` idiom — **`internal/tui/smoke_live_test.go`, `TestSmokeLiveProfileSeam`**
(gated on `APOGEE_LIVE_ENDPOINT`, `-count=1`; NEW, untracked — keep or discard). It drives the
real `agent.Agent` through the real provider client with a non-zero `Profile` set, records the
event stream, and inspects `Snapshot().State` for `reasoning_content`. Command:
`APOGEE_LIVE_ENDPOINT=http://127.0.0.1:1111 go test -race -count=1 -run TestSmokeLiveProfileSeam -v ./internal/tui/`.

**Check A — thinking axis (`delimited`): PASS — mechanism proven live; two surprises.**

*Run 1 — default server (`--reasoning-format` unset).* All three literal checks passed: committed
`MessageEvent` had no markup, `reasoning_content` present in the snapshot, live stream clean. BUT
the **native control run (zero profile) ALSO came back clean with `reasoning_content` present** —
proof that **llama-server was splitting the reasoning out-of-band into `reasoning_content` itself**
(arrives via `DeltaThinking` → `rep.thinking` → `joinThinking`, `loop.go:307`). So apogee never saw
an inline channel, the profile's stripper was a **no-op**, and the delimited profile made **no
observable difference** vs. native. The inline-strip path was NOT exercised here — only the
reasoning_content plumbing and the zero-profile anchor were.

*Run 2 — `llama-server --reasoning-format none` (server-side parsing off, inline markers exposed).*
This is where the two surprises landed:

- **Surprise 1 — gemma-4-e4b-it-qat is NOT a `<think>` model.** With the server no longer parsing,
  the raw inline reasoning showed its actual delimiters: **`<|channel>thought … <channel|>`**, not
  `<think>`/`</think>`. So the `<think>`-configured profile correctly matched nothing and left the
  reasoning in the visible text (a **fail-safe** — no false stripping — but check (1)/(2) "failed"
  only because the configured markers don't match this model). **This contradicts
  `internal/processing/doc.go:8`, which names gemma as the `<think>` reference.** Worth the
  maintainer's attention — either the doc is stale for this build or a different gemma was meant.

- **Surprise 2 (the confirmation) — the inline delimited-strip path works live.** Re-running with
  the profile's delimiters set to the model's ACTUAL markers (`start="<|channel>"`,
  `end="<channel|>"`) → **PASS**: 432-byte committed answer with **no markup and no leaked
  reasoning**, `reasoning_content` populated in the snapshot (2938 bytes), clean live stream. This
  **definitively exercises the inline strip end-to-end against a real streaming model** — the
  earlier "miss" was purely a marker mismatch, not a broken path.

Net: the seam is correct on both the out-of-band (`rep.thinking`) and inline-strip axes. Harness
change to support run 2: the delimited markers are now env-overridable
(`APOGEE_SMOKE_THINK_START` / `_END`, default `<think>`/`</think>`), so the same test points at any
model's real delimiters. Reproduce run 2 with:
`APOGEE_SMOKE_THINK_START='<|channel>' APOGEE_SMOKE_THINK_END='<channel|>' go test -run TestSmokeLiveProfileSeam/CheckA_thinking_delimited ...`
against a `--reasoning-format none` server. (Server config was reverted after the run.)

**Check B — tool-call axis (`markdown-fenced`): PASS; D6 watch clean, friction noted.**
- The prompt-seam wiring injected the fenced menu + emission instructions automatically (no manual
  workaround): the model emitted fenced blocks, **4 calls parsed & dispatched**, the file
  `greeting.txt` landed, and the committed final text had the fence markup stripped (no ` ``` `). ✓
- **D6 watch — no derail.** The 4B model fumbled the JSON args across turns — `{"content":…}`
  (no path) ×2, then `{"path":"greeting.txt"}` (no content), then finally the complete
  `{"content":"Hello, Apogee!","path":"greeting.txt"}`. Each text-parsed call was echoed back
  native-shaped in history; **the model recovered from the tool-error feedback and its own echoed
  calls and converged to a valid write** rather than derailing or looping. The seam handled the
  echo correctly — the D6 divergence did not bite. Friction (4 attempts) is a small-model
  fenced-schema limitation, not a seam defect.

**Harmony axis:** untested-live (no gpt-oss profile in llama-launcher) — as the plan allows,
recorded as untested-live (oracle vectors still gate it); not blocking the release.

---

## 2. Close the prompt-side parity gap — grill first, then its own plan

> **DONE (grill) 2026-07-02; FULFILLED 2026-07-02** — grilled the same day; the resulting plan
> **`docs/plans/prompt-seam-wiring-plan.md`** has **shipped** its wire injection (items 1 & 2
> committed after the seam-wiring run). All decisions below are settled there: engine-owned,
> request-scoped wire injection (never history), native `tools` array suppressed for non-native
> tool-call formats, `processing.InstructionsFor` next to `ParserFor`, oracle-parity text
> vectors, narrow scope (general system-prompt template parked as a TODO entry). **Now that it
> ships, item 1's Check B no longer needs the manual-instruction workaround** — a non-native
> profile is prompted to emit its tool-call format automatically.

**The gap (found reviewing the seam plan; verified 2026-07-02):** the TS oracle injects
**format-specific tool-calling instructions into the system prompt** when the profile is
non-native — `buildToolCallingInstructions` in
`~/Repos/Airic/apogee-code/src/context/context-builder.ts` (~line 117) renders the tool menu as
text plus markdown-fenced / custom-regex emission instructions. The Go port has **no counterpart**:
parsing fenced calls is useless if nothing tells the model to emit them.

**Grounding facts (verified — do not re-derive):**
- Apogee has **no built-in system prompt anywhere**. The conversation starts empty
  (`internal/agent/agent.go:203` — `domain.NewConversation(nil)`); `domain.Config` has no
  system-prompt field; the host (`cmd/apogee/wire.go`) injects nothing. The only built-in
  instruction text is the compaction summarizer (`internal/context/compact.go:92`).
- Tools reach the model **only** via the native `tools` wire array
  (`internal/agent/loop.go:603` `toProviderTools`) — never as text.
- Runtime system-message injection machinery exists but is unused at startup:
  `AppendToSystem` / `InjectContext` (`internal/domain/hooks.go:308-336`).

**Decisions for the grill (`grill-with-docs`):**
- **Who owns the instructions** — the engine (loop injects a system message when
  `Profile` is non-native, using the hook-rewriter machinery or conversation seeding) vs the
  embedder/host (document that a text-format profile requires the host to supply instructions)?
  The oracle is engine-owned; apogee's embedder-first posture (ADR 0010) may argue otherwise.
- **Whether a non-native profile should suppress the native `tools` array** in
  `toProviderRequest` — check what the oracle sends for fenced models; a server template without
  tool support may error or silently drop it.
- **Where the text rendering of the tool menu comes from** (a text projection of
  `domain.ToolDef` — new engine code either way).
- Whether this couples to the (absent) general system-prompt story — a `Config` prompt field is
  its own feature-parity item; don't let the grill balloon into it without deciding so explicitly.

**Acceptance:** a grilled plan file in `docs/plans/` (same discipline as the seam-wiring plan:
design record, numbered items, native byte-identical anchor — a zero profile must add **no**
prompt text). CONTEXT.md/ADR updates as the grill decides.

---

## 3. Release chores (after item 1 passes; item 2 may ship in the same minor or the next)

- Roll up the `[Unreleased]` CHANGELOG entries (the seam-wiring plan added one per item).
- Tag the next **additive minor** — current tag is `v1.0.0`, so `v1.1.0` (ADR 0001 / decision
  #18; the Model profile + seam wiring are additive, native byte-identical).
- Archive `docs/plans/parser-seam-wiring-plan.md` → `docs/plans/archived/` (house convention —
  commit 317b4b7 did the same for the Resolution plan). Archive this file too once item 4 ends it.

**Acceptance:** tag pushed; plans archived; `git status` clean.

---

## 4. Pick the next track (decision, not code)

Two grounded candidates; **recommendation: grill the `/server` item next**, with candidate #3 as
the lighter alternative:

- **[P1] Server / model switching** (`TODO.md` ~line 73): `/server` live endpoint switch
  (re-probe `/v1/models`, rebind the `provider` seam — `upstream` is immutable after
  construction today), a **switchable model-profile** abstraction (sampling, context-budget %,
  thinking/tool-call format — exactly the profile-as-data the seam plan's D1 was designed to
  seed), and start/stop for a local llama.cpp server. Builds directly on what just shipped;
  biggest functional win; also unblocks the deferred `/server` TUI command (TODO.md chat
  mini-language item). Route: `grill-with-docs`.
- **Architecture-review candidate #3 — "Lift the chat input out of the god-Model"**
  (`docs/architecture-review-20260629-110828.html`): `internal/tui/model.go` has ~25 fields / 8
  concerns; the five chat-input concerns (input box, autocomplete, pendingSkills chips, file
  cache, mouse selection) are loose fields — `acceptAutocomplete` mutates `m.input` across a
  file boundary and is only testable through the full `Update` loop. Note: the *feature* work in
  that area shipped piecemeal (2026-06-26 mini-language core, `/skill`, filecache), but the
  review's point is **structural** — re-read the candidate against current code before planning;
  the lift may have shrunk or shifted. TUI-internal, independent of items 1–3.

**Acceptance:** next plan started (grill session opened) or the choice recorded here with a
reason; then archive this file (item 3).
