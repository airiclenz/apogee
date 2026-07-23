package tui

import (
	_ "embed"
	"strings"
)

// apogeeLogoRaw is the embedded block art byte-for-byte as checked out. A core.autocrlf=true
// checkout (common on Windows) materialises logo.txt with CRLF endings and go:embed keeps them,
// so the raw string cannot be painted directly — a trailing \r on every art line breaks the
// wide-layout width math and smears the start-up box.
//
//go:embed logo.txt
var apogeeLogoRaw string

// apogeeLogo is the block-art "APOGEE" wordmark the one-time start-up box paints (render.go).
// The source art is graphics/apogee-logo.md, a tracked branding asset next to the SVG logos; it
// is copied here as logo.txt (art, not markdown) so the binary embeds it with no runtime file
// read. Embedding keeps the exact leading/trailing spaces the block art relies on; line endings
// are normalised to LF here so rendering is checkout-independent, and newModel trims the single
// trailing newline at use so the art carries no blank last line.
var apogeeLogo = strings.ReplaceAll(apogeeLogoRaw, "\r\n", "\n")
