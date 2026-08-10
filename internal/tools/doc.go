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
// tool already computed for its own header. Exactly SEVEN built-ins attach one: read_file
// (ReadSpan), write_file (WroteBytes), list_dir (ListedEntries), grep (MatchedLines),
// view_diff (DiffStat), web_search (SearchHits) and open_file (OpenedFile) — the seven whose
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
// edit_existing_file, a pure-Go view_diff, and a read-and-locate open_file. The write
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
// present_document (ADR 0019) is the Asker pattern applied to showing a finished document:
// the model names a deliverable it has written and the HOST picks the mechanism (the
// presentation ladder — the transcript baseline always, the OS opener on a local desktop, a
// doc-server URL when remote), so no platform reasoning reaches the model. It is ReadOnly and
// mode-independent through the host's Presenter delegate, registered only when one is
// supplied, and its result names the rung actually reached so the model can relay it
// truthfully; a failed mechanism degrades to the baseline rather than failing the call.
package tools
