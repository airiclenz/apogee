# ISSUES — open defects and parked work

## Conventions

The single register of known issues and deliberately deferred work (it absorbed `TODO.md` on
2026-08-13). Two sections:

- **Open defects** — verified, unfixed problems, each with current file:line evidence.
- **Parked / deferred work** — work deferred by decision, each entry recording *enough* design
  that it is not re-derived when picked up. A deferral or a denial recorded here never becomes a
  silent drop.

This file holds OPEN work only. A resolved or executed item is REMOVED from it and recorded in
`CHANGELOG.md` (under `[Unreleased]` until a release is cut) — the changelog is the closed trail;
no closed-entries section, and no "done" narration, lives here. When a run leaves residuals, record
only the still-open, ACTIONABLE findings — a defect, or a concrete missing test or doc with
`file:line` evidence to act on. Narration of how an item's text and its landed change differ,
costs a plan already ratified, and cosmetic observations belong in the run's closing report (the
closeout commit message), never here; the work the run completed belongs in `CHANGELOG.md`.

- [ ] New/Open Items not handled yet <br>
- [P] Planned Items - if you add an item to an implementation plan, mark it with `P`


## Improvements / Ideas

- [ ] sub agent names are often not descriptive. the model seems to not set it at all (sub-agent name just showing input proompt) -> separate auto-name call for sub agents if enabled and name was not set? (grill)
- [ ] when many sub agents are running - the activity status often flickers back and forth between the different sub agents. the stati of the sub-agents need to be unified / merged (grill)
- [ ] all content print-out of any tool need to display line numbers. The write tool does not do that currently. Verify all tools that print file diffs.
- [ ] fold the five hand-rolled `LookPath` + fence pairs (`internal/tools/git.go:142`, `python_exec.go:239`, `run_tests.go:249`, `diagnostics.go:200`, `internal/present/opener.go:228`) onto `security.ResolveProgram`, so the resolver is the only exec entry rather than the newest of six — an architecture pass, deliberately out of scope of `docs/plans/2026-08-26 - 01`
- [ ] Navigating sub-agents is not as smooth as it could be. when "expanding" a sub agent, I'd like to open it "full screen" automatically jumping to the bottom/latest response - meaning that it is taking the session space fully (excluding prompt box and so on..). A button to navigate one level up needs to be added as well (grill)

## Open defects

### Effort detection and the effort picker — residuals deferred out of the 2026-08-25 run

**Status:** found 2026-08-25 at the close of the effort-detection/picker plan
(`docs/plans/archived/2026-08-25 - 03 - effort-detection-picker-plan.md`), deferred out of that run.

- [ ] **The enriched turn-error hint reaches only the kwargs dialect.** Every seam that raises it
  gates on `len(wire.ChatTemplateKwargs) > 0` — `internal/provider/client.go:263` and `:274`,
  `internal/provider/stream.go:82` and `:85` — and its text names that one field
  (`thinkingEffortHint`, `internal/provider/client.go:413`). A server that refuses a level on the
  `reasoning` or `openai` dialect (`internal/provider/wire.go:139`, `:143`) sends no
  `chat_template_kwargs` at all, so its rejection surfaces as a bare status with nothing naming
  `thinking.effort` or the `/effort` override — the case the hint exists for, on the two dialects
  this run added. Widening it means gating on "the request carried an effort intent" rather than on
  the kwargs map, and rewording the hint so it does not name one dialect's field.

- [ ] **The Agent's effort dialect has no construction seed.** `a.effortDialect`
  (`internal/agent/agent.go:171`) is written in exactly one place — `Rebind`
  (`internal/agent/rebind.go:261`) — and read into every request at `internal/agent/wire.go:48`. A
  Driver that builds an engine and never rebinds always sends the zero dialect, i.e. the historical
  `chat_template_kwargs` shape, whatever the bound server actually reads. The firing-block path is
  that Driver: `cmd/apogee/wire_firing.go:134` resolves a spec through `rebindSpecFor` and hands
  back an `apogee.Config`, which carries no dialect field for the construction path to state. The 2026-08-25 code audit (C-03,
  `docs/reviews/code-audit-2026-08-25.md`) rates this High as an ADR 0031 Driver-parity break and
  names the fix shape — carry the forced/observed dialect on `domain.Config` or the Spec, applied at
  construction; `run.Once` never rebinds. Deferred to its own design grill (see "Audit residue").

- [ ] **ADR 0060 D6's "the segment drops whole" is not what the footer does.** The ADR
  (`docs/adr/0060-effort-is-detected-passively-dialected-per-server-and-picked.md:101`) says the
  effort segment drops whole when the footer is too narrow, "like the other optional segments". The
  word is appended into `info` (`internal/tui/model.go:2589`, joined at `:2591`) and the narrow
  branch truncates `bodyIndent+info+offline` with an ellipsis (`internal/tui/model.go:2607`) — only
  the mode marker drops whole. A narrow footer therefore clips the effort word mid-word. Either the
  footer grows a drop rule for the segment, or D6 is amended to the truncation the left run of segments
  has always had.

- [ ] **A `/model` pick judges the override against the PREVIOUS model's level set.** `applyRebind`
  clears an override the newly bound model excludes by reading `m.effortSupport()`
  (`internal/tui/heartbeat.go:384`), which is the last landed beat's report
  (`internal/tui/heartbeat.go:145`). `bindPickedModel` calls it with no beat for the picked model
  (`internal/tui/picker.go:761`, which deliberately carries the outgoing `m.hb.observedDialect`
  through), so a pick into a model that excludes the live override is tested against the outgoing
  model's vocabulary and can leave the override standing until the next beat re-observes. The
  beat-driven switch is correct; only the pick path is blind.

- [ ] **The cleared-override note says "back to auto" even when a profile level sits underneath.**
  `effortClearedNote` (`internal/tui/heartbeat.go:418`) words every clear as "cleared; back to
  auto", but clearing the override hands resolution back to the profile's `thinking.effort:`, which
  the footer then renders as that level rather than as `auto` (`footerEffortLabel`, called at
  `internal/tui/model.go:2588`). Where a profile level is configured, the note names a state the
  session is not in.

- [ ] **`SetEffortOverride`'s doc still says "four levels".** `internal/agent/agent.go:734` reads
  "The four levels (domain.EffortOff … domain.EffortHigh) each stand until another call moves them"
  — stale since the vocabulary widened to the seven-name union (`internal/domain/config.go:452`,
  `EffortOff` … `EffortMax`).

- [ ] **`pickerKindCases()` no longer covers every overlay kind.** Its doc says it does
  (`internal/tui/picker_test.go:1711`), on the grounds that the filtered accept is one uniform
  mechanism rather than a `/model` feature, but the six arms it returns
  (`internal/tui/picker_test.go:1714`) gained no `pickerEffort` case when the effort picker landed
  (`internal/tui/picker.go:88`, offering arm at `:900`). The new kind's filtered-accept seam is
  unpinned.

---

### Delegate token-runaway residuals — deferred out of the 2026-08-26 run

**Status:** found 2026-08-26 at the close of the delegate token-runaway plan
(`docs/plans/archived/2026-08-26 - 00 - delegate-token-runaway-plan.md`), deferred out of that run.

- [ ] **A child's post-fold request ends on the assistant summary and no test pins it.** The
  mid-Exchange exception a child agent takes (`shouldAutoCompact`, `internal/agent/compact.go:190`)
  folds at a quiescent Turn boundary, so the request that follows ends on the fold's assistant
  summary with no user bridge behind it — the very shape the main loop's guard defers a fold to
  avoid, and which the emergency fold pays for with `overflowBridge`
  (`internal/agent/compact.go:258`). The path passes `assertRequestTemplateLegal`, but that helper
  checks only dangling tool results and role alternation
  (`internal/agent/overflowrecovery_test.go:87`) and never requires a trailing user/tool message, so
  what a strict local chat template does with an assistant-terminated request is untested here.

- [ ] **A child's EMPTY capped reply lost the reasoning-spend number.** `replyFault` tests the
  delegate rule first (`internal/agent/loop.go:528`), so at depth > 0 a `finish: length` reply with
  NO visible text takes `cappedDelegateReplyErrFmt` and never reaches `emptyReplyFault`
  (`internal/agent/loop.go:561`), whose `cappedReplyErrFmt` wording is the one that reports roughly
  what the reasoning burned (`internal/agent/loop.go:470`) — the diagnostic the 20,653-token
  incident motivated. A child's fault now names the ceiling without saying what was spent under it.

- [ ] **A resumed delegate row loses its cached share.** `wireEntry` carries a head's prompt and
  completion tokens only (`internal/tui/transcriptcodec.go:95-96`, written at `:410-411`, read back
  at `:517-518`), so `usageSubAgentRows` (`internal/tui/usage.go:211`) draws an empty cached cell for
  every delegate restored from a stored session even where its server reported a share — while the
  session record itself keeps the sum (`internal/session/store.go:147`). The structural guard test
  refuses the extra field without its own decision (`internal/tui/transcriptcodec_test.go:1150`), so
  widening the wire is the call to settle first.

### Safety-floor / subprocess-funnel residuals — deferred out of the 2026-08-26 run

**Status:** found 2026-08-26 at the close of the safety-floors / subprocess-funnel plan
(`docs/plans/archived/2026-08-26 - 01 - safety-floors-subprocess-funnel-plan.md`), deferred out of
that run.

- [ ] **The si-34 exec-site inventory does not name the hook door.** The package doc's inventory
  sentence — `ResolveProgram` is "the one entry a new exec site takes, so no site can acquire a
  program without also acquiring the judgement on it" (`internal/security/doc.go:113-115`) — never
  names `RunHookSubprocess` (`internal/tools/exec_common.go:468`) among the exec sites it covers,
  so the hook door reads as outside the inventory it now goes through.

- [ ] **The bare-name hook-resolution test never observes argv[0].**
  `TestRunHookSubprocessResolvesABareProgramNameToAnAbsolutePath`
  (`internal/tools/exec_common_test.go:490`) asserts only that a bare `sh` still resolves, runs and
  prints `ok`; nothing checks that argv[0] became the absolute program, so the test would pass green
  against the pre-fix code that started the child from the bare name.

- [ ] **Two docs still describe the git funnel's guarantee as the filter-driver refusal.** The
  funnel now refuses every command-valued repo-local key, but
  `docs/adr/0056-terminal-fail-fast-and-session-scratch.md:92-94` and
  `internal/agent/treesnapshot.go:105-107` both still enumerate "the repo-local filter-driver
  refusal" — the narrower pre-widening wording, in the two places the floor's hardening is spelled
  out in prose.

- [ ] **No test pins the settings editor's production fence root.**
  `newExternalEdit(w.opts, w.roots.workspace, os.Getenv)` (`cmd/apogee/wire_live.go:232`) is the one
  place the real fence root is seeded and no test reads it: seeding `""` there would still pass the
  whole suite green.

- [ ] **`WaitDelay` wired by `NewProcessTeardown` is inert on the MCP `Cmd`.** The teardown seam
  sets `cmd.WaitDelay` on every child (`internal/platform/teardown_unix.go:54`,
  `internal/platform/teardown_windows.go:53`) and advertises it as the post-exit drain bound
  (`internal/platform/teardown.go:85-90`), but the MCP `Cmd` is deliberately built on
  `context.Background()` (`internal/mcp/transport.go:149`), where that delay never fires — a stdio
  server holding a pipe open can still block `session.Close`'s `Wait`, so the seam's advertised
  drain bound does not apply to MCP.

- [ ] **The `ResidualNotice` prints have no test.** `probe.ResidualNotice` reaches a stream at
  `cmd/apogee/headless.go:315` and `cmd/apogee/daemon.go:230`; on an ABI ≥ 3 host the notice is
  empty, so only the negative branch is exercised and nothing proves the disclosure ever reaches
  the stream.

- [ ] **The `rmFlag` token loses two spellings the old patterns caught.** `rmFlag` requires a letter
  after the dash (`internal/security/rules.go:54`), so the `rm -rf` rules built from it
  (`internal/security/rules.go:86-98`) no longer match `rm - -rf /etc` — really destructive, since
  getopt permutes the `-rf` — nor the harmless `rm rf /etc`. Restoring a bare `- ` as a flag token
  is the design call to settle first.

- [ ] **The `~/.apogee` rule's Hint still opens with a refusal.** "a terminal command is refused
  whenever its text names ~/.apogee" (`internal/security/rules.go:180`) was written when the rule
  refused; at Tier 2 such a command is now put to the human, so the first clause reads stale both on
  the approval prompt and in the deny result's tail.

- [ ] **`refuseAbsurdObjectCount` scans raw bytes.** The check searches the whole file for `/Size`
  (`internal/doctext/pdf.go:378`, called at `:168`), so a compressed stream that happens to contain
  `/Size <huge>` would refuse a valid document; no fixture reproduces it.

### Untrusted-text / approval-integrity residuals — deferred out of the 2026-08-26 run

**Status:** found 2026-08-26 at the close of the untrusted-text / approval-integrity plan
(`docs/plans/archived/2026-08-26 - 02 - untrusted-text-approval-integrity-plan.md`), deferred out
of that run.

- [ ] **`filehint`'s ID→tool gate needs a non-empty native tool-call ID.** The mechanism now parses
  a result only when its call ID maps to a listing tool, but nothing synthesises an ID for a NATIVE
  call: `internal/processing/toolcall.go` carries through whatever the backend sent, and the one
  synthesis site is the text-parser path (`internal/agent/loop.go:591`). A backend that emits empty
  `id`s therefore silently stops firing `filehint` — the safe direction, and the Bypass floor is
  intact, but the Mechanism is off with no signal. A fallback that would restore it without
  reopening C-08: parse ID-less results only when every call in the opening turn is a listing tool.

- [ ] **The fan-out path resolves without the colliding-key check.** `prepareDelegation`
  (`internal/agent/dispatch.go:237`) does not run the case-collision refusal the serial path gained,
  so a `sub_agent` call spelling both `task` and `Task` is refused when dispatched serially and runs
  when the group fans out. F-11's security property holds — a delegation reaches only Delegate or
  Refuse, never a Gate, so no approval surface is exposed — but the two dispatch paths disagree on
  the same call.

- [ ] **`renderFileGroup`'s context-row form is unexercised.** The `%s:%d-%s` context row is not
  covered by the escaping tests item 6 added: the 1-line fixture yields no surrounding lines, so
  only the match-row form is proven. It shares the one proven `display` binding, so the escape is
  believed correct — but a fixture with surrounding context lines is what would pin it.

- [ ] **The auto blast-radius row truncates at 80 columns.** The mode row's `auto` sub-list cell
  ellipsizes ("…fenced to the worksp…") at 80 columns and, when `auto` is the held value, the
  appended "(current)" marker is cut away entirely; the post-⏎ note clips too. The landed change
  fixed the ordering (sentence, then marker); a narrow-width treatment — marker first, or a wrapped
  row — was out of its scope and is the design call to settle.

### Surfaces-that-lie / restore-consoles residuals — deferred out of the 2026-08-26 run

**Status:** found 2026-08-26 at the close of the surfaces-that-lie / restore-consoles plan
(`docs/plans/archived/2026-08-26 - 03 - surfaces-that-lie-restore-consoles-plan.md`), deferred out
of that run.

- [ ] **`settingsPersistedValue` still answers `mode` from the journal.** The `mode` row now reads
  the live engine wherever a sub-list opens on it (`settingsCurrentValue`,
  `internal/tui/settings.go:1130`), but `settingsPersistedValue`
  (`internal/tui/settings.go:1000`) still answers from the pane's own edit journal
  (`internal/tui/settings.go:1001`). Only the reset path reads it — the one remaining stale-mode
  read left standing by the run.

- [ ] **ADR 0041 still says a watcher apply journals the ` *` marker.** Decision 8
  (`docs/adr/0041-the-config-file-is-watched.md:155`) and its decision-9 recap (`:165`) both state
  that a key the watcher applies journals its ` *` marker exactly as a pane edit does — superseded
  by the ` ~` marker this run gave watcher-sourced edits, and left unamended.

- [ ] **`internal/domain/doc.go:71` still counts seven `ToolSummary` variants.** The package tour
  says toolsummary.go carries "ToolSummary and its seven variants"; with `ChangedFiles` there are
  now eight.

- [ ] **A red slot and a ✓ can land on one delegation row.** A collapsed delegation's verdict is
  derived from the head's summary TEXT (`summary.failed = failedSummary(head.tool.Summary.Text)`,
  `internal/tui/subagentblock.go:595`), so a delegation that SUCCEEDED whose quoted one-line
  report happens to open `error: …` paints the slot red while `subAgentFinished`
  (`internal/tui/subagentblock.go:403`) grants it the done ✓ — the two answers the landed comment
  says are one.

- [ ] **A never-ran delegation whose refusal stays promoted wears no ▶.** At roughly 110 columns
  and wider the promote-guard keeps the one-line refusal in the outcome slot (`guardRefuses`,
  `internal/tui/toolleader.go:88`; the demote it would otherwise force is
  `internal/tui/subagentblock.go:261`), and the row then carries no expand indicator — so the
  prompt body `unframedSubAgentView` (`internal/tui/subagentblock.go:249`) lays out is unreachable
  at exactly those widths.

- [ ] **At 80 columns a long refusal clips the target off a never-ran delegation's collapsed row.**
  The header's target (`subAgentTarget`, `internal/tui/toolregistry.go:1037`) is cut away
  entirely by a long refusal in the slot and only reappears around 120 columns, so the narrowest
  terminals see neither the target nor the prompt.

- [ ] **`loop.go` dispatches native tool calls unfiltered — the probe is now stricter than the
  loop.** `probe model` no longer counts a `tool_calls` entry missing a name or an id as native
  evidence, but `assembleResponse` (`internal/agent/loop.go:582`; the pass-through is
  `calls := nativeCalls` at `:584`) applies no such filter, so a real id-less native call still
  runs and sends back a tool result whose `tool_call_id` drops off the wire.

### Read-fence / egress / docs-truth residuals — deferred out of the 2026-08-26 run

**Status:** found 2026-08-26 at the close of the read-fence / egress / docs plan
(`docs/plans/archived/2026-08-26 - 04 - read-fence-egress-docs-plan.md`), deferred out of that
run.

- [ ] **A dotfiles-symlinked `~/.apogee/skills` hands the model a path the read fence refuses.**
  The mount is now the RESOLVED library, but `Skill.Dir` (`internal/skills/load.go:307`) stays
  UNRESOLVED and is what the model is handed — the `files: %s` line and the `{{SKILL_DIR}}`
  substitution both use it (`internal/agent/loop.go:1160-1165`) — so reads through it are refused
  for an operator whose skills dir is a symlink. Fix: stamp `Skill.Dir` from the same resolved
  anchor the mount uses.

- [ ] **The Firing mount site has no test that would catch a revert to `SourceDirs`.** Reverting
  `cmd/apogee/wire_firing.go:229` from `skillProvider.ReadRoots` to `SourceDirs` leaves
  `go test ./cmd/apogee/ -run 'Firing|Schedule'` green — the fixture providers have no symlinked
  source, so nothing there separates the two. A planted symlink in those fixtures would close it.

- [ ] **The fail-closed proxy paths carry no committed test.** An unusable proxy value and an
  unpinnable proxy host both refuse the call, documented on `newHTTPClient`
  (`internal/tools/network.go:338`) and `vetEndpoint` (`internal/mcp/transport.go:201`); both were
  verified by hand only.

- [ ] **`--model`'s help still calls the startup server's `model:` a "hint".**
  `cmd/apogee/root.go:110-111` reads "the startup server's `model:` hint, else ask the server".
  The behaviour described is accurate, but the retired "hint" vocabulary survives in help text —
  exposed by this run's CONTEXT.md correction, where `model:` became a **trusted** id, never
  substituted.

### Quick-wins / posture / exhaustion / CI residuals — deferred out of the 2026-08-27 run

**Status:** found 2026-08-27 at the close of the quick-wins / posture / exhaustion / CI plan
(`docs/plans/archived/2026-08-26 - 05 - quick-wins-posture-exhaustion-ci-plan.md`), deferred out
of that run.

- [ ] **The JS/TS regex rule still misreports an arrow body and three other predecessors.**
  `regexOpeners` (`internal/mechanisms/syntaxengine.go:145-156`) admits `=` but not `>`, so
  `=> /re/` reads as division and the literal's quotes reopen the very "unclosed string" the rule
  was added to stop; a `+` predecessor and a `case` predecessor fail the same way (`return` is the
  one keyword the rule admits, `internal/mechanisms/syntaxengine.go:161`). Same Bypass-floor class
  as the C-02 case this run fixed: the `syntax` Mechanism retries correct code.

- [ ] **A closed regex literal leaves the preceding-rune state stale.** `checkBrackets` clears
  `inRegex` at `internal/mechanisms/syntaxengine.go:226` without refreshing `lastRune`, so in
  `foo(/a/ / 2)` the predecessor is still the `(` that opened the first literal and the second `/`
  opens a regex that swallows the `)`. Contrived — no realistic trigger found — but the state is
  wrong at the same seam as the entry above.

- [ ] **The manual documents the cap following a `/server` switch, never the `/load` case.**
  `docs/manual/configuration.md:466-474` ends on "a `/server` switch carries the new server's cap
  into the next firing". That sentence stays true; the `/load` arrival this run made re-follow the
  cap is simply unwritten.

- [ ] **No test exercises `/server`'s `switchServer` end-to-end for the cap.** With the follow
  moved into `sessionMover.move`, `switchServer` (`cmd/apogee/wire_verbs.go:132`) no longer
  follows the cap itself, and the switch seam's follow rests entirely on `move`'s coverage — a
  regression that stopped the switch path from reaching `move` would leave
  `go test ./cmd/apogee/` green.

## Parked / deferred work

Live, deliberately deferred work only. Each entry records *enough* design that we don't re-derive
it when we pick it up. When an entry closes, its body leaves this file for the authoritative record
(plan / ADR / CHANGELOG) and the close is noted in `CHANGELOG.md` — so a deferral trail never
becomes a silent drop, and the file never becomes an archive.

### apogee-code feature parity — user-facing affordances not yet ported

**Status:** parked 2026-06-25; most of the surface has since shipped (`CHANGELOG.md` and the
archived plans hold that record). Additive TUI/UX layers on top of the agent core, which is
already at parity. Scope is
*user-facing* parity with the apogee-code VS Code extension (`airic-lenz.apogee-code` v0.2.58)
only — the by-design Phase-4 items were tracked separately.

**Verification note (the source-of-truth correction):** apogee-code's `Apogee-Code-TDD.md`
claims it has *no slash commands, only `@file`*. **That doc is stale.** When porting, treat the
shipped webview (`~/.vscode/extensions/airic-lenz.apogee-code-0.2.58/media/chat.js`, array `Ws`)
as the behavioral oracle, not the TDD. On send the webview posts `{text, skillIds, fileRefs}`.

**Remaining:**

- **Server / model switching** — **every switch SHIPPED (2026-07-28 the two user-facing ones,
  2026-07-29 the local-server half, 2026-08-12 the profile half); the request-side remainder
  below was demoted from `[P1]` on 2026-08-14 (owner call).** The shipped bodies have
  left this file for their authoritative records
  ([ADR 0028](docs/adr/0028-a-server-switch-rehomes-the-session-and-the-first-beat-completes-it.md)
  for `/model` + `/server`,
  [ADR 0029](docs/adr/0029-the-launcher-actuates-local-servers-and-the-beat-completes-every-move.md)
  for the launcher,
  [ADR 0044](docs/adr/0044-model-profiles-are-per-model-and-mostly-shipped.md) for the profile).
  **Remaining:**
  - **Sampling params on the Model profile — deferred, demand-driven (owner call 2026-08-14):
    covered launch-side for launcher-managed servers.** apogee deliberately sends no sampling
    params ([ADR 0046](docs/adr/0046-the-engine-bounds-every-reply-with-an-output-cap.md)
    leaves temperature et al. unset), so the server's own defaults win — and for a
    llama-launcher-managed server those defaults ARE the Launch profile's flags (`--temp`,
    `--top-p`, …), which is the owner's whole workflow. Request-side knobs would matter only for
    an endpoint the launcher did not start (a remote OpenAI-compatible server, LM Studio, a cloud
    API) or for changing sampling mid-session without a model reload; neither need has appeared.
    Pick up only on a real ask; the grill it then needs must keep the request-side "Model profile"
    and launch-side "Launch profile" namespaces from colliding in the UX. Its one prerequisite is
    already discharged: the field-wise `SetSampling` merge landed 2026-08-13, so a profile that
    sets only temperature no longer nils the engine's stamped cap.
    [ADR 0025](docs/adr/0025-interjections-commit-at-the-between-steps-boundary.md) §6 still
    defers the user-after-tool wire risk to this layer by name.

**Related (parked below):** per-tool approval overrides (`toolApprovalOverrides`:
automatic/ask-first/excluded) — apogee-code surfaces this in config; apogee has the internal
disposition table but no user-facing override. See *Configurable tool × mode security matrix*.

**Standing rules the shipped work left behind:** the `model-profiles:` shape table grows one
entry per *sighting*, never per guess ([ADR 0044](docs/adr/0044-model-profiles-are-per-model-and-mostly-shipped.md)).
What has shipped out of this entry since it was parked is recorded in `CHANGELOG.md` and the
archived plans and ADRs it names — only the open remainder above lives here.

---

### Session system follow-ons — deliberately deferred from the 2026-07-24 session-system plan

**Status:** recorded 2026-07-24 at the session-system close-out
([ADR 0022](docs/adr/0022-sessions-persist-per-turn-as-dual-representation-records.md)). Named
out of scope in the plan and left for later — neither is a live gap.

- **[P2] Retention / pruning policy** — today the store never prunes: `~/.apogee/sessions/`
  grows unbounded and the only discard is the browser's `d` (manual delete). A retention policy
  (age- or count-based, opt-in via config) is deferred; the design intent is that pruning stays
  manual until there is a real need, so any auto-prune ships default-off.
- **[P2] Cross-instance file lock** — concurrent apogee instances are **last-write-wins per
  file**. Ids are per-instance unique, so a clobber requires the *same* session resumed into two
  instances at once; documented and accepted, not locked. A cross-instance lock (or a
  resumed-elsewhere detection) is the follow-on if that narrow case ever bites.

Explicitly **not** deferred (deliberate non-goals, so they are not re-opened as gaps):
session search, session export, sub-agent session persistence, and serializing
mode/approvals/confinement/MCP into the record (the last per ADR 0008). The bench keeps composing
`Snapshot`/`Encode` directly (ADR 0001) — no bench-side session-store TODO.

---

### Phase-4 mechanism catalogue — deliberately dropped / folded / deferred

**Status:** recorded 2026-07-04 at the Phase-4 close-out (`v1.2.0`). Every verdict below is
recorded in full — evidence, rationale, ledger row — in
`docs/design/mechanism-catalogue.md` (Table C, the port ledger, Table B); this entry is the
deferral trail's pointer, not the record. **None is a live gap**; revisit a verdict only if the
bench finds a specific win.

- **`codeinfo` — DROPPED (C7):** its specific missed-call-site signal is not significant
  (OR 0.69, p=0.32, gpt-oss-20b, N=75/arm).
- **`correct_tool_result` — DEFERRED, not dropped (owner-ratified 2026-07-04):** the sim defines
  no production trigger (lab-only, operator-supplied correction); the experimental
  post-tool-result hook is the lab surface. A bench-discovered trigger motivates a **new** plan
  item — the only path to porting.
- **`compress` external-client-compaction sniffing — DROPPED (C3):** apogee owns the loop, no
  external client to sniff. The surviving halves shipped: `tool_result_cap`, generative
  Compaction, `truncate_history`.
- **`intent` / `cot` / `feed_forward_correction` — FOLDED (C4/C5/C6):** absorbed into the inline
  classifier, the three completion nudges, and `validate`'s retry-in-place — no catalogue rows
  of their own.
- **Un-ported sim refinements — bench-pending (R2):** the off-ramps' retry-ladder refinements and
  `tool_loop_interceptor`'s count threshold + wall-clock cooldown; the loop's strikes-3
  self-regulation + `maxPostResponseRetries` substitute.

---

### Configurable tool × mode security matrix

**Status:** parked 2026-06-24 (Phase-3 grill). Post-v1, **additive** — config is additive,
so this is a minor bump, not a freeze break. Cross-reference: ADR 0049 (landed 2026-08-14) settled
the same dispatch-gate-vs-tool-level-fence seam — an approved out-of-workspace write executes
through a permit pinned to the disclosed target — and its floor constrains this design: the Gate is
the bound, an approval is final, and there is no hard-deny tier above it.

**The idea (owner, 2026-06-24):** let the user configure precisely how each tool behaves in
each mode — a `(tool × mode) → disposition` matrix surfaced in config.

**Why it's coherent:** the disposition table *already exists internally* — `needsApproval` /
D5 is exactly `(mode × tool-disposition) → {auto-run, confine, gate, deny}`. v1 ships it as an
explicit internal table; this feature would expose a *user-tunable* layer on top.

**The two constraints that make it safe + shippable (must hold when we build it):**

1. **Tighten-only (the law).** A user override may only make a cell **stricter** than its mode
   default (toward gate/deny) — **never looser**. Loosening a whole tool-class would silently
   dissolve a mode's guarantee (e.g. `terminal → Auto → allow, unconfined` reintroduces the
   "unsupervised *and* unbounded" hole ADR 0004/0012 forbid; `write_file → Plan → allow` breaks
   Plan's read-only promise). The **only** blanket loosen is `confine-to-workspace=false`, which
   is gated behind an explicit "I am the sandbox" acknowledgement. Narrow, explicit, opt-in
   loosens (a `NetworkAllow` host; a `terminal` command-pattern allowlist entry like `go build`,
   `npm test`) are fine — same shape as the per-project allowlist already in `ConfinementBox`.

2. **Freeze cost.** A per-tool×mode config block turns **every tool name into a frozen config
   key** and adds a sizable schema right at the `v1.0.0` cut (fights D7 — keep the v1 surface
   minimal). Deferring it past v1 avoids that; config additivity means it loses nothing by waiting.

**Related "approval-precision" knobs to design *together* with this (also parked):**
- **Command-pattern allowlist for `terminal`** — "auto-allow `go build` / `npm test`" without a
  prompt. This is the thing people usually *actually* want when they say "configure the tools";
  finer than tool-level. A narrow explicit loosen (constraint 1), so it's allowed.
- **Per-host `NetworkAllow` precision** — already a field on `ConfinementBox`; a UI/config layer
  to manage it per project belongs with the matrix.

**What v1 ships instead (so the deferral is safe):** the internal disposition table (D5) +
the `confine-to-workspace` flag (the one blanket loosen) + the existing narrow allowlists.

---

### An embedder cannot register a *vouched-for* network tool (export the network funnel?)

**Status:** parked 2026-07-25 (the url-safety choke-point plan, D5). Post-v1, **additive** (a new
exported constructor in `internal/tools` re-exported by the facade — a minor bump).

**The situation.** Apogee's own network tools carry the **unexported** url-filter marker, obtainable
only by embedding the network funnel that owns the `URLGuard` (`internal/tools/network.go`), and the
Auto ladder keys the "runs unattended" cell on that marker (ADR 0012 amendment 2026-07-25). So a
host-registered tool that reaches the network **gates in Auto no matter how carefully it filters its
own URLs** — it has no way to route through the funnel and no way to claim the marker. This is
deliberate and it exactly mirrors `workspaceScopedWriter`, under which an embedder's write tool has
gated in Auto since Phase 3: the marker is unfakeable *because* it is unexported, and gating is the
safe direction.

**The natural move if demand appears:** export the funnel as a public `NewNetworkTool`-style
constructor (host-supplied `URLGuard` in, an embeddable value out) so an embedder's tool **inherits**
the marker by actually routing every URL through the guard, rather than being handed a way to
*assert* it. The property to preserve: the marker must remain impossible to obtain without the
guard — never an exported interface, a declared capability, or a config-listed tool name, each of
which would let an under-declaring tool reach the network unfiltered and unattended.

**Why it waits:** the demand is hypothetical (no embedder has asked), the current answer is merely
an extra prompt rather than a broken tool, and the export is purely additive whenever we want it.

---

### A door left open by the Mechanism-registration collapse

**Status:** parked 2026-07-25 (`docs/plans/archived/2026-07-25 - 01 - mechanism-registration-collapse-plan.md`,
"Explicit non-goals" + D4; [ADR 0003](docs/adr/0003-mechanisms-are-a-constraint-declared-registry-not-a-fixed-pipeline.md)
amendment 2026-07-25). It was named out of scope by that plan and recorded here so the door is
documented rather than silently shut. **It is not a live gap.**

- **`internal/mechanisms` declares which `Deps` a row needs, but does not construct them.** A row now
  carries `needs DepNeeds`, and `DepsNeeded(ids)` ORs the flags for an enabled set, so the engine
  derives exactly the collaborators the enabled rows asked for and its build loop is uniform for every
  ID. The **construction** stays in `internal/agent`'s `deriveDeps` — the library store under
  `Config.LibraryDir`, the corrupt-store-degrades-to-empty stderr notice, and the
  `ResolveFingerprintFrom` identity ladder. Moving that wiring next to the row that needs it reads
  better and was considered, but it contradicts
  [ADR 0015](docs/adr/0015-catalogued-mechanisms-are-enabled-by-id-through-config.md) §2 (*"Deps stay
  internal; the engine derives them from Config"*) for no gain the declaration does not already give.
  Revisit only if a **second** `Deps`-bearing Mechanism arrives and the derivation genuinely wants to
  live beside its row — and treat it as an ADR 0015 amendment, not a refactor, because §2 is what
  makes "arming the library Mechanism against a different model than the loop runs" unrepresentable.

---

### `Request.InjectContext` placement — a `domain` data type encodes chat-template role-safety policy

**Status:** parked 2026-07-26 — carried out of the 2026-07-24 architecture-deepening review so that
review's ledger could close honestly. It is the review's own verdict that parks it, and the verdict
stands as written: ***Speculative* — it reopens an ADR-0010 line; flagged, NOT recommended without a
grill; the current placement is defensible.** So this is **not a code TODO**: no code change, no ADR
amendment, and no "obvious" move happens here until an owner grill (`grill-with-docs`) settles the
question. It is not a live gap either — no wrong output, no bug, no broken invariant; the ladder
below is doing its job. Full card:
[`docs/reviews/archived/2026-07-24 - 00 - architecture-deepening-review.md`](docs/reviews/archived/2026-07-24%20-%2000%20-%20architecture-deepening-review.md)
(the smaller-deepenings list, last entry).

**The question, in one line.** `Request.InjectContext` — a method on a `domain` **data type** —
encodes *chat-template role-safety policy*, while the engine / `internal/context` layer is where
role-alternation is otherwise owned. Which layer should own the placement decision?

**What the code does today** (`internal/domain/hooks.go:504–529`) — recorded so the grill starts from
behaviour rather than re-reading it. `InjectContext(text)` picks the landing spot with a three-branch
ladder:

1. history ends in a **tool result** → append to the system prompt instead (a user message after a
   tool result breaks strict chat templates);
2. history ends in an **assistant** message → append at the end (the retry-exchange shape: the
   correction answers the superseded assistant message it follows, R1);
3. otherwise → insert **before** the last Exchange's opening (`lastExchangeOpening`,
   interjection-aware — drifted since this entry was written, which recorded "before the last user
   message"; re-verified 2026-08-13) — plus the F2 boundary maintenance, since an
   insert below the frozen `committedLen` shifts every committed message right by one and the
   boundary must advance with it;

with no user message present at all, it appends at the end. Sibling for contrast: `AppendToSystem`
(same file, `:481`) is the marker-idempotent system-prompt inject; `InjectContext` carries **no**
marker of its own (noted at `internal/mechanisms/filehint.go:44`).

**The tension.** Strict-template alternation is otherwise the engine/context layer's business:
`internal/context/compact.go` (`:52`, `:105`, `:113`) shapes the folded history and the summarizer
call to keep clean alternation, and `internal/agent/compact.go:193` asserts alternation holds after
`Conversation.Replace`. Chat-template policy therefore has two homes — a `domain` value type and the
engine's reducers — which is the whole of the observation.

**What it reopens, i.e. why this is not a free move.**

- **An ADR-0010 public-surface line.** ADR 0010's canonical placement rule puts every public type in
  `internal/domain` with the root re-exporting it — `apogee.go:416` is `type Request = domain.Request`
  — and ADR 0010's *Stability* consequence states the public surface **is** the set of root aliases
  (ADR 0001 §18). `InjectContext` is a method on that aliased type and is a documented member of the
  hook mutation API (`docs/design/archived/hook-mutation-api.md` §3, the `Request` pre-request surface, and
  its §7 traceability table — a P0.1 draft, so read its line references as stale). Moving it
  is a **breaking public-surface change**, not a refactor — and ADR 0010's own lowest-layer rule
  ("a type lives at the lowest layer that can define it without importing upward", and pure logic
  intrinsic to those types lives with them) is simultaneously the strongest argument that the current
  home is *correct*, not accidental.
- **An ADR-0017 pairing.** ADR 0017 §1 explicitly routes `InjectContext` (and
  `conversationView.LastUser`) through the single Exchange-boundary derivation "with their public
  behaviour unchanged" — `internal/domain/exchange.go`'s `lastRoleIndex` is THE boundary core. And
  `exchange.go`'s header states the Exchange boundary is stable *because* `InjectContext` places
  injections before the last user message or in the system message, **never after it**. The placement
  policy and the boundary derivation are load-bearing for each other; any move must say what happens
  to that pairing.

**Blast radius, so it is known before the grill, not during.** Five non-test callers:
`internal/agent/loop.go:369` (the retry correction) and `:699` (the deferred-correction drain), and
the Mechanisms `readloop.go:89`, `filehint.go:111`, `guideddecomposition.go:159`. Several Mechanisms
additionally *reason about* where the inject lands — `guideddecomposition.go` (`:175`, `:196`,
`:373`, `:516`) keys its idempotency and boundary logic on the roles `InjectContext` writes — so the
ladder's behaviour is depended on beyond the call itself.

**What a grill has to settle (the branch points, so nothing is re-derived):**

- Is the three-branch ladder **policy** (belongs with whoever owns chat-template shape) or **pure
  logic intrinsic to the `Request` value** (ADR 0010's lowest-layer rule — the argument for today's
  home)? Everything else follows from this one.
- If it is policy: what is the public replacement, given `Request` is a root alias and the hook API
  documents the method? A seam the engine injects, versus keeping the method and moving only the
  *decision* behind it, are different-sized breaks.
- What happens to the ADR-0017 pairing — does the "never after the last user message" property stay
  guaranteed by construction, or does it degrade to a convention the boundary derivation merely hopes
  for?
- Is the honest outcome instead to **close** the question — an ADR 0010 (and/or 0017) note naming
  `domain` the deliberate owner of role-safety placement, so it stops being re-flagged at every
  architecture pass? A review that says "defensible" twice is evidence for this branch.

---

### Deferred security-review Lows (P3 `/security-review`, 2026-06-24)

Recorded so the deferral is deliberate, not a silent drop. Each is an INTENDED-design
acceptance or a future-task re-verification, NOT a live hole.

- **[L1] `MergeDangerousRules` tighten-only path is dead code (floor fixed by absence).** The
  project-config dangerous-rule merge (`security/rules.go`, `projectAdd` tighten-only) is never
  called — `guards` is always `NewDefaultGuards()` — so the "project cannot loosen the floor"
  property is currently true **by absence**, and the merge's tighten-only invariant lives only in
  `rules_test.go`. **Deferred** because there is nothing to fix today: when the project/global
  config merge is wired (the parked "configurable tool × mode matrix" / dangerous-rule config
  surfacing above), re-verify the project/global split end-to-end at that point. No change now.

- **[L3] Confined subprocess can read any host file + open network ⇒ exfiltration is in-design.**
  `platform/landlock_linux.go` handles only WRITE accesses (read/exec unrestricted) and the
  network is open by default. A confined Auto subprocess can `cat ~/.ssh/id_rsa` and POST it out.
  **Deferred — INTENDED per ADR 0012**: the box bounds *writes* (stops clobbering the host), the
  network is open by default, and `confine=false` is the only blanket loosen. Recorded as a
  conscious v1.0.0 acceptance. If read-confinement or default-deny egress is ever wanted it is an
  ADDITIVE box tightening (landlock read-handling + a per-host network filter), not a v1 change.

- **[L4 enhancement] Optional env-allowlist scrub for stdio MCP launches.** A configured stdio
  MCP server inherits Apogee's full process environment (all secrets) — see the trust note in
  `internal/mcp/transport.go`. This is **intended** (a trusted, host-configured launch), so v1
  documents the trust rather than scrubbing (a blanket scrub would break MCP servers needing
  inherited PATH/HOME/runtime vars). **Deferred — optional**: a future per-server `EnvAllowlist`
  (mirroring `safeGitEnv`) for a host that wants to run a less-trusted stdio MCP server. Additive,
  post-v1.

- **[L5 accepted cost, 2026-08-12] The exec fence refuses an in-repo virtualenv and
  `node_modules/.bin`, with no way to switch it off.** Every exec site now refuses an `argv[0]`
  resolving inside the workspace or a configured writable path
  (`security.RefuseExecFromWritablePath`, hostile-bytes plan item 2 — it closes the
  plant-then-exec chain: a confined call writes an executable inside its box, a later unconfined
  call runs it outside). `lookInterpreter` and `lookTestProgram` resolve against apogee's own
  inherited `PATH`, so the two most common Python and Node layouts — an activated
  `<repo>/.venv/bin/python3`, a `node_modules/.bin` entry ahead of the system ones — land inside
  the root and start refusing. That is the correct security answer and it is also a real
  regression for real developers. **Accepted as-is** (owner ratification, 2026-08-12): no config
  opt-in and no allow-list carve-out for the conventional tool directories, because a key here
  would be a new operator-armed footgun and a carve-out would name exactly the directories an
  attacker plants in. The refusal is made legible instead — it names the resolved path and the
  rule, so the venv is visibly the cause. Two alternatives were considered and rejected: keying
  the refusal on "resolved via `PATH` rather than an absolute config value" (a venv interpreter
  *is* found via `PATH`) and "was this file written during the session" (stateful and racy). If
  the cost proves unacceptable in practice, a config key is a small later addition.

- **[L6 accepted cost, 2026-08-12] A process a `terminal` call deliberately backgrounded is reaped
  when the call returns.** Process-group teardown used to run on cancellation only — `cmd.Cancel`
  was the single group kill — so a command that backgrounded a server left it running after a clean
  exit, and because `exec.ErrWaitDelay` is not an `*exec.ExitError` a wedged post-exit drain was
  reported as exit code 0. The teardown now also runs after a normal `Wait`, and an expired
  `WaitDelay` is surfaced on the result (hostile-bytes plan item 18). **Accepted as-is:** this is
  what the tool's own contract already promised — "one-shot, a fresh process per call, no persistent
  shell" (ADR 0008) — so it closes a gap between contract and code rather than choosing a new
  policy, and a descendant outliving its call is exactly the persistence primitive the audit named.
  The cost is that anyone who was relying on `terminal` to leave a dev server up now finds it gone,
  with no opt-out: a long-running process belongs outside the one-shot tool.

(L2 — the dangerous-action guard normalises only whitespace, case and `\`→`/` and is evaded by
deliberate OBFUSCATION — `eval`, variable expansion, `$'…'` quoting, encoded payloads — needs no
entry: it is ADR-0012 by-design, and `internal/security/doc.go` states the guard is "NOT a
security boundary." Everyday idiom — `--`, long flags, a quoted absolute target, an absolute
shell path after a pipe — IS covered (2026-08-26, code audit C-10); what stays out of scope is
the obfuscation chase, not the ordinary spelling.)

**Triage and closeout narration — relocated 2026-08-19** to
`docs/reviews/archived/2026-08-11 - 01 - external-audit-triage.md` (§ Addendum). The 2026-08-11
triage tested all 14 external-audit positions against L2/L3/L4 before any was accepted and not one
of them fell to those acceptances; the hostile-bytes batch's 2026-08-12 closeout left two residuals
documented at the code (`gitHardeningEnv`'s filter-driver note; `goVetEnv`'s `GOENV=off` note).

---

### Mid-Exchange auto-compaction for the MAIN loop (fire at Turn boundaries under budget pressure)

**Status:** parked 2026-07-05 (guided-decomposition grill); the CHILD half shipped 2026-08-26.
A child agent now folds at quiescent *Turn* boundaries under budget pressure
(`midExchangeCompaction` — `internal/agent/compact.go`, set by `newChildAgent`), because a
delegation's whole life is one Exchange and the boundary the trigger waits for never comes for it.
What stays parked is the same lift for the MAIN loop, where a long multi-Turn Exchange still has
no generative reducer available for its entire life — only `tool_result_cap` (default-off) can
reduce mid-Exchange there, and guided decomposition covers it with a descriptor `Requires` on
`tool_result_cap`. That change touches a structural reducer's contract on the very arm the bench
measures (it interacts with the saturation logic, the protected prefix, and bench comparability),
so it needs its own grill and bench evidence — deliberately not a rider on the decomposition work.

---

### Startup notices are stderr-only — the in-transcript banner

**Status:** extracted 2026-07-27 from the closed *Auto-mode confinement degradation* entry (its
one still-open residue) so the closed body could leave this file. The underlying parked idea is
the validated-set in-transcript banner — deferred follow-up 04 of the validated-set work
(`docs/plans/archived/validated-set-runtime-surface.md`: "no TUI in-transcript banner in v1,
revisit if the stderr line proves easy to miss").

Every startup notice (the validated-set notice, the Auto-confinement degradation notice) prints
pre-alt-screen on **stderr only**; `/confine status` renders the same facts in the transcript on
demand, but the startup moment does not. Folding startup notices into the transcript means
building the banner rendering the degradation plan deliberately did **not** build (its item 7
used the existing notice path and named follow-up 04 the owner of any framework). No wrong
behaviour — pick it up when a stderr notice proves easy to miss.

---

### Adaptive prompt complexity — request slimming driven by the capability tier

**Status:** parked 2026-07-22 by decision, not by omission
([ADR 0021](docs/adr/0021-probe-is-two-halves-the-host-report-is-free-the-model-battery-is-an-explicit-act.md)
Q3; `../apogee-sim/mission.md` item 2). Phase 5 ships the **capability tier** as a reported
`apogee probe model` field and stops there.

The idea: a pre-request transform that shapes the outgoing request to what the model can
actually digest — stripping tool descriptions down to names and one line, shortening the system
prompt, simplifying output formatting — selected by the tier the probe observed. It is the
mission's "prompt complexity tier" and aims squarely at the smallest models, the ones this
project exists for.

Why it is not built with the probe: this is model-facing behaviour inside the loop, i.e. a
**Mechanism** by definition, and a Mechanism earns its place on the non-inferiority gate against
Bypass, per model, with a catalogue row and a Table B bench-validation entry
([ADR 0009](docs/adr/0009-the-ab-decision-rule.md); the Phase-5 settled design: nothing
model-facing ships default-on without bench evidence). Shipping it alongside the probe would
mean either an unvalidated default-on transform or a catalogue row with a placeholder where its
evidence belongs. The tier signal costs nothing and is already there when the evidence is.

When picked up: catalogue it as a **pre-request** Mechanism, **default-off**, gated on a stored
probe record's tier (so it no-ops entirely for an un-probed model), and bench it on at least one
small model before any default flips. Open design questions kept warm: whether slimming applies
per-request or per-session (a mid-session change of tool descriptions is a history-consistency
question), and whether the tier or the individual battery findings (native tool calls vs. JSON
vs. multi-step) are the better gate — the findings are strictly more informative, the tier is
strictly easier to reason about.

---

### Host-override knob for the rendered instruction block

**Status:** extracted 2026-07-27 from the closed *system-prompt / template story* entry (its
Residual 1); originally the prompt-seam plan's D1 *rejected hybrid* (engine-owned won —
[ADR 0023](docs/adr/0023-the-system-prompt-is-a-configured-template-rendered-per-request.md)).
Additive-later, demand-driven.

The idea: a host (embedder) knob to override the engine-rendered instruction block. The
obligation the old entry attached to it is discharged: because the user's prompt, the mechanism
directives and the profile's tool block merge into **one** system message, a host-supplied block
would replace the rendered block *inside that same message* without reshaping anything ADR 0023
decided — the two compose; they do not fight. Build only if an embedder asks.

---

### A marker phrase in the standing system content suppresses that Mechanism's directive

**Status:** filed 2026-07-28, the entry
[ADR 0023](docs/adr/0023-the-system-prompt-is-a-configured-template-rendered-per-request.md)'s
Consequences asked for (that ADR carries the full record and the rejected alternatives);
[ADR 0026](docs/adr/0026-workspace-context-files-are-session-scoped-prompt-data.md) widened it to a
second source. **Accepted, not a defect to fix on sight** — revisit if a real prompt trips it.

`Request.AppendToSystem(marker, text)` is idempotent by `strings.Contains` over the FIRST system
message, and the markers are natural-language phrases — `decompose`'s `"Focus on one action"`,
`cot`'s `"have not read any files yet"`, `library`'s `"[Apogee context notes"`. That message is now
content the user supplies: the configured system prompt (ADR 0023) and, since ADR 0026, the
workspace context files folded in beside it. A prompt — or a repo's own `AGENTS.md` — that happens
to contain one of those phrases therefore reads as "already injected", and the Mechanism stays
quiet.

It is a **suppression, not a corruption**: the request stays well-formed, and the user's own
sentence on that subject is what the model reads instead. The blast radius is bounded to the
catalogued nudge Mechanisms, which are default-off and enabled per model on bench evidence
([ADR 0015](docs/adr/0015-catalogued-mechanisms-are-enabled-by-id-through-config.md)).

**Standing (do not re-file as new ideas):** both obvious fixes were weighed and declined in ADR
0023 — a non-textual idempotency channel changes the hook API's contract for every Mechanism, and
opaque sentinel strings would put text in front of the model whose only reader is apogee.

---

### Phase-5 verification leftovers — the owner-run passes this machine cannot perform

**Status:** carried forward 2026-07-22 from the "Owner-run checklist" of the archived
[`docs/plans/archived/2026-07-22 - 00 - phase5-cross-platform-hardening-plan.md`](docs/plans/archived/2026-07-22%20-%2000%20-%20phase5-cross-platform-hardening-plan.md)
(read it for full context). What the checklist's runnable passes covered is in `CHANGELOG.md` and
the archived plans; what remains open needs hardware this environment does not have:

- **Live Auto-confined deliverable run on Windows** — the ADR 0020 backend is proven natively
  (escape battery + the real `Terminal` tool under `platform.NewConfiner()`); an end-to-end
  deliverable session under Auto is what remains, if an LLM endpoint is reachable from that
  machine.
- **Live Auto-confined deliverable run on macOS (seatbelt) + runtime smoke** — same shape; the
  darwin binary cross-builds clean (amd64+arm64, re-verified 2026-07-23), so what remains on a
  Mac is `--help`, a trivial session, and the confined Auto run. That run should also probe the
  `/dev/null` seatbelt exemption live — `internal/platform/seatbelt_darwin_test.go` only
  delegates to the shared `confinetest.Probe` battery, so the exemption is pinned by the hermetic
  profile-string tests alone (folded here from the /dev/null confinement run's residuals,
  2026-08-13).
- **Degradation notice on a below-floor Windows host (< build 17763)** — the deny-vs-token
  decision itself is table-proven (`TestBelowWindowsFloor` in the untagged `winguard.go`
  predicate); only the on-host UX observation of the notice stays untested (recorded so in
  [ADR 0020](docs/adr/0020-windows-confinement-is-a-low-integrity-token-and-the-box-is-a-disk-label.md)'s
  consequences), verifiable only if such a host turns up.
- **Job-object breakaway assertion on Windows** (folded here 2026-08-15 from the open-residuals
  sweep's residuals) — the Windows counterpart to the POSIX setsid escape
  (`internal/platform/teardown_unix.go:62`) is prose-only and untested: the job object "does not
  permit breakaway" (`internal/platform/teardown_windows.go:23`), and nothing asserts that a
  descendant cannot leave the job, so the POSIX side's pinned residual has no Windows twin.
  Needs a Windows host.
- **The advisory single-instance lock, on Windows and on macOS** (folded here 2026-08-22 from the
  residuals of `docs/plans/archived/2026-08-22 - 02 - daemon-plan.md` item 4) — the lock that keeps
  one `apogee daemon` per apogee home is `flock(2)` on POSIX (`internal/platform/lock_unix.go:29`)
  and `LockFileEx` translating a refused wait into `ERROR_LOCK_VIOLATION` on Windows
  (`internal/platform/lock_windows.go:47-58`). Both non-Linux halves are COMPILE-verified only from
  this host: `internal/platform/lock_test.go`'s contention and release cases run natively on Linux
  alone. What stays open is the runtime assertion on the other two — a second handle refused while
  the first holds it, the lock dropped when the process dies — which belongs with the live daemon
  activation pass `CHANGELOG.md` already defers to the owner.

**Not repeated here:** the "pre-existing, NOT Phase 5 scope" group (Linux/macOS live-run
variants already tracked elsewhere) lives in the CHANGELOG's **"Known post-release verification
(owner-run / CI)"** note — that note, not this file, is its tracking home.

---

### Windows Auto: box-local `%TEMP%` / toolchain caches

**Status:** recorded 2026-07-22 (Phase 5 review fixes,
`docs/plans/archived/2026-07-22 - 02 - phase5-review-fixes-plan.md` item 12). Not built by that
plan by decision — it needs its own design session. Sources: ADR 0020 §2 (the "Consequence for the
box builder" paragraph) and `docs/design/confinement-execution-contract.md` §7.

**The gap.** ADR 0020 §2 names the toolchain's cache/temp dirs a **hard prerequisite** on Windows,
not the ergonomic nicety contract §7 treats them as on Linux/macOS: the confined child runs under a
*low-integrity* token, and a Low process cannot write to an unlabelled (Medium) directory at all.
`%TEMP%`, `$GOCACHE`, `~/.npm`, `~/.cargo` and the pip cache all live outside the workspace and are
unlabelled, so under the Windows fence a confined `go build`, `go test`, `pip install` or `npm ci`
fails outright — not with a partial result, but at the first write to its cache or temp dir. The
workspace-scoped writes the fence does cover work fine; toolchain work under Auto does not.

**Why nothing bridges it today.** The box field that would carry those dirs,
`domain.Config.ConfineWritablePaths`, has only **readers** —
`internal/agent/dispatch.go:417` and `:457`, which copy it into
`domain.ConfinementBox.WritablePaths` for a tool call and for a hook-time subprocess respectively —
and, repo-wide, **no writer**: nothing probes for toolchain caches and nothing surfaces the field in
config, so it is always empty. Contract §7's own recommendation ("seed `WritablePaths` with the
detected toolchain cache + temp dirs by default, probed, not hard-coded") was never implemented on
any platform; on Windows it is the difference between a usable fence and an unusable one.
`internal/platform`'s `ScopeEnv` (the environment-scoping seam ADR 0020 §2 points at) exists and is
used by `git` and the Go toolchain, but `terminal` and `python_exec` inherit the user's `%TEMP%`
unchanged.

**The design question to settle when this is picked up:** a **box-local `%TEMP%`** — point the
confined child's `TMP`/`TEMP` (and `GOTMPDIR`, `GOCACHE`, …) at a labelled directory inside the box
via `ScopeEnv`, so nothing outside the workspace is ever marked — **versus labelling the user's own
cache/temp trees**, which is simpler but marks large, long-lived, shared trees Low (ADR 0020's own
"keep the labelled surface small" argument, and the ~1 ms-per-object walk cost, both cut against
it). ADR 0020 §2 already prefers the box-local answer, and calls it environment-scoped execution
plus box construction, **not** a `Confine` responsibility — so the work lands in the box builder and
the execution tools, and the `Confine` contract (§2) is unaffected. Whichever way it goes, it also
decides whether cache dirs are *probed* per toolchain or named in config, and it is the natural
moment to give `ConfineWritablePaths` its first writer.

---

### The TUI width authority — the standing rules its work left behind

**Status:** standing rules from the *width authority* plan (`2026-07-31 - 03`, under `docs/plans/`,
archived on completion) —
[ADR 0030](docs/adr/0030-the-tui-has-one-width-authority-and-it-mirrors-the-painter.md). The residue
itself is closed; see `CHANGELOG.md` for what landed. What stays here binds future work, which is
why it is not in the changelog alone.

**The rules:** the one `lipgloss.Style.Width` still in the package — the prompt box framing a
widget that wraps in GraphemeWidth itself — is ADR 0030 §6's widget-mirror exception, so do not
re-file it, and never put a `Width` style on a *bordered* surface (§5). `inputContentRows`' taller
counts need no clamping change: `promptEditor.rows` and `layout()` already clamp
(`TestPromptEditorRowsClampsTheWidgetCount`).

---

### A model-facing `schedule` tool — daemon-era, not v1

**Status:** parked 2026-08-04 (assessment at the `/schedule` close-out,
[ADR 0033](docs/adr/0033-the-scheduler-is-a-library-and-the-tui-is-its-first-driver-surface.md)).
The question: should the model be able to create Schedules itself, via a tool, instead of telling
the human to run `/schedule`?

**Why not now — two costs, one thin benefit.** The benefit is a keystroke: a TUI-hosted Schedule
dies with the process, so the model would only ever set up something short-lived the human can
create with one command. Against that: (1) **catalogue dilution** — every schema costs context and
tool-selection accuracy for the 4B–35B target class, tools are not per-model curated the way
Mechanisms are, and a tool relevant in one session in fifty is exactly what the never-worse
invariant says to refuse; (2) **authorization inversion** — ADR 0033 decision 3 makes the *human's*
creation choice the mode authorization, and a model-created Auto schedule is the model granting its
future self unattended runs; inside a Firing it is self-replication (a firing scheduling more
firings).

**The shape when built (so it is not re-derived).** Layering is *not* the blocker — the precedent
is `ask_user`: a driver-supplied delegate seam, nil = tool not registered, and
[ADR 0002](docs/adr/0002-tools-are-an-open-extension-point-mechanisms-are-curated.md)'s open
registry means the driver that owns the scheduler registers the tool without touching the engine.
Creation routes through the **Approver** as a gated (non-ReadOnly) action, which buys the key
property for free: a Firing's fail-safe denier refuses it, so self-replication is structurally
impossible with no special-casing, and Ask-Before preserves decision 3's human authorization at the
gate. The residual hole is Auto (gated-but-eligible runs unsupervised — the exact bad case):
"create an Auto schedule" belongs in the footgun-guard's dangerous tier, or the tool caps
model-creatable schedules at Plan. Sub-agents don't inherit it (ADR 0005).

**The trigger to pick it up: a real need.** Durable schedules that survive quit are where a model
setting up its own monitoring has real value, and the companion feature is the cross-session
approval surface ADR 0033 already named when it rejected Ask-Before schedules. Build the tool once,
at that layer; the TUI still never has to register it. The daemon's shape has since been settled —
[ADR 0034](docs/adr/0034-the-daemon-is-an-in-repo-subcommand-over-a-declarative-trigger-action-file.md),
an in-repo subcommand over a declarative trigger-action file — and `apogee daemon` itself shipped
on 2026-08-22, which made this tool possible, not needed. Re-parked 2026-08-22 (owner call): this
entry stays parked until a real need for a model-facing schedule tool appears.
**Not the vehicle:** MCP — schedules are
in-process driver state, Firings run without MCP, and ADR 0031 forbids first-party connectors.

---

### The published binaries are not code-signed

**Status:** parked 2026-08-05, the day the first prebuilt binaries shipped (v0.11.0 — all six
targets as release assets, plus the `airiclenz/tap` Homebrew formula that installs them). The
README states the gap where the download is rather than burying it, so this entry exists to keep
that statement honest and to hold the design once it is picked up.

**What the gap actually is, per platform** — it is two different problems wearing one name:

- **macOS.** The Go linker already ad-hoc signs `darwin/arm64` (verified on the v0.11.0 asset:
  `LC_CODE_SIGNATURE`, 162 KB), which is the *load* requirement on Apple Silicon — without it the
  kernel SIGKILLs the process, so the binaries do run. What is missing is **Developer ID signing +
  notarisation**, which is the *Gatekeeper* requirement: a browser download carries
  `com.apple.quarantine` and is refused until `xattr -d com.apple.quarantine` clears it. A `curl`
  download never sets the attribute, and Homebrew's own download does not either — so the brew path
  and the documented `curl` path are both unaffected today. Only the browser path is.
  `darwin/amd64` carries no signature and needs none to load.
- **Windows.** No signature at all, so SmartScreen warns about an unrecognised publisher. An
  Authenticode certificate is the only fix, and reputation accrues per-certificate over time.

**Why it is deferred, not dropped.** Both halves cost money and identity, not engineering: an Apple
Developer Program membership (notarisation also needs a per-release upload to Apple's service, so
it puts a network round-trip and a credential into `make dist`'s path) and an Authenticode
certificate from a CA. Neither belongs in a pre-production 0.x release cut from a laptop, and
`SHA256SUMS` — published as a release asset, and what the Homebrew formula pins per platform — is
the integrity check that is actually available today.

**The design question to settle when this is picked up:** whether signing stays a *release-time*
step layered over `make dist` (the target keeps producing unsigned archives, and a separate
signing/notarisation pass rewrites them before upload) or moves *into* it. The layered answer is
strongly preferred: `make dist` is cross-platform by construction — one CGO-free `go build` per
target from whichever machine cuts the release — whereas `codesign`/`notarytool` run only on macOS
and `signtool` only on Windows. Folding either in would make the whole matrix un-buildable from a
single host, which is the property the target exists to have. That points at CI (a signing job per
platform, secrets held there) rather than at the Makefile, and CI-cut releases are not a thing this
repo does yet — settle that first.

---

### Tool-surface findings (4-poll round, 2026-08-10)

**Status:** recorded 2026-08-10 at the close of the tool-surface plan; extended 2026-08-16 by a
second poll round. The full record — both rounds, the denials with reasons, the deferred
candidates, the engine-level notes and the method lessons — is in
`docs/design/tool-surface-findings.md`; this entry is the deferral trail's pointer, not the record.
**Nothing leaves the roster on poll evidence alone**: each arm below is a bench experiment, not a
decision.

- **(a)** remove `single_find_and_replace`.
- **(b)** patch-only vs find-replace editing.
- **(c)** `open_file`/`read_file` merge — shipped 2026-08-11 by owner call; the open watch-item is
  whether models find `read_file`'s `locate` *parameter* as readily as they found the old *name*.
- **(d)** do sub-35B models use `view_diff` at all? — decide with the `write_file` dry-run.
- **(e)** `web_fetch` → `http_request` merge.
- **(f)** is `edit_existing_file`'s patch mode ever discovered unprompted?
- **(g)** unified `git` action-enum tool vs the named `git_*` family (recorded 2026-08-25; the
  family stays until the arm returns, new git verbs only on a replicated ask).

---

### The hero tape's knob 3 is a clock where a screen-state trigger is needed

**Status:** parked 2026-08-24 — owner call at the close of the hero-GIF refresh plan
(`docs/plans/archived/2026-08-24 - 00 - hero-gif-refresh-plan.md`): the fix is a mechanism, not a
knob value, and the run had a frame-verified keeper in hand.

- [ ] Knob 3 is the fixed `Sleep 10s` at `graphics/demo/tapes/hero.tape:95` that precedes the
  block-cursor gesture (`Escape` + `Type "[1;3A"`, `graphics/demo/tapes/hero.tape:96-98`) which
  opens the fix's collapsed edit card for beat 5. The window it must hit is the gap between the
  `task.go` `Replace` card painting and the queued interjection being DELIVERED — delivery lands at
  the very next tool boundary, so the window is UNDER A SECOND, while the run varies ~19s to ~29s
  end to end and slides that window by ~8s. Measured hit rate ~1 take in 7. Tuning downward is the
  wrong direction, not a better guess: at 3s nothing is settled for the block cursor to stand on,
  the leading ESC of the CSI is read alone, and the run is CANCELLED (session JSON ends
  `note: cancelled`, stage tree clean). Both failure shapes are written into the knob comment
  (`graphics/demo/tapes/hero.tape:49-94`) and into `graphics/demo/README.md:116` ("Knob 3 is a coin
  toss, not a setting"). A clock cannot track a window that slides with run length: what this needs
  is a trigger keyed to screen state — the card having painted — rather than to elapsed time.

---

### Audit residue (2026-08-25 refocus / security / code audits) — deliberately outside the five first-wave plans

**Status:** recorded 2026-08-26 when the merged findings handoff (`docs/handoffs/2026-08-26 - 00 -
merged-audit-findings.md`, untracked by design) was cut into plans `docs/plans/2026-08-26 - 01` … `05`.
Everything the three audits found that is NOT an item in one of those plans lives here with the reason
it waits. Sources: `docs/reviews/code-audit-2026-08-25.md` (C-nn), `docs/skill-runs/security-audit/
2026-08-25/report.md` (F-nn), `docs/skill-runs/refocus/2026-08-25/briefing.md` (R-n).

**Design discussion before code** — each wants its own grill; none is a one-item fix:

- [ ] **C-13 — the library store persists under its lock.** `internal/library/store.go` `Record` /
  `RecordSuccess` call `persist()` (MarshalIndent + temp file + rename) inside `s.mu.Lock()` with no
  `ctx`, so ADR 0039 fan-out serialises every completion behind a disk rewrite and a hung filesystem
  hangs the loop. Shape to settle (write model): mutate under the lock, encode and write outside it;
  optionally a coalescing async writer. Not independently verified.
- [ ] **C-04 — the decompose scoring cap saturates.** `internal/mechanisms/decompose.go`
  `decomposeCountPhraseMatches` returns at `cap` and every caller passes `perMatch` = cap/2, so the
  second match saturates and the classifier degenerates to category count + length bonus (two
  delegation + two conditional phrases = 14 ≥ 10 → "complex" → history collapse on a simple task). The
  fix — sum per-match points, cap the category total afterwards — shifts classification thresholds
  and any calibrated bench arm, so it lands with apogee-sim evidence, not on sight. Not independently
  verified.
- [ ] **C-03 — unattended runs send the empty effort dialect.** The "no construction seed" residual
  under *Effort detection and the effort picker* above; the code audit supplies the seam
  (`cmd/apogee/wire_firing.go` `firingConfig`, `run.Once` never `Rebind`s) and the fix shape (a
  dialect field on `domain.Config` or the Spec). Home of the field is the call.
- [ ] **C-20 + F-08 — the Windows pair.** `internal/platform/confiner_windows.go` keeps no mutex by
  stated design, so `Close` zeroes `token`/`caps` while `Confine` reads across a label walk; ordered
  exits are joined but bubbletea's abnormal exit (SIGINT, closed console) does not wait on Cmd
  goroutines → token 0 → `CreateProcess` unconfined, marked `confined=true`; the console spawn from
  `context.Background()` (`internal/console/process.go`) has no fail-closed. `internal/platform/
  winlabel/journal.go` replays a planted journal's label writes and `IsLowLabel` vouches the write
  side only. Both need a Windows box to verify; the "backend keeps no lock" contract is what to revise.

**Test debt:**

- [ ] **C-15 — `GatherTerminal`'s measurement engine is untested past its two abort paths**
  (`internal/probe/terminal.go`; `internal/probe` at 49.4% coverage, the repo's outlier). Wants a
  scripted `TerminalInputs.Read` that answers each query, asserting Rows/Summary/Mismatch for an
  agreeing and a diverging terminal.

**Architecture pass, not fixes** — candidates for the next architecture-review plan
(`docs/plans/archived/2026-08-24 - 03 - architecture-review-deepening-plan.md` is already archived):

- **S-1** `cmd/apogee/wire_test.go` — a 5,844-line single test drawer for a composition root that
  ADR 0043 split by seam on the production side.
- **S-2** `internal/mechanisms/grammar.go` — grammar-constraint plumbing reachable only through
  `Deps.GrammarConstraint`, which nothing populates, over a provider wire that cannot carry it.

**Signal the audits could not produce:** `golangci-lint` and `govulncheck` were not installed on the
audit host — no lint and no dependency-vulnerability signal from any of the three audits, and the
dependency half of the security audit's `dependency-surface` family is unaudited (no network for a
CVE lookup). Every verdict was a code reading; the external-behaviour claims the security report
flagged (git `core.fsmonitor`/hooks/filters, `net/http` nil `CheckRedirect`, `os.OpenRoot` symlink
semantics, ODF/EPUB handler execution, terminal column-0/bidi rendering) are exercised by the
reproduction tests the corresponding plan items carry, not here.

**Worth watching:** the stock `gemma-4-e4b-it-qat` Validated set (`shipped.json`) arms
`cached_content_intercept`, `autofix` and `filehint` — `filehint` is C-08's stock-install
reachability (plan 02 item 5).

---

### Model-facing skill discovery (B1 auto-attach / B2 `load_skill` tool) — deferred by ADR 0061

**Status:** deferred 2026-08-27 by
[ADR 0061](docs/adr/0061-skill-suggestions-are-driver-side-over-an-engine-matcher.md) decision 4.
**B1** attaches a matched skill's body to a message carrying no `/id`; **B2** registers a
model-callable `load_skill` tool that pulls a body mid-Turn. **Deferred because** either one puts
catalog-derived prompt text into the request, making it a **Mechanism** — catalogued, gated, bound
by the Bypass never-worse floor — and B2 also contradicts CONTEXT.md's *Skill* entry outright
(*"_Avoid_: 'tool'"*), a domain-language change before it is a feature. **Before it is built:** its
own grill, an ADR **explicitly superseding ADR 0061**, and a bench arm against Bypass. **The
reusable half exists:** `skills.Catalog.Suggest` (plan `docs/plans/2026-08-27 - 01 -
skill-suggestions-band-plan.md`) is engine-level and model-free, so B1 would consume the matcher
rather than grow a second one.
