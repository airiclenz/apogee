package mechanisms

import (
	"strings"
	"testing"
)

// The heuristic checker's contract is that it reports only unambiguous breakage: a false positive
// fires ActionRetry against correct code and costs a Turn, which is exactly the regression the
// Bypass floor forbids. This is the negative table — valid code in every language the checker
// claims to understand, with the constructs that used to trip it: `//` where it is floor division
// or a regex literal rather than a comment, brackets hidden in strings, docstrings and block
// comments, and quote characters that are not string delimiters.
func TestCheckSyntaxAcceptsValidCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		path    string
		content string
	}{
		{
			// The audit's primary case: `//` is floor division, so a comment gate abandoned the
			// line before its closing brackets and invented an unclosed paren/bracket.
			name: "python floor division in call, index and range positions",
			path: "stats.py",
			content: "def middle(xs):\n" +
				"    print(xs[len(xs) // 2])\n" +
				"    for i in range(len(xs) // 2):\n" +
				"        total = sum(xs) // len(xs)\n" +
				"    return total\n",
		},
		{
			name: "python docstring holding brackets and an apostrophe",
			path: "doc.py",
			content: "def f(x):\n" +
				"    \"\"\"Return x (halved).\n" +
				"\n" +
				"    It's fine to leave a stray [ in here.\n" +
				"    \"\"\"\n" +
				"    return x // 2\n",
		},
		{
			name:    "python comment holding an unmatched bracket",
			path:    "note.py",
			content: "# a note with a stray ( paren\nvalue = 1\n",
		},
		{
			// Ruby's `//` is an empty regex literal, not a comment.
			name:    "ruby empty regex literal inside a call",
			path:    "split.rb",
			content: "words = text.split(//)\nputs words.length\n",
		},
		{
			// The same gate on the truncation side: stripping `//` here leaves `SEP =`, which
			// reads as an incomplete final expression.
			name:    "ruby empty regex literal as the final expression",
			path:    "sep.rb",
			content: "SEP = //\n",
		},
		{
			name: "javascript string holding brackets and apostrophes",
			path: "app.js",
			content: "// it's a comment with an unmatched ( paren\n" +
				"const tpl = \"a [b] {c} (d) it's fine\";\n" +
				"const other = 'she said \"hi\" }';\n" +
				"function f(x) {\n" +
				"  return tpl.length + other.length + x;\n" +
				"}\n",
		},
		{
			name: "typescript generic with a line comment",
			path: "pick.ts",
			content: "export function pick<T>(xs: T[], n: number): T {\n" +
				"  // pick roughly the middle ( see docs\n" +
				"  return xs[Math.floor(n / 2)];\n" +
				"}\n",
		},
		{
			name: "rust lifetime annotations are not string literals",
			path: "holder.rs",
			content: "struct Holder<'a> {\n" +
				"    name: &'a str,\n" +
				"}\n" +
				"\n" +
				"impl<'a> Holder<'a> {\n" +
				"    // a comment with a stray ( paren\n" +
				"    fn name(&self) -> &'a str {\n" +
				"        self.name\n" +
				"    }\n" +
				"}\n",
		},
		{
			// The block-comment case: a commented-out brace used to reach the bracket stack.
			name: "c block comment holding unmatched brackets",
			path: "main.c",
			content: "int main(void) {\n" +
				"    /* an unmatched { brace and ( paren live in here\n" +
				"       and here: ] */\n" +
				"    return 0;\n" +
				"}\n",
		},
		{
			name: "cpp preprocessor directive is not a comment",
			path: "half.cpp",
			content: "#include <vector>\n" +
				"\n" +
				"int half(int n) {\n" +
				"    // half of ( n\n" +
				"    return n / 2;\n" +
				"}\n",
		},
		{
			name: "java line comment",
			path: "Util.java",
			content: "class Util {\n" +
				"    // returns half, rounding down ( always\n" +
				"    static int half(int n) {\n" +
				"        return n / 2;\n" +
				"    }\n" +
				"}\n",
		},
		{
			name: "csharp expression body with a line comment",
			path: "Box.cs",
			content: "class Box {\n" +
				"    // holds a ( value\n" +
				"    public int Half(int n) => n / 2;\n" +
				"}\n",
		},
		{
			name: "php line comment",
			path: "half.php",
			content: "<?php\n" +
				"$half = intdiv($n, 2); // half of ( n\n" +
				"echo $half;\n",
		},
		{
			name: "swift array literal and a line comment",
			path: "Box.swift",
			content: "struct Box {\n" +
				"    // a comment with ( an unmatched paren\n" +
				"    let items: [String] = [\"a\", \"b\"]\n" +
				"    func half(_ n: Int) -> Int { n / 2 }\n" +
				"}\n",
		},
		{
			name: "kotlin block comment holding an unmatched brace",
			path: "half.kt",
			content: "fun half(n: Int): Int {\n" +
				"    /* an unmatched { lives in this block comment */\n" +
				"    return n / 2\n" +
				"}\n",
		},
		{
			name:    "go goes through the real parser",
			path:    "main.go",
			content: "package main\n\nfunc main() {}\n",
		},
		{
			name:    "blank content has nothing to break",
			path:    "empty.py",
			content: "   \n\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := checkSyntax(tt.path, tt.content)
			if !got.valid {
				t.Errorf("checkSyntax(%q, …).valid = false, want true; errors = %v", tt.path, formatErrors(got.errors))
			}
		})
	}
}

// One broken snippet per error branch: every message the checker can emit is reachable and lands
// on the right line, so a future gate cannot silently turn a branch off.
func TestCheckSyntaxReportsEachBrokenShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		path    string
		content string
		want    string
		line    int
	}{
		{
			name:    "unmatched closing parenthesis",
			path:    "a.js",
			content: "const x = 1);\n",
			want:    "unmatched closing parenthesis ')'",
			line:    1,
		},
		{
			name:    "unmatched closing bracket",
			path:    "a.js",
			content: "const x = 1];\n",
			want:    "unmatched closing bracket ']'",
			line:    1,
		},
		{
			name:    "unmatched closing brace",
			path:    "a.js",
			content: "function f() {}\n}\n",
			want:    "unmatched closing brace '}'",
			line:    2,
		},
		{
			name:    "unclosed string literal",
			path:    "a.js",
			content: "const s = \"oops;\n",
			want:    "unclosed string literal (opened with \")",
			line:    2,
		},
		{
			name:    "unclosed parenthesis",
			path:    "a.py",
			content: "x = (1\n",
			want:    "unclosed parenthesis '('",
			line:    1,
		},
		{
			name:    "unclosed bracket",
			path:    "a.py",
			content: "x = [1\n",
			want:    "unclosed bracket '['",
			line:    1,
		},
		{
			name:    "unclosed brace",
			path:    "a.js",
			content: "function f() {\n  return 1;\n",
			want:    "unclosed brace '{'",
			line:    1,
		},
		{
			name:    "truncated final expression",
			path:    "a.js",
			content: "const a = 1;\nconst b = 2,\n",
			want:    "file appears truncated (ends with incomplete expression)",
			line:    2,
		},
		{
			name:    "missing indented block",
			path:    "a.py",
			content: "def f():\nreturn 1\n",
			want:    "expected indented block after line 1",
			line:    2,
		},
		{
			name:    "go parser error",
			path:    "a.go",
			content: "package main\nfunc main() {\n",
			want:    "expected '}'",
			line:    2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := checkSyntax(tt.path, tt.content)
			if got.valid {
				t.Fatalf("checkSyntax(%q, %q).valid = true, want false", tt.path, tt.content)
			}
			found := false
			for _, e := range got.errors {
				if strings.Contains(e.message, tt.want) && e.line == tt.line {
					found = true
				}
			}
			if !found {
				t.Errorf("errors = %v, want one containing %q on line %d", formatErrors(got.errors), tt.want, tt.line)
			}
		})
	}
}

// A block comment left open swallows the rest of the file rather than corrupting the stack: the
// checker stays conservative — it under-reports instead of inventing breakage.
func TestCheckSyntaxUnclosedBlockCommentReportsNothing(t *testing.T) {
	t.Parallel()
	got := checkSyntax("a.c", "int main(void) {\n    return 0;\n}\n/* trailing note with a stray { brace\n")
	if !got.valid {
		t.Errorf("valid = false, want true; errors = %v", formatErrors(got.errors))
	}
}

// formatErrors renders a result's errors for a failure message.
func formatErrors(errs []syntaxError) []string {
	out := make([]string, 0, len(errs))
	for _, e := range errs {
		out = append(out, e.message)
	}
	return out
}
