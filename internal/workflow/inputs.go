package workflow

import (
	"fmt"
	"regexp"
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
