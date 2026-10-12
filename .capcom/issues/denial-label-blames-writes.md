---
title: Confinement denial label blames writes for a non-write EPERM (ps fails in the sandbox)
status: done
type: bug
priority: 3
aliases: [apogee-denial-label-blames-writes]
created: 2026-09-24T12:39:25Z
closed: 2026-09-30T16:26:58Z
reason: "Resolved by plan 2026-09-30 - 00 - denial-label-and-windows-confiner-tests-plan (item 1), archived at 6487f6a0: denial labels no longer assert a blocked write and still name the writable roots."
---

A sub-agent's `ps` call failed inside apogee's confinement with "Operation not permitted". Likely cause (unverified): /bin/ps is setuid-root here, and a setuid exec under no_new_privs / the confinement backend is refused — or ps's /proc access is refused; which backend was active is not recorded.

The denial watch (internal/platform/denialkill.go, denialLinePattern) matches any line ending in strerror(EPERM), and both labels in internal/tools/exec_common.go (confinementDenialLabel, confinementDenialStopLabel) then tell the model "writes are allowed only inside <roots>". ps wrote nothing, so the label points the model at re-aiming a write that never happened — misleading, especially for a small model.

Impact observed: minor — it was an incidental check and the agent carried on.

Fix direction (to design): word the labels so they do not assert the denied operation was a write (e.g. name the writable roots as one fact among others, or say the sandbox refused an operation such as a write, a setuid/privileged exec or a process inspection); optionally confirm the ps cause and document it as a known confinement limit.

Acceptance: a confined run whose EPERM does not come from a write gets a label that does not claim a write was blocked; existing write-denial labelling still names the writable roots.
