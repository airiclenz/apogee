package tui

import "github.com/airiclenz/apogee/internal/sanitize"

// ----------------------------------------------------------------------------
// The escape-stripping seam — what untrusted text is allowed to carry to a cell
// ----------------------------------------------------------------------------
//
// This file holds the package's security seam: the one rewrite every untrusted string passes
// through before it reaches a display surface, called from every producer-facing entry point in
// the package (ADR 0043). Its rule and the reason the frame needs one are stated in doc.go's second
// invariant — untrusted text is escape-stripped at the SEAM it enters the view through, never at
// each producer.
//
// What the strip DOES — which characters go, which two stay, and why — is no longer written here:
// it is spelled once, for the whole module, in internal/sanitize, and this file is three delegates
// onto it. That package exists because this set was written out four times (here, internal/title,
// internal/session, and the headless CLI) and the fourth copy had drifted; a copy of it anywhere,
// this file included, is a bug. Read internal/sanitize for the contract.
//
// The delegates stay because the seam is the package's own vocabulary: two dozen call sites, the
// doc.go invariant and the tests that pin them all say stripEscapes, and a seam that is grep-able
// under one name in the package it guards is worth three lines. Nothing here knows a transcript, an
// entry or a Model — it is pure string work over runes, and that is the whole of its contract.

// stripEscapes is [sanitize.StripEscapes] — the C0 control characters, DEL and the bidi formatting
// set dropped from untrusted text, the newline and the tab kept because this package's biggest
// callers are wrapped bodies railed by them.
func stripEscapes(s string) string { return sanitize.StripEscapes(s) }

// bidiControl is [sanitize.BidiControl] — the reordering characters stripEscapes drops beside the
// control ones, deliberately the bidi set and not the whole of unicode.Cf.
func bidiControl(r rune) bool { return sanitize.BidiControl(r) }

// stripEscapesAll is [sanitize.StripEscapesAll] — the batch form, so a batch of untrusted labels
// (an approval request's choices) is sanitized in one call.
func stripEscapesAll(xs []string) []string { return sanitize.StripEscapesAll(xs) }
