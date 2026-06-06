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

import "errors"

// passwordLoginGuidance is the canonical remediation message returned with
// every PasswordLoginUnsupportedError. It directs callers to the unified
// factor login ceremony, which is the only supported authentication path.
const passwordLoginGuidance = "password login is not supported: the locked " +
	"architecture has no passwords; use the unified factor login ceremony " +
	"(BeginLogin/FinishLogin) with an mtls, fido2, or piv factor"

// PasswordLoginUnsupportedError is returned by the legacy username/password
// Login path. Authentication in the locked architecture is cert/factor-based;
// the password Login verb is retired and must fail closed rather than silently
// submit an empty password. Callers must migrate to the unified login ceremony
// (BeginLogin/FinishLogin).
//
// It embeds a *QBError carrying CodeUnimplemented so existing predicates such
// as IsUnimplemented continue to classify it, while IsPasswordLoginUnsupported
// identifies it precisely.
type PasswordLoginUnsupportedError struct {
	*QBError
}

// NewPasswordLoginUnsupported constructs a PasswordLoginUnsupportedError for
// the named operation.
func NewPasswordLoginUnsupported(op string) *PasswordLoginUnsupportedError {
	return &PasswordLoginUnsupportedError{
		QBError: New(CodeUnimplemented, op, passwordLoginGuidance),
	}
}

// Is reports whether target is a PasswordLoginUnsupportedError. This makes the
// type matchable with errors.Is in addition to the QBError code match inherited
// from the embedded *QBError.
func (e *PasswordLoginUnsupportedError) Is(target error) bool {
	_, ok := target.(*PasswordLoginUnsupportedError)
	if ok {
		return true
	}
	return e.QBError.Is(target)
}

// Unwrap exposes the embedded *QBError so errors.As and the package's code
// classifiers (Code, IsUnimplemented, ...) descend into it. Without this the
// promoted QBError.Unwrap would return only its (nil) inner cause and the
// classifiers would not see CodeUnimplemented.
func (e *PasswordLoginUnsupportedError) Unwrap() error {
	return e.QBError
}

// IsPasswordLoginUnsupported returns true if err is, or wraps, a
// PasswordLoginUnsupportedError.
func IsPasswordLoginUnsupported(err error) bool {
	var target *PasswordLoginUnsupportedError
	return errors.As(err, &target)
}
