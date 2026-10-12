---
title: ADR 0056 context
---

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
