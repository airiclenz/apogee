package tui

import (
	"strings"
	"testing"
)

// The logo art must reach the renderer with LF endings regardless of how the checkout
// materialised logo.txt: a core.autocrlf=true checkout (Windows) embeds CRLF, and a stray \r on
// each art line both miscounts as a printable cell in the width math and executes as a carriage
// return in the terminal, smearing the wide start-up card. logo.go normalises the embedded bytes;
// this pins that the normalised string is what the renderers consume.
func TestApogeeLogoCarriesNoCarriageReturns(t *testing.T) {
	if strings.Contains(apogeeLogo, "\r") {
		t.Errorf("apogeeLogo contains carriage returns; embed normalisation in logo.go is broken")
	}
}
