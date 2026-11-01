---
title: The terminal pre-flight refuses a heredoc whose body has an apostrophe
status: open
type: bug
priority: 2
created: 2026-11-01T11:30:44Z
---

On POSIX, `terminal` refuses a valid command line when a heredoc body holds an unpaired quote, and the shell never sees it. For example:

    capcom know add --id x --title y --body-file - <<'EOF2'
    capcom's store is git-native.
    EOF2

fails with `could not parse command line: EOF found when expecting closing quote`. Without the apostrophe, the same line passes. Most English prose has an apostrophe, so a body for `git commit -F -`, `capcom create --body-file -` or `cat > file` is refused.

Cause, traced 2026-11-01:

- `preflightCommandLine` (`internal/tools/terminal.go:307`) runs `shlex.Split` over the whole line. shlex does not know heredocs, so it reads the body as shell words and stops at the lone `'`.
- `console_open` (`internal/tools/console_open.go:143`) shares the same pre-flight.
- apogee already parses heredocs correctly in one place: the shell-writes scanner skips a `<<` body up to its delimiter line (`internal/security/shellwrites.go:41`).
- Listed in `docs/reviews/archived/session-mining-2026-09-14.md:34` ("Terminal pre-flight parser rejects lines `sh` accepts"), but never filed.

Cost in practice: the Seqcom agent (Qwen3.8-27B, headless `--mode auto`) hit it in 7 runs between 2026-10-18 and 2026-10-31. Each time it switched to `write_file` or `python_exec` to stage a scratch file, then passed that file with `--body-file <path>`. The detour has become a habit: the run of 2026-11-01 staged every body in scratch without hitting the error first. That costs it a turn per body.

## Acceptance

- A POSIX line whose heredoc body holds an unpaired `'` or `"` passes the pre-flight and runs, for both `<<'EOF'` and `<<EOF`, and for `<<-EOF`.
- A line that really is malformed outside any heredoc body (an unterminated quote in the command words) is still refused with the parse error.
- A test in `internal/tools/terminal_test.go` pins both cases, beside the existing `preflightCommandLine` table.
