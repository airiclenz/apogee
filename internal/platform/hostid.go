package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"sync"
)

// machineIDPaths are the files consulted, in order, for a stable machine identifier.
// Both are plain text holding one opaque id; systemd writes the first, dbus the
// second, and a host that has neither falls back to its hostname (hostIDFrom). They
// are read directly rather than shelled out to (no ioreg/hostnamectl), which keeps
// HostID dependency-free and correct on OSes where neither file exists.
var machineIDPaths = []string{"/etc/machine-id", "/var/lib/dbus/machine-id"}

// hostIDOnce computes the id once per process, so every caller in a session — the
// config resolution, the startup notice, `/confine off --save` — sees the same value
// even if the underlying files are rewritten underneath a long-running process.
var hostIDOnce = sync.OnceValue(func() string {
	hostname, err := os.Hostname()
	if err != nil {
		// A host that cannot name itself still gets an id: hostIDFrom labels it
		// "unknown" and the hash carries whatever identity is left.
		hostname = ""
	}
	return hostIDFrom(hostname, readMachineID(machineIDPaths))
})

// HostID returns a stable identifier for the machine this process runs on, shaped as
// <sanitized hostname>-<first 6 hex of sha256(machine identifier)> — e.g.
// "devbox-a1b2c3". It is the interlock behind the host-scoped confinement
// acknowledgement (`unconfined-hosts:`, ADR 0012 amendment 2026-07-21): an id
// recorded in ~/.apogee/config.yaml is matched against this value, so an
// acknowledgement made on one machine does not silently apply on another.
//
// It is a safety interlock, not an authentication mechanism: its job is to stop an
// acknowledgement silently travelling between machines, not to resist forgery.
// Anyone who can edit config.yaml can write any id they like — that is fine and
// expected, exactly as internal/security/doc.go says the dangerous-action guard "is
// NOT a security boundary".
//
// Accepted trade-off: an ephemeral container that gets a fresh machine-id per run
// will not match a stored acknowledgement and will re-prompt. That fails closed (the
// safe direction), and `/confine off` without `--save` is always available for those
// users, so the annoyance has a one-word answer.
//
// The machine identifier is the first available of /etc/machine-id,
// /var/lib/dbus/machine-id, else the hostname itself. The result is deterministic
// within a process and across runs on the same machine, is never empty (a failing
// os.Hostname() yields "unknown-<hash>"), and contains only [A-Za-z0-9_.-] so it is
// safe as an unquoted YAML scalar.
func HostID() string { return hostIDOnce() }

// hostIDFrom composes the id from its two injected sources: hostname supplies the
// human-readable label and, when machineID is empty, the hashed identity as well —
// the last link of the fallback chain. Taking both as parameters is what lets the
// tests pin the composition without depending on the machine they run on.
func hostIDFrom(hostname string, machineID []byte) string {
	identifier := machineID
	if len(identifier) == 0 {
		identifier = []byte(hostname)
	}
	sum := sha256.Sum256(identifier)

	label := sanitizeHostLabel(hostname)
	if label == "" {
		label = "unknown"
	}
	return label + "-" + hex.EncodeToString(sum[:])[:6]
}

// readMachineID returns the first non-empty machine identifier found among paths,
// whitespace-trimmed so a trailing newline cannot change the hash. An unreadable,
// missing or blank file is not an error — it is simply the next link of the fallback
// chain — and nil means every source was exhausted.
func readMachineID(paths []string) []byte {
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if id := bytes.TrimSpace(raw); len(id) > 0 {
			return id
		}
	}
	return nil
}

// sanitizeHostLabel reduces hostname to [A-Za-z0-9_.-] by replacing every other rune
// with "-", then trims the dashes off both ends so the id can never open with one.
// The result is a label only — the identity lives in the hash — so a hostname that
// sanitizes away to nothing is not a problem: hostIDFrom labels it "unknown".
func sanitizeHostLabel(hostname string) string {
	var b strings.Builder
	b.Grow(len(hostname))
	for _, r := range hostname {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9',
			r == '_', r == '.', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
