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

// Package gate carries what every repository gate under scripts/ needs and
// none of them should own a second copy of: the exit-code contract CI reads,
// the typed errors a gate fails with, and the one way to ask the Makefile for
// a variable's value.
//
// WHY THE EXIT CODES ARE HERE AND NOT IN EACH COMMAND. A gate has three
// outcomes and a caller must be able to tell them apart: the tree is clean,
// the tree has a violation, or the gate could not run. Collapsing the last two
// is how a gate starts passing because it could not look — the defect
// go-quicbaas#942 found in verify-api-shapes, which defaulted its inputs to a
// lab mount point and exited 0 when the mount was absent. Every gate in this
// tree reports 0/1/2 and nothing else.
package gate

import (
	"os/exec"
	"strings"
)

// Exit codes. A finding and a tool failure are distinguished so a caller can
// tell "the tree has a violation" from "this gate could not run".
const (
	ExitClean     = 0
	ExitFindings  = 1
	ExitToolError = 2
)

// MissingInputError is the answer to a required input that is absent. It names
// the input and what the gate needed it for, because "skipping" is not an
// answer a gate is allowed to give: the whole point of go-quicbaas#942's
// verify-api-shapes finding is that a silent skip reads exactly like a pass.
type MissingInputError struct {
	// Input is the thing that is missing, named the way an operator supplies
	// it — a flag, a path, or a module.
	Input string
	// Why states what the gate cannot decide without it.
	Why string
}

func (e *MissingInputError) Error() string {
	return "required input " + e.Input + " is missing: " + e.Why
}

// MakeVarError reports that a Makefile variable could not be read. make's own
// complaint is carried rather than written straight to the terminal, so a gate
// that asks about a tree deliberately without a Makefile does not print noise
// past the error it is already returning.
type MakeVarError struct {
	Name   string
	Dir    string
	Output string
	Err    error
}

func (e *MakeVarError) Error() string {
	msg := "cannot read Makefile variable " + e.Name + " in " + e.Dir + ": " + e.Err.Error()
	if trimmed := strings.TrimSpace(e.Output); trimmed != "" {
		msg += " (" + trimmed + ")"
	}
	return msg
}

func (e *MakeVarError) Unwrap() error { return e.Err }

// EmptyMakeVarError reports that a Makefile variable a gate depends on expanded
// to nothing. An empty value is not a usable answer: deriving an image identity
// from an empty DOCKER_IMAGE would produce the tag ":latest", which matches
// whatever the daemon happens to hold.
type EmptyMakeVarError struct {
	Name string
	Dir  string
}

func (e *EmptyMakeVarError) Error() string {
	return "Makefile variable " + e.Name + " in " + e.Dir + " expanded to nothing"
}

// MakeVar returns the value the Makefile in dir gives name, via the `print-%`
// rule the Makefile defines for exactly this purpose.
//
// Asking make rather than parsing the Makefile is deliberate: the values a gate
// needs are built from other variables and from $(shell ...), and a parser that
// approximated make's expansion would answer a question make never asked. The
// authority on what $(DOCKER_IMAGE) means is make.
func MakeVar(dir, name string) (string, error) {
	cmd := exec.Command("make", "--no-print-directory", "print-"+name)
	cmd.Dir = dir
	var complaint strings.Builder
	cmd.Stderr = &complaint
	out, err := cmd.Output()
	if err != nil {
		return "", &MakeVarError{Name: name, Dir: dir, Output: complaint.String(), Err: err}
	}
	value := strings.TrimSpace(string(out))
	if value == "" {
		return "", &EmptyMakeVarError{Name: name, Dir: dir}
	}
	return value, nil
}
