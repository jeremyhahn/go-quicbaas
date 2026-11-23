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

package server

import (
	"context"
	"crypto/x509"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-xkms/pkg/ca/provider"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/caadapter"
)

// callerTenantCA is the CA the go-xkms service issues through. The go-xkms
// KeystoreService carries no tenant field, so a certificate requested over it
// would be issued at platform scope: no tenant SPIFFE SAN, and an owner the
// requesting tenant may not revoke. When the request names no tenant,
// callerTenantCA issues for the authenticated caller's own tenant, the same
// tenant go-xkms later binds revocation to. A system-tenant or absent
// identity keeps platform scope; a named tenant is passed through unchanged
// (go-xkms has already bound it to the caller).
type callerTenantCA struct{ provider.CA }

// callerTenantTCGCA is callerTenantCA over a CA that also issues TCG
// certificates, so the go-xkms TCG servicer still finds them.
type callerTenantTCGCA struct {
	callerTenantCA
	tcg provider.TCGCA
}

var (
	_ provider.CA    = callerTenantCA{}
	_ provider.TCGCA = callerTenantTCGCA{}
)

// withCallerTenant wraps ca so that issuance naming no tenant is bound to the
// authenticated caller's tenant. A CA that also issues TCG certificates stays
// a provider.TCGCA. Wrapping is idempotent.
func withCallerTenant(ca provider.CA) provider.CA {
	switch ca.(type) {
	case nil:
		return nil
	case callerTenantCA, callerTenantTCGCA:
		return ca
	}
	if tcg, ok := ca.(provider.TCGCA); ok {
		return callerTenantTCGCA{callerTenantCA{ca}, tcg}
	}
	return callerTenantCA{ca}
}

// callerTenant returns tenantID when the request names one, and otherwise the
// tenant of the non-system go-qrdb identity in ctx. A system-tenant or absent
// identity yields "" (platform scope). The predicate is the one go-xkms's
// revocation binding uses, so issuance and revocation agree on the owner.
func callerTenant(ctx context.Context, tenantID string) string {
	if tenantID != "" {
		return tenantID
	}
	id := qrdbsdk.IdentityFromContext(ctx)
	if id == nil || qrdbsdk.IdentityIsSystemTenant(id) {
		return ""
	}
	return id.TenantID
}

// SignCSRWithSPIFFE signs for the caller's tenant when tenantID is empty.
func (c callerTenantCA) SignCSRWithSPIFFE(ctx context.Context, csrPEM []byte, profile string, validityDays int, trustDomain, tenantID, kind, name string) (*x509.Certificate, error) {
	return c.CA.SignCSRWithSPIFFE(ctx, csrPEM, profile, validityDays, trustDomain, callerTenant(ctx, tenantID), kind, name)
}

// IssueCertificateRaw issues for the caller's tenant when tenantID is empty.
func (c callerTenantCA) IssueCertificateRaw(
	ctx context.Context,
	commonName, organization string,
	sans []string,
	validityDays int,
	profile, algorithm, tenantID string,
) (certPEM, chainPEM, keyPEM []byte, serialHex string, err error) {
	return c.CA.IssueCertificateRaw(ctx, commonName, organization, sans, validityDays, profile, algorithm, callerTenant(ctx, tenantID))
}

// tcgCallerAllowed refuses TCG issuance for any caller but the system
// tenant, as caadapter.tcgRequest does on every other transport: the pinned
// go-xkms records no issuing tenant on TCG certificates, so a tenant could
// never revoke one. An absent identity is refused too (fail closed).
func tcgCallerAllowed(ctx context.Context) error {
	id := qrdbsdk.IdentityFromContext(ctx)
	if id != nil && qrdbsdk.IdentityIsSystemTenant(id) {
		return nil
	}
	return caadapter.ErrXKMSCATCGTenantUnsupported
}

// IssueEKCertificateRaw forwards to the wrapped TCG CA, for the system tenant
// only.
func (c callerTenantTCGCA) IssueEKCertificateRaw(ctx context.Context, commonName, organization string, ekPubDER []byte) ([]byte, error) {
	if err := tcgCallerAllowed(ctx); err != nil {
		return nil, err
	}
	return c.tcg.IssueEKCertificateRaw(ctx, commonName, organization, ekPubDER)
}

// IssueAKCertificateRaw forwards to the wrapped TCG CA, for the system tenant
// only.
func (c callerTenantTCGCA) IssueAKCertificateRaw(ctx context.Context, commonName, organization string, pubDER []byte) ([]byte, error) {
	if err := tcgCallerAllowed(ctx); err != nil {
		return nil, err
	}
	return c.tcg.IssueAKCertificateRaw(ctx, commonName, organization, pubDER)
}

// SignTCGCSRIDevIDRaw forwards to the wrapped TCG CA, for the system tenant
// only.
func (c callerTenantTCGCA) SignTCGCSRIDevIDRaw(ctx context.Context, commonName, organization string, packedCSR []byte) (iakDER, idevidDER []byte, err error) {
	if err := tcgCallerAllowed(ctx); err != nil {
		return nil, nil, err
	}
	return c.tcg.SignTCGCSRIDevIDRaw(ctx, commonName, organization, packedCSR)
}

// EnrollDeviceRaw forwards to the wrapped TCG CA, for the system tenant only.
func (c callerTenantTCGCA) EnrollDeviceRaw(ctx context.Context, commonName, organization string, packedCSR []byte) (
	iakDER, idevidDER, credBlob, encSecret, plainSecret []byte, err error,
) {
	if err := tcgCallerAllowed(ctx); err != nil {
		return nil, nil, nil, nil, nil, err
	}
	return c.tcg.EnrollDeviceRaw(ctx, commonName, organization, packedCSR)
}
