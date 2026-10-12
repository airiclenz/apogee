# apogee facts — the repository this run describes

A shared include, verified against the tree when the skill was written. Read it
after `_shared.md`; when the tree contradicts a line here, trust the tree and
say so on your `FLAG` line.

## The baseline (`BASELINE`)

The file is JSON. Use `baseline.tag` (the last **published** GitHub release,
e.g. `v0.24.11`), `baseline.published` (its publish time), `baseline.range`
(`<tag>..HEAD`) and `baseline.commits`. Ignore `gate` and `steps`. `version`
is the checkout's `VERSION`, which micro-bumps per shipped feature
(`chore(version): bump version to vX.Y.Z`) — it is never the release's
number; that is the dispatch's `VERSION:` key, `unreleased` when absent.

## Changelog

- File: `CHANGELOG.md`. Unreleased heading, literally: `## [Unreleased]`.
- Release heading shape: `## [X.Y.Z] — YYYY-MM-DD` (no `v`, an em dash), e.g.
  `## [0.24.11] — 2026-10-08`. A micro-bump adds no heading: entries collect
  under `[Unreleased]` until a release cut rolls them under a new heading.
- An entry is a `- **Bold lead.** detail` bullet.

## Commit conventions

Conventional commits, `type(scope): subject` — types `feat`, `fix`, `docs`,
`build`, `refactor`, `test`, `chore`. Bookkeeping that ships nothing:
`chore(version)`, `chore(release)`, `chore(issues)`, `chore(plans)`,
`docs(plans)`, and the historical `chore(beads)` from before the capcom move.
Ticket ids are capcom issue ids: a slug like `adoption-pane-unarmed`, or an
older `apogee-<slug>` or hash form like `apogee-qi3` that survives as an alias.
The register lives in `.capcom/issues/`. No AI attribution trailers. Plans live in
`docs/plans/`, ADRs in `docs/adr/`.

## User surfaces — where a change becomes visible

| Surface | Paths |
|---|---|
| CLI commands & flags | `cmd/apogee/root.go`, `cmd/apogee/*_cmd.go`, `cmd/apogee/headless.go`, `daemon.go`, `probe.go`, `update.go` |
| TUI slash commands & keys | `internal/tui/command.go`, `internal/tui/commandrun.go`, `internal/tui/help.go` |
| Config keys & defaults | `internal/config/defaults/config.yaml`, `internal/config/registry.go`, `internal/config/options.go`, `internal/config/reactions.go` |
| Model tools | `internal/tools/registry.go` (`KnownToolNames`, `DefaultToolsWithHost`) |
| Public Go API | the module root `apogee.go`, `version.go`; `internal/domain/events.go`, `reaction.go` |
| Docs & manual | `README.md`, `docs/manual/`, `CONTEXT.md`, `layout.md`, `SECURITY.md` |
| Install & packaging | `Makefile`, `.github/workflows/`, `cmd/release/templates/`, `cmd/apogee/defaults/`, `docs/manual/updating.md` |
| Emitted strings | `internal/tui/` (status, help, notices), `internal/notice/`, `cmd/apogee/` user messages, `internal/config/defaults/prompt.txt`, `internal/floor/prompts/`, `internal/skills/shipped/` |

The tool, Floor-guard and Moment counts the README and the manual state are
pinned by tests, so a changed count there is a real surface change.
