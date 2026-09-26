package security_test

import (
	"encoding/json"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/tools"
)

// This file is an external test package so the payload cases run against the REAL built-in
// tools and their own domain.ArgRolePayload declarations: internal/tools imports this
// package, so an in-package test could only stand in a stub for them.

// payloadCall builds a tool call from arbitrary named arguments.
func payloadCall(tool string, args map[string]any) domain.ToolCall {
	raw, _ := json.Marshal(args)
	return domain.ToolCall{ID: "c1", Tool: tool, Arguments: raw}
}

func TestDangerousActionGuard_PayloadTextNotInspected(t *testing.T) {
	t.Parallel()
	g := security.DefaultDangerousActionGuard()

	// A payload is not an action: text a tool merely writes, transmits or searches for
	// must never fire a rule, however dangerous the literal it quotes. Documenting the
	// guard's own ruleset — this repo's ADR 0012 and CONTEXT.md both name ~/.ssh — is the
	// case that first hit it, and a hard-refuse has no per-call override to escape with.
	// Each exclusion is earned by the calling tool's own declaration, so every case passes
	// the real tool, never nil.
	cases := []struct {
		name string
		call domain.ToolCall
		tool domain.Tool
	}{
		{"write a doc quoting ~/.ssh", payloadCall("write_file", map[string]any{
			"path":    "docs/adr/0012-confinement.md",
			"content": "Tier 1 hard-refuses writes under `~/.ssh` and to `~/.bashrc`.",
		}), &tools.WriteFile{}},
		{"write a doc quoting rm -rf /etc", payloadCall("write_file", map[string]any{
			"path":    "CHANGELOG.md",
			"content": "The guard refuses `rm -rf /etc` outright.",
		}), &tools.WriteFile{}},
		{"grep for the ~/.ssh literal", payloadCall("grep", map[string]any{
			"pattern": `~/\.ssh`,
			"path":    "docs",
		}), &tools.Grep{}},
		{"commit message naming ~/.ssh", payloadCall("git_commit", map[string]any{
			"message": "fix(security): stop refusing writes that mention ~/.ssh",
		}), &tools.GitCommit{}},
		{"find_replace payload naming .bashrc", payloadCall("single_find_and_replace", map[string]any{
			"path":    "internal/security/rules.go",
			"oldText": "~/.bashrc",
			"newText": "~/.zshrc",
		}), &tools.SingleFindReplace{}},
		{"nested replacements payload", payloadCall("multi_find_and_replace", map[string]any{
			"path": "docs/security.md",
			"replacements": []any{
				map[string]any{"oldText": "old", "newText": "writes under ~/.ssh are refused"},
			},
		}), &tools.MultiFindReplace{}},
		{"web search about ~/.ssh", payloadCall("web_search", map[string]any{
			"query": "how to configure ~/.ssh/config on macOS",
		}), &tools.WebSearch{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			d := g.Inspect(tc.call, tc.tool, nil)

			if d.Triggered() {
				t.Fatalf("Inspect(%q) wrongly triggered on payload text: tier=%d rule=%q reason=%q",
					tc.name, d.Tier, d.RuleID, d.Reason)
			}
		})
	}
}

// TestDangerousActionGuard_PayloadExemptionNeedsTheDeclaringTool is the control for the
// nested-replacements case above: the identical call, judged without the tool that declares
// `replacements` its payload, is refused — the exemption is the tool's, not the key name's.
func TestDangerousActionGuard_PayloadExemptionNeedsTheDeclaringTool(t *testing.T) {
	t.Parallel()
	g := security.DefaultDangerousActionGuard()
	call := payloadCall("multi_find_and_replace", map[string]any{
		"path": "docs/security.md",
		"replacements": []any{
			map[string]any{"oldText": "old", "newText": "writes under ~/.ssh are refused"},
		},
	})

	d := g.Inspect(call, nil, nil)

	if d.Tier != security.TierHardRefuse {
		t.Fatalf("undeclared nested payload tier = %d (rule %q), want TierHardRefuse", d.Tier, d.RuleID)
	}
}
