# Code Review — apogee, whole repository — 2026-11-27

**Scope:** the whole module (cmd/apogee, internal/*, scripts, CI and build files). Concurrency review covered only eight of the nineteen code groups (agent, tools, tui core and similar). The project's own checks ran: `go vet`, golangci-lint and govulncheck were clean. The full test suite timed out, so there is no race-detector or coverage data.
**Mission:** apogee is a terminal AI coding agent for smaller, locally hosted LLMs. It must never make a model perform worse than the bare loop, and it must stay usable on slow, small machines.
**Files reviewed:** 1632

## Executive Summary

The most serious problem is the vendored PDF parser (`internal/doctext/pdfparse`). A hostile or corrupt PDF opened with `read_file` or `@file` can kill the whole agent. A roughly 100-byte file makes the xref reader allocate tens of GiB, and a 30-byte CMap does the same. A debug print also writes raw document bytes, escape sequences included, to stdout. Outside the parser the code matches its documented contracts well, and the independent checks found no vulnerabilities. Two systemic patterns recur. First, several fixed deadlines (toolchain probe, undo capture, tree snapshot, git tool calls) break the project's own "slow machine makes apogee slower, never broken" rule. Second, a guard or bound exists for one case and was never extended to its sibling (unbounded reads of repo-controlled files, scrubbing applied to one tool but not another).

## Intent & Architecture Findings

### Medium — Fixed deadlines in shipped code fail healthy runs on slow hardware `[Intent & Structure + Correctness]`

- **Where:** `cmd/apogee/toolchain_roots.go:33` (5 s), `internal/agent/agent.go:1664` (2 min), `internal/agent/treesnapshot.go:56` (2 s), `internal/tools/git.go:50-58` (15 s and 30 s)
- **What:** Each is a fixed wall-clock cap on a subprocess or disk wait. The `go env GOROOT GOMODCACHE` probe is cached for the whole process, so one timeout silently drops the GOROOT and GOMODCACHE read roots for the entire run with no retry. The Exchange-close undo capture degrades to funnel-only coverage on expiry. A missed tree snapshot silently loses the "this command changed workspace files" warning. A git tool call that times out is reported to the model as a failure, and a timed-out `git add` in `move_file` or `delete_file` can leave the index partly staged. Sibling budgets were already sized for slow boxes (`vetTimeout` 300 s, `treeProbeTimeout` 30 s).
- **Why it matters:** On a throttled single-board box with a cold cache or a large repo, a healthy run is reported as failed or silently loses safety coverage. AGENTS.md calls this a bug (label `slow-box`). *(independently verified)*
- **Fix:** Size each budget like `vetTimeout`, or wait on a condition with a generous backstop. Re-probe the toolchain on a later Turn after a timeout and say once when giving up. Surface a skipped tree snapshot once per session. Do not shorten the underlying git work to fit.

## Critical & High Findings

### High — Xref start offset from a PDF sizes an unbounded allocation `[Intent & Structure + Correctness + Security]`

- **Where:** `internal/doctext/pdfparse/read.go:437-443` (also `:340-343`)
- **What:** `readXrefTableData` and `readXrefStreamData` grow the xref table up to a subsection start (or `/Index` start) read straight from the bytes. It is never compared to the file size or `/Size`, and the trim to `/Size` happens after the growth. With `/W [0 0 0]` plus a huge `/Index` count the stream path grows with no budget charge.
- **Why it matters:** A ~100-byte PDF allocates tens of GiB. Go's out-of-memory abort cannot be recovered by `ExtractPDF`'s recover, so the whole agent or TUI dies on a `read_file` or `@file` of a hostile document. This contradicts the invariant stated at `pdf.go:28-31`. *(independently verified)*
- **Fix:** In both readers refuse a subsection whose `start` plus `n` exceeds the file end (or `/Size`). Add `/Index` and xref-table subsection headers to the preflight beside `Size`. This is a natural fuzz target (open issue `untrusted-parsers-unfuzzed`).

### High — ToUnicode CMap entry count taken from the document allocates until OOM `[Security]`

- **Where:** `internal/doctext/pdfparse/page.go:425-443`
- **What:** `readCmap` takes the `beginbfchar`/`beginbfrange` loop count `n` from the document and loops `n` times. On an empty stack each pass pops a zero Value and appends an entry, with no read charged, so neither the ReadAt budget nor ctx sees it.
- **Why it matters:** A deflated stream of about 30 bytes (`4000000000 beginbfchar endbfchar`) allocates tens of GiB and kills the process, the same outcome as the xref finding above. *(independently verified)*
- **Fix:** Cap `n` by the stack depth (`min(n, stk.Len()/2)`, `/3` for bfrange) and reject negative or larger counts with `ok=false`.

### High — CMap decode is quadratic and uninterruptible `[Correctness]`

- **Where:** `internal/doctext/pdfparse/page.go:324-383`
- **What:** `cmap.Decode` scans every `bfchar` and `bfrange` entry linearly for each decoded character, and `Page.GetPlainText` never checks ctx inside a page.
- **Why it matters:** A small deflated ToUnicode CMap with 10^5 to 10^6 entries (the 64 MiB inflate budget allows it) plus long `Tj` strings makes `read_file` hang for minutes. The read budget, the page cap and cancellation cannot interrupt it. *(independently verified)*
- **Fix:** Index `bfchar` by code in a map, cap the entry count in `readCmap`, and poll ctx in the decode loop.

## Medium Findings

### Medium — Debug `Printf` in the PDF dictionary reader writes hostile bytes to stdout `[Intent & Structure + Correctness + Security]`

- **Where:** `internal/doctext/pdfparse/lex.go:550`
- **What:** `buffer.readDict` runs an unconditional `fmt.Printf("DEBUG: %T(%v)\n. Skip dict", tok, tok)` when a dictionary key is not a name. Every other debug print in the package is behind `DebugOn`.
- **Why it matters:** Any slightly corrupt PDF writes to process stdout. That corrupts the Bubble Tea frame, breaks the machine-readable channel of `apogee headless` and the daemon, and breaks the wire-silent engine invariant. `%v` of a string token prints attacker bytes raw, so a hostile PDF can inject terminal escape sequences (OSC 52 clipboard write, title change, cursor moves). *(independently verified)*
- **Fix:** Delete the line (the following `errorf` already carries the text), or gate it on `DebugOn` and quote with `%q`. List it in the pdfparse `doc.go` local patches.

### Medium — Project config is read with no size cap or regular-file check `[Security]`

- **Where:** `internal/config/layer.go:385`
- **What:** `readProjectLayer` reads the repo-controlled `<root>/.apogee/config.yaml` with an unbounded `os.ReadFile` after only `os.Stat` and `projectRealPath`. This happens before any adoption prompt and again on every live re-read and daemon Firing.
- **Why it matters:** A cloned repo can make this file a symlink to `/dev/zero`, a blocking device, or a multi-GB file. That exhausts memory (a watchdog reset on a Pi-class box) or hangs startup. Impact is denial of service only. *(independently verified, High lowered to Medium)*
- **Fix:** `os.Lstat` and refuse anything not a regular file (one notice, layer skipped, as `projectwrite.go:154` does for symlinks). Read through a size cap such as `io.LimitReader(f, 1<<20)` and refuse when exceeded.

### Medium — Context files (AGENTS.md, CLAUDE.md) are read unbounded `[Security + Correctness]`

- **Where:** `internal/agent/contextfiles.go:100-107`
- **What:** `readContextFile` does `io.ReadAll` with no size bound and no regular-file check on files a repo controls. The sibling `readFileRef` (`loop.go:1919-1935`) already caps its read at 10 MiB. The in-code comment calls the read deliberately unbounded, but a malicious repo is in the threat model.
- **Why it matters:** A hostile repo with a multi-hundred-MB AGENTS.md exhausts memory at session start, `/clear` and resume, since several copies are made. A FIFO at that name blocks session start forever. *(independently verified)*
- **Fix:** Check for a regular file and read through a cap, as `readFileRef` does. Refuse or truncate with a visible notice above the cap.

### Medium — PDF read budget is exhausted by valid long documents and reports a false cause `[Intent & Structure + Correctness]`

- **Where:** `internal/doctext/pdf.go:141` (with `pdfparse/page.go:25-56` and `:195-200`)
- **What:** `pdfMaxReads = 200_000` is too low. `Reader.Page(n)` re-walks `/Kids` on every call, costing about N^2/2 reads for a flat page tree. `Font.Encoder()` has a value receiver, so its cache never persists and each `Tf` re-parses the whole CMap, charged to the budget.
- **Why it matters:** Valid flat-tree manuals of roughly 400-600+ pages return nothing and fail with the false cause "the document's object graph does not terminate". Failure is a clean refusal, not a crash. *(independently verified, High lowered to Medium)*
- **Fix:** Walk the page tree once into a page list. Use a pointer receiver or the `fonts` map for the encoder cache. Or charge the budget per page.

### Medium — Content-stream interpreter stack is uncapped `[Security]`

- **Where:** `internal/doctext/pdfparse/ps.go:56-140`
- **What:** `Interpret` pushes one 32-byte Value per token with no stack cap, and page callbacks copy the whole stack at the next operator.
- **Why it matters:** A deflated 64 MiB content stream of `1 ` tokens (about 64 KB on disk) forces roughly 1.3 GB or more of memory, which is dangerous on an 8 GB box. *(independently verified)*
- **Fix:** Cap `stk.Len()` in `Interpret` (for example `1<<16`) and fail the page past it.

### Medium — Per-page re-inflation breaks the documented single budget `[Intent & Structure]`

- **Where:** `internal/doctext/pdf.go:1852-1881`
- **What:** `pageContentNesting` calls `readPDFSyntax` with a fresh 64 MiB inflate limit per page, and `GetPlainText` inflates the same stream again. The preflight charges a shared `/Contents` stream once.
- **Why it matters:** 2000 pages sharing one large content stream cost minutes of CPU for one `read_file`. Cancellation still ends it. *(independently verified)*
- **Fix:** Cache the nesting verdict per content-stream object number, or pass a shared remaining-inflate budget.

### Medium — Adoption pane can hide the dangerous part of a project Allow rule `[Security]`

- **Where:** `internal/tui/adoption.go:158-162,192-197` (rendered via `popup.go:533-536, 1730-1752, 1911-1919`)
- **What:** The pane shows the whole Allow rule only in a one-row title truncated to pane width, with no body.
- **Why it matters:** A hostile repo can put a long benign-looking prefix in an Allow rule so the dangerous tail is cut off before the user adopts it. The untruncated rule appears in the transcript notice above the pane, which mitigates it. *(independently verified, High lowered to Medium)*
- **Fix:** Render the full rule wrapped in the pane body, or refuse to offer one-click adoption for a rule longer than the pane width.

### Medium — `rm -rf` hard-refusal judges only the first operand `[Correctness]`

- **Where:** `internal/security/rules.go:122`
- **What:** The `rm-rf-root-home-system` and `rm-fr-root-home-system` patterns require the protected target to be the very next token after the flags.
- **Why it matters:** `rm -rf /etc` is refused but `rm -rf build /etc`, `rm -rf ./out /` and `rm -rf node_modules /etc` slip through, so the same operation is judged differently by operand position. *(independently verified)*
- **Fix:** Insert `(?:[^\s;|&]+\s+)*?` ahead of `quoteOpen + deleteTargetAnchor` in both patterns. The class excludes separators, so it never crosses into the next command.

### Medium — `console_open` skips the env scrub that `Terminal.Execute` applies to rule-answered calls `[Security]`

- **Where:** `internal/tools/console_open.go:182`
- **What:** `Terminal.Execute` removes `CDPATH`, `BASH_ENV`, `ENV` and `BASH_FUNC_*` when an Allow rule answered the call. `console_open` runs the same shell line (`ArgRoleShellCommand`) and dispatch marks it rule-answered, but it does not call `scrubRuleAnsweredEnv`.
- **Why it matters:** An Allow rule on `console_open` (the tool ships default-off) runs with whatever hostile shell-startup environment the workspace left in apogee's own environment. *(independently verified)*
- **Fix:** Apply `scrubRuleAnsweredEnv` in `ConsoleOpen.Execute` under the same `domain.RuleAnswered(ctx)` condition.

### Medium — `write_file` loads an existing file with no size cap `[Correctness]`

- **Where:** `internal/tools/write_file.go:118` (reported at `:101`), `internal/tools/write_target.go:148-149`
- **What:** `write_file` reads the existing file with uncapped `target.read()`, once for diff regions and again for the undo pre-image. Sibling edit tools use `readCapped`.
- **Why it matters:** Overwriting a multi-GB file on an 8 GB box loads it into memory up to twice. *(independently verified)*
- **Fix:** Bound the regions pre-read with `readCapped` (fall back to `okInsertedRegion`). Cap or skip the journal pre-image above a size limit with an explicit "not undoable" note.

### Medium — mDNS lookup gives up after one second with no retransmit `[Correctness]`

- **Where:** `internal/mdns/mdns.go:25`
- **What:** `lookupTimeout = time.Second` caps a single multicast query and reports `errNoAnswer`. RFC 6762 recommends repeating the query.
- **Why it matters:** On a power-saving Wi-Fi link or against a loaded responder, a healthy `.local` model server is reported offline. This is another slow-box violation. *(independently verified)*
- **Fix:** Resend at about 250 ms and 750 ms inside a longer window of a few seconds, bounded by ctx.

## Recommended Action Order

1. Fix the three PDF memory and hang bugs first (xref start, `readCmap` count, CMap decode). They kill or stall the whole agent from one hostile file. Add fuzz targets for the PDF parser at the same time (issue `untrusted-parsers-unfuzzed`).
2. Delete or gate the debug `Printf` in `lex.go` (a one-line quick win), then cap the interpreter stack and fix the PDF read budget and per-page inflate.
3. Apply the same bounded-read treatment to the repo-controlled inputs: project config, context files and `write_file`.
4. Close the sibling-case gaps: the `console_open` env scrub, the `rm -rf` operand rule and the adoption pane rendering.
5. Replace the fixed deadlines (toolchain probe, undo close, tree snapshot, git tools, mDNS) with condition waits or generous backstops. This needs one design decision on the common pattern, so decide it once.

## What Looked Good

The static analysis and vulnerability scan came back clean. Reviewers found the project-config trust model, the Floor guards, the OAuth flow, git and network hardening in the tools, and the workspace I/O handling matching their documented contracts. The weak spots are concentrated in the vendored PDF parser and in a handful of unbounded reads or fixed deadlines. The rest of the codebase needs little change.
