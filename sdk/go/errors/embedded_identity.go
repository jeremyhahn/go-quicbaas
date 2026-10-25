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

// embeddedIdentityGuidance is the remediation message returned with every
// EmbeddedIdentityRequiredError.
const embeddedIdentityGuidance = "an embedded client needs an explicit qrdb " +
	"identity: pass WithEmbeddedIdentity with ServicePrincipalIdentity or " +
	"DelegatedIdentity alongside WithEmbedded"

// EmbeddedIdentityRequiredError is returned by NewClient when WithEmbedded is
// set without WithEmbeddedIdentity. Nothing binds an identity implicitly: an
// embedded client must name the qrdb identity its calls run as.
//
// It embeds a *QBError carrying CodeInvalidInput so IsInvalidInput still
// classifies it, while IsEmbeddedIdentityRequired identifies it precisely.
type EmbeddedIdentityRequiredError struct {
	*QBError
}

// NewEmbeddedIdentityRequired constructs an EmbeddedIdentityRequiredError for
// the named operation.
func NewEmbeddedIdentityRequired(op string) *EmbeddedIdentityRequiredError {
	return &EmbeddedIdentityRequiredError{
		QBError: New(CodeInvalidInput, op, embeddedIdentityGuidance),
	}
}

// Is reports whether target is an EmbeddedIdentityRequiredError, in addition
// to the QBError code match inherited from the embedded *QBError.
func (e *EmbeddedIdentityRequiredError) Is(target error) bool {
	_, ok := target.(*EmbeddedIdentityRequiredError)
	if ok {
		return true
	}
	return e.QBError.Is(target)
}

// Unwrap exposes the embedded *QBError so errors.As and the package's code
// classifiers descend into it.
func (e *EmbeddedIdentityRequiredError) Unwrap() error {
	return e.QBError
}

// IsEmbeddedIdentityRequired returns true if err is, or wraps, an
// EmbeddedIdentityRequiredError.
func IsEmbeddedIdentityRequired(err error) bool {
	var target *EmbeddedIdentityRequiredError
	return errors.As(err, &target)
}
