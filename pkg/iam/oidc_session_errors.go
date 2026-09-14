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

package iam

import "fmt"

// Session-issuance error types.
//
// Both reject a login that verified its factor but cannot be turned into an
// authorized session. They are returned to the login ceremony, which surfaces
// them as a failed login — deliberately, because the alternative is a bearer
// that authorizes nothing and a client that believes it is signed in.

// SessionPrincipalUnresolvedError reports that a verified login identity has no
// resolvable IAM user record in its tenant, so no roles can be minted for it.
type SessionPrincipalUnresolvedError struct {
	// Subject is the identity the ceremony authenticated (a user ID or a
	// tenant-scoped username).
	Subject string
	// TenantID is the tenant the lookup was scoped to.
	TenantID string
	// Cause is the underlying store error, when there was one.
	Cause error
}

func (e *SessionPrincipalUnresolvedError) Error() string {
	if e.Subject == "" {
		return "iam: session issuance requires an authenticated subject"
	}
	return fmt.Sprintf("iam: no user record for session subject %q in tenant %q", e.Subject, e.TenantID)
}

// Unwrap exposes the store error so callers can distinguish a missing record
// from a store outage.
func (e *SessionPrincipalUnresolvedError) Unwrap() error { return e.Cause }

// SessionPrincipalDisabledError reports that the IAM user record behind a
// verified login is suspended, locked, or disabled.
type SessionPrincipalDisabledError struct {
	Subject  string
	TenantID string
}

func (e *SessionPrincipalDisabledError) Error() string {
	return fmt.Sprintf("iam: session subject %q in tenant %q is not enabled", e.Subject, e.TenantID)
}
