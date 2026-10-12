---
title: Deferred security-review Lows (P3 /security-review, 2026-06-24)
status: done
type: decision
priority: 4
labels: [security]
aliases: [apogee-8wy]
created: 2026-09-03T18:40:20Z
closed: 2026-09-13T18:58:27Z
reason: Decision record only; L1 re-verify rides apogee-089 / apogee-9d8
---

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
