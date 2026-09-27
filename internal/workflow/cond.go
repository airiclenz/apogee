package workflow

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The words that join, negate and group condition terms. They are reserved: a field or a bare
// value spelled like one must be written differently (a value may be quoted).
const (
	keywordAnd = "and"
	keywordOr  = "or"
	keywordNot = "not"
)

// The six comparison operators a condition term may use.
const (
	opEqual        = "=="
	opNotEqual     = "!="
	opGreater      = ">"
	opGreaterEqual = ">="
	opLess         = "<"
	opLessEqual    = "<="
)

// comparisonOps lists the operators in the order a fix message names them.
var comparisonOps = []string{opEqual, opNotEqual, opGreater, opGreaterEqual, opLess, opLessEqual}

// integerPattern is the spelling of an integer literal.
var integerPattern = regexp.MustCompile(`^-?[0-9]+$`)

// endOfInput is how an error quotes the position past the last token.
const endOfInput = "end of condition"

// Cond is a parsed `when:` condition: `field op value` terms joined by `and`, `or` and `not`,
// grouped with parentheses. `not` binds tightest, then `and`, then `or`. A Cond is immutable; its
// zero value has no terms and evaluates false.
type Cond struct {
	root condNode
}

// condNode is one node of a parsed condition.
type condNode interface {
	eval(r Receipt) bool
	check(spec ReceiptSpec) error
}

// CondError is a condition that does not parse or does not type-check. Token is the offending
// token exactly as written (or "end of condition"), Offset its byte offset in the input.
type CondError struct {
	Token   string
	Offset  int
	Message string
}

// Error renders the problem quoting the offending token, e.g. `at "=": compare with ==`.
func (e *CondError) Error() string {
	if e.Token == endOfInput {
		return fmt.Sprintf("at the %s: %s", endOfInput, e.Message)
	}
	return fmt.Sprintf("at %q (offset %d): %s", e.Token, e.Offset, e.Message)
}

// ParseCond reads a condition's syntax. It knows nothing about the receipt the condition will read;
// Cond.Check type-checks it against a ReceiptSpec. Every error is a *CondError quoting the token it
// stopped at.
func ParseCond(input string) (Cond, error) {
	tokens, err := lexCond(input)
	if err != nil {
		return Cond{}, err
	}
	if len(tokens) == 1 {
		return Cond{}, &CondError{Token: endOfInput, Offset: len(input), Message: "the condition is empty; write field op value, e.g. status == ok"}
	}

	p := &condParser{tokens: tokens}
	root, err := p.parseOr()
	if err != nil {
		return Cond{}, err
	}
	if next := p.peek(); next.kind != tokenEnd {
		return Cond{}, next.errorf("a complete condition ends before this; join terms with and / or")
	}
	return Cond{root: root}, nil
}

// Check type-checks c against spec: every term names a field spec knows (status and summary are
// always known), a list field is never compared, an int is compared with an integer, and an enum or
// text field only with == or != (an enum's value must be one it declares). The error is a
// *CondError quoting the offending token.
func (c Cond) Check(spec ReceiptSpec) error {
	if c.root == nil {
		return &CondError{Token: endOfInput, Message: "the condition is empty; write field op value, e.g. status == ok"}
	}
	return c.root.check(spec)
}

// Eval reports whether r satisfies c. A term on a field r does not carry, or whose value is not of
// the kind the term compares, is false — never a panic or an error — so `not` of such a term is
// true.
func (c Cond) Eval(r Receipt) bool {
	if c.root == nil {
		return false
	}
	return c.root.eval(r)
}

// andNode is `left and right`.
type andNode struct{ left, right condNode }

func (n andNode) eval(r Receipt) bool { return n.left.eval(r) && n.right.eval(r) }

func (n andNode) check(spec ReceiptSpec) error {
	if err := n.left.check(spec); err != nil {
		return err
	}
	return n.right.check(spec)
}

// orNode is `left or right`.
type orNode struct{ left, right condNode }

func (n orNode) eval(r Receipt) bool { return n.left.eval(r) || n.right.eval(r) }

func (n orNode) check(spec ReceiptSpec) error {
	if err := n.left.check(spec); err != nil {
		return err
	}
	return n.right.check(spec)
}

// notNode is `not inner`.
type notNode struct{ inner condNode }

func (n notNode) eval(r Receipt) bool { return !n.inner.eval(r) }

func (n notNode) check(spec ReceiptSpec) error { return n.inner.check(spec) }

// termNode is one `field op value` comparison. field and value keep their tokens so a type error
// can quote them.
type termNode struct {
	field condToken
	op    condToken
	value condToken
}

// check type-checks the term against spec.
func (n termNode) check(spec ReceiptSpec) error {
	name := n.field.text
	fieldType, known := spec.Field(name)
	if !known {
		return n.field.errorf(fmt.Sprintf("no receipt field is named %s; the fields are %s", name, knownFields(spec)))
	}

	switch fieldType.Kind {
	case FieldList:
		return n.field.errorf(fmt.Sprintf("%s is a list field, and a condition cannot compare a list", name))
	case FieldInt:
		if n.value.kind != tokenInteger {
			return n.value.errorf(fmt.Sprintf("%s is an int field; compare it with a whole number", name))
		}
		return nil
	}

	if n.op.text != opEqual && n.op.text != opNotEqual {
		return n.op.errorf(fmt.Sprintf("%s is a %s field; compare it only with == or !=", name, fieldType.Kind))
	}
	if fieldType.Kind == FieldEnum && !slices.Contains(fieldType.Values, n.value.text) {
		return n.value.errorf(fmt.Sprintf("%s is one of %s", name, strings.Join(fieldType.Values, ", ")))
	}
	return nil
}

// eval compares the receipt's value for the field with the term's value. The receipt value's shape
// picks the comparison: a whole number compares numerically with an integer literal, a string
// compares with the literal's text under == or != (so an enum value spelled `2` still matches).
func (n termNode) eval(r Receipt) bool {
	value, present := receiptValue(r, n.field.text)
	if !present {
		return false
	}
	if number, isInt := asInt64(value); isInt {
		return n.value.kind == tokenInteger && compareOrdered(number, n.value.number, n.op.text)
	}
	have, isString := value.(string)
	if !isString {
		return false
	}
	switch n.op.text {
	case opEqual:
		return have == n.value.text
	case opNotEqual:
		return have != n.value.text
	}
	return false
}

// receiptValue returns the value r holds for the named field, the core fields included. A core
// field is always present; a typed one only when r carries it.
func receiptValue(r Receipt, name string) (any, bool) {
	switch name {
	case FieldStatus:
		return string(r.Status), true
	case FieldSummary:
		return r.Summary, true
	}
	value, present := r.Fields[name]
	return value, present
}

// compareOrdered applies one comparison operator to two integers.
func compareOrdered(have, want int64, op string) bool {
	switch op {
	case opEqual:
		return have == want
	case opNotEqual:
		return have != want
	case opGreater:
		return have > want
	case opGreaterEqual:
		return have >= want
	case opLess:
		return have < want
	case opLessEqual:
		return have <= want
	}
	return false
}

// asInt64 converts a whole number in any shape isInteger accepts; ok is false for anything else,
// or for a float outside the int64 range.
func asInt64(value any) (int64, bool) {
	if !isInteger(value) {
		return 0, false
	}
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int32:
		return int64(number), true
	case int64:
		return number, true
	case float64:
		if number < math.MinInt64 || number >= math.MaxInt64 {
			return 0, false
		}
		return int64(number), true
	case json.Number:
		parsed, err := number.Int64()
		return parsed, err == nil
	}
	return 0, false
}

// knownFields names every field a condition against spec may read, core fields first.
func knownFields(spec ReceiptSpec) string {
	return strings.Join(append([]string{FieldStatus, FieldSummary}, sortedKeys(spec)...), ", ")
}

// condTokenKind classifies a lexed token.
type condTokenKind int

// The token kinds. A word is anything unquoted that is not an operator, a parenthesis or an
// integer; keywords are words the parser treats specially.
const (
	tokenEnd condTokenKind = iota
	tokenWord
	tokenInteger
	tokenString
	tokenOp
	tokenOpen
	tokenClose
)

// condToken is one lexed token: its kind, its text (a string's text without the quotes), its
// spelling as written, its byte offset, and an integer's value.
type condToken struct {
	kind   condTokenKind
	text   string
	raw    string
	offset int
	number int64
}

// errorf builds the CondError for this token.
func (t condToken) errorf(message string) error {
	return &CondError{Token: t.raw, Offset: t.offset, Message: message}
}

// isKeyword reports whether t is one of the joining words.
func (t condToken) isKeyword(word string) bool {
	return t.kind == tokenWord && t.text == word
}

// lexCond splits input into tokens, ending with a tokenEnd.
func lexCond(input string) ([]condToken, error) {
	var tokens []condToken
	for offset := 0; offset < len(input); {
		char := input[offset]
		switch {
		case char == ' ' || char == '\t' || char == '\n' || char == '\r':
			offset++
		case char == '(' || char == ')':
			kind := tokenOpen
			if char == ')' {
				kind = tokenClose
			}
			tokens = append(tokens, condToken{kind: kind, text: string(char), raw: string(char), offset: offset})
			offset++
		case strings.IndexByte("=!<>", char) >= 0:
			token, err := lexOperator(input, offset)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, token)
			offset += len(token.raw)
		case char == '"':
			token, err := lexString(input, offset)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, token)
			offset += len(token.raw)
		default:
			token, err := lexWord(input, offset)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, token)
			offset += len(token.raw)
		}
	}
	return append(tokens, condToken{kind: tokenEnd, raw: endOfInput, offset: len(input)}), nil
}

// lexOperator reads the comparison operator starting at offset.
func lexOperator(input string, offset int) (condToken, error) {
	for _, width := range []int{2, 1} {
		if offset+width > len(input) {
			continue
		}
		candidate := input[offset : offset+width]
		if slices.Contains(comparisonOps, candidate) {
			return condToken{kind: tokenOp, text: candidate, raw: candidate, offset: offset}, nil
		}
	}
	raw := input[offset : offset+1]
	message := "not a comparison; use one of " + strings.Join(comparisonOps, " ")
	if raw == "=" {
		message = "compare with ==, not ="
	}
	return condToken{}, &CondError{Token: raw, Offset: offset, Message: message}
}

// lexString reads the double-quoted value starting at offset. It has no escapes: it runs to the
// next double quote.
func lexString(input string, offset int) (condToken, error) {
	closing := strings.IndexByte(input[offset+1:], '"')
	if closing < 0 {
		return condToken{}, &CondError{Token: input[offset:], Offset: offset, Message: "the quoted value is never closed; end it with \""}
	}
	raw := input[offset : offset+closing+2]
	return condToken{kind: tokenString, text: raw[1 : len(raw)-1], raw: raw, offset: offset}, nil
}

// lexWord reads the bare word or integer starting at offset: every byte up to whitespace, a
// parenthesis, an operator character or a quote.
func lexWord(input string, offset int) (condToken, error) {
	end := offset
	for end < len(input) && strings.IndexByte(" \t\n\r()=!<>\"", input[end]) < 0 {
		end++
	}
	raw := input[offset:end]
	if !integerPattern.MatchString(raw) {
		return condToken{kind: tokenWord, text: raw, raw: raw, offset: offset}, nil
	}
	number, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return condToken{}, &CondError{Token: raw, Offset: offset, Message: "the number is too large"}
	}
	return condToken{kind: tokenInteger, text: raw, raw: raw, offset: offset, number: number}, nil
}

// condParser is a recursive-descent parser over lexed tokens; the last token is always tokenEnd.
type condParser struct {
	tokens   []condToken
	position int
}

// peek returns the next token without consuming it.
func (p *condParser) peek() condToken {
	return p.tokens[p.position]
}

// next consumes and returns the next token; it never moves past tokenEnd.
func (p *condParser) next() condToken {
	token := p.tokens[p.position]
	if token.kind != tokenEnd {
		p.position++
	}
	return token
}

// parseOr reads `and-expr (or and-expr)*`.
func (p *condParser) parseOr() (condNode, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().isKeyword(keywordOr) {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = orNode{left: left, right: right}
	}
	return left, nil
}

// parseAnd reads `not-expr (and not-expr)*`.
func (p *condParser) parseAnd() (condNode, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.peek().isKeyword(keywordAnd) {
		p.next()
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = andNode{left: left, right: right}
	}
	return left, nil
}

// parseNot reads `not not-expr` or a primary.
func (p *condParser) parseNot() (condNode, error) {
	if !p.peek().isKeyword(keywordNot) {
		return p.parsePrimary()
	}
	p.next()
	inner, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	return notNode{inner: inner}, nil
}

// parsePrimary reads `( or-expr )` or a term.
func (p *condParser) parsePrimary() (condNode, error) {
	if p.peek().kind != tokenOpen {
		return p.parseTerm()
	}
	open := p.next()
	inner, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokenClose {
		return nil, p.peek().errorf(fmt.Sprintf("the ( at offset %d is never closed; add )", open.offset))
	}
	p.next()
	return inner, nil
}

// parseTerm reads `field op value`.
func (p *condParser) parseTerm() (condNode, error) {
	field := p.next()
	if field.kind != tokenWord || isJoiningWord(field) {
		return nil, field.errorf("expected a receipt field name, as in status == ok")
	}
	op := p.next()
	if op.kind != tokenOp {
		return nil, op.errorf(fmt.Sprintf("expected a comparison after %s: one of %s", field.text, strings.Join(comparisonOps, " ")))
	}
	value := p.next()
	isValue := value.kind == tokenInteger || value.kind == tokenString || (value.kind == tokenWord && !isJoiningWord(value))
	if !isValue {
		return nil, value.errorf(fmt.Sprintf("expected a value after %s %s; quote it if it is spelled like and / or / not", field.text, op.text))
	}
	return termNode{field: field, op: op, value: value}, nil
}

// isJoiningWord reports whether t is and, or or not.
func isJoiningWord(t condToken) bool {
	return t.isKeyword(keywordAnd) || t.isKeyword(keywordOr) || t.isKeyword(keywordNot)
}
