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

import "strconv"

// The gate distinguishes "this tree has a violation" from "this gate could not
// run", so each way it can fail to run is its own type carrying what a caller
// needs to fix it. A finding is a value, never an error.

// ListError is returned when the tracked-file enumeration fails. The gate reads
// the git index because that is the ship manifest, so a tree with no index — an
// extracted tarball, a module cache entry — cannot be gated, and says so.
type ListError struct {
	// Root is the directory git was asked about.
	Root string

	// Stderr is what git said, which is the only thing that distinguishes
	// "not a work tree" from "git is not installed".
	Stderr string

	// Err is the underlying exec failure.
	Err error
}

func (e *ListError) Error() string {
	msg := "labstrings: cannot list tracked files in " + e.Root + ": " + e.Err.Error()
	if e.Stderr != "" {
		msg += ": " + e.Stderr
	}
	return msg
}

func (e *ListError) Unwrap() error { return e.Err }

// ReadError is returned when a tracked file exists but cannot be read. Absent,
// oversized and binary files are skipped rather than reported, so this only
// covers a genuine I/O failure.
type ReadError struct {
	// Path is the file that could not be read.
	Path string

	// Err is the underlying I/O failure.
	Err error
}

func (e *ReadError) Error() string {
	return "labstrings: cannot read " + e.Path + ": " + e.Err.Error()
}

func (e *ReadError) Unwrap() error { return e.Err }

// AllowRuleError is returned when an allow-list entry is not usable. An
// allow-list is the gate's own configuration: a malformed or unexplained entry
// silently accepts whatever it covers, so it fails the gate instead.
type AllowRuleError struct {
	// Index is the entry's position in the list.
	Index int

	// Path is the entry's path pattern, empty if that is the defect.
	Path string

	// Problem says what is wrong with the entry.
	Problem string
}

func (e *AllowRuleError) Error() string {
	return "labstrings: allow-list entry " + strconv.Itoa(e.Index) + " (" + e.Path + "): " + e.Problem
}

// ReportError is returned when the report cannot be written. The report is the
// gate's product, so a silent write failure would turn a failing gate into a
// passing one.
type ReportError struct {
	// Err is the underlying write failure.
	Err error
}

func (e *ReportError) Error() string {
	return "labstrings: cannot write report: " + e.Err.Error()
}

func (e *ReportError) Unwrap() error { return e.Err }

// PatternProblem says what is wrong with one line of the pattern text. It is a
// fixed vocabulary on purpose: the line is private, so an error may say what
// kind of defect it is and where, and nothing about what the line says.
type PatternProblem string

const (
	// PatternNotClassTabRegex is a line with no tab separating class from regex.
	PatternNotClassTabRegex PatternProblem = "is not class<TAB>regex"

	// PatternUnknownClass is a line whose class is not one the gate reports.
	PatternUnknownClass PatternProblem = "has an unknown class"

	// PatternEmptyRegex is a line with nothing after the tab.
	PatternEmptyRegex PatternProblem = "has an empty regex"

	// PatternBadRegex is a line whose regex does not compile.
	PatternBadRegex PatternProblem = "has a regex that does not compile"
)

// PatternError is returned when a line of the pattern text cannot be used. One
// unusable rule fails the whole set: a gate that silently dropped it would pass
// every tree that rule was written for. The error names the line by number
// only, never its text, because the text is the private value and an error
// message ends up in a CI log.
type PatternError struct {
	// Line is the 1-based line number in the pattern text.
	Line int

	// Problem is what is wrong with the line.
	Problem PatternProblem

	// Code is the regexp/syntax error code for PatternBadRegex, such as
	// "missing closing )". It describes the defect without quoting it.
	Code string
}

func (e *PatternError) Error() string {
	msg := "labstrings: pattern line " + strconv.Itoa(e.Line) + " " + string(e.Problem)
	if e.Code != "" {
		msg += " (" + e.Code + ")"
	}
	return msg
}

// NoPatternsError is returned when the gate is asked to scan with no rules. The
// rules are not in this repository, so a scan without them sees nothing, and
// reporting that as a clean tree would be the blindfold this package exists to
// prevent.
type NoPatternsError struct {
	// Variable is the environment variable the rules were read from, empty
	// when the caller passed an empty set itself.
	Variable string
}

func (e *NoPatternsError) Error() string {
	if e.Variable == "" {
		return "labstrings: no patterns to scan with"
	}
	return "labstrings: no patterns to scan with: " + e.Variable +
		" is unset, empty or holds no class<TAB>regex line"
}
