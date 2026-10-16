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

package errors

// PromptTransportUnsupportedError is returned by the prompt client on a
// transport that does not declare the quicbaas.Prompts service.
//
// FAIL CLOSED, NAMED (docs/00 L4): every transport package asserts
// prompt.PromptsCapableTransport at compile time, so this is reached only by
// a transport outside the SDK's own set. It embeds a *QBError carrying
// CodeUnimplemented so predicates such as IsUnimplemented continue to
// classify it, while errors.Is against the type identifies it precisely.
type PromptTransportUnsupportedError struct {
	*QBError

	// Transport names the concrete transport that refused.
	Transport string
}

// NewPromptTransportUnsupported constructs the refusal for one operation on
// one transport.
func NewPromptTransportUnsupported(op, transport string) *PromptTransportUnsupportedError {
	return &PromptTransportUnsupportedError{
		QBError: New(CodeUnimplemented, op,
			"the "+transport+" transport does not declare the Prompts service"),
		Transport: transport,
	}
}

// Is reports whether target is a PromptTransportUnsupportedError, making the
// type matchable with errors.Is in addition to the embedded code match.
func (e *PromptTransportUnsupportedError) Is(target error) bool {
	_, ok := target.(*PromptTransportUnsupportedError)
	return ok
}

// Unwrap exposes the embedded *QBError so the code predicates (IsUnimplemented
// and friends) classify the refusal through errors.As.
func (e *PromptTransportUnsupportedError) Unwrap() error { return e.QBError }
