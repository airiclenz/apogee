package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The composition — <sanitized hostname>-<first 6 hex of sha256(machine identifier)> —
// is what an acknowledgement stored in config.yaml is matched against, so it is pinned
// against hand-computed digests rather than a re-implementation of the formula. Both
// sources are injected, so the expectations hold on any machine the test runs on.
func TestHostIDFromComposition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		hostname  string
		machineID []byte
		want      string
	}{
		{
			name:      "machine id supplies the hash, hostname the label",
			hostname:  "devbox",
			machineID: []byte("deadbeefdeadbeefdeadbeefdeadbeef"),
			want:      "devbox-a5c6c1",
		},
		{
			name:     "no machine id falls back to hashing the hostname",
			hostname: "devbox",
			want:     "devbox-2ff8dd",
		},
		{
			name:      "an empty machine id is the same fallback as a missing one",
			hostname:  "devbox",
			machineID: []byte{},
			want:      "devbox-2ff8dd",
		},
		{
			name:      "the allowed punctuation survives sanitization",
			hostname:  "laptop.local_2-a",
			machineID: []byte("abc"),
			want:      "laptop.local_2-a-ba7816",
		},
		{
			name:      "everything outside [A-Za-z0-9_.-] becomes a dash",
			hostname:  "my box!/2",
			machineID: []byte("abc"),
			want:      "my-box--2-ba7816",
		},
		{
			name:      "non-ASCII is sanitized and the trailing dashes trimmed",
			hostname:  "café",
			machineID: []byte("abc"),
			want:      "caf-ba7816",
		},
		{
			name:      "a label that sanitizes away entirely reads as unknown",
			hostname:  "!!!",
			machineID: []byte("abc"),
			want:      "unknown-ba7816",
		},
		{
			name:      "a hostname error still yields the machine's id",
			hostname:  "",
			machineID: []byte("abc"),
			want:      "unknown-ba7816",
		},
		{
			name:     "neither source available still yields a non-empty id",
			hostname: "",
			want:     "unknown-e3b0c4",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := hostIDFrom(tt.hostname, tt.machineID)
			if got != tt.want {
				t.Errorf("hostIDFrom(%q, %q) = %q, want %q", tt.hostname, tt.machineID, got, tt.want)
			}
			if got == "" {
				t.Error("hostIDFrom returned an empty id")
			}
			if bad := strings.TrimLeft(got, hostIDAlphabet); bad != "" {
				t.Errorf("hostIDFrom(%q, %q) = %q, contains %q outside [A-Za-z0-9_.-]", tt.hostname, tt.machineID, got, bad)
			}
		})
	}
}

// The interlock only works if the same machine keeps its id and a different machine
// gets a different one — the whole point of decision 4 (host-scoped acknowledgement).
func TestHostIDFromDeterministicAndDiscriminating(t *testing.T) {
	t.Parallel()

	const hostname = "devbox"
	first := hostIDFrom(hostname, []byte("1111111111111111"))
	if second := hostIDFrom(hostname, []byte("1111111111111111")); second != first {
		t.Errorf("hostIDFrom is not deterministic: %q then %q", first, second)
	}
	if other := hostIDFrom(hostname, []byte("2222222222222222")); other == first {
		t.Errorf("a different machine id produced the same host id %q", other)
	}
	if renamed := hostIDFrom("laptop", []byte("1111111111111111")); renamed == first {
		t.Errorf("a different hostname produced the same host id %q", renamed)
	}
}

// The chain is first-available: /etc/machine-id, then /var/lib/dbus/machine-id, then
// nothing (leaving hostIDFrom to hash the hostname). A missing or blank file is a link
// in that chain, never an error.
func TestReadMachineIDFallbackChain(t *testing.T) {
	t.Parallel()

	write := func(t *testing.T, dir, name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
		return path
	}

	t.Run("the first readable file wins", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		primary := write(t, dir, "machine-id", "first\n")
		secondary := write(t, dir, "dbus-machine-id", "second\n")

		if got := string(readMachineID([]string{primary, secondary})); got != "first" {
			t.Errorf("readMachineID = %q, want %q", got, "first")
		}
	})

	t.Run("a missing file falls through to the next", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		secondary := write(t, dir, "dbus-machine-id", "second\n")

		if got := string(readMachineID([]string{filepath.Join(dir, "absent"), secondary})); got != "second" {
			t.Errorf("readMachineID = %q, want %q", got, "second")
		}
	})

	t.Run("a blank file falls through to the next", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		primary := write(t, dir, "machine-id", "  \n\t")
		secondary := write(t, dir, "dbus-machine-id", "second")

		if got := string(readMachineID([]string{primary, secondary})); got != "second" {
			t.Errorf("readMachineID = %q, want %q", got, "second")
		}
	})

	t.Run("surrounding whitespace is trimmed so the hash is stable", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		padded := write(t, dir, "machine-id", "\n  deadbeef \n")

		if got := string(readMachineID([]string{padded})); got != "deadbeef" {
			t.Errorf("readMachineID = %q, want %q", got, "deadbeef")
		}
	})

	t.Run("no source at all reports nothing rather than failing", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		if got := readMachineID([]string{filepath.Join(dir, "absent"), filepath.Join(dir, "also-absent")}); len(got) != 0 {
			t.Errorf("readMachineID = %q, want empty", got)
		}
		if got := readMachineID(nil); len(got) != 0 {
			t.Errorf("readMachineID(nil) = %q, want empty", got)
		}
	})
}

// The exported entry point must behave on whatever machine the suite runs on: a
// non-empty, YAML-safe, repeatable value — including inside a container with no
// machine-id file, which is the environment this whole feature exists for.
func TestHostIDNonEmptyStableAndYAMLSafe(t *testing.T) {
	t.Parallel()

	got := HostID()
	if got == "" {
		t.Fatal("HostID() is empty")
	}
	if bad := strings.TrimLeft(got, hostIDAlphabet); bad != "" {
		t.Errorf("HostID() = %q, contains %q outside [A-Za-z0-9_.-]", got, bad)
	}
	if strings.HasPrefix(got, "-") {
		t.Errorf("HostID() = %q, must not open with a dash", got)
	}
	if again := HostID(); again != got {
		t.Errorf("HostID() is not stable within a process: %q then %q", got, again)
	}
}

func TestSanitizeHostLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		hostname string
		want     string
	}{
		{"an already-safe label is untouched", "dev-box_1.local", "dev-box_1.local"},
		{"spaces and slashes become dashes", "a b/c", "a-b-c"},
		{"leading and trailing dashes are trimmed", "-host-", "host"},
		{"leading punctuation is trimmed with its dash", "!host", "host"},
		{"an all-punctuation label empties out", "!@#", ""},
		{"an empty hostname stays empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := sanitizeHostLabel(tt.hostname); got != tt.want {
				t.Errorf("sanitizeHostLabel(%q) = %q, want %q", tt.hostname, got, tt.want)
			}
		})
	}
}

// hostIDAlphabet is every rune a host id may contain — the cutset the assertions strip
// to prove nothing else survived sanitization.
const hostIDAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_.-"
