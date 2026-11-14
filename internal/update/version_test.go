package update

import (
	"slices"
	"testing"
)

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
		{name: "unburned v1.2.0", current: "v0.24.11", latest: "v1.2.0", want: true},
		{name: "unburned v1.0.0", current: "v0.24.11", latest: "v1.0.0", want: true},
		{name: "burned v1.8.0", current: "v0.24.11", latest: "v1.8.0", want: false},
		{name: "burned placeholder v1.8.1", current: "v0.24.11", latest: "v1.8.1", want: false},
		{name: "first stable v1.0.0 over v0.32.0", current: "v0.32.0", latest: "v1.0.0", want: true},
		{name: "unburned v1.2.0 over v0.32.0", current: "v0.32.0", latest: "v1.2.0", want: true},
		{name: "unburned v1.7.1 over v0.32.0", current: "v0.32.0", latest: "v1.7.1", want: true},
		{name: "unburned v1.8.2 over v0.32.0", current: "v0.32.0", latest: "v1.8.2", want: true},
		{name: "burned v1.1.0 over v0.32.0", current: "v0.32.0", latest: "v1.1.0", want: false},
		{name: "burned v1.7.0 over v0.32.0", current: "v0.32.0", latest: "v1.7.0", want: false},
		{name: "burned v1.8.0 over v0.32.0", current: "v0.32.0", latest: "v1.8.0", want: false},
		{name: "burned v1.8.1 over v0.32.0", current: "v0.32.0", latest: "v1.8.1", want: false},
		{name: "burned with build metadata", current: "v0.32.0", latest: "v1.1.0+g28b6f838e6e1", want: false},
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

func TestRetractedVersionsIsACopy(t *testing.T) {
	t.Parallel()
	want := []string{"v1.1.0", "v1.7.0", "v1.8.0", "v1.8.1"}

	first := RetractedVersions()
	first[0] = "v9.9.9"
	second := RetractedVersions()

	if !slices.Equal(second, want) {
		t.Errorf("RetractedVersions() after a caller changed its copy = %v, want %v", second, want)
	}
	if !IsRetracted("v1.1.0") || IsRetracted("v9.9.9") {
		t.Errorf("IsRetracted followed a caller's change to its copy of the list")
	}
}

func TestIsRetracted(t *testing.T) {
	t.Parallel()
	cases := []struct {
		tag  string
		want bool
	}{
		{tag: "v1.1.0", want: true},
		{tag: "v1.7.0", want: true},
		{tag: "v1.8.0", want: true},
		{tag: "v1.8.1", want: true},
		{tag: "v1.8.1+436.g28b6f838e6e1.dirty", want: true},
		{tag: "v1.0.0", want: false},
		{tag: "v1.2.0", want: false},
		{tag: "v1.7.1", want: false},
		{tag: "v1.8.2", want: false},
		{tag: "v0.35.3", want: false},
		{tag: "1.1.0", want: false},
		{tag: "v1.1.0-rc1", want: false},
		{tag: "v01.1.0", want: false},
		{tag: "", want: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.tag, func(t *testing.T) {
			t.Parallel()

			got := IsRetracted(testCase.tag)

			if got != testCase.want {
				t.Errorf("IsRetracted(%q) = %v, want %v", testCase.tag, got, testCase.want)
			}
		})
	}
}
