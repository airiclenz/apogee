package agent

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// The Floor-guard table is the one place the seven guards are declared, so what it has to hold is
// the wiring the old hand-written ladder carried implicitly: each row's gate reads ITS OWN
// Disable… field and no other's. Flipping each field of the FloorConfig in turn must take exactly
// one guard out of the ladder, and the seven flips together must take out the seven distinct
// guards — a row copied with the wrong gate would show as one field removing two guards and
// another removing none.
func TestFloorGuardTableGatesEachGuardByItsOwnField(t *testing.T) {
	a, _ := ladderAgent(t, nil, nil)
	full := builtinKeys(a.buildBuiltins(domain.FloorConfig{}, false, false))
	if !slices.Equal(full, guardIDs) {
		t.Fatalf("the full ladder = %v, want guardIDs %v", full, guardIDs)
	}

	fields := reflect.TypeFor[domain.FloorConfig]()
	if fields.NumField() != len(floorGuards) {
		t.Fatalf("FloorConfig has %d fields, the table %d rows; want one gate per field", fields.NumField(), len(floorGuards))
	}
	removed := make([]string, 0, fields.NumField())
	for i := 0; i < fields.NumField(); i++ {
		var gates domain.FloorConfig
		reflect.ValueOf(&gates).Elem().Field(i).SetBool(true)
		keys := builtinKeys(a.buildBuiltins(gates, false, false))
		missing := make([]string, 0, 1)
		for _, key := range guardIDs {
			if !slices.Contains(keys, key) {
				missing = append(missing, key)
			}
		}
		if len(missing) != 1 {
			t.Errorf("%s: ladder = %v, want exactly one guard gone, got %v", fields.Field(i).Name, keys, missing)
			continue
		}
		removed = append(removed, missing[0])
	}
	slices.Sort(removed)
	want := slices.Clone(guardIDs)
	slices.Sort(want)
	if !slices.Equal(removed, want) {
		t.Errorf("the seven fields removed %v, want each guard exactly once %v", removed, want)
	}
}

// FloorGuardKeys is the table's key column in firing order — the list the facade hands the
// composition root — and a fresh copy each call, so a caller that sorts it cannot reorder the
// ladder.
func TestFloorGuardKeysIsAFreshCopyOfTheTable(t *testing.T) {
	keys := FloorGuardKeys()
	if !slices.Equal(keys, guardIDs) {
		t.Fatalf("FloorGuardKeys() = %v, want guardIDs %v", keys, guardIDs)
	}
	slices.Reverse(keys)
	if slices.Equal(FloorGuardKeys(), keys) {
		t.Error("reversing the returned slice reversed the table; want a copy")
	}
}

// retryGuard is the one adapter behind the four retry guards: a policy that fired asks for the
// Turn to be re-streamed with its correction, and one that did not books nothing.
func TestRetryGuardAsksForARetryOnlyWhenThePolicyFires(t *testing.T) {
	handler, ok := retryGuard(func(*domain.Response) (string, bool) { return "fix it", true }).(domain.PostResponseFunc)
	if !ok {
		t.Fatal("retryGuard did not build a post-response handler")
	}
	out, err := handler(context.Background(), &domain.Response{})
	if err != nil || !out.Retry || out.Inject != "fix it" {
		t.Errorf("fired policy -> %+v, %v; want Retry with the correction injected", out, err)
	}

	quiet := retryGuard(func(*domain.Response) (string, bool) { return "unused", false }).(domain.PostResponseFunc)
	out, err = quiet(context.Background(), &domain.Response{})
	if err != nil || out != (domain.Outcome{}) {
		t.Errorf("quiet policy -> %+v, %v; want the zero Outcome", out, err)
	}
}

// builtinKeys lists the ids of a built ladder, in order.
func builtinKeys(ladder []armedReaction) []string {
	keys := make([]string, 0, len(ladder))
	for _, r := range ladder {
		keys = append(keys, r.spec.ID)
	}
	return keys
}
