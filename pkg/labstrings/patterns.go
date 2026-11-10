// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package labstrings

import (
	"bytes"
	"errors"
	"os"
	"regexp"
	"regexp/syntax"
	"strings"
	"unicode"
	"unicode/utf8"
)

// PatternsEnv is the environment variable the gate reads its rules from. It
// holds the same text the CI step "Releasability (private patterns)" reads, so
// one private file drives both.
const PatternsEnv = "RELEASABILITY_PATTERNS"

// foldSuffix on a class name asks for a case-insensitive rule.
const foldSuffix = "/i"

// foldFlag is the regexp prefix a foldSuffix rule is compiled with.
const foldFlag = "(?i)"

// Pattern is one rule the scanner applies to every line.
type Pattern struct {
	// Class says what a match leaks.
	Class Class

	// Expr matches the offending text. It matches the value, not the whole
	// line, so the report can quote exactly what has to change.
	Expr *regexp.Regexp

	// Guidance is what the engineer who tripped this rule should do instead.
	Guidance string

	// sieve is the set of literals at least one of which every match of
	// Expr contains; see requiredLiterals. Nil means no such set could be
	// derived, and the rule is applied to every file.
	sieve []needle
}

// ParsePatterns reads rules in the pattern-file format the CI step uses:
//
//   - one rule per line, `class<TAB>regex`, split at the first tab;
//   - a blank line, or one whose first non-space character is `#`, is skipped;
//   - a trailing carriage return is stripped, so a file saved on Windows reads
//     the same;
//   - class is lab-address, lab-host, owner-path or owner-account, optionally
//     followed by `/i` to make the rule case-insensitive;
//   - regex is Go regexp syntax, which for the expressions this file holds
//     reads the same as the `grep -E` the CI step uses.
//
// A line that cannot be used fails the whole parse with a *PatternError naming
// the line by number. The error never quotes the line: the text is private, and
// an error message ends up in a CI log.
//
// Rules keep the order they have in the text, except that every owner-account
// rule moves after all the others. The scanner reports a value once, by the
// first rule that covers it, and an account name is the least specific thing a
// rule can find: the account inside an owner path is reported as the path.
func ParsePatterns(text string) ([]Pattern, error) {
	var specific, accounts []Pattern
	for i, line := range strings.Split(text, "\n") {
		p, ok, err := parsePatternLine(i+1, strings.TrimSuffix(line, "\r"))
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if p.Class == ClassOwnerAccount {
			accounts = append(accounts, p)
			continue
		}
		specific = append(specific, p)
	}
	return append(specific, accounts...), nil
}

// PatternsFromEnv parses PatternsEnv. An unset or empty variable, or one that
// holds only comments, is a *NoPatternsError: the gate has nothing to look for,
// and that is not the same as finding nothing.
func PatternsFromEnv() ([]Pattern, error) {
	patterns, err := ParsePatterns(os.Getenv(PatternsEnv))
	if err != nil {
		return nil, err
	}
	if len(patterns) == 0 {
		return nil, &NoPatternsError{Variable: PatternsEnv}
	}
	return patterns, nil
}

// parsePatternLine parses one line. The bool is false for a line that is
// skipped rather than parsed.
func parsePatternLine(number int, line string) (Pattern, bool, error) {
	trimmed := strings.TrimLeftFunc(line, unicode.IsSpace)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return Pattern{}, false, nil
	}
	field, expr, ok := strings.Cut(line, "\t")
	if !ok {
		return Pattern{}, false, &PatternError{Line: number, Problem: PatternNotClassTabRegex}
	}
	name, fold := strings.CutSuffix(field, foldSuffix)
	class := Class(name)
	guidance, known := classGuidance[class]
	if !known {
		return Pattern{}, false, &PatternError{Line: number, Problem: PatternUnknownClass}
	}
	if expr == "" {
		return Pattern{}, false, &PatternError{Line: number, Problem: PatternEmptyRegex}
	}
	if fold {
		expr = foldFlag + expr
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return Pattern{}, false, &PatternError{Line: number, Problem: PatternBadRegex, Code: syntaxCode(err)}
	}
	return Pattern{Class: class, Expr: re, Guidance: guidance, sieve: deriveSieve(expr)}, true, nil
}

// syntaxCode is the regexp/syntax error code behind a compile failure. The code
// is a fixed description such as "missing closing )"; the error's other field
// quotes the expression, which is exactly what must not be printed.
func syntaxCode(err error) string {
	var syntaxErr *syntax.Error
	if errors.As(err, &syntaxErr) {
		return string(syntaxErr.Code)
	}
	return ""
}

// needle is one literal the sieve searches for.
type needle struct {
	// text is the literal; lowercase when folded is set.
	text []byte

	// folded says text is to be found in lowercased content, because the
	// rule it came from ignores case.
	folded bool
}

// deriveSieve computes a pattern's sieve from its expression. The sieve exists
// because almost no file in this tree contains a lab value, and 30 MB of
// tracked content is read on every scan: settling "there is nothing here" with
// a substring search runs at memory bandwidth, where the expressions run at a
// few megabytes a second.
//
// The rules are no longer written down here, so their sieve cannot be either.
// It is derived instead, and conservatively: a sieve that is wrong would make
// the gate silently blind, so where no literal is provably required the rule
// gets no sieve and is applied to every file. Slow is acceptable; blind is not.
func deriveSieve(expr string) []needle {
	re, err := syntax.Parse(expr, syntax.Perl)
	if err != nil {
		return nil
	}
	return requiredLiterals(re)
}

// requiredLiterals returns literals at least one of which every match of re
// contains, or nil when it cannot prove any. The cases are the regexp syntax
// tree's own fixed set of operators, so this is a switch rather than a table:
// a literal is required; a concatenation requires whatever any one part
// requires (the most selective part is kept); an alternation requires one of
// its branches' sets, and only when every branch has one; a capture, a `+` and
// a repeat of at least one require what their operand does. Everything else
// (classes, `*`, `?`, assertions) requires nothing.
func requiredLiterals(re *syntax.Regexp) []needle {
	switch re.Op {
	case syntax.OpLiteral:
		n, ok := literalNeedle(re.Rune, re.Flags&syntax.FoldCase != 0)
		if !ok {
			return nil
		}
		return []needle{n}
	case syntax.OpCapture, syntax.OpPlus:
		return requiredLiterals(re.Sub[0])
	case syntax.OpRepeat:
		if re.Min < 1 {
			return nil
		}
		return requiredLiterals(re.Sub[0])
	case syntax.OpConcat:
		var best []needle
		for _, sub := range re.Sub {
			candidate := requiredLiterals(sub)
			if candidate != nil && (best == nil || shortestNeedle(candidate) > shortestNeedle(best)) {
				best = candidate
			}
		}
		return best
	case syntax.OpAlternate:
		var oneOf []needle
		for _, sub := range re.Sub {
			branch := requiredLiterals(sub)
			if branch == nil {
				return nil
			}
			oneOf = append(oneOf, branch...)
		}
		return oneOf
	}
	return nil
}

// shortestNeedle is the length of the least selective literal in a set: a set
// is only as cheap to rule out as its shortest member.
func shortestNeedle(set []needle) int {
	shortest := len(set[0].text)
	for _, n := range set[1:] {
		shortest = min(shortest, len(n.text))
	}
	return shortest
}

// literalNeedle turns a literal node into a needle. A case-sensitive literal is
// searched for as written. A case-insensitive one is searched for lowercased in
// lowercased content, which is exact only for letters whose every case variant
// is ASCII: Go's case folding also equates some ASCII letters with non-ASCII
// ones (a Kelvin sign with k, a long s with s), and lowercasing would not bring
// those together. So the needle is the longest run of the literal that folds
// within ASCII; a substring of a required literal is itself required.
func literalNeedle(runes []rune, fold bool) (needle, bool) {
	if !fold {
		text := string(runes)
		return needle{text: []byte(text)}, text != ""
	}
	var run strings.Builder
	best := ""
	flush := func() {
		if run.Len() > len(best) {
			best = run.String()
		}
		run.Reset()
	}
	for _, r := range runes {
		if !foldsWithinASCII(r) {
			flush()
			continue
		}
		run.WriteRune(unicode.ToLower(r))
	}
	flush()
	return needle{text: []byte(best), folded: true}, best != ""
}

// foldsWithinASCII reports whether r and every rune case folding equates it
// with are ASCII.
func foldsWithinASCII(r rune) bool {
	if r >= utf8.RuneSelf {
		return false
	}
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		if f >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// candidates is the patterns that could match somewhere in content and whose
// class the file does not accept. Content is lowercased at most once, and only
// if a case-insensitive needle needs it.
func candidates(content []byte, patterns []Pattern, accepted map[Class]bool) []Pattern {
	var live []Pattern
	var folded []byte
	haveFolded := false
	for _, p := range patterns {
		if accepted[p.Class] {
			continue
		}
		if p.sieve == nil {
			live = append(live, p)
			continue
		}
		for _, n := range p.sieve {
			haystack := content
			if n.folded {
				if !haveFolded {
					folded, haveFolded = bytes.ToLower(content), true
				}
				haystack = folded
			}
			if bytes.Contains(haystack, n.text) {
				live = append(live, p)
				break
			}
		}
	}
	return live
}
