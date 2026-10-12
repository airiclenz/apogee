# Inventory entry format — the shape every producer emits

A shared include; nothing writes to it. The three scouts and the merge emit
entries in exactly this shape, so a later phase can line two readings up field
by field.

**Granularity.** An entry is one thing a reader can verify: a feature, a fix, a
behaviour change, a build/infra change, a dependency upgrade, a docs change.
Fold "fix typo in X" follow-ups into their entry; split a commit that delivers
two independent things. Every input you read lands in exactly one entry or in
the "touched without behaviour change" list.

**Id prefixes** — namespaced by producer, numbered from `01`, never renumbered:
`CL-nn` changelog scout · `C-nn` commits scout · `S-nn` surface scout ·
`R-nn` merged inventory rows.

**Fields.** Leave a field `—` when your phase cannot populate it; never guess.

- **Kind:** feature | fix | behaviour-change | refactor | docs | build-infra |
  dependency — closed vocabulary; pick the nearest.
- **Surface:** cli | api | gui-tui | config | docs | install-distribution |
  internal — where a user would observe it; closed vocabulary.
- **Effect:** what a user can now do or now sees differently — concrete enough
  to check, never a restatement of the title.
- **Commits:** the commits that deliver it, or `uncommitted`.
- **Files:** the behaviour-defining files, ≤ 8. **Tests touched:** or `none`.
- **Refs:** plan file and item, ADR or issue id named in commit messages or
  changelog text, or `none` — recorded, never opened.
- **Evidence** (evidence phases only): `<file> — <before> → <after>`.
- **Verdict** (evidence phases only): `found | changed | absent`.

```
### <prefix>-01 — <title, ≤ 60 chars>
- Kind: ... · Surface: ...
- Effect: <1–2 lines, user-visible>
- Commits: <sha>, <sha> | uncommitted
- Files: <≤ 8> · Tests touched: <files | none>
- Refs: <"docs/plans/… item 4", "apogee-qi3", or "none">
- Evidence: <file> — <before> → <after>
- Verdict: found | changed | absent
```
