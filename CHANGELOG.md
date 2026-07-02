# Changelog

All notable changes to Apogee are recorded here. The public Go API follows
[Semantic Versioning](https://semver.org/spec/v2.0.0.html) from `v1.0.0`
onward (ADR 0001 §consequences, as amended at the Phase-3 cut): Events and
hook points stay **additively extensible**, so a new Event variant or hook
point is a **minor** bump, not a breaking change.

## [Unreleased]

Post-`v1.0.0`, **additive** (minor) — the start of the apogee-code TUI
feature-parity track. See
`docs/handoffs/2026-06-26 - 00 - chat-mini-language-core.md` and
`docs/handoffs/2026-06-26 - 01 - skills-system.md`.

### Compact tool print-outs in the chat (full built-in coverage)

- **The TUI's tool-presentation registry now covers every built-in tool**, not just the
  Phase-2 four: the edit family, `view_diff`, `open_file`, `terminal`, `python_exec`, the
  git family, `diagnostics`, `web_fetch`, `http_request`, `web_search`, `sub_agent`, and
  `ask_user` each render as `✦ [Label] target` — no more raw tool names with JSON argument
  braces in the transcript. Only a dynamic (MCP) tool keeps the raw-name + JSON fallback.
- **Results no longer dump raw into the chat**: `web_search` shows "N results", the fetch/
  request tools their `HTTP 200 OK` status line, free-form output (a command run, a
  diagnostics or sub-agent report) its first line plus a "+N more lines" count, `open_file`
  its Located line or a line count. `view_diff` renders red/green diff lines (the reserved
  diff detail kinds get their first producer), capped at 20 lines.
- Detail and target lines are clipped at 160 runes so a minified blob cannot flood a row.
  The approval dialog still shows the full pretty-printed arguments — the security surface
  (the model's request is never hidden) is unchanged.

### Web search works out of the box (DuckDuckGo default)

- **`web_search` is now default-ON**: with no `web-search-endpoint` configured it uses a
  built-in DuckDuckGo HTML provider — no config, no API key (reverses the P3.11 default-off
  decision; the predecessor apogee-code shipped the same built-in). Set
  `web-search-endpoint: off` (or `none`/`disabled`) to disable the tool — a graceful
  "web search is disabled" result, no request made.
- **The DuckDuckGo provider POSTs the query** as a form field, the way DDG's own search
  form submits: the HTML front-end answers a plain GET with its bot-challenge ("anomaly")
  page — zero result anchors, so every search rendered "No results found". A custom
  endpoint keeps the `q` GET-parameter contract unchanged.
- **An explicitly configured DuckDuckGo endpoint selects the built-in provider**: an
  endpoint whose host is `html.duckduckgo.com` (with or without scheme) now gets the same
  POST + browser-header treatment as the default, instead of degrading to the
  custom-endpoint GET that DDG answers with the challenge page.
- **Results are auto-cleaned**: the DuckDuckGo page (and any custom endpoint's HTML
  response, by Content-Type or body sniff) is parsed into numbered `title / url / snippet`
  results; a custom endpoint's JSON/text response still passes through verbatim. A
  rate-limit/consent page degrades to "No results found", never a crash.
- **Non-2xx responses are now tool errors** naming only the status and endpoint host
  (previously the status + raw body passed through as a normal result). The M2 key
  redaction (`endpointHost`/`scrubURLError`) and the always-on SSRF floor are unchanged.
- **Scheme-less custom endpoints self-heal**: an endpoint like `search.example.com/s`
  (no `https://`) used to parse with an empty host and every request was rejected by
  url-safety; it now self-heals to `https://`. This repairs hand-edited configs — the
  shipped config template never carried a broken value (its endpoint line was always
  commented out), and first-run seeding never overwrites an existing config.

### Context compaction (`/compact`)

- **`/compact` now performs real generative compaction** (replaces the
  `ErrCompactionNotImplemented` stub). The new `internal/context.Compact` reducer
  summarizes the conversation through a single upstream call and replaces the folded
  history with one assistant summary message, keeping the protected prefix (leading
  system messages + the first user message, `Conversation.PrefixEnd`) verbatim so the
  original task framing survives. A conversation with too little past the prefix is
  skipped; a summary-call failure or cancellation leaves the history untouched.
- **Wired through `Agent.Compact`** (guarded to a quiescent boundary like `ClearContext`,
  returning `ErrInputPending` mid-Exchange). The summary call is *silent* — it reuses the
  loop's request projection but emits no `TokenEvent`/`UsageEvent`, so it neither streams
  into the transcript nor moves the live gauge; it runs at low temperature.
- **TUI** drives `/compact` on a worker goroutine (it is a real upstream call and must not
  block the `Update` loop — ADR 0011): the spinner runs, `Esc` cancels, and on success a
  "context compacted" note lands while the context-fill gauge resets so the next Turn
  re-measures the smaller fill.
- **Removed** the now-unused `ErrCompactionNotImplemented` sentinel (it was never in a
  released version).

### Fixes

- **Auto mode now works on macOS — seatbelt fences the workspace correctly.** The
  `sandbox-exec` profile embedded the box's writable roots verbatim, but seatbelt
  matches a write against its *kernel-canonical* path; on macOS `/tmp` and `/var`
  are symlinks into `/private`, so a box rooted at `/var/folders/...` never matched
  the resolved `/private/var/folders/...` and seatbelt denied **every** in-workspace
  write — Auto mode could not write at all. `seatbeltProfile`
  (`internal/platform/seatbelt.go`) now resolves each writable root through symlinks
  (`filepath.EvalSymlinks`, falling back to the cleaned path for a not-yet-created
  root) before emitting the `(subpath ...)`, so the profile matches the kernel's view
  and agrees with path-safety (which already resolves the same way). Landlock is
  unaffected — it is fd-based (`unix.Open(root, O_PATH)`), so the kernel resolves
  symlinks to the inode the rule keys on. Closes the `v1.0.0` "Box-root
  canonicalization" post-release residual; verified on real macOS hardware
  (`TestSeatbeltProbe` in-box write rows now pass under live `sandbox-exec`).

- **Context window now reads the runtime size from llama.cpp `/props`.** Discovery
  (`internal/provider.Discover`) probes `GET /props` after `/v1/models` and prefers
  its `default_generation_settings.n_ctx` — the `-c`/`--ctx-size` the server was
  actually launched with — over the model's advertised *training* window
  (`context_length`, else `meta.n_ctx_train`), which is often far larger than the
  loaded window. This fixes the live context-fill gauge measuring usage against the
  wrong denominator (it barely moved on a server loaded well under its training
  context). Best-effort: a non-llama.cpp server (no `/props`) keeps the `/v1/models`
  value, and a probe failure never fails discovery. Ports the oracle's previously
  deferred `llamacpp-props` strategy; the `ollama-show`/`ollama-tags` strategies
  remain unported (additive, not needed yet).

- **`/compact` and the context gauge now tell the truth.** Four fixes to the
  compaction/gauge seam that had it reporting outcomes it did not produce:
  (a) an Esc landing *after* a compaction committed reported "cancelled" while the
  history had already folded — `startCompact` (`internal/tui/worker.go`) now
  classifies the outcome from `Compact`'s returned error (`context.Canceled`), not a
  post-hoc `ctx.Err()` read, so a committed fold reports as compacted;
  (b) a no-op compaction (conversation too small to fold — the reducer's
  `Result.Skipped`) printed "context compacted" and hid the gauge — `Agent.Compact`
  now returns the skip signal through the `Engine` seam and the TUI says "nothing to
  compact" and leaves the gauge untouched;
  (c) `/clear` left the gauge and tok/s readout lit from the discarded session —
  `ClearContext` now zeroes `ctxUsed`/`tokPerSec` like a fold does;
  (d) a cancelled or faulted stream emits no terminal `UsageEvent`, so the
  generation clock survived into the next turn and mistimed its tok/s — `finishWorker`
  now clears `genStart` on every terminal message.

- **A loop fault no longer risks re-wedging the engine.** The `errMsg` handler
  (`internal/tui/model.go`) now calls `AbortExchange` before returning to the errored
  state, mirroring the `cancelledMsg` recovery: if a `Step` ever faults mid-Exchange
  the interrupted Exchange is discarded so the next `/clear` or message is accepted
  rather than refused with `ErrInputPending`. A latent fix — `Step` surfaces faults as
  an `ErrorEvent` at a boundary today — but it closes the error flavour of the post-Esc
  un-wedge. The `/compact` failure/cancel spine (both `startCompact` outcomes and the
  reducer's overflow/cancel/silence faults) is now covered by tests.

- **Mouse selection and bracketed paste now handle the prompt correctly.** Two input
  fixes on shipped TUI behaviour:
  (a) a click or drag on a prompt row with wide glyphs (CJK, emoji) landed the caret on
  the wrong rune — `caretTo` (`internal/tui/mouse.go`) fed a display-**cell** column into
  the textarea's rune-indexed `SetCursorColumn` (clamped by cell width, not rune count),
  so a drag-copy could put **different text on the clipboard than was highlighted**. It
  now converts the cell column to a rune offset by walking the visual sub-line's runes and
  accumulating `runewidth` (the same width the widget's own cursor math uses), clamped by
  rune count;
  (b) bracketed paste (default-on in bubbletea v2) fell into `Update`'s `default:` case,
  so the textarea inserted the text but skipped the post-edit refresh — a multi-line paste
  rendered unwrapped until the next keypress, the autocomplete overlay went stale, and a
  live drag-selection's cached offsets no longer matched the value (a later copy took the
  wrong runes). A new `tea.PasteMsg` case (`internal/tui/model.go`) mirrors the keypress
  edit path: it clears the selection, inserts, recomputes autocomplete, and re-lays out;
  a paste while a worker runs is dropped, as keystrokes are.

### TUI

- **Context-fill gauge restyled** to match `llama-launcher`: a solid two-tone strip —
  full blocks for the filled cells, an eighth-block partial cell (`▏▎▍▌▋▊▉`) for
  sub-cell granularity, and a solid dark-gray track behind the remainder — replacing
  the old `█░` dotted bar. Periwinkle fill, a min-sliver floor so any nonzero usage
  shows at least `▏`, and a clamp at the window limit. Bar width is now 10 cells (was
  6). The status line composes the gauge raw rather than re-wrapping it in a
  background style, so the bar keeps its own per-cell backgrounds.

### Skills system + `/skill` (apogee-code feature-parity)

- **`internal/skills` package** discovers user-authored skills — a folder
  containing a `SKILL.md` (YAML frontmatter `id`|`name`, `displayName`,
  `summary`|`description`, plus a Markdown body; a no-frontmatter fallback sniffs
  the first lines) — from three layered dirs: `~/.apogee/skills`, the workspace's
  `.apogee/skills`, and (when `use-project-skills` is on) the workspace's bare
  `skills/`. Later source wins on an ID collision. Each dir is walked through
  `os.OpenRoot` so a symlink can't escape it; a missing dir is skipped and a
  malformed skill is skipped with a soft error (one bad file never blanks the
  catalog). No builtin/embedded skills and no auto-created `~/.apogee/skills` ship
  in v1 (additive future hooks).
- **`/skill` in the TUI** — the `/` menu offers `/skill`, which chains into a skill
  picker; a pick pops a chip above the input, and submit attaches the chosen IDs.
  An empty message with skills attached is a valid send. `/skill` is deliberately
  **not** a parser command (attachment is the only way it acts), so an unknown
  `/skill foo` is still sent as an ordinary message. `/clear` and `/compact` drop
  staged chips; `/continue` carries them.
- **Attached skills now resolve** (replaces the `SkillIDs` "reserved/ignored"
  stub): the loop maps each `UserInput.SkillIDs` entry through `Config.Skills` and
  prepends its body to the user message for that one Turn (order: skills → `@file`
  blocks → user text). An unknown ID, or any ID with no resolver wired, is reported
  via an `ErrorEvent` and dropped — never silently ignored.

### Configuration

- **`use-project-skills`** (config-file only, default **true**) gates discovery of
  the workspace's bare `skills/` folder (the global library and the project's
  `.apogee/skills` are always loaded). Documented in the seeded `config.yaml`.

### Chat input mini-language (core)

- **Parse/route layer** between the TUI input box and the agent: `/`-prefixed
  lines route to local command handlers, `@file` tokens are extracted as
  references, and an autocomplete overlay (commands + workspace files, the latter
  via a bounded `os.Root` walk) mirrors the approval-prompt overlay.
- **Commands**: `/clear` (drop the model's context, keep the visible transcript),
  `/continue` ("Please continue"), and `/compact` (generative compaction — the command
  surface and the `Agent.Compact` seam landed here; the reducer that folds the history
  through them shipped in the same track, see the "Context compaction (`/compact`)"
  section above).
- **`@file` references now resolve** (behaviour change): the loop reads each
  `UserInput.FileRefs` entry within the workspace fence (`security.SafeReadFile`,
  `os.Root`-pinned) and injects its content into the user message — replacing the
  prior "refs ignored" `ErrorEvent`. A missing, oversized, or escaping ref is
  reported and skipped; the Turn still proceeds.

### Public API (additive — minor)

- `Agent.ClearContext() error` — drop the conversation history at a quiescent
  boundary (the host's transcript is unaffected); refused mid-Exchange.
- `Agent.Compact(context.Context) (skipped bool, err error)` — on-demand generative
  Compaction: summarizes the conversation and folds the history at a quiescent boundary
  (refused mid-Exchange with `ErrInputPending`, like `ClearContext`). `skipped` is true
  when the conversation was too small past the protected prefix to fold — no upstream
  call, history untouched; always false on error.
- `UserInput.SkillIDs []string` — the skills attached in chat; the loop resolves
  each through `Config.Skills` and prepends its body to the Turn (was reserved).
- `Config.Skills SkillResolver` — host-supplied resolver for attached skill IDs
  (nil ⇒ attached IDs are reported and dropped). `SkillResolver` and its return
  type `ResolvedSkill` are re-exported on the root facade; the disk-backed catalog
  stays internal (`internal/skills`).

## [1.0.0] — 2026-06-25

The first stable release. `v1.0.0` cuts the public Go API after Phase 3 brought
the agent to feature-parity with apogee-code's non-UI behaviour, with **Auto
mode confined** on Linux (landlock) and macOS (seatbelt). Every consumer — the
TUI, the bench, and the embeddable library surface — has exercised the API, so
semver now begins (ADR 0001 §18, amended).

The public surface is the root `apogee` package: `Agent` (`New`/`Resume`),
`Config` and its host delegates (`EventSink`, `Approver`, `Asker`,
`ExternalEffects`), the four-rung `Mode` ladder, the `Tool`/`ToolRegistry`
extension point with the `ReadOnlyTool`/`ExternalEffectTool` markers, the
`Event` variants, and the hook points. Tools live behind the registry (an open
extension point, ADR 0002), not as root types.

### Confinement (Auto mode is real)

- **Blast-radius confinement model** (ADR 0012, supersedes ADR 0004): a tool
  call runs without a human gate only if its blast radius is bounded — by **OS
  confinement** for the unbounded subprocess/network surface, or by Apogee's own
  **path-safety-to-workspace** for its own in-process writes. Confinement
  attaches to blast radius, at a single **subprocess granularity** on every OS
  (no in-process per-thread landlock, no thread-discard).
- **Four-rung autonomy ladder**: Plan → Ask-Before → **Allow-Edits** → Auto.
  The new `ModeAllowEdits` rung auto-approves Apogee's own workspace-scoped
  writes (no confinement needed; identical on every OS) and gates everything
  else.
- **Linux landlock backend** (`//go:build linux`): ABI probed at startup; an
  honest capability matrix (`FSWrite` at ABI ≥1 / kernel ≥5.13, `NetworkEgress`
  at ABI ≥4 / kernel ≥6.7); a confined subprocess applies the landlock domain
  after fork, before `execve`, so the child is fenced and the parent stays
  unrestricted. Raw `golang.org/x/sys/unix` syscalls (now a direct dependency).
- **macOS seatbelt backend** (`//go:build darwin`): a `sandbox-exec` profile
  generated from the `ConfinementBox` (workspace-write-only + network-open by
  default), presence-probed, no new Go dependency.
- **`Confine(ctx, box, *exec.Cmd)`** prepare-in-place contract: the tool builds
  an idiomatic `*exec.Cmd`; the backend rewrites it to launch confined. The
  `confine-to-workspace` global-config key (default `true`) tunes Auto's blast
  radius; `confine-to-workspace=false` is the explicit "I am the sandbox"
  (VM-only) opt-out. `AutoEligible()` requires filesystem confinement only;
  where confinement is unavailable, subprocess tools gate through Approval
  ("confine if you can, gate if you can't") rather than refusing Auto.

### Tools (feature-parity with apogee-code's non-UI surface)

- **File-editing family**: find-replace (single + multi), `edit`/apply-edit,
  `diff`, `open-file` — pure-Go, stateless, carrying the unexported
  `workspaceScopedWriter` marker so Allow-Edits/Auto bound them by path-safety.
- **Execution tools**: `terminal` and `python-exec` — one-shot, stateless, the
  first `Confiner` consumers; process-group teardown on cancel
  (`Setpgid` + `cmd.Cancel` + `WaitDelay`).
- **`git` tool**: branch / commit / diff-range over the system `git`, detected
  and graceful-degrading when absent.
- **`diagnostics` tool**: in-process `go/parser` + optional `go vet`,
  read-only, graceful when the toolchain is absent.
- **Network + host tools**: `web_fetch`, `http_request`, `web_search`
  (external-effect, Approval-gated as MCP-kind / auto-run url-filtered as
  network-kind per the disposition table) and `ask_user` (the new `Asker` host
  delegate). These are routed through the `ExternalEffects.Do` boundary
  (ADR 0008) so the bench can stub them.
- The existing `read_file` / `write_file` / `list_dir` / `grep` built-ins carry
  forward; `write_file` carries the workspace-scoped-writer marker.

### Processing (parity-complete port)

- **All apogee-code tool-call formats parse**: native/JSON `tool_calls`,
  markdown-fenced, and custom-regex, each gated by **ported TS test vectors**.
- **Full harmony / thinking-channel set** handled, with a `processor-factory`
  that selects the format per model/response. The package stays `domain`-only.

### Security guardrails (the human-in-the-loop layer)

- **`internal/security`** consolidates the Phase-1 per-tool path-safety into one
  reusable guard and adds **url-safety**, an **arg-guard**, a **circuit-breaker**
  (halts a runaway tool-loop), and an **audit record** (bounded ring buffer with
  a dropped-count). These run in all modes and a sub-agent inherits them.
- **Two-tier dangerous-action guard** (a footgun-guard, NOT a security
  boundary): a hard-refuse tier (`rm -rf` of root/home/system, fork bombs,
  `~/.ssh`/credential/persistence writes) and a force-approval tier
  (`curl | bash`-class). It runs first and is **tighten-only**; project config
  may only add rules, never dissolve a floor rule by ID.
- **Default-on SSRF floor** for the network tools: loopback / private ranges /
  IMDS `169.254.169.254` / link-local / CGNAT / `0.0.0.0` / NAT64 denied by
  **resolved IP** (pre-flight and at dial time, closing DNS-rebinding),
  tighten-only.

### Sub-agents

- **Sub-agent orchestrator** (ADR 0013): a sub-agent is the embeddable `Agent`,
  constructed through an internal orchestrator that threads the parent's `Mode`,
  `Approver`, `Confiner`, and guardrails verbatim (or stricter) with a tool
  **`Subset` ≤ the parent's** (ADR 0005). It is exposed as a
  dispatch-transparent **`sub_agent`** recursion point — never confined or gated
  as a unit; each child tool call gets the full per-call disposition one level
  down.
- **Isolated live guard state** (`Guards.ForSubAgent`): a sub-agent gets a fresh
  circuit-breaker and audit log over a shared read-only dangerous ruleset.
- Nested events re-emit into the parent stream at **`Depth = parent.Depth + 1`**.
- Stepping is **top-level-only for v1** behind a swappable driver; a sub-agent
  runs atomically within the parent Turn (no mid-sub-agent snapshot; cancel
  rolls back to the parent's pre-`sub_agent` boundary).

### MCP

- **MCP client** on the official Go SDK (`modelcontextprotocol/go-sdk` v1.6.1):
  stdio / SSE / streamable-http transports. Server tools surface into the
  registry as `ExternalEffectTool` of kind `mcp`, so they **Approval-gate in
  Auto** under `confine-to-workspace=true` (an external server Apogee cannot
  fence). **Resume reconnects fresh** — no server-side-state promise (ADR 0008).

### TUI

- **Nested-event rendering**: `Depth > 0` sub-agent events render as a framed,
  labelled block (Phase-2's "tolerate" → "render").

### Notes

- Cross-build stays green on all 6 targets (linux/darwin/windows ×
  amd64/arm64, `CGO_ENABLED=0`); OS-specific confinement is build-tagged behind
  the `denyConfiner` (Windows/other) fallback. **Windows confinement is Phase 5**
  — Auto is simply unavailable on Windows until then.
- The `internal/` packages never import the root module path (ADR 0010).
- Direct dependency additions this release: `golang.org/x/sys` (landlock),
  `github.com/google/shlex` (terminal command splitting),
  `github.com/modelcontextprotocol/go-sdk` (MCP client).

### Known post-release verification (owner-run / CI)

These confinement **enforcement** proofs cannot run in the development
environment and are deferred to an owner-run / CI verification after the tag.
They are not acceptance failures — the hermetic disposition/logic tests (caps
honesty, generated profile strings, command rewriting, fail-closed paths) run
on every host and pass, and the live escape-probe batteries **self-skip loudly**
where the OS cannot enforce:

- **Linux landlock live enforcement** — the dev-host kernel has
  `CONFIG_SECURITY_LANDLOCK` **off**, so `confinetest.Probe` self-skips here.
  Confirm on a landlock-enabled kernel (≥5.13 fs, ≥6.7 net) that a confined
  subprocess's out-of-workspace write and non-allowlisted connect are OS-denied
  while the parent stays unrestricted.
- **macOS seatbelt live enforcement** — ✅ **confirmed on macOS hardware
  (2026-07-02).** `confinetest.Probe` now runs under live `sandbox-exec` on a real
  Mac: a confined subprocess is fenced to the workspace, out-of-box and `~/.ssh`
  writes are OS-denied, the parent stays unrestricted, and network-deny tightens
  while network-open connects. (This surfaced and fixed the box-root canonicalization
  bug below.) The Linux landlock arm above is still open.
- **Live Auto-confined deliverable run** — the opt-in `APOGEE_LIVE_ENDPOINT`
  end-to-end run (a real coding conversation in Auto, a shell write outside the
  workspace OS-denied, an MCP tool still raising Approval, a sub-agent delegated
  and its nested work rendered) is owner-run on Linux (landlock) and macOS
  (seatbelt). *(Still open.)*
- **Box-root canonicalization** — ✅ **resolved (2026-07-02).** Was a real bug, not
  just a verification gap: seatbelt embedded box roots verbatim and denied every
  in-workspace write when the root passed through a symlink (macOS `/var`, `/tmp`).
  Fixed by resolving each writable root through symlinks in `seatbeltProfile`; see
  the `[Unreleased]` Fixes entry.

[1.0.0]: https://github.com/airiclenz/apogee/releases/tag/v1.0.0
