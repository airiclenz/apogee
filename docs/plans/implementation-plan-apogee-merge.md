# Apogee — Implementation Plan (apogee-code + apogee-sim merge)

**Date:** 2026-06-22 (revised 2026-06-23)
**Status:** Broad plan — revised 2026-06-22 after a `grill-with-docs` session, then
2026-06-23 after a `grill-me` session (see §6, "Resolved in the 2026-06-23 grill-me
session"). Those decisions have now been propagated into the ADRs/CONTEXT (ADRs 0006–0009
added; 0001/0003/0004 amended) — see the "Doc propagation — APPLIED" note at the end of §6.
**Source of direction:** the strategic-pivot handoff
(`apogee-sim/docs/handoffs/2026-06-22 - 02 - STRATEGIC-PIVOT...`).

This plan turns the strategic pivot into an actionable, phased build sequence. It is
deliberately *broad* — each phase will get its own detailed plan as we reach it.

> **Revised after grilling.** Several early assumptions changed. The authoritative
> records are now [`CONTEXT.md`](../../CONTEXT.md) (the glossary) and the ADRs in
> [`docs/adr/`](../adr/):
> - **0001** — the agent loop is an **embeddable library**; the bench (apogee-sim)
>   **imports it as a Go module** and drives the real loop in-process (not a serialized
>   headless protocol). No ambient process/filesystem state; bench isolation by default.
> - **0002** — tools are an open extension point; the Mechanism catalogue is curated.
> - **0003** — Mechanisms are a constraint-declared registry with a **deterministic total
>   order** (bench detects order-sensitivity).
> - **0004** — **Auto mode requires OS-level Confinement** as a **capability matrix** (Auto
>   needs fs-write *and* network confinement; no confinement ⇒ no Auto).
> - **0005** — sub-agent privileges are bounded by the parent (≤ parent).
> - **0006** — **Bypass mode** is the honest Mechanisms-off floor (= the bench's control arm).
> - **0007** — **Step/Turn** + the **quiescent boundary** (cancellation + recover-at-boundary).
> - **0008** — tools are **stateless across Turns**; MCP/network are non-forkable external
>   effects (bench disables them with deterministic stubs for v1).
> - **0009** — the **A/B decision rule** (non-inferiority gate + superiority selection,
>   A/A-calibrated δ, task-blocked design, asymmetric multiple-comparison discipline).
>
> Headline shifts: the **bench contract is the public Go API**, not `apogee headless`;
> Apogee ships as **product + library from one repo**; Mechanisms are classified by
> **hook point**; "sandbox" is a **bench** term (production uses Safety guardrails +
> Confinement).

> **Second grilling (2026-06-23, `grill-me`).** Further shifts now folded in below:
> **Bypass mode** (Mechanisms-off floor, orthogonal to Agent mode) is reinstated as the
> honest baseline; **Step/Turn** are defined (quiescent-boundary `Step()`); **forking is a
> bench concern, not an Apogee feature** (Apogee exposes only snapshot/resume + clean
> library hygiene); **Confinement is a capability matrix** (Auto needs fs-write *and*
> network confinement); the **Library keys on a confidence-tagged model fingerprint**.
> Full record + pending ADR/CONTEXT propagation in §6.

> ### ⚠️ Standing requirements (apply to every phase)
>
> 1. **Coding standards — `/coding-standards` is mandatory for all new code.** At the
>    start of any coding work, load the skill (`coding-standards.go.md` +
>    `testing.go.md`) and follow it for naming, formatting, error handling, tests, and
>    security. This is a gate on every phase and every PR, not a suggestion.
> 2. **Minimal external-tool dependencies.** Apogee's promise is a *single static
>    binary*. Prefer in-process Go (stdlib / pure-Go) over shelling out to external
>    programs. Any external program (ripgrep, gofmt/goimports, black, prettier, tsc,
>    linters, even `git`) must be an **optional enhancement** — detected at runtime,
>    with graceful degradation when absent — never a hard requirement for core
>    function. Keep the Go module graph lean too (see §3a). Distinguish *inherent*
>    deps the user explicitly invokes (the `terminal` tool's shell, `python-exec`'s
>    interpreter) from *convenience* deps we should avoid hard-wiring.
> 3. **One deliberate exception: Auto-mode Confinement (ADR 0004).** OS-level confinement
>    is OS-specific and partly external (macOS `sandbox-exec`), which tensions the
>    single-binary promise. Accepted and bounded: the core loop + Plan + Ask-Before run
>    with zero external deps; only **Auto** depends on a confinement facility, and when
>    that facility is absent, **Auto is refused, never run unconfined.**

---

## 1. Goal & guiding principles

**Goal:** one cross-platform Go binary — a terminal coding agent for small local
models — that merges apogee-code's complete agent loop with apogee-sim's
small-model mechanisms and eval harness. No separate proxy.

**Principles that order the work:**

1. **Library-first core; everything is a consumer of it (ADR 0001).** Build the agent
   as an **embeddable, steppable Go library** — `prompt in → loop → typed events out`,
   no UI dependency, no ambient process/filesystem state. The **TUI**, the optional
   `apogee headless` CLI, and the **bench** are all consumers of one public package.
   The bench (apogee-sim) **imports Apogee as a Go module** and drives the real loop
   in-process, which keeps the **eval loop alive throughout the rewrite** — we can A/B
   every change against real models from day one. The repo is the **whole tool**
   (`cmd/apogee` TUI + CLI), *and* the library — not just one. *(A minimal TUI shell is
   stood up early — see Phase 2 — but it is always a view over the library core.)*
2. **Reuse Go, port TS against an oracle.** apogee-sim's Go drops in directly.
   apogee-code's TS is a *behavioral spec*: keep the TS as a reference oracle and
   validate Go parity with the sim — especially for the riskiest layer, `processing/`.
3. **Mechanisms move into the loop.** apogee-sim's mechanisms were proxy-bound; the
   highest-value ones (e.g. `correct_tool_result`, `truncate_history`) were lab-only
   because the proxy didn't own the loop. Once the agent owns the loop, they become
   first-class — but each must be **re-validated in situ** via the sim, not assumed.
4. **Cross-platform from the design stage.** Shell execution and path handling
   (Windows vs POSIX) is a real risk; design the shell/path abstraction early even if
   v1 ships POSIX-first.
5. **Gate everything; measure everything.** Every mechanism stays gated by
   conversation state and self-suppresses if it stops helping. Don't carry a mechanism
   forward on faith — carry it forward on a sim A/B.
6. **In-process first, external tools optional.** Prefer Go stdlib / pure-Go over
   shelling out. External programs are runtime-detected enhancements that degrade
   gracefully — never hard prerequisites for core function (see Standing Requirement 2
   and §3a).
7. **`/coding-standards` for all new code.** Non-negotiable; load and apply the Go
   variants before writing any code (see Standing Requirement 1).

---

## 2. Source-repo inventory (what we're merging)

### apogee-sim (Go — reuse directly)
`internal/` packages, roughly grouped:

- **Mechanisms / transforms:** `compress`, `cot`, `decompose`, `library`,
  `toolfilter`, `syntax`, `autofix`, `validate`, `intent`, `filehint`, `grammar`,
  `codeinfo` *(deprioritized — modest measured effect, superseded by shell-out
  diagnostics)*.
- **Plumbing:** `backend` (LLM-server detection), `httpx`, `metrics`, `config`,
  `logger`, `pipeline`, `transformlog`, `toolsets`, `management`, `setup`.
- **Eval / simulation harness (crown jewel — stays in the bench repo):** `sim`,
  `bench`, `eval` — trace archive, classifier, fork/stepwise/sweep counterfactuals,
  `RealSandbox`, scorers, intervention surface, failure taxonomy. **Decision (ADR 0001):**
  this is *not* pulled into apogee; **apogee-sim stays the bench and reaches Apogee by
  importing it as a Go module** — driving the *real* embeddable loop in-process (owning
  the sandbox, stepping turns, forking via session snapshot/resume, registering
  experimental hooks). The shipped binary links none of the bench's code. The lab-only
  intervention surface (`correct_tool_result`, `truncate_history`) becomes first-class
  Mechanisms once the agent owns the loop; the portable-tier interventions become
  in-process **experimental hooks** the bench registers (see §6, §7).
- **Retire (decided):** `proxy` (the OpenAI-compatible reverse proxy) and the
  `bridge` / `opencode-plugin`. None are ported forward; they remain in apogee-sim's
  git history as reference. apogee *is* the integration now.

Module: `github.com/airiclenz/apogee-sim` (Go 1.26). New module: `github.com/airiclenz/apogee`.

### apogee-code (TypeScript — re-implement in Go, TS as oracle)
`src/` subsystems (79 TS files in `src`, only 16 import `vscode` — coupling is light
and edge-concentrated):

- **orchestrator/** (loop core, *zero vscode* in the heart): `orchestrator.ts`,
  `loop-controller.ts`, `conversation-state.ts`, `context-compactor.ts`,
  `sub-agent-orchestrator.ts`, `agent-mode-manager.ts`.
- **tools/** (~30 tools, only 5 import vscode): file create/read/edit, find-replace
  (single + multi), directory-list, glob/grep search, git (branch/commit/diff-range),
  terminal, http-request, web-fetch/search, python-exec, diff, open-file, sub-agent,
  ask-user, diagnostics — plus `tool-registry`, `tool-executor`, `approval-manager`,
  `path-safety`.
- **processing/ ×8 (RISKIEST PORT — *zero vscode*):** `response-processor`,
  `tool-call-parser`, `native-tool-parser`, `markdown-fenced-parser`,
  `custom-regex-parser`, `thinking-processor`, `thinking-stripper`, `processor-factory`.
- **providers/:** `openai-compatible-provider`, `model-provider`, `model-discovery`,
  `provider-factory`, `server-process-manager`.
- **context/:** `budget-manager`, `context-builder`, `file-reference-resolver`,
  `file-completions`.
- **sessions/:** `session-manager`. **mcp/:** `mcp-client-manager`, `mcp-tool-bridge`.
- **security/:** `audit-log`, `circuit-breaker`, `response-hasher`,
  `tool-argument-guard` (+ `tools/path-safety`, `url-safety`, `approval-manager`).
- **Discard (UI glue, ~2,700 coupled lines):** `webview/`, `chat/`, `inspector/`,
  `extension.ts` → replaced by Bubble Tea TUI + Cobra CLI.

### VS Code API → Go-native seam map
| Seam | VS Code API | Go-native replacement |
|---|---|---|
| Config | `getConfiguration`, `onDidChange…` | config files: `~/.apogee/` + workspace `.apogee/` |
| Events / render | `postMessage`, `EventEmitter` | Bubble Tea `Msg`/`Cmd`; Go channels |
| Workspace / FS | `workspaceFolders`, `findFiles` | **pure-Go `fs.WalkDir` + `regexp`** for search/glob; ripgrep used only if detected on PATH |
| Prompts / UI | `showInputBox`, `showQuickPick` | Bubble Tea prompts |
| Logging | `createOutputChannel` | file logger + TUI pane |
| Code intel | `languages.getDiagnostics` | **in-process where the stdlib allows** (`go/parser`, `go vet` ships with Go); other compilers/linters (`tsc`, etc.) are optional shell-outs, detected + graceful-degrading |
| Editor | `activeTextEditor`, `showTextDocument` | degrade/drop — print paths, fs-walk completions |

---

## 3. Target architecture (proposed Go layout)

```
apogee/
├── apogee.go              # PUBLIC API facade (root package): Agent + Config, Run/Step,
│                          #   Event types, Session snapshot/resume, hook-point interfaces,
│                          #   public Tool interface. The bench + embedders depend on this.
│                          #   (Thin facade over internal/ — mirrors apogee-sim's apogee.go.)
├── cmd/apogee/            # Cobra entrypoint: root TUI + subcommands (run, library, probe…)
│                          #   `headless` is an OPTIONAL user/scripting surface, NOT the
│                          #   bench contract (the Go API is — ADR 0001).
├── internal/
│   ├── agent/             # orchestrator: loop-controller, conversation-state, modes
│   │   ├── loop/          #   the agent loop — embeddable, steppable, NO ambient state;
│   │   │                  #   owns tool dispatch + typed event emission
│   │   ├── subagent/      #   sub-agent orchestration (privileges ≤ parent — ADR 0005)
│   │   └── modes/         #   Plan / Ask-Before / Auto (Auto requires Confinement — ADR 0004)
│   ├── provider/          # openai-compatible client, model discovery, server-process mgr
│   ├── processing/        # PORT-RISK: harmony channels, fenced/native tool-call parsing
│   ├── tools/             # the ~30-tool suite + registry/executor (Tool iface is public)
│   ├── context/           # Budget, Context builder, Compaction (generative, default reducer)
│   ├── session/           # session save/load/resume (= the bench's snapshot/restore)
│   ├── mcp/               # MCP client (official go-sdk): stdio / SSE / streamable-http
│   ├── security/          # Safety guardrails: approval, audit, circuit-breaker,
│   │                      #   path/url safety, arg guard (human-in-the-loop; NOT a sandbox)
│   ├── mechanisms/        # ← re-homed to fire at loop hook points, as a CONSTRAINT-DECLARED
│   │   │                  #   REGISTRY (ADR 0003), classified by hook point not by old kind.
│   │   │                  #   Catalogue mapping = a dedicated sim-data session before Phase 4.
│   │   ├── prerequest/    #   library, decompose, toolfilter, tool-result-capping,
│   │   │                  #   read-loop family, file_hint, correction (deferred), cot nudges
│   │   ├── postresponse/  #   validate, syntax, autofix, read_repeat, tool_loop,
│   │   │                  #   tool_use_enforcer + empty_response_recovery (EXEMPT off-ramps)
│   │   ├── pretoolexec/   #   cached_content_intercept (relocated — hypothesis)
│   │   ├── posttoolresult/#   correct_tool_result (now first-class), error_enrichment (reloc?)
│   │   └── historyrewrite/#   truncate_history (cheap reducer, off by default)
│   ├── platform/          # shell + path abstraction (POSIX/Windows) + Confiner interface
│   │                      #   (seatbelt / landlock / AppContainer) — ADR 0004
│   └── tui/               # Bubble Tea views/models — a thin renderer over agent events
│                          # NOTE: no in-tree eval — apogee-sim is the bench and IMPORTS
│                          #       the public Go API (§7); binary links no bench code.
├── docs/{adr,plans,handoffs}/
└── go.mod                 # github.com/airiclenz/apogee
```

**Key architectural seams:**
- **The public Go API (the single most important seam — ADR 0001).** `Agent` + `Config`,
  `Run`/`Step`, the typed **Event** values (token, tool-call, tool-result,
  approval-request, mechanism-fired, error), Session snapshot/resume, and the hook-point
  interfaces. The TUI and the **bench** consume Events **as Go values in-process**; the
  optional `apogee headless` CLI serializes them for scripting. Get this surface right
  first — it is the contract the bench and third-party embedders depend on. It must be
  embeddable and steppable with **no ambient process/filesystem state** (state roots are
  injected via `Config`).
- **Mechanism hook points (ADR 0003).** Five attach points — **pre-request,
  post-response, pre-tool-exec, post-tool-result, history-rewrite** — and a
  **constraint-declared registry** (not a fixed pipeline): a Mechanism declares its hook,
  descriptor, and ordering constraints, and the loop orders them from those. The
  pre-tool-exec / post-tool-result / history-rewrite hooks are new — the proxy never had
  them, which is why `correct_tool_result` was lab-only.
- **Platform abstraction.** All shell/path access **and Confinement** go through
  `platform/` so Windows is one interface to implement, not a call-site audit. The
  `Confiner` (seatbelt / landlock / AppContainer) gates Auto mode (ADR 0004).

### 3a. Dependency policy (Standing Requirement 2, made concrete)

**External programs — runtime-detected, optional, graceful:**

| Capability | In-process default | Optional external (if on PATH) |
|---|---|---|
| Code/file search & glob | `fs.WalkDir` + `regexp` | ripgrep (`rg`) for speed |
| Go syntax / vet | `go/parser`, `go/format`, `go vet` (ship with Go) | — |
| Formatting / autofix | gofmt via `go/format` for Go | goimports, black, prettier, rustfmt — *enhancement only* |
| Diagnostics (other langs) | structural checks (brackets, truncation) | `tsc`, language linters — *enhancement only* |
| Git ops | start by shelling to `git` (ubiquitous); evaluate `go-git` if we want to drop even that dep | `git` |
| `terminal` / `python-exec` tools | n/a — these *are* the external invocation the user asked for (inherent dep) | shell / python |
| Auto-mode Confinement (ADR 0004) | Linux **landlock** — fs confinement is kernel ≥5.13, but **network confinement needs landlock ABI v4 / kernel ≥6.7**; Auto requires *both* (see §6) | macOS **`sandbox-exec`** (system binary, fs+network); Windows AppContainer (Phase 5) |
| Model fingerprint (Library keying) | **pure-Go GGUF tensor hash** (target) for a definitive weights ID when the file is reachable; behavioral `apogee probe` fingerprint otherwise | `llama-gguf-hash --uuid` (interim, if on PATH) |

Rule: a fresh binary on a bare machine must still **read, edit, search, and run the
agent loop** (in Plan / Ask-Before) with zero external programs installed. Anything beyond
that is a detected bonus, and its absence is logged, not fatal — **except Auto-mode
Confinement (ADR 0004): its absence is not a silent bonus, it disables Auto** (degrade to
Ask-Before). Apogee never runs a tool call unsupervised *and* unconfined.

**Go module dependencies — keep the graph lean:**
- Justified: Cobra (CLI), Bubble Tea + Lipgloss + Bubbles (TUI), the official MCP
  `go-sdk`, `yaml.v3` (already used by apogee-sim), and small utilities already in
  apogee-sim's `go.sum` (`shlex`, `ulid`).
- Default to stdlib: `net/http` (providers, web tools), `encoding/json`, `os/exec`,
  `io/fs`, `regexp`. Don't add a library where ~50 lines of stdlib will do.
- Every new direct dependency is a deliberate choice noted in the phase's detail plan,
  not an incidental `go get`.

---

## 4. Phased build sequence

### Phase 0 — Scaffold & architecture (foundation)
- Stand up the Go module (`github.com/airiclenz/apogee`), Cobra root command,
  CI/build, and the package skeleton above.
- **Do not** bulk-import apogee-sim's packages. The bench stays its own repo (ADR 0001);
  mechanism packages are ported deliberately in Phase 4, copied in when each one is
  validated — not vendored wholesale up front.
- **Design the public Go API** (`apogee.go` root facade): `Agent` + `Config`, `Run`/`Step`,
  typed **Event** values, Session snapshot/resume, and the **hook-point registry
  interfaces**. This is the architectural keystone — the contract the bench (via Go import)
  and third-party embedders depend on. Bake in the hard constraints: **embeddable,
  steppable, no ambient process/filesystem state** (every state root injected via `Config`).
- Define the `platform/` interface: shell + path (POSIX impl first; Windows stub) **and the
  `Confiner` interface** (stub backends; real ones land Phase 3 — ADR 0004).
- **Validate the seam early:** a throwaway in-process harness that constructs an `Agent`,
  `Step`s it, snapshots + resumes, and registers an experimental hook — proving the bench's
  access pattern works before real subsystems exist.
- **Define Step/Turn precisely (new — grill-me):** a **Turn** = one loop iteration (one
  *primary* Upstream call; Compaction's call is internal); **`Step()`** advances one Turn
  and returns at a **quiescent boundary** (no in-flight stream/tool, fully serializable) —
  snapshot/fork are valid *only* here. Sub-agent stepping is **top-level-only for v1**, via
  a swappable driver so nested stepping drops in later; the snapshot schema leaves room for
  a suspended sub-agent.
- **Bake in the Bypass flag and the stateless-tool contract (new):** `Config.Bypass`
  selects the empty nudge/repair Mechanism set (and makes the Library inert); the public
  `Tool` interface declares **stateless-across-Turns** (only durable effect = filesystem
  writes; nothing live held across the quiescent boundary).
- **Forking is *not* an Apogee feature (clarified):** Apogee exposes only snapshot/resume
  (a user feature) + clean-library hygiene (Config-injected roots, no globals, copyable
  state, injectable tool registry, hook interfaces). The bench *composes* forking from
  those — there is no fork API in the binary.
- **Deliverable:** repo builds; `apogee --help` runs; public API + platform seams exist;
  the in-process step/snapshot/hook pattern is exercised by a test.

### Phase 1 — Embeddable agent core (highest-value first step)
Port apogee-code's loop as an embeddable vertical slice (TS as oracle):
- provider (openai-compatible) + model discovery,
- the agent loop + conversation-state + tool dispatch, emitting typed **Events**,
- a **minimal tool set** (file read/write, directory-list, pure-Go grep) behind the
  registry — no external programs required for this slice (§3a),
- `processing/` enough to parse one tool-call format end-to-end,
- Session save/load/resume (it doubles as the bench's snapshot/restore).
- Expose all of this through the **public Go API**; `apogee headless` (serialized
  events to stdout) is an **optional** scripting surface built on top — not a gate, and
  **not** the bench contract.
- **Point apogee-sim at the Go API immediately:** apogee-sim imports
  `github.com/airiclenz/apogee`, constructs an `Agent` against an isolated Library/session
  dir, steps it, and scores it — keeping the eval loop alive for the rest of the build
  with no eval code inside apogee.
- **Co-dev workflow (new — grill-me):** apogee-sim uses a `go.mod replace` → local apogee
  path during active development (the bench measures the working tree); a pinned
  version/commit is used only for archived A/B evidence. The public API is **v0.x with no
  stability promise** through Phase 3.
- **Deliverable:** a local model completes a simple file-edit task; the bench drives,
  steps, snapshots, and scores it **in-process via the library API**.

### Phase 2 — Minimal modular TUI shell
- Build a thin Bubble Tea app over the Phase-1 **Events** (consuming the public API like
  any other consumer): input box, streaming output pane, tool-call/approval display,
  status line. The TUI supplies the **Approval** delegate.
- Keep it deliberately simple and modular (clean model/update/view split) so it grows
  cleanly. **No agent logic in the TUI** — it only renders events and sends user input.
- **Deliverable:** you can hold a real coding conversation with a local model in the
  terminal, watch tools run, and approve writes.

### Phase 3 — Full subsystems
- Complete the **30-tool suite** (git, terminal, web-fetch/search, python-exec,
  sub-agent, ask-user, diagnostics, find-replace family) behind the **public `Tool`
  interface** (tools are an open extension point — ADR 0002). Apply §3a per tool:
  search/glob pure-Go (ripgrep optional); diagnostics in-process for Go, optional
  shell-out otherwise; each external dependency detected with graceful degradation.
  **Sub-agent** orchestrator is constructed with the parent's mode/approval/confiner —
  privileges **≤ parent** (ADR 0005); do **not** port apogee-code's gate-less version.
- **MCP client** on the official Go SDK (`modelcontextprotocol/go-sdk`) — stdio / SSE /
  streamable-http; re-verify SDK maturity at this point.
- **Agent modes** (Plan / Ask-Before / Auto) + **Safety guardrails** (approval, audit,
  circuit-breaker, path/url safety, arg guard). **Implement the `platform/` `Confiner`
  backends for v1 targets — macOS (seatbelt) + Linux (landlock).** Confinement is a
  **capability set, not a binary** (new — grill-me): **Auto requires fs-write *and* network
  confinement**, so Linux Auto needs **landlock ABI v4 / kernel ≥6.7** (5.13–6.6 ⇒ Auto
  refused → Ask-Before; **no `--auto-allow-network` escape**). The invariant is
  **per-tool**: a tool runs unsupervised only if confined, so **MCP tools (which Apogee
  cannot confine) gate through Approval even in Auto.** Default box = workspace-write-only +
  network default-deny + per-project allowlist. (Sessions/resume already landed in Phase 1
  as a core seam.)
- Finish the riskiest **`processing/`** port (all tool-call formats, thinking/harmony
  channels) and validate parity against the TS oracle + the bench.
- **Deliverable:** feature-parity with apogee-code's non-UI behavior, with Auto mode
  confined on Mac/Linux. **Cut `v1.0.0` of the public Go API here** — every consumer (TUI,
  bench, optional `headless`) has now exercised the surface; semver guarantees begin
  (Events/hook-points kept additively extensible — minor bumps for new variants).

### Phase 4 — Merge apogee-sim mechanisms into the loop
**Prerequisite (dedicated session):** map the apogee-sim catalogue onto the five hook
points **driven by real sim traces** — including the relocation hypotheses
(`cached_content_intercept`→pre-tool-exec, `error_enrichment`→post-tool-result) and the
exempt off-ramps (`tool_use_enforcer`, `empty_response_recovery`) the original plan
omitted. The plan's `mechanisms/` layout is provisional until this lands.

Then port each mechanism as a **module in the constraint-declared registry** (ADR 0003) —
declaring hook point, descriptor, ordering constraints — and **A/B-validate via the bench
before keeping it on**:
- the boring-effective robustness stages first (syntax/autofix, validation/auto-retry,
  completion nudges) — these carried most of the measured win. Keep autofix's external
  formatters optional per §3a: in-process gofmt always; goimports/black/prettier/rustfmt
  only when present,
- the **now-native loop interventions** the proxy could never host: `correct_tool_result`
  (post-tool-result) and `truncate_history` (history-rewrite, **config-gated, off by
  default** — the cheap A/B alternative to generative Compaction),
- the **Library** (cross-session per-model learning), decompose, tool-filter, file hints,
  read-loop family. **Bench runs use an isolated/ephemeral Library** (ADR 0001) — sim never
  floods production,
- context-reduction is the **four-way split**: Budget (context/) · Tool-result capping
  (pre-request mechanism, the surviving half of `compress`) · **Compaction** (context/,
  generative, **default**) · History truncation (mechanism, off by default). Retire
  `compress`'s external-client-compaction sniffing (no external client now),
- Adaptive Suppression / Turn Budget so each mechanism self-gates.
- **Baselines & validation discipline (new — grill-me):** the aggregate floor is **Bypass**
  (all nudge/repair Mechanisms off, structure on — the *same code path* users can run);
  per-Mechanism attribution is **leave-one-out**; off-ramps earn exempt status by their own
  leave-one-out. Mechanism ordering is a **deterministic** topo-sort (stable tiebreak by
  canonical ID); the bench flags order-sensitivity among undeclared co-firing pairs. The
  **Library** needs a **longitudinal** experiment (a *sequence* of sessions sharing one
  ephemeral Library; gate on "improves over sessions AND never below baseline"), and keys on
  a **confidence-tagged `ModelFingerprint`** (confidence gates injection).
- **Deliverable:** measurable lift on the bench's hard tasks; each mechanism backed by
  an A/B, not by faith.

### Phase 5 — Cross-platform hardening & retirement
- Implement the **Windows** shell/path backend **and the Windows `Confiner`**
  (AppContainer / Job Objects / restricted tokens) behind `platform/`; test the matrix
  (the real cross-platform risk). The interfaces were designed in Phase 0, so this is
  implementing them — but the Confiner backend is genuine new work (and **Auto mode on
  Windows is gated on it**, per ADR 0004).
- Add `apogee probe` (model capability probing → auto profile) and adaptive prompt
  complexity (from `apogee-sim/mission.md`). **`apogee probe` does double duty (new):** the
  same battery yields the **behavioral model fingerprint** (fuzzy feature match, not a
  response hash; logprobs preferred when exposed) used for Library keying when the GGUF file
  is unreachable.
- **Retire the proxy and the OpenCode plugin / transform-server bridge** (decided —
  not ported forward; they remain in apogee-sim's history).
- **Deliverable:** cross-compiled binaries for Win/Mac/Linux, Auto confined on all three.
- *(The eval harness lives in apogee-sim and reaches Apogee by Go import — **decided, not
  deferred** (ADR 0001). There is no `sim`/`bench` subcommand in apogee; an opt-in
  reduced-weight bleed of sim observations into the production Library may be added later
  if it proves worthwhile.)*

---

## 5. Cross-cutting risks (design for these early)

| Risk | Why it's hard | Mitigation |
|---|---|---|
| `processing/` port | fiddly string logic (harmony channels, fenced/native parsing), currently TS-tested | keep TS as oracle; port test vectors; validate parity via the bench |
| Windows shell exec **+ Confiner** | shell is POSIX-shaped; Windows OS confinement (AppContainer) is a different model and genuine new work | `platform/` shell/path **and `Confiner`** interfaces from Phase 0; Windows backend in Phase 5 |
| **Auto-mode Confinement** | OS confinement is OS-specific + partly external; must not become a silent hard dep | ADR 0004: landlock (Linux, in-kernel) / seatbelt (mac) for v1; **Auto refused, not unconfined, when unavailable**; own design session |
| Mechanism re-validation | proxy-era effects may not transfer to in-loop firing | bench A/Bs each one (Phase 4); never carry forward on faith |
| **Public Go API stability** | the bench *and* third-party embedders depend on it; costly to change later (ADR 0001) | design first (Phase 0); minimal guarded surface; **semver**; typed Events; everything else `internal/` |
| **Library pollution** | bench drives the *real* loop, so the real Library mechanism could flood production with sim data | ADR 0001: Config-injected state roots; **bench isolation by default**; opt-in bleed only if proven |
| MCP SDK maturity | Go SDK is new and moves fast | re-verify version/maturity at Phase 3; mark3labs fallback if needed |
| External-tool creep | easy to silently `os/exec` a formatter/linter and quietly require it | §3a: in-process default, detect + degrade, log absence; review tool deps each phase |
| Dependency bloat | incidental `go get` grows the binary & supply-chain surface | §3a: stdlib-first; every direct dep is a noted, deliberate choice |
| **Library cross-session harm** | the Library injects learned content across sessions, where per-Session safety nets reset clean; bad/mismatched observations could persist and hurt the model | confidence-tagged `ModelFingerprint` (prefer-not-to-inject under uncertainty); TTL + Bayesian counter-evidence; **longitudinal bench gate** (never below baseline) |
| **Non-deterministic Mechanism order** | a partial order + Go's randomized map iteration ⇒ non-reproducible runs & A/B noise | deterministic topo-sort with stable canonical-ID tiebreak; bench flags order-sensitive undeclared pairs |
| **In-process bench fragility** | the bench drives the loop in-process, so a `panic` in a Mechanism/tool can abort a long counterfactual sweep | *(open — see §8)* decide on a recover-per-Step boundary |

---

## 6. Decisions (resolved 2026-06-22)

Strategic decisions from the pivot:

1. **Language & module path — DECIDED: Go, `github.com/airiclenz/apogee`.**
2. **Eval harness packaging — DECIDED (ADR 0001): library import, not a subcommand.** The
   harness stays in **apogee-sim**, which reaches Apogee by **importing it as a Go module**
   and driving the *real embeddable loop in-process*. No `sim`/`bench` subcommand and no
   serialized-headless bench protocol. Apogee ships as **product + library from one repo**.
3. **Windows scope — DECIDED: POSIX-first, Windows fast-follow.** Design the `platform/`
   abstraction (shell/path **+ Confiner**) in Phase 0; ship Mac/Linux for v1; Windows in
   Phase 5. *(Note: the Windows Confiner is genuine new work, not just an interface impl.)*
4. **OpenCode plugin / transform-server bridge — DECIDED: retire.** Not ported forward;
   remains in apogee-sim's git history. apogee *is* the integration.
5. **TUI framework — DECIDED: Bubble Tea (Charm stack: Bubble Tea + Lipgloss + Bubbles,
   Cobra for CLI).**

Resolved in the 2026-06-22 grilling session (see ADRs):

6. **The bench contract is the public Go API**, not `apogee headless` (ADR 0001). The loop
   is embeddable, steppable, with **no ambient process/filesystem state**; `apogee headless`
   drops to an optional user/scripting feature.
7. **Mechanisms are classified by hook point** (pre-request / post-response / pre-tool-exec
   / post-tool-result / history-rewrite) and live in a **constraint-declared registry**
   (ADR 0003). The detailed catalogue mapping is a dedicated sim-data session before Phase 4.
8. **Tools are an open extension point; the Mechanism catalogue is curated** (ADR 0002).
9. **Auto mode requires OS-level Confinement** (ADR 0004); unavailable ⇒ Auto refused.
   **Sub-agent privileges ≤ parent** (ADR 0005).
10. **Context reduction is a four-way split** — Budget / Tool-result capping / **Compaction
    (generative, default)** / History truncation (mechanical, off by default).
11. **Bench isolation by default** (ADR 0001): sim never reads/writes the production
    Library; opt-in reduced-weight bleed only later if proven.

### Resolved in the 2026-06-23 `grill-me` session

12. **Hard constraint is a bench-time, ground-truth, distributional gate** — provable, the
    real guarantee. **Production self-regulation (Adaptive Suppression + Turn Budget) is
    proxy-only** (file activity, errors, loops) and explicitly *weaker* — a safety net, not
    a correctness promise. Reword `CONTEXT.md` so "without Apogee" is not read as the naked
    model (Budget/Compaction are structural and load-bearing; a true naked model just
    overflows the window).
13. **Bypass mode (reinstated).** A `Config` flag (orthogonal to Agent mode) that disables
    the `proactive-nudge` + `response-repair` Mechanisms and makes the **Library inert (no
    inject, no observe/write)**, but **keeps exempt off-ramps** so the floor is *functional*
    (a gate that quits at the first stumble would pass trivially). Bypass *is* the bench's
    aggregate control arm — same code path. Off-ramps still earn exempt status via their own
    **leave-one-out** A/B (exempt-from-suppression ≠ exempt-from-validation).
14. **Three baselines, three claims:** *Bypass* (aggregate floor / "never worse" gate),
    *leave-one-out* (per-Mechanism attribution), *product baseline* (optional, "is Apogee
    worth it" vs another tool).
15. **Step / Turn / Exchange defined.** **Turn** = one loop iteration (one *primary* Upstream
    call; Compaction's call is internal). **Exchange** = user input → final no-tool response.
    **`Step()`** advances one Turn to a **quiescent boundary** (no in-flight stream/tool,
    fully serializable); snapshot/fork valid only there; Approval + streaming happen *inside*
    a Step. **Sub-agent stepping is top-level-only for v1**, designed swappable for nested.
16. **Forking is a bench concern, not an Apogee feature.** Apogee exposes snapshot/resume
    (user feature) + clean-library hygiene; the bench composes forking / record-replay /
    counterfactuals / scoring. Reword ADR 0001 + §7 accordingly.
17. **Tools are stateless across Turns** (a public `Tool`-interface clause): only durable
    side effect = filesystem writes; nothing live held across the quiescent boundary;
    terminal/python-exec stay one-shot (matches apogee-code). **MCP + network are
    non-forkable external effects** (bench record/replay or disable; production resume
    reconnects fresh, no server-side-state promise).
18. **Public-API co-development.** `go.mod replace`/local during active dev (bench measures
    the working tree); pin a version/commit only for archived A/B evidence. **v0.x, no
    stability promise, through Phase 3; cut `v1.0.0` at end of Phase 3.** Events/hook-points
    **additively extensible** (new variant = minor bump). Seed types (e.g.
    `OrderingConstraints`) **move into apogee**; the bench imports them — never backward.
19. **Confinement is a capability matrix, not a binary** (sharpens ADR 0004). Each backend
    reports `{fs-write, network-egress, …}`. **Auto requires fs-write *and* network
    confinement** ⇒ Linux Auto needs **landlock ABI v4 / kernel ≥6.7** (5.13–6.6 ⇒ Auto
    refused; **no escape hatch**). Invariant generalized **per-tool**: unsupervised only if
    confined ⇒ **MCP gates through Approval even in Auto.** Default box = workspace-write-only
    + network default-deny + per-project allowlist.
20. **Mechanism ordering is a deterministic total order:** topo-sort + **stable tiebreak by
    canonical Mechanism ID** (never rely on map iteration); the **bench detects
    order-sensitivity** among undeclared co-firing pairs and surfaces the missing constraint
    (evidence-driven, not exhaustive pre-declaration).
21. **Library keying = confidence-tagged `ModelFingerprint`.** Resolution: **weights-hash
    (high) → behavioral probe (medium) → metadata label (low)**, best-available wins;
    **confidence gates injection** ("prefer not to inject under uncertainty" — dissolves the
    "unknown" bucket). Weights tier = **pure-Go GGUF tensor hash (target)**,
    `llama-gguf-hash --uuid` (interim), optional per §3a, cached by `(path,size,mtime)`,
    **weights-hash alone for v1**. Behavioral tier = the `apogee probe` battery (fuzzy
    feature match, not response hash; logprobs when exposed), cached by `(endpoint,model)`.
    The Library is validated by a **longitudinal** experiment (sequence of sessions, one
    ephemeral Library, never below baseline).

### Resolved in the 2026-06-23 `grill-me` session (continued — open items)

22. **The A/B decision rule (Phase 4's whole validation engine).** Resolved in seven parts:
    - **(a) Two tests, two postures.** **GATE = non-inferiority** (one-sided, *mandatory* =
      the hard constraint #12): ships only on **clear positive evidence it is not worse, down
      to the bench's measurement resolution δ**; *inconclusive ≠ pass* (burden on the
      Mechanism). **SELECTION = superiority** (separate; decides default-ON vs available-off).
      Rejected the "non-significant two-sided p ⇒ no harm" posture — it cannot *prove*
      never-worse and leaks slow-bleed regressions at small N.
    - **(b) δ is measured, not decreed.** Frequentist NI with margin δ = the bench **noise
      floor**, calibrated by an **A/A null run** (two identical arms; δ = an upper quantile of
      |per-task null delta|, e.g. 95th pct, or k·SD_null) — "not worse to the resolution we
      can measure," made operational. The A/A doubles as a **rig self-test** (non-zero center
      ⇒ broken pairing). Calibrate at **production temperature**; re-calibrate per
      (suite × model × temp). Rejected Bayesian P(not-worse)≥0.95 (needs a prior; drifts into
      "must look better" near the boundary).
    - **(c) Unit of analysis = TASK, blocked/paired on task.** N = number of *distinct tasks*,
      **not** tasks×runs — pooling runs into one 2×2 is **pseudo-replication** (the codeinfo
      N=40 fragility). Per-task statistic = ordinal-mean delta; test the T paired deltas
      (Wilcoxon signed-rank / paired-mixed). **Power comes from more distinct *discriminating*
      tasks, not reruns.**
    - **(d) Frozen, Mechanism-agnostic, discriminating suite.** Curate to the band where the
      model *sometimes* succeeds (always-pass/always-fail tasks add noise, no signal); pick
      **once, pre-registered**, independent of any Mechanism (per-Mechanism hand-picking =
      bench-overfitting). Grow task count until the A/A null band is tight — no magic N.
    - **(e) Disposition = one CI read against two lines (−δ and 0).** lower>0 ⇒ **default-ON**
      (superior); −δ<lower≤0 ⇒ **default-OFF, retained** (non-inferior, benefit unproven);
      lower≤−δ or straddling −δ ⇒ **reject — cannot ship**. Proven-neutral ⇒ default-OFF
      because it is **pure cost** (latency / tokens / complexity / ordering-graph / MC-budget)
      for zero measured benefit — "not worse" is *not* "free." Retain default-off with a
      **sunset rule** (retire after K suite/model refreshes of persistent neutrality).
      **Off-ramps are the exception**: judged on their **firing subpopulation** with a
      **recover-vs-dead-end** outcome (a full-suite average wrongly reads them neutral),
      earning **default-ON + exempt** by preventing catastrophic ends — this *is* #13's
      leave-one-out, now with population + outcome specified.
    - **(f) Asymmetric multiple-comparison discipline** (the dangerous error flips between the
      tests). **SELECTION → FDR (Benjamini–Hochberg, one-sided 0.05)** across the family —
      controls the fluke-fraction of default-ONs; FWER/Bonferroni is too strict and kills the
      modest real wins small models need, and a selection false-positive is merely
      useless-not-harmful. **GATE → per-Mechanism, *uncorrected*, stricter one-sided 0.025** —
      a per-Mechanism *safety* claim (correcting it would make safety depend on batch size).
      Safe without FWER on the gate via **three-layer defense**: a harmful Mechanism must also
      fluke FDR-controlled superiority to go ON *and* survive the aggregate Bypass floor.
      NI→superiority is a **closed/hierarchical** procedure (no extra α for the second look).
      Gate endpoint = **ordinal-mean only for v1**; binary good-rate non-inferiority held as
      an IUT **tightening** (no α penalty) if distribution-reshuffling pathologies appear.
    - **(g) Aggregate vs per-Mechanism composition.** The **aggregate Bypass NI test is the
      shipped guarantee** (full default-ON set vs Bypass: never-worse + ideally superior — the
      #12 gate, located at the *system* level, = #13's control arm). Per-Mechanism
      **leave-one-out *from the set*** is in-context attribution (captures interactions;
      catches a harmful Mechanism **masked** inside a net-positive set). The on-set is found by
      **greedy backward elimination** to a stable set (linear in N per round, **not** 2^N) —
      not by summing standalone A/Bs, because **the aggregate ≠ the sum of the parts**
      (Mechanisms interact; ties to ordering #20).

23. **Bench external effects (MCP/network) — disable-with-stub for v1; record/replay
    deferred** (resolves #17's open "record/replay *or* disable" and the §7 open item).
    **Grounding:** the bench *already* network-denies (`RealSandbox` netns / `sandbox-exec`;
    `curl`/`wget` return canned "Network is unreachable"), the task suite uses **no** external
    tools, and **no** replay infra exists — so "disable" is the honest status quo, not a
    regression.
    - **Disable external effects** (MCP + network/web) in bench runs. Record/replay does
      **not** enable fork-counterfactuals — a counterfactual *diverges by construction* ⇒
      cache-miss exactly when it does something new; replay's real value is **variance
      reduction in *whole-task* A/B** (same external responses both arms), not forking.
    - **Stub, don't unregister.** Keep web/MCP tools **in the model's menu** (faithful to
      production; matters for `toolfilter` and any menu-reasoning Mechanism) but back them
      with a **deterministic stub** returning a fixed result (network-unreachable / empty
      MCP), exactly as `RealSandbox` already does for `curl`. Unregistering would distort the
      menu and bias tool-selection evals.
    - **Protects #22's δ.** Live external flakiness would *widen the A/A noise floor* and
      pollute the δ calibration; deterministic stubs confine bench non-determinism to LLM
      sampling — the noise δ is meant to measure.
    - **Scope honesty.** A task that *requires* external content becomes always-fail under
      stubs ⇒ it falls out of the frozen discriminating suite (#22d). v1 validates Mechanisms
      on the **network-independent core**; external-dependent task validation is out of scope
      and flagged.
    - **Defer record/replay** to a demonstrated need (an external-tool task worth validating
      *and* whole-task A/B mode). Build the stub as a **single injectable seam** so replay
      slots in behind the same interface later.

24. **Open items #3 (lower-leverage) — resolved.**
    - **(a) Phase-1-has-no-UI ordering is correct.** The bench is a demanding consumer that
      forces the load-bearing *structural* seams (Step, snapshot, Events, approval delegate);
      the Phase-2 TUI reuses the same seams with interactive implementations, and TUI-specific
      additions are cheap under #18 (additive Events, v0.x). **One gap closed: cancellation /
      interrupt is promoted to a Phase-0 API primitive** (context-cancellation through `Step`,
      clean at the quiescent boundary) — the bench needs it (hard-cap / timeout) and the TUI
      needs it (user-stop), so it must not be a Phase-2 retrofit.
    - **(b) `processing/` parse-spec gate — non-issue.** Principle 2 already mandates "TS as
      oracle, port the test vectors, validate parity"; *that is the gate* — no `project-research`
      prerequisite, no extra ceremony (`project-research` stays an optional escalation only if
      specific TS behavior proves ambiguous during the port). The Phase-4 hook-point mapping is
      likewise **not load-bearing**: it is dynamic, tunable from sim results at any time — only
      the hook-point *interfaces* (already Phase-0) must be right early, not the placement.
    - **(c) In-process bench fragility — reframed as a contract property, resolved:**
      **`Step()` recovers at the extension boundary.** A panic in a tool or Mechanism is
      caught, converted to a typed `error` Event (failed tool-result / skipped Mechanism), and
      the loop degrades to the quiescent boundary rather than unwinding into the host. This is
      a **public-API contract** property (ADR 0002 opens tools to third parties; a faulty
      extension must not crash the embedding host — the `net/http`-per-request-recover
      analogue), Phase 0. The bench's "panic aborts a sweep" robustness falls out for free.

### Doc propagation — APPLIED 2026-06-23

These decisions are now propagated into the authoritative records (kept here as a
traceability map):
- **`CONTEXT.md`** — reworded the hard constraint to mean *Mechanisms-off (Bypass)*, not the
  naked model, proved at bench time (#12); added a **Bypass mode** entry (#13) and a
  **Turns and stepping** subsection defining Turn / Exchange / Step / quiescent boundary
  (#15); rewrote **Confinement** as a capability matrix with the per-tool invariant (#19);
  updated **Library** for confidence-tagged `ModelFingerprint` keying (#21).
- **ADR 0001** — separated "what Apogee exposes" (snapshot/resume + hygiene) from "what the
  bench composes" (forking/record-replay) (#16); added the co-dev / versioning consequences
  (#18).
- **ADR 0003** — added the deterministic total order + bench order-sensitivity detection (#20).
- **ADR 0004** — rewritten around the capability matrix, kernel ≥6.7 for Auto, per-tool
  invariant / MCP-in-Auto (#19).
- **New ADRs:** `0006` Bypass mode (#13); `0007` Step/Turn + quiescent boundary + cancellation
  + recover-at-boundary (#15 + the #24 contract additions); `0008` stateless tools +
  non-forkable external effects + disable-with-stub bench posture (#17, #23); `0009` the A/B
  decision rule (#22).

---

## 7. Validation strategy (the through-line)

apogee-sim stays the **bench** — not pulled into apogee, but not an afterthought either.
The contract that keeps it useful is the **public Go API** (ADR 0001): from Phase 1
onward, apogee-sim **imports `github.com/airiclenz/apogee`** and drives the *real*
embeddable loop in-process — constructing an `Agent` against an isolated Library/session
dir, `Step`ping it, **forking via session snapshot/resume**, registering **experimental
hooks** for discovery, and consuming **Events** as Go values. The failure taxonomy,
classifier, and fork-counterfactual rig in apogee-sim are the **inputs** to the merge —
they tell us which mechanisms to port, in what order, and whether each earns its place
(Phase 4 A/Bs). Keeping the bench in its own repo (depending on apogee, not vice-versa)
means the shipped binary links no bench code and the two repos evolve independently; the
coupling we must protect is the **Go API surface** (semver), and the invariant we must
hold is **isolation** — sim runs never touch the production Library. The optional
`apogee headless` CLI is a *user* scripting surface, decoupled from this contract.

**Boundary correction (grill-me 2026-06-23).** *Forking is a bench technique, not an Apogee
feature.* Apogee exposes only **snapshot/resume** (a real user feature) and **clean-library
hygiene** (Config-injected state roots, no process globals, copyable conversation state,
injectable tool registry, hook interfaces) — all justified as library hygiene, independent of
the bench. The bench **composes** forking, record/replay, and counterfactuals from those
primitives on its side; no fork/record code ships in the binary. **Three baselines for three
claims:** *Bypass* (Mechanisms-off floor, user-runnable) for the aggregate "never worse" gate;
*leave-one-out* for per-Mechanism attribution; an optional *product baseline* (model in
another tool) for "is Apogee worth it." For non-forkable external effects (MCP, network), the
bench uses **record/replay** (or disables them in counterfactual runs for v1 — *open, see
§8*); production resume reconnects fresh and makes no server-side-state promise.

## 8. Skills

**Required (Standing Requirement 1):**
- **`/coding-standards`** (`coding-standards.go.md`, `testing.go.md`) — **mandatory
  for all new Go**, every phase. Load it before writing code; it gates each PR.

**Done:**
- **`grill-with-docs`** — ✅ ran 2026-06-22; produced [`CONTEXT.md`](../../CONTEXT.md) and
  ADRs 0001–0005, and this revision. Stress-tested identity, the Mechanism model, the bench
  contract, the public API boundary, context management, and the safety model.

**Suggested for upcoming work:**
- **`project-research`** — pin the `processing/` parse-layer behavior precisely before
  porting it (the riskiest port).
- **`improve-codebase-architecture`** — when laying out packages (informed by Apogee's
  `CONTEXT.md` domain language).
- **`feature-implementation`** — per-subsystem ports.

**Two deferred deep-dive sessions (flagged during grilling):**
- **Hook-point catalogue mapping** — map the apogee-sim catalogue onto the five hook
  points *driven by real sim traces* (relocation hypotheses + exempt off-ramps). A
  prerequisite to Phase 4.
- **Confinement design** — the `platform/` `Confiner` across seatbelt / landlock /
  AppContainer (ADR 0004). Hard and OS-specific enough to stand alone.

**Open items for the next `grill-me` session (2026-06-23 handoff):**
1. ~~**The A/B decision rule**~~ — **RESOLVED 2026-06-23 → §6 #22** (two-test gate/selection,
   A/A-calibrated noise-floor δ, task-blocked design, asymmetric MC, aggregate-Bypass
   guarantee + leave-one-out attribution, greedy elimination).
2. ~~**Record/replay vs. disable-MCP/network-for-v1**~~ — **RESOLVED 2026-06-23 → §6 #23**
   (disable-with-stub for v1; replay deferred behind one injectable seam).
3. ~~**Lower-leverage**~~ — **RESOLVED 2026-06-23 → §6 #24** (a: ordering right, cancellation
   promoted to Phase 0; b: non-issue — TS-test parity *is* the gate; c: `Step()` recovers at
   the extension boundary as a contract property).
4. ~~**Apply the "Pending doc propagation" list in §6**~~ — **APPLIED 2026-06-23**: CONTEXT
   amendments + ADR 0001/0003/0004 edits + four new ADRs (0006–0009). See §6 "Doc propagation
   — APPLIED".
