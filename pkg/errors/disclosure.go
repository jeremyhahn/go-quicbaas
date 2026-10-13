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

// ServerFailureMessage is the text a transport returns in place of an error
// that describes the SERVER rather than the caller's request.
//
// One sentence for every such class, deliberately. Distinguishing "storage is
// unavailable" from "the leader moved" from "a file could not be opened" tells
// an unauthenticated caller about the deployment's state, and the three are
// equally unactionable by that caller: the error code already carries
// everything a client can branch on. The detail goes to the server log, where
// an operator can act on it.
const ServerFailureMessage = "the request could not be completed"

// requestClassCodes are the error classes that describe the caller's request.
var requestClassCodes = map[ErrorCode]struct{}{
	ErrNotFound:          {},
	ErrTimeout:           {},
	ErrDeadlineExceeded:  {},
	ErrInvalidInput:      {},
	ErrValidation:        {},
	ErrForbidden:         {},
	ErrNotAllowed:        {},
	ErrCrossTenantAccess: {},
	ErrUnauthorized:      {},
	ErrConflict:          {},
	ErrAlreadyExists:     {},
}

// DescribesRequest reports whether an error of class code describes the
// caller's request, so its message may be returned to that caller. Every other
// class describes the server: a transport answers it with ServerFailureMessage
// and logs the cause (go-quicbaas#427). That includes any code added later: a
// new class is sanitized until someone decides it is safe to echo, rather than
// echoed until someone notices it is not.
func DescribesRequest(code ErrorCode) bool {
	_, ok := requestClassCodes[code]
	return ok
}
