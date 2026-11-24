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

package sdlc

import (
	"sort"
	"strconv"
	"strings"
)

// =============================================================================
// Declared dependencies — what an issue says it is waiting for
// =============================================================================
//
// AN ISSUE DECLARES ITS DEPENDENCIES IN ITS OWN TEXT, because that is where the
// person filing it already writes them and where the person reading it already
// looks. Two directives, both line-leading and case-insensitive:
//
//	Blocked by: jeremyhahn/go-qrdb#123, #456
//	Requires release: jeremyhahn/go-qrdb@v0.9.5-alpha
//
// WHY TWO AND NOT ONE. "Blocked by #123" is satisfied when #123 closes. That is
// the right test for work in the same repository, and the WRONG one for a
// consumer that pins a version: the fix can be merged and the issue closed
// while every build still resolves the old tag, because module versions resolve
// from tags. So "requires release" is a separate declaration with a separate
// check, and an issue that needs both states both. Folding them into one
// directive would mean guessing which the author meant, and the cheap guess —
// closed is enough — is exactly the one that unblocks work too early.
//
// THE DIRECTIVES ARE READ FROM THE BODY AND FROM THE COMMENTS, in that order.
// A dependency discovered after filing is written as a comment, which is the
// only place it can be written by somebody without edit rights on the issue,
// and an author-only vocabulary would exclude the reviewer who found the
// ordering problem.
//
// THEY ARE ADDITIVE AND NEVER SUBTRACTIVE. Nothing in the text can retract a
// dependency, because a retraction would have to be ordered against the
// declaration and comment order is not a reliable clock. A dependency that no
// longer applies is cleared by the thing it names being satisfied, or by an
// edit to the text that declared it.

// DependencyKind classifies what a declared dependency waits on.
type DependencyKind string

const (
	// DependencyIssueClosed waits for another forge issue to close.
	DependencyIssueClosed DependencyKind = "issue_closed"

	// DependencyReleaseTag waits for a version tag to exist in another
	// repository — the published-artifact signal a merge does not give.
	DependencyReleaseTag DependencyKind = "release_tag"
)

// String returns the kind's name.
func (k DependencyKind) String() string { return string(k) }

// Dependency is one declaration read out of an issue.
type Dependency struct {
	// Kind decides how the dependency is checked.
	Kind DependencyKind

	// Repo is the repository the dependency lives in. Always populated: a
	// bare "#123" resolves against the declaring issue's own repository at
	// parse time, so nothing downstream has to know where the text came from.
	Repo RepoRef

	// Issue is the issue number, for DependencyIssueClosed.
	Issue int

	// Tag is the version tag, for DependencyReleaseTag.
	Tag string
}

// Ref renders the dependency as the reference a person and a check both read:
// owner/repo#number for an issue, owner/repo@tag for a release.
func (d Dependency) Ref() string {
	if d.Kind == DependencyReleaseTag {
		return d.Repo.String() + "@" + d.Tag
	}
	return IssueRefString(d.Repo, d.Issue)
}

// IssueRef renders the issue reference of a dependency that names an issue, and
// the empty string for one that does not.
//
// It exists so a graph walk can ask "does this dependency lead to another
// issue's declarations" without a type switch at every call: a release
// dependency is a leaf, and the empty answer says so.
func (d Dependency) IssueRef() string {
	if d.Kind != DependencyIssueClosed {
		return ""
	}
	return IssueRefString(d.Repo, d.Issue)
}

// Condition renders the dependency as the typed readmission condition a parked
// task carries, so a dependency and a park state the same fact in one
// vocabulary.
//
// ONE VOCABULARY IS THE POINT. UnblockSweeper already re-examines every blocked
// task on every pass and readmits the ones whose condition cleared; a
// dependency expressed as anything else would need a second sweep that did the
// same thing to the same records.
func (d Dependency) Condition() UnblockCondition {
	if d.Kind == DependencyReleaseTag {
		return UnblockCondition{Kind: UnblockReleaseTag, Subject: d.Repo.String(), Target: d.Tag}
	}
	return UnblockCondition{Kind: UnblockForgeIssueState, Subject: d.Ref(), Target: "closed"}
}

// Describe renders the dependency for a person.
func (d Dependency) Describe() string { return d.Condition().Describe() }

// =============================================================================
// Parsing
// =============================================================================

// dependencyDirectives maps a directive prefix to the parser for its operand
// list. Map-based so the vocabulary is data a completeness test can walk, and
// so adding a directive cannot mean editing a switch somebody else is also
// editing.
//
// Every key is lower-case and includes its colon: the colon is what separates a
// directive from ordinary prose beginning with the same words, and a match that
// ignored it would read "blocked by the missing config" as a declaration.
var dependencyDirectives = map[string]func(repo RepoRef, operands string) ([]Dependency, error){
	"blocked by:":       parseIssueOperands,
	"blocked-by:":       parseIssueOperands,
	"depends on:":       parseIssueOperands,
	"depends-on:":       parseIssueOperands,
	"requires release:": parseReleaseOperands,
	"requires-release:": parseReleaseOperands,
}

// DependencyDirectives returns the recognised directive prefixes, sorted, so an
// operator-facing message can list what the parser accepts rather than
// restating it and drifting.
func DependencyDirectives() []string {
	out := make([]string, 0, len(dependencyDirectives))
	for directive := range dependencyDirectives {
		out = append(out, directive)
	}
	sort.Strings(out)
	return out
}

// ParseDependencies reads every declared dependency out of an issue's body and
// its discussion, resolving bare references against repo.
//
// DUPLICATES COLLAPSE, ORDER IS PRESERVED. The same dependency declared in the
// body and repeated in a comment is one dependency; keeping both would check it
// twice per sweep and report it twice to whoever is reading the park.
//
// A MALFORMED DECLARATION IS AN ERROR, not a silently dropped line. A line that
// begins with a recognised directive is a statement of intent, and the failure
// mode of ignoring the ones we cannot parse is the worst available: work
// proceeds as though it had no dependencies, which is indistinguishable from an
// issue that declared none.
func ParseDependencies(repo RepoRef, body string, comments []ForgeComment) ([]Dependency, error) {
	if repo.Owner == "" || repo.Repo == "" {
		return nil, &InvalidInputError{
			Field:   "repo",
			Message: "a bare issue reference resolves against the declaring issue's own repository, so the parse must know it",
		}
	}
	texts := make([]string, 0, 1+len(comments))
	texts = append(texts, body)
	for _, comment := range comments {
		texts = append(texts, comment.Body)
	}

	var dependencies []Dependency
	seen := make(map[string]struct{})
	for _, text := range texts {
		found, err := parseDependencyText(repo, text)
		if err != nil {
			return nil, err
		}
		for _, dependency := range found {
			key := dependency.Kind.String() + " " + dependency.Ref()
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			dependencies = append(dependencies, dependency)
		}
	}
	return dependencies, nil
}

// parseDependencyText reads the directives of one body of text.
func parseDependencyText(repo RepoRef, text string) ([]Dependency, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	var dependencies []Dependency
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// A directive inside a markdown list item or a blockquote is still a
		// directive: "- Blocked by: #1" is how a person writing a list of them
		// spells it, and refusing the prefix would make the vocabulary depend
		// on markdown formatting nobody was told about.
		line = strings.TrimLeft(line, "->*+ \t")
		// Bold markers around the directive are stripped for the same reason:
		// "**Blocked by:** #1" is the shape a person emphasising it writes.
		line = strings.TrimPrefix(line, "**")
		lower := strings.ToLower(line)
		for directive, parse := range dependencyDirectives {
			operands, matched := matchDirective(lower, line, directive)
			if !matched {
				continue
			}
			found, err := parse(repo, operands)
			if err != nil {
				return nil, err
			}
			dependencies = append(dependencies, found...)
			break
		}
	}
	return dependencies, nil
}

// matchDirective reports whether line begins with directive and returns the
// operand text after it.
//
// The comparison is on the lower-cased copy and the operands are cut from the
// ORIGINAL, so a tag's case survives: "Requires release: owner/repo@V1.2.3" and
// "v1.2.3" are different tags on every forge, and a check fed the folded copy
// would ask for one that does not exist.
func matchDirective(lower, line, directive string) (string, bool) {
	if !strings.HasPrefix(lower, directive) {
		return "", false
	}
	operands := line[len(directive):]
	// A bold-closing marker belongs to the directive, not to the first operand.
	return strings.TrimPrefix(strings.TrimSpace(operands), "**"), true
}

// parseIssueOperands reads a comma-separated list of issue references.
func parseIssueOperands(repo RepoRef, operands string) ([]Dependency, error) {
	fields := splitOperands(operands)
	if len(fields) == 0 {
		return nil, &InvalidInputError{
			Field:   "dependency",
			Message: "a blocked-by declaration with no issue reference states nothing to wait for",
		}
	}
	out := make([]Dependency, 0, len(fields))
	for _, field := range fields {
		depRepo, number, err := parseDependencyIssueRef(repo, field)
		if err != nil {
			return nil, err
		}
		out = append(out, Dependency{Kind: DependencyIssueClosed, Repo: depRepo, Issue: number})
	}
	return out, nil
}

// parseDependencyIssueRef accepts "owner/repo#number" and the bare "#number",
// which resolves against the declaring issue's own repository.
func parseDependencyIssueRef(repo RepoRef, field string) (RepoRef, int, error) {
	trimmed := strings.TrimSpace(field)
	if strings.HasPrefix(trimmed, "#") {
		number, err := strconv.Atoi(trimmed[1:])
		if err != nil || number <= 0 {
			return RepoRef{}, 0, &InvalidInputError{
				Field:   "dependency",
				Message: "issue number in " + strconv.Quote(trimmed) + " is not a positive integer",
			}
		}
		return repo, number, nil
	}
	return ParseIssueRef(trimmed)
}

// parseReleaseOperands reads a comma-separated list of owner/repo@tag
// references.
//
// THE REPOSITORY IS REQUIRED HERE, unlike an issue reference. A release
// dependency on one's own repository would be a task waiting for its own work
// to be tagged, which no sweep can ever satisfy, so a bare "@v1.2.3" is refused
// rather than resolved into a deadlock.
func parseReleaseOperands(_ RepoRef, operands string) ([]Dependency, error) {
	fields := splitOperands(operands)
	if len(fields) == 0 {
		return nil, &InvalidInputError{
			Field:   "dependency",
			Message: "a requires-release declaration with no owner/repo@tag states nothing to wait for",
		}
	}
	out := make([]Dependency, 0, len(fields))
	for _, field := range fields {
		path, tag, ok := strings.Cut(strings.TrimSpace(field), "@")
		if !ok {
			return nil, &InvalidInputError{
				Field:   "dependency",
				Message: "expected owner/repo@tag, got " + strconv.Quote(field),
			}
		}
		owner, name, ok := strings.Cut(path, "/")
		if !ok || owner == "" || name == "" {
			return nil, &InvalidInputError{
				Field:   "dependency",
				Message: "a release dependency names the repository in full (owner/repo@tag), got " + strconv.Quote(field),
			}
		}
		tag = strings.TrimSpace(tag)
		if tag == "" {
			return nil, &InvalidInputError{
				Field:   "dependency",
				Message: "expected owner/repo@tag, got " + strconv.Quote(field),
			}
		}
		dependency := Dependency{Kind: DependencyReleaseTag, Repo: RepoRef{Owner: owner, Repo: name}, Tag: tag}
		// Validated through the condition it will be stored as, so a
		// declaration that cannot be encoded is refused where it is read
		// rather than where it is written hours later.
		if err := dependency.Condition().Validate(); err != nil {
			return nil, err
		}
		out = append(out, dependency)
	}
	return out, nil
}

// splitOperands splits a directive's operand list on commas and whitespace,
// dropping the empties a trailing separator leaves behind.
//
// BOTH SEPARATORS, because both are what people write: "#1, #2" and "#1 #2" are
// the same list, and accepting only one of them would silently read the other
// as a single malformed reference.
func splitOperands(operands string) []string {
	fields := strings.FieldsFunc(operands, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == ';'
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		// Trailing markdown emphasis and sentence punctuation are not part of
		// a reference. A trailing period is deliberately NOT stripped: a tag
		// may end in one and a reference that lost it would name a different
		// artifact.
		trimmed := strings.Trim(field, "`*_")
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}
