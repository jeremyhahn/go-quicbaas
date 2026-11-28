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

package sdlc

import (
	"strconv"
	"strings"
)

// =============================================================================
// Lane brief — what a worker is handed besides the issue contract
// =============================================================================
//
// A lane brief tells a worker its turn budget, objective, scope, when it is
// done, which files it edits and reads first, the edit expected of it and
// the acceptance line its work is checked against. Every brief traces to a
// SPEC row or to the owner's words: a brief that traces to neither is
// refused, not rendered.
//
// RenderLaneBrief writes a brief from a LaneBriefSpec; ValidateLaneBrief
// checks any brief text, rendered here or written by hand, for the lines a
// worker cannot do without.

// Lane brief labels. Each is matched at the start of a line, and the first
// occurrence is the one read.
const (
	briefTitlePrefix       = "# Lane brief: "
	briefTurnBudget        = "Turn budget"
	briefFileMapHeading    = "## File map"
	briefFilesEdited       = "Files this lane edits"
	briefFilesToRead       = "Files to read first"
	briefExpectedEdit      = "Expected edit"
	briefAcceptance        = "Acceptance"
	briefResumeReason      = "Resume reason"
	briefRoute             = "Route"
	briefSpecRow           = "SPEC row"
	briefOwnerWords        = "Owner words"
	briefWrapUpMargin      = 10
	briefFileListSeparator = "; "
)

// LaneBriefSpec is what a lane brief is rendered from.
type LaneBriefSpec struct {
	// Objective is the one-line goal; it is the brief's title.
	Objective string

	// Context is free text on why the work is needed; it may span lines.
	Context string

	// Scope and DoneWhen are rendered one bullet per entry.
	Scope    []string
	DoneWhen []string

	// Route names how the work lands (e.g. the branch or train it rides).
	Route string

	// SpecRow and OwnerWords trace the brief. At least one must be set.
	SpecRow    string
	OwnerWords string

	// TurnBudget is the worker's turn budget. The wrap-up turn is ten
	// before it; a budget of ten or less has no wrap-up clause. A budget
	// of zero or less renders no number and is refused.
	TurnBudget int

	// FilesEdited and FilesToRead fill the file map.
	FilesEdited []string
	FilesToRead []string

	// ExpectedEdit and Acceptance are single lines.
	ExpectedEdit string
	Acceptance   string

	// ResumeReason is set on a resume, and only then rendered.
	ResumeReason string
}

// RenderLaneBrief renders spec as a lane brief.
//
// It refuses, with *UntracedBriefError, a spec whose SpecRow and OwnerWords
// are both blank. Otherwise it renders, in order: the title
// "# Lane brief: <Objective>", the turn budget line, the Objective,
// Context, Scope and Done when sections, the Route line, the SPEC row and/or
// Owner words lines, the File map section, the Expected edit and Acceptance
// lines, and the Resume reason line when ResumeReason is set. Single-line
// fields have their whitespace, newlines included, collapsed to single
// spaces. The rendered text is then checked by ValidateLaneBrief (as a
// resume when ResumeReason is set), and its error returned: a spec missing
// a required field is refused, not rendered.
func RenderLaneBrief(spec LaneBriefSpec) (string, error) {
	specRow, ownerWords := oneLine(spec.SpecRow), oneLine(spec.OwnerWords)
	objective := oneLine(spec.Objective)
	if specRow == "" && ownerWords == "" {
		return "", &UntracedBriefError{Objective: objective}
	}
	resumeReason := oneLine(spec.ResumeReason)

	var b strings.Builder
	line := func(s string) { b.WriteString(s); b.WriteByte('\n') }
	labelled := func(label, value string) { line(strings.TrimRight(label+": "+value, " ")) }
	section := func(heading string) { line(""); line("## " + heading); line("") }

	line(briefTitlePrefix + objective)
	line("")
	labelled(briefTurnBudget, turnBudgetValue(spec.TurnBudget))

	section("Objective")
	line(objective)

	section("Context")
	line(strings.TrimSpace(spec.Context))

	section("Scope")
	for _, s := range nonBlankLines(spec.Scope) {
		line("- " + s)
	}

	section("Done when")
	for _, s := range nonBlankLines(spec.DoneWhen) {
		line("- " + s)
	}

	line("")
	if route := oneLine(spec.Route); route != "" {
		labelled(briefRoute, route)
	}
	if specRow != "" {
		labelled(briefSpecRow, specRow)
	}
	if ownerWords != "" {
		labelled(briefOwnerWords, ownerWords)
	}

	line("")
	line(briefFileMapHeading)
	line("")
	labelled(briefFilesEdited, strings.Join(nonBlankLines(spec.FilesEdited), briefFileListSeparator))
	labelled(briefFilesToRead, strings.Join(nonBlankLines(spec.FilesToRead), briefFileListSeparator))

	line("")
	labelled(briefExpectedEdit, oneLine(spec.ExpectedEdit))
	labelled(briefAcceptance, oneLine(spec.Acceptance))
	if resumeReason != "" {
		labelled(briefResumeReason, resumeReason)
	}

	text := b.String()
	if err := ValidateLaneBrief(text, resumeReason != ""); err != nil {
		return "", err
	}
	return text, nil
}

// ValidateLaneBrief checks text for the lines a lane brief requires and
// returns *MissingBriefLineError naming every gap, or nil when there is none.
//
// The gaps are listed in this order:
//   - "Turn budget": the "Turn budget:" line is missing, its value does not
//     start with a number, or the line carries a parenthetical (any "(").
//   - "## File map": the section heading is missing.
//   - "Files this lane edits" and "Files to read first": the line is
//     missing or not filled.
//   - "Expected edit" and "Acceptance": the line is missing or not filled.
//   - "Resume reason", only when resume is true: missing or not filled.
//
// A label is matched at the start of a line, and only its first occurrence
// is read. A value is filled when, trimmed, it is not empty and does not
// start with "<" (a template placeholder such as "<one-line objective>").
func ValidateLaneBrief(text string, resume bool) error {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var missing []string

	if raw, value, ok := briefLine(lines, briefTurnBudget); !ok ||
		value == "" || value[0] < '0' || value[0] > '9' || strings.Contains(raw, "(") {
		missing = append(missing, briefTurnBudget)
	}
	if !hasBriefHeading(lines, briefFileMapHeading) {
		missing = append(missing, briefFileMapHeading)
	}
	required := []string{briefFilesEdited, briefFilesToRead, briefExpectedEdit, briefAcceptance}
	if resume {
		required = append(required, briefResumeReason)
	}
	for _, label := range required {
		if _, value, ok := briefLine(lines, label); !ok || !briefValueFilled(value) {
			missing = append(missing, label)
		}
	}

	if len(missing) == 0 {
		return nil
	}
	return &MissingBriefLineError{Missing: missing}
}

// briefLine returns the first line starting with "<label>:" and its trimmed
// value after the colon.
func briefLine(lines []string, label string) (raw, value string, ok bool) {
	prefix := label + ":"
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return l, strings.TrimSpace(l[len(prefix):]), true
		}
	}
	return "", "", false
}

// hasBriefHeading reports whether a line is exactly heading, trailing
// whitespace aside.
func hasBriefHeading(lines []string, heading string) bool {
	for _, l := range lines {
		if strings.TrimRight(l, " \t") == heading {
			return true
		}
	}
	return false
}

// briefValueFilled reports whether a trimmed value is neither empty nor a
// template placeholder.
func briefValueFilled(value string) bool {
	return value != "" && !strings.HasPrefix(value, "<")
}

// turnBudgetValue renders the turn budget value: "<n>." with a wrap-up
// clause ten turns before it when n is above ten, and nothing when n is not
// positive, so the line is refused.
func turnBudgetValue(n int) string {
	if n <= 0 {
		return ""
	}
	v := strconv.Itoa(n) + "."
	if n > briefWrapUpMargin {
		v += " Wrap-up at turn " + strconv.Itoa(n-briefWrapUpMargin) + "."
	}
	return v
}

// nonBlankLines returns entries collapsed to one line each, blanks dropped.
func nonBlankLines(entries []string) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e = oneLine(e); e != "" {
			out = append(out, e)
		}
	}
	return out
}
