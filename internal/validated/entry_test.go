package validated

import (
	"strings"
	"testing"
)

func TestDecodeEntry(t *testing.T) {
	good := `{"version":1,"key":"m","set":["a","b"],"evidence":{"campaign":"c-1"}}`

	tests := []struct {
		name    string
		data    string
		wantErr string // substring; "" = success
	}{
		{"good", good, ""},
		{"malformed", `{"version":1,`, "malformed JSON"},
		{"missing version", `{"key":"m","set":["a"]}`, "missing version"},
		{"newer version", `{"version":2,"key":"m","set":["a"]}`, "newer than this apogee"},
		{"missing key", `{"version":1,"set":["a"]}`, "missing key"},
		{"empty set", `{"version":1,"key":"m","set":[]}`, "empty set"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := decodeEntry([]byte(tt.data))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("decodeEntry: %v", err)
				}
				if e.Key != "m" || len(e.Set) != 2 || e.Evidence.Campaign != "c-1" {
					t.Fatalf("decoded entry mismatch: %+v", e)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}
