// Package doctext turns a document's BYTES into the text a model can read — and nothing else.
// It knows formats: how to recognise one from its leading bytes, how to walk it, how to mark up
// what came out, and how to word every way the walk can fail. It knows nothing about tools,
// calls, results, budgets or the agent loop, and it must stay that way.
//
// # Why it is not part of internal/tools
//
// A document FORMAT has one reason to change — the format, or the parser behind it — and it now
// has two consumers that share nothing else: the read_file tool, which reports a failure as an
// IsError tool result (ADR 0007), and the loop's @file reference resolver, which reports one as
// an ErrorEvent and injects nothing. Neither failure channel belongs to the other, but the
// extraction, the page markers and the model-facing sentences must be IDENTICAL whichever seam
// asked — a document that reads one way through a tool call and another way through a reference
// teaches the model that the two are different documents.
//
// Keeping the extractor inside internal/tools would have forced the engine to import the tool
// package to read a PDF, dragging the whole built-in surface — dispatch shapes, argument
// decoding, the workspace fence — behind a question about bytes. The dependency also points the
// wrong way: the loop is the inner ring. So the format lives in its own package that both sides
// import and neither owns.
//
// # What is here
//
// pdf.go is PDF text extraction: the %PDF- content sniff (IsPDF — the FILE NAME never decides,
// so a text file called notes.pdf reads as text and a PDF saved without the extension still
// extracts), the in-memory parse-and-walk behind a recover because the parser panics on
// malformed input (ExtractPDF), the [Page N] marker line before each page, the model-facing
// wording for a scanned-image or unreadable document, and the header annotation both consumers
// stamp on what they extracted (PDFAnnotation).
//
// A second format is a sibling file with the same three-function shape, not a new abstraction:
// nothing here is generalised over formats until a second one exists to generalise from.
package doctext
