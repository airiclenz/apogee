module github.com/airiclenz/apogee

go 1.26.11

// v1.x was an accidental pre-production series; the project restarted at v0.x
// (2026-07-23). The Go module proxy and checksum database keep what they once
// served, so the numbers below are burned: no release may reuse them, and every
// other v1.x number, v1.0.0 included, stays releasable. Go reads retractions
// from the go.mod of a module's highest version, and v1.8.0's immutable go.mod
// still retracts the whole [v1.0.0, v1.8.0] range. So the highest burned number,
// v1.8.1, is a placeholder: `publish` tags it on v1.0.0's commit, where it carries
// this block above v1.8.0 and retracts itself. A bad placeholder is recovered by
// appending the next v1.8.x. The list is internal/update's RetractedVersions;
// the release tool refuses a go.mod that retracts anything else.
retract (
	v1.1.0
	v1.7.0
	v1.8.0
	v1.8.1
)

require (
	charm.land/bubbles/v2 v2.1.0
	charm.land/bubbletea/v2 v2.0.8
	charm.land/lipgloss/v2 v2.0.4
	github.com/airiclenz/llama-launcher v1.6.1
	github.com/atotto/clipboard v0.1.4
	github.com/charmbracelet/colorprofile v0.4.3
	github.com/charmbracelet/ultraviolet v0.0.0-20260803092147-8b693049ce2a
	github.com/charmbracelet/x/ansi v0.11.7
	github.com/charmbracelet/x/term v0.2.2
	github.com/charmbracelet/x/termios v0.1.1
	github.com/charmbracelet/x/vt v0.0.0-20260823001701-96af6d2cb5f6
	github.com/creack/pty v1.1.24
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510
	github.com/lucasb-eyer/go-colorful v1.4.0
	github.com/mattn/go-runewidth v0.0.23
	github.com/modelcontextprotocol/go-sdk v1.6.1
	github.com/muesli/cancelreader v0.2.2
	github.com/rivo/uniseg v0.4.7
	github.com/spf13/cobra v1.10.2
	golang.org/x/net v0.62.0
	golang.org/x/oauth2 v0.35.0
	golang.org/x/sys v0.49.0
	golang.org/x/text v0.43.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/charmbracelet/x/exp/ordered v0.1.0 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/term v0.47.0 // indirect
)
