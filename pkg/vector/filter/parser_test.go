// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package filter

import (
	"errors"
	"testing"
)

func TestParse_SimpleComparisons(t *testing.T) {
	tests := []struct {
		name  string
		input string
		meta  map[string]string
		want  bool
	}{
		{"equal", `name = "alice"`, map[string]string{"name": "alice"}, true},
		{"equal miss", `name = "bob"`, map[string]string{"name": "alice"}, false},
		{"not equal", `name != "bob"`, map[string]string{"name": "alice"}, true},
		{"greater than numeric", `age > 25`, map[string]string{"age": "30"}, true},
		{"greater than numeric false", `age > 25`, map[string]string{"age": "20"}, false},
		{"greater equal", `age >= 30`, map[string]string{"age": "30"}, true},
		{"less than", `age < 25`, map[string]string{"age": "20"}, true},
		{"less equal", `age <= 30`, map[string]string{"age": "30"}, true},
		{"float comparison", `price >= 10.5`, map[string]string{"price": "15.99"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.input, err)
			}
			got := expr.Evaluate(tt.meta)
			if got != tt.want {
				t.Errorf("Parse(%q).Evaluate() = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParse_INExpression(t *testing.T) {
	tests := []struct {
		name  string
		input string
		meta  map[string]string
		want  bool
	}{
		{"in match", `category IN ["tech", "science"]`, map[string]string{"category": "tech"}, true},
		{"in miss", `category IN ["tech", "science"]`, map[string]string{"category": "art"}, false},
		{"not in match", `category NOT IN ["tech", "science"]`, map[string]string{"category": "art"}, true},
		{"not in miss", `category NOT IN ["tech", "science"]`, map[string]string{"category": "tech"}, false},
		{"single value", `status IN ["active"]`, map[string]string{"status": "active"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.input, err)
			}
			got := expr.Evaluate(tt.meta)
			if got != tt.want {
				t.Errorf("Parse(%q).Evaluate() = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParse_LogicalExpressions(t *testing.T) {
	tests := []struct {
		name  string
		input string
		meta  map[string]string
		want  bool
	}{
		{
			"AND both true",
			`age > 25 AND name = "alice"`,
			map[string]string{"age": "30", "name": "alice"},
			true,
		},
		{
			"AND one false",
			`age > 25 AND name = "bob"`,
			map[string]string{"age": "30", "name": "alice"},
			false,
		},
		{
			"OR one true",
			`status = "active" OR priority >= 5`,
			map[string]string{"status": "inactive", "priority": "10"},
			true,
		},
		{
			"OR both false",
			`status = "active" OR priority >= 5`,
			map[string]string{"status": "inactive", "priority": "1"},
			false,
		},
		{
			"NOT",
			`NOT deleted = "true"`,
			map[string]string{"deleted": "false"},
			true,
		},
		{
			"NOT negates true",
			`NOT deleted = "true"`,
			map[string]string{"deleted": "true"},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.input, err)
			}
			got := expr.Evaluate(tt.meta)
			if got != tt.want {
				t.Errorf("Parse(%q).Evaluate() = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParse_Parenthesized(t *testing.T) {
	tests := []struct {
		name  string
		input string
		meta  map[string]string
		want  bool
	}{
		{
			"parens change precedence",
			`(a > 1 OR b < 2) AND c = "x"`,
			map[string]string{"a": "5", "b": "10", "c": "x"},
			true,
		},
		{
			"parens change precedence false",
			`(a > 1 OR b < 2) AND c = "y"`,
			map[string]string{"a": "5", "b": "10", "c": "x"},
			false,
		},
		{
			"nested parens",
			`(a = "1" AND (b = "2" OR c = "3"))`,
			map[string]string{"a": "1", "b": "2", "c": "99"},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.input, err)
			}
			got := expr.Evaluate(tt.meta)
			if got != tt.want {
				t.Errorf("Parse(%q).Evaluate() = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParse_NullChecks(t *testing.T) {
	tests := []struct {
		name  string
		input string
		meta  map[string]string
		want  bool
	}{
		{"equals null missing field", `name = null`, map[string]string{}, true},
		{"equals null empty field", `name = null`, map[string]string{"name": ""}, true},
		{"equals null non-empty", `name = null`, map[string]string{"name": "alice"}, false},
		{"not equals null present", `name != null`, map[string]string{"name": "alice"}, true},
		{"not equals null missing", `name != null`, map[string]string{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.input, err)
			}
			got := expr.Evaluate(tt.meta)
			if got != tt.want {
				t.Errorf("Parse(%q).Evaluate() = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParse_TextOperators(t *testing.T) {
	tests := []struct {
		name  string
		input string
		meta  map[string]string
		want  bool
	}{
		{"contains match", `title CONTAINS "golang"`, map[string]string{"title": "learning golang basics"}, true},
		{"contains miss", `title CONTAINS "python"`, map[string]string{"title": "learning golang basics"}, false},
		{"starts with match", `name STARTS_WITH "al"`, map[string]string{"name": "alice"}, true},
		{"starts with miss", `name STARTS_WITH "bo"`, map[string]string{"name": "alice"}, false},
		{"text match case insensitive", `title TEXT_MATCH "GoLang"`, map[string]string{"title": "learning golang basics"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.input, err)
			}
			got := expr.Evaluate(tt.meta)
			if got != tt.want {
				t.Errorf("Parse(%q).Evaluate() = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParse_ComplexExpression(t *testing.T) {
	input := `age > 25 AND category IN ["tech", "science"] AND status != null`
	meta := map[string]string{"age": "30", "category": "tech", "status": "active"}

	expr, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", input, err)
	}

	if !expr.Evaluate(meta) {
		t.Errorf("Parse(%q).Evaluate() = false, want true", input)
	}

	// Remove status to make null check fail.
	delete(meta, "status")
	if expr.Evaluate(meta) {
		t.Errorf("Parse(%q).Evaluate() = true after removing status, want false", input)
	}
}

func TestParse_NegativeNumber(t *testing.T) {
	input := `temp > -10`
	meta := map[string]string{"temp": "5"}

	expr, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", input, err)
	}
	if !expr.Evaluate(meta) {
		t.Errorf("Parse(%q).Evaluate() = false, want true", input)
	}
}

func TestParse_EmptyExpression(t *testing.T) {
	_, err := Parse("")
	if err == nil {
		t.Error("Parse empty string should return error")
	}
	var parseErr *FilterParseError
	if !errors.As(err, &parseErr) {
		t.Errorf("expected FilterParseError, got %T", err)
	}
}

func TestParse_WhitespaceOnly(t *testing.T) {
	_, err := Parse("   ")
	if err == nil {
		t.Error("Parse whitespace should return error")
	}
}

func TestParse_UnclosedParenthesis(t *testing.T) {
	_, err := Parse(`(a > 1 AND b < 2`)
	if err == nil {
		t.Error("Parse unclosed paren should return error")
	}
	var parseErr *FilterParseError
	if !errors.As(err, &parseErr) {
		t.Errorf("expected FilterParseError, got %T", err)
	}
}

func TestParse_UnterminatedString(t *testing.T) {
	_, err := Parse(`name = "unterminated`)
	if err == nil {
		t.Error("Parse unterminated string should return error")
	}
	var parseErr *FilterParseError
	if !errors.As(err, &parseErr) {
		t.Errorf("expected FilterParseError, got %T", err)
	}
}

func TestParse_InvalidOperator(t *testing.T) {
	_, err := Parse(`name ~ "test"`)
	if err == nil {
		t.Error("Parse invalid operator should return error")
	}
}

func TestParse_MissingValue(t *testing.T) {
	_, err := Parse(`name =`)
	if err == nil {
		t.Error("Parse missing value should return error")
	}
}

func TestParse_TrailingTokens(t *testing.T) {
	_, err := Parse(`name = "test" extra`)
	if err == nil {
		t.Error("Parse with trailing tokens should return error")
	}
}

func TestParse_MissingFieldBeforeOperator(t *testing.T) {
	_, err := Parse(`= "test"`)
	if err == nil {
		t.Error("Parse missing field should return error")
	}
}

func TestParse_INMissingBracket(t *testing.T) {
	_, err := Parse(`name IN "test"`)
	if err == nil {
		t.Error("Parse IN without bracket should return error")
	}
}

func TestParse_INUnclosedBracket(t *testing.T) {
	_, err := Parse(`name IN ["a", "b"`)
	if err == nil {
		t.Error("Parse IN unclosed bracket should return error")
	}
}

func TestParse_EscapedQuotes(t *testing.T) {
	input := `name = "hello\"world"`
	expr, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", input, err)
	}
	meta := map[string]string{"name": `hello"world`}
	if !expr.Evaluate(meta) {
		t.Errorf("Parse(%q).Evaluate() = false, want true", input)
	}
}

func TestTokenize_UnexpectedCharacter(t *testing.T) {
	_, err := tokenize("field @ value")
	if err == nil {
		t.Error("tokenize with @ should return error")
	}
}

func TestTokenTypeName(t *testing.T) {
	if got := tokenTypeName(TokenField); got != "field" {
		t.Errorf("tokenTypeName(TokenField) = %q, want %q", got, "field")
	}
	if got := tokenTypeName(TokenType(99)); got != "unknown" {
		t.Errorf("tokenTypeName(99) = %q, want %q", got, "unknown")
	}
}

func TestParse_ANDWithIN(t *testing.T) {
	input := `age > 25 AND category IN ["tech", "science"]`
	meta := map[string]string{"age": "30", "category": "tech"}

	expr, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", input, err)
	}
	if !expr.Evaluate(meta) {
		t.Errorf("Parse(%q).Evaluate() = false, want true", input)
	}
}

func TestParse_ORPrecedence(t *testing.T) {
	// Without parens: AND has higher precedence than OR.
	// a = "1" OR b = "2" AND c = "3" -> a = "1" OR (b = "2" AND c = "3")
	input := `a = "1" OR b = "2" AND c = "3"`
	meta := map[string]string{"a": "1", "b": "99", "c": "99"}

	expr, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", input, err)
	}
	// a = "1" is true, so OR should return true regardless of AND.
	if !expr.Evaluate(meta) {
		t.Errorf("Parse(%q).Evaluate() = false, want true (OR has lower precedence)", input)
	}
}
