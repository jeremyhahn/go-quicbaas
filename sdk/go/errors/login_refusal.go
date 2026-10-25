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

package errors

import (
	"errors"
	"strings"
)

// tenantLoginGuidance names why a non-system tenant login is refused on this
// release and which change lifts the refusal.
const tenantLoginGuidance = "non-system tenant login is refused on this " +
	"release: it needs go-qrdb v0.54.0-alpha (go-qrdb #1084, #1089), " +
	"tracked by go-quicbaas #1816"

// TenantLoginRefusedError is the one refusal every transport returns when a
// login for a tenant other than the system tenant reaches token issuance.
// Issuing that bearer needs the go-qrdb context binder and effective
// permissions re-export, which this release does not pin yet.
type TenantLoginRefusedError struct{}

// Error implements the error interface.
func (e *TenantLoginRefusedError) Error() string {
	return tenantLoginGuidance
}

// NewTenantLoginRefused wraps the refusal in a QBError carrying
// CodePreconditionFailed, so code classifiers and transports map it to a
// failed precondition while errors.As still finds the typed refusal.
func NewTenantLoginRefused(op string) *QBError {
	return &QBError{
		Code:    CodePreconditionFailed,
		Op:      op,
		Message: "non-system tenant login",
		Err:     &TenantLoginRefusedError{},
	}
}

// RestoreTenantLoginRefused returns NewTenantLoginRefused(op) when err is a
// remote failed precondition carrying the refusal's guidance, the form the
// refusal takes after any network transport, and err unchanged otherwise. A
// failed precondition without the guidance, such as a user that is no longer
// pending, keeps its own meaning; an error that already wraps the typed
// refusal is returned as is.
func RestoreTenantLoginRefused(op string, err error) error {
	var refused *TenantLoginRefusedError
	if errors.As(err, &refused) || Code(err) != CodePreconditionFailed ||
		!strings.Contains(err.Error(), tenantLoginGuidance) {
		return err
	}
	return NewTenantLoginRefused(op)
}
