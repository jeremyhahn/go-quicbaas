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

// Package service provides the unified service facade for QuicBaaS.
// This file implements multi-tenant secrets isolation. When a tenantID is present
// in the request context, all secrets operations are scoped to that tenant's
// namespace. When tenantID is empty, the system operates in single-tenant
// mode with no behavioral changes (full backward compatibility).
package service

import (
	"context"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// secretsTenantPrefix is the storage prefix for tenant-scoped secrets.
const secretsTenantPrefix = "tenant/"

// secretsTenantSeparator follows the tenant ID in the secrets namespace.
const secretsTenantSeparator = "/"

// secretsTenantScopedID returns a tenant-scoped secret ID if tenantID is non-empty.
// For single-tenant mode (empty tenantID), the secret ID is returned as-is.
// Format: "tenant/{tenantID}/{secretID}".
func secretsTenantScopedID(tenantID, secretID string) string {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return secretID
	}
	return secretsTenantPrefix + tenantID + secretsTenantSeparator + secretID
}

// secretsTenantStripID removes the tenant prefix from a secret ID.
// If the secret ID doesn't have a tenant prefix, it is returned as-is.
func secretsTenantStripID(tenantID, secretID string) string {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return secretID
	}
	prefix := secretsTenantPrefix + tenantID + secretsTenantSeparator
	return strings.TrimPrefix(secretID, prefix)
}

// secretsTenantBelongsTo checks if a secret ID belongs to the given tenant.
// For single-tenant mode (empty tenantID), all secrets belong.
// For multi-tenant mode, the secret must have the matching tenant prefix.
func secretsTenantBelongsTo(tenantID, secretID string) bool {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return true
	}
	prefix := secretsTenantPrefix + tenantID + secretsTenantSeparator
	return strings.HasPrefix(secretID, prefix)
}

// secretsTenantListPrefix returns the prefix to use when listing secrets for a tenant.
// For single-tenant mode, returns empty string (list all).
// For multi-tenant mode, returns "tenant/{tenantID}/".
func secretsTenantListPrefix(tenantID string) string {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return ""
	}
	return secretsTenantPrefix + tenantID + secretsTenantSeparator
}

// enforceSecretsTenantAccess validates that the requesting tenant has access to the secret.
// Returns nil if access is allowed, or an error if the tenant doesn't own the secret.
func enforceSecretsTenantAccess(tenantID, secretID, operation string) error {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return nil // Single-tenant mode / system tenant, no access control
	}
	if !secretsTenantBelongsTo(tenantID, secretID) {
		return NewError(errors.ErrCrossTenantAccess, "secrets."+operation,
			"secret does not belong to the requesting tenant")
	}
	return nil
}

// secretsScopedTenant resolves the tenant whose namespace a secrets request
// addresses, reconciling the tenant bound to the CONTEXT with the tenant the
// REQUEST names.
//
// StoreSecret and EnsureSecret — the only two secrets requests that carry a
// tenant — and GetSecret, the read they have to agree with, all resolve their
// namespace through this one function, so the read side and the write side
// can never compute different namespaces for the same request. That divergence is go-quicbaas#534: StoreSecret scoped by the
// context alone and used req.TenantID only to pick a DEK, while EnsureSecret
// scoped by req.TenantID, so a value written through one path was invisible
// to the other and a tenant could steer a write into a namespace it does not
// own.
//
// The rule is that a request may name only its own tenant:
//
//   - an omitted tenant means "my own", the overwhelmingly common case;
//   - a tenant equal to the context tenant is accepted and selects the same
//     namespace and the same tenant-keyed DEK it always did;
//   - any other tenant is refused, for EVERY identity including the system
//     tenant.
//
// Refusing the mismatch outright — rather than following the request the way
// pkg/iam's appTenantContext follows an application record's tenant — is what
// keeps the fix safe. appTenantContext may trust the record because a
// management surface authorized the caller for that tenant before the store
// is reached. No such surface exists here: the secrets routes are authorized
// with go-qrdb's secrets:read/write/delete/list, which are ScopeTenantOnly,
// so the policy decision point has already established that the identity's
// tenant equals the request's target tenant. A req.TenantID naming anything
// else can therefore never have been authorized, and the service says so
// instead of silently writing somewhere the caller did not intend.
//
// The two spellings of the system scope — "" (unbound) and SystemTenantID —
// denote one namespace (qrdbsdk.IsSystemTenant), so naming one from the other
// is not a mismatch. The context's spelling is the one returned, because that
// is the value the rest of the service already scopes and logs by.
func secretsScopedTenant(ctx context.Context, requestedTenantID, operation string) (string, error) {
	ctxTenant, err := prepareTenantContext(ctx)
	if err != nil {
		return "", err
	}
	if requestedTenantID == "" {
		return ctxTenant, nil
	}
	if err := validateTenantID(requestedTenantID); err != nil {
		return "", err
	}
	if !secretsSameTenant(ctxTenant, requestedTenantID) {
		return "", NewError(errors.ErrCrossTenantAccess, "secrets."+operation,
			"request names tenant \""+requestedTenantID+"\", which is not the caller's tenant")
	}
	return ctxTenant, nil
}

// secretsSameTenant reports whether two tenant identifiers denote the same
// namespace, folding the unbound and system spellings onto each other.
func secretsSameTenant(a, b string) bool {
	return qrdbsdk.NormalizeTenantID(a) == qrdbsdk.NormalizeTenantID(b)
}
