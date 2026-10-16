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

package service

import (
	"errors"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

var (
	// ErrPromptsUnavailable is the refusal every transport answers when the
	// prompt store is not mounted on this node.
	ErrPromptsUnavailable = errors.New("prompt store is not running on this node")

	// ErrPromptRequest is the sentinel of a prompt request the service
	// refuses before it reaches the store.
	ErrPromptRequest = errors.New("invalid prompt request")

	// ErrPromptNotOverridden is the sentinel of a Get at a scope whose record
	// follows its parent: nothing is overridden there.
	ErrPromptNotOverridden = errors.New("prompt is not overridden at this scope")

	// ErrPromptServiceConfig is the sentinel of a prompt service built
	// without a required dependency.
	ErrPromptServiceConfig = errors.New("invalid prompt service configuration")
)

// PromptRequestError reports a prompt request field the service refuses.
type PromptRequestError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *PromptRequestError) Error() string {
	return "invalid prompt request: " + e.Field + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *PromptRequestError) Unwrap() error { return ErrPromptRequest }

// PromptNotOverriddenError reports a Get at a scope whose record follows its
// parent. The prompt exists there only as an unedited copy.
type PromptNotOverriddenError struct {
	Ref prompts.Ref
}

// Error implements the error interface.
func (e *PromptNotOverriddenError) Error() string {
	return "prompt " + e.Ref.String() + " is not overridden at this scope"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *PromptNotOverriddenError) Unwrap() error { return ErrPromptNotOverridden }

// PromptServiceConfigError reports a prompt service built without a
// required dependency.
type PromptServiceConfigError struct {
	Field string
}

// Error implements the error interface.
func (e *PromptServiceConfigError) Error() string {
	return "invalid prompt service configuration: " + e.Field + " is required"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *PromptServiceConfigError) Unwrap() error { return ErrPromptServiceConfig }

// PromptFault is what kind of refusal a prompt service error is, so every
// transport answers it with the same status.
type PromptFault int

const (
	// PromptFaultInvalid is a request the store cannot accept: an unknown
	// set or prompt, a body missing a marker, a write at the system scope.
	PromptFaultInvalid PromptFault = iota + 1

	// PromptFaultNotFound names a record, revision or project nobody wrote.
	PromptFaultNotFound

	// PromptFaultConflict is a write against a version somebody already
	// replaced.
	PromptFaultConflict

	// PromptFaultUnavailable is a store that is not mounted or cannot answer.
	PromptFaultUnavailable
)

// promptFaults maps each sentinel a prompt verb returns to its fault. The
// order matters: a ResolveError unwraps to its cause as well as to
// prompts.ErrResolve, and the cause decides.
var promptFaults = []struct {
	cause error
	fault PromptFault
}{
	{prompts.ErrNotFound, PromptFaultNotFound},
	{prompts.ErrUnknownRevision, PromptFaultNotFound},
	{ErrPromptNotOverridden, PromptFaultNotFound},
	{prompts.ErrVersionConflict, PromptFaultConflict},
	{ErrPromptRequest, PromptFaultInvalid},
	{prompts.ErrInvalidInput, PromptFaultInvalid},
	{prompts.ErrUnknownSet, PromptFaultInvalid},
	{prompts.ErrUnknownPrompt, PromptFaultInvalid},
	{prompts.ErrBlankPrompt, PromptFaultInvalid},
	{prompts.ErrUnknownPlaceholder, PromptFaultInvalid},
	{prompts.ErrMissingVariable, PromptFaultInvalid},
	{prompts.ErrUnknownVariable, PromptFaultInvalid},
	{prompts.ErrMissingMarker, PromptFaultInvalid},
	{ErrPromptsUnavailable, PromptFaultUnavailable},
	{prompts.ErrBackend, PromptFaultUnavailable},
	{prompts.ErrGuardedWriteUnsupported, PromptFaultUnavailable},
}

// ClassifyPromptError reports which kind of refusal err is, and false when
// it is none the prompt service makes: a fault of the server.
func ClassifyPromptError(err error) (PromptFault, bool) {
	for _, mapping := range promptFaults {
		if errors.Is(err, mapping.cause) {
			return mapping.fault, true
		}
	}
	var (
		projectNotFound *ProjectNotFoundError
		projectInvalid  *ProjectInvalidError
	)
	switch {
	case errors.As(err, &projectNotFound):
		return PromptFaultNotFound, true
	case errors.As(err, &projectInvalid):
		return PromptFaultInvalid, true
	}
	return 0, false
}
