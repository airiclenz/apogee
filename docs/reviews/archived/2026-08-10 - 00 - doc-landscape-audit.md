# Doc-landscape audit — 2026-08-10

Full-depth verification of the live documentation set against the working tree
(11 parallel read-only auditors; truth = working tree, mid-flight tool-surface
plan noted where relevant). Companion plan:
`docs/plans/2026-08-10 - 01 - doc-landscape-cleanup-plan.md`.

## Ratified ground rules (owner, 2026-08-10)

1. Scope: live core docs + `docs/design/` + `docs/skill-runs/`. Archived
   history and `CHANGELOG.md` untouched.
2. Prune = real docs move to `archived/`; machine junk is deleted + gitignored.
3. `layout.md` + `docs/layout/` are **normative specs** — drift-audit only; a
   spec/code mismatch may be doc drift *or* a code bug and is flagged both ways.
4. `README.md` stays the full user manual (drift-audited) and gains a short
   contributor pointer to `AGENTS.md` (the single code-navigation map).
5. `docs/design/` retention rule: **normative-or-archive** — a design doc stays
   only while it binds future work; a narrator of landed code is archived after
   salvaging any "why" not already in an ADR.
6. `TODO.md`: verify item-by-item, prune the dead.
7. `docs/skill-runs/`: delete finished run dirs, keep the live one, gitignore.
8. coding-standards skill: decide after findings (see §Structure below).
9. Code wins over descriptive docs (README/CONTEXT/TODO).

## Verdict table

| Doc | Verdict | Drift found |
|---|---|---|
| `README.md` (1026) | keep (manual) | 2 real + 4 minor of ~120 claims |
| `CONTEXT.md` (1128) | keep (domain language) | 3 minor of ~67 terms |
| `layout.md` (1535) | keep (normative spec) | 5 doc-side of ~200+ rules; 0 code bugs |
| `docs/layout/settings-screen-layout.md` | keep, untouched | 0 — byte-accurate |
| `docs/layout/user-questions-layout.md` | keep | 3 minor |
| `docs/layout/prompt-box-layout.md` | **archive** | fully redundant with layout.md; only its own spellings drifted |
| `docs/layout/tool-layout.md` (working tree) | owner's in-flight design sketch — out of this cleanup's scope; see §Flag below | spec-ahead-of-code by design |
| `docs/design/confinement-execution-contract.md` | keep-as-contract | 2 stale anchors; 1 real code gap surfaced (§Flag) |
| `docs/design/mcp-client.md` | keep-as-contract | 2 one-line vocabulary fixes |
| `docs/design/mechanism-catalogue.md` | keep-as-contract | 1 real: `guided_decomposition` row missing (code has 21, Table A has 20; README promises 21) |
| `docs/design/hook-mutation-api.md` | **archive after salvage** (3 "why"s → ADRs) | broad; superseded by ADR 0017/0039 + catalogue |
| `docs/design/technical-design.md` | **archive after salvage** (2 fragments → ADRs) | broad; self-declared non-authoritative |
| `TODO.md` (854) | keep, prune ~9–15% | 1 obsolete entry, 4 compressible FIXED narratives, 3 stale refs |
| `ISSUES.md` | keep | 1 closed line prunable |
| `AGENTS.md` | keep (AI entry layer) | 1 stale bullet (distribution), 1 wording ambiguity |
| handoff `2026-08-01 - 01 - merged-findings-roadmap` | keep — sole record of open Wave-4 grills + owner-run proofs | — |
| handoff `2026-08-03 - 00 - readme-drift-fix` | **archive** — both named spots fixed in the 2026-08-05 README rework | — |
| `docs/skill-runs/` | delete finished dirs, keep live `implement-plan/2026-08-10_-_00`, gitignore | — |

Headline: the doc set is far healthier than feared. ~400 discrete claims were
verified; the overwhelming majority match the code exactly, often verbatim.
The cleanup is a trim, not an amputation: two design docs archive, one mockup
archives, one handoff archives, skill-run logs leave git, and ~25 point fixes
land across the rest.

## Findings detail

### README.md (2 real, 4 minor)

- `README.md:70` — Status says "**v0.11.x on main**"; `VERSION` is v0.12.9.
- `README.md:1014–1017` — `make install` search order reverses `~/.local/bin`
  and `/opt/homebrew/bin` vs `Makefile:60` `INSTALL_CANDIDATES` (the Makefile's
  own comment at :47–50 carries the same inversion; the variable is truth).
- Minor: `:1011` `make check` list omits the ADR-0010 import invariant and the
  `--help` smoke; `:1035` "every other tool ships with Go" overstates (tar,
  sha256sum, sed…); `:130` stale `VERSION=0.11.0` example; `:90` "Newest on
  `main`" list predates settings screen / headless / parallel sub-agents /
  color schemes.
- Ratified addition: ~5-line contributor pointer → `AGENTS.md`.

### CONTEXT.md (3 minor)

- `:305` — mid-run verb list omits the Schedule pair `/schedule`,
  `/schedule-stop` (ADR 0033). Same stale list in the code comment at
  `internal/tui/command.go:262`.
- `:356` — `web-fetch`/`http-request` hyphenated; code names are `web_fetch`/
  `http_request` (the doc itself spells it right at :564).
- `:636–637` — names `correct_tool_result` as the post-tool-result resident;
  deferred owner-ratified 2026-07-04. Same staleness in
  `internal/domain/mechanism.go:53–54`. Error enrichment is the real resident.
- All five retired terms confirmed gone from live identifiers.

### layout.md (5, all doc-side; ~200+ rules hold verbatim)

- `:2–4` — intro gutter arithmetic off by one since commit `e2a7c2d`
  (bodyRightGutter 2→1); the file contradicts itself at :448 (which is right).
- `:459–462` — scrollbar key "fixed for the run"; ADR 0037 live-apply moved it
  mid-session (`internal/tui/settings.go:1309–1314`). Stale code comment rides
  along at `theme.go:112–113`.
- `:54` — mockup placeholder legend predates the current
  `⏎ send · ⌥⏎ newline · ↑ recall · ⌃c quit` form.
- `:42–46` — mockup Sub Agent block predates the ⤷-railed run shape
  (P3.14/ADR 0039).
- `:1318–1320` — "the four verbs that take arguments"; six do
  (`/rename`, `/color-scheme` joined). Stale companion comment at
  `internal/tui/autocomplete.go:31`.

### docs/layout/

- `settings-screen-layout.md` — zero mismatches, down to hint strings.
- `user-questions-layout.md` — digit-shortcut rule `[1]/[2]/[3]` was ratified
  out (`plans/archived/2026-08-04 - 03`, "no digit shortcuts"); multi-option
  mockup misses the always-painted hint row; `Command:` should be lowercase.
- `prompt-box-layout.md` — 9-line unlabeled mockup; every element has a
  maintained normative section in layout.md; only its own spellings drifted
  (`30t/sek` vs `tok/s`, `8K` vs `8k`, missing spinner glyph). Archive.

### docs/design/

**confinement-execution-contract.md — keep.** ADR 0012 defers to it; ~40 code
comments cite its §-numbers; the §4 mode×class table exists nowhere else; §7
(box seeding) and §10.4 (permit-widening ledger) are standing obligations.
Amendments needed: §6.1 pinned signatures miss the `Shell` third parameter
(`confinetest.go:45,122`); §6.2 line refs moved into per-OS files; §4's
"out-of-workspace row is unreachable" paragraph is half-false (see §Flag).

**mcp-client.md — keep.** Remarkably current. Two one-liners: `:93`
`dispoGate` → `resolveGate` / Resolution ladder; `:26–27` "disposition (D5)" →
"Resolution (D5)".

**mechanism-catalogue.md — keep.** Live contract on four axes (D7 curation
authority, D1 no-flip gate, L9 append-only campaign ledger, test-pinned twin of
`internal/validated/shipped.json`). One repair: code registers **21**
Mechanisms, Table A lists **20** — `guided_decomposition` (ADR 0014) has no
row; `README.md:443–445` promises 21. Known gap since 2026-07-06.

**hook-mutation-api.md — archive after salvage.** A P0.1 design draft, fully
applied 2026-06-23 and outgrown (ADR 0017 anchoring, ADR 0039 fan-out, R4
semantics, stale `apogee.go:NNN` anchors). Salvage first — none of these live
in any ADR:
1. §5 amendment 2026-08-02: the `ToolOutcome` tri-state "why" (read_loop
   IsError loss, string-sniffing failure story) → amend ADR 0022.
2. §8 #6: "mutation is index-addressed, never raw-slice" → amend ADR 0001.
3. §8 #5: "`Message.Content` is string-only; unknown structure preserved in
   `Extra`" → same amendment.
Repoint `TODO.md:362` and the catalogue/technical-design citations.

**technical-design.md — archive after salvage.** Self-declared non-authoritative
synthesis; its §8 worklist is complete in reality; its §5 component table has
become a per-feature changelog (the "narrates the implementation" condition).
Broad drift (tool counts, Mechanisms/Library rows still marked unbuilt, "ADRs
0001–0010" vs 41 on disk). Salvage first:
1. §3 dependency policy (:142–147) — external programs are runtime-detected
   optional enhancements, never hard prerequisites; single static binary;
   stdlib-first. Cited by code (`autofix.go:50,134`) but recorded in no ADR →
   new ADR.
2. §4.1 #4 — "Config is a struct, not functional options; every field a
   reviewable seam" → ADR 0001 amendment.
Archiving retires the one-file component index; if wanted later, that function
belongs in a deliberately narrative home, not a "design" doc. Plan executions
must stop amending its §5 rows once archived.

### TODO.md / ISSUES.md (prunable ≈ 9–15%)

- OBSOLETE: `:46–48` `/server` persistence non-goals — shipped via ADR 0036 +
  plan `2026-08-05 - 05`.
- Compress: the four struck-through FIXED width narratives (`:635–709`) to
  house-style trail one-liners (all four fixes verified in code).
- Refresh stale refs: InjectContext → `hooks.go:504`; ConfineWritablePaths
  readers → `dispatch.go:365/405`; AskRequest parenthetical predates
  `SubAgentTask`/`SubAgentName`; schedule-tool entry should cite ADR 0034.
- ISSUES.md:8 — closed `[X]` shift+enter line prunable.
- 32 of 41 items are genuinely LIVE and accurate — the file is doing its job.

### AGENTS.md

- Distribution bullet is stale since v0.11.0: Homebrew tap + six prebuilt
  archives per release exist (`make dist`). The never-`go install` half stays.
- "CHANGELOG + VERSION in step" doesn't describe actual practice (per-feature
  VERSION micro-bumps; CHANGELOG headings at release cut) — reword only; no
  version action.

### Handoffs

- `2026-08-03 - 00 - readme-drift-fix` — superseded (both named spots fixed in
  the 2026-08-05 README rework) → archive.
- `2026-08-01 - 01 - merged-findings-roadmap` — keep: sole record of the open
  Wave-4 architecture grills (G1–G6 / C1–C11) and the pending owner-run
  confinement proofs.

## Flags for the owner (not in the cleanup plan)

1. **Real code gap surfaced by the confinement contract §4:** an *approved*
   out-of-workspace write now reaches the approval Gate
   (`resolveTargetUnbounded`, `workspace_scoped.go:77`) but still errors at
   Execute (`write_file.go:82` os.Root fence never learned to honour the
   approval). Tracked nowhere. The plan records it as an ISSUES entry; the
   decision — land the P3.7 reconciliation or ratify strict fencing and amend
   §4 — is an owner call.
2. **`docs/layout/tool-layout.md` working-tree edits are design-intent, not
   documentation:** inline `+N more lines ▶`, right-edge indicators, dotted
   leaders, super-grouping — nothing implements them, no plan item covers
   rendering, and the sketch currently contradicts both the code and
   layout.md:675–683. Its "super-grouped expanded" section is empty and the
   dot-glyph codepoint is wrong (says U+2014 em dash; sketches use · / ⋯ / …).
   Recommendation: keep it out of the tool-surface commits and give it its own
   plan that amends layout.md in the same stroke.
3. **Structure (code, not docs):** package layout is exemplary — `internal/
   domain` mirrors CONTEXT.md file-by-file, 5/5 findability probes passed. The
   gaps are file-level: `internal/tui/model.go` (4,743 lines / 120 funcs — the
   repo's one true junk drawer: session-save queue, approval keys, command-run
   clusters all uninvited), `cmd/apogee/wire.go` (2,932 lines / 102 funcs, no
   header map), the `cmd/apogee` config cluster (~7k lines, could be
   `internal/config`), and `internal/tui/doc.go`'s file map being hand-
   maintained with no guard test. These are refactors → separate plan if
   wanted. The cleanup plan includes only the documentation piece
   (`cmd/apogee` package map).
4. **coding-standards skill:** the audit's verdict — a generic
   "structure mirrors domain" topic would have prevented nothing here (the repo
   already exceeds it); three narrow rules would have prevented the real gaps:
   (i) a package's doc.go must map its files past ~10 files, test-enforced;
   (ii) coordinator-type packages split by concern cluster, not by type;
   (iii) composition roots split by seam. Owner decision pending.
