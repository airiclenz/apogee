package workflow

import (
	"fmt"
	"regexp"
	"strings"
)

// InputDecl is one input a Recipe declares in its skill's header: the value the user's text fills
// in when the recipe starts (for `audit`: scope and focus). It lives here, beside the Plan it
// feeds, so internal/skills can name it on Skill.Inputs without either package importing the other
// the wrong way round. The yaml keys are the recipe author's own spelling.
type InputDecl struct {
	// Name is the key the user may write as `name=value`, and the order of the declarations is the
	// order bare tokens bind in.
	Name string `yaml:"name" json:"name"`
	// Required marks an input the recipe cannot start without; one with a Default is never missing.
	Required bool `yaml:"required,omitempty" json:"required,omitempty"`
	// Default is the value an unbound input takes.
	Default string `yaml:"default,omitempty" json:"default,omitempty"`
	// Description says what the input is for, for the question a missing required input asks.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// inputNameRe is the shape an input name takes: a letter, then letters, digits, `_` or `-`. It
// keeps a name one `key=value` token — no whitespace, no `=`, no quote.
var inputNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

// ValidateInputs reports every way a recipe's input declarations fall short: a missing or
// malformed name, or a name declared twice. Each Problem names the field `inputs` and says how to
// fix it; an empty result means the declarations are usable.
func ValidateInputs(decls []InputDecl) []Problem {
	var problems []Problem
	seen := make(map[string]bool, len(decls))
	for index, decl := range decls {
		switch {
		case decl.Name == "":
			problems = append(problems, Problem{Field: "inputs", Message: fmt.Sprintf(
				"input %d has no name; give it a name the user can write as name=value", index+1,
			)})
		case !inputNameRe.MatchString(decl.Name):
			problems = append(problems, Problem{Field: "inputs", Message: fmt.Sprintf(
				"input name %q is not one word; start with a letter and use only letters, digits, _ or -",
				decl.Name,
			)})
		case seen[decl.Name]:
			problems = append(problems, Problem{Field: "inputs", Message: fmt.Sprintf(
				"input %q is declared twice; declare each input once", decl.Name,
			)})
		}
		seen[decl.Name] = true
	}
	return problems
}

// inputToken is one whitespace-separated token of the user's text, quotes removed. A keyed token
// (`name=value`) carries the name it binds; any other token is positional.
type inputToken struct {
	raw     string
	key     string
	value   string
	isKeyed bool
}

// BindInputs binds the user's text to a recipe's declared inputs, with no model call. The text is
// split on whitespace into tokens; a token may quote any part of itself with " or ' (no escapes),
// so `"two words"` and `focus="error handling"` are each one token. A token whose unquoted lead is
// an input-name-shaped word followed by `=` binds that input by name; every other token binds, in
// declared order, the next input no keyed token has already bound. An empty value (`scope=` or a
// bare `""`) binds nothing — a positional one still takes its slot — so the input falls to its
// default.
//
// The returned map holds every declared input except the missing ones: its bound value, else its
// Default, else "" for an optional input. missing lists, in declared order, the required inputs
// with neither a value nor a default — the ones the caller asks the user for. The error names
// every unknown key, key given twice, surplus positional token and unterminated quote; on an error
// the map and missing are nil.
func BindInputs(decls []InputDecl, text string) (map[string]string, []string, error) {
	tokens, err := tokenizeInputs(text)
	if err != nil {
		return nil, nil, err
	}

	bound, err := bindTokens(decls, tokens)
	if err != nil {
		return nil, nil, err
	}

	values := make(map[string]string, len(decls))
	var missing []string
	for _, decl := range decls {
		value := bound[decl.Name]
		if value == "" {
			value = decl.Default
		}
		if value == "" && decl.Required {
			missing = append(missing, decl.Name)
			continue
		}
		values[decl.Name] = value
	}
	return values, missing, nil
}

// bindTokens assigns keyed tokens by name, then positional tokens to the declared inputs no keyed
// token took, in order. The error names every unknown key, repeated key and surplus token at once,
// so one correction fixes the line.
func bindTokens(decls []InputDecl, tokens []inputToken) (map[string]string, error) {
	declared := make(map[string]bool, len(decls))
	names := make([]string, 0, len(decls))
	for _, decl := range decls {
		declared[decl.Name] = true
		names = append(names, decl.Name)
	}

	var problems []string
	bound := make(map[string]string, len(decls))
	taken := make(map[string]bool, len(decls))
	var positional []inputToken
	for _, token := range tokens {
		switch {
		case !token.isKeyed:
			positional = append(positional, token)
		case !declared[token.key]:
			problems = append(problems, fmt.Sprintf("unknown input %q", token.key))
		case taken[token.key]:
			problems = append(problems, fmt.Sprintf("input %q is given twice", token.key))
		default:
			taken[token.key] = true
			bound[token.key] = token.value
		}
	}

	next := 0
	for _, decl := range decls {
		if taken[decl.Name] || next == len(positional) {
			continue
		}
		taken[decl.Name] = true
		bound[decl.Name] = positional[next].value
		next++
	}
	for _, token := range positional[next:] {
		problems = append(problems, fmt.Sprintf("surplus value %q", token.raw))
	}

	if len(problems) > 0 {
		return nil, fmt.Errorf(
			"recipe inputs: %s (the inputs are: %s)",
			strings.Join(problems, "; "), declaredList(names),
		)
	}
	return bound, nil
}

// declaredList renders the declared input names for an error, or says there are none.
func declaredList(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

// tokenizeInputs splits text into inputTokens: runs of non-whitespace, where a " or ' opens a
// quoted span that runs to the same quote character and may hold whitespace. An unterminated
// quote is an error naming the token it opened in.
func tokenizeInputs(text string) ([]inputToken, error) {
	var tokens []inputToken
	for start := 0; start < len(text); {
		if isInputSpace(text[start]) {
			start++
			continue
		}
		token, end, err := scanInputToken(text, start)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
		start = end
	}
	return tokens, nil
}

// scanInputToken scans the token starting at text[start] (not whitespace) and returns it with the
// offset just past it. The token is keyed when an `=` arrives before any quote and the unquoted
// text before it is an input-name-shaped word.
func scanInputToken(text string, start int) (inputToken, int, error) {
	var value strings.Builder
	token := inputToken{}
	isQuoteSeen := false
	index := start
	for index < len(text) && !isInputSpace(text[index]) {
		current := text[index]
		switch {
		case current == '"' || current == '\'':
			closing := strings.IndexByte(text[index+1:], current)
			if closing < 0 {
				return inputToken{}, 0, fmt.Errorf(
					"recipe inputs: unterminated %c quote in %q; close it with a matching %c",
					current, text[start:], current,
				)
			}
			value.WriteString(text[index+1 : index+1+closing])
			index += closing + 2
			isQuoteSeen = true
		case current == '=' && !isQuoteSeen && !token.isKeyed && inputNameRe.MatchString(value.String()):
			token.isKeyed = true
			token.key = value.String()
			value.Reset()
			index++
		default:
			value.WriteByte(current)
			index++
		}
	}
	token.raw = text[start:index]
	token.value = value.String()
	return token, index, nil
}

// isInputSpace reports whether b separates input tokens: ASCII whitespace, a newline included.
func isInputSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
