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
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"regexp/syntax"
	"slices"
	"strings"
)

// PatternsEnv names the environment variable that points at the private
// pattern file. Unset or empty, the gate applies DefaultPatterns alone. A
// relative path is resolved against the repository root being scanned, so
// the same value works from the Makefile, from `go test` in any package
// directory and inside the CI toolchain container.
const PatternsEnv = "QUICBAAS_LABSTRINGS_PATTERNS"

// PatternFile is one entry of the private pattern file, which is a JSON list:
//
//	[
//	  {"class": "lab-address", "expr": "\\b203\\.0\\.113\\.\\d{1,3}\\b"},
//	  {"class": "lab-host",    "expr": "(?i)\\bcore-\\d{2}\\b"}
//	]
type PatternFile struct {
	// Class is one of the four Class values.
	Class Class `json:"class"`

	// Expr is a Go regexp (RE2 syntax) matching the private value.
	Expr string `json:"expr"`
}

// classGuidance is what an engineer who tripped a rule of each class should do
// instead. Its keys are also the set of classes the gate knows.
var classGuidance = map[Class]string{
	ClassLabAddress:   "use an RFC 5737 documentation address (192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24)",
	ClassLabHost:      "name the role, not the machine: agent-01, gpu-host.internal, quicbaas.example.com",
	ClassOwnerPath:    "make the path relative to the repository root, or use /var/lib, /srv, /home/agent",
	ClassOwnerAccount: "name a role, not a person: operator, agent, or the project's own module path",
}

// placeholderHomes are the home directories this tree names on purpose: the
// service accounts its container images create and the neutral accounts its
// fixtures use. None of them identifies a person or a machine.
var placeholderHomes = []string{
	"/home/agent", "/home/app", "/home/deploy", "/home/dev", "/home/git",
	"/home/node", "/home/operator", "/home/quicbaas", "/home/softhsm",
	"/home/u", "/home/user",
}

// DefaultPatterns is the public rule set: leaks that look the same in every
// fork, so it names nothing about any one network. A deployment's own subnet,
// hostnames, account and mount points go in the private pattern file named by
// PatternsEnv, never here: a published list of private values is itself the
// leak.
var DefaultPatterns = []Pattern{
	// A home directory under /home. Only a developer's checkout puts one in
	// a tracked file, and it names the account. \B keeps a URL segment such
	// as `example.org/iso/home/store` out; the service and fixture accounts
	// in placeholderHomes are excepted.
	mustPattern(ClassOwnerPath, `\B/home/[a-z_][a-z0-9_-]*`, placeholderHomes...),

	// A macOS home directory. Nothing this server-side tree ships runs from
	// one, so any /Users/<name>/ path came from a workstation. The trailing
	// slash keeps a `/Users/i` regexp literal out.
	mustPattern(ClassOwnerPath, `\B/Users/[A-Za-z0-9_.-]+/`),

	// An address in 192.168/16, the range home and lab networks default
	// to. Fixtures use RFC 5737 addresses instead, so a hit here is a real
	// network. 10/8 and 172.16/12 are not covered: container, cluster and
	// cloud defaults use them throughout the tree as addresses no reader
	// ever dials.
	mustPattern(ClassLabAddress, `\b192\.168\.\d{1,3}\.\d{1,3}\b`),

	// A host under .lan, the suffix home routers hand out, or under
	// home.arpa, the RFC 8375 home-network domain. Neither resolves for a
	// public reader.
	mustPattern(ClassLabHost, `(?i)\b[a-z0-9-]+\.lan\b`),
	mustPattern(ClassLabHost, `(?i)\b[a-z0-9-]+\.home\.arpa\b`),
}

// mustPattern builds a built-in pattern. A built-in that does not compile is a
// programming error in this file, so it panics at package initialisation, where
// every test run sees it.
func mustPattern(class Class, expr string, except ...string) Pattern {
	p, err := NewPattern(class, expr)
	if err != nil {
		panic(err)
	}
	p.Except = except
	return p
}

// NewPattern compiles expr into a rule of class. The sieve needle is derived
// from the expression itself, so it is implied by every match by construction:
// the longest literal the expression's top-level sequence requires. An
// expression with no such literal — a bare alternation, a pure character class
// — is refused, because the sieve could not rule a single file out with it.
// Split an alternation into one pattern per branch.
func NewPattern(class Class, expr string) (Pattern, error) {
	guidance, known := classGuidance[class]
	if !known {
		return Pattern{}, &PatternError{Class: class, Expr: expr, Problem: "unknown class"}
	}
	compiled, err := regexp.Compile(expr)
	if err != nil {
		return Pattern{}, &PatternError{Class: class, Expr: expr, Problem: "invalid regexp", Err: err}
	}
	needle, err := sieveNeedle(expr)
	if err != nil {
		return Pattern{}, &PatternError{Class: class, Expr: expr, Problem: "invalid regexp", Err: err}
	}
	if needle == "" {
		return Pattern{}, &PatternError{Class: class, Expr: expr, Problem: "no literal text that every match contains"}
	}
	return Pattern{Class: class, Expr: compiled, Needle: needle, Guidance: guidance}, nil
}

// sieveNeedle returns the longest literal in expr's top-level sequence,
// lowercased, or "" if there is none. Every match of expr contains that
// literal, because a top-level sequence matches only when each of its parts
// does.
func sieveNeedle(expr string) (string, error) {
	re, err := syntax.Parse(expr, syntax.Perl)
	if err != nil {
		return "", err
	}
	re = unwrapCapture(re.Simplify())
	parts := []*syntax.Regexp{re}
	if re.Op == syntax.OpConcat {
		parts = re.Sub
	}
	var longest []rune
	for _, part := range parts {
		part = unwrapCapture(part)
		if part.Op == syntax.OpLiteral && len(part.Rune) > len(longest) {
			longest = part.Rune
		}
	}
	return strings.ToLower(string(longest)), nil
}

// unwrapCapture strips capturing groups, which match exactly what they wrap.
func unwrapCapture(re *syntax.Regexp) *syntax.Regexp {
	for re.Op == syntax.OpCapture {
		re = re.Sub[0]
	}
	return re
}

// LoadPatterns reads a private pattern file (see PatternFile) and compiles every
// entry. Unknown fields are refused, so a misspelt key fails the gate instead of
// leaving a value unguarded.
func LoadPatterns(path string) ([]Pattern, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, &PatternFileError{Path: path, Err: err}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var entries []PatternFile
	if err := decoder.Decode(&entries); err != nil {
		return nil, &PatternFileError{Path: path, Err: err}
	}
	patterns := make([]Pattern, 0, len(entries))
	for _, entry := range entries {
		p, err := NewPattern(entry.Class, entry.Expr)
		if err != nil {
			return nil, &PatternFileError{Path: path, Err: err}
		}
		patterns = append(patterns, p)
	}
	return patterns, nil
}

// ConfiguredPatterns is the rule set the gate applies to the repository at root:
// DefaultPatterns, plus the private file PatternsEnv names when it is set.
func ConfiguredPatterns(root string) ([]Pattern, error) {
	path := os.Getenv(PatternsEnv)
	if path == "" {
		return DefaultPatterns, nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	private, err := LoadPatterns(path)
	if err != nil {
		return nil, err
	}
	return append(slices.Clip(DefaultPatterns), private...), nil
}
