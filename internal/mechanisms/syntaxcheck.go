package mechanisms

import (
	"fmt"
	"go/parser"
	"go/scanner"
	"go/token"
	"path/filepath"
	"strings"
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

	for lineNum, line := range lines {
		lineNo := lineNum + 1
		for i := 0; i < len(line); {
			r, size := utf8.DecodeRuneInString(line[i:])
			i += size

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
				}
				continue
			}

			if r == '#' && (lang == "python" || lang == "ruby") {
				break
			}
			if r == '/' && i < len(line) {
				if next, _ := utf8.DecodeRuneInString(line[i:]); next == '/' {
					break
				}
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

	checkTruncation(lines, &result)
	if lang == "python" {
		checkPythonIndent(lines, &result)
	}

	result.valid = len(result.errors) == 0
	return result
}

// checkTruncation flags a file whose last non-blank line ends on an incomplete expression — the
// shape a truncated generation leaves.
func checkTruncation(lines []string, result *syntaxResult) {
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		trimmed = strings.TrimSpace(stripTrailingComment(trimmed))
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

// stripTrailingComment drops a trailing // or # line comment outside of string literals, so
// truncation detection reads the real last token.
func stripTrailingComment(s string) string {
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
		if r == '#' {
			return s[:i]
		}
		if r == '/' && i+1 < len(s) && s[i+1] == '/' {
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
