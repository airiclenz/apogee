---
title: ADR 0107 decision
---

**v1.0.0 is released under its own number, with a self-retracting placeholder tag above the
burned v1.x numbers.**

1. **The burned set is v1.1.0, v1.7.0, v1.8.0 and v1.8.1.** The first three are the numbers the
   checksum database holds; v1.8.1 is the placeholder, the highest burned version. go.mod
   retracts exactly these four, by name, in one `retract ( … )` block. The list lives once, in
   `internal/update` (`RetractedVersions`, `IsRetracted`): `cmd/release` imports it, and drift
   tests tie go.mod and `.github/scripts/version-bump.sh` to it. Every other v1.x number,
   v1.0.0 included, is releasable.

2. **The placeholder rule.** v1.0.0 is cut with the placeholder tagged on v1.0.0's own commit,
   so the placeholder's go.mod is v1.0.0's: it outranks v1.8.0, so Go reads its list instead of
   v1.8.0's range, and the list names the placeholder, so it retracts itself and `@latest`
   resolves to v1.0.0. A release from v1.0.0 up to the placeholder needs the placeholder; one
   below v1.0.0 or above the placeholder does not, since its own go.mod is then the highest that
   carries the list. `cmd/release publish` places it in a probed, idempotent `placeholder` step
   between `release` and `homebrew` (ADR 0098, amendment of 2026-11-05), and only after it has
   checked that go.mod at the tag retracts exactly the burned versions.

3. **A bridge release ships the list-based update check first.** Every installed release from
   v0.25.0 up to the plan's refuses the whole `[v1.0.0, v1.8.0]` range in its update check, so
   it would never announce v1.0.0. A 0.x bridge release that carries the list-based check is cut
   and published before v1.0.0, so the clients that update to it are told about v1.0.0.

4. **The 1.x line skips the burned numbers.** No release takes v1.1.0, v1.7.0, v1.8.0 or v1.8.1:
   the version after a v1.0.x is v1.1.1 or v1.2.0, the one after a v1.6.x is v1.7.1 or higher,
   and the 1.8 line starts above the placeholder. `prepare`, `publish`, the CI tagging script and
   the update check all refuse a burned number.

5. **Recovery appends the next unused v1.8.x.** A placeholder placed wrongly — on the wrong
   commit, or on a go.mod whose list does not name it — cannot be moved or re-tagged, because
   the proxy keeps what it served. It is burned instead: the next unused v1.8.x is appended to
   the list, becomes the new placeholder, and is tagged on a commit whose go.mod carries the
   extended list.

Cutting the bridge release and v1.0.0, and placing the placeholder for real, stay the owner's act.
