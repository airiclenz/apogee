package mechanisms

import (
	"strings"
	"testing"
)

// TestPromptAssetsKeepTheirMarkers pins the one coupling the move of the directives into
// prompts/*.txt could otherwise break silently: each idempotency marker stayed a Go const while its
// directive became an embedded asset, so the "the marker is inside the text" invariant now spans two
// files. AppendToSystem suppresses a repeat inject by looking for the marker in the system prompt it
// already built, so a marker that no longer occurs in its directive makes that check miss and the
// directive inject twice.
func TestPromptAssetsKeepTheirMarkers(t *testing.T) {
	cases := []struct {
		name      string
		marker    string
		directive string
	}{
		{"library_injection", libraryInjectionMarker, libraryInjectionHeader},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tc.directive, tc.marker) {
				t.Errorf("idempotency marker %q is no longer inside its prompt asset %q: a re-worded asset that drops its marker makes AppendToSystem's no-op check miss, so the directive injects twice on the same request",
					tc.marker, tc.directive)
			}
		})
	}
}

// TestEmbeddedDirectivePromptsLoad pins the loader contract behind this package's prompt assets:
// every file under prompts/ carries text, mustPrompt returns it with the single trailing newline
// the file ends in already stripped, and each asset holds exactly the number of %s verbs the
// builder feeds it — a verb added to or dropped from an asset would otherwise surface only as a
// %!s(MISSING) or an %!(EXTRA ...) inside a live directive. The table doubles as the roster: an
// asset embedded but not named here fails, so a new fragment cannot slip in unpinned.
func TestEmbeddedDirectivePromptsLoad(t *testing.T) {
	t.Parallel()

	verbs := map[string]int{
		// The whole-text directives and notes the Mechanisms inject verbatim.
		"library-tool-use-note.txt":    0,
		"library-shallow-note.txt":     0,
		"library-injection-header.txt": 0,
	}

	entries, err := promptFS.ReadDir("prompts")
	if err != nil {
		t.Fatalf("read the embedded prompts directory: %v", err)
	}
	if len(entries) != len(verbs) {
		t.Errorf("%d prompt assets are embedded, want the %d this test pins", len(entries), len(verbs))
	}
	for _, e := range entries {
		want, pinned := verbs[e.Name()]
		if !pinned {
			t.Errorf("prompt asset %s is embedded but not pinned by this test", e.Name())
			continue
		}
		got := mustPrompt(e.Name())
		if strings.TrimSpace(got) == "" {
			t.Errorf("prompt asset %s loads as empty text", e.Name())
		}
		if strings.HasSuffix(got, "\n") {
			t.Errorf("prompt asset %s still ends in a newline after load: %q", e.Name(), got)
		}
		if n := strings.Count(got, "%s"); n != want {
			t.Errorf("prompt asset %s holds %d %%s verbs, want %d", e.Name(), n, want)
		}
	}
}
