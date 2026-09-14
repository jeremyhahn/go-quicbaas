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

package prompts

import (
	"errors"
	"strconv"
)

// =============================================================================
// Sentinel errors
// =============================================================================

var (
	// ErrInvalidSpec indicates a SetSpec or PromptSpec that describes nothing
	// registrable.
	ErrInvalidSpec = errors.New("prompts: invalid specification")

	// ErrSetAlreadyRegistered indicates a second set claiming a kind.
	ErrSetAlreadyRegistered = errors.New("prompts: set already registered")

	// ErrUnknownSet indicates a set kind nothing registered.
	ErrUnknownSet = errors.New("prompts: unknown set")

	// ErrUnknownPrompt indicates a prompt name the set does not declare.
	ErrUnknownPrompt = errors.New("prompts: unknown prompt")

	// ErrBlankPrompt indicates a prompt body with nothing in it.
	ErrBlankPrompt = errors.New("prompts: prompt body is blank")

	// ErrUnknownPlaceholder indicates a ${x} the prompt's spec does not
	// declare.
	ErrUnknownPlaceholder = errors.New("prompts: undeclared placeholder")

	// ErrMissingVariable indicates a required variable the body or the caller
	// left out.
	ErrMissingVariable = errors.New("prompts: required variable is missing")

	// ErrUnknownVariable indicates a caller supplying a value for a variable
	// the prompt does not declare.
	ErrUnknownVariable = errors.New("prompts: undeclared variable supplied")

	// ErrMissingMarker indicates a body that dropped a literal the engine
	// parses.
	ErrMissingMarker = errors.New("prompts: required marker is missing")
)

// =============================================================================
// Typed errors
// =============================================================================

// InvalidSpecError reports a specification that cannot be registered.
type InvalidSpecError struct {
	// Set is the kind the specification claims, empty when it claims none.
	Set SetKind

	// Prompt is the prompt at fault, empty when the set itself is.
	Prompt string

	// Message says what is wrong.
	Message string
}

// Error implements the error interface.
func (e *InvalidSpecError) Error() string {
	return ErrInvalidSpec.Error() + ": " + subject(e.Set, e.Prompt) + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *InvalidSpecError) Unwrap() error { return ErrInvalidSpec }

// SetAlreadyRegisteredError reports a second set claiming a kind.
type SetAlreadyRegisteredError struct {
	Set SetKind
}

// Error implements the error interface.
func (e *SetAlreadyRegisteredError) Error() string {
	return ErrSetAlreadyRegistered.Error() + ": " + string(e.Set)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *SetAlreadyRegisteredError) Unwrap() error { return ErrSetAlreadyRegistered }

// UnknownSetError reports a set kind nothing registered.
type UnknownSetError struct {
	Set SetKind
}

// Error implements the error interface.
func (e *UnknownSetError) Error() string {
	return ErrUnknownSet.Error() + ": " + string(e.Set)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *UnknownSetError) Unwrap() error { return ErrUnknownSet }

// UnknownPromptError reports a prompt name the set does not declare.
type UnknownPromptError struct {
	Set    SetKind
	Prompt string
}

// Error implements the error interface.
func (e *UnknownPromptError) Error() string {
	return ErrUnknownPrompt.Error() + ": " + subject(e.Set, e.Prompt)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *UnknownPromptError) Unwrap() error { return ErrUnknownPrompt }

// BlankPromptError reports a body with nothing in it. A prompt that says
// nothing is an agent given no instruction at all, which is the one edit that
// cannot be an improvement.
type BlankPromptError struct {
	Set    SetKind
	Prompt string
}

// Error implements the error interface.
func (e *BlankPromptError) Error() string {
	return ErrBlankPrompt.Error() + ": " + subject(e.Set, e.Prompt)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *BlankPromptError) Unwrap() error { return ErrBlankPrompt }

// UnknownPlaceholderError reports a ${x} the spec does not declare. It NAMES
// the placeholder: a person who mistyped ${headline} as ${head1ine} is told
// which word, not that "the prompt is invalid".
type UnknownPlaceholderError struct {
	Set         SetKind
	Prompt      string
	Placeholder string
}

// Error implements the error interface.
func (e *UnknownPlaceholderError) Error() string {
	return ErrUnknownPlaceholder.Error() + ": " + subject(e.Set, e.Prompt) + ": ${" + e.Placeholder + "}"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *UnknownPlaceholderError) Unwrap() error { return ErrUnknownPlaceholder }

// MissingVariableError reports a required variable that is absent — from the
// body at validation, from the caller's values at rendering.
type MissingVariableError struct {
	Set      SetKind
	Prompt   string
	Variable string
}

// Error implements the error interface.
func (e *MissingVariableError) Error() string {
	return ErrMissingVariable.Error() + ": " + subject(e.Set, e.Prompt) + ": ${" + e.Variable + "}"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *MissingVariableError) Unwrap() error { return ErrMissingVariable }

// UnknownVariableError reports a caller supplying a value nothing declares.
// It is an assembler's typo, caught where it happens rather than rendering as
// silence in an agent's instructions.
type UnknownVariableError struct {
	Set      SetKind
	Prompt   string
	Variable string
}

// Error implements the error interface.
func (e *UnknownVariableError) Error() string {
	return ErrUnknownVariable.Error() + ": " + subject(e.Set, e.Prompt) + ": " + e.Variable
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *UnknownVariableError) Unwrap() error { return ErrUnknownVariable }

// MissingMarkerError reports a body that dropped a literal the engine parses.
// It NAMES the marker and says what reads it, because "invalid prompt" leaves
// the person who deleted the stop clause with nothing to put back.
type MissingMarkerError struct {
	Set    SetKind
	Prompt string
	Marker string
	Reason string
}

// Error implements the error interface.
func (e *MissingMarkerError) Error() string {
	message := ErrMissingMarker.Error() + ": " + subject(e.Set, e.Prompt) + ": " + strconv.Quote(e.Marker)
	if e.Reason != "" {
		message += ": " + e.Reason
	}
	return message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *MissingMarkerError) Unwrap() error { return ErrMissingMarker }

// subject names what an error is about: the set, and the prompt within it
// when there is one.
func subject(set SetKind, prompt string) string {
	switch {
	case set == "" && prompt == "":
		return "(unnamed)"
	case prompt == "":
		return string(set)
	case set == "":
		return prompt
	default:
		return string(set) + "/" + prompt
	}
}
