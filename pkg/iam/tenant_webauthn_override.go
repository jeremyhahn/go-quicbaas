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

package iam

import (
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/webauthnrp"
)

// Fields of a tenant's WebAuthn override named by TenantWebAuthnOverrideError.
const (
	tenantWebAuthnFieldRPID      = "settings.webauthn.rp_id"
	tenantWebAuthnFieldRPOrigins = "settings.webauthn.rp_origins"
)

// TenantWebAuthnOverrideError reports a per-tenant WebAuthn relying-party
// override whose RP ID or origins fail the same shape rules the global
// relying party is held to at boot (package webauthnrp).
type TenantWebAuthnOverrideError struct {
	// Field is the settings path of the offending value.
	Field string
	// Cause is the webauthnrp rule the value failed.
	Cause error
}

// Error implements error.
func (e *TenantWebAuthnOverrideError) Error() string {
	return "invalid tenant WebAuthn override: " + e.Field + ": " + e.Cause.Error()
}

// Unwrap returns the webauthnrp rule the value failed.
func (e *TenantWebAuthnOverrideError) Unwrap() error { return e.Cause }

// Validate checks the override's RP ID and origins with the rules the global
// relying party is validated with. Empty fields inherit the global value and
// are not checked; a nil override is valid.
func (c *TenantWebAuthnConfig) Validate() error {
	if c == nil {
		return nil
	}
	if c.RPID != "" {
		if err := webauthnrp.ValidateRPID(c.RPID); err != nil {
			return &TenantWebAuthnOverrideError{Field: tenantWebAuthnFieldRPID, Cause: err}
		}
	}
	if err := webauthnrp.ValidateOrigins(c.RPOrigins); err != nil {
		return &TenantWebAuthnOverrideError{Field: tenantWebAuthnFieldRPOrigins, Cause: err}
	}
	return nil
}

// validateTenantSettings refuses settings a tenant write must not store. The
// returned error carries ErrInvalidInput, which every transport maps to a
// client error (HTTP 400, gRPC InvalidArgument).
func validateTenantSettings(op string, settings *TenantSettings) error {
	if settings == nil {
		return nil
	}
	if err := settings.WebAuthn.Validate(); err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInvalidInput, Op: op, Err: err}
	}
	return nil
}
