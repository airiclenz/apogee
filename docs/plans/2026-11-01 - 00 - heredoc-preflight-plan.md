# Heredoc-aware terminal pre-flight — plan

**Goal:** the POSIX `terminal` / `console_open` pre-flight stops refusing a valid line whose heredoc body holds an unpaired quote, while a line malformed outside any heredoc body is still refused with the parse error. The only work selected for the next patch release.
**Date:** 2026-11-01
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** d50821f2
**Closes:** heredoc-preflight-quote

**Regression check (2026-11-01, d50821f2):**
- 1: guard folded (script-body tokenizer spans, byte-identical no-heredoc result, widened Acceptance) plus the writer's doc.go map-sentence decision.
- 2: guard folded (named `TestTerminal_` Execute test, interpreted-string heredoc rows, the cmd.exe table comment).

**Sources:**
- `.capcom/issues/heredoc-preflight-quote.md` (cause, repro, Acceptance)
- `internal/tools/terminal.go` — `preflightCommandLine`
- `internal/security/shellwrites.go` — `shellTokenizer`, `skipHeredocBodies` (the one heredoc reader apogee has)
- ADR 0049 note of 2026-09-14 (`capcom know show "adr-0049-note-0914"`) — heredoc body is payload

**Ratified design calls:**
- **Release scope (Claude, 2026-11-01, on the owner's request to decide):** only `heredoc-preflight-quote`; the other ready issues are features, design/bench tasks, or need a Windows box.
- **Where the heredoc knowledge lives:** extends `internal/security`'s `shellTokenizer`; no second heredoc parser in `internal/tools`.
- **Which bodies are stripped:** every heredoc body, interpreter-script bodies (`bash <<EOF`) included; the shell reports its own error for a malformed script body.
- **Fail direction:** the pre-flight is a clarity aid, not a guard; a line the tokenizer misreads reaches the shell, which reports its own error.

**Standing requirements:**
- skills: coding-standards
- Machine rule: never race-run a whole heavy package; one package, one test (`GOMEMLIMIT=2GiB go test -race -count=1 -run TestX ./internal/tools/`).

**Out of scope:**
- The tokenizer's paren-depth scan of `$( … )` ignoring a `)` inside a heredoc body (existing limitation of the writes view).
- Any cmd.exe pre-flight (deliberately absent).
- `windows-journal-remove-check` and every feature/task issue.

## 1. Expose the line with its heredoc bodies removed

**What:**
**Goal:** `internal/security` exports `WithoutHeredocBodies(command string) string`, which returns `command` with every heredoc body line and its closing delimiter line removed — for `<<WORD`, `<<'WORD'`, `<<"WORD"` and `<<-WORD`, at top level and inside a `$( … )` or backtick substitution — and leaves every other rune of the line untouched; a line with no heredoc comes back byte-identical.
**Approach (assumed at the header base):** record the rune spans `skipHeredocBodies` consumes (body lines plus delimiter line) on the `shellTokenizer`; a nested tokenizer started by `nest` (via `splitSimpleCommands`) carries its offset in the parent's runes and appends its spans to the root's span list. `WithoutHeredocBodies` runs one tokenizer over the line and cuts the merged spans. `splitSimpleCommands`' existing callers and results stay unchanged — the spans are an extra output, not a change to `simpleCommand`. A `<<<` here-string and a `<<` inside quotes or a `#` comment open no heredoc (the tokenizer already says so). An unterminated body (no delimiter line) is removed to the end of the line.
**Regression guard.** The interpreter-script body call in `skipHeredocBodies` (`splitSimpleCommands(strings.Join(body, "\n"), doc.dir)`) is a third nested tokenizer: drop its spans (they sit inside the body span its parent already removes) or offset them by the body's first rune, so `echo 'a b' && bash <<S\ncat <<X\nhi\nX\nS` keeps its command words. `WithoutHeredocBodies` returns `command` itself when no span was recorded and cuts by byte offsets otherwise, so a no-heredoc `"echo \xff"` is not rewritten to U+FFFD by a `[]rune` round-trip. The edit reaches the tokenizer behind `MatchAllowRules` and the writes view, so Acceptance runs their tests too.
Also update internal/security/doc.go's map sentence for shellwrites.go ("shellwrites.go is the shell write view") so it names the new export WithoutHeredocBodies and its consumer (the terminal / console_open pre-flight).
**Files:** `internal/security/shellwrites.go`, `internal/security/shellwrites_test.go`, `internal/security/doc.go`
**Read first:** internal/security/shellwrites.go — skipHeredocBodies, nest, splitSimpleCommands, shellTokenizer, scanInputRedirect; internal/security/allowmatch.go — MatchAllowRules; internal/security/shellwrites_test.go — TestWriteTargetsOf, TestShellWriteViewReadsInterpreterHeredocs
**Tests:** a table test `TestWithoutHeredocBodies`: quoted/unquoted/`<<-` delimiters with an apostrophe and a `"` in the body; two heredocs on one line; a heredoc inside `"$(cat <<'EOF' … EOF\n)"`; `<<<` here-string unchanged; `<<` inside single quotes unchanged; no-heredoc line byte-identical; a no-heredoc `"echo \xff"` line byte-identical; the interpreter-script line `echo 'a b' && bash <<S\ncat <<X\nhi\nX\nS` keeps `echo 'a b' && bash <<S` intact; unterminated body.
**Acceptance:**
- `go build ./internal/security/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestWithoutHeredocBodies|TestWriteTargetsOf|TestShellWriteView|TestProjectConfigControlPlaneTargets|TestMatchAllowRules' ./internal/security/`
**Commit:** `feat(security): expose a command line with its heredoc bodies removed`

## 2. Pre-flight a POSIX line without its heredoc bodies

**What:** fixes `heredoc-preflight-quote`: `preflightCommandLine` runs `shlex.Split` over heredoc bodies and refuses `capcom know add --id x --title y --body-file - <<'EOF2'\ncapcom's store is git-native.\nEOF2` with `could not parse command line: EOF found when expecting closing quote`. Depends on item 1.
**Goal:** on POSIX, a line whose heredoc body holds an unpaired `'` or `"` passes `preflightCommandLine` and runs through `terminal`, for `<<'EOF'`, `<<EOF` and `<<-EOF`; a line with an unterminated quote in its command words is still refused with `could not parse command line: …`; the cmd.exe path stays unparsed.
**Approach (assumed at the header base):** in `preflightCommandLine` (shared by `terminal` and `console_open`), split `security.WithoutHeredocBodies(command)` instead of `command`. The parse-error wording stays as `Execute` emits it. Update the function's doc comment to say heredoc bodies are payload to the pre-flight and why.
**Regression guard.** Name the new `Execute` test `TestTerminal_HeredocBodyWithApostropheReachesTheShell` so the item's `-run 'TestTerminal_'` gate runs it, and write every heredoc row as an interpreted string with real `\n` — a raw backtick literal carries a literal `\n` and opens no heredoc. Heredoc rows falsify `TestTerminal_PreflightMatchesTheTargetShell`'s doc comment ("every row is a line cmd.exe reads without complaint"): either amend that comment to say the table also pins the POSIX verdict on heredoc bodies, or put the five rows in a sibling `TestTerminal_PreflightSkipsHeredocBodies` table (same posix=true/false asserts).
**Files:** `internal/tools/terminal.go`, `internal/tools/terminal_test.go`
**Read first:** internal/tools/terminal.go — preflightCommandLine, Terminal.Execute; internal/tools/console_open.go — ConsoleOpen.Execute (shares the pre-flight); internal/security/shellwrites.go — skipHeredocBodies; internal/tools/terminal_test.go — TestTerminal_PreflightMatchesTheTargetShell, TestTerminal_EmptyAndUnparseableCommand, hostShellIsPOSIX, executeTerminalLine
**Tests:** add rows, as interpreted strings, to `TestTerminal_PreflightMatchesTheTargetShell` (amending its doc comment) or to a sibling `TestTerminal_PreflightSkipsHeredocBodies` table (posixFail false: the issue's exact line, a `<<EOF` body with `"`, a `<<-EOF` body with a tab-indented delimiter; posixFail true: `echo "unterminated <<EOF\nx\nEOF`, `cat <<EOF don't\nbody\nEOF`). Add one POSIX-only `terminal.Execute` test `TestTerminal_HeredocBodyWithApostropheReachesTheShell` (skip unless `hostShellIsPOSIX()`) running `cat <<'EOF'\ncapcom's store is git-native.\nEOF` and asserting the output holds that line and no `could not parse`.
**Acceptance:**
- `go build ./internal/tools/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestTerminal_' ./internal/tools/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestConsoleOpen' ./internal/tools/`
**Commit:** `fix(tools): pre-flight a POSIX line without its heredoc bodies`
