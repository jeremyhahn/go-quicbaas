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

// QRDBRebalanceUnsupportedError is returned by Client.QRDBRebalance when the
// active qrdb transport does not implement go-qrdb's rebalance service.
//
// FAIL CLOSED, NAMED: go-qrdb's Client.Rebalance returns nil on such a
// transport; the QuicBaaS SDK turns that nil into this refusal so a caller
// never dereferences a nil client. It embeds a *QBError carrying
// CodeUnimplemented so predicates such as IsUnimplemented classify it.
type QRDBRebalanceUnsupportedError struct {
	*QBError
}

// NewQRDBRebalanceUnsupported constructs the refusal for one operation.
func NewQRDBRebalanceUnsupported(op string) *QRDBRebalanceUnsupportedError {
	return &QRDBRebalanceUnsupportedError{
		QBError: New(CodeUnimplemented, op,
			"the active qrdb transport does not implement the rebalance service"),
	}
}

// Is reports whether target is a QRDBRebalanceUnsupportedError.
func (e *QRDBRebalanceUnsupportedError) Is(target error) bool {
	_, ok := target.(*QRDBRebalanceUnsupportedError)
	return ok
}

// Unwrap exposes the embedded *QBError so the code predicates classify the
// refusal through errors.As.
func (e *QRDBRebalanceUnsupportedError) Unwrap() error { return e.QBError }
