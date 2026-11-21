---
title: The daemon never says a config holds a plaintext key or a retired sub-agents flag
status: open
type: task
priority: 3
labels: [driver-parity, security]
plan: 2026-11-21 - 01 - driver-notice-parity-plan.md
created: 2026-11-21T12:44:48Z
---

Found by docs/reviews/archived/architecture-review-2026-08-30.html (B4, the plaintext-key half) and docs/reviews/archived/architecture-review-2026-09-30.html (ledger L5 = in-passing defect D6); re-verified 2026-11-21 at 43ece2f2.

`apogee headless` warns on stderr about two config-file problems: a `servers:` entry with a plaintext `api-key:` (ADR 0047), via `plaintextKeyNotice` (cmd/apogee/headless.go:866), and an entry still carrying ADR 0045's retired `sub-agents: true` flag, via `subAgentsFlagNotice` (headless.go:876). The TUI root raises the migration offer instead, through `prepareKeyMigration` and `prepareSubAgentsMigration` (cmd/apogee/wire.go:161, :165; keymigrate.go:83).

The daemon calls none of these: `runDaemonWith` (cmd/apogee/daemon.go:179-260) prints only the confiner and residual-confinement notices, and nothing records the omission as deliberate. So the longest-running, least-watched Driver runs with a key sitting in config.yaml and no warning, and with the retired flag its Firings delegate somewhere other than the owner meant, silently.

Direction: one producer returns the config-hygiene notices for a resolved Options and config path; headless and the daemon print them on stderr (the daemon beside its existing start-up notices, stdout narration unchanged); the TUI keeps its offer. Could also land inside the shared start-up block proposed in `driver-preamble-copied`.

Constraints: ADR 0083 §5 (no host-boot module), ADR 0047 / ADR 0036 (an unattended run never migrates a key, it only says so), ADR 0031 (Driver parity).

## Acceptance

- `apogee daemon` started with an unacknowledged plaintext `api-key:` entry, or a `sub-agents: true` entry, prints the same notice headless prints, on stderr; a test covers the daemon case.
- Nothing is printed when `plaintext-key-ok: true` is set.
- Headless output stays byte-for-byte unchanged, and headless and the daemon call one shared producer.
