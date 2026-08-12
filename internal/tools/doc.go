// Package tools holds the built-in Tool implementations that sit behind the public
// domain.Tool interface — an open extension point (ADR 0002). Tools are stateless
// across Turns: their only durable side effect is filesystem writes, and nothing
// live is held across the quiescent boundary (ADR 0008).
//
// Phase 1 (P1.4) lands the minimal local set — read_file, write_file, list_dir, and
// a pure-Go grep (no external programs, §3a) — each scoped to a sandbox root at
// construction (tools.NewReadFile(root), …) so the package carries no dependency on
// domain.Config and a test can point it at a t.TempDir(). Every path argument is
// resolved through path-safety, which rejects traversal escapes outside the root.
//
// A tool reports an expected failure (bad arguments, missing file, path escape) as a
// ToolResult with IsError set, so the model sees and can react to it; the Go error
// return is reserved for ctx cancellation. NewDefaultRegistry assembles the built-ins
// into a domain.ToolRegistry — the seam the loop's dispatch (P1.2) wires.
//
// A successful result has TWO halves, and they are for different readers. Content is the
// prose half, written for the MODEL, and its wording is free to change. A domain.ToolSummary
// attached beside it (okSummary, tools.go) is the structured half, written for a HOST — the
// TUI's tool card today, a headless or bench renderer later — carrying as data the facts the
// tool already computed for its own header. Exactly SIX built-ins attach one: read_file
// (ReadSpan), write_file (WroteBytes), list_dir (ListedEntries), grep (MatchedLines),
// view_diff (DiffStat) and web_search (SearchHits) — the six whose
// outcome a host would otherwise have to re-derive from the sentence. The rest deliberately do
// not, and that is not an omission to fill in later: quoting a fixed one-line sentence (the
// find-replace/edit family, web_fetch, http_request, ask_user, present_document) or compressing
// free-form output to a first line plus a count (terminal, python_exec, the git tools,
// diagnostics, sub_agent) is RENDERING, not scavenging — there is no re-derived fact there for a
// type to fix. A summary is optional by construction, so ADR 0002's open extension point is
// untouched (a tool that emits none renders from its prose exactly as before); it is never
// persisted and never sent to the model; and an error result never carries one.
//
// Phase 3 (P3.7) adds the file-editing family: single/multi find-replace, a patch-aware
// edit_existing_file, and a pure-Go view_diff. It also added a read-and-locate open_file,
// merged into read_file on 2026-08-11 — read_file's optional locate parameter carries that
// half now, so the roster asks one tool to read a file instead of two. The write
// tools among them carry the unexported workspaceScopedWriter marker so the dispatch
// disposition path-bounds rather than confines them (ADR 0012 D1).
//
// Phase 3 (P3.8) adds the execution tools (terminal, python-exec) and (P3.9) the git
// tools (git_branch, git_commit, git_diff_range) — SubprocessTools that shell out to a
// detected program (the system shell/interpreter, the system git) and degrade gracefully
// when it is absent (§3a). The disposition confines the write-capable ones in Auto (or
// gates them when fs-confinement is unavailable); git_diff_range is read-only and runs
// freely.
//
// Phase 3 (P3.10) adds the diagnostics tool — a read-only SubprocessTool that checks Go
// in-process (go/parser for syntax, always available) plus an optional go vet, and
// degrades gracefully to a clear "no diagnostics available" for languages with no
// provider (§3a).
//
// Phase 3 (P3.11) adds the network and host tools: web_fetch (GET), http_request (general
// request), and web_search (DuckDuckGo by default; a config'd custom endpoint, or "off" to
// disable) — in-process net/http
// ExternalEffectTools of kind network (the disposition auto-runs them in Auto, url-filtered,
// and routes them through ExternalEffects for the bench). They are url-filtered BECAUSE they
// route through one funnel (network.go's networkTool.do, the single path from a tool to the
// network), which applies the host's security.URLGuard — whose default-on, resolved-IP SSRF
// floor blocks loopback / private / metadata addresses pre-flight AND at dial time, closing
// DNS-rebinding — and renders every failure message host-scoped. Embedding the funnel is the
// only way to obtain the unexported url-filter marker, and the disposition keys on that marker
// rather than on the declared effect kind: a network tool without it gates in Auto instead of
// running unattended (ADR 0012 Amendment 2026-07-25). ask_user routes a
// free-text question to the host's Asker delegate (the public analogue of Approver); it is
// ReadOnly (runs in Plan, mode-independent) and is registered only when an Asker is supplied
// (NewDefaultRegistryWithHost threads the URLGuard, search endpoint, and Asker from Config).
// The MCP tools land in P3.15.
//
// find_files (2026-08-10) is the NAME half of discovery, beside grep's content half: a
// read-only walk returning workspace-relative paths whose BASE NAME matches a comma-separated
// glob list — grep's own include syntax, parsed by grep's own parser, so the two tools cannot
// drift on what a glob means or on a malformed one matching nothing. It is always recursive
// (a capability a model must not have to find in a parameter — list_dir's recursive flag was
// missed by two independent polls, which is why this is a named tool and list_dir is
// untouched), skips grepExcludeDirs, and paginates like grep. It attaches no ToolSummary: what
// it reports IS the list of names, and its header states the count in prose.
//
// git_status (2026-08-10) joins the P3.9 git family as its fourth member: a read-only report
// of the current branch, the ahead/behind divergence from its upstream when one exists, and
// the staged / unstaged / untracked path lists. It reads git's porcelain v2 format with -z, so
// a path with a space, a quote, or a newline arrives verbatim rather than C-quoted, and it
// caps EACH list (maxGitStatusPaths) while stating the full count in the section header — a
// tree mid-refactor must not flood a small model's context. Like git_diff_range it declares
// ReadOnly() and still carries the subprocess marker, which is what classifies the call.
//
// git_log (2026-08-10) is the family's fifth member and the history half of git_status's
// working-tree half: one line per commit — short hash, iso-strict date, subject — for an
// optional ref (default HEAD) and an optional max_count (default 20, ceiling 100, because a
// full log is exactly the kind of output that buries a small model). Its ref takes the same
// two-part guard as git_diff_range's (the conservative character class plus an explicit
// leading-"-" rejection), and its argv ends in "--" for the reason buildBranchArgs does:
// `git log <name>` on a name that is a tracked PATH rather than a ref is a pathspec log, which
// answers a different question with exit 0 — a wrong history reported as success. Like
// git_diff_range and git_status it declares ReadOnly() over the outranking subprocess marker.
//
// copy_file and move_file (2026-08-10) are the P3.7 family's move-bytes-that-already-exist half:
// two write tools taking a source and a destination instead of a path and a payload. Both refuse
// an occupied destination unless the call passes overwrite (a silent clobber is the one mistake a
// model cannot undo), both refuse a directory at either end (a recursive copy is a different tool
// with a different blast radius), and both go through internal/security's os.Root-pinned
// primitives — SafeCopyFileFrom for the copy, SafeRename plus SafeCopyFile and SafeRemove for the
// move — so the fence is decided at OPERATION time at BOTH ends, never on a re-walked path string.
// The two ends need not share a root: copy_file (2026-08-12) resolves its SOURCE over the READ
// scope, so an ABSOLUTE path the workspace refuses may still match a configured read-only root (the
// skills library) and be read through an os.Root pinned at THAT root, while its DESTINATION stays
// pinned to the workspace — the only end either tool writes. move_file keeps BOTH ends
// workspace-fenced: a move removes its source, which is a write, and extra roots are read-only.
// A copy lands with the source's mode (a 0755
// script copied 0644 is a broken copy) and is atomic at the destination name, like every other
// write here; a move renames, falling back to copy-then-remove for a filesystem that cannot
// rename across the two paths. They carry the workspaceScopedWriter marker, and it resolves their
// DESTINATION — where the write lands — because the source is fenced by the operation itself
// (destinationArgWriteTarget, workspace_scoped.go).
//
// delete_file (2026-08-10) closes that family with its remove-bytes half: one path, files only (a
// directory is a different blast radius), removed through SafeRemove's pinned os.Root so the fence
// is decided at REMOVE time. It names a single file, so its marker resolves `path` like every other
// single-file writer. Its dangerous-action classification is the one ADR 0012's ruleset already
// assigns and needs no new rule: that ruleset is precision-over-recall — almost-never-legitimate AND
// catastrophic — and deleting one workspace file is an ordinary refactoring step, the near-miss the
// shipped rules deliberately do not fire on. The wiring that matters is structural and already
// there: `path` is not a payloadKey, so the credential/persistence rules inspect a delete_file
// target and hard-refuse it in every mode, ahead of the ladder.
//
// run_tests (2026-08-10) asks diagnostics' question of the whole project instead of one file:
// it DETECTS the runner from project markers — go.mod ⇒ `go test`, a pytest config ⇒ `pytest`,
// a package.json test script ⇒ `npm test`, in that order — and returns a CONDENSED verdict, never
// the log: PASS/FAIL, the counts the runner reports (or the failing count derived from the blocks
// recognised), and the first failing tests with their first error lines, the whole result
// hard-capped at 8 KiB with a closing note naming the full output size and the command to re-run
// under terminal. Flooding a small context is the failure it exists to prevent, so the cap binds
// the text that leaves the tool. Detection rather than a model-supplied command is the same
// discovery lesson find_files embodies: a capability found by tool NAME beats one found by
// parameter. It is a write-capable SubprocessTool on terminal's disposition — a suite runs the
// project's own code — over the shared runSubprocess, with a 5-minute ceiling and the inherited
// environment the toolchains need.
//
// present_document (ADR 0019) is the Asker pattern applied to showing a finished document:
// the model names a deliverable it has written and the HOST picks the mechanism (the
// presentation ladder — the transcript baseline always, the OS opener on a local desktop, a
// doc-server URL when remote), so no platform reasoning reaches the model. It is ReadOnly and
// mode-independent through the host's Presenter delegate, registered only when one is
// supplied, and its result names the rung actually reached so the model can relay it
// truthfully; a failed mechanism degrades to the baseline rather than failing the call.
//
// # The tool files, one line each
//
// Twenty-two files carry the built-ins, grouped by what a call to them can do — which is
// also what the dispatch disposition keys on (ADR 0012). A file holds a tool FAMILY, not
// always a single tool: the three-tool file_ops.go and the five-tool git.go each keep a
// family's shared argument shape and error wording in one place.
//
// Reading and discovery. read_file.go is read_file, the line-spanned read that attaches a
// ReadSpan and, when its optional locate term is given, also reports the absolute line
// numbers where a substring sits. list_dir.go is list_dir, the depth-bounded listing. grep.go is grep — the
// pure-Go content search plus the include-glob parser, match spans and pagination that
// find_files borrows so the two cannot drift. find_files.go is find_files, the NAME half of
// discovery beside grep's content half.
//
// Writing and editing (the P3.7 family). write_file.go is write_file, the whole-file atomic
// write. file_edit.go is edit_existing_file, the patch-aware hunk apply. find_replace.go
// carries the pair single_find_and_replace and multi_find_and_replace. file_ops.go carries the
// three move-bytes-that-already-exist tools — copy_file, move_file, delete_file — over
// internal/security's os.Root-pinned primitives. diff.go is view_diff, the pure-Go LCS diff
// that reports on that family and is itself read-only.
//
// Execution. terminal.go is terminal, the one-shot shell command through platform.Shell.
// python_exec.go is python_exec, the one-shot interpreter run. run_tests.go is run_tests: the
// runner detection (go.mod / pytest config / package.json), the failure parsing per runner,
// and the 8 KiB condensed verdict. diagnostics.go is diagnostics — go/parser in process, an
// optional go vet, and the graceful "no provider" for every other language. git.go is the
// whole git family: git_branch, git_commit, git_diff_range, git_status and git_log, with the
// ref guards and porcelain-v2 parsing they share.
//
// The subprocess plumbing. exec_common.go is the single runSubprocess every execution tool
// above calls — the ceilings, the default timeout, the capped output buffer, and the
// exit-code result shape. exec_teardown.go holds the OS-independent half of the teardown
// contract (§2.4): planTreeKill, the treeKillAction it returns, and the processTeardown seam —
// including the reap that runs when a command completes rather than being cancelled.
// exec_pgroup_unix.go realises that teardown as a POSIX process group — Setpgid, a
// negative-PID kill, a bounded WaitDelay — and exec_pgroup_other.go realises it on Windows
// with a Job Object, the only facility there that holds a whole tree.
// exec_cmdline_unix.go is a no-op because execve takes a real argv;
// exec_cmdline_other.go hands Windows the raw command line verbatim through
// SysProcAttr.CmdLine, bypassing the argv joining cmd.exe cannot read.
//
// Network. network.go is the funnel itself — networkTool.do, the single path from a tool to
// the network — carrying the URLGuard pre-flight and dial-time checks, the one per-call
// deadline, the response cap, and the unexported url-filter marker the disposition keys on.
// web_fetch.go is web_fetch (GET plus its body rendering), http_request.go is http_request
// (any method, headers, body), and web_search.go is web_search — provider selection between
// the built-in DuckDuckGo, a configured custom endpoint and off, plus the SetEndpoint
// re-point. web_search_render.go is that tool's rendering half: the DuckDuckGo HTML parse and
// the generic HTML-to-text cleaning, best-effort by design.
//
// Host delegates and the recursion point. ask_user.go is ask_user over the host's Asker,
// including the queueing that keeps concurrent askers off one prompt. present_document.go is
// present_document over the host's Presenter ladder, naming the rung actually reached.
// sub_agent.go is sub_agent — the recursion point that drives a nested Agent and deliberately
// carries none of the disposition markers.
//
// # The package spine, one line each
//
// Four files register no tool. tools.go is the shared toolSpec (name, description, JSON
// schema) every built-in embeds, the size ceilings they all read, and the result helpers —
// including okSummary, which attaches the structured half. registry.go is HostTools and the
// two assemblers, NewDefaultRegistry and NewDefaultRegistryWithHost, that turn the built-ins
// into a domain.ToolRegistry. path_safety.go is the thin alias layer onto internal/security's
// one symlink-aware boundary (ResolveInRoot, ErrPathEscape), so every tool and test here keeps
// calling the same names while the rule lives in one place — plus readScope, the READ-only
// multi-root resolver that tries the workspace first and then any extra read-only roots the
// host mounts, returning the matched root so a caller pins every later fenced operation to it.
// workspace_scoped.go is the
// unexported workspaceScopedWriter marker and the write-target resolvers that say WHICH
// argument a given writer lands on.
//
// And doc.go this map.
package tools
