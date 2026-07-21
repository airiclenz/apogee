package present

import (
	"net"
	"strings"
	"testing"
)

// envFrom builds an injected environment lookup over a map, so a table row states exactly
// the variables it is about and nothing else — the same shape the production callers pass
// (os.Getenv), with the machine taken out of the answer.
func envFrom(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

// Remote is decided by ANY of the three OpenSSH variables, because different sshd builds and
// session types set different ones; Local is the absence of all three.
func TestLocality(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		vars map[string]string
		want Kind
	}{
		{
			name: "an empty environment is a local session",
			want: Local,
		},
		{
			name: "the devbox session: SSH_CONNECTION set",
			vars: map[string]string{"SSH_CONNECTION": "192.168.64.1 50072 192.168.64.2 22"},
			want: Remote,
		},
		{
			name: "SSH_TTY alone is enough",
			vars: map[string]string{"SSH_TTY": "/dev/pts/0"},
			want: Remote,
		},
		{
			name: "SSH_CLIENT alone is enough (older sshd)",
			vars: map[string]string{"SSH_CLIENT": "192.168.64.1 50072 22"},
			want: Remote,
		},
		{
			name: "all three together are still just remote",
			vars: map[string]string{
				"SSH_CONNECTION": "192.168.64.1 50072 192.168.64.2 22",
				"SSH_TTY":        "/dev/pts/0",
				"SSH_CLIENT":     "192.168.64.1 50072 22",
			},
			want: Remote,
		},
		{
			name: "an explicitly empty variable is not a remote session",
			vars: map[string]string{"SSH_CONNECTION": "", "SSH_TTY": "", "SSH_CLIENT": ""},
			want: Local,
		},
		{
			name: "a whitespace-only variable reads as unset",
			vars: map[string]string{"SSH_CONNECTION": "   "},
			want: Local,
		},
		{
			name: "an unrelated SSH variable does not make it remote",
			vars: map[string]string{"SSH_AUTH_SOCK": "/tmp/ssh-agent.sock"},
			want: Local,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := Locality(envFrom(tt.vars)); got != tt.want {
				t.Errorf("Locality() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The desktop question is per-OS: the GUI OSes always have one, Linux has one only when a
// display server says so, and an unknown OS answers false rather than guessing.
func TestHasDesktop(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		goos string
		vars map[string]string
		want bool
	}{
		{
			name: "darwin always has a desktop",
			goos: "darwin",
			want: true,
		},
		{
			name: "windows always has a desktop",
			goos: "windows",
			want: true,
		},
		{
			name: "linux with X11 has a desktop",
			goos: "linux",
			vars: map[string]string{"DISPLAY": ":0"},
			want: true,
		},
		{
			name: "linux with Wayland has a desktop",
			goos: "linux",
			vars: map[string]string{"WAYLAND_DISPLAY": "wayland-0"},
			want: true,
		},
		{
			name: "headless linux (the devbox) has none",
			goos: "linux",
			vars: map[string]string{"TERM": "xterm-256color"},
			want: false,
		},
		{
			name: "linux with blank display variables is still headless",
			goos: "linux",
			vars: map[string]string{"DISPLAY": "", "WAYLAND_DISPLAY": "  "},
			want: false,
		},
		{
			name: "an unknown OS answers false even with a display set",
			goos: "freebsd",
			vars: map[string]string{"DISPLAY": ":0"},
			want: false,
		},
		{
			name: "an empty goos answers false",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := HasDesktop(tt.goos, envFrom(tt.vars)); got != tt.want {
				t.Errorf("HasDesktop(%q) = %v, want %v", tt.goos, got, tt.want)
			}
		})
	}
}

// The advertised host is a precedence chain, and the order is load-bearing (ADR 0019): the
// live SSH server address beats the configured one, which beats a probe, which beats
// loopback. The probe is injected so the chain is pinned regardless of the test machine's
// routing table.
func TestAdvertiseHostPrecedence(t *testing.T) {
	t.Parallel()

	const probed = "10.0.0.5"

	tests := []struct {
		name     string
		vars     map[string]string
		override string
		dial     string
		want     string
	}{
		{
			name: "the devbox case: the third SSH_CONNECTION field is the server IP",
			vars: map[string]string{"SSH_CONNECTION": "192.168.64.1 50072 192.168.64.2 22"},
			dial: probed,
			want: "192.168.64.2",
		},
		{
			name:     "SSH_CONNECTION outranks the configured host (settled order)",
			vars:     map[string]string{"SSH_CONNECTION": "192.168.64.1 50072 192.168.64.2 22"},
			override: "apogee.example",
			dial:     probed,
			want:     "192.168.64.2",
		},
		{
			name: "an IPv6 server address is bracketed for URL use",
			vars: map[string]string{"SSH_CONNECTION": "2001:db8::1 50072 2001:db8::2 22"},
			want: "[2001:db8::2]",
		},
		{
			name:     "no SSH session: the configured host is used",
			override: "apogee.example",
			dial:     probed,
			want:     "apogee.example",
		},
		{
			name:     "a bare IPv6 override is bracketed too",
			override: "2001:db8::5",
			want:     "[2001:db8::5]",
		},
		{
			name:     "an already-bracketed override is left alone",
			override: "[2001:db8::5]",
			want:     "[2001:db8::5]",
		},
		{
			name:     "a padded override is trimmed",
			override: "  apogee.example  ",
			want:     "apogee.example",
		},
		{
			name:     "a whitespace-only override falls through to the probe",
			override: "   ",
			dial:     probed,
			want:     probed,
		},
		{
			name:     "a truncated SSH_CONNECTION falls through to the override",
			vars:     map[string]string{"SSH_CONNECTION": "192.168.64.1 50072"},
			override: "apogee.example",
			dial:     probed,
			want:     "apogee.example",
		},
		{
			name: "a non-IP third field falls through rather than advertising garbage",
			vars: map[string]string{"SSH_CONNECTION": "a b c d"},
			dial: probed,
			want: probed,
		},
		{
			name: "a zoned link-local server address falls through",
			vars: map[string]string{"SSH_CONNECTION": "fe80::1 50072 fe80::2%eth0 22"},
			dial: probed,
			want: probed,
		},
		{
			name: "nothing configured: the outbound-dial probe answers",
			dial: probed,
			want: probed,
		},
		{
			name: "an IPv6 probe result is bracketed",
			dial: "2001:db8::7",
			want: "[2001:db8::7]",
		},
		{
			name: "an empty environment with no probe answer ends at loopback",
			want: loopbackHost,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dial := func() string { return tt.dial }
			if got := advertiseHost(envFrom(tt.vars), tt.override, dial); got != tt.want {
				t.Errorf("advertiseHost(%v, %q) = %q, want %q", tt.vars, tt.override, got, tt.want)
			}
		})
	}
}

// The exported entry point wires the real probe in. Its SSH answer is machine-independent
// and pinned; its probe answer is not, so that case only asserts the shape a URL needs.
func TestAdvertiseHostUsesTheRealProbe(t *testing.T) {
	t.Parallel()

	ssh := envFrom(map[string]string{"SSH_CONNECTION": "192.168.64.1 50072 192.168.64.2 22"})
	if got := AdvertiseHost(ssh, ""); got != "192.168.64.2" {
		t.Errorf("AdvertiseHost(ssh, \"\") = %q, want %q", got, "192.168.64.2")
	}

	// Whatever this machine's routing table says — a real address, or loopback when the probe
	// finds no route — the result must be a usable URL authority: non-empty, and parseable as
	// a host once any IPv6 brackets come off.
	got := AdvertiseHost(envFrom(nil), "")
	if got == "" {
		t.Fatal("AdvertiseHost(empty env, no override) = \"\", want a usable host")
	}
	if bare := strings.Trim(got, "[]"); strings.Contains(got, "[") && net.ParseIP(bare) == nil {
		t.Errorf("AdvertiseHost bracketed a non-IPv6 value: %q", got)
	}
	if _, _, err := net.SplitHostPort(got + ":8080"); err != nil {
		t.Errorf("AdvertiseHost() = %q, which does not compose a URL authority: %v", got, err)
	}
}

// Every detector takes its environment injected, so a caller that has none to give (a
// non-interactive embedder) must get the conservative answer rather than a panic.
func TestDetectorsTolerateANilEnv(t *testing.T) {
	t.Parallel()

	if got := Locality(nil); got != Local {
		t.Errorf("Locality(nil) = %q, want %q", got, Local)
	}
	if HasDesktop("linux", nil) {
		t.Error("HasDesktop(\"linux\", nil) = true, want false")
	}
	if !HasDesktop("darwin", nil) {
		t.Error("HasDesktop(\"darwin\", nil) = false, want true")
	}
	if got := advertiseHost(nil, "", func() string { return "" }); got != loopbackHost {
		t.Errorf("advertiseHost(nil, \"\") = %q, want %q", got, loopbackHost)
	}
}

// The probe is a route lookup, not a transmission: it must answer promptly with either an
// address or nothing, on a box with a network and on one without.
func TestOutboundIPAnswersWithAnAddressOrNothing(t *testing.T) {
	t.Parallel()

	got := outboundIP()
	if got == "" {
		return // no route on this box — a legitimate answer the chain handles
	}
	ip := net.ParseIP(got)
	if ip == nil {
		t.Fatalf("outboundIP() = %q, which is not an IP address", got)
	}
	if ip.IsUnspecified() {
		t.Errorf("outboundIP() = %q, want a specific local address", got)
	}
}
