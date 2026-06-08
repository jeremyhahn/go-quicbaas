//go:build iam

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
	"context"
	"crypto/x509"
)

// DualIdentity carries the two principals an authenticated broker request
// involves: the SERVICE that presented the request at the transport (verified
// client-certificate identity) and the USER the access token authorizes. An
// audit entry built from a DualIdentity satisfies AU-2/AU-3 — it records WHO
// (service + user), the tenant, and links to the issued token (jti).
type DualIdentity struct {
	// ServiceIdentity is the verified transport-level service identity: the
	// client certificate's SPIFFE URI SAN when present, otherwise the cert
	// Subject DN. Empty when the call had no verified client certificate.
	ServiceIdentity string

	// Subject is the user identifier carried by the access token (sub).
	Subject string

	// Username is the human-readable subject name when known.
	Username string

	// TenantID is the tenant the token is scoped to (tenant_id claim). It is
	// the routing key for the per-tenant audit store and is stamped on the
	// event regardless of store layout.
	TenantID string

	// ClientID is the OAuth client the token was issued to.
	ClientID string

	// JTI is the access-token identifier.
	JTI string

	// Scopes is the array-form scope set (scp) carried by the token.
	Scopes []string

	// Roles is the role set carried by the token.
	Roles []string

	// IPAddress is the network source of the request (client IP). It is
	// proxy-aware at the transport layer (X-Forwarded-For/X-Real-IP, falling
	// back to RemoteAddr). Empty when the caller does not supply it.
	IPAddress string

	// UserAgent is the client user-agent string of the request. Empty when the
	// caller does not supply it.
	UserAgent string
}

// ServiceIdentityFromCert derives a stable service-identity string from a
// verified client certificate. It prefers the first spiffe:// URI SAN (the
// canonical workload identity) and falls back to the certificate Subject DN.
// A nil certificate yields the empty string so callers can record an event
// with no service identity rather than fail.
func ServiceIdentityFromCert(cert *x509.Certificate) string {
	if cert == nil {
		return ""
	}
	for _, u := range cert.URIs {
		if u != nil && u.Scheme == "spiffe" {
			return u.String()
		}
	}
	return cert.Subject.String()
}

// LogDualIdentity records an audit event carrying BOTH the service identity
// (verified client cert SPIFFE/DN) and the user identity (sub, tenant_id, scp,
// roles, jti) for an authenticated request. The event is stamped with the
// tenant from the DualIdentity (falling back to the tenant on the context)
// so it routes to the correct per-tenant audit partition. The network source
// (IPAddress) and UserAgent are stamped on the event when supplied. A nil
// logger or empty tenant returns an error rather than silently dropping the
// event (fail-closed: an unauditable security event must be visible to the
// caller).
func LogDualIdentity(
	ctx context.Context,
	logger AuditLogger,
	id DualIdentity,
	eventType AuditEventType,
	category AuditCategory,
	action string,
	outcome AuditOutcome,
	details map[string]interface{},
) error {
	if logger == nil {
		return NewIAMError(ErrAuditInvalid, "iam.audit.dual_identity", "audit logger is nil", nil)
	}

	tenantID := id.TenantID
	if tenantID == "" {
		tenantID = TenantFromContext(ctx)
	}
	if tenantID == "" {
		return NewIAMError(ErrAuditInvalid, "iam.audit.dual_identity", "tenant id is required for dual-identity audit", nil)
	}

	actor := &AuditActor{
		UserID:          id.Subject,
		Username:        id.Username,
		ClientID:        id.ClientID,
		ServiceIdentity: id.ServiceIdentity,
		JTI:             id.JTI,
		Scopes:          id.Scopes,
		Roles:           id.Roles,
	}

	event := &AuditEvent{
		TenantID:  tenantID,
		EventType: eventType,
		Category:  category,
		Actor:     actor,
		Action:    action,
		Outcome:   outcome,
		Details:   details,
		IPAddress: id.IPAddress,
		UserAgent: id.UserAgent,
	}

	return logger.Log(ctx, event)
}
