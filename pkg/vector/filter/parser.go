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
	"strings"
	"unicode"
)

// TokenType classifies lexer tokens.
type TokenType int

const (
	// TokenField is an unquoted identifier (metadata field name).
	TokenField TokenType = iota
	// TokenOperator is a comparison operator (=, !=, >, >=, <, <=, CONTAINS, STARTS_WITH).
	TokenOperator
	// TokenValue is a quoted string or numeric literal.
	TokenValue
	// TokenLogical is a logical keyword (AND, OR, NOT).
	TokenLogical
	// TokenLeftParen is an opening parenthesis.
	TokenLeftParen
	// TokenRightParen is a closing parenthesis.
	TokenRightParen
	// TokenLeftBracket is an opening square bracket.
	TokenLeftBracket
	// TokenRightBracket is a closing square bracket.
	TokenRightBracket
	// TokenComma is a comma separator.
	TokenComma
	// TokenNull is the null keyword.
	TokenNull
	// TokenEOF indicates end of input.
	TokenEOF
)

// Token represents a lexed token from a filter expression.
type Token struct {
	// Type is the token classification.
	Type TokenType
	// Value is the raw token text.
	Value string
	// Pos is the zero-based character position in the input.
	Pos int
}

// operatorKeywords maps text keywords to operator tokens.
var operatorKeywords = map[string]bool{
	"IN":          true,
	"CONTAINS":    true,
	"STARTS_WITH": true,
	"TEXT_MATCH":  true,
}

// logicalKeywords maps text keywords to logical tokens.
var logicalKeywords = map[string]bool{
	"AND": true,
	"OR":  true,
	"NOT": true,
}

// tokenize breaks a filter expression string into tokens.
func tokenize(input string) ([]Token, error) {
	tokens := make([]Token, 0, 16)
	i := 0
	for i < len(input) {
		ch := input[i]

		// Skip whitespace.
		if unicode.IsSpace(rune(ch)) {
			i++
			continue
		}

		// Parentheses.
		if ch == '(' {
			tokens = append(tokens, Token{Type: TokenLeftParen, Value: "(", Pos: i})
			i++
			continue
		}
		if ch == ')' {
			tokens = append(tokens, Token{Type: TokenRightParen, Value: ")", Pos: i})
			i++
			continue
		}

		// Brackets.
		if ch == '[' {
			tokens = append(tokens, Token{Type: TokenLeftBracket, Value: "[", Pos: i})
			i++
			continue
		}
		if ch == ']' {
			tokens = append(tokens, Token{Type: TokenRightBracket, Value: "]", Pos: i})
			i++
			continue
		}

		// Comma.
		if ch == ',' {
			tokens = append(tokens, Token{Type: TokenComma, Value: ",", Pos: i})
			i++
			continue
		}

		// Two-character operators: !=, >=, <=
		if i+1 < len(input) {
			two := input[i : i+2]
			if two == "!=" || two == ">=" || two == "<=" {
				tokens = append(tokens, Token{Type: TokenOperator, Value: two, Pos: i})
				i += 2
				continue
			}
		}

		// Single-character operators: =, >, <
		if ch == '=' || ch == '>' || ch == '<' {
			tokens = append(tokens, Token{Type: TokenOperator, Value: string(ch), Pos: i})
			i++
			continue
		}

		// Quoted string value.
		if ch == '"' {
			start := i
			i++ // skip opening quote
			var b strings.Builder
			for i < len(input) && input[i] != '"' {
				if input[i] == '\\' && i+1 < len(input) {
					i++ // skip escape
				}
				b.WriteByte(input[i])
				i++
			}
			if i >= len(input) {
				return nil, &FilterParseError{
					Expression: input,
					Position:   start,
					Message:    "unterminated string literal",
				}
			}
			i++ // skip closing quote
			tokens = append(tokens, Token{Type: TokenValue, Value: b.String(), Pos: start})
			continue
		}

		// Number (possibly negative or decimal).
		if ch == '-' || (ch >= '0' && ch <= '9') {
			start := i
			if ch == '-' {
				i++
			}
			for i < len(input) && ((input[i] >= '0' && input[i] <= '9') || input[i] == '.') {
				i++
			}
			tokens = append(tokens, Token{Type: TokenValue, Value: input[start:i], Pos: start})
			continue
		}

		// Identifier or keyword.
		if isIdentStart(ch) {
			start := i
			for i < len(input) && isIdentPart(input[i]) {
				i++
			}
			word := input[start:i]
			upper := strings.ToUpper(word)

			// Check for "NOT IN" as a two-word operator.
			if upper == "NOT" {
				// Peek ahead for "IN".
				j := i
				for j < len(input) && unicode.IsSpace(rune(input[j])) {
					j++
				}
				if j+2 <= len(input) && strings.ToUpper(input[j:j+2]) == "IN" {
					// Verify the "IN" is a complete word.
					afterIN := j + 2
					if afterIN >= len(input) || !isIdentPart(input[afterIN]) {
						tokens = append(tokens, Token{Type: TokenOperator, Value: "NOT IN", Pos: start})
						i = afterIN
						continue
					}
				}
			}

			switch {
			case upper == "NULL":
				tokens = append(tokens, Token{Type: TokenNull, Value: upper, Pos: start})
			case logicalKeywords[upper]:
				tokens = append(tokens, Token{Type: TokenLogical, Value: upper, Pos: start})
			case operatorKeywords[upper]:
				tokens = append(tokens, Token{Type: TokenOperator, Value: upper, Pos: start})
			default:
				tokens = append(tokens, Token{Type: TokenField, Value: word, Pos: start})
			}
			continue
		}

		return nil, &FilterParseError{
			Expression: input,
			Position:   i,
			Message:    "unexpected character: " + string(ch),
		}
	}

	tokens = append(tokens, Token{Type: TokenEOF, Value: "", Pos: len(input)})
	return tokens, nil
}

func isIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isIdentPart(ch byte) bool {
	return isIdentStart(ch) || (ch >= '0' && ch <= '9') || ch == '.'
}

// Parser is a recursive descent parser for filter expressions.
type Parser struct {
	tokens []Token
	pos    int
	input  string
}

// Parse parses a filter expression string into an Expression AST.
func Parse(input string) (Expression, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil, &FilterParseError{
			Expression: input,
			Position:   0,
			Message:    "empty expression",
		}
	}

	tokens, err := tokenize(trimmed)
	if err != nil {
		return nil, err
	}

	p := &Parser{
		tokens: tokens,
		pos:    0,
		input:  trimmed,
	}

	expr, err := p.parseExpression()
	if err != nil {
		return nil, err
	}

	// Ensure all tokens were consumed.
	if p.current().Type != TokenEOF {
		tok := p.current()
		return nil, &FilterParseError{
			Expression: trimmed,
			Position:   tok.Pos,
			Message:    "unexpected token: " + tok.Value,
		}
	}

	return expr, nil
}

// current returns the token at the current position.
func (p *Parser) current() Token {
	if p.pos >= len(p.tokens) {
		return Token{Type: TokenEOF, Pos: len(p.input)}
	}
	return p.tokens[p.pos]
}

// advance moves to the next token and returns the previous one.
func (p *Parser) advance() Token {
	tok := p.current()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return tok
}

// expect consumes a token of the given type or returns an error.
func (p *Parser) expect(tt TokenType) (Token, error) {
	tok := p.current()
	if tok.Type != tt {
		return tok, &FilterParseError{
			Expression: p.input,
			Position:   tok.Pos,
			Message:    "expected " + tokenTypeName(tt) + ", got " + tokenTypeName(tok.Type) + " (" + tok.Value + ")",
		}
	}
	p.advance()
	return tok, nil
}

// parseExpression parses an expression with OR as the lowest precedence.
func (p *Parser) parseExpression() (Expression, error) {
	return p.parseLogicalOr()
}

// parseLogicalOr handles OR expressions.
func (p *Parser) parseLogicalOr() (Expression, error) {
	left, err := p.parseLogicalAnd()
	if err != nil {
		return nil, err
	}

	for p.current().Type == TokenLogical && p.current().Value == "OR" {
		p.advance()
		right, err := p.parseLogicalAnd()
		if err != nil {
			return nil, err
		}
		left = &LogicalExpr{Op: LogicalOR, Left: left, Right: right}
	}

	return left, nil
}

// parseLogicalAnd handles AND expressions.
func (p *Parser) parseLogicalAnd() (Expression, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}

	for p.current().Type == TokenLogical && p.current().Value == "AND" {
		p.advance()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &LogicalExpr{Op: LogicalAND, Left: left, Right: right}
	}

	return left, nil
}

// parseUnary handles NOT expressions.
func (p *Parser) parseUnary() (Expression, error) {
	if p.current().Type == TokenLogical && p.current().Value == "NOT" {
		p.advance()
		operand, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		return &LogicalExpr{Op: LogicalNOT, Left: operand}, nil
	}
	return p.parsePrimary()
}

// parsePrimary handles parenthesized expressions and comparison expressions.
func (p *Parser) parsePrimary() (Expression, error) {
	// Parenthesized sub-expression.
	if p.current().Type == TokenLeftParen {
		p.advance()
		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TokenRightParen); err != nil {
			return nil, err
		}
		return expr, nil
	}

	return p.parseComparison()
}

// parseComparison handles field-operator-value expressions.
func (p *Parser) parseComparison() (Expression, error) {
	fieldTok, err := p.expect(TokenField)
	if err != nil {
		return nil, err
	}
	field := fieldTok.Value

	opTok := p.current()

	// Handle IN / NOT IN as special cases.
	if opTok.Type == TokenOperator && (opTok.Value == "IN" || opTok.Value == "NOT IN") {
		negate := opTok.Value == "NOT IN"
		p.advance()
		values, err := p.parseValueList()
		if err != nil {
			return nil, err
		}
		return &InExpr{Field: field, Values: values, Negate: negate}, nil
	}

	// Handle CONTAINS, STARTS_WITH, TEXT_MATCH.
	if opTok.Type == TokenOperator {
		op, ok := textOperatorMap[opTok.Value]
		if ok {
			p.advance()
			valueTok, err := p.expect(TokenValue)
			if err != nil {
				return nil, err
			}
			return &TermExpr{Field: field, Op: op, Value: valueTok.Value}, nil
		}
	}

	// Handle standard comparison operators: =, !=, >, >=, <, <=
	if opTok.Type != TokenOperator {
		return nil, &FilterParseError{
			Expression: p.input,
			Position:   opTok.Pos,
			Message:    "expected operator, got " + tokenTypeName(opTok.Type) + " (" + opTok.Value + ")",
		}
	}
	p.advance()

	// Handle null comparisons: field = null, field != null
	if p.current().Type == TokenNull {
		p.advance()
		isNull := opTok.Value == "="
		return &NullExpr{Field: field, IsNull: isNull}, nil
	}

	valueTok, err := p.expect(TokenValue)
	if err != nil {
		return nil, err
	}

	op, ok := comparisonOperatorMap[opTok.Value]
	if !ok {
		return nil, &FilterParseError{
			Expression: p.input,
			Position:   opTok.Pos,
			Message:    "unknown operator: " + opTok.Value,
		}
	}

	return &TermExpr{Field: field, Op: op, Value: valueTok.Value}, nil
}

// parseValueList parses [value, value, ...].
func (p *Parser) parseValueList() ([]string, error) {
	if _, err := p.expect(TokenLeftBracket); err != nil {
		return nil, err
	}

	var values []string
	for {
		valueTok, err := p.expect(TokenValue)
		if err != nil {
			return nil, err
		}
		values = append(values, valueTok.Value)

		if p.current().Type == TokenComma {
			p.advance()
			continue
		}
		break
	}

	if _, err := p.expect(TokenRightBracket); err != nil {
		return nil, err
	}
	return values, nil
}

// comparisonOperatorMap maps operator tokens to FilterOp values.
var comparisonOperatorMap = map[string]FilterOp{
	"=":  OpEqual,
	"!=": OpNotEqual,
	">":  OpGreaterThan,
	">=": OpGreaterEqual,
	"<":  OpLessThan,
	"<=": OpLessEqual,
}

// textOperatorMap maps keyword operators to FilterOp values.
var textOperatorMap = map[string]FilterOp{
	"CONTAINS":    OpContains,
	"STARTS_WITH": OpPrefixMatch,
	"TEXT_MATCH":  OpTextMatch,
}

// tokenTypeName returns a display name for a token type.
func tokenTypeName(tt TokenType) string {
	name, ok := tokenTypeNames[tt]
	if !ok {
		return "unknown"
	}
	return name
}

// tokenTypeNames maps token types to display names.
var tokenTypeNames = map[TokenType]string{
	TokenField:        "field",
	TokenOperator:     "operator",
	TokenValue:        "value",
	TokenLogical:      "logical",
	TokenLeftParen:    "(",
	TokenRightParen:   ")",
	TokenLeftBracket:  "[",
	TokenRightBracket: "]",
	TokenComma:        ",",
	TokenNull:         "null",
	TokenEOF:          "EOF",
}
