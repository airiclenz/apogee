package agent

// Construction-surface tests: what the host puts on domain.Config has to survive the translation
// into the tool assembly's own configuration, since a field that silently stops there is a feature
// the operator configured and never got.

import (
	"slices"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
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
