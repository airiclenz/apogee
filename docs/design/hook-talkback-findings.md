# Hook talk-back findings — can a user-configured reaction speak to the model?

**Provenance:** recorded 2026-09-07 at the owner's request to *research* one question, raised after
the Hooks feature shipped: should apogee's observe-only [Hook](../../CONTEXT.md) and its
[Mechanism](../../CONTEXT.md) lab surface become one user-facing surface offering three modes —
(1) event → external action, (2) event → external action → response to the model, (3) both. Three
parallel readers produced it: a rival survey, a seam analysis of this repo, and a doctrine check
against the settled ADRs. **Status: research only.** No decision is taken here, nothing is
designed, and no ADR is superseded. The next step, if the owner wants one, is a grill.

**The one-line answer:** the proposal is smaller and more orthodox than it first appears — it is a
single tier of a six-tier design space, it does not require reopening the veto decision, and its
execution machinery already exists and is contract-governed. What it costs is one structural
invariant, one bench arm, and a per-request prefix that local models pay for every Turn.

---

## 1. The design space, and where apogee sits

Stripped of naming, there are **six** architectures for "a user script that can affect the model".
Every rival surveyed is a point in this space or a bundle of points.

| Tier | What the reaction does | Who ships it |
|---|---|---|
| **A. Observation sink** | fire-and-forget; the loop never waits and never reads a reply | **apogee**, opencode `formatter`, Claude Code `async:` hooks |
| **B. Synchronous veto** | the loop blocks and reads a yes/no | Claude Code, Cursor, Codex, Crush, goose, pi |
| **C. Advisory injection** | the reaction returns *text* that becomes model-visible context | Claude Code, Cursor, Codex, Crush, aider |
| **D. Data mutation** | rewrites tool arguments, tool results, or the prompt itself | Claude Code, opencode, pi, Crush |
| **E. Continuation control** | decides whether the Turn *ends* | Claude Code, Crush, goose, aider, Cursor |
| **F. In-process embedding** | reaction is code in the agent's address space | opencode, pi, Claude Code Agent SDK |

apogee is at **A**, alone. The gap is named most precisely by goose's own open issue #10358, which
proposes closing it there and has not: *"hooks today can **block** … or **observe** (output
discarded) — but never **advise**."*

## 2. The proposal is tier C, and that is the finding that matters

The owner's three modes are **A and C**. Mode 1 is what ships today. Modes 2 and 3 are advisory
injection. They are **not** B, D or E.

That separation is load-bearing, because it decides how much doctrine has to move:

- **ADR 0073's rejection of "Tool-call policy or veto hooks" stays untouched.** That rejection
  borrows ADR 0071's closure of the second-Mechanism-surface door, which is the most expensive
  objection in the file. Tier C never asks for it.
- **ADR 0073 decision 4's "No `pre-*` event, ever" stays untouched.** Advisory text lands on the
  *next* request, which is a post-hoc moment. No pre-event is required.
- **What must move is ADR 0073 decision 2 and its third rejection**, "Post-edit lint fed back to the
  model — a benched Mechanism if it returns, never a Hook". That rejection names this use case
  directly and must be addressed rather than sidestepped.

Decision 2 does not shut the door; it *names the route*: "The day someone wants a Hook's output in
front of the model, that is a Mechanism and goes through the bench." So the feature is permitted
doctrine already. It is called a Mechanism and it carries a gate.

## 3. Feasibility — most of it exists

- **The execution door is built and contract-governed.** `tools.RunHookSubprocess`
  (`internal/tools/exec_common.go`) is the single sanctioned door for an in-loop hook to spawn a
  subprocess and consume its stdout as a payload, with confinement, process-group teardown, output
  cap, timeout clamp and the credential scrub. Built for the retired `autofix` row; the row went,
  the door stayed (`docs/design/confinement-execution-contract.md` §10).
- **The injection seam is built and publicly re-exported.** A `PreRequestHook` that shells out and
  calls `Request.InjectContext(stdout)` works today. `NewMechanismRegistry`, `AddExperimental`, the
  five hook interfaces and `Request` are all on the facade (`apogee.go`). A Go embedder needs no
  engine change. What is missing is a *configuration surface*, not a capability.
- **The Event stream cannot carry modes 2 and 3.** `EventSink.Emit` returns nothing by type, and one
  mutex serializes emission for the whole agent tree at every depth
  (`internal/agent/construct.go:148-165`). A reaction that waited there would stall every sibling,
  the streaming and the TUI. So the three modes split at runtime whether or not they are unified in
  config: mode 1 stays on the Event stream, modes 2 and 3 register as in-loop hooks. **One config
  shape over two execution paths** — the split is hideable, not removable.
- **The two execution paths have deliberately opposite trust postures.** A Mechanism subprocess runs
  confined, scrubbed and capped, because it fires on model activity. A Hook subprocess runs
  unconfined with the whole environment, because a desktop notifier needs a display and a message
  bus. A unified surface must say which posture each mode takes. The defensible line is that
  anything reaching the model takes the confined path.

## 4. What it costs

Four costs are generic to tier C, evidenced across the survey:

1. **It is a prompt-injection channel by construction.** A hook's stdout is typically derived from
   files, CI output or network responses — attacker-reachable text going straight into context.
   Claude Code's mitigation is partial and awkward: the model's own injection defences fire on
   imperative phrasing and surface the text to the user instead of using it, so the documented
   workaround is "write factual statements, not imperatives".
2. **Token cost and context pollution.** Hence Claude Code's 10,000-char cap with file spillover,
   Codex's ~2,500-token spill, and Crush's planned `context_files` (return paths, not contents).
3. **Replay staleness.** Injected text is transcript-persisted, so a resume replays a stale value
   rather than re-running the hook. Timestamps and commit SHAs go stale silently.
4. **It converts hooks from an ops surface into a model-behaviour surface.** That is the tier that
   needs evidence, and it is why every apogee record so far has routed it to the Mechanism rung.

Two costs are specific to apogee and appear in no rival, because no rival targets locally hosted
models:

5. **Prefix-cache instability.** Everything apogee puts at the front of a request today is
   session-stable by construction, down to `{{datetime}}` being date-only so a local server's prefix
   cache survives a Turn. Per-request injected text moves that prefix every Turn. This is a
   recurring, measurable cost on exactly the deployment apogee exists for.
6. **Forgery adjacency.** `InjectContext` folds into the system message when the tail is a tool
   result. Out-of-process text would then sit beside host-authored orientation — the exact adjacency
   ADR 0023 pushed the model's own task list *behind*.

## 5. Doctrine — what must be superseded, ranked

**Tier 1, structural. Needs ADR 0031's tiebreaker force argued down and 0031 superseded explicitly.**

- **Invariant 4, "benchable all the way up".** Its own worked example list names *trigger-injected
  prompts*. Partially mitigated — `internal/hooks` is already facade-exported, so the *library* is
  bench-drivable. Unmitigated in substance: **a per-user script's output is not a behaviour anyone
  can bench.**
- **The Reaction surface exclusivity rule** (`CONTEXT.md`), which defines rung 4 as "it changes
  nothing the model sees" and names the lint case as the worked rejection. Moving this is a
  domain-language change before it is a feature.
- **ADR 0073 decision 7, "The engine never waits on a Hook"**, plus decision 2's `Emit` returning
  nothing. The drop-under-overload dispatch is incompatible with a return channel.

**The counter-argument to invariant 4, which is the case the owner would have to make.** The same
unbenchability is true of `system-prompt-text`, and ADR 0064 §6 answered it not with a gate but with
a **standing obligation** on whoever writes the text: "a default prompt that makes any model worse
has moved the floor down and is a defect in the same sense a Mechanism regression is." The
principled version of this proposal is therefore: bench the *machinery* once — does running a script
and injecting its stdout hurt a model — and let the *content* carry the same standing obligation a
user's prompt already carries.

**Tier 2, settled decisions a new ADR could supersede with an argument.** ADR 0073 decision 2 (which
names its own reopening route) and rejection 3; ADR 0061 §4's B1 deferral, which demands explicit
supersession by its own terms; ADR 0033 decision 5 and ADR 0034 §8, the payload-discarded versus
payload-injected split, both of which cite invariant 4 and fall with it; ADR 0064 §5's placement
rule, which would need a fifth home for user-authored *dynamic* content.

**Tier 3, housekeeping.** ADR 0073 decision 1's terminology, the `Hook` glossary entry and its
`_Avoid_` list, `docs/manual/hooks.md`, `docs/manual/configuration.md`.

**Not obstacles, and one that gets worse.** ADR 0073 decision 5's denial of repo-local hook files
and the workspace exec fence become the *primary* defence rather than a nicety. But decision 6's
"the payload is not secret-scrubbed … the same trust as the screen — accepted" is an **outbound**
argument that does not transfer to an inbound channel. It must be re-argued from scratch, not
superseded.

**One precedent that is weaker than it looks.** "User-authored text is outside the floor" does not
hold: ADR 0064 §6 pulls the user's configured prompt *inside* it. A user can already push arbitrary
text into context through eight routes, so doctrine is comfortable with the *trust*. What it is
consistent about is *timing*: every existing route is either standing configured text or this turn's
content asked for by name. Four independent records send unbidden, dynamically generated text to the
Mechanism rung.

## 6. What the rivals' scars say

| | Claude Code | Cursor | Codex | opencode | pi | Crush | goose | aider |
|---|---|---|---|---|---|---|---|---|
| Events | 33 | ~22 | 11 | 20 | 36 | 1 | 12 | 2 fixed |
| Inject context | `additionalContext` | `additional_context` | `additionalContext` | rewrites prompt/system/history | ~14 channels | `context` | **none** | lint output as a user message |
| Timeout | 600s/30s/10s by event | per-hook | 600s | **none** | **none** | 30s | 30s | none |
| Fail posture | open, except in-process | open, `failClosed` opt-in | open | **no isolation** | open, `tool_call` closed | open only | open, `on_failure` opt-in | n/a |
| Loop guard | cap 8 | `loop_limit` | — | — | — | — | env cap | `max_reflections=3` |
| Repo-local config runs? | behind trust; **not in `-p`** | yes | behind trust | **no gate at all** | after `project_trust` | yes | yes | n/a |

Five lessons apogee can buy without paying for them:

- **Every implementation that shipped tier E needed a loop breaker**, and every one shipped it late.
  Tier C does not need E. Not building E is the cheapest safety decision available.
- **The projects with no timeout wedge.** opencode's dispatcher is a bare loop with no isolation, so
  one throwing plugin silently disables every later plugin, and a never-settling promise parks the
  turn forever — measured at 40 of 120 subagent dispatches lost. apogee's bounded per-hook timeout
  and drop-newest queue are already ahead of both in-process rivals.
- **Repo-local hook config is where the CVEs are.** CVE-2025-59536 / CVE-2026-21852 ran a cloned
  repo's hooks before the trust dialog could be read; opencode has no gate at all and a published
  proof-of-concept repo. ADR 0073 decision 5 denied this before it shipped, and that call looks
  better under talk-back, not worse.
- **The decision channel and the log channel must not be the same pipe.** goose and Claude Code both
  document the same trap: a stray `echo` on stdout makes a healthy hook read as "no decision", and
  it fails silently.
- **Name the audiences in the schema.** Cursor is alone in splitting `user_message` from
  `agent_message`. Claude Code splits by field convention across four audiences and has a
  troubleshooting entry for people who confused two of them.

Two things worth **not** copying: LLM-as-hook handler types (Claude Code's `prompt` and `agent`),
which produced the unbounded loop that burned a session quota over fifty minutes; and mutation tiers
where ordering becomes semantics — pi's issue #9175 shows that once a later hook can mutate arguments
after an earlier one approved them, no permission hook in the system can be sound.

## 7. What a decision still needs

1. **Does the bench arm exist?** "Bench the machinery once" is only honest if the arm is real. The
   arm is: a fixed script injecting fixed text, measured against Bypass, on the target class.
2. **What is the prefix-cache answer?** Injecting at a stable position, or only at Exchange
   boundaries, may recover most of it. Unstudied.
3. **Which mode takes which trust posture**, and whether one config entry may name both a confined
   in-loop reaction and an unconfined notifier.
4. **What happens on failure.** For advice-only, fail-open is obviously right: no advice is just no
   advice. That is worth writing down before someone proposes `failClosed`.
5. **How the injected text is fenced** so it cannot forge an engine header, on the model of ADR
   0026's `[workspace text]` fence.

## 8. Recorded denials

Ruled out by this research, so they do not come back as silent options:

- **Riding the Event stream for modes 2 and 3.** Type-level impossible, and a shared tree-wide mutex
  makes any waiting variant a full-tree stall.
- **The Approver as the channel.** `ApprovalDecision` is a bare enum; the deny text the model reads
  is engine-authored from a static guard rule.
- **The deferred-response queue as an outside door.** It is only reachable through a registered
  hook, so it reduces to the Mechanism seam with an extra Turn of latency.
- **Child interjection as a general seam.** It needs a live `sub_agent` call id, does nothing at
  depth 0, and `Interject` is not goroutine-safe.
