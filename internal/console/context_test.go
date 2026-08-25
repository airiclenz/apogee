package console

import (
	"context"
	"testing"
)

// TestFromContextReturnsTheRegistryDispatchInstalled is the seam's whole job: what the engine put
// on the context is the very registry — same pointer, not a copy — the tool call finds there.
func TestFromContextReturnsTheRegistryDispatchInstalled(t *testing.T) {
	t.Parallel()

	registry := New()

	found := FromContext(WithRegistry(context.Background(), registry))

	if found != registry {
		t.Fatalf("FromContext() = %p, want the installed registry %p", found, registry)
	}
}

// TestFromContextIsNilOnAContextThatNeverPassedThroughDispatch pins the absent case: a tool
// invoked outside an engine finds no registry rather than an empty one it could open Consoles in.
func TestFromContextIsNilOnAContextThatNeverPassedThroughDispatch(t *testing.T) {
	t.Parallel()

	if found := FromContext(context.Background()); found != nil {
		t.Fatalf("FromContext() on a bare context = %p, want nil", found)
	}
}

// TestWithRegistryInstallsNothingForANilRegistry pins that "no registry" is encoded as an
// untouched context, so nothing downstream can find a typed nil sitting under the key.
func TestWithRegistryInstallsNothingForANilRegistry(t *testing.T) {
	t.Parallel()

	bare := context.Background()

	carrying := WithRegistry(bare, nil)

	if carrying != bare {
		t.Fatal("WithRegistry(ctx, nil) returned a different context")
	}
	if found := FromContext(carrying); found != nil {
		t.Fatalf("FromContext() after installing nil = %p, want nil", found)
	}
}
