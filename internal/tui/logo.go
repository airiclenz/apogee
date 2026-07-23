package tui

import _ "embed"

// apogeeLogo is the block-art "APOGEE" wordmark the one-time start-up box paints (render.go).
// The source art is graphics/apogee-logo.md, a tracked branding asset next to the SVG logos; it
// is copied here as logo.txt (art, not markdown) so the binary embeds it with no runtime file
// read. Embedding keeps the exact leading/trailing spaces the block art relies on; newModel trims
// the single trailing newline at use so the art carries no blank last line.
//
//go:embed logo.txt
var apogeeLogo string
