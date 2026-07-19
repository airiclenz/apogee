package validated

import (
	"errors"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

func TestMatch(t *testing.T) {
	entries := map[string]Entry{
		"gemma":  {Key: "gemma", Set: ids("a", "b")},
		"direct": {Key: "direct", Set: ids("a")},
	}

	tests := []struct {
		name       string
		label      string
		confidence domain.FingerprintConfidence
		alias      map[string]string
		wantKind   DecisionKind
		wantKey    string
		wantAlias  bool
	}{
		{name: "zero label matches nothing", label: "", confidence: domain.ConfidenceHigh, wantKind: KindNone},
		{name: "no entry no decision", label: "unknown-model", confidence: domain.ConfidenceHigh, wantKind: KindNone},
		{name: "direct match at low offers", label: "direct", confidence: domain.ConfidenceLow, wantKind: KindOffered, wantKey: "direct"},
		{name: "direct match at medium applies", label: "direct", confidence: domain.ConfidenceMedium, wantKind: KindApplied, wantKey: "direct"},
		{name: "direct match at high applies", label: "direct", confidence: domain.ConfidenceHigh, wantKind: KindApplied, wantKey: "direct"},
		{
			name: "identity alias applies at low", label: "gemma", confidence: domain.ConfidenceLow,
			alias: map[string]string{"gemma": "gemma"}, wantKind: KindApplied, wantKey: "gemma", wantAlias: true,
		},
		{
			name: "transfer alias applies from a hash label", label: "sha256:abc", confidence: domain.ConfidenceHigh,
			alias: map[string]string{"sha256:abc": "gemma"}, wantKind: KindApplied, wantKey: "gemma", wantAlias: true,
		},
		{
			name: "alias wins over a direct entry for the same label", label: "direct", confidence: domain.ConfidenceHigh,
			alias: map[string]string{"direct": "gemma"}, wantKind: KindApplied, wantKey: "gemma", wantAlias: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Match(tt.label, tt.confidence, tt.alias, entries)
			if err != nil {
				t.Fatalf("Match: %v", err)
			}
			if got.Kind != tt.wantKind {
				t.Fatalf("kind: want %v, got %v", tt.wantKind, got.Kind)
			}
			if tt.wantKey != "" && got.Entry.Key != tt.wantKey {
				t.Fatalf("key: want %q, got %q", tt.wantKey, got.Entry.Key)
			}
			if got.ViaAlias != tt.wantAlias {
				t.Fatalf("ViaAlias: want %v, got %v", tt.wantAlias, got.ViaAlias)
			}
			if tt.wantAlias && got.AliasFrom != tt.label {
				t.Fatalf("AliasFrom: want %q, got %q", tt.label, got.AliasFrom)
			}
		})
	}
}

func TestMatch_DanglingAliasIsLoud(t *testing.T) {
	entries := map[string]Entry{"gemma": {Key: "gemma"}}
	_, err := Match("my-model", domain.ConfidenceLow, map[string]string{"my-model": "nope"}, entries)

	var dangling *DanglingAliasError
	if !errors.As(err, &dangling) {
		t.Fatalf("want DanglingAliasError, got %v", err)
	}
	if dangling.Label != "my-model" || dangling.Target != "nope" {
		t.Fatalf("error fields: %+v", dangling)
	}
	if !strings.Contains(err.Error(), "gemma") {
		t.Fatalf("error should list known keys, got %q", err.Error())
	}
}
