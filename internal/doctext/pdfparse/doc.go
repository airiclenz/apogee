// Package pdfparse is apogee's in-tree copy of the PDF parser github.com/ledongthuc/pdf, itself
// a fork of rsc.io/pdf. It is the parser internal/doctext extracts text with, and nothing else
// imports it.
//
// # Provenance
//
// The files ascii85.go, lex.go, name.go, page.go, ps.go, read.go and text.go are the root
// package of github.com/ledongthuc/pdf at v0.0.0-20250511090121-5959a4027728 (upstream commit
// 5959a4027728), copied verbatim apart from the package clause and gofmt; the upstream
// examples/ and pdfpasswd/ directories were not copied. The code is the Go Authors' and stays
// under its BSD-style licence, kept beside it in LICENSE.
//
// The parser lives here rather than as a module dependency so it can be fixed at the root: it
// was written for well-formed documents, and apogee feeds it whatever a user or a download hands
// over. golangci-lint skips this directory (.golangci.yml) so the copy stays diffable against
// upstream; gofmt and go vet still hold over it.
//
// # Local patches
//
// Every change made to the copied files after the copy is listed here, one entry per patch, so
// the distance from upstream is readable without a diff.
//
// None yet.
package pdfparse
