---
title: A syntax stream whose /Predictor or /DecodeParms is a reference is depth-scanned unpredicted
status: done
type: bug
priority: 3
labels: [security]
plan: 2026-11-11 - 00 - pdf-parser-fork-and-defects-plan
created: 2026-11-11T05:56:07Z
closed: 2026-11-11T10:11:34Z
reason: "Resolved by item 2: the parser's own depth bound refuses /ToUnicode and /ObjStm with indirect /DecodeParms hiding nesting, pinned in pdf_test.go (plan '2026-11-11 - 00 - pdf-parser-fork-and-defects-plan', archived at 87846d40)"
---

The PDF nesting preflight models the parser's PNG Up predictor (/Predictor 12) when decoding an /ObjStm, /CMap or /ToUnicode stream for its depth scan, but a /Predictor or /DecodeParms given by reference is not followed (`internal/doctext/pdf.go:971`, `readPDFPredictor`), so the stream is scanned unpredicted and its `[` bytes can hide from the scan while the parser, which resolves the reference, decodes and recurses into them. Stated as a known limit in ExtractPDF's doc. Found by the audit-defects run, item 1 (docs/plans/archived/2026-11-10 - 02 - audit-defects-plan.md).

## Why

It is a remaining way past the nesting bound that pdf-parser-unbounded-nesting closed.

## Acceptance

- A /ToUnicode CMap and an /ObjStm whose /DecodeParms (or /Predictor) is an indirect reference to PNG Up, hiding deep nesting, are refused, pinned by tests in internal/doctext/pdf_test.go
