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

// PatternError is returned when a pattern cannot become a rule: its class is
// not one of the four, its expression does not compile, or the expression has
// no literal the sieve can search for.
type PatternError struct {
	// Class is the class the pattern declared.
	Class Class

	// Expr is the expression as written.
	Expr string

	// Problem says what is wrong with the pattern.
	Problem string

	// Err is the regexp compiler's error, nil for the other problems.
	Err error
}

func (e *PatternError) Error() string {
	msg := "labstrings: pattern " + strconv.Quote(e.Expr) + " (" + string(e.Class) + "): " + e.Problem
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *PatternError) Unwrap() error { return e.Err }

// PatternFileError is returned when the private pattern file named by
// PatternsEnv cannot be used: it is missing or unreadable, it is not a JSON
// list of {class, expr} entries, or one entry is a PatternError. Configured
// but unusable fails the gate: a private value the gate was told to look for
// and silently did not is exactly the leak it exists to stop.
type PatternFileError struct {
	// Path is the file, after resolution against the repository root.
	Path string

	// Err is the I/O, decoding or *PatternError failure.
	Err error
}

func (e *PatternFileError) Error() string {
	return "labstrings: cannot load patterns from " + e.Path + ": " + e.Err.Error()
}

func (e *PatternFileError) Unwrap() error { return e.Err }
