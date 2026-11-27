---
title: The parsers of untrusted input have no fuzz targets
status: open
type: task
priority: 2
created: 2026-11-27T18:35:35Z
---

The module has no Go fuzz target (`func Fuzz…` appears in no test file). The parsers that read
hostile bytes are checked only by hand-written cases and by whole-repo audits, and the audits
keep finding the same class: the PDF parser alone has eight closed issues (`pdf-unclosed-array-hang`,
`pdf-parser-unbounded-nesting`, `pdf-forged-offset-nesting`, `pdf-odd-hex-panic`,
`pdf-reference-quadratic-scan`, `pdf-indirect-predictor-unscanned`, `pdf-ceiling-not-discriminating`,
`served-pdf-csp-sandbox`), and hostile PDFs turned up in five of eight audits.

`internal/doctext/pdfparse` is a patched fork of `rsc.io/pdf` and is excluded from golangci-lint in
`.golangci.yml` on purpose (copied verbatim, patches listed in `doc.go`), so it also gets no lint.

Fuzz targets for, at least:
- PDF text extraction (`internal/doctext` / `pdfparse`): no panic, no stack overflow, bounded time
  and memory on any input.
- The model stream readers (OpenAI SSE and Anthropic Messages) and tool-call salvage
  (tool_call-tag and whole-text shapes).
- MCP message decoding.
- Any other parser `internal/` feeds model, server or workspace bytes into (survey first).

Seed corpora are committed next to the targets (`testdata/fuzz/`). A crasher a run finds is
committed as a seed with its fix, so it stays a regression test in `make test`.

Machine note: the fuzz engine is memory-hungry. On a small box run one target at a time, with
`GOMEMLIMIT` and a short `-fuzztime`; never several at once.

## Why

Audits find these bugs after they ship; a fuzzer finds them before. A guard that a fuzzer keeps
exercising cannot quietly stop covering a sibling input shape.

## Acceptance

- Every parser of untrusted input named in the body has a `Fuzz…` target with a committed seed corpus.
- A scheduled CI job (not the push gate) runs each target for a bounded `-fuzztime` and fails on a new crasher.
- `make test` replays the committed corpora (plain `go test` runs seeds without fuzzing).
- The decision on linting `pdfparse` is written down — a narrow linter set for the fork, or why the exclusion stays — in `pdfparse/doc.go` or an ADR amendment.
