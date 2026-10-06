package security

import "testing"

// TestWriteTargetsOf pins the verb-aware reading of a command line: what a write-shaped rule
// on the shell write view gets to see. The first five rows are the item's own; the rest pin
// the seams the reading turns on — pipelines and chains, redirect forms, quoting, wrappers,
// the conditional leaders (find / sed / perl / git / dd), substitutions and heredocs.
func TestWriteTargetsOf(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, command, want string
	}{
		// --- the item's rows -------------------------------------------------------
		{"a listing reads", "ls -la .git/hooks", ""},
		{"a redirect writes its target, not what cat read", "cat .git/config > out", "out"},
		{"rm's operands, options dropped", "rm -rf .git/hooks", ".git/hooks"},
		{"git config is a mutating verb: its operands feed", "git config -f .git/config user.name x", ".git/config user.name x"},
		{"an unknown leader fails closed", "frobnicate .git/config", ".git/config"},

		// --- read leaders -----------------------------------------------------------
		{"cmp reads both sides", "cmp .beads/hooks/commit-msg .git/hooks/commit-msg", ""},
		{"cat reads", "cat .git/config", ""},
		{"a pipeline of reads", "cat .git/config | grep url | head -1", ""},
		{"a chain of reads", "ls .git/hooks && stat .git/config; wc -l .git/config", ""},
		{"a read through an absolute program path", "/bin/cat .git/config", ""},
		{"find without a writing predicate reads", "find .git/hooks -name '*.sample'", ""},
		{"find -delete writes", "find .git/hooks -name '*.sample' -delete", ".git/hooks *.sample"},
		{"find -exec writes", "find .git/hooks -exec rm {} ;", ".git/hooks rm {}"},
		{"sed without -i reads", "sed -n 1p .git/config", ""},
		{"sed -i writes", "sed -i 's/a/b/' .git/config", "s/a/b/ .git/config"},
		{"sed --in-place writes", "sed --in-place=.bak 's/a/b/' .git/config", ".bak s/a/b/ .git/config"},
		{"perl -pi writes", "perl -pi -e 's/a/b/' .git/config", "s/a/b/ .git/config"},
		{"perl without -i fails closed", "perl script.pl .git/config", "script.pl .git/config"},

		// --- redirect forms -----------------------------------------------------------
		{"an append redirect", "echo x >> .git/config", ".git/config"},
		{"an attached redirect", "echo x>.git/config", ".git/config"},
		{"a clobbering redirect", "echo x >| .git/config", ".git/config"},
		{"a combined-stream redirect", "make &> .git/hooks/log", ".git/hooks/log"},
		{"an fd-numbered redirect", "cmd 2> .git/hooks/err", ".git/hooks/err"},
		{"an fd dup names no file", "cat .git/config 2>&1", ""},
		{"an input redirect reads", "wc -l < .git/config", ""},
		{"a here-string reads", "cat <<< .git/config", ""},
		{"tee writes its operands", "cat a | tee .git/hooks/pre-commit", ".git/hooks/pre-commit"},
		{"tee after a stderr pipe", "make |& tee -a .git/hooks/log", ".git/hooks/log"},

		// --- quoting and wrappers -------------------------------------------------------
		{"a quoted operand keeps its spaces and reads", `cat ".git/config"`, ""},
		{"a quoted redirect target", `echo x > ".git/hooks/pre commit"`, ".git/hooks/pre commit"},
		{"a single-quoted separator is text", `echo 'a; rm -rf .git/hooks'`, ""},
		{"an escaped separator is text", `echo a\; rm -rf .git/hooks`, ""},
		{"sudo is stripped", "sudo rm -rf .git/hooks", ".git/hooks"},
		{"env with an assignment is stripped", "env GIT_DIR=.git cat .git/config", ""},
		{"a leading assignment is not the leader", "GOCACHE=/tmp/x cat .git/config", ""},
		{"xargs hands its operands to the wrapped leader", "ls | xargs rm -f .git/hooks/x", ".git/hooks/x"},
		{"an option's value feeds", "frobnicate --output=.git/config", ".git/config"},
		{"a comment is not a command", "cat .git/config # rm -rf .git/hooks", ""},

		// --- git ----------------------------------------------------------------------------
		{"git status reads", "git status", ""},
		{"git log with a path reads", "git log -3 --oneline -- .git/config", ""},
		{"git -C then a read verb", "git -C .git/modules/x log", ""},
		{"git --git-dir then a write verb", "git --git-dir=.git/modules/x config a b", ".git/modules/x a b"},
		{"git remote fails closed", "git remote add origin x", "add origin x"},
		{"git branch fails closed", "git branch -D x", "x"},
		{"git config writing a command-valued key names .git/config", "git config --local filter.x.clean cmd", "filter.x.clean cmd .git/config"},
		{"git config setting core.hooksPath names .git/config (lowered before matching)", "git config core.hooksPath /tmp/h", "core.hooksPath /tmp/h .git/config"},
		{"git config --global writes the operator's file, not .git/config", "git config --global filter.x.clean cmd", "filter.x.clean cmd"},
		{"git config of a plain key names no file", "git config user.name x", "user.name x"},
		{"git config --get is a read of the key", "git config --get filter.x.clean", "filter.x.clean"},

		// --- dd ----------------------------------------------------------------------------
		{"dd writes only its of=", "dd if=.git/config of=/tmp/x bs=1", "/tmp/x"},

		// --- output-writing verbs and options ---------------------------------------------
		{"uniq writes its second operand", "uniq in .git/hooks/pre-commit", ".git/hooks/pre-commit"},
		{"uniq's -f value is not an operand", "uniq -f 1 in .git/hooks/pre-commit", ".git/hooks/pre-commit"},
		{"uniq's bundle ending in a value letter consumes the next word", "uniq -cs 2 in out", "out"},
		{"uniq's options stop at --", "uniq -- -in out", "out"},
		{"uniq with one operand writes nothing", "uniq in", ""},
		{"xxd -r writes its second operand", "xxd -r in .git/hooks/pre-commit", ".git/hooks/pre-commit"},
		{"xxd's -l value is not an operand", "xxd -l 64 in .git/hooks/pre-commit", ".git/hooks/pre-commit"},
		{"xxd with one operand writes nothing", "xxd -l 64 .git/config", ""},
		{"tree's -o value", "tree -o .git/hooks/pre-commit", ".git/hooks/pre-commit"},
		{"tree's operands are what it lists", "tree .git/hooks", ""},
		{"a bare -o writes only for tree", "ls -o .git/hooks", ""},
		{"sed's w command", "sed -n 'w .git/hooks/pre-commit' in", ".git/hooks/pre-commit"},
		{"sed's W command after a regex address", "sed -n '/x/W .git/hooks/pre-commit' in", ".git/hooks/pre-commit"},
		{"sed's s///w flag", "sed 's/a/b/w .git/hooks/pre-commit' in", ".git/hooks/pre-commit"},
		{"sed's s///gw flag in an -e script", "sed -e 's/a/b/gw .git/hooks/pre-commit' in", ".git/hooks/pre-commit"},
		{"sed's w in a later -e script, after a chained command", "sed -n -e p -e 'p;$w out' in", "out"},
		{"sed's w in a regex is not a command", "sed -n '/worktree/p' .git/config", ""},
		{"sed's r reads its file", "sed 'r .git/hooks/x' in", ""},
		{"sed's operands are read, not written", "sed -n 's/a/b/p' .git/config", ""},
		{"git diff --output= writes its value", "git diff --output=.git/hooks/pre-commit", ".git/hooks/pre-commit"},
		{"git log --output writes the next word", "git log --output .git/hooks/pre-commit", ".git/hooks/pre-commit"},
		{"a read leader's --output writes", "cat --output .git/hooks/pre-commit x", ".git/hooks/pre-commit"},
		{"the --output scan stops at --", "grep -rn -- --output .git/hooks", ""},
		{"grep's -e value is a pattern", "grep -e --output .git/config", ""},
		{"echo prints --output", "echo --output .git/config", ""},
		{"an unknown leader's --output word is emitted once", "frobnicate --output .git/config", ".git/config"},

		// --- substitutions and heredocs --------------------------------------------------
		{"a substitution's read contributes nothing", "cat $(find .git/hooks -name x)", ""},
		{"a substitution's write contributes", "echo $(rm -rf .git/hooks)", ".git/hooks"},
		{"a backtick substitution's write contributes", "echo `rm -rf .git/hooks`", ".git/hooks"},
		{"a heredoc body is payload, its redirect a write", "cat > .git/hooks/pre-commit <<'EOF'\nrm -rf /\nEOF\nls", ".git/hooks/pre-commit"},
		{"a heredoc body naming a path is not a command", "cat > notes.md <<EOF\nsee .git/config\nEOF", "notes.md"},
		{"a subshell's members are judged", "(cat x && rm -rf .git/hooks)", ".git/hooks"},
		{"cd fails closed: it moves every later operand", "cd .git/hooks && rm -rf pre-commit", ".git/hooks pre-commit"},
		{"export fails closed", "export GIT_DIR=.git/modules/x", "GIT_DIR=.git/modules/x"},
		{"a bare assignment feeds its value: a later operand resolves through it", "d=.git/hooks; rm -rf $d", ".git/hooks $d"},
		{"a brace group's members are judged", "{ cat a; rm -rf .git/hooks; }", ".git/hooks"},
		{"a parameter expansion stays in its word", "cat ${GIT_DIR}/config", ""},
		{"an unterminated quote still reads", "cat '.git/config", ""},
		{"an empty line", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := writeTargetsOf(tc.command)

			if got != tc.want {
				t.Errorf("writeTargetsOf(%q) = %q, want %q", tc.command, got, tc.want)
			}
		})
	}
}

// TestShellWriteViewRefusesOutputWrites pins what the shell write view hands the guard for the
// verbs and options that write a file without naming it as a mutating verb's operand: each
// spelling aimed at the git control plane is hard-refused, while the same leaders reading it,
// and the look-alike flags of other leaders, pass.
func TestShellWriteViewRefusesOutputWrites(t *testing.T) {
	t.Parallel()
	g := DefaultDangerousActionGuard()

	cases := []struct {
		command  string
		wantTier Tier
		wantRule string
	}{
		{"uniq in .git/hooks/pre-commit", TierHardRefuse, "write-git-control-plane"},
		{"xxd -r in .git/hooks/pre-commit", TierHardRefuse, "write-git-control-plane"},
		{"tree -o .git/hooks/pre-commit", TierHardRefuse, "write-git-control-plane"},
		{"sed -n 'w .git/hooks/pre-commit' in", TierHardRefuse, "write-git-control-plane"},
		{"sed -n '/x/W .git/hooks/pre-commit' in", TierHardRefuse, "write-git-control-plane"},
		{"sed 's/a/b/w .git/hooks/pre-commit' in", TierHardRefuse, "write-git-control-plane"},
		{"git diff --output=.git/hooks/pre-commit", TierHardRefuse, "write-git-control-plane"},
		{"git log --output .git/hooks/pre-commit", TierHardRefuse, "write-git-control-plane"},
		{"cat --output=.git/config x", TierHardRefuse, "write-git-control-plane"},
		{"frobnicate --output=.git/config", TierHardRefuse, "write-git-control-plane"},
		{"uniq in", TierNone, ""},
		{"tree", TierNone, ""},
		{"tree .git/hooks", TierNone, ""},
		{"xxd .git/config", TierNone, ""},
		{"uniq .git/config", TierNone, ""},
		{"xxd -l 64 .git/config", TierNone, ""},
		{"uniq -f 1 .git/config", TierNone, ""},
		{"ls -o .git/hooks", TierNone, ""},
		{"grep -o x .git/config", TierNone, ""},
		{"grep -rn -- --output .git/hooks", TierNone, ""},
		{"sed -n '/worktree/p' .git/config", TierNone, ""},
	}

	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			t.Parallel()

			d := g.Inspect(terminalCall(tc.command), shellTool, nil)

			if d.Tier != tc.wantTier || d.RuleID != tc.wantRule {
				t.Errorf("Inspect(%q) = tier %d rule %q, want tier %d rule %q",
					tc.command, d.Tier, d.RuleID, tc.wantTier, tc.wantRule)
			}
		})
	}
}
