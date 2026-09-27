package workflow

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// condSpec is the receipt spec the condition cases below type-check against.
var condSpec = ReceiptSpec{
	"findings": "int",
	"verdict":  "confirmed|refuted|unclear",
	"level":    "1|2|3",
	"note":     "text",
	"paths":    "list",
}

// mustParseCond parses input or fails t.
func mustParseCond(t *testing.T, input string) Cond {
	t.Helper()
	cond, err := ParseCond(input)
	if err != nil {
		t.Fatalf("ParseCond(%q) = %v, want no error", input, err)
	}
	return cond
}

func TestCondEvaluatesComparisonsAndPrecedence(t *testing.T) {
	t.Parallel()

	receipt := Receipt{Status: StatusOK, Summary: "two issues", Fields: map[string]any{
		"findings": float64(3), "verdict": "confirmed", "level": "2", "note": "looks fine",
	}}
	cases := []struct {
		input string
		want  bool
	}{
		{"findings == 3", true},
		{"findings != 3", false},
		{"findings > 2", true},
		{"findings >= 3", true},
		{"findings < 3", false},
		{"findings <= 3", true},
		{"findings > -1", true},
		{"verdict == confirmed", true},
		{"verdict != confirmed", false},
		{`verdict == "confirmed"`, true},
		{"level == 2", true},
		{"status == ok", true},
		{"status == blocked", false},
		{`summary == "two issues"`, true},
		{`note != "looks fine"`, false},
		{"status == blocked or findings > 2 and verdict == refuted", false},
		{"(status == blocked or findings > 2) and verdict == confirmed", true},
		{"status == ok or findings > 2 and verdict == refuted", true},
		{"not status == ok or findings == 3", true},
		{"not (status == ok and findings == 3)", false},
		{"not not status == ok", true},
		{"((findings==3))", true},
	}
	for _, c := range cases {
		t.Run(c.input, func(t *testing.T) {
			t.Parallel()
			cond := mustParseCond(t, c.input)
			if err := cond.Check(condSpec); err != nil {
				t.Fatalf("Check = %v, want no error", err)
			}

			got := cond.Eval(receipt)

			if got != c.want {
				t.Errorf("Eval = %v, want %v", got, c.want)
			}
		})
	}
}

func TestCondReadsEveryIntegerShape(t *testing.T) {
	t.Parallel()

	cond := mustParseCond(t, "findings >= 2")
	for _, value := range []any{2, int32(2), int64(2), float64(2), json.Number("2")} {
		receipt := Receipt{Status: StatusOK, Fields: map[string]any{"findings": value}}

		got := cond.Eval(receipt)

		if !got {
			t.Errorf("Eval with findings %T(%v) = false, want true", value, value)
		}
	}
}

func TestCondComparisonOnAnAbsentFieldIsFalse(t *testing.T) {
	t.Parallel()

	partial := Receipt{Status: StatusPartial, Summary: "ran out of steps"}
	cases := []struct {
		input string
		want  bool
	}{
		{"findings > 0", false},
		{"findings == 0", false},
		{"findings != 0", false},
		{"verdict == confirmed", false},
		{"not findings > 0", true},
		{"findings > 0 or status == partial", true},
	}
	for _, c := range cases {
		t.Run(c.input, func(t *testing.T) {
			t.Parallel()
			cond := mustParseCond(t, c.input)

			got := cond.Eval(partial)

			if got != c.want {
				t.Errorf("Eval = %v, want %v", got, c.want)
			}
		})
	}
}

func TestCondComparisonOnAMistypedValueIsFalse(t *testing.T) {
	t.Parallel()

	receipt := Receipt{Status: StatusOK, Fields: map[string]any{"findings": "three", "verdict": 3}}
	for _, input := range []string{"findings > 2", "findings == 3", "verdict == confirmed"} {
		cond := mustParseCond(t, input)

		got := cond.Eval(receipt)

		if got {
			t.Errorf("Eval(%q) = true on a mistyped value, want false", input)
		}
	}
}

func TestZeroCondIsFalseAndFailsCheck(t *testing.T) {
	t.Parallel()

	var cond Cond

	got := cond.Eval(Receipt{Status: StatusOK})

	if got {
		t.Error("zero Cond evaluated true, want false")
	}
	if err := cond.Check(condSpec); err == nil {
		t.Error("zero Cond passed Check, want an error")
	}
}

func TestParseCondQuotesTheOffendingToken(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		input     string
		wantToken string
		wantText  string
	}{
		{"empty", "", endOfInput, "empty"},
		{"blank", "   ", endOfInput, "empty"},
		{"single equals", "verdict = confirmed", "=", "=="},
		{"bang alone", "verdict ! confirmed", "!", "comparison"},
		{"missing operator", "verdict confirmed", "confirmed", "expected a comparison"},
		{"missing value", "findings >", endOfInput, "expected a value"},
		{"keyword as value", "verdict == and", "and", "quote it"},
		{"keyword as field", "and == 3", "and", "field name"},
		{"number as field", "3 == findings", "3", "field name"},
		{"dangling and", "status == ok and", endOfInput, "field name"},
		{"trailing term", "status == ok findings > 2", "findings", "join terms with and / or"},
		{"unclosed paren", "(status == ok", endOfInput, "never closed"},
		{"stray close", "status == ok)", ")", "join terms"},
		{"empty parens", "()", ")", "field name"},
		{"unclosed quote", `note == "open`, `"open`, "never closed"},
		{"huge number", "findings > 99999999999999999999", "99999999999999999999", "too large"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			_, err := ParseCond(c.input)

			var condErr *CondError
			if !errors.As(err, &condErr) {
				t.Fatalf("ParseCond(%q) = %v, want a *CondError", c.input, err)
			}
			if condErr.Token != c.wantToken || !strings.Contains(condErr.Message, c.wantText) {
				t.Errorf("got token %q message %q, want token %q message containing %q", condErr.Token, condErr.Message, c.wantToken, c.wantText)
			}
			if !strings.Contains(err.Error(), c.wantToken) {
				t.Errorf("Error() = %q does not quote %q", err.Error(), c.wantToken)
			}
		})
	}
}

func TestCondCheckRefusesUnknownFieldsAndTypeMismatches(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		input     string
		wantToken string
		wantText  string
	}{
		{"unknown field", "severity == high", "severity", "the fields are status, summary, findings, level, note, paths, verdict"},
		{"unknown field in a later term", "status == ok and severity > 1", "severity", "no receipt field"},
		{"int against a word", "findings > many", "many", "whole number"},
		{"int against a quoted number", `findings == "3"`, `"3"`, "whole number"},
		{"enum value not declared", "verdict == maybe", "maybe", "confirmed, refuted, unclear"},
		{"enum ordered", "verdict > confirmed", ">", "only with == or !="},
		{"status value not a status", "status == done", "done", "ok, partial, blocked"},
		{"text ordered", `summary < "b"`, "<", "only with == or !="},
		{"list compared", "paths == a", "paths", "cannot compare a list"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			cond := mustParseCond(t, c.input)

			err := cond.Check(condSpec)

			var condErr *CondError
			if !errors.As(err, &condErr) {
				t.Fatalf("Check(%q) = %v, want a *CondError", c.input, err)
			}
			if condErr.Token != c.wantToken || !strings.Contains(condErr.Message, c.wantText) {
				t.Errorf("got token %q message %q, want token %q message containing %q", condErr.Token, condErr.Message, c.wantToken, c.wantText)
			}
		})
	}
}

func TestCondCheckAlwaysKnowsStatusAndSummary(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"status == ok", "status != blocked", `summary == "done"`, "summary != x"} {
		cond := mustParseCond(t, input)

		err := cond.Check(nil)

		if err != nil {
			t.Errorf("Check(%q) against an empty spec = %v, want no error", input, err)
		}
	}
}
