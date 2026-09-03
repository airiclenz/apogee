package tasklist

import (
	"context"
	"testing"
)

// TestFromContextReturnsTheListDispatchInstalled is the seam's whole job: what the engine
// put on the context is the very list — same pointer, not a copy — the tool call finds
// there, so a replace lands on the state the standing block renders.
func TestFromContextReturnsTheListDispatchInstalled(t *testing.T) {
	t.Parallel()

	list := New()

	found := FromContext(WithList(context.Background(), list))

	if found != list {
		t.Fatalf("FromContext() = %p, want the installed list %p", found, list)
	}
}

// TestFromContextIsNilOnAContextThatNeverPassedThroughDispatch pins the absent case: a
// tool invoked outside an engine finds no list rather than an empty one it could write a
// checklist into that nobody would ever render.
func TestFromContextIsNilOnAContextThatNeverPassedThroughDispatch(t *testing.T) {
	t.Parallel()

	if found := FromContext(context.Background()); found != nil {
		t.Fatalf("FromContext() on a bare context = %p, want nil", found)
	}
}

// TestWithListInstallsNothingForANilList pins that "no list" is encoded as an untouched
// context, so nothing downstream can find a typed nil sitting under the key.
func TestWithListInstallsNothingForANilList(t *testing.T) {
	t.Parallel()

	bare := context.Background()

	carrying := WithList(bare, nil)

	if carrying != bare {
		t.Fatal("WithList(ctx, nil) returned a different context")
	}
	if found := FromContext(carrying); found != nil {
		t.Fatalf("FromContext() after installing nil = %p, want nil", found)
	}
}
