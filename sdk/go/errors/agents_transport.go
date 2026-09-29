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

// AgentsTransportUnsupportedError is returned by the Agents client on a
// transport that does not declare the quicbaas.Agents service.
//
// FAIL CLOSED, NAMED: every transport package asserts agents.CapableTransport
// at compile time, so this is reached only by a transport outside the SDK's
// own set. It embeds a *QBError carrying CodeUnimplemented so predicates such
// as IsUnimplemented continue to classify it.
type AgentsTransportUnsupportedError struct {
	*QBError

	// Transport names the concrete transport that refused.
	Transport string
}

// NewAgentsTransportUnsupported constructs the refusal for one operation on
// one transport.
func NewAgentsTransportUnsupported(op, transport string) *AgentsTransportUnsupportedError {
	return &AgentsTransportUnsupportedError{
		QBError: New(CodeUnimplemented, op,
			"the "+transport+" transport does not declare the Agents service"),
		Transport: transport,
	}
}

// Is reports whether target is an AgentsTransportUnsupportedError.
func (e *AgentsTransportUnsupportedError) Is(target error) bool {
	_, ok := target.(*AgentsTransportUnsupportedError)
	return ok
}

// Unwrap exposes the embedded *QBError so the code predicates classify the
// refusal through errors.As.
func (e *AgentsTransportUnsupportedError) Unwrap() error { return e.QBError }
