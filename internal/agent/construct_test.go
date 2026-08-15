package agent

// Construction-surface tests: what the host puts on domain.Config has to survive the translation
// into the tool assembly's own configuration, since a field that silently stops there is a feature
// the operator configured and never got.

import (
	"context"
	"errors"
	"net"
	"slices"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

// TestHostToolsCarriesSecretEnvVars covers the credential half of that translation: the variables a
// host resolved out of its configured `api-key-env:` entries (two servers naming distinct ones) have
// to reach HostTools, because that is the only route by which the execution tools learn to drop them
// from a subprocess environment.
func TestHostToolsCarriesSecretEnvVars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  domain.Config
		want []string
	}{
		{
			name: "two configured key variables both reach the tools",
			cfg:  domain.Config{SecretEnvVars: []string{"FIRST_KEY", "SECOND_KEY"}},
			want: []string{"FIRST_KEY", "SECOND_KEY"},
		},
		{
			name: "a config naming none leaves the scrub at apogee's own",
			cfg:  domain.Config{},
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := hostTools(tc.cfg).SecretEnvVars; !slices.Equal(got, tc.want) {
				t.Errorf("hostTools().SecretEnvVars = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestHostToolsBuildsTheURLGuardFromTheConfiguredHosts covers the network half of that
// translation. Until this key existed hostTools handed the tools a zero URLGuard, so the whole
// question was whether the SSRF floor was on; now the operator's own `url-safety:` hosts ride
// Config, and they reach the network tools through this one field or not at all. The deny is
// spelled the way a human writes one into config.yaml (mixed case, a trailing root dot) because
// the entry has to be normalised on the way in — an un-normalised list assembles a guard that
// looks configured and matches nothing.
func TestHostToolsBuildsTheURLGuardFromTheConfiguredHosts(t *testing.T) {
	t.Parallel()

	// Every name resolves to a public address, so the string-level allow/deny decisions under
	// test are reached without touching DNS.
	publicResolver := func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}

	t.Run("a configured deny reaches the guard", func(t *testing.T) {
		t.Parallel()

		guard := hostTools(domain.Config{URLDenyHosts: []string{"Blocked.EXAMPLE."}}).
			URLGuard.WithResolver(publicResolver)

		if err := guard.Check("https://blocked.example/x"); !errors.Is(err, security.ErrURLBlocked) {
			t.Errorf("the configured deny never reached the tools' guard: %v", err)
		}
		if err := guard.Check("https://elsewhere.example/x"); err != nil {
			t.Errorf("the deny entry blocked a host it does not name: %v", err)
		}
	})

	t.Run("a configured allow list reaches the guard", func(t *testing.T) {
		t.Parallel()

		guard := hostTools(domain.Config{URLAllowHosts: []string{"docs.example.com"}}).
			URLGuard.WithResolver(publicResolver)

		if err := guard.Check("https://docs.example.com/x"); err != nil {
			t.Errorf("the allowed host was refused: %v", err)
		}
		if err := guard.Check("https://elsewhere.example/x"); !errors.Is(err, security.ErrURLBlocked) {
			t.Errorf("a host outside the configured allow list was permitted: %v", err)
		}
	})

	t.Run("a config naming no hosts leaves the reach as it was", func(t *testing.T) {
		t.Parallel()

		guard := hostTools(domain.Config{}).URLGuard
		if guard.AllowHosts != nil || guard.DenyHosts != nil {
			t.Errorf("an unconfigured Config produced host lists: allow=%q deny=%q", guard.AllowHosts, guard.DenyHosts)
		}
	})
}
