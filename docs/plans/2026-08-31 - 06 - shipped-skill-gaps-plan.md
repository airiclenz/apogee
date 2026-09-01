# Shipped-skill gaps — a planning readiness gate, a code-review scope line, an export hint

**Goal:** Close the three real gaps an external review of the four shipped skills found, plus the
one host-side residue behind its fourth. `planning` gains the hard readiness gate the other three
skills all have; `code-review` names its review unit and excludes generated and vendored files;
`/skills` names the export verb in the listing itself, where the human reading it can act on it.

**Date:** 2026-08-31 · **Status:** unexecuted
**sized for:** ~200k-context host
**Base commit:** `ad962676`

**Sources:** `docs/adr/0065-shipped-skills-and-the-load-skill-door.md` (§1 lowest-priority source,
§5 the export verb, §7 the catalog stays out of the standing prompt) ·
`docs/adr/0032-the-user-skill-library-outranks-the-workspace.md` ·
`internal/skills/shipped/` · `internal/tui/skills.go` · `internal/tui/skillscmd.go`

**Ratified design calls** (owner, 2026-08-31, this session):
- **planning gate:** a hard readiness test closing §3 plus a required lead line in §Reporting — the wording quoted in chat and approved verbatim.
- **code-review scope:** the skill names its review unit (diff / branch-vs-merge-base / PR / working tree) before reading, and says which.
- **code-review exclusions:** generated, vendored and third-party files are read for context, never for findings, unless the change is to the generator or the pin.
- **Export hint shape:** a fourth section of `skillCatalogNote`, blank-line separated like the failed and shadowed halves, emitted ONLY when a loaded row carries the `shipped` label.
- **Unwired home:** the hint is also withheld when `home == ""` — `exportShippedSkill` refuses outright there (`noSkillExporterNote`), so naming a folder would announce a write that cannot happen.
- **Rejected from the same review, do not re-open:** the "garbled `checklist.md` prose" (the file is clean at `checklist.md:13-15`); debugging's reporting "hedge" (the hard stall rule belongs in `checklist.md:45`); the `planning` trigger vs ADR 0065 §8 (triggers feed the ADR 0061 band, which offers and never attaches); a "fork me" line in all four bodies (a body is model-facing prompt text charged per invocation, and the model cannot run `/skills export` — item 3 is the human-facing form of that idea); a failure-mode section per skill (ADR 0065 keeps these short, shadowable defaults).

**Standing requirements:** `skills: coding-standards`

**Out of scope:**
- Any change to `debugging` or `commit-hygiene`, or to any skill's frontmatter (`triggers:`, `summary:`, `description:`) — the suggestion band's index is unchanged by this plan.
- `docs/manual/commands.md` and `docs/manual/configuration.md` — both already document `/skills export <id>`; the gap is the in-app listing only.
- The `load_skill` tool, the virtual `shipped:` mount, and the priority order.

**Regression check (2026-08-31, `ad962676`):**
- 1: guard folded — Acceptance now greps for the ratified gate wording, and the shipped-tree
  load pin is cited at its own line (`internal/skills/load_test.go:782`).
- 2: guard folded — Acceptance now greps for both ratified bullets; same load-pin correction.
- 3: guard folded — the hostile e2e golden `t12-skills` is re-recorded and `hostileRedactions`'
  padding trim widened to reach `<home>` rows, both in this item's commit.

---

## 1. `planning` gains a hard readiness gate and a required lead line — ✅ DONE (2026-09-01)

**What:** `internal/skills/shipped/planning/SKILL.md` ends §3 with a soft sanity-check sentence
(line 78) and no test for "the plan is ready", where the other three shipped skills all close on a
hard gate (`debugging/SKILL.md:72`, `code-review/SKILL.md:100`, `commit-hygiene/SKILL.md:104`).
Replace the §3 closing sentence with the ratified gate, and add the matching lead line to
§Reporting. Binding text, verbatim:

- §3, replacing lines 78–79: *"The list is ready when you can say, in one line: the goal is true
  when [observable], N steps, files [paths], first verification command [cmd]. If you cannot fill
  that line, the list is not finished — a missing observable means step 1 is missing."*
- §Reporting, as the new first line of its first paragraph: *"Lead with that one-line readiness
  statement, then the numbered list itself: step, files, verification. No preamble."* — replacing
  the existing "Present the plan as the numbered list itself: step, files, verification. No
  preamble."

No frontmatter change. The body stays under the 150-line cap the shipped bodies are written to.

**Regression guard.** Acceptance must prove the new text LANDED, not merely that the body still
parses — reviewer 1's NOTES is accepted. Add to item 1's Acceptance a literal grep for the ratified
gate wording in internal/skills/shipped/planning/SKILL.md (a distinctive fragment of "The list is
ready when you can say, in one line" and of the §Reporting lead line), keeping the existing
parse/build/line-cap checks; correct the load_test.go line reference in Tests to the one that
actually holds the shipped-tree load pin.

**Files:** `internal/skills/shipped/planning/SKILL.md`
**Tests:** none new — the shipped tree's own pins cover it (`internal/skills/load_test.go:782`,
`TestLoadShippedSkillsAllParse`, loads every shipped folder and fails on a body that will not parse
or is empty).
**Acceptance:** `go test ./internal/skills/ -run 'Shipped'` · `go build ./...` ·
`grep -c '' internal/skills/shipped/planning/SKILL.md` reports ≤ 150 · both
`grep -F 'The list is ready when you can say, in one line' internal/skills/shipped/planning/SKILL.md`
and `grep -F 'Lead with that one-line readiness statement' internal/skills/shipped/planning/SKILL.md`
match
**Commit:** `docs(skills): give the shipped planning skill a readiness gate`

---

## 2. `code-review` names its review unit and excludes generated and vendored files

**What:** `internal/skills/shipped/code-review/SKILL.md` never says WHAT it is reviewing — §What
to read opens on "Read the whole change" (line 32) with *change* undefined — and its §What NOT to
report (lines 89–96) has no generated/vendored exclusion, an omission in a skill whose thesis is
budget discipline. Two edits, binding text verbatim:

- §What to read, as a new FIRST bullet above the existing "Read the whole change" bullet: *"Name
  the review unit before reading: a diff, a branch against its merge base, a PR, or the working
  tree. Say which. 'The whole change' means that unit, and a finding outside it is out of scope."*
- §What NOT to report, as a new bullet after the "Speculative performance" bullet: *"Generated,
  vendored and third-party files — unless the change is to the generator or the pin itself. Read
  them for context, never for findings."*

No frontmatter change; the §Reporting four-line format is untouched.

**Regression guard.** Same decision as item 1 — add to item 2's Acceptance a literal grep for each
of the two ratified bullets in internal/skills/shipped/code-review/SKILL.md ("Name the review unit
before reading" and "Generated, vendored and third-party files"), keeping the existing
parse/build/line-cap checks; correct the load_test.go line reference in Tests.

**Files:** `internal/skills/shipped/code-review/SKILL.md`
**Tests:** none new — covered by the shipped-tree load pin (`internal/skills/load_test.go:782`,
`TestLoadShippedSkillsAllParse`).
**Acceptance:** `go test ./internal/skills/ -run 'Shipped'` · `go build ./...` ·
`grep -c '' internal/skills/shipped/code-review/SKILL.md` reports ≤ 150 · both
`grep -F 'Name the review unit before reading' internal/skills/shipped/code-review/SKILL.md` and
`grep -F 'Generated, vendored and third-party files' internal/skills/shipped/code-review/SKILL.md`
match
**Commit:** `docs(skills): scope the shipped code-review skill and exclude generated files`

---

## 3. The `/skills` listing names the export verb

**What:** `skillCatalogNote` (`internal/tui/skills.go:111`) renders three sections — loaded, failed,
shadowed — and never names `/skills export <id>`, so the one supported way to fork a shipped skill
(ADR 0065 §5) is discoverable only from the `/` menu row's summary (`internal/tui/command.go:265`).
Add a fourth section, appended last in the existing `[][]string` join loop so it inherits the
blank-line separation the other three already get.

Binding calls:
- **Condition:** emitted only when at least one entry in `list` resolves to `skillSourceShipped`
  through the existing `skillSource(sk.Dir, home, workspace)`, AND `home != ""`.
- **Wording**, two lines, the second indented two spaces — mirroring `skillExportedNote`'s shape
  (`internal/tui/skillscmd.go:118`):
  `edit a skill apogee ships: /skills export <id>`
  `  copies it into <dir>, where your copy wins`
  where `<dir>` is `filepath.Join(home, "skills", "<id>")` — composed from the SAME `home` the
  export itself composes its library root from (`exportShippedSkill`, `skillscmd.go:99`), never a
  hard-coded `~/.apogee`.
- **Placement:** a new unexported helper beside `shadowedSkillLines`, returning `nil` when the
  condition does not hold, so the section loop's `len(section) == 0` skip is the only gate.

**Regression guard.** The hint appends to the hostile golden's shipped listing: re-record it in the
same commit (`go test ./cmd/apogee/ -run TestE2EHostileSurfacesKeepTheirOwnRows -update`), and widen
the padding trim at `cmd/apogee/e2e_hostile_test.go:405` to `^(.*(<root>|<home>).*?) +([│┃])$` — it
matches `<root>` rows only, so the hint row's pad would otherwise carry the temp home's length.

**Files:** `internal/tui/skills.go`, `internal/tui/skill_test.go`,
`cmd/apogee/testdata/frames/t12-skills.txt`, `cmd/apogee/e2e_hostile_test.go`
**Tests:** a listing carrying a shipped row emits both lines with the composed folder path; a
listing whose rows are all `library`/`workspace` emits neither; an empty `home` with a shipped row
emits neither; the announced folder prefix EQUALS the directory `skills.ExportShipped` returns for
the same id and home (drive the real verb, not the string — the journey test); the existing pins at
`internal/tui/skill_test.go:903`, `:1163` and `:1200` stay green. In `cmd/apogee`, the widened
padding trim keeps the re-recorded `t12-skills` golden independent of the temp home's length
(`e2e_smoke_test.go:110-124` is Contains-only and is unaffected).
**Acceptance:** `go test ./internal/tui/ -run 'Skill'` · `go test ./cmd/apogee/ -run TestE2EHostile` ·
`go build ./...`
**Commit:** `feat(tui): the /skills listing names the export verb`

---

## Suggested version bump

Patch (`VERSION` `0.19.6` → `0.19.7`) — three shipped-skill body corrections and one new listing
line, no interface change. Not performed by this plan; the owner cuts it.
