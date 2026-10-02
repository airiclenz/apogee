# Refocus findings — Anthropic thinking fixes, bwrap fence, tui import rule, doc drift — plan

**Goal:** No Anthropic request carries a thinking shape its model rejects or replays signed thinking over a changed
prefix; bwrap resolves only from fixed system directories; `internal/tui` production code no longer imports
`internal/provider`; the verified and suspected doc drift from the 2026-10-02 refocus is checked against code and fixed.
**Date:** 2026-10-02
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** fbf02a1a
**Sources:**
- beads `apogee-anthropic-thinking-disabled-400`, `apogee-anthropic-merged-text-blocks`, `apogee-anthropic-prefix-rebuilt-per-request`, `apogee-bwrap-direnv-path`
- ADRs 0010, 0024, 0076, 0078 (amendment 2026-10-02), 0081; `docs/design/confinement-execution-contract.md`
- Claude API (claude-api skill, models.md / model-migration.md / preserved-thinking-migration.md): Opus 5.5, Fable 5/5.1, Mythos 5.x 400 on `thinking:{type:disabled}` (omit the key); Sonnet 5.5 400s on it (send `{type:between_tools}` at effort ≤ high); Opus 5 accepts it at effort ≤ high; a thinking block's signature binds the top-level `system`, `tools` and every earlier message.

**Ratified design calls (owner, 2026-10-02):**
- **B1 tui→provider:** move the effort types out of provider's reach for tui; no ADR exception (handoff C4 dropped).
- **A1 no-effort shape:** Opus 5.5 / Fable 5.x / Mythos 5.x get no `thinking` key plus `output_config.effort: "low"`; Sonnet 5.5 gets `between_tools`; every other id keeps `disabled`.
- **A1 matching:** a wire-local prefix denylist in `internal/provider`; a future model needs a table row.
- **A3:** a design ADR, then its implementation; the design is a client-side prefix-digest guard (append-only redesign rejected).
- **A4:** bwrap only from `/usr/bin`, `/bin`, `/usr/local/bin`, `/run/current-system/sw/bin`; any other location blocks (fail closed).
- **D-list:** one verify-then-fix item per document group.
- **Writer calls:** B1 uses the domain mirror-and-convert pattern `domain.EffortDialect` already follows (provider stays domain-free, ADR 0010); the A1 omit/between_tools shapes drop profile sampling as adaptive does; Mechanism notes sit as a blockquote after the ADR front matter; an archived plan's status flips to done only when its archiving commit says completed/implemented.

**Standing requirements:**
- skills: coding-standards
- Pi 5 machine rule: never `go test -race`/`-cover` over a package pattern or a whole heavy package (`./internal/tui/`, `./internal/agent/`, `./cmd/apogee/`) — narrow those with `-run`; prefix every test run with `GOMEMLIMIT=2GiB`; never two test runs at once. Brief every sub-agent with this rule.
- A CHANGELOG wording fix travels in the item's sidecar entry, never as a direct edit.

**Out of scope:**
- CHANGELOG release headings lagging VERSION (a `/cut-release` step); any version change.
- Feature beads apogee-rw6, -089, -afu, -bmj, -3fs, -zlg, -vi5, -4h5, -l8s, -sbj, -ifrv, -03a, -8za, -per-server-idle-timeout; the 25 parked beads.
- `docs/design/workflow-bench-experiment.md` (runs in `apogee-sim`); `IDEAS.md` (gitignored, owner-local).
- Making the system prompt / tail notes append-only (A3 alternative the owner rejected).
- ADR 0011 (it never names `internal/provider`; nothing to amend).

**Regression check (2026-10-02, fbf02a1a):**
- 1: guard folded; supersedes, for the omit+low ids, ADR 0078's 2026-10-02 amendment bullet 3 and CHANGELOG.md's apogee-4kl "Compaction summaries never request thinking" claim
- 2: guard folded
- 3: guard folded (writer decision: preserved-thinking scope); narrows CHANGELOG.md's apogee-4kl engine-half resume promise and CONTEXT.md's thinking entry
- 4: guard folded
- 5: guard folded (writer decision: preserved-thinking scope)
- 6: guard folded (writer decision: acceptance grep); supersedes ADR 0081's and ADR 0042's 2026-09-17 "resolved on PATH at construction"
- 7: guard folded
- 8: guard folded
- 9: guard folded
- 10: guard folded (writer decision: note marker)
- 11: guard folded
- 12: guard folded (writer decision: depends on item 4)
- 13: guard folded
- 15: guard folded
- 16: guard folded
- 17: guard folded
- 18: guard folded

## 1. Anthropic no-effort requests send each model a thinking shape it accepts

**What:**
**Goal:** On the anthropic wire, a request whose effort is off, none, minimal or unset sends: for a model id starting
(case-insensitive) with `claude-opus-5-5`, `claude-fable-5` or `claude-mythos-5` — no `thinking` key and
`output_config.effort: "low"`; for `claude-sonnet-5-5` — `thinking: {"type":"between_tools"}` and no effort; for every
other id — `thinking: {"type":"disabled"}` byte-identical to base. Efforts low and above are unchanged. Compaction
summaries and the naming call get the same mapping. ADR 0078 and `docs/manual/configuration.md` describe the mapping.
Fixes bead `apogee-anthropic-thinking-disabled-400`: every compaction on Opus 5.5 / Sonnet 5.5 / Fable 5.x 400s.
**Approach (assumed at the header base):** in `internal/provider/wire_anthropic.go` `buildBody`, replace the
unconditional `anthropicThinking{Type: anthropicThinkingDisabled}` start with one unexported prefix table looked up on
`req.Model` (longest prefix wins); make `anthropicRequest.Thinking` omittable. The omit+low and between_tools shapes
drop profile temperature/top_p/top_k exactly as the adaptive shape does. `internal/profiles` is not touched. Add an
ADR 0078 amendment "(2026-10-02) — the no-effort shape is per model" that supersedes the first bullet of the earlier
2026-10-02 amendment (mark that bullet superseded, do not delete it). The sidecar CHANGELOG entry rewords the
"Anthropic thinking follows the effort (apogee-4kl)" bullet's "no effort keeps it off, so a request with no effort is
unchanged" claim. Prose rule: every doc or comment claiming no effort always sends `disabled` —
`grep -rn '"disabled"\|thinking.*disabled' docs internal --include=*.md --include=*.go`.
**Regression guard.** `anthropicCodec.encode` keeps reporting `carriesEffort` as the requested effort
(`anthropicEffort`'s bool), never `wire.OutputConfig != nil`: the omit+low shape writes `output_config` for a no-effort
request, and its faults must not gain `thinkingEffortHint`; the table pins carries=false for "", off, none and minimal
on every id. The compact test cannot run over `scriptResponder` (an openai-wire client, and `Client.encode` overrides
`req.Model`): it builds a `provider.NewClient` with `WithWire(provider.WireAnthropic)`, model `claude-opus-5-5` and
stubllm's Transport, and asserts on `stubllm.Request.Body` and `Effort.OutputEffort`. The prose rule widens to every
claim that the off rung or the summariser means no thinking on the anthropic wire. Superseded for the omit+low ids:
ADR 0078's 2026-10-02 amendment bullet 3 ("The compaction summary never requests thinking on this wire" — marked
superseded beside bullet 1), CHANGELOG.md's apogee-4kl "Compaction summaries never request thinking on this wire"
(corrected in the sidecar entry), and the `internal/agent/compact.go` override comment and `cappedSummaryAskedOffCause`.
**Files:** internal/provider/wire_anthropic.go; internal/provider/wire_anthropic_test.go; internal/agent/compact_test.go; docs/adr/0078-a-servers-wire-is-a-per-entry-codec-inside-the-provider-client.md; docs/manual/configuration.md; internal/agent/compact.go
**Read first:** internal/provider/wire_anthropic.go — buildBody, anthropicEffort, anthropicCodec.encode; internal/provider/client.go — Client.encode, thinkingEffortHint; internal/provider/fault.go — classify;
internal/agent/compact.go — compactCompleter.Complete, cappedSummaryAskedOffCause; internal/agent/compact_test.go — TestCompactSummarizerAsksForNoThinkingOnTheAnthropicWire; internal/agent/harness_test.go — scriptResponder;
internal/stubllm/log.go — Request.Body, Effort.OutputEffort; internal/provider/wire_anthropic_test.go — TestAnthropicCodecEffort, TestAnthropicCodecEffortDropsSamplingUnderThinking
**Tests:** table test in `wire_anthropic_test.go`: ids `claude-opus-5-5`, `claude-fable-5-1`, `claude-mythos-5`,
`claude-sonnet-5-5`, `claude-opus-4-8`, `minimax-m3` × efforts "", off, none, minimal, low — exact `thinking` /
`output_config` / sampling JSON and the carries-effort bool (false for "", off, none, minimal on every id). In
`compact_test.go`, over a `provider.NewClient(…, WithWire(provider.WireAnthropic))` with model `claude-opus-5-5` on
stubllm's Transport: the summary request's `Request.Body` has no `"thinking"` key and `Effort.OutputEffort` is `low`.
**Acceptance:**
- `go vet ./internal/provider/ ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/provider/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Compact' ./internal/agent/`
- `grep -c 'between_tools' docs/adr/0078-a-servers-wire-is-a-per-entry-codec-inside-the-provider-client.md docs/manual/configuration.md` — each ≥ 1
**Closes:** apogee-anthropic-thinking-disabled-400
**Commit:** `fix(provider): send each anthropic model a no-effort thinking shape it accepts`

## 2. Anthropic replay keeps a reply's text blocks in their original places

**What:**
**Goal:** An assistant reply carrying at least one `thinking` or `redacted_thinking` block round-trips decode (whole
and streamed) → persisted message → encode with every block (text, thinking, redacted_thinking, tool_use) in its
original order and content, including `[text A, thinking, text B]` and text after a `tool_use`. A reply with no
thinking block encodes byte-identically to base, and reasoning entries persisted at base still encode as at base.
Fixes bead `apogee-anthropic-merged-text-blocks`.
**Approach (assumed at the header base):** `domain.Message.Content` stays one string — no domain or agent change. The
provider-private position record `anthropicReasoningEntry` (`after_text` / `after_calls`) gains what rebuilds the full
block layout (e.g. byte offsets into `Content`, and the layout of text relative to tool calls); its producers
(`anthropicResponse.toRawResponse`, the stream parser's `anthropicReasoningBlock` / `reasoningEntry`) record it and its
consumers (`decodeReasoningEntry`, `slot`, `placeReasoning`, `assistantBlocks`) split `Content` back into the original
text blocks. Everything between treats entries as opaque. Update the ADR 0078 placement wording (prose rule:
`grep -rn 'after_text\|after_calls\|order the reply had' docs CONTEXT.md`). The sidecar CHANGELOG entry corrects the
replay-order claim in the 324cd2fd bullet.
**Regression guard.** `Content` is edited after decode (`salvageToolCall`'s `resp.SetText`,
`collectCompletion`'s stripper, `assembleResponse`'s `StripToolCall`), so the consumers check the recorded layout
against the `Content` they get — every offset ≤ len(Content), on a rune boundary, total length as stamped — and on any
mismatch fall back to base placement, never a panic or a mid-rune split. The prose rule's grep adds
`order received\|keeps its place\|original places` (ADR 0078's "Thinking blocks go back in the order received" bullet;
`CONTEXT.md`'s thinking entry). The sidecar correction names both apogee-4kl CHANGELOG bullets (provider half and engine half).
**Files:** internal/provider/wire_anthropic.go; internal/provider/wire_anthropic_stream.go; internal/provider/wire_anthropic_test.go; internal/provider/wire_anthropic_stream_test.go; docs/adr/0078-a-servers-wire-is-a-per-entry-codec-inside-the-provider-client.md; CONTEXT.md
**Read first:** internal/provider/wire_anthropic.go — anthropicReasoningEntry, reasoningEntry, decodeReasoningEntry, placeReasoning, assistantBlocks, anthropicResponse.toRawResponse; internal/provider/wire_anthropic_stream.go — anthropicStream.openReasoning, anthropicReasoningBlock;
internal/provider/wire_anthropic_test.go — TestAnthropicReplyReplaysInTheOrderReceived, TestAnthropicCodecEncode; internal/agent/builtins.go — salvageToolCall; internal/agent/loop.go — assembleResponse, assistantMessage;
internal/agent/collect.go — collectCompletion
**Tests:** extend `TestAnthropicReplyReplaysInTheOrderReceived` with `[text, thinking, text]`,
`[thinking, text, tool_use, text]`, `[text, redacted_thinking, text, tool_use]` (whole and SSE); a base-shaped entry
(`after_text`/`after_calls` only) encodes as at base; a no-thinking reply is byte-identical; an entry whose offsets
overrun `Content` (or split a rune) encodes as at base without a panic.
**Acceptance:**
- `go vet ./internal/provider/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/provider/`
Depends on item 1.
**Closes:** apogee-anthropic-merged-text-blocks
**Commit:** `fix(provider): replay an anthropic reply's text blocks in their original places`

## 3. ADR 0092 — replayed signed thinking is guarded by a prefix digest

**What:**
**Goal:** `docs/adr/0092-replayed-signed-thinking-is-guarded-by-a-prefix-digest.md` exists (front-matter
`Status: accepted`) and records: a reply's signed thinking blocks go back upstream only when the request's prefix before
that reply — top-level `system`, `tools`, and every earlier message, as the anthropic codec encodes them — digests
identically to the prefix the reply was produced on; otherwise the codec drops that reply's thinking blocks client-side
(never a 400, never a server-side beta field). It also records: the digest is computed by the codec at encode time and
stamped into the reply's reasoning entries at decode; an entry without a digest (persisted before this ADR) is dropped;
compaction (`context.Compact` keeps only the prefix and a summary with no thinking, so nothing replays across a fold);
resume (`recordContent` strips committed advice, so a resumed prefix differs and drops); the accepted churn sources
(task-list block, mode/orientation, tail notes, pruning, the cancel note); the rejected alternative (append-only
prefix, owner 2026-10-02). ADR 0078's 2026-10-02 amendment and `CONTEXT.md`'s thinking glossary entry point to ADR 0092.
**Regression guard.** ADR 0092 states the guard's scope — it applies only to model ids in the provider's preserved-thinking set (prefixes claude-fable-5-1, claude-mythos-5-1, claude-opus-5-5, claude-sonnet-5-5), held in the same wire-local prefix table as item 1's no-effort shapes; every other model (older Claude, unknown ids, non-Anthropic servers on the anthropic wire) replays as at base, because older Claude models require the last assistant turn's thinking in a tool loop and a client-side drop there would itself 400 (writer decision, 2026-10-02).
The churn sources are a rule, not a closed list: any change to `system`, `tools` or an earlier message between the
producing request and a later one, whatever its source — `floor.CapToolResults` (internal/floor/resultcap.go) is one
named example. The digest travels per request from encode to decode, as `carriedEffort` does (`Client.encode` →
`parseSSE`), never as codec or Client state (an unrouted delegate shares the parent's client and codec). Narrowed, not
only pointed at: CHANGELOG.md's apogee-4kl engine-half "survive a session save and resume" promise (named in the ADR,
corrected in the sidecar entry) and `CONTEXT.md`'s thinking entry, which is qualified.
**Files:** docs/adr/0092-replayed-signed-thinking-is-guarded-by-a-prefix-digest.md; docs/adr/0078-a-servers-wire-is-a-per-entry-codec-inside-the-provider-client.md; CONTEXT.md
**Read first:** docs/adr/0078-a-servers-wire-is-a-per-entry-codec-inside-the-provider-client.md — Amendment (2026-10-02); CONTEXT.md — thinking channel entry; internal/agent/wire.go — toProviderRequest, replayedThinking;
internal/context/compact.go — Compact; internal/domain/advice.go — recordContent; internal/floor/resultcap.go — CapToolResults;
internal/agent/turn.go — turnLifecycle.settle; internal/agent/subagent.go — newChildAgentOn
**Tests:** none (docs).
**Acceptance:**
- `test -f "docs/adr/0092-replayed-signed-thinking-is-guarded-by-a-prefix-digest.md"`
- `grep -c '0092' docs/adr/0078-a-servers-wire-is-a-per-entry-codec-inside-the-provider-client.md CONTEXT.md` — each ≥ 1
- `grep -q '^Status: accepted' "docs/adr/0092-replayed-signed-thinking-is-guarded-by-a-prefix-digest.md"`
Depends on item 2.
**Commit:** `docs(adr): guard replayed signed thinking with a prefix digest`

## 4. stubllm scripts signed thinking on the anthropic route

**What:**
**Goal:** An `internal/stubllm` script on the anthropic route can return an assistant turn holding `thinking` blocks
(with `signature`) and `redacted_thinking` blocks interleaved with text and `tool_use`, whole and streamed; every
existing script's output is byte-identical to base.
**Approach (assumed at the header base):** extend the script turn type in `internal/stubllm/script.go` with an ordered
block list the anthropic renderer in `internal/stubllm/wire_anthropic.go` emits as content blocks / SSE events; the
openai route ignores it.
**Regression guard.** The Messages renderer lives in `internal/stubllm/server.go` (`messageEvents`,
`writeMessagesWhole`, `writeMessagesStream`); `internal/stubllm/wire_anthropic.go` gains only the shape members —
`anthropicBlock` `signature` and `data`, `anthropicDelta` `signature` (for `signature_delta`) — all omitempty, so
existing bytes are unchanged. `docs/design/test-drivers.md` gets a Turn-kinds row for the new key, and its Messages
render-order sentence covers the scripted interleaved order. The test asserts the inner wire block (the entry's `block`
member, or the bare entry) or a decode→encode round trip, never raw `RawResponse.ThinkingBlocks` entry bytes, which
items 2 and 5 re-shape.
**Files:** internal/stubllm/script.go; internal/stubllm/wire_anthropic.go; internal/stubllm/wire_anthropic_test.go; internal/stubllm/server.go; docs/design/test-drivers.md
**Read first:** internal/stubllm/server.go — messageEvents, writeMessagesStream, writeMessagesWhole, beforeCut; internal/stubllm/wire_anthropic.go — anthropicBlock, anthropicDelta, stopReason; internal/stubllm/script.go — Turn, Turn.validate, kindCount, isCompletion, finishReason;
internal/stubllm/server_test.go — TestMessagesRouteStreamsBlockEvents, TestMessagesRouteRunsAScriptedToolLoop; internal/provider/wire_anthropic_test.go — TestAnthropicReplyReplaysInTheOrderReceived (signature_delta SSE shape);
docs/design/test-drivers.md — Turn kinds
**Tests:** a script with `[thinking(sig), text, tool_use]` decodes through `provider.Client` on the anthropic wire with
blocks and signature intact, whole and streamed (asserted on the inner wire block or a decode→encode round trip, not
raw entry bytes); existing stubllm tests unchanged.
**Acceptance:**
- `go vet ./internal/stubllm/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/stubllm/`
**Commit:** `test(stubllm): script signed thinking blocks on the anthropic route`

## 5. Replayed signed thinking passes the prefix-digest guard

**What:**
**Goal:** As ADR 0092 records: on the anthropic wire, a request replays an earlier reply's thinking blocks only when
the prefix before that reply digests as it did when the reply was produced. An agent-level test over stubllm shows:
(a) an unchanged prefix replays the first reply's thinking byte-identically in place; (b) after the system prompt
changes (a `task_list` call changes the standing task-list block) the earlier reply's thinking is absent from the next
request; (c) after a compaction fold no thinking block is sent; (d) a base-persisted entry without a digest is not sent.
Fixes bead `apogee-anthropic-prefix-rebuilt-per-request`.
**Approach (assumed at the header base):** the codec digests (system, tools, messages before each assistant message) as
it encodes; the `provider.Client` hands the produced request's digest to the whole and stream decoders, which stamp it
into the reply's reasoning entries; `assistantBlocks` drops a message's entries whose digest differs from the digest of
the prefix encoded so far. `internal/agent` `replayedThinking`'s same-model gate is unchanged. The sidecar CHANGELOG
entry states the guard.
**Regression guard.** the guard fires only for the preserved-thinking set ADR 0092 records; add a test that a claude-opus-4-8 reply's thinking replays across a changed prefix exactly as at base; item 5 re-keys item 2's new entry-format tests to the added digest field and keeps base-shaped entries decoding.
The digest is passed per call from `Client.encode` to the whole and stream decoders (`internal/provider/stream.go`
`Client.Stream` included), never as a Client or codec field, since a borrowed parent client streams concurrently for
children; where the `wireCodec` signatures change, `openaiCodec` (`wire_openai.go`) and the direct `parseSSE` callers in
`wire_anthropic_stream_test.go` follow. The agent tests use a new helper in `thinking_replay_test.go` —
`provider.NewClient(…, WithWire(provider.WireAnthropic))` over `stubllm.InProcess` on a preserved-thinking-set model,
`cfg.Wire = "anthropic"`, and for (b) `cfg.SystemPrompt` set (the task-list block only rides along) — and read the
replayed blocks from stubllm `Request.Body`. Every comment claiming ThinkingBlocks are always written back, or
describing the entry shape, is updated (`internal/provider/wire.go`, the `wire_anthropic.go` header).
**Files:** internal/provider/wire_anthropic.go; internal/provider/wire_anthropic_stream.go; internal/provider/client.go; internal/provider/wire_anthropic_test.go; internal/agent/thinking_replay_test.go; internal/provider/stream.go; internal/provider/wire_openai.go; internal/provider/wire_anthropic_stream_test.go; internal/provider/wire.go
**Read first:** internal/provider/wire_anthropic.go — buildBody, assistantBlocks, placeReasoning, reasoningEntry, decodeReasoningEntry; internal/provider/client.go — wireCodec, Client.encode; internal/provider/stream.go — Client.Stream;
internal/provider/wire_anthropic_test.go — TestAnthropicCodecEncode, TestAnthropicReplyReplaysInTheOrderReceived; internal/agent/wire.go — toProviderRequest, replayedThinking; internal/agent/harness_test.go — scriptResponder, baseConfig;
internal/agent/state_test.go — TestSnapshot_RoundTripsSignedThinking; internal/agent/standingblocks.go — standingBlocks
**Tests:** provider, for a preserved-thinking-set id: digest match replays, mismatch drops, no-digest drops; a
`claude-opus-4-8` reply's thinking replays across a changed prefix exactly as at base; item 2's entry-format tests
re-keyed to the digest field, base-shaped entries still decoding. Agent: (a)–(d) above in
`internal/agent/thinking_replay_test.go` using the new anthropic-wire helper (not `scriptedUpstream`, which is
openai-wire) and item 4's signed blocks.
**Acceptance:**
- `go vet ./internal/provider/ ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/provider/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'ThinkingReplay|SignedThinking' ./internal/agent/`
- `grep -rn 'writes them back\|written back\|block itself' internal/provider internal/agent --include=*.go` — every hit states the guard or the new entry shape
Depends on items 3 and 4.
**Closes:** apogee-anthropic-prefix-rebuilt-per-request
**Commit:** `fix(provider): replay signed thinking only over the prefix it was produced on`

## 6. bwrap resolves only from fixed system directories and fails closed elsewhere

**What:**
**Goal:** `NewNamespaceConfiner` takes bwrap only as the first executable among `/usr/bin/bwrap`, `/bin/bwrap`,
`/usr/local/bin/bwrap`, `/run/current-system/sw/bin/bwrap`; `PATH` never chooses it. None present → the namespace
backend is unavailable with `CauseBackendAbsent` and reason `bwrap not found in /usr/bin, /bin, /usr/local/bin or
/run/current-system/sw/bin`; a bwrap found only on `PATH` → unavailable, same cause, reason `bwrap at <path> is outside
the trusted system directories` (that path is never executed). Unavailable behaves as at base: auto gates to approval,
unattended runs block, sync reactions are refused — no unconfined fallback. Fixes bead `apogee-bwrap-direnv-path`.
**Approach (assumed at the header base):** replace the inline `exec.LookPath("bwrap")` in
`internal/platform/namespace_linux.go` with an unexported candidate list and a stat seam for tests; reuse
`newNamespaceConfiner` / `probeNamespace`. Rework `TestNamespaceProbeReasonNamesTheCause/bwrap_absent_from_path` and
the `"bwrap not on PATH"` literal in `TestSelectLinuxConfiner`. Rewrite the contract's 2026-10-02 bwrap-exception
amendment into the fixed-list rule. Prose rule: every passage saying bwrap is resolved on PATH —
`grep -rn 'bwrap not on PATH\|on PATH at construction\|bwrap.*on PATH\|LookPath("bwrap")' internal docs SECURITY.md`.
**Regression guard.** The comment and fixture sites carrying the old reason take the new one:
`internal/domain/confinement.go`, `internal/probe/confinement.go`, `internal/probe/confinement_test.go`,
`internal/probe/host_test.go`. The prose rule covers every passage saying bwrap is found, resolved or looked up on
PATH, backticked or line-split ones included — `` grep -rni 'PATH`\?\b.*\(construction\|lookup\)\|on `\?PATH\|PATH-lookup\|LookPath' internal docs/design docs/adr docs/manual SECURITY.md ``
plus `grep -rn -A1 'bwrap$'` — and `internal/security/doc.go` declares the fixed-list bwrap as `ResolveProgram`'s third
exception. Superseded, each by a dated note: ADR 0081's "`bwrap` is resolved on `PATH` once at construction" and ADR
0042's 2026-09-17 amendment ("resolved on `PATH` at construction"). A sidecar CHANGELOG entry names the four trusted
dirs, the two reason sentences, and that a PATH-only bwrap now gates Auto. "Candidate present → used" makes the
candidate list itself the seam (pointed at `stubLauncher`'s path, exit 0) or splits a pure resolver (candidates, stat,
look) → (path, reason) tested without a launch — a stat seam alone still probes the real path.
**Files:** internal/platform/namespace_linux.go; internal/platform/namespace_linux_test.go; internal/platform/confiner_linux.go; internal/platform/confiner_linux_test.go; internal/platform/doc.go; docs/design/confinement-execution-contract.md; docs/manual/probe.md; docs/manual/configuration.md; internal/domain/confinement.go; internal/probe/confinement.go; internal/probe/confinement_test.go; internal/probe/host_test.go; internal/security/doc.go; docs/adr/0081-linux-falls-back-to-a-namespace-fence-through-bwrap.md; docs/adr/0042-external-programs-are-optional-enhancements-never-prerequisites.md
**Read first:** internal/platform/namespace_linux.go — NewNamespaceConfiner, newNamespaceConfiner, probeNamespace; internal/platform/namespace_linux_test.go — TestNamespaceProbeReasonNamesTheCause, stubLauncher; internal/platform/confiner_linux_test.go — neitherHostReason, TestSelectLinuxConfiner;
internal/platform/confiner_linux.go — selectLinuxConfiner; docs/design/confinement-execution-contract.md — §2.3 namespace backend, §5 Linux bullet, Amended 2026-10-02 note;
internal/probe/confinement_test.go — TestCapabilityLine; internal/security/doc.go — ResolveProgram exceptions paragraph
**Tests:** candidates absent → reason names the four dirs; bwrap only on a temp `PATH` dir → reason names that path and
the stub is never run; candidate present → used (the candidate list is the seam, pointed at `stubLauncher`'s path);
the `internal/probe` fixtures carry the new reason. `TestSelectLinuxConfiner` asserts the exact joined reason string the
probe prints.
**Acceptance:**
- `go vet ./internal/platform/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/platform/`
- `go vet ./internal/probe/ ./internal/domain/ ./internal/security/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/probe/`
- `! git grep -n 'bwrap not on PATH' -- internal docs SECURITY.md ':!docs/plans'`
**Closes:** apogee-bwrap-direnv-path
**Commit:** `fix(platform): resolve bwrap only from fixed system directories`

## 7. The TUI reads effort capabilities through internal/domain

**What:**
**Goal:** No non-test `.go` file in `internal/tui` imports `internal/provider`. `domain.EffortSupport` (stdlib-only,
beside `domain.EffortDialect`) carries what the TUI reads; `internal/provider` keeps its own `EffortDialect` /
`EffortSupport` and stays domain-free; the conversion happens once, outside `internal/tui`.
**Approach (assumed at the header base):** add `domain.EffortSupport{Supported, Dialect, Efforts, Default, Mandatory}` in
`internal/domain/config.go`. `internal/heartbeat`'s `Beat` carries the domain type, converted by one function in
`internal/heartbeat`; `tui.Rebind` takes `domain.EffortDialect` and its `cmd/apogee` callers convert. Switch
`internal/tui/effort.go`, `heartbeat.go`, `tui.go` and the tui tests to the domain types (mechanical); reword
`internal/tui/doc.go`'s provider mention. `live_test.go` / `smoke_live_test.go` keep `provider.NewClient` (test-only).
**Regression guard.** Retyping `Beat` / `ModelSummary.EffortSupport` reaches `cmd/apogee` production code:
`resolveDelegationTarget` (delegation.go), `firingConfig` (wire_firing.go) and `scheduleWiring.fire` (schedule.go) each
convert at their read/write through one total domain↔provider helper (the `internal/agent/wire.go`
`toProviderDialect` / `toDomainDialect` shape); the cmd/apogee tests that build or compare `Beat.EffortSupport` or pass
`provider.EffortDialectNone` to `Rebind` retype to the domain values. `tui.Rebind` has no cmd/apogee caller: the
conversion site is the implementer `serverHost.Rebind` (cmd/apogee/wire_server.go), domain→provider; `h.w.rebind` keeps
its provider signature (wire_helpers_test.go `rebindProbe` untouched); `fakeServerHost` (internal/tui/picker_test.go)
retypes with the tui tests.
**Files:** internal/domain/config.go; internal/heartbeat/heartbeat.go; internal/heartbeat/heartbeat_test.go; internal/tui/effort.go; internal/tui/heartbeat.go; internal/tui/tui.go; internal/tui/doc.go; internal/tui/*_test.go; cmd/apogee/wire_server.go; cmd/apogee/delegation.go; cmd/apogee/wire_firing.go; cmd/apogee/schedule.go; cmd/apogee/upstream_test.go; cmd/apogee/delegation_test.go; cmd/apogee/wire_firing_test.go; cmd/apogee/dial_test.go; cmd/apogee/wire_server_test.go; cmd/apogee/wire_boot_test.go
**Read first:** internal/heartbeat/heartbeat.go — Beat, ModelSummary, Monitor.Beat; internal/tui/heartbeat.go — heartbeatState, effortExcluded, effortSupport; internal/tui/tui.go — ServerHost.Rebind;
cmd/apogee/wire_server.go — serverHost.Rebind; internal/agent/wire.go — toProviderDialect, toDomainDialect; internal/domain/config.go — EffortDialect;
cmd/apogee/delegation.go — resolveDelegationTarget; cmd/apogee/wire_firing.go — firingConfig
**Tests:** existing heartbeat and tui effort tests, retyped; a heartbeat test pins the provider→domain conversion
field by field.
**Acceptance:**
- `go vet ./internal/domain/ ./internal/heartbeat/ ./internal/tui/ ./cmd/apogee/`
- `test "$(go list -f '{{join .Imports "\n"}}' ./internal/tui | grep -c 'internal/provider$')" = 0`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/heartbeat/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Effort|Heartbeat|Rebind' ./internal/tui/`
**Commit:** `refactor(tui): read effort capabilities through internal/domain`

## 8. An import guard keeps internal/tui production code off internal/provider

**What:**
**Goal:** A go/parser test in `internal/tui` fails when any non-test `.go` file in `internal/tui` imports
`internal/provider`; ADR 0024's rule line says the rule covers production files and that the live tests' test-only
import is allowed.
**Approach (assumed at the header base):** model the test on `TestWebhookPackageImportsOnlyDomainAndSecurityFromApogee`
(`internal/webhook/webhook_test.go`); add a dated note under ADR 0024's tui import rule.
**Regression guard.** The guard is named `TestTUIImportsNoProvider` and the Acceptance `-run` matches it (an
unmatched `-run` passes vacuously). The hand bite check adds a blank import —
`_ "github.com/airiclenz/apogee/internal/provider"` — to a non-test file (a named unused import fails compilation before
the guard runs), then reverts it.
**Files:** internal/tui/imports_guard_test.go; docs/adr/0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md
**Read first:** internal/webhook/webhook_test.go — TestWebhookPackageImportsOnlyDomainAndSecurityFromApogee, isStandardLibrary; internal/tui/seams_guard_test.go — packageSeams (go/parser over non-test files);
docs/adr/0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md — decision 5 "never internal/provider" rule; internal/tui/altscreen_windows.go — build-tagged file the parser walk must still read
**Tests:** the guard itself, `TestTUIImportsNoProvider`; it fails when a blank scratch import of `internal/provider` is
added to a non-test file (check by hand, then revert).
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'ImportsGuard|ImportsNo' ./internal/tui/`
Depends on item 7.
**Commit:** `test(tui): guard internal/tui production code off internal/provider`

## 9. ADR notes for the removed Mechanism layer, ADR 0075's package name, a stale plan status

**What:**
**Goal:** ADRs 0018, 0023, 0024, 0026, 0028, 0033, 0034, 0044, 0055, 0057, 0061, 0064, 0065 and 0072 each carry, as the
first line after the closing front-matter `---`, `> Note (2026-10-02): the Mechanism layer this ADR refers to was
removed; ADR 0076 (Reactions) replaces it. Read the Mechanism-specific parts as historical.` — bodies otherwise
untouched; ADR 0034 also carries `> Headless has since shipped (\`apogee headless\`, ADR 0075).`. ADR 0075 names
`internal/reactions` wherever it named `internal/hooks`, with its file:line references re-checked against
`internal/reactions`. `docs/plans/archived/2026-10-02 - 01 - architecture-review-9-to-20-plan.md` line 7 reads
`**Status:** done — all 23 items done (#20c dropped by owner decision)`.
**Approach (assumed at the header base):** verified at write time: `internal/mechanisms`, `internal/hooks`,
`internal/validated` are gone; the ADR front matter is `---` / `Status: …` / `---`.
**Regression guard.** The ADR front matter is not one fixed shape: ADR 0072's is four lines (`---` / `Status:` /
`Amends:` / `---`), so each note goes directly after the SECOND `---` line of its file, never at a fixed line number
(`sed 3a` would land inside 0072's front matter); the Approach's three-line claim does not hold for 0072.
**Files:** docs/adr/0018-*.md; docs/adr/0023-*.md; docs/adr/0024-*.md; docs/adr/0026-*.md; docs/adr/0028-*.md; docs/adr/0033-*.md; docs/adr/0034-*.md; docs/adr/0044-*.md; docs/adr/0055-*.md; docs/adr/0057-*.md; docs/adr/0061-*.md; docs/adr/0064-*.md; docs/adr/0065-*.md; docs/adr/0072-*.md; docs/adr/0075-*.md; docs/plans/archived/2026-10-02 - 01 - architecture-review-9-to-20-plan.md
**Read first:** docs/adr/0072-*.md — front matter with Amends:; docs/adr/0075-the-headless-event-stream-is-a-versioned-driver-protocol.md — internal/hooks refs (match.go, runner.go line cites); internal/reactions — match.go, runner.go;
docs/adr/0034-*.md — headless deferral line; docs/plans/archived/2026-10-02 - 01 - architecture-review-9-to-20-plan.md — Status line 7
**Tests:** none (docs).
**Acceptance:**
- `grep -l 'Note (2026-10-02): the Mechanism layer' docs/adr/*.md | wc -l` prints 14
- `! grep -n 'internal/hooks' docs/adr/0075-*.md`
- `sed -n 7p "docs/plans/archived/2026-10-02 - 01 - architecture-review-9-to-20-plan.md" | grep -q 'done — all 23'`
Depends on item 8.
**Commit:** `docs(adr): mark the removed Mechanism layer in the ADRs that name it`

## 10. Verify, then fix: ADR conflicts on /undo and WritablePaths

**What:**
**Goal:** ADRs 0088, 0074 and 0086 agree with the code on what `/undo` reverts (files only, or the conversation too);
ADR 0049's "the field still has no writer" agrees with the code and with ADRs 0056 / 0012 on `WritablePaths`. The ADR
the code proves wrong gets a dated note (accepted bodies are amended by note, not rewritten).
**Approach (assumed at the header base):** read `internal/undo` and the `/undo` command handler; find production
writers with `grep -rn 'WritablePaths' --include=*.go internal cmd | grep -v _test`. Name the evidence (path, symbol) in
each note.
**Regression guard.** each added ADR note opens with the literal `Note (2026-10-02, verified against code):`, and the acceptance greps that marker instead of the bare date (ADR 0012 already carries a 2026-10-02 date).
**Files:** docs/adr/0088-*.md; docs/adr/0074-*.md; docs/adr/0086-*.md; docs/adr/0049-*.md; docs/adr/0056-*.md; docs/adr/0012-*.md
**Read first:** internal/tui/undo.go — previewUndo, confirmUndo; internal/agent/agent.go — UndoRevert, RedoRevert; internal/undo/doc.go — package doc (files only, no conversation);
internal/domain/confinement.go — Config.ConfinementBox (writes box.WritablePaths = ConfineWritablePaths ∪ ScratchDir); internal/domain/config.go — ConfineWritablePaths (no production writer: no config key in internal/config);
docs/adr/0088-cancel-settles-and-never-rewinds-finished-work.md — decision paragraph; docs/adr/0049-an-approved-write-escape-executes-through-a-permit-pinned-to-the-disclosed-target.md — §3
**Tests:** none (docs).
**Acceptance:**
- `grep -n 'Note (2026-10-02, verified against code):' docs/adr/0088-*.md docs/adr/0074-*.md docs/adr/0086-*.md docs/adr/0049-*.md docs/adr/0056-*.md docs/adr/0012-*.md` lists each note added
Depends on item 9.
**Commit:** `docs(adr): reconcile the /undo and WritablePaths claims with the code`

## 11. Confinement contract: narrow the permit row, then fix its stale claims

**What:**
**Goal:** The §11 `subprocessPermitCtxKey` row says the permit door covers hooks and `advise:` / `gate:` reactions only,
and that observe-lane `run:` commands spawn through `internal/userexec` `Run` with no permit and outside confinement
(matching `docs/manual/reactions.md`'s `run:` passage). After checking each against the code: the "both backends"
sentence (§2) and "only `write_file` is a writer" (§3) state the current backend count and writer set; the §7
`WritablePaths` recommendation and its "Implemented 2026-09-15" note no longer contradict; `confinetest` is not both
"pending" and "done"; the residuals agree with ADR 0081 §4 (fix whichever the code proves wrong).
**Approach (assumed at the header base):** evidence sites: `internal/reactions/command.go` (observe `run:`),
`internal/userexec/userexec.go` `Run`, `internal/tools/exec_common.go` (permit refusal), `internal/agent/dispatch.go`
`syncPermitCtx` (only minting), `internal/platform` confiner files, `internal/platform/confinetest`.
**Regression guard.** `tools.RunHookSubprocess`'s one production caller is `runSyncArgv`
(internal/agent/syncexec.go) and no hook spawns through it, so the row says the permit covers only the sync lane's argv
`advise:` / `gate:` reactions — not hooks — and §10.5's "`runSyncArgv` … is its second caller" sentence is fixed in the
same edit. The `confinetest` "pending"/"done" clause is dropped (no "pending" site exists at base; the only candidate,
contract "P3.2 lands it", falls under the stale-claims check); every other Goal clause has an Acceptance grep naming its site.
**Files:** docs/design/confinement-execution-contract.md; docs/adr/0081-linux-falls-back-to-a-namespace-fence-through-bwrap.md
**Read first:** docs/design/confinement-execution-contract.md — §11 table subprocessPermitCtxKey row, §10.4 Scope, §10.5, §3.3 Who carries it, §7 Implemented 2026-09-15 note, §2.3 intro; internal/agent/syncexec.go — runSyncArgv; internal/agent/dispatch.go — syncPermitCtx;
internal/reactions/command.go — commandExecutor.Run; internal/tools/exec_common.go — RunHookSubprocess, errNoSubprocessPermit; docs/manual/reactions.md — Running a command, Outside confinement;
docs/adr/0081-linux-falls-back-to-a-namespace-fence-through-bwrap.md — §4 residual amendment
**Tests:** none (docs).
**Acceptance:**
- `grep -n 'subprocessPermitCtxKey' docs/design/confinement-execution-contract.md` — the row names `userexec`
- `! grep -n 'only `write_file` is a writer' docs/design/confinement-execution-contract.md`
- `! grep -n 'is its second caller' docs/design/confinement-execution-contract.md`
- `grep -n 'Both backends\|two backends' docs/design/confinement-execution-contract.md` — every hit states the current backend count
- `grep -n -A3 'Implemented 2026-09-15' docs/design/confinement-execution-contract.md` — the note agrees with the §7 `WritablePaths` recommendation above it
- `grep -n -i 'residual' docs/design/confinement-execution-contract.md docs/adr/0081-linux-falls-back-to-a-namespace-fence-through-bwrap.md` — the two residual lists agree
Depends on items 6 and 10.
**Commit:** `docs(design): narrow the permit door and correct stale confinement-contract claims`

## 12. Verify, then fix: design-doc drift and the AGENTS.md design index

**What:**
**Goal:** `docs/design/mcp-client.md` names a live owning ADR, not the superseded ADR 0004;
`reaction-core-greenfield.md` cites files and symbols that exist (or carries a dated historical note);
`hook-talkback-findings.md` opens with a note that ADR 0076 overtook it; `test-drivers.md` has an accurate status
label, one test count, and one name for program lookup (the code's current symbol); `tool-surface-findings.md` records
Consoles as shipped; `AGENTS.md`'s `docs/design/` bullet lists `workflow-bench-experiment.md`.
**Approach (assumed at the header base):** grep each cited symbol (`EnableMechanisms`, `domain/hooks.go`,
`openerLookPath`, `ResolveProgram`, Console tools) before editing; count tests with `grep -c '^func Test'` on the files
the doc covers.
**Regression guard.** add `Depends on item 4.` (docs/design/test-drivers.md is parsed by an internal/stubllm test) and add that stubllm test to the item's Acceptance.
`TestDesignDocExampleScriptParses` / `TestDesignDocCapturesExampleParses` cut at the FIRST `## stubllm` / `### Captures`:
keep both headings and their first yaml fences intact and never quote either heading string above them. The test count
is the `TestE2E` count — `grep -h '^func TestE2E' cmd/apogee/*_test.go | wc -l`, not `grep -c '^func Test'` — and the
121.7 s / 89.3 s timings (and "thirty-six") stay labelled as the dated 2026-08-28 measurement; never re-run it (a
whole-package race E2E run breaks the machine rule).
**Files:** docs/design/mcp-client.md; docs/design/reaction-core-greenfield.md; docs/design/hook-talkback-findings.md; docs/design/test-drivers.md; docs/design/tool-surface-findings.md; AGENTS.md
**Read first:** docs/design/test-drivers.md — status line, "## Gates and budgets", openerLookPath passage under "### In-process Driver"; internal/stubllm/script_test.go — designDocExample, TestDesignDocExampleScriptParses, TestDesignDocCapturesExampleParses;
cmd/apogee/wire_present.go — openerLookPath; internal/present/opener.go — Opener.LookPath, security.ResolveProgram call; docs/design/mcp-client.md — Owner ADRs header;
docs/adr/0004-auto-mode-requires-os-level-confinement.md — Status superseded by 0012; AGENTS.md — docs/design/ bullet
**Tests:** none added; `internal/stubllm`'s `TestDesignDoc*` parse `test-drivers.md` and stay green.
**Acceptance:**
- `! grep -n 'EnableMechanisms' docs/design/reaction-core-greenfield.md` or the doc carries a dated historical note
- `grep -n 'workflow-bench-experiment' AGENTS.md`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestDesignDoc' ./internal/stubllm/`
Depends on item 4.
**Commit:** `docs(design): correct design-doc drift and index workflow-bench-experiment`

## 13. Verify, then fix: CONTEXT.md contradictions

**What:**
**Goal:** `CONTEXT.md` states one direction for the dangerous guard (tighten-only vs add-or-remove), one nested-structure
count matching `docs/layout/settings-screen-layout.md` and the code, a `ui.*` key count matching `domain.UIPrefs`
(including `cursor-shape`), and a Driver list that includes the daemon — each per the code.
**Approach (assumed at the header base):** read the dangerous-guard merge in `internal/config`, `domain.UIPrefs`, and
the settings external-edit handling before editing; if `settings-screen-layout.md` is the wrong one, fix it instead.
**Regression guard.** `cursor-shape` is a top-level key (internal/config/registry.go), not a `domain.UIPrefs`
field: the `ui.*` count is the ui.* registry rows = UIPrefs fields (ten), `cursor-shape` outside it — the Goal's
"(including `cursor-shape`)" is dropped. The dangerous-guard merge is `security.MergeDangerousRules`
(internal/security/rules.go), not internal/config, and no config key calls it: the fix states the guard is tighten-only
and not user-configurable today, the add/remove split being the ADR 0012 seam's rule — never wire it here (apogee-089).
**Files:** CONTEXT.md; docs/layout/settings-screen-layout.md
**Read first:** CONTEXT.md — Driver, Dangerous-action guard, Confine-to-workspace and settings-pane entries; internal/security/rules.go — MergeDangerousRules; internal/security/dangerous.go — Rule.ID comment;
internal/domain/uiprefs.go — UIPrefs; internal/config/registry.go — ui.* rows, cursor-shape row, KindStructured rows; docs/layout/settings-screen-layout.md — "The external edit"
**Tests:** none (docs).
**Acceptance:** `git diff --stat HEAD~1 -- CONTEXT.md` shows the edit; each corrected sentence cites its code symbol in the commit body.
Depends on item 3.
**Commit:** `docs(context): reconcile CONTEXT.md's guard, settings and driver claims with the code`

## 14. Verify, then fix: layout docs

**What:**
**Goal:** `layout.md` states one rule for which panes may open mid-run, per the tui code; `docs/layout/tool-layout.md`
uses current role names and current preview wording; `docs/layout/user-questions-layout.md` no longer points at
llama-launcher.
**Approach (assumed at the header base):** check the mid-run pane gate and the tool-row roles in `internal/tui`.
**Files:** layout.md; docs/layout/tool-layout.md; docs/layout/user-questions-layout.md
**Read first:** internal/tui/autocomplete.go — idleOnlyTag, commandSuggestions; internal/tui/commandrun.go — queueCommand; internal/tui/workflows.go — workflowAnswerNotIdle; internal/scheme/scheme.go — Scheme role fields (tool-marker, tool-leader, success);
docs/layout/tool-layout.md — "Rules", "Vocabulary", "Grouped Sub-agents" headings (cited by comments in internal/tui/transcript_test.go and subagentblock_test.go — keep them);
docs/layout/user-questions-layout.md — line 8 llama-launcher pointer
**Tests:** none (docs).
**Acceptance:** `! grep -n 'llama-launcher' docs/layout/user-questions-layout.md`
**Commit:** `docs(layout): reconcile layout docs with the tui`

## 15. Verify, then fix: manual cross-doc conflicts and the README profile line

**What:**
**Goal:** The Moment count in `docs/manual/README.md` and `configuration.md` agrees with `reactions.md`'s notice count
and the code (if they count different things, each says what it counts); `configuration.md`'s "`server:` never moves on
a re-read" and `commands.md`'s settings-row `server:` passage agree with the code; `headless.md`'s `data.cancelled`
row matches the encoder; `building.md`'s closing note and benchmark numbers are dated; `README.md`'s built-in profile
sentence names the shipped patterns `minimax-m3` and `qwen3.8` instead of "MiniMax and Qwen" (verified).
**Approach (assumed at the header base):** evidence: the Moment / notice enumerations in `internal/reactions`, the
`server:` re-read path in `internal/config` / settings, `internal/eventjson` (or the headless encoder),
`internal/profiles/shipped.go`.
**Regression guard.** `TestManualStatesTheMomentCount` (internal/domain/reaction_test.go) pins
`docs/manual/README.md`'s first "the <n> Moments" to seams + notices: the manual index keeps "the sixteen Moments"
(`allSeams` + `allNotices`, internal/domain/reaction.go), while reactions.md and configuration.md count the eleven
notices — each says what it counts. End state for README.md: its built-in profile sentence names `minimax-m3` and
`qwen3.8`. Evidence: the Moment enumeration is `internal/domain/reaction.go` (`Seams` / `Notices`), not
`internal/reactions`; the `server:` re-read path is `cmd/apogee/settingsedit.go` `settingKeyServer`, which also exempts
`sub-agents-server:` — check configuration.md's "the one ordinary key a re-read never moves" against it.
**Files:** docs/manual/README.md; docs/manual/configuration.md; docs/manual/reactions.md; docs/manual/commands.md; docs/manual/headless.md; docs/manual/building.md; README.md
**Read first:** internal/domain/reaction.go — allSeams, allNotices, Seams, Notices; internal/domain/reaction_test.go — TestManualStatesTheMomentCount, manualMomentCount; cmd/apogee/settingsedit.go — settingKeyServer, externalEdit.changed;
internal/eventjson/encode.go — subAgentPhaseData.Cancelled; cmd/apogee/docs_eventlines_test.go — TestManualListsEveryEventLineKind; internal/profiles/shipped.go — Pattern "minimax-m3", "qwen3.8";
internal/tools/manual_drift_test.go — TestReadmeStatesTheToolCounts; internal/config/reactions_test.go — TestReadmeStatesTheFloorGuardCount
**Tests:** none added; the README and manual drift tests below stay green.
**Acceptance:**
- `grep -n 'minimax-m3' README.md`
- `GOMEMLIMIT=2GiB go test -count=1 -run TestManualStatesTheMomentCount ./internal/domain/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestReadmeStates' ./internal/tools/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestReadmeStatesTheFloorGuardCount' ./internal/config/`
Depends on items 1 and 6.
**Commit:** `docs(manual): reconcile manual cross-references with the code`

## 16. The manual reads as present-tense reference

**What:**
**Goal:** `docs/manual/configuration.md` and `docs/manual/commands.md` describe current behaviour only — no
changelog framing ("used to", "no longer", "previously", "now" contrasting an old behaviour, "since <version/date>").
Exempt: "now" as the present moment of a user action, and migration notes for an old key spelling still accepted
(`internal/config/configmigrate.go`).
**Approach (assumed at the header base):** grep-driven rewrite; each kept hit is justified in a NOTES line.
**Regression guard.** The exemption is a rule: verbatim program output (configuration.md's "The flag no longer
routes anything" is `subAgentsFlagNotice`'s stderr text, cmd/apogee/keymigrate.go, pinned by keymigrate_test.go),
present-tense conditionals (commands.md's "the moment the draft no longer does"), and upgrade notes for any retired key
`internal/config/configmigrate.go` still detects — migrated or refused (the top-level `llama-launcher:` is refused, not
"still accepted"). configuration.md feeds drift tests a prose rewrite can break (backticked keys, tool names, env vars,
"Both lists are live."), so the Acceptance runs them (narrow `-run`, no `-race` on cmd/apogee).
**Files:** docs/manual/configuration.md; docs/manual/commands.md
**Read first:** docs/manual/configuration.md — Environment overrides section, url-safety prose, sub-agents flag notice block; cmd/apogee/keymigrate.go — subAgentsFlagNotice; cmd/apogee/keymigrate_test.go — TestSubAgentsFlagNoticeNamesTheEntriesAndTheReplacement;
cmd/apogee/docs_settings_test.go — TestManualDocumentsEverySettingsKey; cmd/apogee/docs_env_test.go — TestManualListsEveryEnvironmentOverride, TestDocsEnvURLSafetyProseIsLiveAndCoversMCP;
internal/tools/manual_drift_test.go — TestManualListsEveryKnownToolName; internal/config/configmigrate.go — migrateLegacyConfig, retired llama-launcher block
**Tests:** none added; the manual drift tests below stay green.
**Acceptance:**
- `grep -nE '\b(used to|no longer|previously)\b|\bsince v?[0-9]' docs/manual/configuration.md docs/manual/commands.md` — every remaining hit is exempt
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestManual|TestDocsEnvURLSafety|TestSubAgentsFlagNotice' ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -count=1 -run TestManualListsEveryKnownToolName ./internal/tools/`
Depends on item 15.
**Commit:** `docs(manual): state configuration and commands in the present tense`

## 17. Bookkeeping: architecture review status, hostile-bytes plan, .beads README

**What:**
**Goal:** `docs/reviews/architecture-review-2026-09-30.html` shows #9–#17 as Landed, each matched to a done item of the
archived `2026-10-02 - 01` plan; `docs/plans/archived/2026-08-11 - 06 - hostile-bytes-hardening-plan.md`'s status and
item markers reflect what `git log` shows landed; `.beads/README.md` is a short apogee-specific note pointing at
`AGENTS.md`'s beads rules (bd usage, spoken ids, the `issues.jsonl` export).
**Approach (assumed at the header base):** verify each review entry and each hostile-bytes item against `git log --oneline --grep`.
**Regression guard.** The archived `2026-10-02 - 01` plan also landed #20 (20a/b/d/e/f; 20c dropped), and #11,
#12 (12a only) and #17 landed partially (its Out of scope): badge #9–#17 and #20
`Landed · plan 2026-10-02 - 01 - architecture-review-9-to-20`, with a residue note on #11, #12, #17 and #20 as #7's
"— three host types remain" does; each nav link #c9–#c17 and #c20 carries the `✓ landed` marker (that plan's item 1 precedent).
**Files:** docs/reviews/architecture-review-2026-09-30.html; docs/plans/archived/2026-08-11 - 06 - hostile-bytes-hardening-plan.md; .beads/README.md
**Read first:** docs/reviews/architecture-review-2026-09-30.html — nav links #c9–#c20, articles c9–c20 badge rows, c7 residue badge, #landed ledger; docs/plans/archived/2026-10-02 - 01 - architecture-review-9-to-20-plan.md — ratified calls (#12 12a only, #20c dropped), Out of scope, item 1 (badge format);
docs/plans/archived/2026-08-11 - 06 - hostile-bytes-hardening-plan.md — Date/Status line, items 1–20; .beads/README.md;
AGENTS.md — beads rules (issue register, spoken ids, issues.jsonl lag, pre-commit export on staged .beads paths)
**Tests:** none (docs).
**Acceptance:**
- `grep -c 'Landed · plan' docs/reviews/architecture-review-2026-09-30.html` prints 18 (8 at base)
- `grep -c '✓ landed' docs/reviews/architecture-review-2026-09-30.html` prints 18 (8 at base)
**Commit:** `docs: record landed review items and plan statuses`

## 18. Archived plans' stale "unexecuted" status lines

**What:**
**Goal:** Every `docs/plans/archived/*.md` whose status line says unexecuted and whose archiving commit subject
(`git log --diff-filter=A --format=%s -- <file>`) contains "completed" or "implemented" has that status line reading
`**Status:** done (status corrected 2026-10-02)`; no other line changes; files that fail the test stay as they are and
are listed in the commit body.
**Approach (assumed at the header base):** a throwaway shell loop in `$CLAUDE_JOB_DIR/tmp` or `mktemp -d`, not
committed; the two files items 9 and 17 own are skipped.
**Regression guard.** Many archived status lines share the line with other metadata
(`- **Date:** … · **Status:** unexecuted`, `… · **Base:** … · **Bead:** …`,
`**Status.** Finalized 2026-08-04, unexecuted. All design decisions —`): replace only the status value — the text after
`**Status:**` / `**Status.**` up to the next ` · ` or sentence end — with `done (status corrected 2026-10-02)`, keeping
the bullet and every other field on the line.
**Files:** docs/plans/archived/*.md
**Read first:** docs/plans/archived/2026-08-04 - 01 - prompt-recall-plan.md — Date·Status line; docs/plans/archived/2026-08-04 - 05 - tool-call-collapse-uniformity-plan.md — "**Status.** Finalized …, unexecuted." paragraph;
docs/plans/archived/2026-08-11 - 01 - grouped-sub-agent-display-plan.md — "saved, unexecuted"; docs/plans/archived/2026-10-02 - 01 - architecture-review-9-to-20-plan.md — owned by item 9, skip;
docs/plans/archived/2026-08-11 - 06 - hostile-bytes-hardening-plan.md — owned by item 17, skip
**Tests:** none (docs).
**Acceptance:**
- `git diff --numstat HEAD~1 -- docs/plans/archived | awk '$1!=1||$2!=1' | wc -l` prints 0
- `git diff -U0 HEAD~1 -- docs/plans/archived | grep '^-[^-]' | grep -v 'Status'` prints nothing
- `test "$(git diff -U0 HEAD~1 -- docs/plans/archived | grep -c '^-[^-].*\*\*\(Date\|Base\)')" = "$(git diff -U0 HEAD~1 -- docs/plans/archived | grep -c '^+[^+].*\*\*\(Date\|Base\)')"`
Depends on item 17.
**Commit:** `docs(plans): mark completed archived plans done`
