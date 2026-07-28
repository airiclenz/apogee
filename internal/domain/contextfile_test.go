package domain

import "testing"

// TestContextFilesReportOversize pins the one predicate over the report: the standing system
// content is over its share only when there IS a share and the content is strictly past it — an
// unknown window (a zero share) never trips, and content that exactly fills its share still fits.
func TestContextFilesReportOversize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		title    string
		standing int
		share    int
		want     bool
	}{
		{"over its share", 4200, 3900, true},
		{"exactly at its share", 3900, 3900, false},
		{"well inside its share", 100, 3900, false},
		{"an unknown window allocates no share", 4200, 0, false},
		{"nothing standing, no share", 0, 0, false},
	}

	for _, tc := range tests {
		t.Run(tc.title, func(t *testing.T) {
			t.Parallel()
			report := ContextFilesReport{StandingTokens: tc.standing, SystemShare: tc.share}

			if got := report.Oversize(); got != tc.want {
				t.Errorf("Oversize() = %v for %d tokens against a share of %d, want %v",
					got, tc.standing, tc.share, tc.want)
			}
		})
	}
}
