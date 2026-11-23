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
	"encoding/pem"
	"errors"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-xkms/pkg/ca/provider"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/caadapter"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// callerTenantCA is the CA the go-xkms service issues through. The go-xkms
// KeystoreService carries no tenant field, so a certificate requested over it
// would be issued at platform scope: no tenant SPIFFE SAN, and an owner the
// requesting tenant may not revoke. When the request names no tenant,
// callerTenantCA issues for the authenticated caller's own tenant, the same
// tenant go-xkms later binds revocation to. A system-tenant or absent
// identity keeps platform scope; a named tenant is passed through unchanged
// (go-xkms has already bound it to the caller). When a SPIFFE trust domain is
// configured, a certificate issued for a tenant carries the tenant SPIFFE SAN,
// built as the caadapter builds it for REST, MCP, Unix and embedded callers.
type callerTenantCA struct {
	provider.CA
	// issuer issues keypair certificates with the tenant SPIFFE SAN fields;
	// nil forwards keypair issuance to the wrapped CA unchanged.
	issuer xkmssdk.TrustedCA
	// trustDomain is the configured SPIFFE trust domain ("" for none).
	trustDomain string
}

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
// a provider.TCGCA. issuer, when non-nil, is ca's TrustedCA face, used to
// issue keypair certificates carrying trustDomain's tenant SPIFFE SAN.
// Wrapping is idempotent: an already-wrapped CA is returned unchanged.
func withCallerTenant(ca provider.CA, issuer xkmssdk.TrustedCA, trustDomain string) provider.CA {
	switch ca.(type) {
	case nil:
		return nil
	case callerTenantCA, callerTenantTCGCA:
		return ca
	}
	wrapped := callerTenantCA{CA: ca, issuer: issuer, trustDomain: trustDomain}
	if tcg, ok := ca.(provider.TCGCA); ok {
		return callerTenantTCGCA{wrapped, tcg}
	}
	return wrapped
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
// With a trust domain (the request's, else the configured one) and a tenant,
// an unset kind comes from the profile and an unset workload name from the
// CSR's common name, as the caadapter sets them on every other transport.
func (c callerTenantCA) SignCSRWithSPIFFE(ctx context.Context, csrPEM []byte, profile string, validityDays int, trustDomain, tenantID, kind, name string) (*x509.Certificate, error) {
	tenant := callerTenant(ctx, tenantID)
	td := trustDomain
	if td == "" {
		td = c.trustDomain
	}
	if td != "" && tenant != "" {
		if kind == "" {
			kind = service.SPIFFEKindForProfile(profile)
		}
		if name == "" {
			// A CSR that does not parse is left for the CA to reject.
			if block, _ := pem.Decode(csrPEM); block != nil {
				if csr, err := x509.ParseCertificateRequest(block.Bytes); err == nil {
					name = service.SPIFFEWorkloadName(csr.Subject.CommonName)
				}
			}
		}
	}
	return c.CA.SignCSRWithSPIFFE(ctx, csrPEM, profile, validityDays, td, tenant, kind, name)
}

// IssueCertificateRaw issues for the caller's tenant when tenantID is empty.
// With a configured trust domain and a tenant, it issues through the
// TrustedCA with the request the caadapter builds, so the certificate carries
// the tenant SPIFFE SAN; algorithm is ignored there, as go-xkms ignores it.
func (c callerTenantCA) IssueCertificateRaw(
	ctx context.Context,
	commonName, organization string,
	sans []string,
	validityDays int,
	profile, algorithm, tenantID string,
) (certPEM, chainPEM, keyPEM []byte, serialHex string, err error) {
	tenant := callerTenant(ctx, tenantID)
	if c.trustDomain == "" || tenant == "" || c.issuer == nil {
		return c.CA.IssueCertificateRaw(ctx, commonName, organization, sans, validityDays, profile, algorithm, tenant)
	}
	req := caadapter.KeypairCertificateRequest(commonName, organization, sans, validityDays, profile, tenant, c.trustDomain)
	issued, err := c.issuer.IssueCertificateWithProfile(ctx, req, profile)
	if err != nil {
		return nil, nil, nil, "", err
	}
	if issued == nil {
		return nil, nil, nil, "", errors.New("ca returned nil certificate")
	}
	chain := issued.CACertificatePEM
	if len(chain) == 0 {
		chain = issued.ChainPEM
	}
	if issued.SerialNumber != nil {
		serialHex = issued.SerialNumber.Text(16)
	}
	return issued.CertificatePEM, chain, issued.PrivateKeyPEM, serialHex, nil
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
