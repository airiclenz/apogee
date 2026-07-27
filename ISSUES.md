A: Activated / Active
P: Planned
X: Executed


- [ ] Functionality that exists in apogee-code has not been fully ported to apogee (`@file`, `/clear`, `/continue`, `/skill`, session-management UI now done; `/server`, inspector still pending). Verified + list collected → see **TODO.md → "apogee-code feature parity — user-facing affordances not yet ported"**. Porting still in progress.

- [X] File references with filenames including spaces are not working properly (plan: `docs/plans/2026-07-27 - 03 - quoted-file-refs-plan.md`): @"docs/plans/2026-07-23 - 04 - version-build-number-plan.md" returns this error: loop: @"docs/plans/2026-07-23 could not be resolved and was ignored: statat "docs/plans/2026-07-23: no such file or directory
