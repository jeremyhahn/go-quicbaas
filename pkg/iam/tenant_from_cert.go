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

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// TenantUnresolvedError is the transport-neutral signal that a caller's tenant
// could not be derived from either a tenant-scoped SPIFFE SAN or a home-realm
// routing hint. It carries the underlying cause so the REST login path can map
// it to its own fail-closed error.
type TenantUnresolvedError struct {
	// Cause is the underlying derivation failure, when one is available.
	Cause error
}

// Error implements the error interface.
func (e *TenantUnresolvedError) Error() string {
	if e.Cause != nil {
		return "tenant could not be resolved from peer certificate or routing hints: " + e.Cause.Error()
	}
	return "tenant could not be resolved from peer certificate or routing hints"
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *TenantUnresolvedError) Unwrap() error {
	return e.Cause
}

// TenantFromPeerCert is the login tenant-derivation core. It resolves a
// caller's tenant in the locked priority order:
//
//  1. The tenant-scoped SPIFFE URI SAN on the verified peer certificate, with
//     the operator-configured trust domain PINNED. A foreign trust domain or a
//     malformed SAN fails closed immediately (it does NOT fall through to HRD),
//     so a presented-but-invalid cert can never be silently re-routed. The SO /
//     bootstrap cert's tenant SAN resolves the system tenant so the control
//     plane works pre-unseal.
//  2. When the cert simply carries no tenant SPIFFE SAN (or there is no cert),
//     home-realm discovery from the supplied non-secret routing hints
//     (SNI/Host, email domain, explicit hint), read from the shared secret-free
//     routing directory.
//  3. A TenantUnresolvedError carrying the SPIFFE cause (or a bare miss).
//
// This function never trusts client-supplied data as a tenant directly; the
// SPIFFE SAN is read from the VERIFIED peer cert and the hints are validated
// against the shared routing directory. It is pure resolution: callers decide
// whether an unresolved tenant is fatal (login) or simply absent (interceptors).
//
// cert may be nil (non-mTLS callers), in which case derivation goes straight to
// HRD. hrd may be nil, in which case only the SPIFFE SAN is consulted.
func TenantFromPeerCert(ctx context.Context, cert *x509.Certificate, trustDomain string, hrd HomeRealmResolver, hints RealmHints) (string, error) {
	// 1. SPIFFE SAN (pinned trust domain).
	if cert != nil {
		id, err := qrdbsdk.TenantFromCert(cert, trustDomain)
		switch {
		case err == nil:
			// The subject OU carries the tenant too, and must agree.
			if ouErr := CheckCertOUTenant(cert, id.TenantID); ouErr != nil {
				return "", &TenantUnresolvedError{Cause: ouErr}
			}
			return id.TenantID, nil
		case errors.Is(err, qrdbsdk.ErrSpiffeNoTenantID):
			// No SPIFFE SAN on this cert: fall through to HRD (covers external
			// PIV/CAC and browser/WebAuthn flows).
		default:
			// Foreign trust domain or malformed SAN: fail closed without
			// consulting HRD, preserving the cause so a fail-closed caller can
			// map it precisely.
			return "", &TenantUnresolvedError{Cause: err}
		}
	}

	// 2. Home-realm discovery (shared, secret-free, readable pre-unseal).
	if hrd != nil {
		if tenantID, err := hrd.Resolve(ctx, hints); err == nil && tenantID != "" {
			return tenantID, nil
		}
	}

	// 3. Unresolved. When a cert was presented it carried no usable SPIFFE SAN
	//    (ErrNoTenantSPIFFEID); surface that cause so a fail-closed caller can
	//    distinguish a reissue-required cert from a no-cert miss.
	var cause error
	if cert != nil {
		cause = qrdbsdk.ErrSpiffeNoTenantID
	}
	return "", &TenantUnresolvedError{Cause: cause}
}

// TenantFromSubjectOU returns the tenant a certificate's subject OU carries:
// the first OU value, trimmed, or "" when the certificate has none.
//
// The subject OU carries the tenant on every layer (owner ruling, rulings.md
// f518455): go-qrdb, go-xkms and this project all stamp it, and go-qrdb reads
// it (go-qrdb#777).
func TenantFromSubjectOU(cert *x509.Certificate) string {
	if cert == nil || len(cert.Subject.OrganizationalUnit) == 0 {
		return ""
	}
	return strings.TrimSpace(cert.Subject.OrganizationalUnit[0])
}

// CertOUTenantMismatchError reports a certificate whose subject OU names a
// tenant other than the one its SPIFFE SAN names. Such a certificate names no
// tenant: the two carriers must agree (go-qrdb#777).
type CertOUTenantMismatchError struct {
	// OU is the tenant the subject OU names.
	OU string
	// Tenant is the tenant the SPIFFE SAN names.
	Tenant string
}

func (e *CertOUTenantMismatchError) Error() string {
	return fmt.Sprintf("certificate subject OU names tenant %q but its SPIFFE SAN names tenant %q", e.OU, e.Tenant)
}

// CheckCertOUTenant refuses a certificate whose subject OU names a tenant
// other than tenant, the tenant resolved from its SPIFFE SAN. A certificate
// with no OU passes: it was issued before the OU carried the tenant, and its
// SAN alone still names one.
func CheckCertOUTenant(cert *x509.Certificate, tenant string) error {
	ou := TenantFromSubjectOU(cert)
	if ou == "" || qrdbsdk.NormalizeTenantID(ou) == qrdbsdk.NormalizeTenantID(tenant) {
		return nil
	}
	return &CertOUTenantMismatchError{OU: ou, Tenant: tenant}
}
