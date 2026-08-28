package mechanisms

// The syntax-check ENGINE: a pure checker over a write payload, no catalogue row, no Mechanism.
// It registers nothing and decides nothing about the loop — syntax.go owns the Mechanism that
// calls in here and acts on the verdict. Named `syntaxcheck.go` until ADR 0043, which cured the
// pair: `syntax.go` is the Mechanism (matching validate.go / autofix.go), this is its engine.

import (
	"fmt"
	"go/parser"
	"go/scanner"
	"go/token"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Lightweight syntax validation for LLM-written code, ported from apogee-sim's internal/syntax
// package @pin (syntax.go, go_check.go, generic_check.go). Go is checked with the real parser;
// every other detected language falls back to a bracket/string/truncation heuristic. The checker
// is deliberately conservative — it reports only unambiguous breakage — because a false positive
// would defer a needless correction and cost a Turn.

// syntaxResult is the outcome of a syntax check: valid, the detected language, and any errors.
type syntaxResult struct {
	valid    bool
	language string
	errors   []syntaxError
}

// syntaxError is one located syntax problem.
type syntaxError struct {
	line    int
	column  int
	message string
}

// checkSyntax validates content by the language its path implies. Empty content is treated as
// valid (there is nothing to break); an unrecognised extension yields an empty language and a
// valid result, so the caller skips it.
func checkSyntax(path, content string) syntaxResult {
	lang := detectLanguage(path)
	if strings.TrimSpace(content) == "" {
		return syntaxResult{valid: true, language: lang}
	}
	switch lang {
	case "go":
		return checkGoSyntax(content)
	default:
		return checkBrackets(content, lang)
	}
}

// detectLanguage maps a file extension to a language identifier, or "" when unrecognised.
func detectLanguage(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js", ".jsx", ".mjs":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".cxx", ".hpp":
		return "cpp"
	case ".rb":
		return "ruby"
	case ".cs":
		return "csharp"
	case ".php":
		return "php"
	case ".swift":
		return "swift"
	case ".kt", ".kts":
		return "kotlin"
	default:
		return ""
	}
}

// hasCStyleComments reports whether lang comments with `//` to end of line and `/* … */` blocks.
// The gate matters because `//` is not a comment everywhere: in Python it is floor division
// (`n // 2`) and in Ruby an empty regex literal, so breaking out of the line scan there abandons a
// valid line before its closing bracket and invents an "unclosed bracket" — a false positive that
// fires ActionRetry against correct code, which the Bypass floor forbids. The second false-positive
// family the gate must not create is the JavaScript/TypeScript regex literal: where `//` really is
// a comment, a lone `/` may still open a literal whose quotes and brackets are inert, so
// checkBrackets applies the regexOpeners rule immediately after this gate.
func hasCStyleComments(lang string) bool {
	switch lang {
	case "javascript", "typescript", "go", "rust", "java", "c", "cpp", "csharp", "swift", "kotlin", "php":
		return true
	default:
		return false
	}
}

// hasHashComments reports whether lang's line comment is `#`. Elsewhere `#` is code — a C
// preprocessor directive, a JavaScript private field — so it must not end the line scan. PHP is
// deliberately absent even though `#` comments there: `#[Attr(…)]` is an attribute, and ending the
// scan at `#` would drop a multi-line attribute's opening paren while still seeing its closer.
func hasHashComments(lang string) bool {
	switch lang {
	case "python", "ruby":
		return true
	default:
		return false
	}
}

// checkGoSyntax parses Go source with the standard parser and reports exact syntax errors.
func checkGoSyntax(content string) syntaxResult {
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "check.go", content, parser.AllErrors); err == nil {
		return syntaxResult{valid: true, language: "go"}
	} else {
		result := syntaxResult{language: "go"}
		if errList, ok := err.(scanner.ErrorList); ok {
			for _, e := range errList {
				result.errors = append(result.errors, syntaxError{line: e.Pos.Line, column: e.Pos.Column, message: e.Msg})
			}
		} else {
			result.errors = append(result.errors, syntaxError{line: 1, message: err.Error()})
		}
		return result
	}
}

// regexOpeners are the significant runes after which a `/` opens a JavaScript/TypeScript regex
// literal rather than dividing. The rule checkBrackets applies — for `javascript` and `typescript`
// only, and only once the comment gate has ruled out `//` and `/*`: a `/` opens a regex literal
// when the last significant rune on the current line (the last rune consumed outside a string,
// comment or regex, ignoring spaces and tabs; 0 at line start) is one of these runes, or there is
// none (line start), or the last identifier token on the line, with only whitespace between it and
// the `/`, is one of regexOpenerKeywords. Any other predecessor — an identifier, a digit, `)`, `]`,
// a closing quote — leaves `/` as the division operator it is.
var regexOpeners = map[rune]bool{
	'=': true,
	'(': true,
	',': true,
	'[': true,
	'{': true,
	':': true,
	'!': true,
	'&': true,
	'|': true,
	'?': true,
	';': true,
	'>': true,
	'+': true,
}

// regexOpenerKeywords are the keyword predecessors in the regexOpeners rule: `return /^\s*$/`
// and `case /^\s*$/:` open a regex literal, while `total / 2` divides.
var regexOpenerKeywords = map[string]bool{
	"return": true,
	"case":   true,
}

// isIdentifierRune reports whether r can appear inside a JavaScript/TypeScript identifier token,
// which is what the keyword clause of the regexOpeners rule is matched against.
func isIdentifierRune(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// checkBrackets validates bracket/paren/brace balance, unclosed strings, and common truncation
// patterns for languages without a bundled parser.
func checkBrackets(content, lang string) syntaxResult {
	result := syntaxResult{language: lang}

	lines := strings.Split(content, "\n")
	type bracketInfo struct {
		char rune
		line int
	}
	var stack []bracketInfo
	inString := rune(0)
	escaped := false
	inBlockComment := false
	hasRegexLiterals := lang == "javascript" || lang == "typescript"

	for lineNum, line := range lines {
		lineNo := lineNum + 1
		// A regex literal never spans lines, and neither does the preceding-token state that
		// decides whether a `/` opens one (regexOpeners): both start every line clean. Reaching
		// end of line inside a literal simply ends it — the checker under-reports an unterminated
		// literal rather than inventing breakage.
		inRegex, inCharClass, regexEscaped := false, false, false
		lastRune := rune(0)
		identFrom, identTo, identOpen := 0, 0, false
		for i := 0; i < len(line); {
			start := i
			r, size := utf8.DecodeRuneInString(line[i:])
			i += size

			// A block comment spans lines and holds no code: only `*/` is read inside it, so a
			// commented-out bracket can never reach the stack.
			if inBlockComment {
				if r == '*' && i < len(line) {
					if next, nextSize := utf8.DecodeRuneInString(line[i:]); next == '/' {
						i += nextSize
						inBlockComment = false
					}
				}
				continue
			}

			// Inside a regex literal only the closing unescaped `/` matters: `\` escapes the next
			// rune, and within a `[ … ]` character class a `/` is an ordinary rune. Quotes,
			// backticks, brackets and `//` in here are inert — reading them as code is the false
			// positive that fires ActionRetry against correct JS/TS.
			if inRegex {
				switch {
				case regexEscaped:
					regexEscaped = false
				case r == '\\':
					regexEscaped = true
				case r == '[':
					inCharClass = true
				case r == ']':
					inCharClass = false
				case r == '/' && !inCharClass:
					inRegex = false
					// The closed literal is a value, so the next `/` on the line divides
					// (regexOpeners): in `foo(/a/ / 2)` the stale `(` used to open a second
					// literal that swallowed the closing paren.
					lastRune, identFrom, identTo, identOpen = '/', 0, 0, false
				}
				continue
			}

			if escaped {
				escaped = false
				continue
			}
			if r == '\\' && inString != 0 {
				escaped = true
				continue
			}
			if inString != 0 {
				if r == inString {
					inString = 0
					// A closing quote is a value, so the `/` in `"x" / 1` divides (regexOpeners).
					lastRune, identFrom, identTo, identOpen = r, 0, 0, false
				}
				continue
			}

			if r == '#' && hasHashComments(lang) {
				break
			}
			if r == '/' && i < len(line) && hasCStyleComments(lang) {
				next, nextSize := utf8.DecodeRuneInString(line[i:])
				if next == '/' {
					break
				}
				if next == '*' {
					i += nextSize
					inBlockComment = true
					continue
				}
			}
			if r == '/' && hasRegexLiterals &&
				(lastRune == 0 || regexOpeners[lastRune] || regexOpenerKeywords[line[identFrom:identTo]]) {
				inRegex = true
				continue
			}

			// What precedes the next rune, for the rule above: spaces and tabs leave the last
			// identifier token intact, so `return /^\s*$/` still sees `return`.
			switch {
			case r == ' ' || r == '\t':
				identOpen = false
			case isIdentifierRune(r):
				if !identOpen {
					identFrom, identOpen = start, true
				}
				identTo, lastRune = i, r
			default:
				identFrom, identTo, identOpen = 0, 0, false
				lastRune = r
			}

			switch r {
			case '"', '\'', '`':
				if r == '\'' && (lang == "go" || lang == "rust") {
					continue
				}
				inString = r
			case '(':
				stack = append(stack, bracketInfo{'(', lineNo})
			case '[':
				stack = append(stack, bracketInfo{'[', lineNo})
			case '{':
				stack = append(stack, bracketInfo{'{', lineNo})
			case ')':
				if len(stack) == 0 || stack[len(stack)-1].char != '(' {
					result.errors = append(result.errors, syntaxError{line: lineNo, message: "unmatched closing parenthesis ')'"})
				} else {
					stack = stack[:len(stack)-1]
				}
			case ']':
				if len(stack) == 0 || stack[len(stack)-1].char != '[' {
					result.errors = append(result.errors, syntaxError{line: lineNo, message: "unmatched closing bracket ']'"})
				} else {
					stack = stack[:len(stack)-1]
				}
			case '}':
				if len(stack) == 0 || stack[len(stack)-1].char != '{' {
					result.errors = append(result.errors, syntaxError{line: lineNo, message: "unmatched closing brace '}'"})
				} else {
					stack = stack[:len(stack)-1]
				}
			}
		}
	}

	if inString != 0 {
		result.errors = append(result.errors, syntaxError{line: len(lines), message: fmt.Sprintf("unclosed string literal (opened with %c)", inString)})
	}
	for i := len(stack) - 1; i >= 0; i-- {
		var name string
		switch stack[i].char {
		case '(':
			name = "parenthesis '('"
		case '[':
			name = "bracket '['"
		case '{':
			name = "brace '{'"
		}
		result.errors = append(result.errors, syntaxError{line: stack[i].line, message: fmt.Sprintf("unclosed %s", name)})
	}

	checkTruncation(lines, lang, &result)
	if lang == "python" {
		checkPythonIndent(lines, &result)
	}

	result.valid = len(result.errors) == 0
	return result
}

// checkTruncation flags a file whose last non-blank line ends on an incomplete expression — the
// shape a truncated generation leaves.
func checkTruncation(lines []string, lang string, result *syntaxResult) {
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		trimmed = strings.TrimSpace(stripTrailingComment(trimmed, lang))
		if trimmed == "" {
			continue
		}
		if strings.HasSuffix(trimmed, ",") ||
			strings.HasSuffix(trimmed, "(") ||
			strings.HasSuffix(trimmed, "[") ||
			strings.HasSuffix(trimmed, "{") ||
			strings.HasSuffix(trimmed, "=") ||
			strings.HasSuffix(trimmed, "=>") ||
			strings.HasSuffix(trimmed, "->") {
			result.errors = append(result.errors, syntaxError{line: i + 1, message: "file appears truncated (ends with incomplete expression)"})
		}
		break
	}
}

// stripTrailingComment drops a trailing line comment outside of string literals, so truncation
// detection reads the real last token. Which marker opens a comment is language-dependent (see
// hasCStyleComments): stripping Ruby's `SEP = //` down to `SEP =` would report a complete file as
// truncated.
func stripTrailingComment(s, lang string) string {
	inStr := rune(0)
	escaped := false
	for i, r := range s {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && inStr != 0 {
			escaped = true
			continue
		}
		if inStr != 0 {
			if r == inStr {
				inStr = 0
			}
			continue
		}
		if r == '"' || r == '\'' || r == '`' {
			inStr = r
			continue
		}
		if r == '#' && hasHashComments(lang) {
			return s[:i]
		}
		if r == '/' && i+1 < len(s) && s[i+1] == '/' && hasCStyleComments(lang) {
			return s[:i]
		}
	}
	return s
}

// checkPythonIndent flags a block-opening line (ending in ':') whose following line is not
// indented past it — the missing-indented-block shape.
func checkPythonIndent(lines []string, result *syntaxResult) {
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasSuffix(trimmed, ":") {
			continue
		}
		isBlock := false
		for _, kw := range []string{"def ", "class ", "if ", "elif ", "else:", "for ", "while ", "with ", "try:", "except ", "finally:"} {
			if strings.HasPrefix(trimmed, kw) || trimmed == strings.TrimSuffix(kw, " ") {
				isBlock = true
				break
			}
		}
		if !isBlock {
			continue
		}
		if i+1 < len(lines) {
			nextTrimmed := strings.TrimSpace(lines[i+1])
			if nextTrimmed == "" {
				continue
			}
			if leadingSpaces(lines[i+1]) <= leadingSpaces(line) {
				result.errors = append(result.errors, syntaxError{line: i + 2, message: fmt.Sprintf("expected indented block after line %d", i+1)})
			}
		}
	}
}

// leadingSpaces counts a line's leading indentation, a tab counting as four columns.
func leadingSpaces(s string) int {
	n := 0
	for _, r := range s {
		switch r {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n
		}
	}
	return n
}
