package update

import "testing"

func TestNewer(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		{name: "higher minor", current: "v0.24.11", latest: "v0.25.0", want: true},
		{name: "higher patch", current: "v0.24.11", latest: "v0.24.12", want: true},
		{name: "numeric not lexical minor", current: "v0.9.0", latest: "v0.10.0", want: true},
		{name: "numeric not lexical patch", current: "v0.24.9", latest: "v0.24.10", want: true},
		{name: "equal", current: "v0.25.0", latest: "v0.25.0", want: false},
		{name: "older", current: "v0.25.0", latest: "v0.24.11", want: false},
		{name: "build metadata on current ignored", current: "v0.24.11+436.g28b6f838e6e1.dirty", latest: "v0.25.0", want: true},
		{name: "build metadata equal release", current: "v0.25.0+g28b6f838e6e1", latest: "v0.25.0", want: false},
		{name: "retracted v1.2.0", current: "v0.24.11", latest: "v1.2.0", want: false},
		{name: "retracted lower bound v1.0.0", current: "v0.24.11", latest: "v1.0.0", want: false},
		{name: "retracted upper bound v1.8.0", current: "v0.24.11", latest: "v1.8.0", want: false},
		{name: "above the retracted series", current: "v0.24.11", latest: "v1.8.1", want: true},
		{name: "latest dev", current: "v0.24.11", latest: "dev", want: false},
		{name: "current dev", current: "dev", latest: "v0.25.0", want: false},
		{name: "latest empty", current: "v0.24.11", latest: "", want: false},
		{name: "missing v prefix", current: "v0.24.11", latest: "0.25.0", want: false},
		{name: "pre-release latest", current: "v0.24.11", latest: "v0.25.0-rc1", want: false},
		{name: "leading zero", current: "v0.24.11", latest: "v0.025.0", want: false},
		{name: "overflowing field", current: "v0.24.11", latest: "v0.99999999999999999999.0", want: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := Newer(testCase.current, testCase.latest)

			if got != testCase.want {
				t.Errorf("Newer(%q, %q) = %v, want %v", testCase.current, testCase.latest, got, testCase.want)
			}
		})
	}
}
