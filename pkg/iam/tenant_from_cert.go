//go:build iam

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

	qrdbspiffe "github.com/jeremyhahn/go-qrdb/pkg/auth/spiffe"
)

// TenantUnresolvedError is the transport-neutral signal that a caller's tenant
// could not be derived from either a tenant-scoped SPIFFE SAN or a home-realm
// routing hint. It carries the underlying cause so a caller that wants to fail
// closed (e.g. the REST login path) can map it to its own error, while a
// best-effort caller (gRPC/QUIC interceptors) can simply ignore it and proceed
// without a tenant.
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

// TenantFromPeerCert is the transport-neutral tenant-derivation core shared by
// every API surface (REST login, gRPC interceptor, QUIC hook). It resolves a
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
		id, err := qrdbspiffe.TenantFromCert(cert, trustDomain)
		switch {
		case err == nil:
			return id.TenantID, nil
		case errors.Is(err, qrdbspiffe.ErrNoTenantSPIFFEID):
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
		cause = qrdbspiffe.ErrNoTenantSPIFFEID
	}
	return "", &TenantUnresolvedError{Cause: cause}
}
