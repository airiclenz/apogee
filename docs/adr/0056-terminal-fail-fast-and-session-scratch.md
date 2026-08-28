---
Status: accepted
---

# Terminal scripts fail fast, a confinement denial stops the call, and every session gets a scratch dir

## Context

On 2026-08-22 a confined sub-agent truncated a tracked source file in a live workspace
(`docs/handoffs/archived/apogee-workspace-clobber-incident.md`). The shape: a script opened
with `mkdir -p /tmp/srtest && cd /tmp/srtest && cat > SConstruct <<'EOF' …`, confinement
denied the `mkdir`, the `&&` chain short-circuited so the `cd` never ran, and the script's
later **unguarded relative writes** landed in the workspace root — the exact tree the fence
exists to protect. The call then reported `exit 0` (the exit status came from the tail of a
`… | head` pipeline), so the agent never knew.

Three harness faults, each individually survivable: the denial did not stop the script;
confinement left the agent **nowhere writable** for scratch work, so its improvisations
default into the workspace; and the failure was invisible in the result. ADR 0012's posture —
a subprocess escape is OS-blocked with no Approval prompt, "the command simply fails and the
model routes around it" — assumed the failed command was the whole story. In a multi-command
script it is not: the commands *after* the denial are the blast radius.

An owner-ratified plan (`2026-08-22 - 04`, workspace-clobber hardening) closed the faults.
One ratified call was corrected mid-execution by evidence — recorded below as decided, with
the correction.

## Decision

**1. Every POSIX terminal script runs under a fail-fast preamble — and the preamble is not
the incident's fix.** The `terminal` tool prepends `set -e`, plus `set -o pipefail` when a
one-time cached probe shows the host `sh` accepts it (`platform.FailFastPreamble`), to every
POSIX command line after the pre-flight parse. Not config-gated. This makes a failed plain
command or (with pipefail) a failed pipeline element abort the script and surface non-zero —
fault 3's exit-masking is closed. **Corrected rationale:** the ratifying plan claimed `set -e`
alone aborts the incident's denied `&&` chain; that is false — POSIX `set -e` exempts every
command of an AND-OR list but the last, so a denied `mkdir d && cd d && …` chain does **not**
abort the script (verified under the real landlock backend, dash, and bash). The preamble is
the exit-honesty floor, not the clobber stop; decision 2 is the stop. Windows is asymmetric
by design: `cmd /c` has no `set -e` analogue, lines pass through verbatim (recorded in the
`Terminal` doc comment).

**2. A confinement denial stops the whole confined call — the kill-on-denial watch.** Every
**confined** subprocess run (the shared `runSubprocess` funnel: `terminal`, `python_exec`,
confined hook subprocesses) wires its output through `platform.DenialKillWriter`, a live
watch that forwards every byte and kills the call's process group (contract §2.4 teardown) at
the first streamed OS-denial signature. A denied command therefore ends the script instead of
handing its remaining lines a half-done state — the job `set -e` cannot do. The stopped call
renders a definitive error label (`[blocked by workspace confinement: an operation was
denied, so the command was stopped; writes are allowed only inside the workspace <root> and
<the box's other writable paths>]` — **amended 2026-08-25**: both labels are rendered from the
`domain.ConfinementBox` the run was fenced by and NAME the writable roots by path, the session
scratch dir among them, because a model that is only told a fence exists has nowhere to put the
file); a confined *unstopped* failure whose output merely looks denial-shaped gets the weaker
`[likely blocked by workspace confinement: …]` heuristic label; a clean exit is never forced
into an error. The **shared signature set lives in `internal/platform`**
(`platform.LooksLikeConfinementDenial` — the watch and both labels key on the same list) and
is the source of truth. It matches **both errnos' spellings**: strerror(EPERM) ("Operation
not permitted" — what macOS seatbelt denials print) *and* strerror(EACCES) ("Permission
denied" — what Linux landlock denials actually print; landlock filesystem refusals are
EACCES, not EPERM). Best-effort by design (strerror text is locale-dependent; the kill races
the shell's next command through a pipe): a miss still leaves the fence intact and the
non-zero exit visible; a false match is confined to confined runs and surfaces loudly as a
labeled error. POSIX-only, mirroring decision 1's Windows asymmetry ("Access is denied." is
deliberately unmatched). The escape battery's `chained_script_clobber_denied` probe
reproduces the incident shape under the real backends and asserts the watch matched, the
script died non-zero, and the unguarded relative write never reached the workspace.

**3. Every session gets a scratch dir inside the confinement box.** A new dotdir root
`~/.apogee/scratch/<session-id>/` (sibling of `sessions/`, `library/`, …), created `0700`
when the session id is minted and **following the active session** across rotation;
`Config.ConfinementBox()` appends it to the box's `WritablePaths` (into a fresh slice, and
only when the dir actually exists — a path that was never created is never advertised
writable). Startup GC best-effort-removes scratch dirs untouched for 14 days. Sessions
storage stays flat. The model is told: a fourth prompt placeholder **`{{scratch}}`** joins
the closed template set, and the shipped default prompt gains one guidance line ("Scratch and
test files go in {{scratch}} — it is writable; the workspace is for the project's own files
only. /tmp may not be writable."). Per-session constant, KV-cache safe. This removes fault
2's hazard inversion: confinement no longer blocks the safe destination while leaving the
workspace as the only writable ground.

**4. Tracked-file mutation warnings are an always-on structural floor.** When the workspace
root is a git repository, the agent snapshots `git status --porcelain` immediately before and
after each subprocess tool call; differing snapshots append `[warning: this command changed
workspace files: <paths>]` (capped at 10 with an "… and N more" tail) to the result —
success **and** error results, every mode **including Bypass** (the ADR 0006 exempt class,
like the tool-result clamp). Not a Mechanism; no gating, no config key. Each snapshot runs
with a 2s timeout in the workspace root and any git error skips the check silently — the
floor must never break or slow a call. This converts a silent clobber into something the
model reacts to within one call. **Amended 2026-08-26:** the snapshot no longer spawns a bare
`git` of its own — it runs through the same hardened git funnel as the git tools
(`tools.RunGitQuery`: the exec fence on the resolved binary, `-c core.hooksPath=`,
`GIT_CONFIG_NOSYSTEM`, the allowlisted workspace-scoped environment, the repo-local
command-config refusal — every repo-local key whose value is a program git executes — and the
§2.4 process-tree teardown), and it runs OUTSIDE the call's
confinement box — apogee's own bookkeeping is not the model's command. The 2 s timeout,
workspace-root cwd and silent-skip-on-any-failure contract above are unchanged; a fenced or
refused git is simply one more failure that skips the check.

**5. Relation to ADR 0012 — extended, not superseded.** The posture stands: a subprocess
escape is OS-blocked with no Approval prompt. Two refinements land back into its documents:
the escape now also **stops the rest of the confined call** (decision 2), and the denial
errno is stated honestly — EACCES under Linux landlock, EPERM under macOS seatbelt — where
ADR 0012 and the execution contract previously said "EPERM" unqualified. That wording had a
real cost: the denial label shipped with an EPERM-only signature list that could never have
matched a real Linux denial. The contract's §2.2/§6.2 and ADR 0012's decision prose are
reconciled; the platform signature set is where the spellings live from now on.

## Considered options

- **`set -e` alone as the incident fix** — rejected by evidence: the POSIX AND-OR exemption
  means the incident's own script survives the preamble; only the kill-on-denial watch stops
  it. The plan originally ratified the preamble as sufficient; the escape-probe work
  falsified that and the owner redirected the item to the watch (the incident report's fix A).
- **Config-gating the preamble or the watch** — rejected: both are safety floors in the
  ADR 0006 sense; a knob to turn them off is a knob to reintroduce the incident.
- **Blocking the denial at Confine time with a prompt** — rejected: ADR 0012 settled that the
  subprocess surface is OS-fenced, not gated; the watch keeps that (no prompt — the call just
  ends, labeled).
- **Scratch under `~/.apogee/sessions/<id>/scratch/`** (the incident report's sketch) —
  rejected: sessions storage is a flat `<id>.json` store; a sibling `scratch/` root keeps the
  session store layout untouched and gives GC one sweepable root.
- **Prompt-level hardening alone** (tell the model to use absolute paths and `set -euo
  pipefail`) — rejected as primary: guidance a model can forget; shipped only as the
  `{{scratch}}` guidance line on top of the structural fixes.
- **Making the mutation warning a Mechanism** — rejected: Mechanisms are gated and benched;
  this is a floor that must hold under Bypass, exactly like the clamp (ADR 0006).

## Consequences

- The 2026-08-22 incident shape is structurally closed and regression-proven: the
  `chained_script_clobber_denied` probe runs in the escape battery under the real backends.
- `docs/design/confinement-execution-contract.md` is amended (2026-08-22): §2.2 states the
  EACCES/EPERM reality and the kill-on-denial watch, §6.2 gains the chained-script row, §7
  records the box's writable set as workspace ∪ per-project `WritablePaths` ∪ the session
  scratch dir (plus the backend-level `/dev/null` exemption). ADR 0012's "EPERM" wording is
  reconciled in place.
- The terminal result surface grows three model-facing signals: the stop label, the
  heuristic denial label, and the mutation warning. All are additive lines on existing
  results; no wire or Driver obligation changes (ADR 0031 holds — the scratch path rides the
  existing Config/Agent seam, and the engine stays wire-silent).
- Windows keeps neither the fail-fast preamble nor the denial watch — its terminal path has
  no `set -e` analogue and its denials print "Access is denied.", deliberately unmatched.
  The asymmetry is recorded here and in the `Terminal` doc comment; closing it is future
  work, not an oversight.
- `~/.apogee` gains a `scratch/` root with a 14-day GC; custom prompts opt into `{{scratch}}`
  like the other placeholders.
